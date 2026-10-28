package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndSaveAs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.json")
	copyPath := filepath.Join(dir, "copy.json")
	os.WriteFile(path, []byte("{}"), 0640)
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	post := func(endpoint string, payload any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(payload)
		return request(t, h, "POST", endpoint, string(b), c)
	}
	if w := post("/api/open", map[string]any{"revision": 0, "path": path}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := post("/api/apply", map[string]any{"revision": 1, "text": "{\"a\":1}", "format": "json"}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := post("/api/save", map[string]any{"revision": 2}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	content, _ := os.ReadFile(path)
	if string(content) != "{\"a\":1}" {
		t.Fatal(string(content))
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatal(info.Mode())
	}
	if w := post("/api/save-as", map[string]any{"revision": 2, "path": copyPath}); w.Code != 200 || !strings.Contains(w.Body.String(), `"filename":"copy.json"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	// Save keeps the original import and document revision/history.
	if got := request(t, h, "GET", "/api/source?revision=2&original=true", "", c).Body.String(); got != "{}" {
		t.Fatal(got)
	}
	if w := post("/api/save-as", map[string]any{"revision": 2, "path": path}); w.Code == 200 {
		t.Fatal("overwrote existing Save As target")
	}
	os.WriteFile(copyPath, []byte("external"), 0600)
	if w := post("/api/save", map[string]any{"revision": 2}); w.Code == 200 {
		t.Fatal("overwrote external edit")
	}
	content, _ = os.ReadFile(copyPath)
	if string(content) != "external" {
		t.Fatal(string(content))
	}
	if w := post("/api/save-as", map[string]any{"revision": 1, "path": filepath.Join(dir, "stale.json")}); w.Code == 200 {
		t.Fatal("accepted stale save")
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.json")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if w := post("/api/save-as", map[string]any{"revision": 2, "path": filepath.Join(dir, "wrong.yaml")}); w.Code == 200 {
		t.Fatal("accepted wrong extension")
	}
}

func TestSaveAsUploadsAndSymlinks(t *testing.T) {
	for _, format := range []string{"json", "jsonl", "yaml", "csv"} {
		t.Run(format, func(t *testing.T) {
			source := map[string]string{"json": "{}", "jsonl": "{}\n", "yaml": "a: 1\n", "csv": "a\nb\n"}[format]
			h := New()
			c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
			post := func(endpoint string, payload any) *httptest.ResponseRecorder {
				b, _ := json.Marshal(payload)
				return request(t, h, "POST", endpoint, string(b), c)
			}
			post("/api/import", map[string]any{"revision": 0, "format": format, "text": source})
			if w := post("/api/save", map[string]any{"revision": 1}); w.Code == 200 {
				t.Fatal("saved without destination")
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "data."+format)
			if w := post("/api/save-as", map[string]any{"revision": 1, "path": path}); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			if w := post("/api/save", map[string]any{"revision": 1}); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			data, _ := os.ReadFile(path)
			if string(data) != source {
				t.Fatal(string(data))
			}
			link := filepath.Join(dir, "link."+format)
			os.Symlink(path, link)
			if w := post("/api/save-as", map[string]any{"revision": 1, "path": link}); w.Code == 200 {
				t.Fatal("followed existing symlink")
			}
			os.Remove(path)
			os.Symlink(link, path)
			if w := post("/api/save", map[string]any{"revision": 1}); w.Code == 200 {
				t.Fatal("followed replaced destination symlink")
			}
		})
	}
}
