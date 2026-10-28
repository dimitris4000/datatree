package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"datatree/internal/document"
	"datatree/internal/perftest"
)

// Opt in with DATATREE_ACCEPT_FILE (private file stays local), or
// DATATREE_ACCEPT_MIB=10|50|100 and DATATREE_ACCEPT_FORMAT=json|jsonl|yaml|csv.
// This exercises real HTTP handlers in process; it is not browser timing proof.
func TestLargeFileAcceptance(t *testing.T) {
	path := os.Getenv("DATATREE_ACCEPT_FILE")
	format := os.Getenv("DATATREE_ACCEPT_FORMAT")
	if format == "" {
		format = "json"
	}
	if path == "" {
		size := os.Getenv("DATATREE_ACCEPT_MIB")
		if size == "" {
			t.Skip("opt-in large-file acceptance")
		}
		mib, err := strconv.Atoi(size)
		if err != nil || mib < 1 || mib > 100 {
			t.Fatal("DATATREE_ACCEPT_MIB must be 1..100")
		}
		path = filepath.Join(t.TempDir(), "generated."+format)
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := perftest.WriteFixture(file, format, mib*1024*1024); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	s, err := NewWithFile(path, document.Limits{MaxBytes: 100 * 1024 * 1024, MaxNodes: 4000000})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("initial index: %s, bytes=%d nodes=%d", time.Since(started), len(s.initial.data.Doc.Source), s.initial.data.Doc.NodeCount())
	var cookie *http.Cookie
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://127.0.0.1:18080"+path, strings.NewReader(body))
		if method == "POST" {
			r.Header.Set("Origin", "http://127.0.0.1:18080")
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		start := time.Now()
		s.ServeHTTP(w, r)
		if cookie == nil {
			for _, c := range w.Result().Cookies() {
				if c.Name == "datatree_session" {
					cookie = c
				}
			}
		}
		if w.Code >= 400 {
			t.Fatalf("%s %s: status %d", method, path, w.Code)
		}
		t.Logf("%s %s: %s bytes=%d", method, strings.Split(path, "?")[0], time.Since(start), w.Body.Len())
		return w
	}
	w := request("GET", "/api/state", "")
	var state map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if _, ok := state["source"]; ok {
		t.Fatal("metadata serialized entire source")
	}
	if w.Body.Len() > 8192 {
		t.Fatal("metadata is not bounded")
	}
	request("GET", "/api/source?revision=1", "")
	request("GET", "/api/tree?revision=1", "")
	root := s.initial.data.Doc.Root.ID
	if w := request("GET", "/inspector?revision=1&node="+root, ""); w.Body.Len() > 65536 {
		t.Fatal("inspector serialized whole document")
	}
	for _, kind := range []string{"native", "json", "yaml"} {
		// A lossy YAML JSON representation may correctly be rejected; generated
		// inputs have no such metadata. Native CSV/JSONL is a JSON subtree.
		w := request("GET", "/api/representation?revision=1&node="+root+"&format="+kind, "")
		var representation struct {
			Text      string
			Truncated bool
		}
		if err := json.Unmarshal(w.Body.Bytes(), &representation); err != nil {
			t.Fatal(err)
		}
		if len(representation.Text) > 65536 || !utf8.ValidString(representation.Text) {
			t.Fatal("unbounded or invalid preview")
		}
	}
	request("GET", "/api/node-location?revision=1&node="+root, "")
	request("GET", "/api/find?revision=1&mode=search&text=no-match-328414290&keys=true&values=true", "")
	var target *document.Node
	var firstString func(*document.Node)
	firstString = func(n *document.Node) {
		if target != nil {
			return
		}
		if n.Kind == "string" {
			target = n
			return
		}
		_, total := s.initial.data.Doc.ChildPage(n.ID, 0, 0)
		for offset := 0; offset < total && target == nil; offset += 50 {
			children, _ := s.initial.data.Doc.ChildPage(n.ID, offset, 50)
			for _, c := range children {
				firstString(c)
			}
		}
	}
	firstString(s.initial.data.Doc.Root)
	if target == nil {
		t.Fatal("no string scalar")
	}
	request("POST", "/api/edit", fmt.Sprintf(`{"revision":1,"op":"replace","node":%q,"format":"json","text":"\"edited\""}`, target.ID))
	request("POST", "/api/history", `{"revision":2,"direction":"undo"}`)
	for _, session := range s.sessions {
		if string(session.data.Doc.Source) != string(s.initial.data.Doc.Source) {
			t.Fatal("undo changed original bytes")
		}
	}
}
