package document

import (
	"strings"
	"testing"
)

func TestCSVScalarEditPreservesOtherFieldsAndCRLF(t *testing.T) {
	source := "name,description\r\n\"first\",\"line one\r\nline two\"\r\nsecond,unchanged\r\n"
	d, err := Parse("csv", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	target := d.Root.Children[0].Children[0]
	next, err := d.Edit(Action{Op: "replace", Node: target.ID, Format: "json", Text: `"new,name"`})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(source, `"first"`, `"new,name"`, 1)
	if string(next.Source) != want {
		t.Fatalf("changed bytes outside CSV cell: %q", next.Source)
	}
	if string(d.Source) != source {
		t.Fatal("mutated previous source")
	}
	if next.Root.Children[0].Children[1].Value != "line one\nline two" {
		t.Fatal("multiline field semantic changed")
	}
}

func TestCSVScalarEditRejectsNonStringWithoutChangingSource(t *testing.T) {
	d, err := Parse("csv", []byte("name\r\nold\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Edit(Action{Op: "replace", Node: d.Root.Children[0].Children[0].ID, Format: "json", Text: "1"}); err == nil {
		t.Fatal("accepted numeric CSV cell")
	}
}
