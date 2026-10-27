package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompanionSchemaOnStartupAndWebOpen(t *testing.T) {
	for _, ext := range []string{"json", "yaml", "yml", "csv", "jsonl", "ndjson"} {
		for _, startup := range []bool{false, true} {
			t.Run(ext+map[bool]string{true: "/startup", false: "/web"}[startup], func(t *testing.T) {
				dir := t.TempDir()
				text := `{"name":"Alice"}`
				if ext == "yaml" || ext == "yml" {
					text = "name: Alice\n"
				}
				if ext == "csv" {
					text = "name\nAlice\n"
				}
				path := filepath.Join(dir, "my.data."+ext)
				if err := os.WriteFile(path, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "my.data.schema.json"), []byte("false"), 0600); err != nil {
					t.Fatal(err)
				}
				h := New()
				if startup {
					var err error
					h, err = NewWithFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "my.data.schema.json"), []byte("true"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
				if !startup {
					body, _ := json.Marshal(map[string]any{"revision": 0, "path": path})
					if w := request(t, h, "POST", "/api/open", string(body), c); w.Code != 200 {
						t.Fatal(w.Body.String())
					}
				}
				var state struct {
					Revision int
					Schema   *schemaMetadata
				}
				w := request(t, h, "GET", "/api/state", "", c)
				if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
					t.Fatal(err)
				}
				if state.Schema == nil || state.Schema.Filename != "my.data.schema.json" || state.Schema.Token == "" || state.Revision != 1 {
					t.Fatal(w.Body.String())
				}
				body, _ := json.Marshal(map[string]any{"revision": 1, "schemaToken": state.Schema.Token})
				w = request(t, h, "POST", "/api/validate", string(body), c)
				if w.Code != 200 || !strings.Contains(w.Body.String(), `"valid":false`) || !strings.Contains(w.Body.String(), `"complete":true`) {
					t.Fatal(w.Code, w.Body.String())
				}
				if startup {
					request(t, h, "POST", "/api/schema-clear", `{"revision":1}`, c)
					other := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
					if w := request(t, h, "GET", "/api/state", "", other); !strings.Contains(w.Body.String(), "my.data.schema.json") {
						t.Fatal("startup schema leaked clear across sessions")
					}
				}
			})
		}
	}
}

func TestCompanionSchemaRejectsUnreadableAndNonregularFiles(t *testing.T) {
	for _, mode := range []string{"directory", "oversized", "unreadable", "dangling symlink"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path, schema := filepath.Join(dir, "data.json"), filepath.Join(dir, "data.schema.json")
			if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "directory" {
				if err := os.Mkdir(schema, 0700); err != nil {
					t.Fatal(err)
				}
			} else if mode == "dangling symlink" {
				if err := os.Symlink(filepath.Join(dir, "missing.json"), schema); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(schema, []byte("true"), 0600); err != nil {
					t.Fatal(err)
				}
				if mode == "oversized" {
					if err := os.Truncate(schema, 1024*1024+1); err != nil {
						t.Fatal(err)
					}
				} else {
					if os.Geteuid() == 0 {
						t.Skip("root can read mode-000 files")
					}
					if err := os.Chmod(schema, 0); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { os.Chmod(schema, 0600) })
				}
			}
			if _, err := NewWithFile(path); err == nil || !strings.Contains(err.Error(), "data.schema.json") {
				t.Fatalf("companion %s: %v", mode, err)
			}
		})
	}
}

func TestCompanionSchemaBesideSelectedSymlink(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(original, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "selected.json")
	if err := os.Symlink(original, path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "selected.schema.json"), []byte("false"), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := NewWithFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.initial.schema == nil || h.initial.schema.metadata.Filename != "selected.schema.json" {
		t.Fatal("did not check beside selected path")
	}
}

func TestCompanionSchemaFailurePreservesOpenDocument(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.json")
	next := filepath.Join(dir, "next.yaml")
	schema := filepath.Join(dir, "next.schema.json")
	for path, text := range map[string]string{old: `{"old":true}`, filepath.Join(dir, "old.schema.json"): `true`, next: "new: true\n", schema: `{"type":42}`} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	h, err := NewWithFile(old)
	if err != nil {
		t.Fatal(err)
	}
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	before := request(t, h, "GET", "/api/state", "", c).Body.String()
	body, _ := json.Marshal(map[string]any{"revision": 1, "path": next})
	w := request(t, h, "POST", "/api/open", string(body), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "next.schema.json") {
		t.Fatal(w.Code, w.Body.String())
	}
	if after := request(t, h, "GET", "/api/state", "", c).Body.String(); after != before {
		t.Fatal("failed schema replacement changed current document", after)
	}
	if _, err := NewWithFile(next); err == nil || !strings.Contains(err.Error(), "next.schema.json") {
		t.Fatalf("startup accepted invalid companion: %v", err)
	}
	if err := os.Remove(schema); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "POST", "/api/open", string(body), c)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"schema":`) {
		t.Fatal("missing companion retained prior schema", w.Body.String())
	}
}

func TestUploadedCompanionSchemaIsAtomic(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	post := func(rev int, text string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"revision": rev, "format": "json", "filename": "data.json", "text": "{}", "schema": map[string]string{"filename": "data.schema.json", "text": text}})
		w := request(t, h, "POST", "/api/import", string(body), c)
		if text == "false" && w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if text != "false" && w.Code != 400 {
			t.Fatal("invalid schema accepted", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	if !strings.Contains(post(0, "false"), `"filename":"data.schema.json"`) {
		t.Fatal("upload lost companion")
	}
	before := request(t, h, "GET", "/api/state", "", c).Body.String()
	post(1, `{"type":42}`)
	if after := request(t, h, "GET", "/api/state", "", c).Body.String(); after != before {
		t.Fatal("invalid upload changed state")
	}
}
