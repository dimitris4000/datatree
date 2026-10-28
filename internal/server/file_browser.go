package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const browserEntryLimit = 1000
const browserScanLimit = 10000

type browserEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
}

type directoryListing struct {
	Revision  int            `json:"revision"`
	Path      string         `json:"path"`
	Parent    string         `json:"parent"`
	Entries   []browserEntry `json:"entries"`
	Truncated bool           `json:"truncated"`
	Limit     int            `json:"limit"`
}

func supportedDocumentName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json", ".jsonl", ".ndjson", ".yaml", ".yml", ".csv":
		return true
	}
	return false
}

func browseDirectory(path string, hidden bool) (directoryListing, error) {
	result := directoryListing{Entries: []browserEntry{}, Limit: browserEntryLimit}
	path, err := filepath.Abs(path)
	if err != nil {
		return result, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, errors.New("choose a directory to browse")
	}
	dir, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(browserScanLimit + 1)
	if err != nil && err != io.EOF {
		return result, err
	}
	result.Path, result.Parent = path, filepath.Dir(path)
	if len(entries) > browserScanLimit {
		result.Truncated = true
		entries = entries[:browserScanLimit]
	}
	for _, entry := range entries {
		if !hidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		fullPath := filepath.Join(path, entry.Name())
		isDir, regular := entry.IsDir(), entry.Type().IsRegular()
		if entry.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(fullPath)
			if err != nil {
				continue
			}
			isDir, regular = info.IsDir(), info.Mode().IsRegular()
		}
		if !isDir && (!regular || !supportedDocumentName(entry.Name())) {
			continue
		}
		result.Entries = append(result.Entries, browserEntry{Name: entry.Name(), Path: fullPath, Directory: isDir})
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i], result.Entries[j]
		if a.Directory != b.Directory {
			return a.Directory
		}
		if strings.ToLower(a.Name) != strings.ToLower(b.Name) {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return a.Name < b.Name
	})
	if len(result.Entries) > browserEntryLimit {
		result.Truncated = true
		result.Entries = result.Entries[:browserEntryLimit]
	}
	return result, nil
}

// Filesystem reads run outside the global session lock. Same-origin POST is
// enforced by ServeHTTP before this handler can disclose directory contents.
func (s *Server) fileBrowserRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Revision   *int   `json:"revision"`
		Path       string `json:"path"`
		ShowHidden bool   `json:"showHidden"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
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
		fail(w, errors.New("file browsing requires a document revision"))
		return
	}
	if len(in.Path) > 4096 {
		fail(w, errors.New("directory path exceeds 4096 bytes"))
		return
	}
	s.mu.Lock()
	session := s.session(w, r)
	s.mu.Unlock()
	session.mu.Lock()
	if *in.Revision != session.data.Revision {
		session.mu.Unlock()
		fail(w, errors.New("stale revision; reopen the file picker"))
		return
	}
	path := in.Path
	if path == "" && session.path != "" {
		path = filepath.Dir(session.path)
	}
	session.mu.Unlock()
	listing, err := browseDirectory(path, in.ShowHidden)
	if err != nil {
		fail(w, fmt.Errorf("browse directory %q: %w", path, err))
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if *in.Revision != session.data.Revision {
		fail(w, errors.New("stale revision; reopen the file picker"))
		return
	}
	listing.Revision = *in.Revision
	writeJSON(w, listing)
}
