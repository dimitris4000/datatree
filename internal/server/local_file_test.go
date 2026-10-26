package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenLocalFileGitContext(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "test-branch")
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(`{"value":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "data.json")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	hash := git("rev-parse", "HEAD")
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	open := func(path string, rev int) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"path": path, "revision": rev})
		w := request(t, h, "POST", "/api/open", string(body), cookie)
		if w.Code != 200 {
			t.Fatalf("open: %d %s", w.Code, w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := open(path, 0)
	if result["filename"] != "data.json" || result["source"] != `{"value":1}` {
		t.Fatal(result)
	}
	info, ok := result["git"].(map[string]any)
	if !ok || info["branch"] != "test-branch" || info["commit"] != hash {
		t.Fatal(result)
	}
	// Editing and undo retain the opened file's checkout context.
	w := request(t, h, "POST", "/api/apply", `{"format":"json","text":"{\"value\":2}","revision":1}`, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), hash) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "POST", "/api/history", `{"direction":"undo","revision":2}`, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), hash) {
		t.Fatal(w.Code, w.Body.String())
	}
	// Invalid file content must not replace the current document or context.
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	badBody, _ := json.Marshal(map[string]any{"path": badPath, "revision": 3})
	if w = request(t, h, "POST", "/api/open", string(badBody), cookie); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/state", "", cookie)
	if !strings.Contains(w.Body.String(), hash) || !strings.Contains(w.Body.String(), `"filename":"data.json"`) {
		t.Fatal(w.Body.String())
	}
	git("checkout", "--detach")
	w = request(t, h, "GET", "/api/state", "", cookie)
	if !strings.Contains(w.Body.String(), `"branch":""`) || !strings.Contains(w.Body.String(), hash) {
		t.Fatal(w.Body.String())
	}
	// A different session must not inherit the opened file's context.
	w = request(t, h, "GET", "/api/state", "", nil)
	if strings.Contains(w.Body.String(), hash) {
		t.Fatal("Git context leaked")
	}
	untracked := filepath.Join(dir, "untracked.yaml")
	os.WriteFile(untracked, []byte("value: 2\n"), 0600)
	if result = open(untracked, 3); result["git"] != nil {
		t.Fatal(result)
	}
	// Rejected opens keep the previous document and its provenance.
	body, _ := json.Marshal(map[string]any{"path": path, "revision": 1})
	if w = request(t, h, "POST", "/api/open", string(body), cookie); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "POST", "/api/import", `{"text":"{}","format":"json","revision":4}`, cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"git"`) || strings.Contains(w.Body.String(), "untracked.yaml") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestOpenLocalFileRejectsInvalidInput(t *testing.T) {
	h := New()
	cookie := request(t, h, "GET", "/", "", nil).Result().Cookies()[0]
	dir := t.TempDir()
	for _, name := range []string{"missing.json", "bad.json", "data.txt"} {
		if name != "missing.json" {
			os.WriteFile(filepath.Join(dir, name), []byte("not json"), 0600)
		}
	}
	for _, path := range []string{dir, filepath.Join(dir, "missing.json"), filepath.Join(dir, "bad.json"), filepath.Join(dir, "data.txt")} {
		body, _ := json.Marshal(map[string]any{"path": path, "revision": 0})
		w := request(t, h, "POST", "/api/open", string(body), cookie)
		if w.Code != 400 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
