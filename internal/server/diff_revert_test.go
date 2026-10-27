package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiffRevertAPI(t *testing.T) {
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"revision":0,"format":"json","text":"{\"x\":1}"}`, cookie)
	request(t, h, "POST", "/api/apply", `{"revision":1,"format":"json","text":"{\"x\":2}"}`, cookie)
	w := request(t, h, "GET", "/api/diff?revision=2&baseline=original", "", cookie)
	var response struct {
		Baseline struct {
			Token string `json:"token"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response.Baseline.Token == "" {
		t.Fatal("missing baseline token")
	}
	payload := map[string]any{"revision": 2, "baseline": "original", "baselineToken": response.Baseline.Token, "changeIndex": 0}
	body, _ := json.Marshal(payload)
	w = request(t, h, "POST", "/api/diff-revert", string(body), cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":3`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/diff?revision=3&baseline=original", "", cookie)
	if !strings.Contains(w.Body.String(), `"changed":0`) {
		t.Fatal(w.Body.String())
	}
	// Stale requests and invalid indices must not make additional revisions.
	for _, alter := range []map[string]any{
		{"revision": 2, "baseline": "original", "baselineToken": response.Baseline.Token, "changeIndex": 0},
		{"revision": 3, "baseline": "original", "baselineToken": "old", "changeIndex": 0},
		{"revision": 3, "baseline": "original", "baselineToken": response.Baseline.Token, "changeIndex": 99},
		{"revision": 3, "baseline": "original", "baselineToken": response.Baseline.Token},
	} {
		body, _ := json.Marshal(alter)
		if w := request(t, h, "POST", "/api/diff-revert", string(body), cookie); w.Code == 200 {
			t.Fatal("accepted invalid revert")
		}
	}
	w = request(t, h, "POST", "/api/history", `{"revision":3,"direction":"undo"}`, cookie)
	var undoState struct{ Source string }
	json.Unmarshal(w.Body.Bytes(), &undoState)
	if w.Code != 200 || undoState.Source != `{"x":2}` {
		t.Fatal("revert not undoable", w.Code, w.Body.String())
	}
}
func TestDiffRevertRejectsReplacedBaseline(t *testing.T) {
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"revision":0,"format":"json","text":"{\"x\":3}"}`, cookie)
	w := request(t, h, "POST", "/api/diff-baseline", `{"revision":1,"format":"json","text":"{\"x\":1}","filename":"old.json"}`, cookie)
	var response struct {
		Baseline struct {
			Token string `json:"token"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &response)
	request(t, h, "POST", "/api/diff-baseline", `{"revision":1,"format":"json","text":"{\"x\":2}","filename":"new.json"}`, cookie)
	body, _ := json.Marshal(map[string]any{"revision": 1, "baseline": "file", "baselineToken": response.Baseline.Token, "changeIndex": 0})
	w = request(t, h, "POST", "/api/diff-revert", string(body), cookie)
	if w.Code != 409 {
		t.Fatal("accepted superseded comparison file", w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/state", "", cookie)
	if !strings.Contains(w.Body.String(), `"revision":1`) {
		t.Fatal("failed revert mutated working state")
	}
	// Baselines and revert actions remain session isolated.
	if w := request(t, h, "POST", "/api/diff-revert", string(body), nil); w.Code == 200 {
		t.Fatal("session isolation failed")
	}
}
