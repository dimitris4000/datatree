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
	data    *document.Session
	touched time.Time
}
type Server struct {
	mu        sync.Mutex
	sessions  map[string]*browserSession
	static    http.Handler
	inspector *template.Template
}

func New() *Server {
	sub, _ := fs.Sub(assets, "web")
	return &Server{sessions: map[string]*browserSession{}, static: http.FileServer(http.FS(sub)), inspector: template.Must(template.ParseFS(assets, "web/inspector.html"))}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.session(w, r)
	d := session.data
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/api/state":
			state(w, d)
			return
		case "/api/original":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write(d.Original)
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
			n := d.Doc.Nodes[id]
			if n == nil {
				http.Error(w, "node not found", 404)
				return
			}
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if offset < 0 {
				offset = 0
			}
			if limit < 1 || limit > 50 {
				limit = 50
			}
			writeJSON(w, treeNode(n, offset, limit, 2))
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
		Revision  int    `json:"revision"`
		Direction string `json:"direction"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, document.MaxBytes*6+4096)
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
			parsed, err := document.Parse("json", []byte(in.Text))
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
	var err error
	switch r.URL.Path {
	case "/api/import":
		err = d.Import(in.Format, []byte(in.Text), in.Revision)
	case "/api/apply":
		err = d.Apply(in.Format, []byte(in.Text), in.Revision)
	case "/api/edit", "/actions/edit":
		err = d.Mutate(in.Action, in.Revision)
	case "/api/history":
		err = d.History(in.Direction, in.Revision)
	case "/api/preview":
		if in.Revision != d.Revision {
			err = errors.New("stale revision")
		} else {
			var text string
			text, err = d.Preview(in.Format)
			if err == nil {
				writeJSON(w, map[string]any{"text": text, "format": in.Format, "revision": d.Revision})
				return
			}
		}
	case "/api/convert":
		if !in.Confirm {
			err = errors.New("preview and confirm conversion first")
		} else {
			err = d.Convert(in.Format, in.Revision)
		}
	default:
		http.NotFound(w, r)
		return
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
	state(w, d)
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
	v := &browserSession{data: document.NewSession(), touched: now}
	s.sessions[id] = v
	http.SetCookie(w, &http.Cookie{Name: "datatree_session", Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	return v
}
func state(w http.ResponseWriter, d *document.Session) {
	v := map[string]any{"revision": d.Revision, "canUndo": d.CanUndo(), "canRedo": d.CanRedo()}
	if d.Doc != nil {
		v["format"] = d.Doc.Format
		v["source"] = string(d.Doc.Source)
		v["root"] = d.Doc.Root.ID
		v["nodes"] = len(d.Doc.Nodes)
	}
	writeJSON(w, v)
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

func treeNode(n *document.Node, offset, limit, depth int) *wireNode {
	value := n.Value
	if len(value) > 160 {
		value = value[:160] + "…"
	}
	v := &wireNode{ID: n.ID, Label: n.Label, Kind: n.Kind, Value: value, Path: n.Path, Location: n.Location, Target: n.Target, Total: len(n.Children), Offset: offset}
	if depth > 0 {
		end := offset + limit
		if end > len(n.Children) {
			end = len(n.Children)
		}
		for i := offset; i < end; i++ {
			v.Children = append(v.Children, treeNode(n.Children[i], 0, limit, depth-1))
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
	n := d.Doc.Nodes[id]
	if n == nil {
		io.WriteString(w, "<p class=error>Node no longer exists. Select another node.</p>")
		return
	}
	j, je := d.Doc.JSON(n.ID, false)
	y, ye := d.Doc.YAML(n.ID)
	if ye != nil {
		message = ye.Error()
	}
	e := ""
	if je != nil {
		e = je.Error()
		j = ""
	}
	f := "json"
	if d.Doc.Format == "yaml" {
		f = "yaml"
	}
	scalar := n.Kind != "object" && n.Kind != "array" && n.Kind != "stream" && n.Kind != "alias" && n.Kind != "tagged"
	if message != "" && r.FormValue("op") == "replace" {
		if _, ok := r.Form["text"]; ok {
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
	data := inspection{Node: n, Revision: d.Revision, JSON: j, YAML: y, JSONError: e, Error: message, NativeFormat: f, Scalar: scalar, HasParent: n != d.Doc.Root, CanRename: d.Doc.Format != "csv" && n != d.Doc.Root}
	if err := s.inspector.Execute(w, data); err != nil {
		fmt.Println("inspector:", err)
	}
}
