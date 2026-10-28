package server

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"datatree/internal/document"
)

//go:embed web
var assets embed.FS

type browserSession struct {
	mu                 sync.Mutex
	jobs               map[string]*operationJob
	originalDocument   *document.Document
	originalMetadata   diffBaseline
	comparisonMetadata diffBaseline
	git                *gitInfo
	gitPath            string
	gitLoaded          bool
	data               *document.Session
	filename           string
	path               string
	comparison         *document.Document
	comparisonFilename string
	schema             *attachedSchema
	schemaGeneration   uint64
	touched            time.Time
}
type Server struct {
	jobSlots  chan struct{}
	mu        sync.Mutex
	sessions  map[string]*browserSession
	static    http.Handler
	inspector *template.Template
	initial   *browserSession
	limits    document.Limits
}

func New(options ...document.Limits) *Server {
	limits := document.DefaultLimits()
	if len(options) > 0 {
		limits = options[0].Normalized()
	}
	sub, _ := fs.Sub(assets, "web")
	return &Server{jobSlots: make(chan struct{}, 4), sessions: map[string]*browserSession{}, static: http.FileServer(http.FS(sub)), inspector: template.Must(template.ParseFS(assets, "web/inspector.html")), limits: limits}
}

// NewWithFile validates the initial document before the caller starts listening.
func NewWithFile(path string, options ...document.Limits) (*Server, error) {
	s := New(options...)
	initial := &browserSession{data: document.NewSession(s.limits)}
	if err := initial.openLocal(path, 0); err != nil {
		return nil, fmt.Errorf("load startup file %q: %w", path, err)
	}
	initial.originalDocument = initial.data.Doc
	initial.originalMetadata = baselineMetadata("original", initial.filename, initial.data.Doc)
	s.initial = initial
	return s, nil
}
func loopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if !loopbackHost(r.Host) {
		http.Error(w, "loopback host required", http.StatusForbidden)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		origin, err := url.Parse(r.Header.Get("Origin"))
		if err != nil || origin.Host != r.Host || origin.Scheme != "http" {
			http.Error(w, "same-origin request required", http.StatusForbidden)
			return
		}
	}
	if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/vendor/") || strings.HasPrefix(r.URL.Path, "/assets/") {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		if r.URL.Path == "/" {
			s.mu.Lock()
			s.session(w, r)
			s.mu.Unlock()
			b, _ := assets.ReadFile("web/index.html")
			if _, err := fs.Stat(assets, "web/vendor/p5.min.js"); err != nil {
				b = bytes.ReplaceAll(b, []byte(`<script defer src="/vendor/p5.min.js"></script>`), nil)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
			return
		}
		r2 := r.Clone(r.Context())
		u := *r.URL
		r2.URL = &u
		r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/assets")
		s.static.ServeHTTP(w, r2)
		return
	}
	if r.URL.Path == "/api/jobs" || r.URL.Path == "/api/jobs/cancel" {
		s.jobsRequest(w, r)
		return
	}
	if r.URL.Query().Get("async") == "true" {
		s.startJob(w, r)
		return
	}
	if r.URL.Path == "/api/files" {
		s.fileBrowserRequest(w, r)
		return
	}
	if r.URL.Path == "/api/schema" || r.URL.Path == "/api/schema-clear" || r.URL.Path == "/api/validate" {
		s.schemaRequest(w, r)
		return
	}
	s.mu.Lock()
	session := s.session(w, r)
	s.mu.Unlock()
	if job, ok := r.Context().Value(operationJobKey{}).(*operationJob); ok && job.owner != session {
		fail(w, errors.New("stale revision; session expired"))
		return
	}
	session.mu.Lock()
	locked := true
	lockedSession := session
	defer func() {
		if locked {
			lockedSession.mu.Unlock()
		}
	}()
	if r.Method == "GET" {
		snapshot := session.snapshot()
		session.mu.Unlock()
		locked = false
		session = snapshot
	}
	d := session.data
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/api/source", "/api/locate", "/api/node-location", "/api/representation":
			s.largeFileRead(w, r, session)
			return
		case "/api/state":
			state(w, session)
			return
		case "/api/original":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write(d.Original)
			return
		case "/api/diff":
			session.diff(w, r)
			return
		case "/api/find":
			s.find(w, r, d)
			return
		case "/api/source-map":
			if d.Doc == nil {
				fail(w, errors.New("open a file first"))
				return
			}
			if err := checkQueryRevision(r, d); err != nil {
				fail(w, err)
				return
			}
			writeJSON(w, map[string]any{"revision": d.Revision, "ranges": d.Doc.SourceRanges()})
			return
		case "/api/tree":
			if d.Doc == nil {
				fail(w, errors.New("open a file first"))
				return
			}
			if err := checkQueryRevision(r, d); err != nil {
				fail(w, err)
				return
			}
			id := r.URL.Query().Get("node")
			if id == "" {
				id = d.Doc.Root.ID
			}
			n, ok := d.Doc.LookupNode(id)
			if !ok {
				http.Error(w, "node not found", 404)
				return
			}
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if offset < 0 {
				offset = 0
			}
			if limit < 1 {
				limit = 20
			}
			if limit > 50 {
				limit = 50
			}
			writeJSON(w, treeNode(d.Doc, n, offset, limit, 2))
			return
		case "/inspector":
			s.renderInspector(w, r, d, "")
			return
		case "/api/download":
			if d.Doc == nil {
				fail(w, errors.New("open a file first"))
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="document.`+d.Doc.Format+`"`)
			w.Write(d.Doc.Source)
			return
		default:
			http.NotFound(w, r)
			return
		}
	}
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var in struct {
		document.Action
		Revision      int           `json:"revision"`
		Direction     string        `json:"direction"`
		Path          string        `json:"path"`
		Filename      string        `json:"filename"`
		Baseline      string        `json:"baseline"`
		BaselineToken string        `json:"baselineToken"`
		ChangeIndex   *int          `json:"changeIndex"`
		SaveFilename  *string       `json:"saveFilename"`
		Schema        *schemaUpload `json:"schema"`
		Patches       []sourcePatch `json:"patches"`
		Full          bool          `json:"full"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, (int64(s.limits.MaxBytes)+document.MaxSchemaBytes)*6+8192)
	if r.URL.Path == "/actions/edit" {
		if err := r.ParseForm(); err != nil {
			fail(w, err)
			return
		}
		in.Revision, _ = strconv.Atoi(r.FormValue("revision"))
		in.Op = r.FormValue("op")
		in.Node = r.FormValue("node")
		in.Key = r.FormValue("key")
		in.Text = r.FormValue("text")
		in.Format = r.FormValue("format")
		in.Confirm = r.FormValue("confirm") == "true"
		if r.FormValue("structured") == "true" {
			in.Structured = true
			kind := r.FormValue("kind")
			value := r.FormValue("value")
			if kind == "string" {
				b, _ := json.Marshal(value)
				in.Text = string(b)
			} else if kind == "null" {
				in.Text = "null"
			} else {
				in.Text = value
			}
			in.Format = "json"
			parsed, err := d.Parse("json", []byte(in.Text))
			if err != nil || parsed.Root.Kind != kind {
				s.renderInspector(w, r, d, "Value does not match the selected type")
				return
			}
		}
	} else {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			fail(w, err)
			return
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			fail(w, errors.New("trailing request data"))
			return
		}
	}
	var live *browserSession
	baseRevision, baseGeneration := d.Revision, session.schemaGeneration
	baseComparison := session.comparison
	switch r.URL.Path {
	case "/api/apply", "/api/patch", "/api/edit", "/actions/edit", "/api/history", "/api/import", "/api/open", "/api/diff-revert", "/api/preview":
		live = session
		session = live.snapshot()
		d = session.data
		live.mu.Unlock()
		locked = false
	}
	if r.URL.Path == "/api/convert" && in.SaveFilename == nil {
		live = session
		session = live.snapshot()
		d = session.data
		live.mu.Unlock()
		locked = false
	}
	var err error
	var savedPath string
	switch r.URL.Path {
	case "/api/import":
		err = session.importUploadedContext(r.Context(), in.Format, in.Text, in.Filename, in.Schema, in.Revision)
	case "/api/open":
		err = session.openLocalContext(r.Context(), in.Path, in.Revision)
	case "/api/diff-revert":
		if in.ChangeIndex == nil {
			err = errors.New("choose a change to revert")
		} else {
			err = session.revertDiff(in.Baseline, in.BaselineToken, *in.ChangeIndex, in.Revision)
		}
	case "/api/diff-baseline":
		if in.Path != "" {
			session.replaceDiffBaselineFromPath(w, in.Path, in.Revision)
		} else {
			session.replaceDiffBaseline(w, in.Format, in.Text, in.Filename, in.Revision)
		}
		return
	case "/api/patch":
		err = applyPatches(r.Context(), d, in.Patches, in.Revision)
	case "/api/apply":
		err = d.ApplyContext(r.Context(), in.Format, []byte(in.Text), in.Revision)
	case "/api/edit", "/actions/edit":
		err = d.MutateContext(r.Context(), in.Action, in.Revision)
	case "/api/history":
		err = d.HistoryContext(r.Context(), in.Direction, in.Revision)
	case "/api/preview":
		if in.Revision != d.Revision {
			err = errors.New("stale revision")
		} else {
			var text string
			text, err = d.Preview(in.Format)
			if err == nil {
				if r.Context().Err() != nil {
					return
				}
				writeJSON(w, session.conversionPreview(in.Format, text, in.Full))
				return
			}
		}
	case "/api/convert":
		if !in.Confirm {
			err = errors.New("preview and confirm conversion first")
		} else {
			savedPath, err = session.convert(in.Format, in.Revision, in.SaveFilename)
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err == nil && (r.URL.Path == "/api/import" || r.URL.Path == "/api/open") {
		session.originalDocument = d.Doc
		session.originalMetadata = baselineMetadata("original", session.filename, d.Doc)
		session.comparisonMetadata = diffBaseline{}
		session.comparison = nil
		session.comparisonFilename = ""
	}
	if live != nil && err == nil {
		live.mu.Lock()
		if r.Context().Err() != nil {
			err = r.Context().Err()
		} else if live.data.Revision != baseRevision || live.schemaGeneration != baseGeneration || live.comparison != baseComparison {
			err = errors.New("stale revision; document changed during operation")
		} else {
			if job, ok := r.Context().Value(operationJobKey{}).(*operationJob); ok {
				job.committed = true
			}
			live.data = session.data
			live.filename = session.filename
			live.path = session.path
			live.comparison = session.comparison
			live.comparisonFilename = session.comparisonFilename
			live.schema = session.schema
			live.schemaGeneration = session.schemaGeneration
			live.originalDocument = session.originalDocument
			live.originalMetadata = session.originalMetadata
			live.comparisonMetadata = session.comparisonMetadata
			live.git = session.git
			live.gitPath = session.gitPath
			live.gitLoaded = session.gitLoaded
		}
		if err != nil {
			d = live.data.Clone()
		}
		live.mu.Unlock()
	}
	if r.URL.Path == "/actions/edit" {
		if err != nil {
			s.renderInspector(w, r, d, err.Error())
			return
		}
		w.Header().Set("HX-Trigger", "documentChanged")
		r.URL.RawQuery = "node=" + url.QueryEscape(d.Doc.Root.ID)
		s.renderInspector(w, r, d, "")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	if r.URL.Path == "/api/patch" {
		v := stateValue(session, savedPath)
		v["previousRevision"] = in.Revision
		v["patches"] = in.Patches
		writeJSON(w, v)
		return
	}
	writeState(w, session, savedPath)
}
func checkQueryRevision(r *http.Request, d *document.Session) error {
	q := r.URL.Query().Get("revision")
	if q != "" && q != strconv.Itoa(d.Revision) {
		return errors.New("stale revision; refresh selection")
	}
	return nil
}
func (s *Server) session(w http.ResponseWriter, r *http.Request) *browserSession {
	now := time.Now()
	for id, v := range s.sessions {
		if now.Sub(v.touched) > 30*time.Minute {
			delete(s.sessions, id)
		}
	}
	if c, err := r.Cookie("datatree_session"); err == nil {
		if v := s.sessions[c.Value]; v != nil {
			v.touched = now
			return v
		}
	}
	if len(s.sessions) >= 16 {
		var oldest string
		var when time.Time
		for id, v := range s.sessions {
			if oldest == "" || v.touched.Before(when) {
				oldest = id
				when = v.touched
			}
		}
		delete(s.sessions, oldest)
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		panic(err)
	}
	id := hex.EncodeToString(token)
	v := &browserSession{data: document.NewSession(s.limits), touched: now}
	if s.initial != nil {
		initial := s.initial
		// Parsed documents are immutable; edits produce a new document. Each
		// session owns its revision, original bytes and history independently.
		v.data = &document.Session{Doc: initial.data.Doc, Original: initial.data.Original, OriginalFormat: initial.data.OriginalFormat, Revision: 1, Limits: s.limits}
		v.path, v.filename = initial.path, initial.filename
		v.git, v.gitPath, v.gitLoaded = initial.git, initial.gitPath, initial.gitLoaded
		v.originalDocument = initial.data.Doc
		v.originalMetadata = initial.originalMetadata
		v.schema, v.schemaGeneration = initial.schema, initial.schemaGeneration
	}
	s.sessions[id] = v
	http.SetCookie(w, &http.Cookie{Name: "datatree_session", Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	return v
}
func state(w http.ResponseWriter, session *browserSession) {
	writeState(w, session, "")
}

func writeState(w http.ResponseWriter, session *browserSession, savedPath string) {
	writeJSON(w, stateValue(session, savedPath))
}
func stateValue(session *browserSession, savedPath string) map[string]any {
	d := session.data
	v := map[string]any{"revision": d.Revision, "canUndo": d.CanUndo(), "canRedo": d.CanRedo()}
	v["limits"] = d.Limits.Normalized()
	if savedPath != "" {
		v["savedPath"] = savedPath
	}
	if session.schema != nil {
		v["schema"] = session.schema.metadata
	}
	if session.filename != "" {
		v["filename"] = session.filename
	}
	if !session.gitLoaded || session.gitPath != session.path {
		session.git = localGitInfo(session.path)
		session.gitPath = session.path
		session.gitLoaded = true
	}
	if info := session.git; info != nil {
		v["git"] = info
	}
	if d.Doc != nil {
		v["format"] = d.Doc.Format
		v["originalMatches"] = normalizedSourcesEqual(d.Doc.Source, d.Original)
		v["root"] = d.Doc.Root.ID
		v["nodes"] = d.Doc.NodeCount()
		if changed := d.Doc.StableChanges(); len(changed) > 0 {
			v["stableIDs"] = true
			v["changedNodes"] = changed
		}
	}
	return v
}

// Editor offsets normalize all line endings. Compare at revision boundaries,
// never on each browser caret movement, without creating normalized copies.
func normalizedSourcesEqual(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	if !bytes.ContainsAny(a, "\r") && !bytes.ContainsAny(b, "\r") {
		return false
	}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		left, right := a[i], b[j]
		i++
		j++
		if left == '\r' {
			left = '\n'
			if i < len(a) && a[i] == '\n' {
				i++
			}
		}
		if right == '\r' {
			right = '\n'
			if j < len(b) && b[j] == '\n' {
				j++
			}
		}
		if left != right {
			return false
		}
	}
	return i == len(a) && j == len(b)
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if strings.Contains(err.Error(), "stale revision") {
		code = http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

type wireNode struct {
	ID       string      `json:"id"`
	Label    string      `json:"label"`
	Kind     string      `json:"kind"`
	Value    string      `json:"value,omitempty"`
	Path     string      `json:"path,omitempty"`
	Location string      `json:"location,omitempty"`
	Target   string      `json:"target,omitempty"`
	Total    int         `json:"total"`
	Offset   int         `json:"offset"`
	Children []*wireNode `json:"children,omitempty"`
}

func treeNode(d *document.Document, n *document.Node, offset, limit, depth int) *wireNode {
	pageLimit := limit
	if depth <= 0 {
		pageLimit = 0
	}
	children, total := d.ChildPage(n.ID, offset, pageLimit)
	value := n.Value
	if len(value) > 160 {
		value = boundedText(value, 160)
	}
	v := &wireNode{ID: n.ID, Label: boundedText(n.Label, 512), Kind: n.Kind, Value: value, Path: boundedText(n.Path, 8192), Location: boundedText(n.Location, 8192), Target: n.Target, Total: total, Offset: offset}
	if depth > 0 {
		for _, child := range children {
			v.Children = append(v.Children, treeNode(d, child, 0, limit, depth-1))
		}
	}

	return v
}

type inspection struct {
	Node                         *document.Node
	Revision                     int
	JSON, YAML, JSONError, Error string
	NativeFormat                 string
	Scalar                       bool
	Draft                        bool
	HasParent                    bool
	CanRename                    bool
}

func (s *Server) renderInspector(w http.ResponseWriter, r *http.Request, d *document.Session, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if d.Doc == nil {
		io.WriteString(w, "<p class=muted>Open a file and select a node.</p>")
		return
	}
	if err := checkQueryRevision(r, d); err != nil {
		io.WriteString(w, "<p class=error>Selection is stale. Select the node again.</p>")
		return
	}
	if message != "" && r.Method == "POST" && r.FormValue("revision") != strconv.Itoa(d.Revision) {
		io.WriteString(w, "<p class=error>Selection is stale. Select the node again.</p>")
		return
	}
	id := r.URL.Query().Get("node")
	if id == "" {
		id = r.FormValue("node")
	}
	if id == "" {
		id = d.Doc.Root.ID
	}
	n, ok := d.Doc.LookupNode(id)
	if !ok {
		io.WriteString(w, "<p class=error>Node no longer exists. Select another node.</p>")
		return
	}
	j, y, e := "", "", ""
	f := "json"
	if d.Doc.Format == "yaml" {
		f = "yaml"
	}
	scalar := len(n.Value) <= 65536 && n.Kind != "object" && n.Kind != "array" && n.Kind != "stream" && n.Kind != "alias" && n.Kind != "tagged"
	draft := false
	if message != "" && r.FormValue("op") == "replace" {
		if _, ok := r.Form["text"]; ok {
			draft = true
			if r.FormValue("format") == "yaml" {
				y = r.FormValue("text")
			} else {
				j = r.FormValue("text")
			}
		}
		if r.FormValue("structured") == "true" {
			copy := *n
			copy.Value = r.FormValue("value")
			n = &copy
		}
	}
	copyNode := *n
	copyNode.Label = boundedText(n.Label, 512)
	copyNode.Path = boundedText(n.Path, 8192)
	copyNode.Location = boundedText(n.Location, 8192)
	copyNode.Value = boundedText(n.Value, 65536)
	data := inspection{Draft: draft, Node: &copyNode, Revision: d.Revision, JSON: j, YAML: y, JSONError: e, Error: message, NativeFormat: f, Scalar: scalar, HasParent: n.ID != d.Doc.Root.ID, CanRename: d.Doc.Format != "csv" && n.ID != d.Doc.Root.ID}
	if err := s.inspector.Execute(w, data); err != nil {
		fmt.Println("inspector:", err)
	}
}
