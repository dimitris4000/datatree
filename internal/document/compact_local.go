package document

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"strings"

	"datatree/internal/compact"
	"go.yaml.in/yaml/v3"
)

func compactScalar(kind string) bool {
	return kind == "string" || kind == "number" || kind == "boolean" || kind == "null"
}
func (d *Document) compactScalarPatch(ctx context.Context, start, end int, text string) (*Document, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	id, ok := d.compact.LocateByte(start, end)
	if !ok {
		return nil, false, nil
	}
	r, ok := d.compact.Record(id)
	if !ok {
		return nil, false, nil
	}
	if d.Format == "json" || d.Format == "jsonl" {
		// A mapping key belongs to its enclosing value/container, never to its value
		// token. Grow only as far as needed to contain the complete source edit.
		for start < r.Start || end > r.End {
			if r.Parent == 0 {
				return nil, false, nil
			}
			var found bool
			r, found = d.compact.Record(r.Parent)
			if !found {
				return nil, false, nil
			}
		}
	} else if !compactScalar(r.Kind) || start < r.Start || end > r.End {
		return nil, false, nil
	}
	replacement := make([]byte, 0, start-r.Start+len(text)+r.End-end)
	replacement = append(replacement, d.Source[r.Start:start]...)
	replacement = append(replacement, text...)
	replacement = append(replacement, d.Source[end:r.End]...)
	next, handled, err := d.compactReplace(ctx, r, replacement)
	// A source edit may expand its syntactic context (for example into another
	// CSV row). Let the complete parser decide before rejecting that operation.
	if err != nil && ctx.Err() == nil {
		return nil, false, nil
	}
	return next, handled, err
}
func (d *Document) compactReplace(ctx context.Context, target compact.Record, replacement []byte) (*Document, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	var inputs []compact.Input
	var fragmentRoot uint32
	var complexKeys bool
	var fragmentMetadata bool
	switch d.Format {
	case "json", "jsonl":
		if d.Format == "jsonl" && (target.Parent == 0 || bytes.ContainsAny(replacement, "\r\n")) {
			return nil, false, nil
		}
		incoming, err := parseLegacyWithLimitsContext(ctx, "json", replacement, d.Limits())
		if err != nil {
			return nil, true, err
		}
		inputs, complexKeys, fragmentMetadata, err = incoming.compactInputs(ctx)
		if err != nil {
			return nil, true, err
		}
		fragmentRoot = compactID(incoming.Root.ID)
		inputs[0].Start, inputs[0].End = incoming.Root.Start, incoming.Root.End
	case "yaml":
		if !compactScalar(target.Kind) || bytes.ContainsAny(replacement, "\r\n") || bytes.ContainsAny(d.Source[target.Start:target.End], "\r\n") || len(bytes.TrimSpace(replacement)) == 0 {
			return nil, false, nil
		}
		for r := target; ; {
			if r.HasMetadata {
				return nil, false, nil
			}
			if r.Parent == 0 {
				break
			}
			var ok bool
			r, ok = d.compact.Record(r.Parent)
			if !ok {
				return nil, true, errors.New("compact parent missing")
			}
		}
		incoming, err := parseLegacyWithLimitsContext(ctx, "yaml", replacement, d.Limits())
		if err != nil {
			return nil, true, err
		}
		if !compactScalar(incoming.Root.Kind) || rawMetadata(incoming.Root.raw) || incoming.Root.Start != 0 || incoming.Root.End != len(replacement) {
			return nil, false, nil
		}
		inputs, complexKeys, fragmentMetadata, err = incoming.compactInputs(ctx)
		if err != nil {
			return nil, true, err
		}
		fragmentRoot = compactID(incoming.Root.ID)
		inputs[0].NativeIndent = target.NativeIndent
	case "csv":
		if len(replacement) == 0 {
			parent, ok := d.compact.Record(target.Parent)
			if ok && parent.ChildCount == 1 {
				return nil, false, nil
			}
		}
		if target.Kind != "string" || target.Parent == 0 {
			return nil, false, nil
		}
		r := csv.NewReader(bytes.NewReader(replacement))
		r.FieldsPerRecord = 1
		// An empty field is a value even though encoding/csv treats an empty input as EOF.
		value := ""
		if len(replacement) > 0 {
			row, err := r.Read()
			if err != nil {
				return nil, true, err
			}
			if len(row) != 1 {
				return nil, true, errors.New("CSV cell patch must remain one field")
			}
			value = row[0]
			if _, err = r.Read(); err != io.EOF {
				return nil, true, errors.New("CSV cell patch must remain one record")
			}
			if replacement[0] == '"' {
				if replacement[len(replacement)-1] != '"' {
					return nil, false, nil
				}
			} else if bytes.ContainsAny(replacement, "\r\n") {
				return nil, false, nil
			}
		}
		inputs = []compact.Input{{ID: 1, Kind: "string", Value: value, Start: 0, KeyStart: -1, End: len(replacement), Pathable: true, Weight: 1}}
		fragmentRoot = 1
	default:
		return nil, false, nil
	}
	// Raw mapping keys may be one level deeper than the application tree,
	// matching validateRawContext. Enforce the separate public value-depth bound.
	targetDepth := 0
	for parent := target.Parent; parent != 0; {
		r, ok := d.compact.Record(parent)
		if !ok {
			return nil, true, errors.New("compact parent missing")
		}
		targetDepth++
		parent = r.Parent
	}
	depths := make(map[uint32]int, len(inputs))
	for _, in := range inputs {
		depth := 0
		if in.ID != fragmentRoot {
			depth = depths[in.Parent] + 1
		}
		depths[in.ID] = depth
		if targetDepth+depth > MaxDepth {
			return nil, true, errors.New("document exceeds depth 256")
		}
	}
	tree, change, err := d.compact.Replace(ctx, target.ID, replacement, inputs, fragmentRoot)
	if err != nil {
		return nil, true, err
	}
	if tree.Text().ByteLen() > d.Limits().MaxBytes {
		return nil, true, errors.New("patched source exceeds configured size limit")
	}
	source := tree.Text().Bytes()
	next := compactDocument(d.Format, source, d.headers, d.limits, tree, d.compactComplexKeys || complexKeys)
	next.compactYAMLMetadata = d.compactYAMLMetadata || fragmentMetadata
	next.localCost = target.End - target.Start + len(replacement)
	if change.StableGeometry && compactScalar(target.Kind) && len(inputs) == 1 && compactScalar(inputs[0].Kind) {
		next.incremental = true
		for _, id := range change.Affected {
			next.changedNodes = append(next.changedNodes, publicID(id))
		}
	}
	return next, true, nil
}
func (d *Document) compactEdit(ctx context.Context, a Action) (*Document, bool, error) {
	if a.Op != "replace" {
		return d.compactStructuralEdit(ctx, a)
	}
	target, ok := d.compact.Record(compactID(a.Node))
	if !ok {
		return nil, true, errors.New("selected node no longer exists")
	}
	format := a.Format
	if format == "" {
		format = "json"
	}
	replacement := []byte(a.Text)
	if d.Format == "csv" || d.Format == "yaml" && format != "yaml" || (d.Format == "json" || d.Format == "jsonl") && format != "json" || d.Format == "jsonl" {
		incoming, err := parseLegacyWithLimitsContext(ctx, format, []byte(a.Text), d.Limits())
		if err != nil {
			return nil, true, err
		}
		switch d.Format {
		case "csv":
			if incoming.Root.Kind != "string" {
				return nil, true, errors.New("CSV cells must remain strings")
			}
			var b bytes.Buffer
			w := csv.NewWriter(&b)
			if err := w.Write([]string{incoming.Root.Value}); err != nil {
				return nil, true, err
			}
			w.Flush()
			if err := w.Error(); err != nil {
				return nil, true, err
			}
			replacement = bytes.TrimSuffix(b.Bytes(), []byte("\n"))
			if incoming.Root.Value == "" {
				parent, ok := d.compact.Record(target.Parent)
				if ok && parent.ChildCount == 1 {
					replacement = []byte(`""`)
				}
			}
		case "yaml":
			if !compactScalar(target.Kind) || !compactScalar(incoming.Root.Kind) {
				return nil, false, nil
			}
			encoded, err := marshalYAML(incoming.raw)
			if err != nil {
				return nil, true, err
			}
			replacement = []byte(strings.TrimSuffix(string(encoded), "\n"))
		default:
			encoded, err := jsonBytes(incoming.raw, false, d.Limits())
			if err != nil {
				return nil, true, err
			}
			replacement = encoded
		}
	}
	return d.compactReplace(ctx, target, replacement)
}

