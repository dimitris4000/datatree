package document

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"datatree/internal/compact"
	"go.yaml.in/yaml/v3"
)

var errCompactMetadata = errors.New("compact representation requires YAML materialization")

func (d *Document) compactRepresentation(ctx context.Context, id, format string, maxBytes int) (string, int, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	r, ok := d.compact.Record(compactID(id))
	if !ok {
		return "", 0, errors.New("node not found")
	}
	out := &representationBuffer{ctx: ctx, limit: maxBytes}
	var err error
	switch {
	case format == "native" && d.Format == "json":
		raw := d.Source[r.Start:r.End]
		if r.ID == d.compact.Root() {
			raw = d.Source
		}
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
	case (format == "native" || format == "yaml") && d.Format == "yaml" && r.End > r.Start:
		raw := d.Source[r.Start:r.End]
		prefix := bytes.Repeat([]byte(" "), r.NativeIndent)
		for {
			i := bytes.IndexByte(raw, '\n')
			if i < 0 {
				_, err = out.Write(raw)
				break
			}
			if _, err = out.Write(raw[:i+1]); err != nil {
				break
			}
			raw = raw[i+1:]
			raw = bytes.TrimPrefix(raw, prefix)
		}
	case format == "json" || format == "native" && (d.Format == "csv" || d.Format == "jsonl"):
		if d.compactComplexKeys || d.Root.Kind == "stream" {
			legacy, e := d.materialize(ctx)
			if e != nil {
				return "", 0, e
			}
			return legacy.RepresentationContext(ctx, id, format, maxBytes)
		}
		sink := &prettyJSONSink{out: out}
		err = d.compactWriteJSON(ctx, r, sink, 0)
	case format == "yaml" || format == "native":
		legacy, e := d.materialize(ctx)
		if e != nil {
			return "", 0, e
		}
		return legacy.RepresentationContext(ctx, id, format, maxBytes)
	default:
		return "", 0, errors.New("choose native, json or yaml representation")
	}
	if errors.Is(err, errCompactMetadata) {
		legacy, e := d.materialize(ctx)
		if e != nil {
			return "", 0, e
		}
		return legacy.RepresentationContext(ctx, id, format, maxBytes)
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
func (d *Document) compactWriteJSON(ctx context.Context, r compact.Record, out *prettyJSONSink, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > MaxDepth || out.Len() > d.Limits().MaxBytes {
		return errors.New("conversion exceeds document limits")
	}
	if r.HasMetadata && d.compactYAMLMetadata || r.Kind == "alias" || r.Kind == "tagged" {
		return errCompactMetadata
	}
	switch r.Kind {
	case "object", "array", "stream":
		open, close := byte('['), byte(']')
		if r.Kind == "object" {
			open, close = '{', '}'
		}
		if err := out.WriteByte(open); err != nil {
			return err
		}
		for offset := 0; offset < r.ChildCount; offset += 128 {
			ids := d.compact.Children(r.ID, offset, 128)
			for j, id := range ids {
				if offset+j > 0 {
					if err := out.WriteByte(','); err != nil {
						return err
					}
				}
				child, ok := d.compact.Record(id)
				if !ok {
					return errors.New("compact child missing")
				}
				if r.Kind == "object" {
					if !child.Pathable {
						return errors.New("non-string YAML keys cannot be converted safely")
					}
					key, _ := json.Marshal(child.Label)
					if _, err := out.Write(key); err != nil {
						return err
					}
					if err := out.WriteByte(':'); err != nil {
						return err
					}
				}
				if err := d.compactWriteJSON(ctx, child, out, depth+1); err != nil {
					return err
				}
			}
		}
		return out.WriteByte(close)
	default:
		if r.Kind == "number" && (d.Format == "json" || d.Format == "jsonl") {
			_, err := out.Write([]byte(r.Value))
			return err
		}
		tag := "!!str"
		switch r.Kind {
		case "null":
			tag = "!!null"
		case "boolean":
			tag = "!!bool"
		case "number":
			tag = "!!int"
			v := strings.ToLower(strings.TrimLeft(r.Value, "+-"))
			if !strings.HasPrefix(v, "0x") && !strings.HasPrefix(v, "0o") && !strings.HasPrefix(v, "0b") && strings.ContainsAny(v, ".e") {
				tag = "!!float"
			}
		}
		raw, err := jsonBytes(&yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: r.Value}, false, d.Limits())
		if err != nil {
			return err
		}
		_, err = out.Write(raw)
		return err
	}
}
