// Package document implements ordered documents, source spans and atomic edits.
package document

import (
	"bytes"
	"context"
	"datatree/internal/compact"
	"datatree/internal/textstore"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const MaxBytes = 20 * 1024 * 1024
const MaxNodes = 1000000
const MaxDepth = 256
const compactThreshold = 1024 * 1024

type Node struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Kind     string  `json:"kind"`
	Value    string  `json:"value,omitempty"`
	Path     string  `json:"path,omitempty"`
	Location string  `json:"location,omitempty"`
	Target   string  `json:"target,omitempty"`
	Children []*Node `json:"children,omitempty"`
	Start    int     `json:"-"`
	End      int     `json:"-"`
	raw      *yaml.Node
	parent   *Node
	key      *yaml.Node
	keyStart int
}
type Document struct {
	compact             *compact.Tree
	compactComplexKeys  bool
	compactYAMLMetadata bool
	localCost           int
	Format              string
	Source              []byte
	Root                *Node
	Nodes               map[string]*Node
	raw                 *yaml.Node
	headers             []string
	limits              Limits
	sourceIndexOnce     sync.Once
	sourceIndex         atomic.Pointer[sourceIndex]
	incremental         bool
	changedNodes        []string
}

func Parse(format string, source []byte) (*Document, error) {
	return ParseWithLimits(format, source, DefaultLimits())
}

func ParseWithLimits(format string, source []byte, limits Limits) (*Document, error) {
	return ParseWithLimitsContext(context.Background(), format, source, limits)
}

