package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalSchemaAndComparisonPickers(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{"rules.json": `{"properties":{"a":{"const":1}}}`, "baseline.yml": "a: 1\n", "baseline.schema.json": `invalid`, "bad.json": "{"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
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
	post("/api/import", map[string]any{"revision": 0, "format": "json", "text": `{"a":2}`, "filename": "working.json"})
	w := post("/api/schema", map[string]any{"revision": 1, "path": filepath.Join(dir, "rules.json")})
	var attached struct{ Schema schemaMetadata }
	if err := json.Unmarshal(w.Body.Bytes(), &attached); err != nil || w.Code != 200 || attached.Schema.Filename != "rules.json" || attached.Schema.Token == "" {
		t.Fatalf("schema: %d %s %v", w.Code, w.Body.String(), err)
	}
	w = post("/api/validate", map[string]any{"revision": 1, "schemaToken": attached.Schema.Token})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"valid":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = post("/api/diff-baseline", map[string]any{"revision": 1, "path": filepath.Join(dir, "baseline.yml")})
	var comparison diffResponse
	if err := json.Unmarshal(w.Body.Bytes(), &comparison); err != nil || w.Code != 200 || comparison.Baseline.Filename != "baseline.yml" || comparison.Baseline.Format != "yaml" || len(comparison.Changes) != 1 {
		t.Fatalf("comparison: %d %s %v", w.Code, w.Body.String(), err)
	}
	before := request(t, h, "GET", "/api/state", "", c).Body.String()
	if !strings.Contains(before, `"source":"{\"a\":2}"`) || !strings.Contains(before, `"canUndo":false`) || !strings.Contains(before, attached.Schema.Token) {
		t.Fatal("picker changed working state", before)
	}
	for _, endpoint := range []string{"/api/schema", "/api/diff-baseline"} {
		for _, name := range []string{"missing.json", "bad.json"} {
			w = post(endpoint, map[string]any{"revision": 1, "path": filepath.Join(dir, name)})
			if w.Code != 400 {
				t.Fatal(endpoint, w.Code, w.Body.String())
			}
		}
		if w = post(endpoint, map[string]any{"revision": 0, "path": filepath.Join(dir, "rules.json")}); w.Code != 409 {
			t.Fatal("stale file selection accepted", w.Code, w.Body.String())
		}
		w = post(endpoint, map[string]any{"revision": 1, "path": dir})
		if w.Code != 400 || !strings.Contains(w.Body.String(), "regular file") {
			t.Fatal("directory accepted", endpoint, w.Code, w.Body.String())
		}
	}
	if after := request(t, h, "GET", "/api/state", "", c).Body.String(); after != before {
		t.Fatal("failed selection changed working document or schema")
	}
	w = request(t, h, "GET", "/api/diff?revision=1&baseline=file", "", c)
	if !strings.Contains(w.Body.String(), comparison.Baseline.Token) {
		t.Fatal("failed selection replaced comparison baseline")
	}
}

func TestPanelFilePickerLimits(t *testing.T) {
	dir := t.TempDir()
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"revision":0,"format":"json","text":"{}"}`, c)
	for _, fixture := range []struct {
		endpoint string
		limit    int
	}{{"/api/schema", 1024 * 1024}, {"/api/diff-baseline", 10 * 1024 * 1024}} {
		path := filepath.Join(dir, "oversized.json")
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(path, int64(fixture.limit)+1); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"revision": 1, "path": path})
		w := request(t, h, "POST", fixture.endpoint, string(body), c)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "exceeds") {
			t.Fatal(fixture.endpoint, w.Code, w.Body.String())
		}
	}
}
