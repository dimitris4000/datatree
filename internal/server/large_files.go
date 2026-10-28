package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"datatree/internal/document"
)

type sourcePatch = document.Patch

func normalizedSource(b []byte) string {
	return strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\r", "\n")
}

func applyPatches(ctx context.Context, d *document.Session, patches []sourcePatch, revision int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.PatchContext(ctx, patches, revision)
}
func (s *Server) largeFileRead(w http.ResponseWriter, r *http.Request, session *browserSession) {
	d := session.data
	if d.Doc == nil {
		fail(w, errors.New("open a file first"))
		return
	}
	if r.URL.Query().Get("revision") == "" {
		fail(w, errors.New("document revision required"))
		return
	}
	if err := checkQueryRevision(r, d); err != nil {
		fail(w, err)
		return
	}
	q := r.URL.Query()
	w.Header().Set("X-Document-Revision", strconv.Itoa(d.Revision))
	switch r.URL.Path {
	case "/api/source":
		source := d.Doc.Source
		if q.Get("original") == "true" {
			source = d.Original
		}
		if q.Has("start") || q.Has("end") {
			start, e1 := strconv.Atoi(q.Get("start"))
			end, e2 := strconv.Atoi(q.Get("end"))
			if e1 != nil || e2 != nil || end < start {
				fail(w, errors.New("invalid source slice"))
				return
			}
			a, err := utf16RawByte(string(source), start)
			if err != nil {
				fail(w, err)
				return
			}
			b, err := utf16RawByte(string(source), end)
			if err != nil {
				fail(w, err)
				return
			}
			source = source[a:b]
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if bytes.IndexByte(source, '\r') < 0 {
			w.Write(source)
		} else {
			w.Write([]byte(normalizedSource(source)))
		}
	case "/api/locate", "/api/node-location":
		var span document.SourceRange
		var ok bool
		if r.URL.Path == "/api/node-location" {
			span, ok = d.Doc.SourceRange(q.Get("node"))
		} else {
			start, e1 := strconv.Atoi(q.Get("start"))
			end, e2 := strconv.Atoi(q.Get("end"))
			if e1 != nil || e2 != nil || start < 0 || end < start {
				fail(w, errors.New("invalid source range"))
				return
			}
			span, ok = d.Doc.Locate(start, end)
		}
		var result any
		var ancestors any = []any{}
		if ok {
			result = span
			ancestors = d.Doc.Ancestors(span.ID, 20)
		}
		writeJSON(w, map[string]any{"revision": d.Revision, "range": result, "ancestors": ancestors})
	case "/api/representation":
		id := q.Get("node")
		if id == "" {
			id = d.Doc.Root.ID
		}
		_, ok := d.Doc.LookupNode(id)
		if !ok {
			http.Error(w, "node not found", 404)
			return
		}
		format := q.Get("format")
		limit := 65536
		if q.Get("full") == "true" {
			limit = 0
		}
		text, total, err := d.Doc.RepresentationContext(r.Context(), id, format, limit)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]any{"revision": d.Revision, "text": text, "truncated": limit > 0 && total > len(text), "totalBytes": total, "format": format})
	}
}

// snapshot is called while holding the session lock. Documents and attachments
// are immutable; history slices are copied by Session.Clone.
func (s *browserSession) snapshot() *browserSession {
	return &browserSession{data: s.data.Clone(), filename: s.filename, path: s.path, savedHash: s.savedHash, comparison: s.comparison, comparisonFilename: s.comparisonFilename, schema: s.schema, schemaGeneration: s.schemaGeneration, originalDocument: s.originalDocument, originalMetadata: s.originalMetadata, comparisonMetadata: s.comparisonMetadata, git: s.git, gitPath: s.gitPath, gitLoaded: s.gitLoaded}
}

func boundedText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit] + "…"
}

// Browser offsets normalize CRLF into one LF; preserve untouched exact bytes.
func utf16RawByte(text string, target int) (int, error) {
	if target < 0 {
		return 0, errors.New("invalid UTF-16 boundary")
	}
	units := 0
	for pos := 0; pos < len(text); {
		if units == target {
			return pos, nil
		}
		r, size := utf8.DecodeRuneInString(text[pos:])
		if r == '\r' && pos+1 < len(text) && text[pos+1] == '\n' {
			pos++
			r = '\n'
		}
		units++
		if r > 0xffff {
			units++
		}
		if units > target {
			return 0, errors.New("invalid UTF-16 boundary")
		}
		pos += size
	}
	if units == target {
		return len(text), nil
	}
	return 0, errors.New("UTF-16 boundary outside source")
}
