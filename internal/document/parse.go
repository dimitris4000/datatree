// Package document implements ordered documents, source spans and atomic edits.
package document

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const MaxBytes = 20 * 1024 * 1024
const MaxNodes = 1000000
const MaxDepth = 256

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
}
type Document struct {
	Format  string
	Source  []byte
	Root    *Node
	Nodes   map[string]*Node
	raw     *yaml.Node
	headers []string
	limits  Limits
}

func Parse(format string, source []byte) (*Document, error) {
	return ParseWithLimits(format, source, DefaultLimits())
}

func ParseWithLimits(format string, source []byte, limits Limits) (*Document, error) {
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
	d := &Document{Format: format, Source: bytes.Clone(source), Nodes: map[string]*Node{}, limits: limits}
	var root *yaml.Node
	switch format {
	case "json":
		var err error
		root, err = parseJSON(source, limits)
		if err != nil {
			return nil, err
		}
	case "yaml":
		dec := yaml.NewDecoder(bytes.NewReader(source))
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
		for i, line := range bytes.Split(source, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			n, err := parseJSON(line, limits)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", i+1, err)
			}
			shiftLines(n, i)
			root.Content = append(root.Content, n)
		}
	case "csv":
		r := csv.NewReader(bytes.NewReader(source))
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
	if err := validateRaw(root, limits); err != nil {
		return nil, err
	}
	d.raw = root
	starts := lineStarts(source)
	positions := columnOffsets(source, starts)
	count := 0
	var build func(*yaml.Node, *Node, *yaml.Node, string, string, int) (*Node, error)
	build = func(raw *yaml.Node, parent *Node, key *yaml.Node, label, path string, depth int) (*Node, error) {
		count++
		if count > limits.MaxNodes {
			return nil, fmt.Errorf("document exceeds configured node limit (%d)", limits.MaxNodes)
		}
		if depth > MaxDepth {
			return nil, fmt.Errorf("document exceeds depth %d", MaxDepth)
		}
		n := &Node{ID: strconv.Itoa(count), Label: label, Path: path, raw: raw, parent: parent, key: key, Start: offset(source, starts, positions, raw.Line, raw.Column)}
		n.End = scalarEnd(source, n.Start, raw)
		if raw.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			n.End = blockEnd(source, skipProperties(source, n.Start), contextIndent(n))
		}
		if format == "yaml" && raw.Kind == yaml.ScalarNode && raw.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) == 0 && !inFlow(parent) {
			n.End = plainYAMLEnd(source, n.Start, key, parent)
		}
		n.Location = fmt.Sprintf("line %d, column %d", raw.Line, raw.Column)
		switch raw.Kind {
		case yaml.MappingNode:
			n.Kind = "object"
			keys := map[string]bool{}
			for i := 0; i < len(raw.Content); i += 2 {
				k, v := raw.Content[i], raw.Content[i+1]
				lab := k.Value
				if k.Kind != yaml.ScalarNode {
					lab = fmt.Sprintf("[complex key %d]", i/2)
				}
				fingerprint, _ := yaml.Marshal(k)
				if keys[string(fingerprint)] {
					return nil, fmt.Errorf("duplicate mapping key %q", lab)
				}
				keys[string(fingerprint)] = true
				p := ""
				if path != "" && k.Kind == yaml.ScalarNode && k.Tag == "!!str" {
					p = path + pathKey(lab)
				}
				child, err := build(v, n, k, lab, p, depth+1)
				if err != nil {
					return nil, err
				}
				n.Children = append(n.Children, child)
			}
		case yaml.SequenceNode, yaml.DocumentNode:
			n.Kind = "array"
			if raw.Kind == yaml.DocumentNode {
				n.Kind = "stream"
			}
			for i, v := range raw.Content {
				p := ""
				if path != "" {
					p = fmt.Sprintf("%s[%d]", path, i)
				}
				child, err := build(v, n, nil, fmt.Sprintf("[%d]", i), p, depth+1)
				if err != nil {
					return nil, err
				}
				n.Children = append(n.Children, child)
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
				n.End = flowEnd(source, n.Start)
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
		d.Nodes[n.ID] = n
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
	index := map[*yaml.Node]string{}
	for id, n := range d.Nodes {
		index[n.raw] = id
	}
	for _, n := range d.Nodes {
		if n.raw.Alias != nil {
			n.Target = index[n.raw.Alias]
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
func parseJSON(b []byte, limits Limits) (*yaml.Node, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	budget := 0
	var read func(int) (*yaml.Node, error)
	read = func(depth int) (*yaml.Node, error) {
		budget++
		if budget > limits.MaxNodes {
			return nil, fmt.Errorf("JSON exceeds parsed node limit (%d; object keys also count)", limits.MaxNodes)
		}
		if depth > MaxDepth {
			return nil, errors.New("JSON depth exceeds 256")
		}
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch v := t.(type) {
		case json.Delim:
			n := &yaml.Node{}
			if v == '{' {
				n.Kind = yaml.MappingNode
				n.Tag = "!!map"
				seen := map[string]bool{}
				for dec.More() {
					kt, err := dec.Token()
					if err != nil {
						return nil, err
					}
					budget++
					if budget > limits.MaxNodes {
						return nil, fmt.Errorf("JSON exceeds parsed node limit (%d; object keys also count)", limits.MaxNodes)
					}
					k, ok := kt.(string)
					if !ok {
						return nil, errors.New("invalid JSON key")
					}
					if seen[k] {
						return nil, fmt.Errorf("duplicate JSON key %q", k)
					}
					seen[k] = true
					value, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					n.Content = append(n.Content, strNode(k), value)
				}
			} else if v == '[' {
				n.Kind = yaml.SequenceNode
				n.Tag = "!!seq"
				for dec.More() {
					value, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					n.Content = append(n.Content, value)
				}
			} else {
				return nil, errors.New("unexpected JSON delimiter")
			}
			_, err = dec.Token()
			return n, err
		case string:
			return strNode(v), nil
		case json.Number:
			tag := "!!int"
			if strings.ContainsAny(string(v), ".eE") {
				tag = "!!float"
			}
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: string(v)}, nil
		case bool:
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}, nil
		case nil:
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
		}
		return nil, errors.New("invalid JSON value")
	}
	root, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err = dec.Token(); err != io.EOF {
		return nil, errors.New("trailing content after JSON")
	}
	// The JSON decoder establishes JSON semantics; YAML supplies locations only.
	var located yaml.Node
	if err = yaml.Unmarshal(b, &located); err != nil {
		return nil, err
	}
	var locate func(*yaml.Node, *yaml.Node)
	locate = func(n, l *yaml.Node) {
		n.Line = l.Line
		n.Column = l.Column
		n.Style = l.Style
		for i := range n.Content {
			if i < len(l.Content) {
				locate(n.Content[i], l.Content[i])
			}
		}
	}
	if len(located.Content) > 0 {
		locate(root, located.Content[0])
	}
	return root, nil
}
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
	count := 0
	var visit func(*yaml.Node, int) error
	visit = func(n *yaml.Node, depth int) error {
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
