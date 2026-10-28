package textstore

import (
	"math/rand"
	"strings"
	"testing"
)

func TestAnchorsMoveWithTheirOriginalPieces(t *testing.T) {
	text := New([]byte("one 😀 two\r\nthree"))
	start, err := text.AnchorAt(9)
	if err != nil {
		t.Fatal(err)
	}
	end, _ := text.AnchorEnd(text.ByteLen())
	next, err := text.ReplaceBytes(0, 3, []byte("longer"))
	if err != nil {
		t.Fatal(err)
	}
	index := next.AnchorIndex()
	if at, err := index.ByteOffset(start); err != nil || at != 12 {
		t.Fatalf("moved anchor %d %v", at, err)
	}
	if at, err := index.ByteOffset(end); err != nil || at != next.ByteLen() {
		t.Fatal("end anchor", at, err)
	}
	if at, err := text.AnchorIndex().ByteOffset(start); err != nil || at != 9 {
		t.Fatal("old version anchor changed")
	}
	removed, _ := text.AnchorAt(1)
	if _, err := index.ByteOffset(removed); err == nil {
		t.Fatal("removed interior anchor resolved")
	}
}

func TestAnchoredUnchangedSpansSurviveManyReplacements(t *testing.T) {
	type span struct {
		start, end  int
		left, right Anchor
	}
	raw := strings.Repeat("abcdefghij", 20)
	text := New([]byte(raw))
	spans := []span{}
	for i := 0; i < len(raw); i++ {
		left, _ := text.AnchorAt(i)
		right, _ := text.AnchorEnd(i + 1)
		spans = append(spans, span{i, i + 1, left, right})
	}
	random := rand.New(rand.NewSource(59))
	for step := 0; step < 200; step++ {
		start := random.Intn(len(raw) + 1)
		end := min(len(raw), start+random.Intn(4))
		insert := strings.Repeat("X", random.Intn(4))
		next, err := text.ReplaceBytes(start, end, []byte(insert))
		if err != nil {
			t.Fatal(err)
		}
		delta := len(insert) - (end - start)
		kept := spans[:0]
		for _, s := range spans {
			if start < s.end && end > s.start {
				continue
			}
			if s.start >= end {
				s.start += delta
				s.end += delta
			}
			kept = append(kept, s)
		}
		spans = kept
		text = next
		raw = raw[:start] + insert + raw[end:]
		index := text.AnchorIndex()
		for _, s := range spans {
			a, e1 := index.ByteOffset(s.left)
			b, e2 := index.ByteOffset(s.right)
			if e1 != nil || e2 != nil || a != s.start || b != s.end {
				t.Fatalf("step%d: span %+v got%d,%d %v/%v", step, s, a, b, e1, e2)
			}
		}
	}
}

func TestBoundaryBiasAcrossInsertion(t *testing.T) {
	text := New([]byte("ab"))
	before, _ := text.AnchorEnd(1)
	after, _ := text.AnchorAt(1)
	next, err := text.ReplaceBytes(1, 1, []byte("insert"))
	if err != nil {
		t.Fatal(err)
	}
	index := next.AnchorIndex()
	if at, _ := index.ByteOffset(before); at != 1 {
		t.Fatal("left boundary moved", at)
	}
	if at, _ := index.ByteOffset(after); at != 7 {
		t.Fatal("right boundary stayed", at)
	}
}

func TestIndexedPositionAcrossPiecesAndCRLF(t *testing.T) {
	text := New([]byte(strings.Repeat("α😀\r\nx", 6000)))
	for _, at := range []int{0, 2, 6, 8, 9, 9 * 3000, text.ByteLen()} {
		point, err := text.Position(at)
		if err != nil {
			t.Fatal(at, err)
		}
		prefix := text.Bytes()[:at]
		ref := FromImmutable(prefix)
		if point.UTF16 != ref.Len() || point.Line != ref.Lines() {
			t.Fatalf("position %d: %+v", at, point)
		}
		back, err := text.ByteOffset(point.UTF16)
		if err != nil || back != at {
			t.Fatalf("position roundtrip %d -> %+v -> %d %v", at, point, back, err)
		}
		anchor, _ := text.AnchorAt(at)
		if back, err := text.AnchorIndex().ByteOffset(anchor); err != nil || back != at {
			t.Fatal("piece anchor roundtrip", at, back, err)
		}
	}
	point, _ := text.Position(9)
	if point.Line != 2 || point.Column != 2 {
		t.Fatal("rune column", point)
	}
	if _, err := text.Position(1); err == nil {
		t.Fatal("accepted split UTF8")
	}
}

func TestEmptyAnchorAndInsert(t *testing.T) {
	var text Text
	anchor, err := text.AnchorAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if at, err := text.AnchorIndex().ByteOffset(anchor); err != nil || at != 0 {
		t.Fatal(at, err)
	}
	text, err = text.ReplaceBytes(0, 0, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := text.AnchorIndex().ByteOffset(anchor); err == nil {
		t.Fatal("empty origin anchor resolved in another text")
	}
}
