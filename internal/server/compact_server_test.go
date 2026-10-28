package server

import (
	"context"
	"datatree/internal/document"
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactCoreHTTPReadsAndLocalPatch(t *testing.T) {
	d, err := document.ParseCompactWithLimitsContext(context.Background(), "json", []byte(`{"name":"old","other":"keep"}`), document.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	h := New()
	h.initial = &browserSession{data: &document.Session{Doc: d, Original: d.Source, OriginalFormat: d.Format, Revision: 1, Limits: document.DefaultLimits()}}
	c := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	w := request(t, h, "GET", "/api/state", "", c)
	if !strings.Contains(w.Body.String(), `"nodes":3`) {
		t.Fatal(w.Body.String())
	}
	w = request(t, h, "GET", "/api/tree?revision=1&limit=1", "", c)
	var page struct {
		Total    int
		Children []struct{ ID string }
	}
	json.Unmarshal(w.Body.Bytes(), &page)
	if page.Total != 2 || len(page.Children) != 1 || page.Children[0].ID != "2" {
		t.Fatal(w.Body.String())
	}
	w = request(t, h, "GET", "/inspector?revision=1&node=1", "", c)
	if strings.Contains(w.Body.String(), "Duplicate or remove") {
		t.Fatal("root treated as child")
	}
	w = request(t, h, "POST", "/api/patch", `{"revision":1,"patches":[{"start":9,"end":12,"text":"new longer 😀"}]}`, c)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"stableIDs":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/source?revision=2", "", c)
	if w.Body.String() != `{"name":"new longer 😀","other":"keep"}` {
		t.Fatal(w.Body.String())
	}
	w = request(t, h, "GET", "/api/find?revision=2&mode=query&text=$.other", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"3"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "POST", "/api/history", `{"revision":2,"direction":"undo"}`, c)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(t, h, "GET", "/api/source?revision=3", "", c)
	if w.Body.String() != string(d.Source) {
		t.Fatal("undo source changed")
	}
}
