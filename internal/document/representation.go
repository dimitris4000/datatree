package document

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// RepresentationContext retains at most maxBytes of serialized output while
// counting the complete representation. Zero means an explicit full result.
// Returned text always ends at a UTF-8 boundary. Native JSON normalizes CRLF;
// native YAML preserves standalone-subtree indentation semantics from YAML.
func (d *Document) RepresentationContext(ctx context.Context, id, format string, maxBytes int) (string, int, error) {
	if d.compact != nil {
		return d.compactRepresentation(ctx, id, format, maxBytes)
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	n := d.Nodes[id]
	if n == nil {
		return "", 0, errors.New("node not found")
	}
	out := &representationBuffer{ctx: ctx, limit: maxBytes}
	var err error
	switch format {
	case "native":
		switch d.Format {
		case "yaml":
			err = d.writeYAMLRepresentation(ctx, n, out)
		case "json":
			start, end := n.Start, n.End
			if n == d.Root {
				start, end = 0, len(d.Source)
			}
			raw := d.Source[start:end]
			for len(raw) > 0 {
				i := bytes.Index(raw, []byte("\r\n"))
				if i < 0 {
					_, err = out.Write(raw)
					break
				}
				if _, err = out.Write(raw[:i]); err != nil {
					break
				}
				if _, err = out.Write([]byte("\n")); err != nil {
					break
				}
				raw = raw[i+2:]
			}
		default:
			err = jsonWrite(ctx, n.raw, false, d.Limits(), &prettyJSONSink{out: out})
		}
	case "json":
		err = jsonWrite(ctx, n.raw, false, d.Limits(), &prettyJSONSink{out: out})
	case "yaml":
		err = d.writeYAMLRepresentation(ctx, n, out)
	default:
		return "", 0, errors.New("choose native, json or yaml representation")
	}
	if err == nil {
		err = out.err
	}
	if err != nil {
		return "", 0, err
	}
	raw := out.buffer.Bytes()
	for len(raw) > 0 && !utf8.Valid(raw) {
		raw = raw[:len(raw)-1]
	}
	return string(raw), out.total, nil
}

type representationBuffer struct {
	ctx          context.Context
	limit, total int
	buffer       bytes.Buffer
	err          error
}

func (w *representationBuffer) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.err = w.ctx.Err(); w.err != nil {
		return 0, w.err
	}
	w.total += len(p)
	keep := len(p)
	if w.limit > 0 {
		keep = min(keep, max(0, w.limit-w.buffer.Len()))
	}
	if keep > 0 {
		w.buffer.Write(p[:keep])
	}
	return len(p), nil
}

func (d *Document) writeYAMLRepresentation(ctx context.Context, n *Node, out *representationBuffer) error {
	if d.Format == "yaml" && n.End > n.Start {
		raw := d.Source[n.Start:n.End]
		prefix := []byte(strings.Repeat(" ", max(0, fragmentIndent(n))))
		for {
			i := bytes.IndexByte(raw, '\n')
			if i < 0 {
				_, err := out.Write(raw)
				return err
			}
			if _, err := out.Write(raw[:i+1]); err != nil {
				return err
			}
			raw = raw[i+1:]
			if bytes.HasPrefix(raw, prefix) {
				raw = raw[len(prefix):]
			}
		}
	}
	root := n.raw
	if d.Format != "yaml" {
		table := map[*yaml.Node]*yaml.Node{}
		root = clone(root, table)
		for _, node := range table {
			if err := ctx.Err(); err != nil {
				return err
			}
			node.Style = 0
		}
	}
	encoder := yaml.NewEncoder(out)
	encoder.SetIndent(2)
	if root.Kind == yaml.DocumentNode {
		for _, child := range root.Content {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := encoder.Encode(child); err != nil {
				return err
			}
		}
	} else if err := encoder.Encode(root); err != nil {
		return err
	}
	return encoder.Close()
}

// prettyJSONSink applies json.Indent's two-space layout to compact JSON as it
// streams, without retaining the complete compact or formatted representation.
type prettyJSONSink struct {
	out                      *representationBuffer
	depth, rawBytes          int
	quoted, escaped, pending bool
}

func (w *prettyJSONSink) Len() int {
	// The preview's retained-output cap bounds allocation independently of
	// discarded conversion bytes (notably JSON representations of CSV).
	if w.out.limit > 0 {
		return min(w.rawBytes, w.out.limit)
	}
	return w.rawBytes
}
func (w *prettyJSONSink) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *prettyJSONSink) WriteByte(c byte) error            { _, err := w.Write([]byte{c}); return err }
func (w *prettyJSONSink) newline() {
	w.out.Write([]byte("\n"))
	w.out.Write([]byte(strings.Repeat("  ", w.depth)))
}
func (w *prettyJSONSink) Write(p []byte) (int, error) {
	w.rawBytes += len(p)
	for i := 0; i < len(p); {
		if w.quoted {
			start := i
			for i < len(p) {
				c := p[i]
				i++
				if w.escaped {
					w.escaped = false
				} else if c == '\\' {
					w.escaped = true
				} else if c == '"' {
					w.quoted = false
					break
				}
			}
			w.out.Write(p[start:i])
			continue
		}
		c := p[i]
		i++
		if w.pending && c != '}' && c != ']' {
			w.newline()
			w.pending = false
		}
		switch c {
		case '"':
			w.quoted = true
			w.out.Write([]byte{c})
		case '{', '[':
			w.out.Write([]byte{c})
			w.depth++
			w.pending = true
		case '}', ']':
			w.depth--
			if !w.pending {
				w.newline()
			}
			w.pending = false
			w.out.Write([]byte{c})
		case ',':
			w.out.Write([]byte{c})
			w.newline()
		case ':':
			w.out.Write([]byte(": "))
		default:
			start := i - 1
			for i < len(p) && !strings.ContainsRune("{}[],:\"", rune(p[i])) {
				i++
			}
			w.out.Write(p[start:i])
		}
	}
	return len(p), w.out.err
}
