package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"datatree/internal/document"
)

func (s *browserSession) openLocal(path string, revision int) error {
	if revision != s.data.Revision {
		return errors.New("stale revision; refresh the current document")
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("enter a local file path")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	schemaPath := companionPath(path)
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	// Check before opening so named pipes and devices cannot block the server.
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("choose a regular file")
	}
	format := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch format {
	case "yml":
		format = "yaml"
	case "ndjson":
		format = "jsonl"
	case "json", "jsonl", "yaml", "csv":
	default:
		return errors.New("choose a JSON, JSONL, YAML or CSV file")
	}
	if info.Size() > document.MaxBytes {
		return errors.New("file exceeds 10 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, document.MaxBytes+1))
	if err != nil {
		return err
	}
	if len(data) > document.MaxBytes {
		return errors.New("file exceeds 10 MiB")
	}
	schema, err := readCompanionSchema(schemaPath, s.schemaGeneration+1)
	if err != nil {
		return err
	}
	if err = s.data.Import(format, data, revision); err != nil {
		return fmt.Errorf("invalid %s document: %w", strings.ToUpper(format), err)
	}
	s.path = path
	s.filename = filepath.Base(path)
	s.schema = schema
	s.schemaGeneration++
	return nil
}

type gitInfo struct {
	Branch string `json:"branch"`
	Commit string `json:"commit"`
}

// Only a tracked file with a resolvable HEAD has Git provenance. Commands are
// read-only, literal pathspecs prevent glob matching, and no shell is involved.
func localGitInfo(path string) *gitInfo {
	if path == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"--literal-pathspecs", "-C", filepath.Dir(path)}, args...)...)
		// Do not inherit repository overrides from the server's launching shell.
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "GIT_") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	if _, err := run("ls-files", "--error-unmatch", "--", filepath.Base(path)); err != nil {
		return nil
	}
	commit, err := run("rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil
	}
	branch, _ := run("symbolic-ref", "--quiet", "--short", "HEAD")
	return &gitInfo{Branch: branch, Commit: commit}
}
