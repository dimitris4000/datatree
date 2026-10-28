package compact

import (
	"context"
	"testing"

	"datatree/internal/textstore"
)

func BenchmarkScalarReplace100KRecords(b *testing.B) {
	const count = 100000
	source := make([]byte, 0, 2*count+1)
	source = append(source, '[')
	inputs := make([]Input, count+1)
	children := make([]uint32, count)
	for i := 0; i < count; i++ {
		if i > 0 {
			source = append(source, ',')
		}
		start := len(source)
		source = append(source, '0')
		id := uint32(i + 2)
		children[i] = id
		inputs[i+1] = Input{ID: id, Parent: 1, Kind: "number", Value: "0", Start: start, KeyStart: -1, End: start + 1, Pathable: true}
	}
	source = append(source, ']')
	inputs[0] = Input{ID: 1, Children: children, Kind: "array", Start: 0, KeyStart: -1, End: len(source), Pathable: true}
	tree, err := New(context.Background(), textstore.New(source), inputs, 1, count+1, 2)
	if err != nil {
		b.Fatal(err)
	}
	fragment := []Input{{ID: 1, Kind: "number", Value: "1234567", Start: 0, KeyStart: -1, End: 7, Pathable: true}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		next, _, err := tree.Replace(context.Background(), 50000, []byte("1234567"), fragment, 1)
		if err != nil || next.Count() != count+1 {
			b.Fatal(err)
		}
	}
}
