// Package textstore provides persistent UTF-8 text with normalized UTF-16
// coordinates. Versions share immutable pieces; edits copy only tree paths.
package textstore

import (
	"bytes"
	"errors"
	"io"
	"sync/atomic"
	"unicode/utf8"
)

const pieceBytes = 32768

var errBoundary = errors.New("text offset is out of range or splits a UTF-16 surrogate pair")

type node struct {
	left, right                  *node
	data                         []byte
	bytes, units, breaks, height int
	runes, tail                  int
	origin, originOffset         uint32
	first, last                  byte
}

// Text is an immutable value; its zero value is an empty document.
type Text struct{ root *node }

// New copies data, so caller mutations cannot affect this or future versions.
func New(data []byte) Text { return FromImmutable(bytes.Clone(data)) }

// FromImmutable shares data. The caller must never modify the supplied bytes.
func FromImmutable(data []byte) Text {
	var root *node
	for len(data) > 0 {
		end := min(pieceBytes, len(data))
		for end < len(data) && !utf8.RuneStart(data[end]) {
			end++
		}
		root = join(root, leaf(data[:end]))
		data = data[end:]
	}
	return Text{root}
}

var nextOrigin atomic.Uint32

func leaf(data []byte) *node {
	if len(data) == 0 {
		return nil
	}
	id := nextOrigin.Add(1)
	if id == 0 {
		panic("source origin identities exhausted")
	}
	return leafPart(data, id, 0)
}

func leafPart(data []byte, origin, originOffset uint32) *node {
	if len(data) == 0 {
		return nil
	}
	n := &node{data: data, bytes: len(data), height: 1, first: data[0], last: data[len(data)-1], origin: origin, originOffset: originOffset}
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			size++
		}
		n.units++
		n.runes++
		if r > 0xffff {
			n.units++
		}
		if r == '\r' || r == '\n' {
			n.breaks++
			n.tail = 0
		} else {
			n.tail++
		}
		i += size
	}
	return n
}

func height(n *node) int {
	if n == nil {
		return 0
	}
	return n.height
}
func branch(a, b *node) *node {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	n := &node{left: a, right: b, bytes: a.bytes + b.bytes, units: a.units + b.units, breaks: a.breaks + b.breaks, runes: a.runes + b.runes, height: max(a.height, b.height) + 1, first: a.first, last: b.last}
	if b.breaks > 0 {
		n.tail = b.tail
	} else {
		n.tail = a.tail + b.runes
	}
	if a.last == '\r' && b.first == '\n' {
		n.units--
		n.breaks--
		n.runes--
	}
	return n
}

func balance(n *node) *node {
	if n == nil || n.data != nil {
		return n
	}
	if height(n.left) > height(n.right)+1 {
		a := n.left
		if height(a.left) < height(a.right) {
			a = branch(branch(a.left, a.right.left), a.right.right)
		}
		return branch(a.left, branch(a.right, n.right))
	}
	if height(n.right) > height(n.left)+1 {
		b := n.right
		if height(b.right) < height(b.left) {
			b = branch(b.left.left, branch(b.left.right, b.right))
		}
		return branch(branch(n.left, b.left), b.right)
	}
	return n
}

func join(a, b *node) *node {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.height > b.height+1 {
		return balance(branch(a.left, join(a.right, b)))
	}
	if b.height > a.height+1 {
		return balance(branch(join(a, b.left), b.right))
	}
	return branch(a, b)
}

func split(n *node, at int) (*node, *node) {
	if n == nil {
		return nil, nil
	}
	if at == 0 {
		return nil, n
	}
	if at == n.bytes {
		return n, nil
	}
	if n.data != nil {
		return leafPart(n.data[:at], n.origin, n.originOffset), leafPart(n.data[at:], n.origin, n.originOffset+uint32(at))
	}
	if at < n.left.bytes {
		a, b := split(n.left, at)
		return a, join(b, n.right)
	}
	a, b := split(n.right, at-n.left.bytes)
	return join(n.left, a), b
}

