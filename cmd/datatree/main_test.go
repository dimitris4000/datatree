package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
		wantAddress := ""
		if len(args) > 1 {
			wantAddress = "127.0.0.1:8090"
		}
		if err != nil || h == nil || address != wantAddress {
			t.Fatalf("%v: %s %v", args, address, err)
		}
	}
	for _, fixture := range []struct {
		args   []string
		reason string
	}{
		{[]string{path, path}, "at most one"},
		{[]string{"-listen", "0.0.0.0:8080", path}, "loopback"},
		{[]string{"-listen", ""}, "invalid -listen"},
		{[]string{filepath.Join(t.TempDir(), "missing.yaml")}, "missing.yaml"},
		{[]string{"-unknown"}, "flag provided but not defined"},
	} {
		if _, _, err := application(fixture.args); err == nil || !strings.Contains(err.Error(), fixture.reason) {
			t.Fatalf("%v: %v", fixture.args, err)
		}
	}
}

func TestListenAutomaticPorts(t *testing.T) {
	for _, succeedAt := range []int{1, 3, 5, 0} {
		t.Run(strconv.Itoa(succeedAt), func(t *testing.T) {
			seen := make(map[string]bool)
			want := &net.TCPListener{}
			bindErr := errors.New("port unavailable")
			listener, err := listen("", func(network, address string) (net.Listener, error) {
				host, portText, splitErr := net.SplitHostPort(address)
				port, portErr := strconv.Atoi(portText)
				if network != "tcp" || splitErr != nil || portErr != nil || host != "127.0.0.1" || port < 1000 || port > 9999 {
					t.Fatalf("invalid automatic listener: %s %s", network, address)
				}
				if seen[address] {
					t.Fatalf("retried the same port: %s", address)
				}
				seen[address] = true
				if len(seen) == succeedAt {
					return want, nil
				}
				return nil, bindErr
			})
			if succeedAt == 0 {
				if listener != nil || !errors.Is(err, bindErr) || !strings.Contains(err.Error(), "5 attempts") || len(seen) != 5 {
					t.Fatalf("exhausted attempts: listener=%v error=%v attempts=%d", listener, err, len(seen))
				}
			} else if err != nil || listener != want || len(seen) != succeedAt {
				t.Fatalf("successful attempt: listener=%v error=%v attempts=%d", listener, err, len(seen))
			}
		})
	}
}

func TestListenExplicitAddress(t *testing.T) {
	for _, bindErr := range []error{nil, errors.New("address in use")} {
		calls := 0
		want := &net.TCPListener{}
		listener, err := listen("[::1]:8080", func(network, address string) (net.Listener, error) {
			calls++
			if network != "tcp" || address != "[::1]:8080" {
				t.Fatalf("explicit address changed: %s %s", network, address)
			}
			if bindErr != nil {
				return nil, bindErr
			}
			return want, nil
		})
		if calls != 1 || !errors.Is(err, bindErr) || (bindErr == nil && listener != want) || (bindErr != nil && listener != nil) {
			t.Fatalf("explicit listener: listener=%v error=%v attempts=%d", listener, err, calls)
		}
	}
}

func TestCLIDocumentLimitFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte(`{"a":1,"b":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := application([]string{"-max-file-size-mb", "20", "-max-nodes", "5", path}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := application([]string{"-max-nodes", "4", path}); err == nil || !strings.Contains(err.Error(), "4") {
		t.Fatalf("configured startup limit ignored: %v", err)
	}
	for _, args := range [][]string{{"-max-file-size-mb", "0"}, {"-max-file-size-mb", "-1"}, {"-max-nodes", "0"}, {"-max-nodes", "-1"}} {
		if _, _, err := application(args); err == nil {
			t.Fatalf("accepted invalid limits: %v", args)
		}
	}
	if err := os.WriteFile(path, []byte(`"`+strings.Repeat("x", 1024*1024)+`"`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := application([]string{"-max-file-size-mb", "1", path}); err == nil || !strings.Contains(err.Error(), "1048576") {
		t.Fatalf("configured byte limit ignored: %v", err)
	}
	if _, _, err := application([]string{"-max-file-size-mb", "2", path}); err != nil {
		t.Fatal(err)
	}
}
