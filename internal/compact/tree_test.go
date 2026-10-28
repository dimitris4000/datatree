package compact

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"datatree/internal/textstore"
)

func sampleInputs() ([]byte, []Input) {
	source := []byte(`{"a":[1,2],"b":3}`)
	return source, []Input{
		{ID: 1, Children: []uint32{2, 5}, Kind: "object", Start: 0, KeyStart: -1, End: 17, Pathable: true, Weight: 1},
		{ID: 2, Parent: 1, Children: []uint32{3, 4}, Kind: "array", Label: "a", Start: 5, KeyStart: 1, End: 10, Pathable: true, Weight: 2},
		{ID: 3, Parent: 2, Kind: "number", Label: "[0]", Value: "1", Start: 6, KeyStart: -1, End: 7, Pathable: true, Weight: 1},
		{ID: 4, Parent: 2, Kind: "number", Label: "[1]", Value: "2", Start: 8, KeyStart: -1, End: 9, Pathable: true, Weight: 1},
		{ID: 5, Parent: 1, Kind: "number", Label: "b", Value: "3", Start: 16, KeyStart: 11, End: 17, Pathable: true, Weight: 2},
	}
}
func newSample(t *testing.T) *Tree {
	t.Helper()
	source, inputs := sampleInputs()
	inputs[0].End = len(source)
	inputs[4].Start = strings.LastIndex(string(source), "3")
	inputs[4].End = inputs[4].Start + 1
	tree, err := New(context.Background(), textstore.New(source), inputs, 1, 100, 8)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
func TestNewNavigationAndImmutability(t *testing.T) {
	source, inputs := sampleInputs()
	inputs[0].End = len(source)
	inputs[4].Start = 15
	inputs[4].End = 16
	tree, err := New(context.Background(), textstore.New(source), inputs, 1, 100, 8)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Root() != 1 || tree.Count() != 5 || tree.Weight() != 7 {
		t.Fatalf("root/count/weight %d %d %d", tree.Root(), tree.Count(), tree.Weight())
	}
	inputs[0].Children[0] = 99
	inputs[2].Value = "changed"
	source[0] = '!'
	if got := tree.Children(1, 0, 1); !reflect.DeepEqual(got, []uint32{2}) {
		t.Fatal(got)
	}
	page := tree.Children(1, 0, 2)
	page[0] = 99
	if tree.Children(1, 0, 2)[0] != 2 {
		t.Fatal("caller changed child vector")
	}
	if tree.Path(4) != "$['a'][1]" || tree.Path(5) != "$['b']" {
		t.Fatalf("paths %q %q", tree.Path(4), tree.Path(5))
	}
	r, ok := tree.Record(4)
	if !ok || r.Index != 1 || r.Start != 8 || r.End != 9 {
		t.Fatalf("record %+v %v", r, ok)
	}
	if id, ok := tree.LocateByte(1, 3); !ok || id != 2 {
		t.Fatalf("key locate %d %v", id, ok)
	}
	if id, ok := tree.LocateByte(7, 7); !ok || id != 2 {
		t.Fatalf("terminal cursor %d %v", id, ok)
	}
	var ids []uint32
	if err := tree.Preorder(context.Background(), func(r Record) bool { ids = append(ids, r.ID); return true }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []uint32{1, 2, 3, 4, 5}) {
		t.Fatal(ids)
	}
	if string(tree.Text().Bytes()) != `{"a":[1,2],"b":3}` {
		t.Fatal("source mutated")
	}
}
func TestReplaceKeepsUnrelatedIDsAndMovesAnchoredSpans(t *testing.T) {
	tree := newSample(t)
	fragment := []Input{{ID: 9, Kind: "string", Value: "😀😀", Start: 0, KeyStart: -1, End: 10, Pathable: true, Weight: 1}}
	next, change, err := tree.Replace(context.Background(), 3, []byte(`"😀😀"`), fragment, 9)
	if err != nil {
		t.Fatal(err)
	}
	if string(next.Text().Bytes()) != `{"a":["😀😀",2],"b":3}` || string(tree.Text().Bytes()) != `{"a":[1,2],"b":3}` {
		t.Fatal("immutable source replacement failed")
	}
	if !reflect.DeepEqual(change.Affected, []uint32{3, 2, 1}) || change.StableGeometry || len(change.RemovedRoots) != 0 {
		t.Fatalf("change %+v", change)
	}
	r, ok := next.Record(5)
	if !ok || r.Start != 24 || r.End != 25 || r.Label != "b" {
		t.Fatalf("unrelated record %+v %v", r, ok)
	}
	if next.Count() != 5 || next.Weight() != 7 || next.Path(3) != "$['a'][0]" {
		t.Fatal("topology or weights changed")
	}
	old, _ := tree.Record(3)
	if old.Value != "1" {
		t.Fatal("old record mutated")
	}
}
func TestReplaceSubtreeReusesDeletedSlotsAndPreservesMappingWeight(t *testing.T) {
	tree := newSample(t)
	originalB, _ := tree.Record(5)
	scalar := []Input{{ID: 1, Kind: "number", Value: "9", Start: 0, KeyStart: -1, End: 1, Pathable: true, Weight: 1}}
	next, change, err := tree.Replace(context.Background(), 2, []byte("9"), scalar, 1)
	if err != nil {
		t.Fatal(err)
	}
	if next.Count() != 3 || next.Weight() != 5 || !reflect.DeepEqual(change.RemovedRoots, []uint32{3, 4}) {
		t.Fatalf("after deletion count=%d weight=%d %+v", next.Count(), next.Weight(), change)
	}
	if _, ok := next.Record(3); ok {
		t.Fatal("deleted descendant remains")
	}
	for i := 0; i < 100; i++ {
		fragment := []Input{{ID: 1, Children: []uint32{2, 3}, Kind: "array", Start: 0, KeyStart: -1, End: 5, Pathable: true, Weight: 1}, {ID: 2, Parent: 1, Kind: "number", Value: "1", Start: 1, KeyStart: -1, End: 2, Pathable: true, Weight: 1}, {ID: 3, Parent: 1, Kind: "number", Value: "2", Start: 3, KeyStart: -1, End: 4, Pathable: true, Weight: 1}}
		next, _, err = next.Replace(context.Background(), 2, []byte("[1,2]"), fragment, 1)
		if err != nil {
			t.Fatal(err)
		}
		next, _, err = next.Replace(context.Background(), 2, []byte("9"), scalar, 1)
		if err != nil {
			t.Fatal(err)
		}
	}
	if next.Count() != 3 || next.Weight() != 5 {
		t.Fatal("replacement weight accumulated")
	}
	b, _ := next.Record(5)
	if b.Label != originalB.Label || b.Parent != originalB.Parent {
		t.Fatal("unrelated identity changed")
	}
}
func TestReplaceValidatesLimitsAndCancellationAtomically(t *testing.T) {
	tree := newSample(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := tree.Replace(ctx, 3, []byte("2"), []Input{{ID: 1, Kind: "number", Start: 0, KeyStart: -1, End: 1}}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel %v", err)
	}
	if err := tree.Preorder(ctx, func(Record) bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("preorder cancel %v", err)
	}
	source, inputs := sampleInputs()
	inputs[0].End = len(source)
	inputs[4].Start = 15
	inputs[4].End = 16
	if _, err := New(context.Background(), textstore.New(source), inputs, 1, 6, 8); err == nil {
		t.Fatal("raw-node weight limit ignored")
	}
	if _, err := New(context.Background(), textstore.New(source), inputs, 1, 100, 1); err == nil {
		t.Fatal("depth limit ignored")
	}
	malformed := []Input{{ID: 1, Children: []uint32{1}, Kind: "array", Start: 0, KeyStart: -1, End: 1}}
	if _, _, err := tree.Replace(context.Background(), 3, []byte("1"), malformed, 1); err == nil {
		t.Fatal("cyclic fragment accepted")
	}
	if string(tree.Text().Bytes()) != `{"a":[1,2],"b":3}` {
		t.Fatal("failure changed baseline")
	}
}
func TestUTF16CRLFAnchorsAndEscapedPaths(t *testing.T) {
	source := []byte("{\r\n\"é'😀\":\"😀\",\r\n\"z\":1}")
	start := strings.Index(string(source), `:"`) + 1
	end := start + len(`"😀"`)
	z := strings.LastIndex(string(source), "1")
	inputs := []Input{{ID: 1, Children: []uint32{2, 3}, Kind: "object", Start: 0, KeyStart: -1, End: len(source), Pathable: true}, {ID: 2, Parent: 1, Kind: "string", Label: "é'😀", Value: "😀", Start: start, KeyStart: 3, End: end, Pathable: true, Weight: 2}, {ID: 3, Parent: 1, Kind: "number", Label: "z", Value: "1", Start: z, KeyStart: z - 4, End: z + 1, Pathable: true, Weight: 2}}
	tree, err := New(context.Background(), textstore.New(source), inputs, 1, 100, 8)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Path(2) != "$['é\\'😀']" {
		t.Fatal(tree.Path(2))
	}
	next, _, err := tree.Replace(context.Background(), 2, []byte(`"é"`), []Input{{ID: 1, Kind: "string", Value: "é", Start: 0, KeyStart: -1, End: 4, Pathable: true}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := next.Record(3)
	pos, err := next.Text().Position(r.Start)
	if err != nil || pos.Line != 3 || pos.Column != 5 {
		t.Fatalf("position %+v %v", pos, err)
	}
	if fmt.Sprint(next.Root()) != "1" {
		t.Fatal("root ID changed")
	}
}

func TestReplaceSharesUntouchedChunksAndChildVectors(t *testing.T) {
	const children = 800
	var source strings.Builder
	source.WriteByte('[')
	inputs := make([]Input, children+1)
	ids := make([]uint32, children)
	for i := 0; i < children; i++ {
		if i > 0 {
			source.WriteByte(',')
		}
		start := source.Len()
		source.WriteByte('0')
		id := uint32(i + 2)
		ids[i] = id
		inputs[i+1] = Input{ID: id, Parent: 1, Kind: "number", Label: fmt.Sprintf("[%d]", i), Value: "0", Start: start, KeyStart: -1, End: start + 1, Pathable: true}
	}
	source.WriteByte(']')
	inputs[0] = Input{ID: 1, Children: ids, Kind: "array", Start: 0, KeyStart: -1, End: source.Len(), Pathable: true}
	tree, err := New(context.Background(), textstore.New([]byte(source.String())), inputs, 1, 2000, 8)
	if err != nil {
		t.Fatal(err)
	}
	target := uint32(300)
	r, _ := tree.Record(target)
	next, _, err := tree.Replace(context.Background(), target, []byte("12345"), []Input{{ID: 1, Kind: "number", Value: "12345", Start: 0, KeyStart: -1, End: 5, Pathable: true}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if next.chunks[2] != tree.chunks[2] || next.chunks[3] != tree.chunks[3] {
		t.Fatal("unrelated record chunks were cloned")
	}
	if next.chunks[1] == tree.chunks[1] || next.chunks[0] == tree.chunks[0] {
		t.Fatal("modified chunks were not copied")
	}
	oldRoot, _ := tree.get(1)
	newRoot, _ := next.get(1)
	if &oldRoot.children[0] != &newRoot.children[0] {
		t.Fatal("unchanged ancestor child vector was copied")
	}
	moved, _ := next.Record(700)
	old, _ := tree.Record(700)
	oldAnchor, _ := tree.Text().AnchorAt(old.Start)
	newAnchor, _ := next.Text().AnchorAt(moved.Start)
	if oldAnchor != newAnchor {
		t.Fatal("unrelated source origin piece was copied")
	}
	if moved.Start != old.Start+4 || moved.Value != old.Value {
		t.Fatalf("anchor shift failed %+v %+v", old, moved)
	}
	original, _ := tree.Record(target)
	if original.Start != r.Start || original.Value != "0" {
		t.Fatal("old version mutated")
	}
}

func TestManySubtreeEditsReleaseObsoleteChunks(t *testing.T) {
	source, inputs := sampleInputs()
	inputs[0].End = len(source)
	inputs[4].Start = 15
	inputs[4].End = 16
	tree, err := New(context.Background(), textstore.New(source), inputs, 1, 1000, 8)
	if err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 40; iteration++ {
		count := 300 + iteration%3
		var source strings.Builder
		source.WriteByte('[')
		inputs := make([]Input, count+1)
		ids := make([]uint32, count)
		for i := 0; i < count; i++ {
			if i > 0 {
				source.WriteByte(',')
			}
			start := source.Len()
			source.WriteByte('0')
			ids[i] = uint32(i + 2)
			inputs[i+1] = Input{ID: ids[i], Parent: 1, Kind: "number", Value: "obsolete-value", Start: start, KeyStart: -1, End: start + 1, Pathable: true}
		}
		source.WriteByte(']')
		inputs[0] = Input{ID: 1, Children: ids, Kind: "array", Start: 0, KeyStart: -1, End: source.Len(), Pathable: true}
		// Raise the fixture's global limit; the first expansion should allocate a
		// second chunk, then deletion must release it and clear old value strings.
		next, _, err := tree.Replace(context.Background(), 2, []byte(source.String()), inputs, 1)
		if err != nil {
			t.Fatal(err)
		}
		tree, _, err = next.Replace(context.Background(), 2, []byte("0"), []Input{{ID: 1, Kind: "number", Value: "0", Start: 0, KeyStart: -1, End: 1, Pathable: true}}, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(tree.chunks) != 1 || tree.Count() != 3 || len(tree.free) > 304 {
			t.Fatalf("retention grew: chunks=%d count=%d free=%d", len(tree.chunks), tree.Count(), len(tree.free))
		}
		for _, c := range tree.chunks {
			for _, r := range c.rows {
				if r.id == 0 && (r.value != "" || r.children != nil || r.label != "") {
					t.Fatal("deleted slot retains record data")
				}
			}
		}
	}
}

func TestGlobalReplacementWeightDepthAndInvalidAliases(t *testing.T) {
	source, inputs := sampleInputs()
	inputs[0].End = len(source)
	inputs[4].Start = 15
	inputs[4].End = 16
	tree, err := New(context.Background(), textstore.New(source), inputs, 1, 7, 2)
	if err != nil {
		t.Fatal(err)
	}
	fragment := []Input{{ID: 1, Children: []uint32{2}, Kind: "array", Start: 0, KeyStart: -1, End: 3, Weight: 1}, {ID: 2, Parent: 1, Kind: "number", Value: "9", Start: 1, KeyStart: -1, End: 2, Weight: 1}}
	if _, _, err := tree.Replace(context.Background(), 3, []byte("[9]"), fragment, 1); err == nil {
		t.Fatal("global depth/weight budget ignored")
	}
	if _, _, err := tree.Replace(context.Background(), 2, []byte("[9]"), fragment, 1); err != nil {
		t.Fatalf("equal global budget rejected: %v", err)
	}
	invalid := append([]Input(nil), inputs...)
	invalid[2].Target = 999
	if _, err := New(context.Background(), textstore.New(source), invalid, 1, 100, 8); err == nil {
		t.Fatal("missing alias target accepted")
	}
	invalid = append([]Input(nil), inputs...)
	invalid[4].Parent = 2
	if _, err := New(context.Background(), textstore.New(source), invalid, 1, 100, 8); err == nil {
		t.Fatal("wrong child parent accepted")
	}
	invalid = append([]Input(nil), inputs...)
	invalid[3].Start = 7
	invalid[3].End = 8
	invalid[3].KeyStart = 6
	if _, err := New(context.Background(), textstore.New(source), invalid, 1, 100, 8); err == nil {
		t.Fatal("overlapping siblings accepted")
	}
}

func TestFragmentRootMappingKeyWeightIsIgnored(t *testing.T) {
	source, inputs := sampleInputs()
	inputs[0].End = len(source)
	inputs[4].Start = 15
	inputs[4].End = 16
	tree, err := New(context.Background(), textstore.New(source), inputs, 1, 7, 4)
	if err != nil {
		t.Fatal(err)
	}
	// A fragment extracted from another mapping can carry its former key's raw
	// weight. Replacement preserves the destination key, not that discarded key.
	next, _, err := tree.Replace(context.Background(), 2, []byte("9"), []Input{{ID: 9, Parent: 88, Kind: "number", Label: "discarded", Value: "9", Start: 0, KeyStart: -1, End: 1, Weight: 20, Pathable: true}}, 9)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := next.Record(2)
	if r.Weight != 2 || r.Label != "a" || next.Weight() != 5 {
		t.Fatalf("key weight changed: %+v total=%d", r, next.Weight())
	}
}

func TestOwnMappingKeyDepthContributesToGlobalLimit(t *testing.T) {
	source, inputs := sampleInputs()
	inputs[0].End = len(source)
	inputs[4].Start = 15
	inputs[4].End = 16
	inputs[4].OwnDepth = 3
	if _, err := New(context.Background(), textstore.New(source), inputs, 1, 100, 3); err == nil {
		t.Fatal("complex-key depth ignored")
	}
	tree, err := New(context.Background(), textstore.New(source), inputs, 1, 100, 4)
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := tree.Replace(context.Background(), 5, []byte("9"), []Input{{ID: 1, Kind: "number", Value: "9", Start: 0, KeyStart: -1, End: 1, OwnDepth: 50}}, 1)
	if err != nil {
		t.Fatalf("discarded fragment key depth was retained: %v", err)
	}
	raw, _ := next.get(5)
	if raw.ownDepth != 3 {
		t.Fatal("destination key depth was lost")
	}
	root, _ := next.get(next.Root())
	if root.height != 4 {
		t.Fatalf("global height=%d", root.height)
	}
}

func TestEmptySpanInsertionAndRepeatedVariableLengthEdits(t *testing.T) {
	source := []byte("a,b\n,x\n")
	inputs := []Input{{ID: 1, Children: []uint32{2}, Kind: "array", Start: 0, KeyStart: -1, End: len(source), Pathable: true}, {ID: 2, Parent: 1, Children: []uint32{3, 4}, Kind: "object", Start: 4, KeyStart: -1, End: 6, Pathable: true}, {ID: 3, Parent: 2, Kind: "string", Label: "a", Start: 4, KeyStart: -1, End: 4, Pathable: true, Weight: 2}, {ID: 4, Parent: 2, Kind: "string", Label: "b", Value: "x", Start: 5, KeyStart: -1, End: 6, Pathable: true, Weight: 2}}
	tree, err := New(context.Background(), textstore.New(source), inputs, 1, 100, 8)
	if err != nil {
		t.Fatal(err)
	}
	values := []string{"long", "é😀", "", "short", "x", ""}
	for _, value := range values {
		fragment := []Input{{ID: 1, Kind: "string", Value: value, Start: 0, KeyStart: -1, End: len(value), Pathable: true}}
		next, _, err := tree.Replace(context.Background(), 3, []byte(value), fragment, 1)
		if err != nil {
			t.Fatal(err)
		}
		want := "a,b\n" + value + ",x\n"
		if string(next.Text().Bytes()) != want {
			t.Fatalf("source %q want %q", next.Text().Bytes(), want)
		}
		r, ok := next.Record(4)
		if !ok || r.Start != 5+len(value) || r.End != 6+len(value) {
			t.Fatalf("adjacent source anchors %+v %v", r, ok)
		}
		row, ok := next.Record(2)
		if !ok || row.Start != 4 || row.End != 6+len(value) {
			t.Fatalf("ancestor endpoints %+v %v", row, ok)
		}
		tree = next
	}
}
func TestStableGeometryIncludesNormalizedUTF16AndRuneColumns(t *testing.T) {
	source := []byte(`["😀a",1]`)
	inputs := []Input{{ID: 1, Children: []uint32{2, 3}, Kind: "array", Start: 0, KeyStart: -1, End: len(source), Pathable: true}, {ID: 2, Parent: 1, Kind: "string", Value: "😀a", Start: 1, KeyStart: -1, End: 8, Pathable: true}, {ID: 3, Parent: 1, Kind: "number", Value: "1", Start: 9, KeyStart: -1, End: 10, Pathable: true}}
	tree, err := New(context.Background(), textstore.New(source), inputs, 1, 100, 8)
	if err != nil {
		t.Fatal(err)
	}
	next, change, err := tree.Replace(context.Background(), 2, []byte(`"é€"`), []Input{{ID: 1, Kind: "string", Value: "é€", Start: 0, KeyStart: -1, End: 7, Pathable: true}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if change.StableGeometry {
		t.Fatal("different UTF16 length claims stable geometry")
	}
	before, _ := tree.Text().Position(9)
	after, _ := next.Text().Position(9)
	if before.UTF16 != after.UTF16+1 {
		t.Fatalf("UTF16 positions %+v %+v", before, after)
	}
	next, change, err = tree.Replace(context.Background(), 3, []byte("2"), []Input{{ID: 1, Kind: "number", Value: "2", Start: 0, KeyStart: -1, End: 1, Pathable: true}}, 1)
	if err != nil || !change.StableGeometry {
		t.Fatalf("same geometry rejected: %+v %v", change, err)
	}
}
