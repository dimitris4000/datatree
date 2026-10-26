package document

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestSourceRanges(t *testing.T) {
	cases := []struct{ format, source, label, want string }{
		{"json", "{\r\n  \"emoji\": \"😀\", \"nested\": {\"x\": 2}\r\n}", "emoji", `"emoji": "😀"`},
		{"yaml", "name: café\r\nitems:\r\n  - value: 2\r\n", "name", "name: café"},
		{"jsonl", "{\"a\":1}\n{\"b\":2}\n", "b", `"b":2`},
		{"csv", "name,note\r\nAlice,\"one\r\ntwo\"\r\nBob,😀\r\n", "note", `note`},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			d, err := Parse(tc.format, []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			ranges := d.SourceRanges()
			if len(ranges) != len(d.Nodes) {
				t.Fatalf("ranges %d nodes %d", len(ranges), len(d.Nodes))
			}
			normalized := normalizeUnits(tc.source)
			if ranges[0].Start != 0 || ranges[0].End != len(normalized) {
				t.Fatal("root does not cover source", ranges[0])
			}
			found := false
			for _, r := range ranges {
				if d.Nodes[r.ID].Label == tc.label {
					got := stringFromUnits(normalized[r.Start:r.End])
					if tc.format == "csv" {
						if got != "\"one\ntwo\"" && got != "😀" {
							t.Fatalf("CSV field: %q", got)
						}
					} else if got != tc.want {
						t.Fatalf("got %q want %q", got, tc.want)
					}
					if r.Parent == "" {
						t.Fatal("missing parent")
					}
					found = true
				}
			}
			if !found {
				t.Fatal("field not found")
			}
		})
	}
}

func normalizeUnits(s string) []uint16 {
	return utf16.Encode([]rune(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")))
}
func stringFromUnits(s []uint16) string { return string(utf16.Decode(s)) }

func TestSourceRangeHierarchyAndCSVQuotes(t *testing.T) {
	cases := []struct{ format, source string }{
		{"json", `{"a":[{"x":"😀"},{"y":false}],"b":[]}`},
		{"yaml", "# comment\nanchor: &base\n  value: café\nalias: *base\ntext: |\n  one\n  two\n"},
		{"yaml", "---\nname: first\n---\nname: second\n"},
		{"csv", "name,note,last\r\nα,\"a,\"\"b\"\"\",\r\nβ,\"one\r\ntwo\",😀\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			d, err := Parse(tc.format, []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			ranges := d.SourceRanges()
			byID := map[string]SourceRange{}
			for _, r := range ranges {
				byID[r.ID] = r
			}
			for _, r := range ranges {
				if r.Start > r.End {
					t.Fatal("reversed range", r)
				}
				if r.Parent != "" {
					p := byID[r.Parent]
					if r.Start < p.Start || r.End > p.End {
						t.Fatal("child outside parent", r, p)
					}
					if d.Nodes[r.Parent].Children[r.Index].ID != r.ID {
						t.Fatal("wrong child index", r)
					}
				}
			}
			if tc.format == "csv" {
				units := normalizeUnits(tc.source)
				wants := [][]string{{"α", `"a,""b"""`, ""}, {"β", "\"one\ntwo\"", "😀"}}
				for i, row := range d.Root.Children {
					for j, cell := range row.Children {
						r := byID[cell.ID]
						got := stringFromUnits(units[r.Start:r.End])
						if got != wants[i][j] {
							t.Fatalf("cell %d/%d: %q", i, j, got)
						}
					}
				}
			}
		})
	}
}
