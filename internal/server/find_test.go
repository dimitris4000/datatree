package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"datatree/internal/document"
)

func TestFindEndpoint(t *testing.T) {
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	body := `{"format":"json","text":"{\"services\":[{\"name\":\"Alice\",\"replicas\":4},{\"name\":\"Bob\",\"replicas\":1}]}","revision":0}`
	if w := request(t, h, "POST", "/api/import", body, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, c := range []struct {
		q     string
		count int
	}{
		{"mode=search&text=alice&values=true", 1},
		{"mode=search&text=name&keys=true", 2},
		{"mode=search&text=alice&values=true&caseSensitive=true", 0},
		{"mode=query&text=" + url.QueryEscape("$.services[?(@.replicas > 3)].name"), 1},
	} {
		w := request(t, h, "GET", "/api/find?revision=1&"+c.q, "", cookie)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", c.q, w.Code, w.Body.String())
		}
		var result struct {
			document.FindResult
			Revision int
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Revision != 1 || len(result.Matches) != c.count {
			t.Fatalf("%s: %+v", c.q, result)
		}
	}
	w := request(t, h, "GET", "/api/state", "", cookie)
	if !strings.Contains(w.Body.String(), `"canUndo":false`) || !strings.Contains(w.Body.String(), `"revision":1`) {
		t.Fatal("find changed state", w.Body.String())
	}
	for _, c := range []struct {
		query  string
		cookie *http.Cookie
		code   int
	}{
		{"revision=0&mode=search&text=name&keys=true", cookie, 409},
		{"mode=search&text=name&keys=true", cookie, 400},
		{"revision=1&mode=query&text=" + url.QueryEscape("$[?(alert(1))]"), cookie, 400},
		{"revision=1&mode=unknown&text=x", cookie, 400},
		{"revision=1&mode=search&text=" + strings.Repeat("x", 4097), cookie, 400},
		{"revision=1&mode=search&text=name&keys=oops", cookie, 400},
		{"revision=1&mode=search&text=name&keys=true", nil, 400},
	} {
		w := request(t, h, "GET", "/api/find?"+c.query, "", c.cookie)
		if w.Code != c.code {
			t.Errorf("expected %d got %d: %s", c.code, w.Code, w.Body.String())
		}
	}
}
func TestFindMatchesUnloadedPages(t *testing.T) {
	// Search is independent of the tree's page size and returns a real navigable ID.
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	body, _ := json.Marshal(map[string]any{"format": "json", "text": "[" + strings.Repeat(`"other",`, 45) + `"needle"]`, "revision": 0})
	request(t, h, "POST", "/api/import", string(body), cookie)
	w := request(t, h, "GET", "/api/find?revision=1&mode=search&text=needle&values=true", "", cookie)
	var result document.FindResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 || result.Matches[0].Path != "$[45]" {
		t.Fatalf("%+v", result)
	}
	w = request(t, h, "GET", "/inspector?revision=1&node="+result.Matches[0].ID, "", cookie)
	if !strings.Contains(w.Body.String(), "needle") {
		t.Fatal(w.Body.String())
	}
}
