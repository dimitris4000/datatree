package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConversionSaveBesideLocalFile(t *testing.T) {
	for _, source := range []struct{ name, text, target, suggested string }{
		{"my data.json", `{"name":"Alice"}`, "yaml", "my data.yaml"},
		{"data.yml", "name: Alice\n", "json", "data.json"},
		{"table.csv", "name\nAlice\n", "yaml", "table.yaml"},
	} {
		t.Run(source.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, source.name)
			if err := os.WriteFile(path, []byte(source.text), 0600); err != nil {
				t.Fatal(err)
			}
			h, err := NewWithFile(path)
			if err != nil {
				t.Fatal(err)
			}
			cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
			post := func(endpoint string, payload any) *httptest.ResponseRecorder {
				t.Helper()
				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				return request(t, h, "POST", endpoint, string(body), cookie)
			}
			w := post("/api/preview", map[string]any{"revision": 1, "format": source.target})
			var preview struct {
				Text, SuggestedFilename, SaveDirectory string
				CanSaveLocal                           bool
			}
			if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			resolvedDir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || preview.SuggestedFilename != source.suggested || !preview.CanSaveLocal || preview.SaveDirectory != resolvedDir {
				t.Fatalf("preview: %d %s", w.Code, w.Body.String())
			}
			// Preview must not write any files.
			if _, err := os.Stat(filepath.Join(dir, source.suggested)); !os.IsNotExist(err) {
				t.Fatalf("preview wrote a file: %v", err)
			}
			w = post("/api/convert", map[string]any{"revision": 1, "format": source.target, "confirm": true, "saveFilename": source.suggested})
			var state struct {
				Source, Format, SavedPath string
				Revision                  int
				CanUndo                   bool
			}
			if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || state.Format != source.target || state.Revision != 2 || !state.CanUndo || state.SavedPath != filepath.Join(resolvedDir, source.suggested) {
				t.Fatalf("convert: %d %s", w.Code, w.Body.String())
			}
			written, err := os.ReadFile(state.SavedPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(written) != preview.Text || state.Source != preview.Text {
				t.Fatalf("saved content differs from preview: %q", written)
			}
			original, err := os.ReadFile(path)
			if err != nil || string(original) != source.text {
				t.Fatalf("original changed: %q %v", original, err)
			}
			w = post("/api/history", map[string]any{"revision": 2, "direction": "undo"})
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			if _, err := os.Stat(state.SavedPath); err != nil {
				t.Fatal("undo removed saved copy", err)
			}
			// Existing copies cannot be overwritten, and failed saves leave history alone.
			w = post("/api/convert", map[string]any{"revision": 3, "format": source.target, "confirm": true, "saveFilename": source.suggested})
			if w.Code == 200 || !strings.Contains(w.Body.String(), "exist") {
				t.Fatalf("existing file: %d %s", w.Code, w.Body.String())
			}
			get := request(t, h, "GET", "/api/state", "", cookie)
			if err := json.Unmarshal(get.Body.Bytes(), &state); err != nil {
				t.Fatal(err)
			}
			if state.Revision != 3 || state.Format == source.target || state.Source != source.text {
				t.Fatalf("failed save changed document: %+v", state)
			}
		})
	}
}

func TestConversionSaveGuards(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := NewWithFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	for _, fixture := range []struct {
		filename string
		revision int
		confirm  bool
	}{
		{"../escape.yaml", 1, true}, {filepath.Join(dir, "absolute.yaml"), 1, true}, {"wrong.json", 1, true}, {"", 1, true}, {"sub\\escape.yaml", 1, true}, {"stale.yaml", 0, true}, {"unconfirmed.yaml", 1, false},
	} {
		body, _ := json.Marshal(map[string]any{"revision": fixture.revision, "format": "yaml", "confirm": fixture.confirm, "saveFilename": fixture.filename})
		w := request(t, h, "POST", "/api/convert", string(body), cookie)
		if w.Code == 200 {
			t.Fatalf("accepted invalid save: %+v", fixture)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("rejected conversion wrote files: %v %v", entries, err)
	}
	// Uploads carry a name but no trusted local directory.
	upload := New()
	c := request(t, upload, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, upload, "POST", "/api/import", `{"revision":0,"format":"json","filename":"upload.json","text":"{}"}`, c)
	w := request(t, upload, "POST", "/api/preview", `{"revision":1,"format":"yaml"}`, c)
	if !strings.Contains(w.Body.String(), `"suggestedFilename":"upload.yaml"`) || !strings.Contains(w.Body.String(), `"canSaveLocal":false`) {
		t.Fatal(w.Body.String())
	}
	w = request(t, upload, "POST", "/api/convert", `{"revision":1,"format":"yaml","confirm":true,"saveFilename":"upload.yaml"}`, c)
	if w.Code == 200 {
		t.Fatal("upload wrote to server working directory")
	}
}

func TestConversionSaveRefusesSymlinksAndUnwritableDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	original := []byte(`{"name":"Alice"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	h, err := NewWithFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(dir, "link.yaml")); err != nil {
		t.Fatal(err)
	}
	name := "link.yaml"
	if _, err := h.initial.convert("yaml", 1, &name); err == nil {
		t.Fatal("save followed an existing symlink")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(original) {
		t.Fatalf("symlink target changed: %q %v", got, err)
	}
	if os.Geteuid() != 0 {
		if err := os.Chmod(dir, 0500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0700) })
		name = "new copy.yaml"
		if _, err := h.initial.convert("yaml", 1, &name); err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("unwritable directory: %v", err)
		}
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if h.initial.data.Revision != 1 || h.initial.data.CanUndo() || string(h.initial.data.Doc.Source) != string(original) {
		t.Fatal("failed save modified working state/history")
	}
}
