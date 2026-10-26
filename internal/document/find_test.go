package document

import (
	"reflect"
	"strings"
	"testing"
)

func findDoc(t *testing.T, format, source string) *Document {
	t.Helper()
	d, err := Parse(format, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestFindSearch(t *testing.T) {
	d := findDoc(t, "json", `{"Name":"ALICE","nested":{"name":"Alice","active":true,"count":42,"empty":null},"names":["alice"]}`)
	cases := []struct {
		name  string
		opt   FindOptions
		paths []string
	}{
		{"keys", FindOptions{Mode: "search", Text: "name", Keys: true}, []string{`$['Name']`, `$['nested']['name']`, `$['names']`}},
		{"values", FindOptions{Mode: "search", Text: "alice", Values: true}, []string{`$['Name']`, `$['nested']['name']`, `$['names'][0]`}},
		{"case", FindOptions{Mode: "search", Text: "Alice", Values: true, CaseSensitive: true}, []string{`$['nested']['name']`}},
		{"boolean", FindOptions{Mode: "search", Text: "true", Values: true}, []string{`$['nested']['active']`}},
		{"null", FindOptions{Mode: "search", Text: "null", Values: true}, []string{`$['nested']['empty']`}},
		{"empty", FindOptions{Mode: "search", Text: "", Keys: true, Values: true}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := d.Find(c.opt)
			if err != nil {
				t.Fatal(err)
			}
			paths := []string{}
			for _, m := range r.Matches {
				paths = append(paths, m.Path)
			}
			if !reflect.DeepEqual(paths, c.paths) {
				t.Fatalf("%v want %v", paths, c.paths)
			}
		})
	}
	d = findDoc(t, "json", `{"Ålice":"åLICE","same":"same"}`)
	r, err := d.Find(FindOptions{Mode: "search", Text: "ÅLICE", Keys: true, Values: true})
	if err != nil || len(r.Matches) != 1 {
		t.Fatalf("unicode/dedup: %+v %v", r, err)
	}
	r, err = d.Find(FindOptions{Mode: "search", Text: "same", Keys: true, Values: true})
	if err != nil || len(r.Matches) != 1 {
		t.Fatalf("dedup: %+v %v", r, err)
	}
}
func TestFindQueries(t *testing.T) {
	d := findDoc(t, "json", `{"services":[{"name":"api","replicas":4},{"name":"worker","replicas":2},{"name":"db","replicas":9007199254740993}],"a.b":{"":"empty","quote'key":"yes","😀":"unicode"},"name":"root"}`)
	cases := []struct {
		q      string
		values []string
	}{
		{`$.services[0].name`, []string{"api"}},
		{`$['a.b']['']`, []string{"empty"}},
		{`$['a.b']['quote\'key']`, []string{"yes"}},
		{`$["a.b"]["\ud83d\ude00"]`, []string{"unicode"}},
		{`$.services[*].name`, []string{"api", "worker", "db"}},
		{`$..name`, []string{"api", "worker", "db", "root"}},
		{`$..['name']`, []string{"api", "worker", "db", "root"}},
		{`$.services[?(@.replicas > 3)].name`, []string{"api", "db"}},
		{`$.services[?(@['replicas'] == 9007199254740993)].name`, []string{"db"}},
		{`$.services[?(@.replicas == 9.007199254740993e15)].name`, []string{"db"}},
		{`$.services[?(@.replicas > 9007199254740992)].name`, []string{"db"}},
		{`$.services[?(@.name >= 'db')].name`, []string{"worker", "db"}},
		{`$.services[?(@.missing != null)].name`, []string{}},
		{`$.missing`, []string{}},
	}
	for _, c := range cases {
		t.Run(c.q, func(t *testing.T) {
			r, err := d.Find(FindOptions{Mode: "query", Text: c.q})
			if err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, m := range r.Matches {
				got = append(got, d.Nodes[m.ID].Value)
			}
			if !reflect.DeepEqual(got, c.values) {
				t.Fatalf("%v want %v", got, c.values)
			}
		})
	}
	r, err := d.Find(FindOptions{Mode: "query", Text: "$"})
	if err != nil || len(r.Matches) != 1 || r.Matches[0].ID != d.Root.ID {
		t.Fatalf("root %+v %v", r, err)
	}
}
func TestQueryTypedFilters(t *testing.T) {
	d := findDoc(t, "json", `[{"v":true},{"v":null},{"v":"3"},{"v":3},{"v":false},{"v":-1e999999},{"v":1e999999},{"v":1e999998}]`)
	for _, c := range []struct {
		q     string
		count int
	}{
		{`$[?(@.v == true)]`, 1}, {`$[?(@.v == null)]`, 1}, {`$[?(@.v == 3)]`, 1},
		{`$[?(@.v == '3')]`, 1}, {`$[?(@.v < 0)]`, 1}, {`$[?(@.v > 9e999998)]`, 1},
		{`$[?(@.v <= false)]`, 0}, {`$[?(@.v != 3)]`, 7},
	} {
		r, err := d.Find(FindOptions{Mode: "query", Text: c.q})
		if err != nil || len(r.Matches) != c.count {
			t.Fatalf("%s: %+v %v", c.q, r, err)
		}
	}
}
func TestFindRejectsUnsupportedQueries(t *testing.T) {
	d := findDoc(t, "json", `{"x":1}`)
	for _, q := range []string{"", "x", "$.", "$[-1]", "$[0:2]", "$['x','y']", "$..*", "$[?(@.x)]", "$[?(@.x == undefined)]", "$[?(@.x == 1 || true)]", "$[?(@.x == 01)]", "$[?(@.x = 1)]", "$[?(@.x == 1)].", "$[?(alert(1))]", "$[999999999999999999999999]", "$['unterminated]", "$['bad\\q']", "$" + strings.Repeat(".x", 2100)} {
		if _, err := d.Find(FindOptions{Mode: "query", Text: q}); err == nil {
			t.Errorf("accepted %q", q)
		}
	}
	if _, err := d.Find(FindOptions{Mode: "unknown", Text: "x"}); err == nil {
		t.Fatal("unknown mode")
	}
}
func TestFindYAMLAndLimits(t *testing.T) {
	d := findDoc(t, "yaml", "base: &b\n  name: Alice\n  loop: *b\ncopy: *b\n? [complex, key]\n: hidden\n")
	r, err := d.Find(FindOptions{Mode: "search", Text: "hidden", Values: true})
	if err != nil || len(r.Matches) != 1 || r.Matches[0].Path != "" || r.Matches[0].Location == "" {
		t.Fatalf("complex key %+v %v", r, err)
	}
	r, err = d.Find(FindOptions{Mode: "query", Text: "$..name"})
	if err != nil || len(r.Matches) != 1 {
		t.Fatalf("alias recursion %+v %v", r, err)
	}
	r, err = d.Find(FindOptions{Mode: "search", Text: "*b", Values: true})
	if err != nil || len(r.Matches) != 2 {
		t.Fatalf("alias search %+v %v", r, err)
	}
	d = findDoc(t, "yaml", "name: first\n---\nname: second\n")
	r, err = d.Find(FindOptions{Mode: "search", Text: "name", Keys: true})
	if err != nil || len(r.Matches) != 2 || r.Matches[0].Path != "" {
		t.Fatalf("stream %+v %v", r, err)
	}
	if _, err = d.Find(FindOptions{Mode: "query", Text: "$..name"}); err == nil {
		t.Fatal("stream query should explain unsupported shape")
	}
	d = findDoc(t, "json", "["+strings.Repeat(`"match",`, 200)+`"match"]`)
	r, err = d.Find(FindOptions{Mode: "search", Text: "match", Values: true})
	if err != nil || len(r.Matches) != 200 || !r.Truncated {
		t.Fatalf("limit %+v %v", r, err)
	}
	r, err = d.Find(FindOptions{Mode: "query", Text: "$[*]"})
	if err != nil || len(r.Matches) != 200 || !r.Truncated {
		t.Fatalf("query limit %+v %v", r, err)
	}
	d = findDoc(t, "json", `{"value":"`+strings.Repeat("😀", 300)+`"}`)
	r, err = d.Find(FindOptions{Mode: "search", Text: "😀", Values: true})
	if err != nil || len([]rune(r.Matches[0].Preview)) > 161 {
		t.Fatalf("preview bound %+v %v", r, err)
	}
}

func TestQueryYAMLNumbersAndScalarFilters(t *testing.T) {
	d := findDoc(t, "yaml", "- 0x10\n- 1_000\n- .5\n- 1.\n- +2.5\n- 0o20\n- 020\n- -.inf\n")
	for _, c := range []struct {
		q     string
		count int
	}{
		{`$[?(@ == 16)]`, 3}, {`$[?(@ >= 1)]`, 6}, {`$[?(@ == 0.5)]`, 1}, {`$[?(@ == 2.5)]`, 1},
	} {
		r, err := d.Find(FindOptions{Mode: "query", Text: c.q})
		if err != nil || len(r.Matches) != c.count {
			t.Fatalf("%s: %+v %v", c.q, r, err)
		}
	}
}
func TestQueryWorkBound(t *testing.T) {
	// Each recursive stage must traverse many siblings as well as the deep chain.
	text := `{"end":true}`
	fields := strings.Builder{}
	for i := 0; i < 80; i++ {
		fields.WriteString(`,"leaf`)
		fields.WriteString(strings.Repeat("x", i+1))
		fields.WriteString(`":0`)
	}
	for i := 0; i < 200; i++ {
		text = `{"a":` + text + fields.String() + `}`
	}
	d := findDoc(t, "json", text)
	if _, err := d.Find(FindOptions{Mode: "query", Text: "$" + strings.Repeat("..a", 200)}); err == nil || !strings.Contains(err.Error(), "work limit") {
		t.Fatalf("expected work limit, got %v", err)
	}
}

func TestFindComplexMappingKeys(t *testing.T) {
	d := findDoc(t, "yaml", "? [region, west]\n: hidden\n")
	r, err := d.Find(FindOptions{Mode: "search", Text: "west", Keys: true})
	if err != nil || len(r.Matches) != 1 || r.Matches[0].Path != "" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = d.Find(FindOptions{Mode: "search", Text: "region", Keys: true})
	if err != nil || len(r.Matches) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestQueryYAMLLeadingZeroDecimals(t *testing.T) {
	d := findDoc(t, "yaml", "- 01.0\n- +01.00\n- 0b10000\n")
	r, err := d.Find(FindOptions{Mode: "query", Text: `$[?(@ == 1)]`})
	if err != nil || len(r.Matches) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestQueryRecursiveDedupAndExactResultLimit(t *testing.T) {
	d := findDoc(t, "json", `{"name":{"name":{"name":1}},"name2":2}`)
	r, err := d.Find(FindOptions{Mode: "query", Text: `$..name..name`})
	if err != nil || len(r.Matches) != 2 || r.Matches[0].Path != `$['name']['name']` {
		t.Fatalf("%+v %v", r, err)
	}
	d = findDoc(t, "json", "["+strings.Repeat(`"match",`, 199)+`"match"]`)
	r, err = d.Find(FindOptions{Mode: "query", Text: `$[*]`})
	if err != nil || len(r.Matches) != 200 || r.Truncated {
		t.Fatalf("%+v %v", r, err)
	}
}