// ParseWithLimitsContext checks cancellation during input reads and tree construction.
func ParseWithLimitsContext(ctx context.Context, format string, source []byte, limits Limits) (*Document, error) {
	return parseWithStorageContext(ctx, format, source, limits, len(source) > compactThreshold)
}
func parseWithStorageContext(ctx context.Context, format string, source []byte, limits Limits, compactMode bool) (*Document, error) {
	d, err := parseDocumentContext(ctx, format, source, limits, compactMode)
	if err != nil {
		return nil, err
	}
	if compactMode && d.compact == nil {
		return d.compactify(ctx)
	}
	return d, nil
}
func parseLegacyWithLimitsContext(ctx context.Context, format string, source []byte, limits Limits) (*Document, error) {
	return parseDocumentContext(ctx, format, source, limits, false)
}
func parseDocumentContext(ctx context.Context, format string, source []byte, limits Limits, compactMode bool) (*Document, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits = limits.Normalized()
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if len(source) > limits.MaxBytes {
		return nil, fmt.Errorf("file exceeds configured size limit (%d bytes)", limits.MaxBytes)
	}
	if !utf8.Valid(source) {
		return nil, errors.New("input must be UTF-8")
	}
	format = strings.ToLower(format)
	if format == "yml" {
		format = "yaml"
	}
	if compactMode && format == "csv" {
		return parseCompactCSV(ctx, source, limits)
	}
	d := &Document{Format: format, Source: bytes.Clone(source), Nodes: map[string]*Node{}, limits: limits}
	var root *yaml.Node
	jsonSpans := map[*yaml.Node]byteSpan{}
	switch format {
	case "json":
		var err error
		root, err = parseJSON(ctx, source, limits, jsonSpans, 0)
		if err != nil {
			return nil, err
		}
	case "yaml":
		dec := yaml.NewDecoder(contextReader{ctx, bytes.NewReader(source)})
		var docs []*yaml.Node
		for {
			var n yaml.Node
			err := dec.Decode(&n)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if len(n.Content) > 0 {
				docs = append(docs, n.Content[0])
			}
		}
		if len(docs) == 0 {
			root = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null", Line: 1, Column: 1}
		} else if len(docs) == 1 {
			root = docs[0]
		} else {
			root = &yaml.Node{Kind: yaml.DocumentNode, Content: docs, Line: 1, Column: 1}
		}
	case "jsonl":
		root = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Line: 1, Column: 1}
		lineOffset := 0
		for i, line := range bytes.Split(source, []byte("\n")) {
			base := lineOffset
			lineOffset += len(line) + 1
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			n, err := parseJSON(ctx, line, limits, jsonSpans, base)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", i+1, err)
			}
			shiftLines(n, i)
			root.Content = append(root.Content, n)
		}
	case "csv":
		r := csv.NewReader(contextReader{ctx, bytes.NewReader(source)})
		headers, err := r.Read()
		if err != nil {
			return nil, fmt.Errorf("CSV header: %w", err)
		}
		seen := map[string]bool{}
		for _, h := range headers {
			if strings.TrimSpace(h) == "" || seen[h] {
				return nil, errors.New("CSV headers must be nonempty and unique")
			}
			seen[h] = true
		}
		d.headers = headers
		root = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for {
			row, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			obj := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for i, v := range row {
				obj.Content = append(obj.Content, strNode(headers[i]), strNode(v))
			}
			root.Content = append(root.Content, obj)
		}
	default:
		return nil, errors.New("format must be json, jsonl, yaml or csv")
	}
	if err := validateRawContext(ctx, root, limits); err != nil {
		return nil, err
	}
	d.raw = root
	var starts []int
	var positions map[int][]int
	if format != "json" && format != "jsonl" {
		starts = lineStarts(source)
		positions = columnOffsets(source, starts)
	}
	count := 0
	var builder *compact.Builder
	var rawIDs map[*yaml.Node]uint32
	complexKeys, hasYAMLMetadata := false, false
	if compactMode {
		var err error
		builder, err = compact.NewBuilder(ctx, textstore.FromImmutable(d.Source), limits.MaxNodes, MaxDepth+1)
		if err != nil {
			return nil, err
		}
		if format == "yaml" {
			rawIDs = make(map[*yaml.Node]uint32)
			var nextID uint32
			var anchors func(*yaml.Node) error
			anchors = func(n *yaml.Node) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				nextID++
				if n.Anchor != "" {
					rawIDs[n] = nextID
				}
				for i, child := range n.Content {
					if n.Kind != yaml.MappingNode || i%2 == 1 {
						if err := anchors(child); err != nil {
							return err
						}
					}
				}
				return nil
			}
			if err := anchors(root); err != nil {
				return nil, err
			}
		}
	}
	flowEnds := map[int]int{}
	keyFingerprints := map[scalarKeyFingerprint]string{}
	var build func(*yaml.Node, *Node, *yaml.Node, string, string, int) (*Node, error)
	build = func(raw *yaml.Node, parent *Node, key *yaml.Node, label, path string, depth int) (*Node, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count++
		if count > limits.MaxNodes {
			return nil, fmt.Errorf("document exceeds configured node limit (%d)", limits.MaxNodes)
		}
		if depth > MaxDepth {
			return nil, fmt.Errorf("document exceeds depth %d", MaxDepth)
		}
		n := &Node{ID: strconv.Itoa(count), Label: label, Path: path, raw: raw, parent: parent, key: key}
		var children []uint32
		if span, ok := jsonSpans[raw]; ok {
			n.Start, n.End = span.start, span.end
		} else {
			n.Start = offset(source, starts, positions, raw.Line, raw.Column)
			if raw.Kind == yaml.MappingNode || raw.Kind == yaml.SequenceNode || raw.Kind == yaml.DocumentNode {
				n.End = n.Start
				if raw.Style&yaml.FlowStyle != 0 {
					n.End = flowEndIndexed(source, n.Start, flowEnds)
				}
			} else {
				n.End = scalarEnd(source, n.Start, raw)
			}
		}
		if key != nil {
			if span, ok := jsonSpans[key]; ok {
				n.keyStart = span.start
			} else {
				n.keyStart = offset(source, starts, positions, key.Line, key.Column)
			}
		}
		if raw.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			n.End = blockEnd(source, skipProperties(source, n.Start), contextIndent(n))
		}
		if format == "yaml" && raw.Kind == yaml.ScalarNode && raw.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) == 0 && !inFlow(parent) {
			n.End = plainYAMLEnd(source, n.Start, key, parent)
		}
		if !compactMode {
			n.Location = fmt.Sprintf("line %d, column %d", raw.Line, raw.Column)
		}
		switch raw.Kind {
		case yaml.MappingNode:
			n.Kind = "object"
			var keys map[string]bool
			if format != "json" && format != "jsonl" {
				keys = map[string]bool{}
			}
			for i := 0; i < len(raw.Content); i += 2 {
				k, v := raw.Content[i], raw.Content[i+1]
				lab := k.Value
				if k.Kind != yaml.ScalarNode {
					lab = fmt.Sprintf("[complex key %d]", i/2)
				}
				if keys != nil {
					fingerprint := mappingKeyFingerprint(k, keyFingerprints)
					if keys[fingerprint] {
						return nil, fmt.Errorf("duplicate mapping key %q", lab)
					}
					keys[fingerprint] = true
				}

				p := ""
				if path != "" && k.Kind == yaml.ScalarNode && k.Tag == "!!str" {
					p = "$"
					if !compactMode {
						p = path + pathKey(lab)
					}
				}
				child, err := build(v, n, k, lab, p, depth+1)
				if err != nil {
					return nil, err
				}
				if compactMode {
					children = append(children, compactID(child.ID))
					n.End = max(n.End, child.End)
					raw.Content[i], raw.Content[i+1] = nil, nil
				} else {
					n.Children = append(n.Children, child)
				}
			}
		case yaml.SequenceNode, yaml.DocumentNode:
			n.Kind = "array"
			if raw.Kind == yaml.DocumentNode {
				n.Kind = "stream"
			}
			for i, v := range raw.Content {
				p := ""
				if path != "" {
					p = "$"
					if !compactMode {
						p = fmt.Sprintf("%s[%d]", path, i)
					}
				}
				child, err := build(v, n, nil, fmt.Sprintf("[%d]", i), p, depth+1)
				if err != nil {
					return nil, err
				}
				if compactMode {
					children = append(children, compactID(child.ID))
					n.End = max(n.End, child.End)
					raw.Content[i] = nil
				} else {
					n.Children = append(n.Children, child)
				}
			}
		case yaml.AliasNode:
			n.Kind = "alias"
			n.Value = "*" + raw.Value
		default:
			n.Kind = scalarKind(raw)
			n.Value = raw.Value
		}
		if raw.Kind == yaml.MappingNode || raw.Kind == yaml.SequenceNode || raw.Kind == yaml.DocumentNode {
			if raw.Style&yaml.FlowStyle != 0 || format == "json" || format == "jsonl" {
				if span, ok := jsonSpans[raw]; ok {
					n.End = span.end
				} else {
					n.End = flowEndIndexed(source, n.Start, flowEnds)
				}
			} else {
				for _, c := range n.Children {
					if c.End > n.End {
						n.End = c.End
					}
				}
			}
		}
		if n.Start > n.End || n.End > len(source) {
			return nil, errors.New("invalid source span")
		}
		if compactMode {
			in := compact.Input{ID: compactID(n.ID), Kind: n.Kind, Label: n.Label, Value: n.Value, Start: n.Start, End: n.End, KeyStart: -1, Pathable: path != "", Weight: 1 + rawWeight(key), OwnDepth: rawDepth(key), Children: children}
			if parent != nil {
				in.Parent = compactID(parent.ID)
			}
			if key != nil {
				in.KeyStart = n.keyStart
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
					complexKeys = true
					in.HasMetadata = true
				}
			}
			if parent == nil && format == "jsonl" {
				in.Start, in.End, in.KeyStart = 0, len(source), -1
			}
			if format == "yaml" {
				in.HasMetadata = in.HasMetadata || yamlCompactMetadata(raw) || rawMetadata(key)
				in.NativeIndent = max(0, fragmentIndent(n))
				hasYAMLMetadata = hasYAMLMetadata || rawMetadata(raw) || rawMetadata(key)
				if raw.Alias != nil {
					in.Target = rawIDs[raw.Alias]
				}
			}
			if err := builder.Add(in); err != nil {
				return nil, err
			}
			// No compact record points back into the parser. Release consumed
			// nodes during traversal instead of keeping the whole graph alive.
			delete(jsonSpans, raw)
			delete(jsonSpans, key)
			raw.Content = nil
		} else {
			d.Nodes[n.ID] = n
		}
		return n, nil
	}
	path := "$"
	if root.Kind == yaml.DocumentNode {
		path = ""
	}
	var err error
	d.Root, err = build(root, nil, nil, "$", path, 0)
	if err != nil {
		return nil, err
	}
	if compactMode {
		rootID := compactID(d.Root.ID)
		// The builder now owns the final records. No parser references escape.
		d.raw, d.Root, root = nil, nil, nil
		jsonSpans, rawIDs = nil, nil
		positions, flowEnds, keyFingerprints = nil, nil, nil
		tree, err := builder.Finish(rootID)
		if err != nil {
			return nil, err
		}
		next := compactDocument(format, d.Source, d.headers, limits, tree, complexKeys)
		next.compactYAMLMetadata = hasYAMLMetadata
		return next, nil
	}
	if format == "yaml" {
		index := map[*yaml.Node]string{}
		for id, n := range d.Nodes {
			index[n.raw] = id
		}
		for _, n := range d.Nodes {
			if n.raw.Alias != nil {
				n.Target = index[n.raw.Alias]
			}
		}

	}

	return d, nil
}
func strNode(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
func scalarKind(n *yaml.Node) string {
	switch n.Tag {
	case "!!str":
		return "string"
	case "!!bool":
		return "boolean"
	case "!!null":
		return "null"
	case "!!int", "!!float":
		return "number"
	}
	return "tagged"
}
func shiftLines(n *yaml.Node, delta int) {
	n.Line += delta
	for _, c := range n.Content {
		shiftLines(c, delta)
	}
}

type byteSpan struct{ start, end int }

// parseJSON captures token spans during decoding. JSON never passes through YAML's
// parser: doing so duplicates work and imposes YAML semantics on JSON strings.
func lineStarts(b []byte) []int {
	r := []int{0}
	for i, c := range b {
		if c == '\n' {
			r = append(r, i+1)
		}
	}
	return r
}
func columnOffsets(b []byte, lines []int) map[int][]int {
	result := map[int][]int{}
	for i, start := range lines {
		end := len(b)
		if i+1 < len(lines) {
			end = lines[i+1]
		}
		ascii := true
		for _, c := range b[start:end] {
			if c >= 128 {
				ascii = false
				break
			}
		}
		if ascii {
			continue
		}
		var cols []int
		for pos := start; pos < end; {
			cols = append(cols, pos)
			_, size := utf8.DecodeRune(b[pos:])
			pos += size
		}
		cols = append(cols, end)
		result[i+1] = cols
	}
	return result
}
func offset(b []byte, lines []int, columns map[int][]int, line, col int) int {
	if line < 1 || line > len(lines) {
		return 0
	}
	if cols := columns[line]; cols != nil {
		if col > 0 && col <= len(cols) {
			return cols[col-1]
		}
		return cols[len(cols)-1]
	}
	p := lines[line-1] + col - 1
	if p > len(b) {
		return len(b)
	}
	return p
}

func validateRaw(root *yaml.Node, limits Limits) error {
	return validateRawContext(context.Background(), root, limits)
}
func validateRawContext(ctx context.Context, root *yaml.Node, limits Limits) error {
	count := 0
	var visit func(*yaml.Node, int) error
	visit = func(n *yaml.Node, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > limits.MaxNodes {
			return fmt.Errorf("document exceeds parsed node limit (%d; mapping keys also count)", limits.MaxNodes)
		}
		if depth > MaxDepth+1 {
			return errors.New("document exceeds depth 256")
		}
		for _, c := range n.Content {
			if err := visit(c, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(root, 0)
}
func inFlow(n *Node) bool {
	for ; n != nil; n = n.parent {
		if n.raw.Style&yaml.FlowStyle != 0 {
			return true
		}
	}
	return false
}

func pathKey(key string) string {
	var b strings.Builder
	b.WriteString("['")
	for _, r := range key {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 32 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteString("']")
	return b.String()
}

// contextReader bounds the interval between cancellation checks even when a
// decoder requests an entire large input buffer in one read.
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > 32*1024 {
		p = p[:32*1024]
	}
	return r.reader.Read(p)
}

// Repeated scalar keys (CSV headers and structured YAML rows) share exactly the
// same yaml.Marshal fingerprint. Include every serialized scalar property;
// complex keys retain the original marshal path and duplicate-key semantics.
type scalarKeyFingerprint struct {
	tag, value, anchor, head, line, foot string
	style                                yaml.Style
}

func mappingKeyFingerprint(k *yaml.Node, cache map[scalarKeyFingerprint]string) string {
	if k.Kind != yaml.ScalarNode {
		b, _ := yaml.Marshal(k)
		return string(b)
	}
	key := scalarKeyFingerprint{k.Tag, k.Value, k.Anchor, k.HeadComment, k.LineComment, k.FootComment, k.Style}
	if value, ok := cache[key]; ok {
		return value
	}
	b, _ := yaml.Marshal(k)
	value := string(b)
	if len(cache) < 4096 {
		cache[key] = value
	}
	return value
}
