package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAsyncJobOwnershipAndResult(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/import?async=true", `{"revision":0,"format":"json","text":"{\"x\":1}"}`, c)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var accepted struct{ Job string }
	json.Unmarshal(w.Body.Bytes(), &accepted)
	if accepted.Job == "" {
		t.Fatal(w.Body.String())
	}
	if w = request(t, h, "GET", "/api/jobs?id="+accepted.Job, "", nil); w.Code != 404 {
		t.Fatal("job leaked", w.Code)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		w = request(t, h, "GET", "/api/jobs?id="+accepted.Job, "", c)
		if strings.Contains(w.Body.String(), `"status":"complete"`) {
			if !strings.Contains(w.Body.String(), `"revision":1`) {
				t.Fatal(w.Body.String())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job never completed", w.Body.String())
}

func TestAsyncCancellationPreventsCommit(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	body, _ := json.Marshal(map[string]any{"revision": 0, "format": "json", "text": "[" + strings.Repeat("0,", 200000) + "0]"})
	w := request(t, h, "POST", "/api/import?async=true", string(body), c)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var accepted struct{ Job string }
	json.Unmarshal(w.Body.Bytes(), &accepted)
	payload, _ := json.Marshal(map[string]string{"id": accepted.Job})
	w = request(t, h, "POST", "/api/jobs/cancel", string(payload), c)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w = request(t, h, "GET", "/api/jobs?id="+accepted.Job, "", c)
		if strings.Contains(w.Body.String(), `"status":"cancelled"`) {
			state := request(t, h, "GET", "/api/state", "", c)
			if !strings.Contains(state.Body.String(), `"revision":0`) {
				t.Fatal("cancelled job committed", state.Body.String())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job never cancelled", w.Body.String())
}
