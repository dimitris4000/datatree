package document

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"datatree/internal/compact"
)

func (d *Document) compactQuery(ctx context.Context, text string) (map[string]bool, error) {
	p := queryParser{text: strings.TrimSpace(text)}
	if !p.take("$") {
		return nil, p.syntax()
	}
	steps := []queryStep{}
	simple := true
	for p.pos < len(p.text) {
		s, err := p.step(true)
		if err != nil {
			return nil, err
		}
		steps = append(steps, s)
		if len(steps) > MaxDepth {
			return nil, errors.New("query exceeds 256 steps")
		}
		if s.kind != "key" && s.kind != "index" {
			simple = false
		}
	}
	if !simple {
		legacy, err := d.materialize(ctx)
		if err != nil {
			return nil, err
		}
		return legacy.queryContext(ctx, text)
	}
	if d.Root.Kind == "stream" {
		return nil, errors.New("path queries do not support YAML document streams; use Search")
	}
	work := &queryWork{remaining: queryWorkLimit, ctx: ctx}
	current := d.compact.Root()
	found := true
	for _, step := range steps {
		if err := work.spend(); err != nil {
			return nil, err
		}
		r, ok := d.compact.Record(current)
		if !ok {
			found = false
			break
		}
		next := uint32(0)
		if step.kind == "index" && r.Kind == "array" && step.index >= 0 {
			page := d.compact.Children(current, step.index, 1)
			if len(page) > 0 {
				next = page[0]
			}
		} else if step.kind == "key" && r.Kind == "object" {
			for offset := 0; offset < r.ChildCount && next == 0; offset += 128 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				for _, id := range d.compact.Children(current, offset, 128) {
					if err := work.spend(); err != nil {
						return nil, err
					}
					child, _ := d.compact.Record(id)
					if child.Pathable && child.Label == step.key {
						next = id
						break
					}
				}
			}
		}
		if next == 0 {
			found = false
			break
		}
		current = next
	}
	ids := map[string]bool{}
	if found {
		r, _ := d.compact.Record(current)
		if r.Pathable {
			ids[publicID(current)] = true
		}
	}
	return ids, nil
}
func (d *Document) compactFind(ctx context.Context, o FindOptions) (FindResult, error) {
	result := FindResult{Matches: []FindMatch{}}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(o.Text) > MaxFindText || !utf8.ValidString(o.Text) {
		return result, errors.New("find text must be UTF-8 and at most 4096 bytes")
	}
	limit := o.Limit
	if limit <= 0 || limit > MaxFindResults {
		limit = MaxFindResults
	}
	cursor := 0
	if o.Cursor != "" {
		var err error
		cursor, err = strconv.Atoi(o.Cursor)
		if err != nil || cursor < 0 || cursor > d.NodeCount() {
			return result, errors.New("invalid find cursor")
		}
	}
	var ids map[string]bool
	switch o.Mode {
	case "query":
		var err error
		ids, err = d.compactQuery(ctx, o.Text)
		if err != nil {
			return result, err
		}
	case "search":
		if o.Text == "" {
			return result, nil
		}
		if o.Keys && d.compactComplexKeys {
			legacy, err := d.materialize(ctx)
			if err != nil {
				return result, err
			}
			return legacy.FindContext(ctx, o)
		}
	default:
		return result, errors.New("choose search or query mode")
	}
	// A direct path has zero or one result. Its order and truncation are known
	// without visiting unrelated records. Nonzero cursors still use preorder.
	if o.Mode == "query" && len(ids) == 0 {
		return result, ctx.Err()
	}
	if o.Mode == "query" && len(ids) == 1 && cursor == 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		for id := range ids {
			n, ok := d.LookupNode(id)
			if ok {
				preview := n.Value
				if n.Kind == "object" || n.Kind == "array" || n.Kind == "stream" {
					preview = n.Label
				}
				result.Matches = append(result.Matches, FindMatch{n.ID, n.Path, n.Location, n.Kind, shortPreview(preview)})
			}
		}
		return result, ctx.Err()
	}
	needle := o.Text
	if !o.CaseSensitive {
		needle = strings.ToLower(needle)
	}
	contains := func(text string) bool {
		if !o.CaseSensitive {
			text = strings.ToLower(text)
		}
		return strings.Contains(text, needle)
	}
	position := 0
	err := d.compact.Preorder(ctx, func(r compact.Record) bool {
		current := position
		position++
		if current < cursor {
			return true
		}
		match := false
		if o.Mode == "query" {
			match = ids[publicID(r.ID)]
		} else {
			if o.Keys && r.Parent != 0 {
				parent, ok := d.compact.Record(r.Parent)
				if ok && parent.Kind == "object" {
					match = contains(r.Label)
				}
			}
			if o.Values && r.Kind != "object" && r.Kind != "array" && r.Kind != "stream" {
				match = match || contains(r.Value)
			}
		}
		if !match {
			return true
		}
		if len(result.Matches) == limit {
			result.Truncated = true
			result.NextCursor = strconv.Itoa(current)
			return false
		}
		n, ok := d.LookupNode(publicID(r.ID))
		if !ok {
			return false
		}
		preview := r.Value
		if r.Kind == "object" || r.Kind == "array" || r.Kind == "stream" {
			preview = r.Label
		}
		result.Matches = append(result.Matches, FindMatch{n.ID, n.Path, n.Location, n.Kind, shortPreview(preview)})
		return true
	})
	return result, err
}
