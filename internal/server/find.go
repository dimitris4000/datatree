package server

import (
	"errors"
	"net/http"
	"strconv"

	"datatree/internal/document"
)

func (s *Server) find(w http.ResponseWriter, r *http.Request, d *document.Session) {
	if d.Doc == nil {
		fail(w, errors.New("open a file first"))
		return
	}
	q := r.URL.Query()
	if q.Get("revision") == "" {
		fail(w, errors.New("find requires a document revision"))
		return
	}
	if err := checkQueryRevision(r, d); err != nil {
		fail(w, err)
		return
	}
	options := document.FindOptions{Mode: q.Get("mode"), Text: q.Get("text")}
	for key, target := range map[string]*bool{"keys": &options.Keys, "values": &options.Values, "caseSensitive": &options.CaseSensitive} {
		if raw, exists := q[key]; exists {
			if len(raw) != 1 {
				fail(w, errors.New("duplicate find option: "+key))
				return
			}
			value, err := strconv.ParseBool(raw[0])
			if err != nil {
				fail(w, errors.New("invalid find option: "+key))
				return
			}
			*target = value
		}
	}
	result, err := d.Doc.Find(options)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, struct {
		document.FindResult
		Revision int `json:"revision"`
	}{result, d.Revision})
}
