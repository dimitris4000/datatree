package textstore

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func TestReplaceSharesVersionsAndNormalizesCoordinates(t *testing.T) {
	original := New([]byte("a\r\n😀\rb\n"))
	if original.Len() != 7 || original.ByteLen() != 10 || original.Lines() != 4 {
		t.Fatalf("metrics %d/%d/%d", original.Len(), original.ByteLen(), original.Lines())
	}
	next, err := original.Replace(2, 4, []byte("世界"))
	if err != nil {
		t.Fatal(err)
	}
	if string(next.Bytes()) != "a\r\n世界\rb\n" {
		t.Fatalf("replacement %q", next.Bytes())
	}
	if string(original.Bytes()) != "a\r\n😀\rb\n" {
		t.Fatal("mutated original version")
	}
	var out bytes.Buffer
	if _, err := next.WriteTo(&out); err != nil || !bytes.Equal(out.Bytes(), next.Bytes()) {
		t.Fatal("stream differs", err)
	}
}

func TestRandomUnicodeAndCRLFEditsPreserveEarlierVersions(t *testing.T) {
	random := rand.New(rand.NewSource(42))
	raw := "start\r\n😀 é\rend\n"
	text := New([]byte(raw))
	for step := 0; step < 1000; step++ {
		boundaries := []int{0}
		for i := 0; i < len(raw); {
			_, size := utf8.DecodeRuneInString(raw[i:])
			if raw[i] == '\r' && i+1 < len(raw) && raw[i+1] == '\n' {
				size++
			}
			i += size
			boundaries = append(boundaries, i)
		}
		a := random.Intn(len(boundaries))
		b := a + random.Intn(len(boundaries)-a)
		normalize := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n") }
		start := len(utf16.Encode([]rune(normalize(raw[:boundaries[a]]))))
		end := len(utf16.Encode([]rune(normalize(raw[:boundaries[b]]))))
		insert := []string{"é", "😀", "\r", "\n", "\r\n", "abc", ""}[random.Intn(7)]
		before := text
		next, err := text.Replace(start, end, []byte(insert))
		if err != nil {
			t.Fatal(step, err)
		}
		if string(before.Bytes()) != raw {
			t.Fatal("changed an earlier version")
		}
		raw = raw[:boundaries[a]] + insert + raw[boundaries[b]:]
		text = next
		if string(text.Bytes()) != raw || text.Len() != len(utf16.Encode([]rune(normalize(raw)))) || text.Lines() != strings.Count(normalize(raw), "\n")+1 {
			t.Fatalf("step %d: inconsistent text metrics", step)
		}
	}
}

func TestByteReplacementRejectsBrokenUTF8Boundaries(t *testing.T) {
	text := New([]byte("a😀éz"))
	for _, span := range [][2]int{{2, 5}, {1, 4}, {3, 3}, {0, 6}} {
		if _, err := text.ReplaceBytes(span[0], span[1], nil); err == nil {
			t.Fatal("accepted split UTF8", span)
		}
	}
	next, err := text.ReplaceBytes(1, 5, []byte("é"))
	if err != nil || string(next.Bytes()) != "aééz" {
		t.Fatal("valid byte edit failed", err)
	}
}

func TestRejectsInvalidBoundariesAndUTF8(t *testing.T) {
	s := New([]byte("😀"))
	for _, bounds := range [][2]int{{-1, 0}, {0, 3}, {1, 2}, {2, 1}} {
		if _, err := s.Replace(bounds[0], bounds[1], nil); err == nil {
			t.Fatalf("accepted %v", bounds)
		}
	}
	if _, err := s.Replace(0, 0, []byte{0xff}); err == nil {
		t.Fatal("accepted invalid UTF8")
	}
}

func TestCRLFAcrossPiecesHasOneCoordinate(t *testing.T) {
	s := New([]byte("a\nb"))
	next, err := s.Replace(1, 1, []byte("\r"))
	if err != nil {
		t.Fatal(err)
	}
	if next.Len() != 3 || next.Lines() != 2 {
		t.Fatalf("cross-piece CRLF metrics %d %d", next.Len(), next.Lines())
	}
	again, err := next.Replace(2, 3, []byte("c"))
	if err != nil || string(again.Bytes()) != "a\r\nc" {
		t.Fatalf("bad normalized replacement %q %v", again.Bytes(), err)
	}
}

func TestManyEditsAndSlices(t *testing.T) {
	plain := strings.Repeat("abcdef\n", 20000)
	s := New([]byte(plain))
	for i := 0; i < 1000; i++ {
		at := (i * 101) % len(plain)
		next, err := s.Replace(at, at+1, []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		s = next
		plain = plain[:at] + "x" + plain[at+1:]
	}
	if !bytes.Equal(s.Bytes(), []byte(plain)) {
		t.Fatal("edits differ")
	}
	slice, err := s.Slice(500, 1000)
	if err != nil || string(slice) != plain[500:1000] {
		t.Fatal("slice differs", err)
	}
	if s.root.height > 32 {
		t.Fatal("unbalanced tree", s.root.height)
	}
}
