package document

import (
	"strings"
	"testing"
)

func TestStructuralDiffChangesAndTargets(t *testing.T) {
	before := findDoc(t, "json", `{"changed":1,"gone":{"nested":true},"same":false,"items":["a","b"]}`)
	after := findDoc(t, "json", `{"same":false,"changed":2,"added":{"x":1},"items":["z","a","b"]}`)
	r, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts.Added != 2 || r.Counts.Removed != 1 || r.Counts.Changed != 3 || len(r.Changes) != 6 {
		t.Fatalf("%+v", r)
	}
	statuses := []string{"changed", "added", "changed", "changed", "added", "removed"}
	for i, c := range r.Changes {
		if c.Status != statuses[i] {
			t.Fatalf("change %d %+v", i, c)
		}
		if after.Nodes[c.Target] == nil {
			t.Fatalf("invalid current target %+v", c)
		}
	}
	removed := r.Changes[5]
	if removed.Target != after.Root.ID || removed.Before.Path != `$['gone']` || removed.After != nil {
		t.Fatalf("removed %+v", removed)
	}
	if !r.Marks[after.Root.ID].Contains {
		t.Fatalf("missing ancestor indicator %+v", r.Marks)
	}
	added := after.Nodes[r.Changes[1].Target]
	if !r.Marks[added.ID].Added || !r.Marks[added.Children[0].ID].Added {
		t.Fatal("added subtree not highlighted")
	}
}
func TestStructuralDiffIgnoresPresentationAndPreservesTypes(t *testing.T) {
	cases := []struct {
		baseFormat, base, workFormat, work string
		changed                            int
	}{
		{"json", `{"a":1,"b":"two","c":true,"d":null}`, "yaml", "# comment\nb: 'two'\na: 1.0\nd: ~\nc: True\n", 0},
		{"json", `{"x":9007199254740993}`, "json", `{"x":9007199254740992}`, 1},
		{"json", `[1e999999,-0,9007199254740993]`, "json", `[10e999998,0,9.007199254740993e15]`, 0},
		{"yaml", "n: 0x10\n", "yaml", "n: 16.0\n", 0},
		{"yaml", "n: .NaN\np: +.inf\nm: -.Inf\n", "yaml", "m: -.inf\np: .Inf\nn: .nan\n", 0},
		{"yaml", "v: !custom value\n", "yaml", "v: value\n", 1},
		{"yaml", "v: !a [1, 2]\n", "yaml", "v: !b [1, 2]\n", 1},
		{"json", `{"v":1}`, "json", `{"v":"1"}`, 1},
		{"json", `{"v":{}}`, "json", `{"v":[]}`, 1},
		{"yaml", "? [west, region]\n: 1\n? 1\n: number\n? '1'\n: string\n", "yaml", "? '1'\n: string\n? [west, region]\n: 1.0\n? 1\n: number\n", 0},
		{"yaml", "? {a: 1, b: 2}\n: before\n", "yaml", "? {b: 2, a: 1}\n: after\n", 1},
		{"yaml", "name: first\n---\nname: second\n", "yaml", "name: first\n---\nname: changed\n", 1},
	}
	for _, c := range cases {
		t.Run(c.base+" -> "+c.work, func(t *testing.T) {
			r, err := Diff(findDoc(t, c.baseFormat, c.base), findDoc(t, c.workFormat, c.work))
			if err != nil {
				t.Fatal(err)
			}
			if r.Counts.Changed != c.changed || r.Counts.Added != 0 || r.Counts.Removed != 0 {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestStructuralDiffAliases(t *testing.T) {
	for _, c := range []struct {
		before, after string
		changed       int
	}{
		{"a: &old {v: 1, loop: *old}\nb: *old\n", "a: &new {loop: *new, v: 1}\nb: *new\n", 0},
		{"a: &x {v: 1}\nb: *x\n", "a: &x {v: 2}\nb: *x\n", 1},
		{"a: &x 1\nb: &y 1\nc: *x\n", "a: &x 1\nb: &y 1\nc: *y\n", 1},
		{"? &key [self, *key]\n: value\n", "? &renamed [self, *renamed]\n: value\n", 0},
	} {
		r, err := Diff(findDoc(t, "yaml", c.before), findDoc(t, "yaml", c.after))
		if err != nil {
			t.Fatal(err)
		}
		if r.Counts.Changed != c.changed || r.Counts.Added != 0 || r.Counts.Removed != 0 {
			t.Fatalf("%+v", r)
		}
	}
}
func TestStructuralDiffCapAndPurity(t *testing.T) {
	before := findDoc(t, "json", "["+strings.Repeat("0,", 299)+"0]")
	after := findDoc(t, "json", "["+strings.Repeat("1,", 299)+"1]")
	r, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changes) != 200 || !r.Truncated || r.Counts.Changed != 300 || len(r.Marks) != 301 {
		t.Fatalf("counts %+v changes %d marks %d", r.Counts, len(r.Changes), len(r.Marks))
	}
	if before.Root.Children[0].Value != "0" || after.Root.Children[0].Value != "1" {
		t.Fatal("comparison mutated inputs")
	}
	_, err = Diff(findDoc(t, "json", "["+strings.Repeat("1", 4097)+"]"), after)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected numeric work/size error, got %v", err)
	}
}
func TestOriginalFormatRetained(t *testing.T) {
	s := NewSession()
	if err := s.Import("json", []byte(`{"x":1}`), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Convert("yaml", 1); err != nil {
		t.Fatal(err)
	}
	if s.OriginalFormat != "json" {
		t.Fatalf("original format %q", s.OriginalFormat)
	}
	if err := s.History("undo", 2); err != nil {
		t.Fatal(err)
	}
	if s.OriginalFormat != "json" {
		t.Fatal("history changed original format")
	}
}

func TestStructuralDiffAliasTargetInsideChangedComplexKey(t *testing.T) {
	before := findDoc(t, "yaml", "? [&word west, *word]\n: value\nref: *word\n")
	after := findDoc(t, "yaml", "? [&renamed east, *renamed]\n: value\nref: *renamed\n")
	r, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts != (DiffCounts{Added: 1, Removed: 1, Changed: 1}) {
		t.Fatalf("alias target changed structural location: %+v", r.Counts)
	}
}

func TestStructuralDiffAliasKeyTargetInsideChangedComplexKey(t *testing.T) {
	before := findDoc(t, "yaml", "? [&word west, *word]\n: value\n? *word\n: another\n")
	after := findDoc(t, "yaml", "? [&renamed east, *renamed]\n: value\n? *renamed\n: another\n")
	r, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts != (DiffCounts{Added: 2, Removed: 2}) {
		t.Fatalf("alias key target changed structural location: %+v", r.Counts)
	}
}

func TestStructuralDiffNestedKeyAliasReordering(t *testing.T) {
	before := findDoc(t, "yaml", "? {a: &word west, b: *word, c: 1}\n: value\nref: *word\n")
	after := findDoc(t, "yaml", "? {c: 1, a: &renamed west, b: *renamed}\n: value\nref: *renamed\n")
	r, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts != (DiffCounts{}) {
		t.Fatalf("reordered complex key with aliases: %+v", r.Counts)
	}
}
func TestStructuralDiffWorkBudget(t *testing.T) {
	d := findDoc(t, "json", `{"a":1}`)
	identity := newDiffIdentity(d, &diffWork{left: 1})
	if _, err := identity.hash(d.raw, false); err == nil || !strings.Contains(err.Error(), "work limit") {
		t.Fatalf("expected bounded work error, got %v", err)
	}
}
