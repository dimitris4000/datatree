package document

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type queryStep struct {
	kind   string
	key    string
	index  int
	filter *queryFilter
}
type queryFilter struct {
	path            []queryStep
	op, kind, value string
}
type queryParser struct {
	text string
	pos  int
}

var queryIdentifier = regexp.MustCompile(`^[\pL_$][\pL\pN_$]*`)
var queryNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

func (p *queryParser) take(s string) bool {
	if strings.HasPrefix(p.text[p.pos:], s) {
		p.pos += len(s)
		return true
	}
	return false
}
func (p *queryParser) spaces() {
	for p.pos < len(p.text) && strings.ContainsRune(" \t\r\n", rune(p.text[p.pos])) {
		p.pos++
	}
}
func (p *queryParser) syntax() error {
	return fmt.Errorf("unsupported or invalid query at byte %d; use $, .key, ['key'], [index], [*], ..key or [?(@.key > literal)]", p.pos+1)
}
func (p *queryParser) quoted() (string, error) {
	if p.pos >= len(p.text) || (p.text[p.pos] != '\'' && p.text[p.pos] != '"') {
		return "", p.syntax()
	}
	quote := p.text[p.pos]
	p.pos++
	var b strings.Builder
	b.WriteByte('"')
	for p.pos < len(p.text) {
		c := p.text[p.pos]
		p.pos++
		if c == quote {
			b.WriteByte('"')
			var s string
			if err := json.Unmarshal([]byte(b.String()), &s); err != nil {
				return "", p.syntax()
			}
			return s, nil
		}
		if c == '\\' {
			if p.pos == len(p.text) {
				return "", p.syntax()
			}
			next := p.text[p.pos]
			p.pos++
			if next == '\'' && quote == '\'' {
				b.WriteByte('\'')
			} else {
				b.WriteByte('\\')
				b.WriteByte(next)
			}
		} else if c == '"' {
			b.WriteString(`\"`)
		} else {
			b.WriteByte(c)
		}
	}
	return "", p.syntax()
}
func (p *queryParser) property() (queryStep, error) {
	if p.take("[") {
		p.spaces()
		key, err := p.quoted()
		if err != nil {
			return queryStep{}, err
		}
		p.spaces()
		if !p.take("]") {
			return queryStep{}, p.syntax()
		}
		return queryStep{kind: "key", key: key}, nil
	}
	key := queryIdentifier.FindString(p.text[p.pos:])
	if key == "" {
		return queryStep{}, p.syntax()
	}
	p.pos += len(key)
	return queryStep{kind: "key", key: key}, nil
}
func (p *queryParser) step(extended bool) (queryStep, error) {
	if extended && p.take("..") {
		s, err := p.property()
		s.kind = "recursive"
		return s, err
	}
	if p.take(".") {
		if extended && p.take("*") {
			return queryStep{kind: "wildcard"}, nil
		}
		return p.property()
	}
	if !p.take("[") {
		return queryStep{}, p.syntax()
	}
	p.spaces()
	if p.pos < len(p.text) && (p.text[p.pos] == '\'' || p.text[p.pos] == '"') {
		key, err := p.quoted()
		if err != nil {
			return queryStep{}, err
		}
		p.spaces()
		if !p.take("]") {
			return queryStep{}, p.syntax()
		}
		return queryStep{kind: "key", key: key}, nil
	}
	if extended && p.take("*") {
		p.spaces()
		if !p.take("]") {
			return queryStep{}, p.syntax()
		}
		return queryStep{kind: "wildcard"}, nil
	}
	if extended && p.take("?(") {
		f, err := p.predicate()
		if err != nil {
			return queryStep{}, err
		}
		p.spaces()
		if !p.take(")") {
			return queryStep{}, p.syntax()
		}
		p.spaces()
		if !p.take("]") {
			return queryStep{}, p.syntax()
		}
		return queryStep{kind: "filter", filter: f}, nil
	}
	start := p.pos
	for p.pos < len(p.text) && p.text[p.pos] >= '0' && p.text[p.pos] <= '9' {
		p.pos++
	}
	if start == p.pos {
		return queryStep{}, p.syntax()
	}
	idx, err := strconv.Atoi(p.text[start:p.pos])
	if err != nil {
		return queryStep{}, p.syntax()
	}
	p.spaces()
	if !p.take("]") {
		return queryStep{}, p.syntax()
	}
	return queryStep{kind: "index", index: idx}, nil
}
func (p *queryParser) predicate() (*queryFilter, error) {
	p.spaces()
	if !p.take("@") {
		return nil, p.syntax()
	}
	f := &queryFilter{}
	for p.pos < len(p.text) && (p.text[p.pos] == '.' || p.text[p.pos] == '[') {
		s, err := p.step(false)
		if err != nil {
			return nil, err
		}
		f.path = append(f.path, s)
	}
	p.spaces()
	for _, op := range []string{"==", "!=", "<=", ">=", "<", ">"} {
		if p.take(op) {
			f.op = op
			break
		}
	}
	if f.op == "" {
		return nil, p.syntax()
	}
	p.spaces()
	if p.pos < len(p.text) && (p.text[p.pos] == '\'' || p.text[p.pos] == '"') {
		v, err := p.quoted()
		if err != nil {
			return nil, err
		}
		f.kind = "string"
		f.value = v
		return f, nil
	}
	start := p.pos
	for p.pos < len(p.text) && !strings.ContainsRune(" )\t\r\n", rune(p.text[p.pos])) {
		p.pos++
	}
	f.value = p.text[start:p.pos]
	switch f.value {
	case "true", "false":
		f.kind = "boolean"
	case "null":
		f.kind = "null"
	default:
		if !queryNumber.MatchString(f.value) {
			return nil, p.syntax()
		}
		f.kind = "number"
	}
	return f, nil
}