// Len reports normalized UTF-16 units. CRLF occupies one unit, as in editors.
func (t Text) Len() int {
	if t.root == nil {
		return 0
	}
	return t.root.units
}
func (t Text) ByteLen() int {
	if t.root == nil {
		return 0
	}
	return t.root.bytes
}
func (t Text) Lines() int {
	if t.root == nil {
		return 1
	}
	return t.root.breaks + 1
}

// ByteOffset resolves a normalized UTF-16 position without flattening the text.
func (t Text) ByteOffset(offset int) (int, error) { return byteOffset(t.root, offset) }

func byteOffset(n *node, units int) (int, error) {
	if n == nil {
		if units == 0 {
			return 0, nil
		}
		return 0, errBoundary
	}
	if units < 0 || units > n.units {
		return 0, errBoundary
	}
	if units == 0 {
		return 0, nil
	}
	if units == n.units {
		return n.bytes, nil
	}
	if n.data != nil {
		at := 0
		for units > 0 {
			r, size := utf8.DecodeRune(n.data[at:])
			cost := 1
			if r > 0xffff {
				cost = 2
			}
			if cost > units {
				return 0, errBoundary
			}
			if r == '\r' && at+1 < len(n.data) && n.data[at+1] == '\n' {
				size++
			}
			units -= cost
			at += size
		}
		return at, nil
	}
	if units < n.left.units {
		return byteOffset(n.left, units)
	}
	remaining := units - n.left.units
	if n.left.last == '\r' && n.right.first == '\n' {
		remaining++
	}
	at, err := byteOffset(n.right, remaining)
	return n.left.bytes + at, err
}

// Replace uses normalized UTF-16 offsets in the old version and preserves all
// bytes outside that range, including CRLF and non-ASCII text.
func (t Text) Replace(start, end int, data []byte) (Text, error) {
	if end < start || !utf8.Valid(data) {
		return Text{}, errBoundary
	}
	a, err := byteOffset(t.root, start)
	if err != nil {
		return Text{}, err
	}
	b, err := byteOffset(t.root, end)
	if err != nil {
		return Text{}, err
	}
	left, tail := split(t.root, a)
	_, right := split(tail, b-a)
	return Text{join(join(left, New(data).root), right)}, nil
}

// ReplaceBytes is for validated byte-span patches from parsers. Endpoints must
// lie on UTF-8 boundaries; unlike Replace they may split a CRLF sequence.
func (t Text) ReplaceBytes(start, end int, data []byte) (Text, error) {
	if start < 0 || end < start || end > t.ByteLen() || !utf8.Valid(data) {
		return Text{}, errBoundary
	}
	left, tail := split(t.root, start)
	removed, right := split(tail, end-start)
	if (removed != nil && !utf8.Valid(firstPiece(removed))) || (right != nil && !utf8.RuneStart(right.first)) {
		return Text{}, errBoundary
	}
	return Text{join(join(left, New(data).root), right)}, nil
}
func firstPiece(n *node) []byte {
	for n.data == nil {
		n = n.left
	}
	return n.data
}

func (t Text) Slice(start, end int) ([]byte, error) {
	if end < start {
		return nil, errBoundary
	}
	a, err := byteOffset(t.root, start)
	if err != nil {
		return nil, err
	}
	b, err := byteOffset(t.root, end)
	if err != nil {
		return nil, err
	}
	_, tail := split(t.root, a)
	middle, _ := split(tail, b-a)
	return (Text{middle}).Bytes(), nil
}

func (t Text) Bytes() []byte {
	b := make([]byte, 0, t.ByteLen())
	var walk func(*node)
	walk = func(n *node) {
		if n == nil {
			return
		}
		if n.data != nil {
			b = append(b, n.data...)
			return
		}
		walk(n.left)
		walk(n.right)
	}
	walk(t.root)
	return b
}

// WriteTo streams pieces without allocating a contiguous copy.
func (t Text) WriteTo(w io.Writer) (int64, error) {
	var total int64
	var walk func(*node) error
	walk = func(n *node) error {
		if n == nil {
			return nil
		}
		if n.data != nil {
			written, err := w.Write(n.data)
			total += int64(written)
			if err == nil && written != len(n.data) {
				return io.ErrShortWrite
			}
			return err
		}
		if err := walk(n.left); err != nil {
			return err
		}
		return walk(n.right)
	}
	err := walk(t.root)
	return total, err
}
