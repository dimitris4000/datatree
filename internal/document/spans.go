package document

import (
	"bytes"
	"strings"

	"go.yaml.in/yaml/v3"
)

func quotedEnd(b []byte, start int) int {
	if start >= len(b) {
		return start
	}
	q := b[start]
	for i := start + 1; i < len(b); i++ {
		if q == '"' && b[i] == '\\' {
			i++
			continue
		}
		if b[i] == q {
			if q == '\'' && i+1 < len(b) && b[i+1] == '\'' {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(b)
}
func skipProperties(b []byte, p int) int {
	for p < len(b) && (b[p] == '&' || b[p] == '!') {
		if b[p] == '!' && p+1 < len(b) && b[p+1] == '<' {
			for p < len(b) && b[p] != '>' {
				p++
			}
			if p < len(b) {
				p++
			}
		} else {
			for p < len(b) && !strings.ContainsRune(" \t\r\n[]{},", rune(b[p])) {
				p++
			}
		}
		for p < len(b) && (b[p] == ' ' || b[p] == '\t' || b[p] == '\r' || b[p] == '\n') {
			p++
		}
	}
	return p
}
func flowEnd(b []byte, start int) int { return flowEndIndexed(b, start, make(map[int]int)) }

// Cache every nested collection encountered by the outer scan. The parser can
// then request each child span without rescanning all of its descendants.
func flowEndIndexed(b []byte, start int, ends map[int]int) int {
	p := skipProperties(b, start)
	if p >= len(b) {
		return p
	}
	if b[p] != '{' && b[p] != '[' {
		return scalarEnd(b, start, &yaml.Node{Kind: yaml.ScalarNode})
	}
	if end, ok := ends[p]; ok {
		return end
	}
	stack := []int{}
	for i := p; i < len(b); i++ {
		switch b[i] {
		case '"', '\'':
			i = quotedEnd(b, i) - 1
		case '#':
			for i < len(b) && b[i] != '\n' {
				i++
			}
		case '{', '[':
			if cached, ok := ends[i]; ok && i != p {
				i = cached - 1
				continue
			}
			stack = append(stack, i)
		case '}', ']':
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			ends[open] = i + 1
			if len(stack) == 0 {
				return i + 1
			}
		}
	}
	return len(b)
}
func scalarEnd(b []byte, start int, n *yaml.Node) int {
	if start >= len(b) {
		return len(b)
	}
	p := skipProperties(b, start)
	if p >= len(b) {
		return p
	}
	if n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode || n.Kind == yaml.DocumentNode {
		if b[p] == '{' || b[p] == '[' {
			return flowEnd(b, start)
		}
		return start
	}
	if b[p] == '\'' || b[p] == '"' {
		return quotedEnd(b, p)
	}
	if b[p] == '|' || b[p] == '>' {
		base := bytes.LastIndexByte(b[:start], '\n') + 1
		indent := 0
		for base+indent < len(b) && b[base+indent] == ' ' {
			indent++
		}
		return blockEnd(b, p, indent)
	}
	end := p
	for end < len(b) {
		c := b[end]
		if c == '\r' || c == '\n' || c == ',' || c == '}' || c == ']' {
			break
		}
		if c == '#' && (end == p || b[end-1] == ' ' || b[end-1] == '\t') {
			break
		}
		end++
	}
	for end > p && (b[end-1] == ' ' || b[end-1] == '\t') {
		end--
	}
	return end
}

// plainYAMLEnd handles commas and folded continuation lines in block context.
func plainYAMLEnd(b []byte, start int, key *yaml.Node, parent *Node) int {
	p := skipProperties(b, start)
	end := p
	for end < len(b) && b[end] != '\r' && b[end] != '\n' {
		if b[end] == '#' && (end == p || b[end-1] == ' ' || b[end-1] == '\t') {
			break
		}
		end++
	}
	for end > p && (b[end-1] == ' ' || b[end-1] == '\t') {
		end--
	}
	base := -1
	if key != nil {
		base = key.Column - 1
	} else if parent != nil {
		base = parent.raw.Column - 1
	}
	next := bytes.IndexByte(b[p:], '\n')
	if next < 0 {
		return end
	}
	pos := p + next + 1
	for pos < len(b) {
		length := bytes.IndexByte(b[pos:], '\n')
		if length < 0 {
			length = len(b) - pos
		}
		line := b[pos : pos+length]
		trim := bytes.TrimSpace(line)
		indent := len(line) - len(bytes.TrimLeft(line, " "))
		if len(trim) == 0 || trim[0] == '#' {
			pos += length + 1
			continue
		}
		if indent <= base || (base < 0 && (bytes.Equal(trim, []byte("---")) || bytes.Equal(trim, []byte("...")) || trim[0] == '%')) {
			break
		}
		end = pos + len(bytes.TrimRight(line, " \t\r"))
		pos += length + 1
	}
	return end
}

func blockEnd(b []byte, start, indent int) int {
	line := bytes.IndexByte(b[start:], '\n')
	if line < 0 {
		return len(b)
	}
	pos := start + line + 1
	end := pos
	for pos < len(b) {
		next := bytes.IndexByte(b[pos:], '\n')
		if next < 0 {
			next = len(b) - pos
		}
		lineb := b[pos : pos+next]
		trim := bytes.TrimSpace(lineb)
		leading := len(lineb) - len(bytes.TrimLeft(lineb, " "))
		if len(trim) > 0 && leading <= indent {
			break
		}
		end = pos + next
		if end < len(b) {
			end++
		}
		pos = end
	}
	return end
}
func contextIndent(n *Node) int {
	if n.key != nil {
		return n.key.Column - 1
	}
	if n.parent != nil {
		return n.parent.raw.Column - 1
	}
	return 0
}
func fragmentIndent(n *Node) int {
	if n.raw.Kind == yaml.ScalarNode || n.raw.Kind == yaml.AliasNode {
		return contextIndent(n)
	}
	return n.raw.Column - 1
}
func dedentFragment(text string, indent int) string {
	lines := strings.Split(text, "\n")
	prefix := strings.Repeat(" ", max(0, indent))
	for i := 1; i < len(lines); i++ {
		lines[i] = strings.TrimPrefix(lines[i], prefix)
	}
	return strings.Join(lines, "\n")
}
