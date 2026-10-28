package document

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// jsonParser scans strict JSON once and records raw spans. Unescaped strings
// avoid encoding/json's token/interface allocation path; escaped strings still
// use its Unicode and surrogate handling to preserve the standard semantics.
type jsonParser struct {
	err                             error
	ctx                             context.Context
	source                          []byte
	pos, line, column, base, budget int
	limits                          Limits
	spans                           map[*yaml.Node]byteSpan
}

func parseJSON(ctx context.Context, b []byte, limits Limits, spans map[*yaml.Node]byteSpan, base int) (*yaml.Node, error) {
	p := jsonParser{ctx: ctx, source: b, line: 1, column: 1, base: base, limits: limits, spans: spans}
	root, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.space()
	if p.err != nil {
		return nil, p.err
	}
	if p.pos != len(b) {
		return nil, errors.New("trailing content after JSON")
	}
	return root, nil
}
func (p *jsonParser) syntax() error {
	if p.err != nil {
		return p.err
	}
	return fmt.Errorf("invalid JSON at byte %d", p.pos+1)
}
func (p *jsonParser) advance(end int) {
	for p.pos < end {
		if p.pos&32767 == 0 {
			if p.err = p.ctx.Err(); p.err != nil {
				return
			}
		}
		c := p.source[p.pos]
		if c < utf8.RuneSelf {
			p.pos++
			if c == '\n' {
				p.line++
				p.column = 1
			} else {
				p.column++
			}
		} else {
			_, size := utf8.DecodeRune(p.source[p.pos:])
			p.pos += size
			p.column++
		}
	}
}
func (p *jsonParser) space() {
	for p.pos < len(p.source) {
		if p.pos&32767 == 0 {
			if p.err = p.ctx.Err(); p.err != nil {
				return
			}
		}
		switch p.source[p.pos] {
		case ' ', '\t', '\r':
			p.pos++
			p.column++
		case '\n':
			p.pos++
			p.line++
			p.column = 1
		default:
			return
		}
	}
}
func (p *jsonParser) spend(depth int) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	p.budget++
	if p.budget > p.limits.MaxNodes {
		return fmt.Errorf("JSON exceeds parsed node limit (%d; object keys also count)", p.limits.MaxNodes)
	}
	if depth > MaxDepth {
		return errors.New("JSON depth exceeds 256")
	}
	return nil
}
func (p *jsonParser) stringValue() (string, error) {
	start := p.pos
	escaped := false
	for i := start + 1; i < len(p.source); i++ {
		if i&32767 == 0 {
			if err := p.ctx.Err(); err != nil {
				return "", err
			}
		}
		switch p.source[i] {
		case '"':
			var value string
			if escaped {
				if err := json.Unmarshal(p.source[start:i+1], &value); err != nil {
					return "", err
				}
			} else {
				value = string(p.source[start+1 : i])
			}
			p.advance(i + 1)
			return value, nil
		case '\\':
			escaped = true
			i++
		default:
			if p.source[i] < 0x20 {
				return "", p.syntax()
			}
		}
	}
	return "", p.syntax()
}
func (p *jsonParser) value(depth int) (*yaml.Node, error) {
	if err := p.spend(depth); err != nil {
		return nil, err
	}
	p.space()
	if p.pos == len(p.source) {
		return nil, p.syntax()
	}
	start := p.pos
	n := &yaml.Node{Line: p.line, Column: p.column}
	switch p.source[p.pos] {
	case '{', '[':
		open := p.source[p.pos]
		close := byte(']')
		n.Kind, n.Tag = yaml.SequenceNode, "!!seq"
		if open == '{' {
			close = '}'
			n.Kind, n.Tag = yaml.MappingNode, "!!map"
		}
		n.Style = yaml.FlowStyle
		p.advance(p.pos + 1)
		p.space()
		if p.pos < len(p.source) && p.source[p.pos] == close {
			p.advance(p.pos + 1)
			break
		}
		var seen map[string]bool
		if open == '{' {
			seen = map[string]bool{}
		}
		for {
			if open == '{' {
				if err := p.spend(depth + 1); err != nil {
					return nil, err
				}
				p.space()
				if p.pos == len(p.source) || p.source[p.pos] != '"' {
					return nil, p.syntax()
				}
				keyStart, kl, kc := p.pos, p.line, p.column
				key, err := p.stringValue()
				if err != nil {
					return nil, err
				}
				if seen[key] {
					return nil, fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = true
				k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key, Style: yaml.DoubleQuotedStyle, Line: kl, Column: kc}
				p.spans[k] = byteSpan{p.base + keyStart, p.base + p.pos}
				p.space()
				if p.pos == len(p.source) || p.source[p.pos] != ':' {
					return nil, p.syntax()
				}
				p.advance(p.pos + 1)
				n.Content = append(n.Content, k)
			}
			child, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, child)
			p.space()
			if p.pos == len(p.source) {
				return nil, p.syntax()
			}
			if p.source[p.pos] == close {
				p.advance(p.pos + 1)
				break
			}
			if p.source[p.pos] != ',' {
				return nil, p.syntax()
			}
			p.advance(p.pos + 1)
		}
	case '"':
		value, err := p.stringValue()
		if err != nil {
			return nil, err
		}
		n.Kind, n.Tag, n.Value, n.Style = yaml.ScalarNode, "!!str", value, yaml.DoubleQuotedStyle
	case 't', 'f', 'n':
		literal, tag := "true", "!!bool"
		if p.source[p.pos] == 'f' {
			literal = "false"
		} else if p.source[p.pos] == 'n' {
			literal, tag = "null", "!!null"
		}
		if !bytes.HasPrefix(p.source[p.pos:], []byte(literal)) {
			return nil, p.syntax()
		}
		p.advance(p.pos + len(literal))
		n.Kind, n.Tag, n.Value = yaml.ScalarNode, tag, literal
	default:
		i := p.pos
		if p.source[i] == '-' {
			i++
		}
		if i == len(p.source) {
			return nil, p.syntax()
		}
		if p.source[i] == '0' {
			i++
		} else if p.source[i] >= '1' && p.source[i] <= '9' {
			for i < len(p.source) && p.source[i] >= '0' && p.source[i] <= '9' {
				i++
			}
		} else {
			return nil, p.syntax()
		}
		if i < len(p.source) && p.source[i] == '.' {
			i++
			begin := i
			for i < len(p.source) && p.source[i] >= '0' && p.source[i] <= '9' {
				i++
			}
			if i == begin {
				return nil, p.syntax()
			}
		}
		if i < len(p.source) && (p.source[i] == 'e' || p.source[i] == 'E') {
			i++
			if i < len(p.source) && (p.source[i] == '+' || p.source[i] == '-') {
				i++
			}
			begin := i
			for i < len(p.source) && p.source[i] >= '0' && p.source[i] <= '9' {
				i++
			}
			if i == begin {
				return nil, p.syntax()
			}
		}
		n.Kind, n.Tag, n.Value = yaml.ScalarNode, "!!int", string(p.source[start:i])
		if strings.ContainsAny(n.Value, ".eE") {
			n.Tag = "!!float"
		}
		p.advance(i)
	}
	if p.err != nil {
		return nil, p.err
	}
	p.spans[n] = byteSpan{p.base + start, p.base + p.pos}
	return n, nil
}
