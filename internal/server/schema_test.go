package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Pause at the post-evaluation cancellation boundary without a production test
// hook. Concurrent requests must still progress, and stale work must be rejected.
type schemaGateContext struct {
	context.Context
	evaluated, release chan struct{}
}

func (c schemaGateContext) Err() error { close(c.evaluated); <-c.release; return nil }

func TestSchemaEvaluationRechecksSnapshotOutsideGlobalLock(t *testing.T) {
	for _, change := range []string{"document", "schema", "eviction"} {
		t.Run(change, func(t *testing.T) {
			h := New()
			cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
			request(t, h, "POST", "/api/import", `{"revision":0,"format":"json","text":"{}"}`, cookie)
			w := request(t, h, "POST", "/api/schema", `{"revision":1,"text":"true"}`, cookie)
			var metadata struct{ Schema struct{ Token string } }
			json.Unmarshal(w.Body.Bytes(), &metadata)
			body, _ := json.Marshal(map[string]any{"revision": 1, "schemaToken": metadata.Schema.Token})
			r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/validate", strings.NewReader(string(body)))
			r.AddCookie(cookie)
			r.Header.Set("Origin", "http://127.0.0.1:8080")
			gate := schemaGateContext{context.Background(), make(chan struct{}), make(chan struct{})}
			r = r.WithContext(gate)
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { defer close(done); h.ServeHTTP(response, r) }()
			select {
			case <-gate.evaluated:
			case <-time.After(3 * time.Second):
				t.Fatal("validation never reached unlocked completion")
			}
			// Inspect the lock directly so a broken implementation fails rather than hangs.
			if !h.mu.TryLock() {
				close(gate.release)
				<-done
				t.Fatal("evaluation retained global session lock")
			}
			h.mu.Unlock()
			switch change {
			case "document":
				request(t, h, "POST", "/api/apply", `{"revision":1,"format":"json","text":"{\"new\":1}"}`, cookie)
			case "schema":
				request(t, h, "POST", "/api/schema-clear", `{"revision":1}`, cookie)
			case "eviction":
				h.mu.Lock()
				delete(h.sessions, cookie.Value)
				h.mu.Unlock()
			}
			close(gate.release)
			<-done
			if response.Code != 409 {
				t.Fatalf("accepted stale %s result: %d %s", change, response.Code, response.Body.String())
			}
		})
	}
}

func TestSchemaAPI(t *testing.T) {
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"revision":0,"format":"json","text":"{\"mode\":\"bad\"}"}`, cookie)
	post := func(path string, payload any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(payload)
		return request(t, h, "POST", path, string(b), cookie)
	}
	attach := post("/api/schema", map[string]any{"revision": 1, "filename": "mode.json", "text": `{"properties":{"mode":{"enum":["prod"]}}}`})
	if attach.Code != 200 {
		t.Fatalf("%d %s", attach.Code, attach.Body.String())
	}
	var meta struct {
		Schema   struct{ Token string }
		Revision int
	}
	json.Unmarshal(attach.Body.Bytes(), &meta)
	if meta.Revision != 1 || meta.Schema.Token == "" {
		t.Fatal(attach.Body.String())
	}
	validate := func(rev int, token string) *httptest.ResponseRecorder {
		return post("/api/validate", map[string]any{"revision": rev, "schemaToken": token})
	}
	w := validate(1, meta.Schema.Token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"valid":false`) || !strings.Contains(w.Body.String(), `"suggestions":["\"prod\""]`) {
		t.Fatal(w.Body.String())
	}
	if bad := post("/api/schema", map[string]any{"revision": 1, "text": `{"type":42}`}); bad.Code != 400 {
		t.Fatal(bad.Body.String())
	}
	if w := validate(1, meta.Schema.Token); w.Code != 200 {
		t.Fatal("failed replacement lost attachment", w.Body.String())
	}
	for _, c := range []struct {
		rev   int
		token string
	}{{0, meta.Schema.Token}, {1, "stale"}} {
		if w := validate(c.rev, c.token); w.Code != 409 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	state := request(t, h, "GET", "/api/state", "", cookie).Body.String()
	if !strings.Contains(state, `"filename":"mode.json"`) || !strings.Contains(state, `"canUndo":false`) || !strings.Contains(state, `"revision":1`) {
		t.Fatal(state)
	}
	post("/api/apply", map[string]any{"revision": 1, "format": "json", "text": `{"mode":"prod"}`})
	if w := validate(2, meta.Schema.Token); w.Code != 200 || !strings.Contains(w.Body.String(), `"valid":true`) {
		t.Fatal(w.Body.String())
	}
	post("/api/history", map[string]any{"revision": 2, "direction": "undo"})
	if w := validate(3, meta.Schema.Token); w.Code != 200 || !strings.Contains(w.Body.String(), `"valid":false`) {
		t.Fatal(w.Body.String())
	}
	if w := post("/api/schema-clear", map[string]any{"revision": 3}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := validate(3, meta.Schema.Token); w.Code == 200 {
		t.Fatal("cleared schema still validates")
	}
	post("/api/schema", map[string]any{"revision": 3, "text": "true"})
	post("/api/import", map[string]any{"revision": 3, "format": "yaml", "text": "mode: prod"})
	if state := request(t, h, "GET", "/api/state", "", cookie).Body.String(); strings.Contains(state, `"schema":{`) {
		t.Fatal("import retained schema", state)
	}
}

func TestSchemaAPIGuards(t *testing.T) {
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"revision":0,"format":"json","text":"{}"}`, cookie)
	for _, c := range []struct {
		path, body string
		code       int
	}{
		{"/api/schema", `{"text":"true"}`, 400},
		{"/api/schema", `{"revision":0,"text":"true"}`, 409},
		{"/api/schema", `{"revision":1,"text":"true","unknown":1}`, 400},
		{"/api/schema", `{"revision":1,"text":"true"} {}`, 400},
		{"/api/validate", `{"revision":1}`, 400},
		{"/api/schema-clear", `{}`, 400},
	} {
		w := request(t, h, "POST", c.path, c.body, cookie)
		if w.Code != c.code {
			t.Errorf("%s: %d %s", c.path, w.Code, w.Body.String())
		}
	}
	if w := request(t, h, "POST", "/api/schema", `{"revision":1,"text":"true"}`, nil); w.Code == 200 {
		t.Fatal("session isolation")
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/schema", strings.NewReader(`{"revision":1,"text":"true"}`))
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request(t, h, "GET", "/api/schema", "", cookie); w.Code == 200 {
		t.Fatal("GET attached schema")
	}
}
