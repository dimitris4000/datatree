package perftest

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestFixturesAreValidAndBounded(t *testing.T) {
	for _, format := range []string{"json", "jsonl", "yaml", "csv"} {
		t.Run(format, func(t *testing.T) {
			var b bytes.Buffer
			if err := WriteFixture(&b, format, 32768); err != nil {
				t.Fatal(err)
			}
			if b.Len() > 32768 || b.Len() < 32000 {
				t.Fatalf("fixture size %d", b.Len())
			}
			switch format {
			case "json":
				if !json.Valid(b.Bytes()) {
					t.Fatal("invalid JSON")
				}
			case "jsonl":
				for _, line := range bytes.Split(bytes.TrimSpace(b.Bytes()), []byte("\n")) {
					if !json.Valid(line) {
						t.Fatal("invalid JSONL record")
					}
				}
			case "yaml":
				var v any
				if err := yaml.Unmarshal(b.Bytes(), &v); err != nil {
					t.Fatal(err)
				}
			case "csv":
				r := csv.NewReader(&b)
				count := 0
				for {
					_, err := r.Read()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					count++
				}
				if count < 2 {
					t.Fatal("no CSV data")
				}
			}
		})
	}
}

func TestFixtureRejectsUnsupportedFormat(t *testing.T) {
	if err := WriteFixture(io.Discard, "xml", 1024); err == nil {
		t.Fatal("accepted XML")
	}
}

func TestFixturePropagatesWriteErrors(t *testing.T) {
	if err := WriteFixture(failedWriter{}, "json", 1024); err == nil {
		t.Fatal("lost write error")
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
