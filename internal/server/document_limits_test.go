package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"datatree/internal/document"
)

func TestConfiguredServerLimitsApplyAcrossLoadPaths(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.json")
	large := filepath.Join(dir, "large.json")
	if err := os.WriteFile(small, []byte(`{"a":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(large, []byte(`{"a":1,"b":2,"c":3}`), 0600); err != nil {
		t.Fatal(err)
	}
	limits := document.Limits{MaxBytes: 128, MaxNodes: 5}
	if _, err := NewWithFile(large, limits); err == nil {
		t.Fatal("startup ignored node limit")
	}
	h, err := NewWithFile(small, limits)
	if err != nil {
		t.Fatal(err)
	}
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	for _, endpoint := range []string{"/api/open", "/api/diff-baseline", "/api/import"} {
		payload := map[string]any{"revision": 1, "path": large}
		if endpoint == "/api/import" {
			payload = map[string]any{"revision": 1, "format": "json", "text": `{"a":1,"b":2,"c":3}`}
		}
		body, _ := json.Marshal(payload)
		w := request(t, h, "POST", endpoint, string(body), c)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "5") {
			t.Fatalf("%s ignored node limit: %d %s", endpoint, w.Code, w.Body.String())
		}
	}
	// Comparison/source request envelopes must follow the configured byte cap.
	body, _ := json.Marshal(map[string]any{"revision": 1, "format": "json", "text": `"` + strings.Repeat("x", 128) + `"`})
	if w := request(t, h, "POST", "/api/apply", string(body), c); w.Code != 400 || !strings.Contains(w.Body.String(), "128") {
		t.Fatal("source apply ignored byte limit", w.Body.String())
	}
	// Another server's settings cannot alter this server or its startup sessions.
	other := New(document.Limits{MaxBytes: 1024, MaxNodes: 20})
	oc := request(t, other, "GET", "/", "", nil).Result().Cookies()[0]
	w := request(t, other, "POST", "/api/import", `{"revision":0,"format":"json","text":"{\"a\":1,\"b\":2,\"c\":3}"}`, oc)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	second := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	w = request(t, h, "POST", "/api/import", `{"revision":1,"format":"json","text":"{\"a\":1,\"b\":2,\"c\":3}"}`, second)
	if w.Code != 400 {
		t.Fatal("independent server changed limits", w.Body.String())
	}
}
