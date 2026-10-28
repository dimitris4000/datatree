// Package compact stores immutable document topology in persistent record chunks.
// It deliberately has no dependency on document parsers or application graphs.
package compact

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"datatree/internal/textstore"
)

type Input struct {
	OwnDepth int // deepest raw mapping-key descendant, relative to this value

	ID, Parent, Target   uint32
	Children             []uint32
	Kind, Label, Value   string
	Start, KeyStart, End int
	Pathable             bool
	NativeIndent         int
	Weight               int
	HasMetadata          bool
}
type Record struct {
	ID, Parent, Target                      uint32
	Kind, Label, Value                      string
	Start, KeyStart, End                    int
	ChildCount, Index, NativeIndent, Weight int
	Pathable, HasMetadata                   bool
}
type Change struct {
	Affected       []uint32
	RemovedRoots   []uint32
	StableGeometry bool
}

const chunkSize = 256

type kind uint8

const (
	objectKind kind = iota + 1
	arrayKind
	streamKind
	stringKind
	numberKind
	booleanKind
	nullKind
	aliasKind
	taggedKind
)

var kindNames = [...]string{"", "object", "array", "stream", "string", "number", "boolean", "null", "alias", "tagged"}

func parseKind(name string) (kind, error) {
	for i := 1; i < len(kindNames); i++ {
		if kindNames[i] == name {
			return kind(i), nil
		}
	}
	return 0, fmt.Errorf("unknown node kind %q", name)
}

type row struct {
	id, parent, target                                        uint32
	children                                                  []uint32
	label, value                                              string
	start, keyStart, end                                      textstore.Anchor
	kind                                                      kind
	index, indent, weight, count, subweight, height, ownDepth int
	pathable, metadata, hasKey                                bool
}
type chunk struct {
	rows [chunkSize]row
	used int
}

// Tree versions own their chunk directory and share unchanged immutable chunks.
// No row points to a predecessor Tree, parent record or raw parser object.
type Tree struct {
	text                              textstore.Text
	anchors                           *textstore.AnchorIndex
	chunks                            map[uint32]*chunk
	root                              uint32
	count, weight, maxNodes, maxDepth int
	free                              []uint32
	nextID                            uint64
	references                        map[uint32]int
}

