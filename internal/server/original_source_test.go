package server

import (
	"encoding/json"
	"testing"
)

func TestOriginalSourceSynchronizationMetadata(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"format":"json","text":"{\r\n\"a\":1}","revision":0}`, c)
	check := func(want bool) {
		t.Helper()
		w := request(t, h, "GET", "/api/state", "", c)
		var state struct {
			OriginalMatches bool `json:"originalMatches"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if state.OriginalMatches != want {
			t.Fatalf("original matches=%v, want %v", state.OriginalMatches, want)
		}
	}
	check(true)
	request(t, h, "POST", "/api/apply", `{"format":"json","text":"{\n\"a\":1}","revision":1}`, c)
	check(true)
	request(t, h, "POST", "/api/apply", `{"format":"json","text":"{\n\"a\":2}","revision":2}`, c)
	check(false)
}

func TestScalarPatchAdvertisesOnlyStableChangedNodes(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"format":"json","text":"{\"a\":1,\"b\":2}","revision":0}`, c)
	w := request(t, h, "POST", "/api/patch", `{"revision":1,"patches":[{"start":5,"end":6,"text":"3"}]}`, c)
	var state struct {
		StableIDs    bool     `json:"stableIDs"`
		ChangedNodes []string `json:"changedNodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if !state.StableIDs || len(state.ChangedNodes) != 2 || state.ChangedNodes[0] != "2" || state.ChangedNodes[1] != "1" {
		t.Fatalf("missing stable changes %+v", state)
	}
}
