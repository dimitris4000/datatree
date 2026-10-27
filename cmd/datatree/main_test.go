package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplicationArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.csv")
	if err := os.WriteFile(path, []byte("name\nAlice\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {path}, {"-listen", "127.0.0.1:8090", path}} {
		h, address, err := application(args)
		if err != nil || h == nil || !strings.HasPrefix(address, "127.0.0.1:") {
			t.Fatalf("%v: %s %v", args, address, err)
		}
	}
	for _, fixture := range []struct {
		args   []string
		reason string
	}{
		{[]string{path, path}, "at most one"},
		{[]string{"-listen", "0.0.0.0:8080", path}, "loopback"},
		{[]string{filepath.Join(t.TempDir(), "missing.yaml")}, "missing.yaml"},
		{[]string{"-unknown"}, "flag provided but not defined"},
	} {
		if _, _, err := application(fixture.args); err == nil || !strings.Contains(err.Error(), fixture.reason) {
			t.Fatalf("%v: %v", fixture.args, err)
		}
	}
}
