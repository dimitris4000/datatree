package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *browserSession) conversionPreview(format, text string) map[string]any {
	name := filepath.Base(s.filename)
	if s.path != "" {
		name = filepath.Base(s.path)
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if stem == "" || stem == "." {
		stem = "document"
	}
	directory := ""
	if s.path != "" {
		directory = filepath.Dir(s.path)
	}
	return map[string]any{"text": text, "format": format, "revision": s.data.Revision, "suggestedFilename": stem + "." + format, "canSaveLocal": s.path != "", "saveDirectory": directory}
}

func (s *browserSession) convert(format string, revision int, filename *string) (string, error) {
	if filename == nil {
		return "", s.data.Convert(format, revision)
	}
	if s.path == "" {
		return "", errors.New("the uploaded file's directory is unavailable; download the converted copy instead")
	}
	name := *filename
	if strings.TrimSpace(name) == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\\x00") {
		return "", errors.New("enter a filename without directory separators")
	}
	if strings.ToLower(filepath.Ext(name)) != "."+format {
		return "", fmt.Errorf("the converted filename must end in .%s", format)
	}
	path := filepath.Join(filepath.Dir(s.path), name)
	err := s.data.ConvertWithSave(format, revision, func(content []byte) error {
		if err := saveNewCopy(path, content); err != nil {
			return fmt.Errorf("save converted copy %q: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// Exclusive creation preserves existing files, including symlinks and the input.
func saveNewCopy(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("a file already exists with that name; choose a different filename")
		}
		return err
	}
	_, err = file.Write(content)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		if cleanupErr := os.Remove(path); cleanupErr != nil {
			return fmt.Errorf("%w; remove partial copy: %v", err, cleanupErr)
		}
	}
	return err
}
