package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartupFileLoadsSnapshotIntoIndependentSessions(t *testing.T) {
	for _, fixture := range []struct{ extension, format, source string }{
		{"json", "json", `{"name":"Alice"}`},
		{"yaml", "yaml", "name: Alice\n"},
		{"yml", "yaml", "name: Alice\n"},
		{"csv", "csv", "name\nAlice\n"},
		{"jsonl", "jsonl", "{\"name\":\"Alice\"}\n"},
		{"ndjson", "jsonl", "{\"name\":\"Alice\"}\n"},
	} {
		t.Run(fixture.extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input."+fixture.extension)
			if err := os.WriteFile(path, []byte(fixture.source), 0600); err != nil {
				t.Fatal(err)
			}
			h, err := NewWithFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// Startup is a validated snapshot, even if the file changes later.
			if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
				t.Fatal(err)
			}
			first := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
			body, _ := json.Marshal(map[string]any{"revision": 1, "format": fixture.format, "text": strings.ReplaceAll(fixture.source, "Alice", "Bob")})
			if w := request(t, h, "POST", "/api/apply", string(body), first); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			second := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
			var state struct {
				Source, Format, Filename string
				Revision                 int
				CanUndo                  bool
			}
			w := request(t, h, "GET", "/api/state", "", second)
			if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
				t.Fatal(err)
			}
			if state.Source != fixture.source || state.Format != fixture.format || state.Filename != "input."+fixture.extension || state.Revision != 1 || state.CanUndo {
				t.Fatalf("%+v", state)
			}
			if got := request(t, h, "GET", "/api/original", "", second).Body.String(); got != fixture.source {
				t.Fatalf("original: %q", got)
			}
		})
	}
}

func TestStartupFileRejectsInvalidFilesWithContext(t *testing.T) {
	dir := t.TempDir()
	for _, fixture := range []struct{ name, source, reason string }{
		{"bad.json", `{"x":`, "JSON"},
		{"bad.yaml", "name: [\n", "YAML"},
		{"bad.csv", "name,name\nx,y\n", "CSV"},
		{"data.txt", "data", "JSON"},
		{"missing.json", "", "no such file"},
		{"utf8.yaml", string([]byte{0xff}), "UTF-8"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(dir, fixture.name)
			if fixture.name != "missing.json" {
				if err := os.WriteFile(path, []byte(fixture.source), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := NewWithFile(path)
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), fixture.reason) {
				t.Fatalf("expected path and reason, got %v", err)
			}
		})
	}
	if _, err := NewWithFile(dir); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory: %v", err)
	}
}

func TestStartupFileRejectsUnreadableAndOversizedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.csv")
	if err := os.WriteFile(path, []byte("name\nAlice\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0600) })
	if os.Geteuid() != 0 {
		if _, err := NewWithFile(path); err == nil || !strings.Contains(err.Error(), "permission denied") || !strings.Contains(err.Error(), path) {
			t.Fatalf("unreadable file: %v", err)
		}
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 20*1024*1024+1); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWithFile(path); err == nil || !strings.Contains(err.Error(), "20 MiB") || !strings.Contains(err.Error(), path) {
		t.Fatalf("oversized file: %v", err)
	}
}
