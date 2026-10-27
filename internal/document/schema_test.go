package document

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func schemaCheck(t *testing.T, format, source, schema string) (*Document, ValidationResult) {
	t.Helper()
	d, err := Parse(format, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	s, err := CompileSchema([]byte(schema))
	if err != nil {
		t.Fatal(err)
	}
	r, err := ValidateSchema(d, s)
	if err != nil {
		t.Fatal(err)
	}
	return d, r
}

func TestSchemaConstraintsAndTargets(t *testing.T) {
	d, r := schemaCheck(t, "json", `{"mode":"bad","extra":1}`, `{"type":"object","required":["name"],"properties":{"mode":{"$ref":"#/$defs/mode"}},"additionalProperties":false,"$defs":{"mode":{"enum":["dev","prod"]}}}`)
	if r.Valid || !r.Complete || len(r.Diagnostics) != 3 {
		t.Fatalf("%+v", r)
	}
	found := map[string]bool{}
	for _, e := range r.Diagnostics {
		found[e.Keyword] = true
		if d.Nodes[e.Target] == nil {
			t.Fatalf("invalid target: %+v", e)
		}
		if e.Keyword == "required" && e.Target != d.Root.ID {
			t.Fatal(e)
		}
		if e.Keyword == "enum" && (len(e.Suggestions) != 2 || e.Suggestions[0] != `"dev"` || e.InstancePath != "/mode") {
			t.Fatal(e)
		}
		if e.Keyword == "additionalProperties" && d.Nodes[e.Target].Label != "extra" {
			t.Fatal(e)
		}
	}
	if !found["required"] || !found["enum"] || !found["additionalProperties"] {
		t.Fatal(found)
	}
	if !r.Marks[d.Root.ID].Direct {
		t.Fatal(r.Marks)
	}
}

func TestSchemaNumbersCompositionAndEscapes(t *testing.T) {
	for _, c := range []struct {
		format, source, schema string
		valid                  bool
	}{
		{"json", `9007199254740993`, `{"const":9007199254740993}`, true},
		{"json", `9007199254740992`, `{"const":9007199254740993}`, false},
		{"yaml", "+9_007_199_254_740_993", `{"const":9007199254740993}`, true},
		{"yaml", ".12345678901234567890123456789", `{"const":0.12345678901234567890123456789}`, true},
		{"json", `{"a/b~":{"":false}}`, `{"properties":{"a/b~":{"properties":{"":{"const":true}}}}}`, false},
		{"json", `5`, `{"oneOf":[{"type":"string"},{"minimum":10}]}`, false},
		{"json", `null`, `true`, true}, {"json", `{}`, `false`, false},
		{"json", `{"mode":"a","n":0}`, `{"if":{"properties":{"mode":{"const":"a"}}},"then":{"properties":{"n":{"minimum":1}}}}`, false},
	} {
		t.Run(c.source, func(t *testing.T) {
			d, r := schemaCheck(t, c.format, c.source, c.schema)
			if r.Valid != c.valid || !r.Complete {
				t.Fatalf("%+v", r)
			}
			for _, e := range r.Diagnostics {
				if d.Nodes[e.Target] == nil {
					t.Fatal(e)
				}
			}
		})
	}
}

func TestSchemaYAMLResolution(t *testing.T) {
	source := "base: &base\n  mode: bad\nuse: *base\nmerged:\n  <<: *base\noverride:\n  <<: *base\n  mode: prod\n"
	d, r := schemaCheck(t, "yaml", source, `{"additionalProperties":{"properties":{"mode":{"enum":["prod"]}}}}`)
	if r.Valid || len(r.Diagnostics) != 3 {
		t.Fatalf("%+v", r)
	}
	for _, e := range r.Diagnostics {
		n := d.Nodes[e.Target]
		switch e.InstancePath {
		case "/base/mode":
			if n.Label != "mode" {
				t.Fatal(e, n)
			}
		case "/use/mode":
			if n.Kind != "alias" || n.Label != "use" {
				t.Fatal(e, n)
			}
		case "/merged/mode":
			if n.Label != "<<" {
				t.Fatal(e, n)
			}
		default:
			t.Fatal(e)
		}
	}
	_, r = schemaCheck(t, "yaml", "mode: dev\n---\nmode: bad\n", `{"properties":{"mode":{"const":"dev"}}}`)
	if r.Valid || len(r.Diagnostics) != 1 || r.Diagnostics[0].DocumentIndex != 1 {
		t.Fatalf("%+v", r)
	}
	_, r = schemaCheck(t, "yaml", "stamp: 2026-10-27\ndata: !!binary YQ==\n", `{"additionalProperties":{"type":"string"}}`)
	if !r.Valid {
		t.Fatal(r)
	}
}

func TestSchemaUnsupportedYAMLAndBounds(t *testing.T) {
	for _, source := range []string{"x: .nan", "? [a,b]\n: c", "x: !custom foo", "x: &x [*x]", "x: 1\n1: a"} {
		_, r := schemaCheck(t, "yaml", source, `true`)
		if r.Valid || r.Complete || len(r.Diagnostics) == 0 || r.Diagnostics[0].Keyword != "projection" {
			t.Fatalf("%s: %+v", source, r)
		}
	}
	for _, schema := range []string{`{"type":"string","type":"number"}`, `{"$ref":"file:///tmp/schema.json"}`, `{"$ref":"https://example.com/schema.json"}`, `{"$schema":"https://example.com/custom"}`, `{"type":42}`, `[]`, strings.Repeat(" ", 1024*1024+1)} {
		if _, err := CompileSchema([]byte(schema)); err == nil {
			t.Fatalf("accepted %s", schema[:min(100, len(schema))])
		}
	}
	_, r := schemaCheck(t, "json", "["+strings.Repeat(`"bad",`, 210)+`"bad"]`, `{"items":{"enum":["ok"]}}`)
	if r.Valid || !r.Complete || !r.Truncated || len(r.Diagnostics) != 200 {
		t.Fatal(r)
	}
	values := make([]int, 60)
	for i := range values {
		values[i] = i
	}
	b, _ := json.Marshal(values)
	_, r = schemaCheck(t, "json", `"bad"`, `{"enum":`+string(b)+`}`)
	if len(r.Diagnostics[0].Suggestions) != 50 || !r.Diagnostics[0].SuggestionsTruncated {
		t.Fatal(r)
	}
}

func TestSchemaSuggestionsForOtherwiseInvalidValue(t *testing.T) {
	_, r := schemaCheck(t, "json", `"a"`, `{"enum":["a","long"],"minLength":3}`)
	if r.Valid || len(r.Diagnostics) != 1 || len(r.Diagnostics[0].Suggestions) != 2 {
		t.Fatal(r)
	}
}

func TestSchemaPropertyNamesAndReferenceCycle(t *testing.T) {
	d, r := schemaCheck(t, "json", `{"":1}`, `{"propertyNames":{"minLength":1}}`)
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Target == d.Root.ID {
		t.Fatalf("property name error should target empty-key field: %+v", r)
	}
	_, r = schemaCheck(t, "json", `{}`, `{"$ref":"#"}`)
	if r.Valid || r.Complete {
		t.Fatalf("evaluation cycle must be incomplete: %+v", r)
	}
}

func TestSchemaNestedMergePrecedenceAndProjectionLimits(t *testing.T) {
	_, r := schemaCheck(t, "yaml", "a: &a {mode: first}\nb: &b {mode: second, n: 2}\nc:\n  <<: [*a, *b]\n", `{"properties":{"c":{"properties":{"mode":{"const":"first"},"n":{"const":2}}}}}`)
	if !r.Valid {
		t.Fatal(r)
	}
	// An invalid custom tag on the merge container must not be silently discarded.
	_, r = schemaCheck(t, "yaml", "x:\n  <<: !custom {mode: first}\n", `true`)
	if r.Valid || r.Complete {
		t.Fatalf("ignored custom merge tag: %+v", r)
	}
	for _, source := range []string{`1e99999999`, `1e-99999999`, `0e999999999999999999999999`} {
		_, r = schemaCheck(t, "json", source, `true`)
		if r.Valid || r.Complete {
			t.Fatal(r)
		}
	}
	schema := `{"properties":{"n":{"type":"number"}}}`
	for i := 0; i < 66; i++ {
		schema = `{"properties":{"a":` + schema + `}}`
	}
	if _, err := CompileSchema([]byte(schema)); err == nil {
		t.Fatal("accepted deep schema")
	}
}

func TestSchemaZeroExponentOverflowIsRejectedBeforeEngine(t *testing.T) {
	if _, err := CompileSchema([]byte(`{"minimum":0e999999999999999999999999}`)); err == nil {
		t.Fatal("accepted oversized zero exponent in schema")
	}
	_, r := schemaCheck(t, "json", `0e999999999999999999999999`, `{"minimum":1}`)
	if r.Complete || r.Valid || len(r.Diagnostics) != 1 {
		t.Fatal(r)
	}
}

func TestSchemaRejectsMalformedYAMLScalarTags(t *testing.T) {
	for _, source := range []string{"x: !!bool definitely", "x: !!bool yes", "x: !!null nonsense"} {
		_, r := schemaCheck(t, "yaml", source, `true`)
		if r.Valid || r.Complete {
			t.Fatalf("coerced invalid tagged value %s: %+v", source, r)
		}
	}
}

func TestSchemaEvaluationAndOutputBounds(t *testing.T) {
	branches := strings.Repeat(`{"items":{"const":0}},`, 299) + `{"items":{"const":0}}`
	source := "[" + strings.Repeat("1,", 999) + "1]"
	_, r := schemaCheck(t, "json", source, `{"allOf":[`+branches+`]}`)
	if r.Complete || r.Valid || !strings.Contains(r.Diagnostics[0].Message, "work limit") {
		t.Fatalf("unbounded evaluation: %+v", r)
	}
	key := strings.Repeat("x", 800)
	encoded, _ := json.Marshal(map[string]any{key: map[string]any{key: []int{1, 2}}})
	_, r = schemaCheck(t, "json", string(encoded), `{"additionalProperties":{"additionalProperties":{"items":{"const":0}}}}`)
	if len(r.Diagnostics) != 2 || len(r.Diagnostics[0].InstancePath) > 1030 {
		t.Fatal("unbounded display path")
	}
}

func TestSchemaDependencyWorkBudget(t *testing.T) {
	names := make([]string, 1000)
	for i := range names {
		names[i] = fmt.Sprintf("b%d", i)
	}
	required, _ := json.Marshal(names)
	source := "[" + strings.Repeat(`{"a":1},`, 109) + `{"a":1}]`
	for _, keyword := range []string{"dependentRequired", "dependencies"} {
		dialect := ""
		if keyword == "dependencies" {
			dialect = `"$schema":"http://json-schema.org/draft-07/schema#",`
		}
		_, r := schemaCheck(t, "json", source, `{`+dialect+`"items":{"`+keyword+`":{"a":`+string(required)+`}}}`)
		if r.Complete || r.Valid {
			t.Fatalf("%s bypassed work budget", keyword)
		}
	}
}

func TestSchemaStreamSharesWorkBudget(t *testing.T) {
	source := strings.Repeat("0\n---\n", 109) + "0\n"
	schema := `{"allOf":[` + strings.Repeat("false,", 999) + `false]}`
	_, r := schemaCheck(t, "yaml", source, schema)
	if r.Complete || r.Valid {
		t.Fatal("stream reset evaluation budget")
	}
}
