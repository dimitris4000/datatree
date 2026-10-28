package server

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

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
		Path        string `json:"path"`
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
	s.mu.Unlock()
	session.mu.Lock()
	if session.data.Doc == nil {
		session.mu.Unlock()
		fail(w, errors.New("open a file first"))
		return
	}
	if *in.Revision != session.data.Revision {
		session.mu.Unlock()
		fail(w, errors.New("stale revision; refresh Validation"))
		return
	}
	if r.URL.Path == "/api/schema-clear" {
		session.schema = nil
		session.schemaGeneration++
		writeJSON(w, map[string]any{"revision": session.data.Revision, "schema": nil})
		session.mu.Unlock()
		return
	}
	if session.data.Doc.Format != "json" && session.data.Doc.Format != "yaml" && session.data.Doc.Format != "csv" && session.data.Doc.Format != "jsonl" {
		session.mu.Unlock()
		fail(w, errors.New("schema validation supports JSON, JSONL, YAML and CSV"))
		return
	}
	attachment := session.schema
	if r.URL.Path == "/api/validate" {
		if attachment == nil {
			session.mu.Unlock()
			fail(w, errors.New("attach a schema first"))
			return
		}
		if in.SchemaToken == "" {
			session.mu.Unlock()
			fail(w, errors.New("validation requires a schema token"))
			return
		}
		if in.SchemaToken != attachment.metadata.Token {
			session.mu.Unlock()
			fail(w, errors.New("stale revision or schema; refresh Validation"))
			return
		}
	}
	generation := session.schemaGeneration
	snapshot := session.data.Doc
	session.mu.Unlock()

	var result document.ValidationResult
	var compiled *document.CompiledSchema
	var err error
	schemaText, schemaFilename := []byte(in.Text), in.Filename
	if r.URL.Path == "/api/schema" {
		if in.Path != "" {
			if in.Text != "" {
				fail(w, errors.New("choose a schema path or schema text, not both"))
				return
			}
			var path string
			path, schemaText, err = readLocalInput(in.Path, document.MaxSchemaBytes)
			if err != nil {
				fail(w, fmt.Errorf("load schema %q: %w", in.Path, err))
				return
			}
			schemaFilename = filepath.Base(path)
		}
		compiled, err = document.CompileSchema(schemaText)
		if err != nil && in.Path != "" {
			err = fmt.Errorf("load schema %q: %w", in.Path, err)
		}
	} else {
		result, err = document.ValidateSchema(snapshot, attachment.compiled)
	}
	if err != nil {
		fail(w, err)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	s.mu.Lock()
	live := false
	for _, candidate := range s.sessions {
		if candidate == session {
			live = true
			break
		}
	}
	s.mu.Unlock()
	session.mu.Lock()
	defer session.mu.Unlock()
	if !live || session.data.Revision != *in.Revision || session.schemaGeneration != generation {
		fail(w, errors.New("stale revision or schema; refresh Validation"))
		return
	}
	if compiled != nil {
		session.schemaGeneration++
		session.schema = newAttachedSchema(compiled, schemaText, schemaFilename, session.schemaGeneration)
		writeJSON(w, map[string]any{"revision": *in.Revision, "schema": session.schema.metadata})
		return
	}
	writeJSON(w, struct {
		document.ValidationResult
		Revision int            `json:"revision"`
		Schema   schemaMetadata `json:"schema"`
	}{result, *in.Revision, attachment.metadata})
}
