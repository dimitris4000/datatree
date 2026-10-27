package document

import (
	"strings"
	"testing"
)

func TestRevertDiffInverse(t *testing.T) {
	for _, c := range []struct{ name, base, work, status, want string }{
		{"delete addition", `{"a":1}`, `{"a":1,"new":{"x":true}}`, "added", `{"a":1}`},
		{"restore removal", `{"a":1,"gone":{"x":true},"b":2}`, `{"a":1,"b":2}`, "removed", `{"a":1,"gone":{"x":true},"b":2}`},
		{"replace scalar", `{"a":1,"other":2}`, `{"a":"changed","other":3}`, "changed", `{"a":1,"other":3}`},
		{"root type", `[1,2]`, `{"x":true}`, "changed", `[1,2]`},
		{"array tail", `[1,2,3]`, `[1,2]`, "removed", `[1,2,3]`},
		{"array addition", `[1]`, `[1,2]`, "added", `[1]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			base := findDoc(t, "json", c.base)
			work := findDoc(t, "json", c.work)
			r, err := Diff(base, work)
			if err != nil {
				t.Fatal(err)
			}
			index := -1
			for i, x := range r.Changes {
				if x.Status == c.status {
					index = i
					break
				}
			}
			next, err := work.RevertDiff(base, index)
			if err != nil {
				t.Fatal(err)
			}
			want := findDoc(t, "json", c.want)
			diff, err := Diff(want, next)
			if err != nil || diff.Counts != (DiffCounts{}) {
				t.Fatalf("%s: %+v %v", next.Source, diff.Counts, err)
			}
			if c.name == "restore removal" && next.Root.Children[1].Label != "gone" {
				t.Fatalf("restore lost baseline position: %s", next.Source)
			}
		})
	}
}
func TestRevertDiffSessionAtomicHistory(t *testing.T) {
	s := NewSession()
	s.Import("json", []byte(`{"a":1}`), 0)
	s.Apply("json", []byte(`{"a":2}`), 1)
	base := findDoc(t, "json", `{"a":1}`)
	if err := s.RevertDiff(base, 0, 1); err == nil {
		t.Fatal("accepted stale revision")
	}
	if err := s.RevertDiff(base, 99, 2); err == nil || s.Revision != 2 {
		t.Fatal("invalid change mutated session")
	}
	if err := s.RevertDiff(base, 0, 2); err != nil {
		t.Fatal(err)
	}
	if s.Revision != 3 {
		t.Fatal(s.Revision)
	}
	s.History("undo", 3)
	if s.Doc.Root.Children[0].Value != "2" {
		t.Fatal("undo missing")
	}
	s.History("redo", 4)
	if s.Doc.Root.Children[0].Value != "1" {
		t.Fatal("redo missing")
	}
}
func TestRevertDiffYAMLPreservationAndKeys(t *testing.T) {
	base := findDoc(t, "yaml", "header: keep\nobject:\n  ? [west, region]\n  : baseline\n  '': empty\nfooter: unchanged\n")
	work := findDoc(t, "yaml", "# outside\r\nheader: keep\r\nobject: {}\r\nfooter: unchanged # retain\r\n")
	next, err := work.RevertDiff(base, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(next.Source), "# outside\r\nheader: keep\r\n") || !strings.HasSuffix(string(next.Source), "footer: unchanged # retain\r\n") {
		t.Fatalf("unrelated bytes changed: %q", next.Source)
	}
	diff, err := Diff(base, next)
	if err != nil || diff.Counts.Removed != 1 {
		t.Fatalf("%+v %v", diff.Counts, err)
	}
	next, err = next.RevertDiff(base, 0)
	if err != nil {
		t.Fatal(err)
	}
	diff, err = Diff(base, next)
	if err != nil || diff.Counts != (DiffCounts{}) {
		t.Fatalf("%+v %v", diff.Counts, err)
	}
}
func TestRevertDiffYAMLReferences(t *testing.T) {
	for _, c := range []struct {
		name, base, work string
		index            int
	}{
		{"external alias renamed", "a: &old {v: 1}\nb: *old\nc: 2\n", "a: &new {v: 1}\nb: changed\nc: 3\n", 0},
		{"preserve target anchor", "a: &old {v: 1}\nb: *old\n", "a: &new [1]\nb: *new\n", 0},
		{"cyclic restore", "a: &old {v: 1, self: *old}\nb: true\n", "b: true\n", 0},
		{"stream removal", "a: 1\n---\na: 2\n", "a: 1\n", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			base := findDoc(t, "yaml", c.base)
			work := findDoc(t, "yaml", c.work)
			next, err := work.RevertDiff(base, c.index)
			if err != nil {
				t.Fatal(err)
			}
			if c.name != "external alias renamed" {
				r, err := Diff(base, next)
				if err != nil || r.Counts != (DiffCounts{}) {
					t.Fatalf("%s: %+v %v", next.Source, r.Counts, err)
				}
			} else {
				if next.Root.Children[1].Kind != "alias" || next.Root.Children[2].Value != "3" {
					t.Fatalf("%s", next.Source)
				}
			}
		})
	}
}
func TestRevertDiffRejectsUnsafeInverse(t *testing.T) {
	for _, c := range []struct {
		format, base, work string
		index              int
	}{
		{"yaml", "{}\n", "a: &x 1\nb: *x\n", 0},
		{"json", "[1,2,3,4]", "[1,2]", 1},
	} {
		base := findDoc(t, c.format, c.base)
		work := findDoc(t, c.format, c.work)
		before := string(work.Source)
		if _, err := work.RevertDiff(base, c.index); err == nil {
			t.Fatalf("accepted unsafe revert: %s", c.work)
		}
		if string(work.Source) != before {
			t.Fatal("error mutated source")
		}
	}
	// A YAML-only key cannot be silently coerced into JSON by reverting a removal.
	base := findDoc(t, "yaml", "? [complex, key]\n: value\n")
	work := findDoc(t, "json", `{}`)
	if _, err := work.RevertDiff(base, 0); err == nil {
		t.Fatal("accepted unrepresentable JSON key")
	}
}

func TestRevertDiffFullValueAndFormats(t *testing.T) {
	baseline := `{"value":"` + strings.Repeat("whole", 200) + `","other":1}`
	base := findDoc(t, "json", baseline)
	work := findDoc(t, "json", `{"value":"changed","other":2}`)
	next, err := work.RevertDiff(base, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next.Root.Children[0].Value != strings.Repeat("whole", 200) || next.Root.Children[1].Value != "2" {
		t.Fatal("revert used truncated preview or changed sibling")
	}
	for _, c := range []struct{ format, base, work string }{
		{"jsonl", "{\"x\":1}\n{\"x\":2}\n", "{\"x\":9}\n{\"x\":2}\n"},
		{"csv", "name,value\na,one\nb,two\n", "name,value\na,changed\nb,two\n"},
	} {
		base := findDoc(t, c.format, c.base)
		work := findDoc(t, c.format, c.work)
		next, err := work.RevertDiff(base, 0)
		if err != nil {
			t.Fatal(err)
		}
		r, err := Diff(base, next)
		if err != nil || r.Counts != (DiffCounts{}) {
			t.Fatalf("%s: %+v %v", next.Source, r.Counts, err)
		}
	}
}
func TestRevertDiffAnchorCollisionAndDescendantReferences(t *testing.T) {
	for _, c := range []struct{ base, work string }{
		{"added: &x {self: *x}\nother: 1\n", "other: &x 1\n"},
		{"a: !old {v: &old 1}\nb: *old\n", "a: !new {v: &new 1}\nb: *new\n"},
		{"a: &a 1\nb: &b 2\nref: *b\n", "a: &renamedA 1\nb: &renamedB 2\nref: *renamedA\n"},
	} {
		base := findDoc(t, "yaml", c.base)
		work := findDoc(t, "yaml", c.work)
		next, err := work.RevertDiff(base, 0)
		if err != nil {
			t.Fatal(err)
		}
		r, err := Diff(base, next)
		if err != nil || r.Counts != (DiffCounts{}) {
			t.Fatalf("%s: %+v %v", next.Source, r.Counts, err)
		}
	}
}
