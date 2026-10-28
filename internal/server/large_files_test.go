package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLargeFileReadContracts(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"format":"json","text":"{\r\n\"x\":\"😀\"}","revision":0}`, c)
	w := request(t, h, "GET", "/api/state", "", c)
	if strings.Contains(w.Body.String(), `"source"`) {
		t.Fatal("state contains source")
	}
	w = request(t, h, "GET", "/api/source?revision=1&start=7&end=9", "", c)
	if w.Code != 200 || w.Body.String() != "😀" || w.Header().Get("X-Document-Revision") != "1" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/node-location?revision=1&node=2", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"range"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/source?revision=0", "", c)
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
}
func TestPatchAtomicUTF16(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"format":"json","text":"{\"x\":\"😀\"}","revision":0}`, c)
	for _, patch := range []string{`[{"start":7,"end":8,"text":"x"}]`, `[{"start":6,"end":8,"text":"\""}]`, `[{"start":6,"end":8,"text":"x"},{"start":5,"end":6,"text":"y"}]`} {
		w := request(t, h, "POST", "/api/patch", `{"revision":1,"patches":`+patch+`}`, c)
		if w.Code == 200 {
			t.Fatal("accepted invalid patch", patch)
		}
	}
	w := request(t, h, "POST", "/api/patch", `{"revision":1,"patches":[{"start":6,"end":8,"text":"ok"}]}`, c)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if out["previousRevision"] != float64(1) || out["revision"] != float64(2) {
		t.Fatal(out)
	}
	w = request(t, h, "GET", "/api/source?revision=2", "", c)
	if w.Body.String() != `{"x":"ok"}` {
		t.Fatal(w.Body.String())
	}
}
func TestLazyInspectorAndRepresentations(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	body, _ := json.Marshal(map[string]any{"format": "json", "text": `{"x":"` + strings.Repeat("a", 70000) + `"}`, "revision": 0})
	request(t, h, "POST", "/api/import", string(body), c)
	w := request(t, h, "GET", "/inspector?revision=1&node=1", "", c)
	if len(w.Body.Bytes()) > 16000 || !strings.Contains(w.Body.String(), `data-representation-node="1"`) {
		t.Fatal("inspector eagerly serializes")
	}
	w = request(t, h, "GET", "/api/representation?revision=1&node=1&format=native", "", c)
	var out struct {
		Text       string
		Truncated  bool
		TotalBytes int
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || !out.Truncated || len(out.Text) > 65536 || out.TotalBytes <= 65536 {
		t.Fatal(w.Code, out.Truncated, len(out.Text), out.TotalBytes)
	}
}

func TestPatchesPreserveUntouchedCRLFAndOriginal(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"format":"json","text":"{\r\n\"x\":\"😀\"\r\n}","revision":0}`, c)
	w := request(t, h, "POST", "/api/patch", `{"revision":1,"patches":[{"start":7,"end":9,"text":"ok"}]}`, c)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got := request(t, h, "GET", "/api/download", "", c).Body.String(); got != "{\r\n\"x\":\"ok\"\r\n}" {
		t.Fatal(got)
	}
	if got := request(t, h, "GET", "/api/source?revision=2&original=true&start=7&end=9", "", c).Body.String(); got != "😀" {
		t.Fatal(got)
	}
}

func TestConcurrentSessionMutationsHaveOneWinner(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"format":"json","text":"{}","revision":0}`, c)
	start := make(chan struct{})
	results := make(chan int, 8)
	for i := 0; i < 8; i++ {
		go func() {
			<-start
			results <- request(t, h, "POST", "/api/apply", `{"format":"json","text":"{\"x\":1}","revision":1}`, c).Code
		}()
	}
	close(start)
	success := 0
	for i := 0; i < 8; i++ {
		code := <-results
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatal(code)
		}
	}
	if success != 1 {
		t.Fatal("concurrent writes overwrote each other", success)
	}
}

func TestNativeYAMLRepresentationIsStandaloneSubtree(t *testing.T) {
	h := New()
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	body, _ := json.Marshal(map[string]any{"revision": 0, "format": "yaml", "text": "outer:\n  child:\n    a: 1\n    b: 2\n"})
	request(t, h, "POST", "/api/import", string(body), c)
	w := request(t, h, "GET", "/api/representation?revision=1&node=3&format=native&full=true", "", c)
	var representation struct{ Text string }
	json.Unmarshal(w.Body.Bytes(), &representation)
	if strings.TrimSpace(representation.Text) != "a: 1\nb: 2" {
		t.Fatal("native YAML is not independently editable", representation.Text)
	}
	body, _ = json.Marshal(map[string]any{"revision": 1, "node": "3", "op": "replace", "format": "yaml", "text": representation.Text})
	w = request(t, h, "POST", "/api/edit", string(body), c)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
