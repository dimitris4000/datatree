package textstore

import (
	"errors"
	"sort"
	"unicode/utf8"
)

// Anchor identifies a boundary within immutable source pieces. End anchors are
// left-biased; start anchors are right-biased when an insertion splits a piece.
type Anchor uint64

const anchorLeft uint32 = 1 << 31
const anchorOffsetMask = anchorLeft - 1

func (t Text) AnchorAt(at int) (Anchor, error)  { return t.anchor(at, false) }
func (t Text) AnchorEnd(at int) (Anchor, error) { return t.anchor(at, true) }
func (t Text) anchor(at int, left bool) (Anchor, error) {
	if at < 0 || at > t.ByteLen() {
		return 0, errBoundary
	}
	n := t.root
	if n == nil {
		return 0, nil
	}
	for n.data == nil {
		if at < n.left.bytes || (left && at == n.left.bytes) {
			n = n.left
		} else {
			at -= n.left.bytes
			n = n.right
		}
	}
	if at < len(n.data) && !utf8.RuneStart(n.data[at]) {
		return 0, errBoundary
	}
	offset := n.originOffset + uint32(at)
	if left {
		offset |= anchorLeft
	}
	return Anchor(uint64(n.origin)<<32 | uint64(offset)), nil
}

type anchorPart struct {
	start, end uint32
	at         int
}

// AnchorIndex belongs to one immutable Text version. Construction is
// proportional to source pieces, not bytes or document nodes.
type AnchorIndex struct {
	parts map[uint32][]anchorPart
	bytes int
}

func (t Text) AnchorIndex() *AnchorIndex {
	index := &AnchorIndex{parts: make(map[uint32][]anchorPart), bytes: t.ByteLen()}
	var walk func(*node, int)
	walk = func(n *node, at int) {
		if n == nil {
			return
		}
		if n.data != nil {
			index.parts[n.origin] = append(index.parts[n.origin], anchorPart{n.originOffset, n.originOffset + uint32(n.bytes), at})
			return
		}
		walk(n.left, at)
		walk(n.right, at+n.left.bytes)
	}
	walk(t.root, 0)
	for id, parts := range index.parts {
		sort.Slice(parts, func(i, j int) bool { return parts[i].start < parts[j].start })
		index.parts[id] = parts
	}
	return index
}

var errRemovedAnchor = errors.New("source anchor does not exist in this version")

func (i *AnchorIndex) ByteOffset(anchor Anchor) (int, error) {
	if anchor == 0 {
		if i.bytes == 0 {
			return 0, nil
		}
		return 0, errRemovedAnchor
	}
	origin := uint32(uint64(anchor) >> 32)
	encoded := uint32(anchor)
	offset := encoded & anchorOffsetMask
	parts := i.parts[origin]
	// Most origins have one fragment. Binary search also bounds lookup for an
	// origin split many times by edits within the same initial source piece.
	n := sort.Search(len(parts), func(n int) bool { return parts[n].end >= offset })
	found := false
	result := 0
	for ; n < len(parts) && parts[n].start <= offset; n++ {
		part := parts[n]
		if offset > part.end {
			continue
		}
		at := part.at + int(offset-part.start)
		if !found || (encoded&anchorLeft != 0 && at < result) || (encoded&anchorLeft == 0 && at > result) {
			result = at
			found = true
		}
	}
	if !found {
		return 0, errRemovedAnchor
	}
	return result, nil
}

type Position struct{ UTF16, Line, Column int }

// Position reads aggregate tree metrics and at most one source piece. Columns
// count Unicode runes; UTF16 counts normalized editor units.
func (t Text) Position(at int) (Position, error) {
	if at < 0 || at > t.ByteLen() {
		return Position{}, errBoundary
	}
	metrics, err := prefixMetrics(t.root, at)
	if err != nil {
		return Position{}, err
	}
	if metrics == nil {
		return Position{Line: 1, Column: 1}, nil
	}
	return Position{metrics.units, metrics.breaks + 1, metrics.tail + 1}, nil
}

func prefixMetrics(n *node, at int) (*node, error) {
	if n == nil || at == 0 {
		return nil, nil
	}
	if at == n.bytes {
		return n, nil
	}
	if n.data != nil {
		if !utf8.Valid(n.data[:at]) {
			return nil, errBoundary
		}
		return leafPart(n.data[:at], n.origin, n.originOffset), nil
	}
	if at <= n.left.bytes {
		return prefixMetrics(n.left, at)
	}
	right, err := prefixMetrics(n.right, at-n.left.bytes)
	if err != nil {
		return nil, err
	}
	return branch(n.left, right), nil
}
