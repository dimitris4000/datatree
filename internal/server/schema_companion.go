package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"datatree/internal/document"
)

type schemaUpload struct {
	Filename string `json:"filename"`
	Text     string `json:"text"`
}

func companionPath(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".schema.json"
}

func readCompanionSchema(path string, generation uint64) (*attachedSchema, error) {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("load companion schema %q: %w", path, err)
	}
	read := func() ([]byte, error) {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("choose a regular schema file")
		}
		if info.Size() > document.MaxSchemaBytes {
			return nil, errors.New("schema exceeds 1 MiB")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		return io.ReadAll(io.LimitReader(file, document.MaxSchemaBytes+1))
	}
	text, err := read()
	if err != nil {
		return nil, fmt.Errorf("load companion schema %q: %w", path, err)
	}
	compiled, err := document.CompileSchema(text)
	if err != nil {
		return nil, fmt.Errorf("load companion schema %q: %w", path, err)
	}
	return newAttachedSchema(compiled, text, filepath.Base(path), generation), nil
}

func (s *browserSession) importUploaded(format, text, filename string, schema *schemaUpload, revision int) error {
	if revision != s.data.Revision {
		return errors.New("stale revision; refresh the current document")
	}
	var attachment *attachedSchema
	if schema != nil {
		if schema.Filename != companionPath(filename) {
			return fmt.Errorf("expected companion schema %q", companionPath(filename))
		}
		compiled, err := document.CompileSchema([]byte(schema.Text))
		if err != nil {
			return fmt.Errorf("load companion schema %q: %w", schema.Filename, err)
		}
		attachment = newAttachedSchema(compiled, []byte(schema.Text), schema.Filename, s.schemaGeneration+1)
	}
	if err := s.data.Import(format, []byte(text), revision); err != nil {
		return err
	}
	s.path, s.filename = "", filename
	s.schema = attachment
	s.schemaGeneration++
	return nil
}
