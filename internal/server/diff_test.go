package server

import (
	"encoding/json"
	"strings"
	"testing"

	"datatree/internal/document"
)

type diffWire struct {
	document.DiffResult
	Revision int                                     `json:"revision"`
	Baseline struct{ Kind, Filename, Format string } `json:"baseline"`
}

func TestDiffAPIOriginalAndFile(t *testing.T) {
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	post := func(path string, payload map[string]any) int {
		body, _ := json.Marshal(payload)
		return request(t, h, "POST", path, string(body), cookie).Code
	}
	if code := post("/api/import", map[string]any{"revision": 0, "format": "json", "text": `{"x":1}`, "filename": "work.json"}); code != 200 {
		t.Fatal(code)
	}
	if code := post("/api/apply", map[string]any{"revision": 1, "format": "json", "text": `{"x":2,"y":true}`}); code != 200 {
		t.Fatal(code)
	}
	w := request(t, h, "GET", "/api/diff?revision=2&baseline=original", "", cookie)
	var r diffWire
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Code != 200 || r.Counts.Changed != 1 || r.Counts.Added != 1 || r.Revision != 2 || r.Baseline.Format != "json" || r.Baseline.Kind != "original" {
		t.Fatalf("%d %+v", w.Code, r)
	}
	stateBefore := request(t, h, "GET", "/api/state", "", cookie).Body.String()
	body, _ := json.Marshal(map[string]any{"revision": 2, "format": "yaml", "text": "x: 2\ny: true\n", "filename": "baseline.yaml"})
	w = request(t, h, "POST", "/api/diff-baseline", string(body), cookie)
	r = diffWire{}
	json.Unmarshal(w.Body.Bytes(), &r)
	if w.Code != 200 || r.Counts != (document.DiffCounts{}) || r.Baseline.Filename != "baseline.yaml" || r.Baseline.Kind != "file" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if got := request(t, h, "GET", "/api/state", "", cookie).Body.String(); got != stateBefore {
		t.Fatal("diff modified working state/history")
	}
	for _, bad := range []string{
		`{"revision":1,"format":"json","text":"{}","filename":"stale.json"}`,
		`{"revision":2,"format":"json","text":"invalid","filename":"bad.json"}`,
	} {
		if w := request(t, h, "POST", "/api/diff-baseline", bad, cookie); w.Code == 200 {
			t.Fatal("accepted invalid baseline")
		}
	}
	w = request(t, h, "GET", "/api/diff?revision=2&baseline=file", "", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "baseline.yaml") {
		t.Fatal("failed replacement discarded baseline", w.Body.String())
	}
	for _, q := range []string{"baseline=original", "revision=1&baseline=original", "revision=2&baseline=unknown"} {
		if w := request(t, h, "GET", "/api/diff?"+q, "", cookie); w.Code == 200 {
			t.Fatal("accepted", q)
		}
	}
	if w := request(t, h, "GET", "/api/diff?revision=2&baseline=file", "", nil); w.Code == 200 {
		t.Fatal("session leak")
	}
	// Converting does not lose the format needed to parse Original.
	if code := post("/api/convert", map[string]any{"revision": 2, "format": "yaml", "confirm": true}); code != 200 {
		t.Fatal(code)
	}
	w = request(t, h, "GET", "/api/diff?revision=3&baseline=original", "", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// A successful working import resets the comparison file; a failed one does not.
	post("/api/import", map[string]any{"revision": 3, "format": "json", "text": "invalid"})
	if w := request(t, h, "GET", "/api/diff?revision=3&baseline=file", "", cookie); w.Code != 200 {
		t.Fatal("failed import reset baseline")
	}
	post("/api/import", map[string]any{"revision": 3, "format": "json", "text": "{}"})
	if w := request(t, h, "GET", "/api/diff?revision=4&baseline=file", "", cookie); w.Code == 200 {
		t.Fatal("import retained comparison baseline")
	}
}
func TestDiffBaselineFailureIsAtomic(t *testing.T) {
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"revision":0,"format":"json","text":"{}"}`, cookie)
	request(t, h, "POST", "/api/diff-baseline", `{"revision":1,"format":"json","text":"{}","filename":"good.json"}`, cookie)
	// Parsing succeeds but comparison hits the numeric bound. Keep the good baseline.
	body, _ := json.Marshal(map[string]any{"revision": 1, "format": "json", "text": "[" + strings.Repeat("1", 4097) + "]", "filename": "expensive.json"})
	if w := request(t, h, "POST", "/api/diff-baseline", string(body), cookie); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := request(t, h, "GET", "/api/diff?revision=1&baseline=file", "", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "good.json") {
		t.Fatal(w.Code, w.Body.String())
	}
}
