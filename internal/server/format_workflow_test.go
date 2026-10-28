package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"datatree/internal/document"
)

func TestCSVAndYAMLWorkflows(t *testing.T) {
	for _, fixture := range []struct{ format, extension, source string }{
		{"csv", "csv", "name,note\r\nAlice,\"hello, world\"\r\n"},
		{"yaml", "yaml", "# retain this comment\n- name: Alice\n  note: 'hello, world'\n"},
		{"yaml", "yml", "- name: Alice\n  note: hello\n"},
	} {
		for _, local := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/local=%t", fixture.extension, local), func(t *testing.T) {
				h := New()
				cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
				post := func(path string, payload any) *httptest.ResponseRecorder {
					t.Helper()
					body, err := json.Marshal(payload)
					if err != nil {
						t.Fatal(err)
					}
					w := request(t, h, "POST", path, string(body), cookie)
					if w.Code != 200 {
						t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
					}
					return w
				}
				get := func(path string, target any) string {
					t.Helper()
					w := request(t, h, "GET", path, "", cookie)
					if w.Code != 200 {
						t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
					}
					if target != nil {
						if err := json.Unmarshal(w.Body.Bytes(), target); err != nil {
							t.Fatal(err)
						}
					}
					return w.Body.String()
				}
				if local {
					path := filepath.Join(t.TempDir(), "data."+fixture.extension)
					if err := os.WriteFile(path, []byte(fixture.source), 0600); err != nil {
						t.Fatal(err)
					}
					post("/api/open", map[string]any{"revision": 0, "path": path})
				} else {
					post("/api/import", map[string]any{"revision": 0, "format": fixture.extension, "text": fixture.source, "filename": "data." + fixture.extension})
				}
				if got := get("/api/download", nil); got != fixture.source {
					t.Fatalf("import changed bytes: %q", got)
				}
				var found document.FindResult
				get("/api/find?revision=1&mode=query&text="+url.QueryEscape("$[0].name"), &found)
				if len(found.Matches) != 1 {
					t.Fatalf("query: %+v", found)
				}
				node := found.Matches[0].ID
				var mapping struct{ Ranges []document.SourceRange }
				get("/api/source-map?revision=1", &mapping)
				mapped := false
				for _, span := range mapping.Ranges {
					if span.ID == node && span.End > span.Start && span.Parent != "" {
						mapped = true
					}
				}
				if !mapped {
					t.Fatal("missing source selection range")
				}
				if html := get("/inspector?revision=1&node="+node, nil); !strings.Contains(html, "Alice") {
					t.Fatal("missing inspector value")
				}
				post("/api/edit", map[string]any{"revision": 1, "op": "replace", "node": node, "format": "json", "text": `"Bob"`, "structured": true})
				var matches document.FindResult
				get("/api/find?revision=2&mode=search&text=Bob&values=true", &matches)
				if len(matches.Matches) != 1 {
					t.Fatalf("search: %+v", matches)
				}
				var diff struct {
					Baseline struct{ Token string }
					document.DiffResult
				}
				get("/api/diff?revision=2&baseline=original", &diff)
				if diff.Baseline.Token == "" || len(diff.Changes) != 1 {
					t.Fatalf("expected one structural change: %+v", diff)
				}
				post("/api/diff-revert", map[string]any{"revision": 2, "baseline": "original", "baselineToken": diff.Baseline.Token, "changeIndex": 0})
				post("/api/history", map[string]any{"revision": 3, "direction": "undo"})
				post("/api/history", map[string]any{"revision": 4, "direction": "redo"})
				var attached struct{ Schema struct{ Token string } }
				w := post("/api/schema", map[string]any{"revision": 5, "text": `{"type":"array","items":{"properties":{"name":{"const":"Alice"}}}}`})
				if err := json.Unmarshal(w.Body.Bytes(), &attached); err != nil {
					t.Fatal(err)
				}
				w = post("/api/validate", map[string]any{"revision": 5, "schemaToken": attached.Schema.Token})
				var validation document.ValidationResult
				if err := json.Unmarshal(w.Body.Bytes(), &validation); err != nil {
					t.Fatal(err)
				}
				if !validation.Complete || !validation.Valid {
					t.Fatalf("validation: %+v", validation)
				}
				post("/api/preview", map[string]any{"revision": 5, "format": "json"})
				post("/api/convert", map[string]any{"revision": 5, "format": "json", "confirm": true})
				var rows []map[string]string
				if err := json.Unmarshal([]byte(get("/api/download", nil)), &rows); err != nil {
					t.Fatal(err)
				}
				if len(rows) != 1 || rows[0]["name"] != "Alice" {
					t.Fatalf("conversion: %+v", rows)
				}
				post("/api/history", map[string]any{"revision": 6, "direction": "undo"})
				var state struct{ Format, Source string }
				get("/api/state", &state)
				state.Source = get("/api/source?revision=7", nil)
				if state.Format != fixture.format {
					t.Fatal("undo did not restore format")
				}
				if fixture.extension == "yaml" && !strings.Contains(state.Source, "# retain this comment") {
					t.Fatal("YAML lost comments")
				}
				post("/api/apply", map[string]any{"revision": 7, "format": fixture.format, "text": strings.Replace(state.Source, "Alice", "Charlie", 1)})
				var row document.FindResult
				get("/api/find?revision=8&mode=query&text="+url.QueryEscape("$[0]"), &row)
				if len(row.Matches) != 1 {
					t.Fatalf("row query: %+v", row)
				}
				post("/api/edit", map[string]any{"revision": 8, "op": "clone", "node": row.Matches[0].ID})
				get("/api/find?revision=9&mode=query&text="+url.QueryEscape("$[1]"), &row)
				if len(row.Matches) != 1 {
					t.Fatalf("duplicate row query: %+v", row)
				}
				post("/api/edit", map[string]any{"revision": 9, "op": "delete", "node": row.Matches[0].ID})
				w = post("/api/validate", map[string]any{"revision": 10, "schemaToken": attached.Schema.Token})
				if err := json.Unmarshal(w.Body.Bytes(), &validation); err != nil {
					t.Fatal(err)
				}
				if validation.Valid || !validation.Complete || len(validation.Diagnostics) != 1 || validation.Diagnostics[0].InstancePath != "/0/name" {
					t.Fatalf("expected navigable error after source apply: %+v", validation)
				}
			})
		}
	}
}
