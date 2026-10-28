package document

import (
	"bytes"
	"context"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// TryScalarPatchContext applies an exact-byte-span JSON/JSONL scalar patch when
// byte and source-rune lengths remain unchanged. A false result requests the
// normal validated full-parse fallback. The original document is immutable.
func (d *Document) TryScalarPatchContext(ctx context.Context, start, end int, text string) (*Document, bool, error) {
	if d.compact != nil {
		return d.compactScalarPatch(ctx, start, end, text)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if d.Format != "json" && d.Format != "jsonl" {
		return nil, false, nil
	}
	count := 0
	for _, n := range d.Nodes {
		count++
		if count&8191 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
		}
		if n.Start == start && n.End == end && n.raw.Kind == yaml.ScalarNode {
			replacement := []byte(text)
			if len(replacement) != end-start {
				return nil, false, nil
			}
			incoming, err := ParseWithLimitsContext(ctx, "json", replacement, d.Limits())
			if err != nil {
				return nil, false, err
			}
			return d.tryScalarReplacement(ctx, n, replacement, incoming.raw)
		}
	}
	return nil, false, nil
}

func (d *Document) tryScalarReplacement(ctx context.Context, target *Node, replacement []byte, incoming *yaml.Node) (*Document, bool, error) {
	if d.Format != "json" && d.Format != "jsonl" || target.raw.Kind != yaml.ScalarNode || incoming.Kind != yaml.ScalarNode || len(replacement) != target.End-target.Start || len(bytes.TrimSpace(replacement)) != len(replacement) || bytes.ContainsAny(replacement, "\r\n") || utf8.RuneCount(replacement) != utf8.RuneCount(d.Source[target.Start:target.End]) || supplementaryRunes(replacement) != supplementaryRunes(d.Source[target.Start:target.End]) {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	next := &Document{Format: d.Format, Source: bytes.Clone(d.Source), Nodes: make(map[string]*Node, len(d.Nodes)), headers: d.headers, limits: d.limits, incremental: true}
	copy(next.Source[target.Start:target.End], replacement)
	count := 0
	for id, n := range d.Nodes {
		count++
		if count&8191 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
		}
		next.Nodes[id] = n
	}
	raw := *incoming
	raw.Line, raw.Column = target.raw.Line, target.raw.Column
	child := *target
	child.raw = &raw
	child.Kind = scalarKind(&raw)
	child.Value = raw.Value
	current := &child
	next.Nodes[current.ID] = current
	next.changedNodes = append(next.changedNodes, current.ID)
	original := target
	for original.parent != nil {
		parent := d.Nodes[original.parent.ID]
		copied := *parent
		parentRaw := *parent.raw
		parentRaw.Content = append([]*yaml.Node(nil), parent.raw.Content...)
		copied.raw = &parentRaw
		copied.Children = append([]*Node(nil), parent.Children...)
		for i, n := range parent.Children {
			if n.ID == original.ID {
				copied.Children[i] = current
				rawIndex := i
				if parent.raw.Kind == yaml.MappingNode {
					rawIndex = 2*i + 1
				}
				parentRaw.Content[rawIndex] = current.raw
				break
			}
		}
		current = &copied
		next.Nodes[current.ID] = current
		next.changedNodes = append(next.changedNodes, current.ID)
		original = parent
	}
	// Copied nodes need only ancestor identities for read-only marking. A
	// pointer to the full copied ancestor would let shared siblings retain its
	// entire previous raw/Children graph across alternating edits.
	var thinParent *Node
	for i := len(next.changedNodes) - 1; i >= 0; i-- {
		n := next.Nodes[next.changedNodes[i]]
		n.parent = thinParent
		thinParent = &Node{ID: n.ID, Kind: n.Kind, Path: n.Path, parent: thinParent}
	}
	next.Root, next.raw = current, current.raw
	// Geometry is unchanged. Share an already-built immutable index without
	// retaining a predecessor document or forcing an expensive initial build.
	next.sourceIndex.Store(d.sourceIndex.Load())
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return next, true, nil
}

func supplementaryRunes(source []byte) int {
	count := 0
	for len(source) > 0 {
		r, size := utf8.DecodeRune(source)
		source = source[size:]
		if r > 0xffff {
			count++
		}
	}
	return count
}

// StableChanges identifies nodes needing refresh after a same-span scalar edit.
// Nil means IDs may have changed and callers must rebuild their cached tree.
func (d *Document) StableChanges() []string {
	if !d.incremental {
		return nil
	}
	return append([]string(nil), d.changedNodes...)
}