func (t *Tree) Text() textstore.Text { return t.text }
func (t *Tree) Root() uint32         { return t.root }
func (t *Tree) Count() int           { return t.count }
func (t *Tree) Weight() int          { return t.weight }
func (t *Tree) get(id uint32) (row, bool) {
	if id == 0 {
		return row{}, false
	}
	c := t.chunks[id/chunkSize]
	if c == nil {
		return row{}, false
	}
	r := c.rows[id%chunkSize]
	return r, r.id == id
}
func (t *Tree) resolve(r row) (Record, bool) {
	start, e1 := t.anchors.ByteOffset(r.start)
	key, e2 := t.anchors.ByteOffset(r.keyStart)
	end, e3 := t.anchors.ByteOffset(r.end)
	if e1 != nil || e2 != nil || e3 != nil {
		return Record{}, false
	}
	return Record{ID: r.id, Parent: r.parent, Target: r.target, Kind: kindNames[r.kind], Label: r.label, Value: r.value, Start: start, KeyStart: key, End: end, ChildCount: len(r.children), Index: r.index, NativeIndent: r.indent, Weight: r.weight, Pathable: r.pathable, HasMetadata: r.metadata}, true
}
func (t *Tree) Record(id uint32) (Record, bool) {
	r, ok := t.get(id)
	if !ok {
		return Record{}, false
	}
	return t.resolve(r)
}
func (t *Tree) Children(id uint32, offset, limit int) []uint32 {
	r, ok := t.get(id)
	if !ok || offset < 0 || limit <= 0 || offset >= len(r.children) {
		return []uint32{}
	}
	end := len(r.children)
	if limit < end-offset {
		end = offset + limit
	}
	return append([]uint32(nil), r.children[offset:end]...)
}
func (t *Tree) Path(id uint32) string {
	r, ok := t.get(id)
	if !ok || !r.pathable {
		return ""
	}
	segments := []string{}
	for r.parent != 0 {
		parent, ok := t.get(r.parent)
		if !ok || !parent.pathable {
			return ""
		}
		switch parent.kind {
		case objectKind:
			segments = append(segments, pathKey(r.label))
		case arrayKind:
			segments = append(segments, "["+strconv.Itoa(r.index)+"]")
		default:
			return ""
		}
		r = parent
	}
	var b strings.Builder
	b.WriteByte('$')
	for i := len(segments) - 1; i >= 0; i-- {
		b.WriteString(segments[i])
	}
	return b.String()
}
func pathKey(key string) string {
	var b strings.Builder
	b.WriteString("['")
	for _, r := range key {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 32 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteString("']")
	return b.String()
}

// LocateByte uses half-open cursor containment and includes a mapping key's
// source span. A nonempty selection may include the selected node's end.
func (t *Tree) LocateByte(start, end int) (uint32, bool) {
	if start < 0 || end < start || end > t.text.ByteLen() {
		return 0, false
	}
	best := t.root
	bestWidth := t.text.ByteLen() + 1
	var visit func(uint32)
	visit = func(id uint32) {
		r, ok := t.Record(id)
		if !ok {
			return
		}
		lo, hi := r.KeyStart, r.End
		if id == t.root {
			lo, hi = 0, t.text.ByteLen()
		}
		if lo > start || hi < end || start == end && start == hi && lo < hi && id != t.root {
			return
		}
		if hi-lo < bestWidth {
			best, bestWidth = id, hi-lo
		}
		raw, _ := t.get(id)
		// Sibling source starts are monotone. Equal/overlapping starts are rare in
		// YAML; examine those candidates while skipping all later siblings.
		upper := len(raw.children)
		left, right := 0, upper
		for left < right {
			mid := (left + right) / 2
			c, ok := t.Record(raw.children[mid])
			if !ok || c.KeyStart > start {
				right = mid
			} else {
				left = mid + 1
			}
		}
		for i := left - 1; i >= 0; i-- {
			child, ok := t.Record(raw.children[i])
			if !ok {
				continue
			}
			if child.End < end {
				break
			}
			visit(child.ID)
		}
	}
	visit(t.root)
	return best, true
}
func (t *Tree) Preorder(ctx context.Context, visit func(Record) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stack := []uint32{t.root}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		r, ok := t.Record(id)
		if !ok {
			return errors.New("invalid compact record source anchors")
		}
		if !visit(r) {
			return ctx.Err()
		}
		raw, _ := t.get(id)
		for i := len(raw.children) - 1; i >= 0; i-- {
			stack = append(stack, raw.children[i])
		}
	}
	return nil
}

type mutation struct {
	tree    *Tree
	touched map[uint32]bool
}

func (m *mutation) put(r row) {
	index := r.id / chunkSize
	c := m.tree.chunks[index]
	if !m.touched[index] {
		copied := new(chunk)
		if c != nil {
			*copied = *c
		}
		c = copied
		m.tree.chunks[index] = c
		m.touched[index] = true
	}
	slot := r.id % chunkSize
	if c.rows[slot].id == 0 {
		c.used++
	}
	c.rows[slot] = r
}
func (m *mutation) remove(id uint32) {
	index := id / chunkSize
	c := m.tree.chunks[index]
	if c == nil {
		return
	}
	if !m.touched[index] {
		copied := *c
		c = &copied
		m.tree.chunks[index] = c
		m.touched[index] = true
	}
	slot := id % chunkSize
	if c.rows[slot].id != 0 {
		c.rows[slot] = row{}
		c.used--
	}
	if c.used == 0 {
		delete(m.tree.chunks, index)
		delete(m.touched, index)
	}
}
func (m *mutation) allocate() (uint32, error) {
	n := len(m.tree.free)
	if n > 0 {
		id := m.tree.free[n-1]
		m.tree.free = m.tree.free[:n-1]
		m.put(row{id: id})
		return id, nil
	}
	for m.tree.nextID <= uint64(^uint32(0)) {
		id := uint32(m.tree.nextID)
		m.tree.nextID++
		if _, ok := m.tree.get(id); !ok && id != 0 {
			m.put(row{id: id})
			return id, nil
		}
	}
	return 0, errors.New("compact node IDs exhausted")
}
