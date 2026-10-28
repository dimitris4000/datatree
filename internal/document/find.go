package document

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const MaxFindResults = 200
const MaxFindText = 4096

type FindOptions struct {
	Cursor                      string
	Limit                       int
	Mode                        string
	Text                        string
	Keys, Values, CaseSensitive bool
}
type FindMatch struct {
	ID       string `json:"id"`
	Path     string `json:"path,omitempty"`
	Location string `json:"location"`
	Kind     string `json:"kind"`
	Preview  string `json:"preview"`
}
type FindResult struct {
	NextCursor string      `json:"nextCursor,omitempty"`
	Matches    []FindMatch `json:"matches"`
	Truncated  bool        `json:"truncated"`
}

// Find reads the applied ordered AST; aliases are leaves, never traversed.
func (d *Document) Find(o FindOptions) (FindResult, error) {
	return d.FindContext(context.Background(), o)
}

// FindContext returns a bounded traversal page and honors cancellation.
func (d *Document) FindContext(ctx context.Context, o FindOptions) (FindResult, error) {
	if d.compact != nil {
		return d.compactFind(ctx, o)
	}
	result := FindResult{Matches: []FindMatch{}}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	limit := o.Limit
	if limit <= 0 || limit > MaxFindResults {
		limit = MaxFindResults
	}
	cursor := 0
	if o.Cursor != "" {
		var err error
		cursor, err = strconv.Atoi(o.Cursor)
		if err != nil || cursor < 0 || cursor > len(d.Nodes) {
			return result, errors.New("invalid find cursor")
		}
	}
	if len(o.Text) > MaxFindText || !utf8.ValidString(o.Text) {
		return result, errors.New("find text must be UTF-8 and at most 4096 bytes")
	}
	var match func(*Node) bool
	switch o.Mode {
	case "search":
		if o.Text == "" {
			return result, nil
		}
		needle := o.Text
		if !o.CaseSensitive {
			needle = strings.ToLower(needle)
		}
		contains := func(s string) bool {
			if !o.CaseSensitive {
				s = strings.ToLower(s)
			}
			return strings.Contains(s, needle)
		}
		match = func(n *Node) bool {
			return (o.Keys && keyContains(n.key, contains)) ||
				(o.Values && n.Kind != "object" && n.Kind != "array" && n.Kind != "stream" && contains(n.Value))
		}
	case "query":
		ids, err := d.queryContext(ctx, o.Text)
		if err != nil {
			return result, err
		}
		match = func(n *Node) bool { return ids[n.ID] }
	default:
		return result, errors.New("choose search or query mode")
	}
	position := 0
	var cancelled error
	var visit func(*Node) bool
	visit = func(n *Node) bool {
		if err := ctx.Err(); err != nil {
			cancelled = err
			return false
		}
		current := position
		position++
		if current >= cursor && match(n) {
			if len(result.Matches) == limit {
				result.Truncated = true
				result.NextCursor = strconv.Itoa(current)
				return false
			}
			preview := n.Value
			if n.Kind == "object" || n.Kind == "array" || n.Kind == "stream" {
				preview = n.Label
			}
			result.Matches = append(result.Matches, FindMatch{n.ID, n.Path, n.Location, n.Kind, shortPreview(preview)})
		}
		for _, c := range n.Children {
			if !visit(c) {
				return false
			}
		}
		return true
	}
	visit(d.Root)
	return result, cancelled
}
func shortPreview(s string) string {
	count := 0
	for i := range s {
		if count == 160 {
			return s[:i] + "…"
		}
		count++
	}
	return s
}

// Complex YAML keys have synthetic tree labels; search their actual scalar parts.
func keyContains(key *yaml.Node, contains func(string) bool) bool {
	if key == nil {
		return false
	}
	if key.Kind == yaml.ScalarNode {
		return contains(key.Value)
	}
	if key.Kind == yaml.AliasNode {
		return contains("*" + key.Value)
	}
	for _, part := range key.Content {
		if keyContains(part, contains) {
			return true
		}
	}
	return false
}
