package document

import "testing"

func TestCursorAtValueEndSelectsContainer(t *testing.T) {
	d, err := Parse("json", []byte(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	value := d.Root.Children[0]
	span, _ := d.SourceRange(value.ID)
	n, ok := d.Locate(span.End, span.End)
	if !ok || n.ID != d.Root.ID {
		t.Fatalf("cursor after scalar selected %v", n)
	}
	n, ok = d.Locate(span.End-1, span.End)
	if !ok || n.ID != value.ID {
		t.Fatal("selection through scalar end must still select it")
	}
}
