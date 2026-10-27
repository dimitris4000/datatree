package document

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Action struct {
	Op         string `json:"op"`
	Node       string `json:"node"`
	Key        string `json:"key"`
	Text       string `json:"text"`
	Format     string `json:"format"`
	Confirm    bool   `json:"confirm"`
	Structured bool   `json:"structured"`
}

func clone(n *yaml.Node, table map[*yaml.Node]*yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if v := table[n]; v != nil {
		return v
	}
	c := *n
	table[n] = &c
	c.Content = nil
	for _, v := range n.Content {
		c.Content = append(c.Content, clone(v, table))
	}
	c.Alias = clone(n.Alias, table)
	return &c
}
func (d *Document) Edit(a Action) (*Document, error) {
	target := d.Nodes[a.Node]
	if target == nil {
		return nil, errors.New("selected node no longer exists")
	}
	if a.Op == "replace" && d.Format == "yaml" && a.Format == "yaml" {
		current, _ := d.YAML(target.ID)
		normalized := func(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
		if normalized(a.Text) == normalized(current) {
			return d.parse(d.Format, d.Source)
		}
		incoming, err := d.yamlFragment(a.Text)
		if err != nil {
			return nil, fmt.Errorf("invalid YAML subtree: %w", err)
		}
		if incoming.Kind == yaml.DocumentNode {
			return nil, errors.New("subtree must contain one YAML document")
		}
		replacement := indentFragment(strings.ReplaceAll(a.Text, "\r\n", "\n"), fragmentIndent(target))
		if target.parent != nil && target.parent.Kind == "object" && (incoming.Kind == yaml.MappingNode || incoming.Kind == yaml.SequenceNode) && incoming.Style&yaml.FlowStyle == 0 && sameLine(d.Source, target.Start, target.key) {
			indent := target.key.Column + 1
			replacement = "\n" + strings.Repeat(" ", indent) + indentFragment(strings.ReplaceAll(a.Text, "\r\n", "\n"), indent)
		}
		return d.patch(target, replacement, false)
	}
	table := map[*yaml.Node]*yaml.Node{}
	root := clone(d.raw, table)
	n := table[target.raw]
	patchTarget := target
	if a.Op == "replace" {
		f := a.Format
		if f == "" {
			f = "json"
		}
		incoming, err := d.parse(f, []byte(a.Text))
		if err != nil {
			return nil, err
		}
		if incoming.Root.Kind == "stream" {
			return nil, errors.New("subtree must contain one document")
		}
		if d.Format == "yaml" && !a.Confirm && !a.Structured {
			if _, err := jsonBytes(target.raw, false, d.Limits()); err != nil {
				return nil, errors.New("confirm lossy JSON replacement of YAML subtree first")
			}
		}
		if d.Format == "yaml" && a.Structured {
			if n.Kind != yaml.ScalarNode || incoming.raw.Kind != yaml.ScalarNode {
				return nil, errors.New("structured value edit requires scalars")
			}
			oldTag := n.Tag
			n.Value = incoming.raw.Value
			n.Tag = incoming.raw.Tag
			if n.Tag != oldTag {
				n.Style = 0
			}
		} else {
			c := *incoming.raw
			*n = c
		}
	} else if a.Op == "add" {
		if n.Kind != yaml.MappingNode && n.Kind != yaml.SequenceNode {
			return nil, errors.New("select a collection to add an entry")
		}
		f := a.Format
		if f == "" {
			f = "json"
		}
		v, err := d.parse(f, []byte(a.Text))
		if err != nil {
			return nil, err
		}
		if v.Root.Kind == "stream" {
			return nil, errors.New("entry must contain one value")
		}
		if n.Kind == yaml.MappingNode {
			if err := uniqueKey(n, a.Key, -1); err != nil {
				return nil, err
			}
			n.Content = append(n.Content, strNode(a.Key), v.raw)
		} else {
			n.Content = append(n.Content, v.raw)
		}
	} else {
		if target.parent == nil {
			return nil, errors.New("cannot delete, rename, or duplicate the document root")
		}
		patchTarget = target.parent
		parent := table[target.parent.raw]
		index := -1
		for i, c := range parent.Content {
			if c == n {
				index = i
				break
			}
		}
		if index < 0 {
			return nil, errors.New("invalid parent")
		}
		switch a.Op {
		case "delete":
			start := index
			if parent.Kind == yaml.MappingNode {
				start--
			}
			parent.Content = append(parent.Content[:start], parent.Content[index+1:]...)
		case "rename":
			if parent.Kind != yaml.MappingNode {
				return nil, errors.New("only object keys can be renamed")
			}
			if err := uniqueKey(parent, a.Key, index-1); err != nil {
				return nil, err
			}
			parent.Content[index-1] = strNode(a.Key)
		case "clone":
			copied := clone(n, map[*yaml.Node]*yaml.Node{})
			renameAnchors(copied, root)
			var inserted []*yaml.Node
			if parent.Kind == yaml.MappingNode {
				if err := uniqueKey(parent, a.Key, -1); err != nil {
					return nil, err
				}
				inserted = []*yaml.Node{strNode(a.Key), copied}
			} else {
				inserted = []*yaml.Node{copied}
			}
			suffix := append([]*yaml.Node(nil), parent.Content[index+1:]...)
			parent.Content = append(parent.Content[:index+1], inserted...)
			parent.Content = append(parent.Content, suffix...)
		default:
			return nil, errors.New("unknown edit operation")
		}
	}
	if d.Format == "yaml" {
		p := table[patchTarget.raw]
		// Foot comments beyond the patch remain in the untouched suffix.
		p.FootComment = ""
		for _, original := range d.Nodes {
			if original.End == patchTarget.End && original.Start >= patchTarget.Start {
				table[original.raw].FootComment = ""
				if original.key != nil {
					table[original.key].FootComment = ""
				}
			}
		}
		head := p.HeadComment
		p.HeadComment = ""
		// A leading comment preceding the first key remains outside this patch.
		if len(p.Content) > 0 && p.Content[0].Line == patchTarget.raw.Line {
			p.Content[0].HeadComment = ""
		}
		b, err := marshalYAML(p)
		p.HeadComment = head
		if err != nil {
			return nil, err
		}
		replacement := indentFragment(string(b), fragmentIndent(patchTarget))
		if a.Op == "replace" && target.parent != nil && target.parent.Kind == "object" && (n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode) && n.Style&yaml.FlowStyle == 0 && sameLine(d.Source, target.Start, target.key) {
			indent := target.key.Column + 1
			replacement = "\n" + strings.Repeat(" ", indent) + indentFragment(string(b), indent)
		}
		return d.patch(patchTarget, replacement, true)
	}
	b, err := encode(d.Format, root, d.headers, d.Limits())
	if err != nil {
		return nil, err
	}
	return d.parse(d.Format, b)
}
func sameLine(source []byte, start int, key *yaml.Node) bool {
	if key == nil {
		return false
	}
	return bytes.Count(source[:start], []byte("\n"))+1 == key.Line
}
func indentFragment(text string, indent int) string {
	if indent < 0 {
		indent = 0
	}
	hasFinalNewline := strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")
	result := strings.ReplaceAll(text, "\n", "\n"+strings.Repeat(" ", indent))
	if hasFinalNewline {
		result += "\n"
	}
	return result
}
func (d *Document) patch(n *Node, replacement string, includeComment bool) (*Document, error) {
	start, end := n.Start, n.End
	if start > 0 && d.Source[start-1] == ':' && !strings.HasPrefix(replacement, "\n") {
		replacement = " " + replacement
	}
	if includeComment && end < len(d.Source) {
		lineEnd := bytes.IndexByte(d.Source[end:], '\n')
		if lineEnd < 0 {
			lineEnd = len(d.Source) - end
		}
		tail := d.Source[end : end+lineEnd]
		if bytes.HasPrefix(bytes.TrimSpace(tail), []byte("#")) {
			end += lineEnd
			for end > start && d.Source[end-1] == '\r' {
				end--
			}
		}
	}
	if strings.HasSuffix(replacement, "\n") && end < len(d.Source) {
		if d.Source[end] == '\r' && end+1 < len(d.Source) && d.Source[end+1] == '\n' {
			end += 2
		} else if d.Source[end] == '\n' {
			end++
		}
	}
	if bytes.Contains(d.Source, []byte("\r\n")) {
		replacement = strings.ReplaceAll(strings.ReplaceAll(replacement, "\r\n", "\n"), "\n", "\r\n")
	}
	if end > start && d.Source[end-1] == '\n' && !strings.HasSuffix(replacement, "\n") {
		if bytes.Contains(d.Source, []byte("\r\n")) {
			replacement += "\r\n"
		} else {
			replacement += "\n"
		}
	}
	b := make([]byte, 0, len(d.Source)+len(replacement))
	b = append(b, d.Source[:start]...)
	b = append(b, replacement...)
	b = append(b, d.Source[end:]...)
	next, err := d.parse(d.Format, b)
	if err != nil {
		return nil, fmt.Errorf("edit rejected; source unchanged: %w", err)
	}
	return next, nil
}
func uniqueKey(n *yaml.Node, key string, except int) error {
	if key == "" {
		return errors.New("enter a nonempty destination key")
	}
	for i := 0; i < len(n.Content); i += 2 {
		if i != except && n.Content[i].Value == key {
			return fmt.Errorf("key %q already exists", key)
		}
	}
	return nil
}
func renameAnchors(copied, root *yaml.Node) {
	used := map[string]bool{}
	var walk func(*yaml.Node, func(*yaml.Node))
	walk = func(n *yaml.Node, f func(*yaml.Node)) {
		f(n)
		for _, c := range n.Content {
			walk(c, f)
		}
	}
	walk(root, func(n *yaml.Node) {
		if n.Anchor != "" {
			used[n.Anchor] = true
		}
	})
	names := map[*yaml.Node]string{}
	walk(copied, func(n *yaml.Node) {
		if n.Anchor != "" {
			base := n.Anchor + "_copy"
			name := base
			for i := 2; used[name]; i++ {
				name = fmt.Sprintf("%s%d", base, i)
			}
			used[name] = true
			n.Anchor = name
			names[n] = name
		}
	})
	walk(copied, func(n *yaml.Node) {
		if n.Kind == yaml.AliasNode && names[n.Alias] != "" {
			n.Value = names[n.Alias]
		}
	})
}

// Validate standalone fragments, with harmless anchor stubs for references into the document.
func (d *Document) yamlFragment(text string) (*yaml.Node, error) {
	parsed, err := d.parse("yaml", []byte(text))
	if err == nil {
		return parsed.raw, nil
	}
	var prefix strings.Builder
	prefix.WriteString("known:\n")
	anchors := map[string]bool{}
	for _, n := range d.Nodes {
		if n.raw.Anchor != "" && !anchors[n.raw.Anchor] {
			anchors[n.raw.Anchor] = true
			fmt.Fprintf(&prefix, "  - &%s null\n", n.raw.Anchor)
		}
	}
	if len(anchors) == 0 {
		return nil, err
	}
	prefix.WriteString("value:\n  ")
	prefix.WriteString(indentFragment(strings.ReplaceAll(text, "\r\n", "\n"), 2))
	wrapped, e := d.parse("yaml", []byte(prefix.String()))
	if e != nil {
		return nil, err
	}
	if len(wrapped.Root.Children) != 2 {
		return nil, err
	}
	return wrapped.Root.Children[1].raw, nil
}
