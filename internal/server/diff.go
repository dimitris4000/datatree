package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"datatree/internal/document"
)

type diffBaseline struct {
	Kind     string `json:"kind"`
	Filename string `json:"filename"`
	Format   string `json:"format"`
	Token    string `json:"token"`
}
type diffResponse struct {
	document.DiffResult
	Revision int          `json:"revision"`
	Baseline diffBaseline `json:"baseline"`
}

func (s *browserSession) diff(w http.ResponseWriter, r *http.Request) {
	if s.data.Doc == nil {
		fail(w, errors.New("open a file first"))
		return
	}
	if r.URL.Query().Get("revision") == "" {
		fail(w, errors.New("diff requires a document revision"))
		return
	}
	if err := checkQueryRevision(r, s.data); err != nil {
		fail(w, err)
		return
	}
	kind := r.URL.Query().Get("baseline")
	base, metadata, err := s.diffBaseline(kind)
	if err != nil {
		fail(w, err)
		return
	}

	result, err := document.Diff(base, s.data.Doc)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, diffResponse{result, s.data.Revision, metadata})
}
func (s *browserSession) replaceDiffBaseline(w http.ResponseWriter, format, text, filename string, revision int) {
	if s.data.Doc == nil {
		fail(w, errors.New("open a file first"))
		return
	}
	if revision != s.data.Revision {
		fail(w, errors.New("stale revision; refresh comparison"))
		return
	}
	base, err := s.data.Parse(format, []byte(text))
	if err != nil {
		fail(w, err)
		return
	}
	result, err := document.Diff(base, s.data.Doc)
	if err != nil {
		fail(w, err)
		return
	}
	// Commit only after both parsing and comparison succeed. This does not make a
	// document revision or history entry, and failed replacements retain the file.
	s.comparison = base
	s.comparisonFilename = filename
	writeJSON(w, diffResponse{result, s.data.Revision, baselineMetadata("file", filename, base)})
}

func (s *browserSession) replaceDiffBaselineFromPath(w http.ResponseWriter, path string, revision int) {
	if s.data.Doc == nil {
		fail(w, errors.New("open a file first"))
		return
	}
	if revision != s.data.Revision {
		fail(w, errors.New("stale revision; refresh comparison"))
		return
	}
	resolved, data, err := readLocalInput(path, s.data.Limits.Normalized().MaxBytes)
	if err != nil {
		fail(w, fmt.Errorf("load comparison %q: %w", path, err))
		return
	}
	format := strings.ToLower(strings.TrimPrefix(filepath.Ext(resolved), "."))
	if format == "yml" {
		format = "yaml"
	}
	if format == "ndjson" {
		format = "jsonl"
	}
	s.replaceDiffBaseline(w, format, string(data), filepath.Base(resolved), revision)
}

func baselineMetadata(kind, filename string, base *document.Document) diffBaseline {
	h := sha256.New()
	h.Write([]byte(base.Format))
	h.Write([]byte{0})
	h.Write(base.Source)
	return diffBaseline{Kind: kind, Filename: filename, Format: base.Format, Token: hex.EncodeToString(h.Sum(nil))}
}
func (s *browserSession) diffBaseline(kind string) (*document.Document, diffBaseline, error) {
	var base *document.Document
	var err error
	filename := s.filename
	switch kind {
	case "original":
		base, err = s.data.Parse(s.data.OriginalFormat, s.data.Original)
	case "file":
		base = s.comparison
		filename = s.comparisonFilename
		if base == nil {
			err = errors.New("choose a comparison file first")
		}
	default:
		err = errors.New("choose original or file comparison")
	}
	if err != nil {
		return nil, diffBaseline{}, err
	}
	return base, baselineMetadata(kind, filename, base), nil
}
func (s *browserSession) revertDiff(kind, token string, index, revision int) error {
	if s.data.Doc == nil {
		return errors.New("open a file first")
	}
	if revision != s.data.Revision {
		return errors.New("stale revision; refresh Changes before reverting")
	}
	base, metadata, err := s.diffBaseline(kind)
	if err != nil {
		return err
	}
	if token == "" || token != metadata.Token {
		return errors.New("stale revision or comparison baseline; refresh Changes before reverting")
	}
	return s.data.RevertDiff(base, index, revision)
}
