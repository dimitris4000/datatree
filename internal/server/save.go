package server

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func saveFormatMatches(path, format string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "yml" {
		ext = "yaml"
	}
	if ext == "ndjson" {
		ext = "jsonl"
	}
	return ext == format
}

func (s *browserSession) saveLocal(path string, as bool, revision int) (string, error) {
	if revision != s.data.Revision {
		return "", errors.New("stale revision; refresh the current document")
	}
	if s.data.Doc == nil {
		return "", errors.New("open a document first")
	}
	if !as {
		path = s.path
	}
	if strings.TrimSpace(path) == "" {
		return "", errors.New("choose a destination with Save As")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if !saveFormatMatches(path, s.data.Doc.Format) {
		return "", fmt.Errorf("choose a .%s filename with Save As", s.data.Doc.Format)
	}
	content := s.data.Doc.Source
	if as {
		if err = saveNewCopy(path, content); err != nil {
			return "", err
		}
	} else {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", errors.New("save destination is no longer a regular file; use Save As")
		}
		_, current, err := readLocalInput(path, s.data.Limits.Normalized().MaxBytes)
		if err != nil {
			return "", err
		}
		if sha256.Sum256(current) != s.savedHash {
			return "", errors.New("file changed on disk since opening or saving; use Save As to preserve both versions")
		}
		file, err := os.CreateTemp(filepath.Dir(path), ".datatree-save-*")
		if err != nil {
			return "", err
		}
		temp := file.Name()
		defer os.Remove(temp)
		err = file.Chmod(info.Mode().Perm())
		if err == nil {
			_, err = file.Write(content)
		}
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return "", err
		}
		// Recheck after writing the temporary copy before replacing the destination.
		latest, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !os.SameFile(info, latest) || !latest.Mode().IsRegular() {
			return "", errors.New("save destination changed; use Save As")
		}
		_, current, err = readLocalInput(path, s.data.Limits.Normalized().MaxBytes)
		if err != nil {
			return "", err
		}
		if sha256.Sum256(current) != s.savedHash {
			return "", errors.New("file changed during save; use Save As")
		}
		if err = os.Rename(temp, path); err != nil {
			return "", err
		}
	}
	s.path = path
	s.filename = filepath.Base(path)
	s.savedHash = sha256.Sum256(content)
	return path, nil
}
