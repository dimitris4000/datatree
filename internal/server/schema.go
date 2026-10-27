package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"datatree/internal/document"
)

type schemaMetadata struct {
	Filename string `json:"filename"`
	Token    string `json:"token"`
}
type attachedSchema struct {
	compiled *document.CompiledSchema
	metadata schemaMetadata
}

func newAttachedSchema(compiled *document.CompiledSchema, text []byte, filename string, generation uint64) *attachedSchema {
	hash := sha256.Sum256(text)
	if len(filename) > 1024 {
		filename = "Uploaded schema"
	}
	if strings.TrimSpace(filename) == "" {
		filename = "Pasted schema"
	}
	parts := strings.Split(strings.ReplaceAll(filename, "\\", "/"), "/")
	filename = parts[len(parts)-1]
	return &attachedSchema{compiled: compiled, metadata: schemaMetadata{Filename: filename, Token: fmt.Sprintf("%d-%x", generation, hash)}}
}

// Schema work deliberately bypasses ServeHTTP's global-lock dispatch. Snapshot
// only immutable inputs, then verify session identity and both generations.
func (s *Server) schemaRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Revision    *int   `json:"revision"`
		Text        string `json:"text"`
		Filename    string `json:"filename"`
		SchemaToken string `json:"schemaToken"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, document.MaxSchemaBytes*6+4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		fail(w, err)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		fail(w, errors.New("trailing request data"))
		return
	}
	if in.Revision == nil {
		fail(w, errors.New("schema operations require a document revision"))
		return
	}
	s.mu.Lock()
	session := s.session(w, r)
	if session.data.Doc == nil {
		s.mu.Unlock()
		fail(w, errors.New("open a file first"))
		return
	}
	if *in.Revision != session.data.Revision {
		s.mu.Unlock()
		fail(w, errors.New("stale revision; refresh Validation"))
		return
	}
	if r.URL.Path == "/api/schema-clear" {
		session.schema = nil
		session.schemaGeneration++
		writeJSON(w, map[string]any{"revision": session.data.Revision, "schema": nil})
		s.mu.Unlock()
		return
	}
	if session.data.Doc.Format != "json" && session.data.Doc.Format != "yaml" && session.data.Doc.Format != "csv" && session.data.Doc.Format != "jsonl" {
		s.mu.Unlock()
		fail(w, errors.New("schema validation supports JSON, JSONL, YAML and CSV"))
		return
	}
	attachment := session.schema
	if r.URL.Path == "/api/validate" {
		if attachment == nil {
			s.mu.Unlock()
			fail(w, errors.New("attach a schema first"))
			return
		}
		if in.SchemaToken == "" {
			s.mu.Unlock()
			fail(w, errors.New("validation requires a schema token"))
			return
		}
		if in.SchemaToken != attachment.metadata.Token {
			s.mu.Unlock()
			fail(w, errors.New("stale revision or schema; refresh Validation"))
			return
		}
	}
	generation := session.schemaGeneration
	source := bytes.Clone(session.data.Doc.Source)
	format := session.data.Doc.Format
	s.mu.Unlock()

	var result document.ValidationResult
	var compiled *document.CompiledSchema
	var err error
	if r.URL.Path == "/api/schema" {
		compiled, err = document.CompileSchema([]byte(in.Text))
	} else {
		var snapshot *document.Document
		snapshot, err = document.Parse(format, source)
		if err == nil {
			result, err = document.ValidateSchema(snapshot, attachment.compiled)
		}
	}
	if err != nil {
		fail(w, err)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	live := false
	for _, candidate := range s.sessions {
		if candidate == session {
			live = true
			break
		}
	}
	if !live || time.Since(session.touched) > 30*time.Minute || session.data.Revision != *in.Revision || session.schemaGeneration != generation {
		fail(w, errors.New("stale revision or schema; refresh Validation"))
		return
	}
	if compiled != nil {
		session.schemaGeneration++
		session.schema = newAttachedSchema(compiled, []byte(in.Text), in.Filename, session.schemaGeneration)
		writeJSON(w, map[string]any{"revision": *in.Revision, "schema": session.schema.metadata})
		return
	}
	writeJSON(w, struct {
		document.ValidationResult
		Revision int            `json:"revision"`
		Schema   schemaMetadata `json:"schema"`
	}{result, *in.Revision, attachment.metadata})
}
