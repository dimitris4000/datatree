package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func request(t *testing.T, h http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if method != "GET" {
		r.Header.Set("Origin", "http://127.0.0.1:8080")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestImportEditHistoryAndIsolation(t *testing.T) {
	h := New()
	w := request(t, h, "GET", "/", "", nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	cookie := w.Result().Cookies()[0]
	w = request(t, h, "POST", "/api/import", `{"format":"json","text":"{\"x\":1}","revision":0}`, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/state", "", cookie)
	var state struct {
		Revision int
		Source   string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Revision != 1 || state.Source != "{\"x\":1}" {
		t.Fatalf("%+v", state)
	}
	w = request(t, h, "POST", "/api/edit", `{"op":"replace","node":"2","text":"2","format":"json","revision":1}`, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(t, h, "POST", "/api/history", `{"direction":"undo","revision":2}`, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(t, h, "GET", "/api/download", "", cookie)
	if !bytes.Contains(w.Body.Bytes(), []byte("1")) {
		t.Fatal("undo download")
	}
	w = request(t, h, "GET", "/api/state", "", nil)
	if bytes.Contains(w.Body.Bytes(), []byte(`\"x\"`)) {
		t.Fatal("session leak")
	}
	w = request(t, h, "POST", "/api/apply", `{"text":"{}","format":"json","revision":1}`, cookie)
	if w.Code != 409 {
		t.Fatal("stale", w.Code, w.Body.String())
	}
}
func TestOriginAndHostChecks(t *testing.T) {
	h := New()
	for _, host := range []string{"evil.test", "192.168.1.1:8080"} {
		r := httptest.NewRequest("GET", "http://"+host+"/", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(host, w.Code)
		}
	}
	for _, origin := range []string{"http://evil.test", ""} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/import", strings.NewReader(`{}`))
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(origin, w.Code)
		}
	}
}
func TestInspectorEscapesAndPagination(t *testing.T) {
	h := New()
	w := request(t, h, "GET", "/", "", nil)
	c := w.Result().Cookies()[0]
	source := `{"x":"<script>alert(1)</script>","items":[1,2,3]}`
	body, _ := json.Marshal(map[string]any{"format": "json", "text": source, "revision": 0})
	w = request(t, h, "POST", "/api/import", string(body), c)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(t, h, "GET", "/inspector?node=2&revision=1", "", c)
	if strings.Contains(w.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("unescaped source")
	}
	if !strings.Contains(w.Body.String(), "hx-post") {
		t.Fatal("missing htmx form")
	}
	w = request(t, h, "GET", "/api/tree?node=3&offset=1&limit=1", "", c)
	var page struct {
		Children []struct{ Label string }
		Total    int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Children) != 1 || page.Children[0].Label != "[1]" || page.Total != 3 {
		t.Fatal(w.Body.String())
	}
}

func TestInvalidInspectorEditKeepsDraft(t *testing.T) {
	h := New()
	w := request(t, h, "GET", "/", "", nil)
	c := w.Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"format":"json","text":"{\"x\":1}","revision":0}`, c)
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/actions/edit", strings.NewReader("op=replace&node=1&revision=1&format=json&text=%7Bbroken-draft"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://127.0.0.1:8080")
	r.AddCookie(c)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "{broken-draft") {
		t.Fatal("invalid draft was discarded")
	}
	w = request(t, h, "GET", "/api/state", "", c)
	if !strings.Contains(w.Body.String(), `"revision":1`) {
		t.Fatal("invalid edit committed")
	}
}
func TestStructuredEditingEnforcesChosenType(t *testing.T) {
	h := New()
	w := request(t, h, "GET", "/", "", nil)
	c := w.Result().Cookies()[0]
	request(t, h, "POST", "/api/import", `{"format":"json","text":"{\"x\":1}","revision":0}`, c)
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/actions/edit", strings.NewReader("op=replace&node=2&revision=1&structured=true&kind=number&value=true"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://127.0.0.1:8080")
	r.AddCookie(c)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	w = request(t, h, "GET", "/api/state", "", c)
	if !strings.Contains(w.Body.String(), `"revision":1`) {
		t.Fatal("wrong value type accepted")
	}
}

func TestPageLoadsOnlyAvailableLocalScripts(t *testing.T) {
	h := New()
	w := request(t, h, "GET", "/", "", nil)
	for _, match := range regexp.MustCompile(`src="([^"]+)"`).FindAllStringSubmatch(w.Body.String(), -1) {
		asset := request(t, h, "GET", match[1], "", nil)
		if asset.Code != 200 {
			t.Fatalf("missing embedded script %s: %d", match[1], asset.Code)
		}
	}
}

func TestJSONToYAMLConversionWorkflow(t *testing.T) {
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	source := `{"person":{"name":"Alice","roles":["admin","reader"]}}`
	want := "person:\n  name: Alice\n  roles:\n    - admin\n    - reader\n"
	body, _ := json.Marshal(map[string]any{"format": "json", "text": source, "revision": 0})
	if w := request(t, h, "POST", "/api/import", string(body), cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := request(t, h, "POST", "/api/preview", `{"format":"yaml","revision":1}`, cookie)
	var preview struct {
		Text     string
		Revision int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || preview.Text != want || preview.Revision != 1 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/download", "", cookie)
	if w.Body.String() != source {
		t.Fatal("preview changed working source")
	}
	w = request(t, h, "GET", "/inspector?node=1&revision=1", "", cookie)
	if !strings.Contains(w.Body.String(), `<pre data-fold-format="yaml" id="yaml-preview">`+want+`</pre>`) {
		t.Fatal("inspector differs from preview")
	}
	w = request(t, h, "POST", "/api/convert", `{"format":"yaml","revision":1}`, cookie)
	if w.Code == 200 {
		t.Fatal("conversion accepted without confirmation")
	}
	w = request(t, h, "POST", "/api/convert", `{"format":"yaml","confirm":true,"revision":1}`, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/download", "", cookie)
	if w.Code != 200 || w.Body.String() != preview.Text || w.Header().Get("Content-Disposition") != `attachment; filename="document.yaml"` {
		t.Fatalf("download: %d %s %s", w.Code, w.Header(), w.Body.String())
	}
	w = request(t, h, "POST", "/api/history", `{"direction":"undo","revision":2}`, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/download", "", cookie)
	if w.Body.String() != source || w.Header().Get("Content-Disposition") != `attachment; filename="document.json"` {
		t.Fatal("undo did not restore JSON download")
	}
}

func TestSourceMapRevisionAndIsolation(t *testing.T) {
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	if w := request(t, h, "GET", "/api/source-map?revision=0", "", cookie); w.Code == 200 {
		t.Fatal("empty session has a map")
	}
	body := `{"format":"json","text":"{\"nested\":{\"value\":123}}","revision":0}`
	if w := request(t, h, "POST", "/api/import", body, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := request(t, h, "GET", "/api/source-map?revision=1", "", cookie)
	var mapping struct {
		Revision int
		Ranges   []struct {
			ID, Parent        string
			Index, Start, End int
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &mapping); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || mapping.Revision != 1 || len(mapping.Ranges) != 3 {
		t.Fatal(w.Code, w.Body.String())
	}
	leaf := mapping.Ranges[2]
	if leaf.ID != "3" || leaf.Parent != "2" || leaf.Index != 0 || leaf.Start != 11 || leaf.End != 22 {
		t.Fatalf("leaf map: %+v", leaf)
	}
	if w := request(t, h, "GET", "/api/source-map?revision=0", "", cookie); w.Code != 409 {
		t.Fatal("accepted stale source map", w.Code)
	}
	if w := request(t, h, "GET", "/api/source-map?revision=1", "", nil); w.Code == 200 {
		t.Fatal("source map leaked across sessions")
	}
	if w := request(t, h, "GET", "/assets/source-editor.js", "", nil); w.Code != 200 {
		t.Fatal("missing source editor asset")
	}
}

func TestDocumentShellStatusRegionInFooter(t *testing.T) {
	w := request(t, New(), "GET", "/", "", nil)
	html := w.Body.String()
	footer := regexp.MustCompile(`(?s)<footer>.*?</footer>`).FindString(html)
	if strings.Count(html, `id="status"`) != 1 || !strings.Contains(footer, `id="status" role="status" aria-live="polite"`) {
		t.Fatal("document operations need one live status region in the footer")
	}
}

func TestDocumentShellHasOneFileOpeningEntry(t *testing.T) {
	html := request(t, New(), "GET", "/", "", nil).Body.String()
	toolbar := regexp.MustCompile(`(?s)<nav class="toolbar".*?</nav>`).FindString(html)
	if !strings.Contains(toolbar, `id="open-file"`) || strings.Contains(toolbar, `type="file"`) || strings.Contains(toolbar, `id="open-path"`) {
		t.Fatal("toolbar must have one file-opening button, with upload and path options in a dialog")
	}
	dialog := regexp.MustCompile(`(?s)<dialog id="open-document".*?</dialog>`).FindString(html)
	for _, id := range []string{`id="file"`, `id="local-file-form"`, `id="local-path"`} {
		if !strings.Contains(dialog, id) {
			t.Fatalf("open dialog missing %s", id)
		}
	}
}
