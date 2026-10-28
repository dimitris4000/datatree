package document

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestJSONStrictGrammarAndTokenColumns(t *testing.T) {
	invalid := []string{"", " ", "01", "-", "+1", "1.", "1e", "1e+", "NaN", "Infinity", "[1,]", "{\"x\":1,}", "[true false]", "{\"x\" 1}", "[}", "{]", `"\x00"`, "\"raw\x01\"", "falseX", "null null", `{"a":1,"\u0061":2}`}
	for _, source := range invalid {
		t.Run(fmt.Sprintf("%q", source), func(t *testing.T) {
			if _, err := Parse("json", []byte(source)); err == nil {
				t.Fatal("invalid JSON accepted")
			}
		})
	}
	source := " {\r\n \"é😀\": [1e+1000, \"a\\\"😀\", true, null],\r\n \"last\": 0} "
	d, err := Parse("json", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	child := d.Root.Children[0].Children[1]
	before := source[:child.Start]
	line := strings.Count(before, "\n") + 1
	column := len([]rune(before[strings.LastIndexByte(before, '\n')+1:])) + 1
	if child.Location != fmt.Sprintf("line %d, column %d", line, column) {
		t.Fatalf("location=%s", child.Location)
	}
	if string(d.Source[child.Start:child.End]) != `"a\"😀"` {
		t.Fatalf("span=%q", d.Source[child.Start:child.End])
	}
}

func FuzzJSONMatchesStandardGrammar(f *testing.F) {
	for _, source := range []string{`{"x":[1,-0.2e+5,true,false,null,"é😀"]}`, `"\ud800"`, `{"a":1,"a":2}`, `[1,]`, "\"a\n\"", "null"} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 64*1024 {
			t.Skip()
		}
		d, err := Parse("json", []byte(source))
		valid := json.Valid([]byte(source))
		if err == nil && !valid {
			t.Fatalf("accepted invalid JSON %q", source)
		}
		// Duplicate keys and resource/depth limits are intentionally stricter than encoding/json.
		if valid && err != nil && !strings.Contains(err.Error(), "duplicate") && !strings.Contains(err.Error(), "depth") && !strings.Contains(err.Error(), "UTF-8") {
			t.Fatalf("valid JSON rejected: %q: %v", source, err)
		}
		if err == nil {
			encoded, err := d.JSON(d.Root.ID, false)
			if err != nil || !json.Valid([]byte(encoded)) {
				t.Fatalf("invalid re-encoding: %v", err)
			}
		}
	})
}