// Keep the metadata/flow marker conservative for local YAML parsing. Explicit
// conversions can materialize and validate features requiring raw YAML nodes.
func yamlCompactMetadata(n *yaml.Node) bool {
	return rawMetadata(n) || n != nil && n.Style&yaml.FlowStyle != 0
}

func (d *Document) compactStructuralEdit(ctx context.Context, a Action) (*Document, bool, error) {
	if d.Format != "json" && d.Format != "jsonl" {
		return nil, false, nil
	}
	switch a.Op {
	case "add", "rename", "clone", "delete":
	default:
		return nil, false, nil
	}
	target, ok := d.compact.Record(compactID(a.Node))
	if !ok {
		return nil, true, errors.New("selected node no longer exists")
	}
	scope := target
	if a.Op != "add" {
		if target.Parent == 0 {
			return nil, true, errors.New("cannot delete, rename, or duplicate the document root")
		}
		scope, ok = d.compact.Record(target.Parent)
		if !ok {
			return nil, true, errors.New("compact parent missing")
		}
	}
	// The synthetic JSONL root does not have one standalone JSON value.
	if d.Format == "jsonl" && scope.Parent == 0 {
		return nil, false, nil
	}
	fragment, err := parseLegacyWithLimitsContext(ctx, "json", d.Source[scope.Start:scope.End], d.Limits())
	if err != nil {
		return nil, true, err
	}
	local := a
	local.Node = fragment.Root.ID
	if a.Op != "add" {
		if target.Index < 0 || target.Index >= len(fragment.Root.Children) {
			return nil, true, errors.New("compact child position changed")
		}
		local.Node = fragment.Root.Children[target.Index].ID
	}
	edited, err := fragment.EditContext(ctx, local)
	if err != nil {
		return nil, true, err
	}
	replacement := edited.Source
	if d.Format == "jsonl" {
		if edited.compact != nil {
			edited, err = edited.materialize(ctx)
			if err != nil {
				return nil, true, err
			}
		}
		replacement, err = jsonBytes(edited.raw, false, d.Limits())
		if err != nil {
			return nil, true, err
		}
	}
	return d.compactReplace(ctx, scope, replacement)
}