const queryWorkLimit = 1000000

type queryWork struct {
	remaining int
	ctx       context.Context
}

func (w *queryWork) spend() error {
	if w.ctx != nil {
		if err := w.ctx.Err(); err != nil {
			return err
		}
	}
	w.remaining--
	if w.remaining < 0 {
		return errors.New("query work limit exceeded; use a more specific path")
	}
	return nil
}
func queryChild(n *Node, s queryStep, w *queryWork) (*Node, error) {
	if err := w.spend(); err != nil {
		return nil, err
	}
	if s.kind == "index" {
		if n.Kind == "array" && s.index < len(n.Children) {
			return n.Children[s.index], nil
		}
		return nil, nil
	}
	if n.Kind == "object" {
		for _, c := range n.Children {
			if err := w.spend(); err != nil {
				return nil, err
			}
			if c.key != nil && c.key.Tag == "!!str" && c.Path != "" && c.Label == s.key {
				return c, nil
			}
		}
	}
	return nil, nil
}
func (d *Document) query(text string) (map[string]bool, error) {
	return d.queryContext(context.Background(), text)
}
func (d *Document) queryContext(ctx context.Context, text string) (map[string]bool, error) {
	if d.compact != nil {
		return d.compactQuery(ctx, text)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := queryParser{text: strings.TrimSpace(text)}
	if !p.take("$") {
		return nil, p.syntax()
	}
	steps := []queryStep{}
	for p.pos < len(p.text) {
		s, err := p.step(true)
		if err != nil {
			return nil, err
		}
		steps = append(steps, s)
		if len(steps) > MaxDepth {
			return nil, errors.New("query exceeds 256 steps")
		}
	}
	if d.Root.Kind == "stream" {
		return nil, errors.New("path queries do not support YAML document streams; use Search")
	}
	w := &queryWork{remaining: queryWorkLimit, ctx: ctx}
	current := []*Node{d.Root}
	for _, s := range steps {
		next := []*Node{}
		seen := map[string]bool{}
		visited := map[string]bool{}
		add := func(n *Node) {
			if n != nil && n.Path != "" && !seen[n.ID] {
				seen[n.ID] = true
				next = append(next, n)
			}
		}
		var recurse func(*Node) error
		recurse = func(n *Node) error {
			if visited[n.ID] || n.Path == "" {
				return nil
			}
			visited[n.ID] = true
			if err := w.spend(); err != nil {
				return err
			}
			for _, c := range n.Children {
				if n.Kind == "object" && c.key != nil && c.key.Tag == "!!str" && c.Label == s.key {
					add(c)
				}
				if err := recurse(c); err != nil {
					return err
				}
			}
			return nil
		}
		for _, n := range current {
			if err := w.spend(); err != nil {
				return nil, err
			}
			switch s.kind {
			case "recursive":
				if err := recurse(n); err != nil {
					return nil, err
				}
			case "wildcard":
				for _, c := range n.Children {
					if err := w.spend(); err != nil {
						return nil, err
					}
					add(c)
				}
			case "filter":
				if n.Kind == "array" {
					for _, c := range n.Children {
						ok, err := s.filter.matches(c, w)
						if err != nil {
							return nil, err
						}
						if ok {
							add(c)
						}
					}
				}
			default:
				c, err := queryChild(n, s, w)
				if err != nil {
					return nil, err
				}
				add(c)
			}
		}
		current = next
	}
	ids := map[string]bool{}
	for _, n := range current {
		ids[n.ID] = true
	}
	return ids, nil
}
func (f *queryFilter) matches(n *Node, w *queryWork) (bool, error) {
	if err := w.spend(); err != nil {
		return false, err
	}
	var err error
	for _, s := range f.path {
		n, err = queryChild(n, s, w)
		if err != nil || n == nil {
			return false, err
		}
	}
	scalar := n.Kind == "string" || n.Kind == "number" || n.Kind == "boolean" || n.Kind == "null"
	if !scalar {
		return false, nil
	}
	same := n.Kind == f.kind
	cmp := 0
	ordered := same && (f.kind == "string" || f.kind == "number")
	if same {
		switch f.kind {
		case "null":
			cmp = 0
		case "boolean":
			cmp = strings.Compare(strings.ToLower(n.Value), f.value)
		case "number":
			if len(n.Value) > MaxFindText {
				return false, errors.New("numeric filter values exceed the 4096-byte comparison limit")
			}
			w.remaining -= len(n.Value) + len(f.value)
			if err := w.spend(); err != nil {
				return false, err
			}
			var ok bool
			cmp, ok = compareQueryNumbers(n, f.value)
			if !ok {
				return false, nil
			}
		default:
			cmp = strings.Compare(n.Value, f.value)
		}
	}
	switch f.op {
	case "==":
		return same && cmp == 0, nil
	case "!=":
		return !same || cmp != 0, nil
	case "<":
		return ordered && cmp < 0, nil
	case "<=":
		return ordered && cmp <= 0, nil
	case ">":
		return ordered && cmp > 0, nil
	case ">=":
		return ordered && cmp >= 0, nil
	}
	return false, nil
}
