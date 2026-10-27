package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileBrowserListsServerFilesAndOpensWithCompanion(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{"data.yaml": "name: Alice\n", "data.schema.json": "false", "notes.txt": "skip", ".hidden.json": "{}", "UPPER.CSV": "name\nAlice\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "nested"), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	post := func(endpoint string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return request(t, h, "POST", endpoint, string(body), c)
	}
	w := post("/api/files", map[string]any{"revision": 0, "path": dir})
	var listing struct {
		Path, Parent string
		Revision     int
		Entries      []struct {
			Name, Path string
			Directory  bool
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || listing.Path != dir || listing.Parent != filepath.Dir(dir) || listing.Revision != 0 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	names := []string{}
	for _, entry := range listing.Entries {
		names = append(names, entry.Name)
		if entry.Path != filepath.Join(dir, entry.Name) {
			t.Fatal(entry)
		}
	}
	if strings.Join(names, ",") != "linked,nested,data.schema.json,data.yaml,UPPER.CSV" {
		t.Fatalf("listing: %v", names)
	}
	if !listing.Entries[0].Directory || !listing.Entries[1].Directory {
		t.Fatal("directories not navigable")
	}
	w = post("/api/files", map[string]any{"revision": 0, "path": dir, "showHidden": true})
	if !strings.Contains(w.Body.String(), ".hidden.json") {
		t.Fatal("hidden-file toggle ignored", w.Body.String())
	}
	// Browsing never imports a document or changes its revision/history.
	state := request(t, h, "GET", "/api/state", "", c).Body.String()
	if strings.Contains(state, `"source":`) || !strings.Contains(state, `"revision":0`) {
		t.Fatal(state)
	}
	w = post("/api/open", map[string]any{"revision": 0, "path": filepath.Join(dir, "data.yaml")})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"filename":"data.schema.json"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = post("/api/files", map[string]any{"revision": 1})
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil || listing.Path != resolvedDir {
		t.Fatalf("default directory: %s %v", w.Body.String(), err)
	}
}

func TestFileBrowserRejectsInvalidRequestsAndPaths(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	for _, fixture := range []struct {
		body string
		code int
	}{
		{`{}`, 400}, {`{"revision":1}`, 409}, {`{"revision":0,"path":"/this/path/does/not/exist"}`, 400}, {`{"revision":0,"unknown":true}`, 400}, {`{"revision":0} {}`, 400},
	} {
		w := request(t, h, "POST", "/api/files", fixture.body, c)
		if w.Code != fixture.code {
			t.Fatalf("%s: %d %s", fixture.body, w.Code, w.Body.String())
		}
	}
	path := filepath.Join(t.TempDir(), "file.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"revision": 0, "path": path})
	w := request(t, h, "POST", "/api/files", string(body), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "directory") {
		t.Fatal(w.Code, w.Body.String())
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/files", strings.NewReader(`{"revision":0}`))
	r.AddCookie(c)
	r.Header.Set("Origin", "http://evil.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin browsing accepted")
	}
	w = request(t, h, "GET", "/api/files", "", c)
	if w.Code != 405 {
		t.Fatal("listing must require same-origin POST", w.Code)
	}
}

func TestFileBrowserDefaultDirectoryAndBoundedListing(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/files", `{"revision":0}`, c)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Path      string
		Truncated bool
		Entries   []json.RawMessage
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil || w.Code != 200 || listing.Path != cwd {
		t.Fatalf("default path: %d %s %v", w.Code, w.Body.String(), err)
	}
	dir := t.TempDir()
	for i := 0; i < 1002; i++ {
		name := filepath.Join(dir, fmt.Sprintf("data%04d.json", i))
		if err := os.WriteFile(name, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	body, _ := json.Marshal(map[string]any{"revision": 0, "path": dir})
	w = request(t, h, "POST", "/api/files", string(body), c)
	if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil || !listing.Truncated || len(listing.Entries) != 1000 {
		t.Fatalf("unbounded listing: %d entries, truncated %v, err %v", len(listing.Entries), listing.Truncated, err)
	}
}
