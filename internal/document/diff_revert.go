package document

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

func rawWalk(n *yaml.Node, visit func(*yaml.Node)) {
	visit(n)
	for _, c := range n.Content {
		rawWalk(c, visit)
	}
}

// RevertDiff recomputes the selected difference and grafts its full baseline
// node, never its display preview. All modifications happen on a private clone.
func (d *Document) RevertDiff(base *Document, index int) (*Document, error) {
	result, err := Diff(base, d)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(result.Changes) {
		return nil, errors.New("change no longer exists; refresh Changes")
	}
	change := result.Changes[index]
	current := map[*yaml.Node]*yaml.Node{}
	root := clone(d.raw, current)
	imported := map[*yaml.Node]*yaml.Node{}
	var copyIn func(*yaml.Node) *yaml.Node
	copyIn = func(n *yaml.Node) *yaml.Node {
		if found := imported[n]; found != nil {
			return found
		}
		c := *n
		imported[n] = &c
		c.Content = nil
		for _, child := range n.Content {
			c.Content = append(c.Content, copyIn(child))
		}
		return &c
	}
	patchTarget := change.afterNode
	switch change.Status {
	case "added":
		patchTarget = change.afterNode.parent
		p := current[patchTarget.raw]
		target := current[change.afterNode.raw]
		for j, c := range p.Content {
			if c == target {
				start := j
				if p.Kind == yaml.MappingNode {
					start--
				}
				p.Content = append(p.Content[:start], p.Content[j+1:]...)
				break
			}
		}
	case "changed":
		target := current[change.afterNode.raw]
		oldAnchor := target.Anchor
		value := copyIn(change.beforeNode.raw)
		*target = *value
		imported[change.beforeNode.raw] = target
		if oldAnchor != "" {
			target.Anchor = oldAnchor
		}
	case "removed":
		patchTarget = change.parent
		p := current[patchTarget.raw]
		position, err := d.restorePosition(base, change.beforeNode, patchTarget)
		if err != nil {
			return nil, err
		}
		values := []*yaml.Node{copyIn(change.beforeNode.raw)}
		if p.Kind == yaml.MappingNode {
			values = []*yaml.Node{copyIn(change.beforeNode.key), values[0]}
		}
		tail := append([]*yaml.Node(nil), p.Content[position:]...)
		p.Content = append(p.Content[:position], values...)
		p.Content = append(p.Content, tail...)
	}
	wider, err := rebindDiffReferences(base, d, root, current, imported)
	if err != nil {
		return nil, err
	}
	if wider {
		patchTarget = d.Root
	}
	// Validate the entire desired graph and format before touching source bytes.
	encoded, err := encode(d.Format, root, d.headers)
	if err != nil {
		return nil, fmt.Errorf("cannot revert in %s format: %w", d.Format, err)
	}
	desired, err := Parse(d.Format, encoded)
	if err != nil {
		return nil, fmt.Errorf("revert rejected; source unchanged: %w", err)
	}
	if err := sameRevertStructure(root, desired.raw); err != nil {
		return nil, err
	}
	if d.Format != "yaml" || patchTarget == d.Root {
		return desired, nil
	}
	// Patch only the affected YAML value/collection, preserving unrelated bytes.
	p := current[patchTarget.raw]
	p.FootComment = ""
	p.HeadComment = ""
	for _, original := range d.Nodes {
		if original.End == patchTarget.End && original.Start >= patchTarget.Start {
			current[original.raw].FootComment = ""
			if original.key != nil {
				current[original.key].FootComment = ""
			}
		}
	}
	if len(p.Content) > 0 && p.Content[0].Line == patchTarget.raw.Line {
		p.Content[0].HeadComment = ""
	}
	b, err := marshalYAML(p)
	if err != nil {
		return nil, err
	}
	replacement := indentFragment(string(b), fragmentIndent(patchTarget))
	if patchTarget.parent != nil && patchTarget.parent.Kind == "object" && (p.Kind == yaml.MappingNode || p.Kind == yaml.SequenceNode) && p.Style&yaml.FlowStyle == 0 && sameLine(d.Source, patchTarget.Start, patchTarget.key) {
		indent := patchTarget.key.Column + 1
		replacement = "\n" + strings.Repeat(" ", indent) + indentFragment(string(b), indent)
	}
	next, err := d.patch(patchTarget, replacement, true)
	if err != nil {
		return nil, err
	}
	if err := sameRevertStructure(root, next.raw); err != nil {
		return nil, err
	}
	return next, nil
}
func sameRevertStructure(expected, actual *yaml.Node) error {
	w := &diffWork{left: diffWorkLimit}
	a, err := newDiffIdentity(&Document{raw: expected}, w).hash(expected, false)
	if err != nil {
		return err
	}
	b, err := newDiffIdentity(&Document{raw: actual}, w).hash(actual, false)
	if err != nil {
		return err
	}
	if a != b {
		return errors.New("revert cannot preserve this value or its references in the current format; source unchanged")
	}
	return nil
}
func (d *Document) restorePosition(base *Document, b, parent *Node) (int, error) {
	position := -1
	for j, c := range b.parent.Children {
		if c == b {
			position = j
			break
		}
	}
	if position < 0 {
		return 0, errors.New("invalid baseline parent")
	}
	if parent.Kind != "object" {
		if position > len(parent.Children) {
			return 0, errors.New("restore earlier removed array items first")
		}
		return position, nil
	}
	w := &diffWork{left: diffWorkLimit}
	before, after := newDiffIdentity(base, w), newDiffIdentity(d, w)
	positions := map[string]int{}
	for j, c := range parent.Children {
		h, err := after.hash(c.key, true)
		if err != nil {
			return 0, err
		}
		positions[h] = j
	}
	// Insert relative to surviving baseline siblings while retaining working order.
	for j := position + 1; j < len(b.parent.Children); j++ {
		h, err := before.hash(b.parent.Children[j].key, true)
		if err != nil {
			return 0, err
		}
		if pos, ok := positions[h]; ok {
			return pos * 2, nil
		}
	}
	for j := position - 1; j >= 0; j-- {
		h, err := before.hash(b.parent.Children[j].key, true)
		if err != nil {
			return 0, err
		}
		if pos, ok := positions[h]; ok {
			return (pos + 1) * 2, nil
		}
	}
	return len(parent.raw.Content), nil
}

// Rebind imported aliases by structural location and preserve current anchors.
// References whose targets would be removed are rejected atomically.
func rebindDiffReferences(base, working *Document, root *yaml.Node, current, imported map[*yaml.Node]*yaml.Node) (bool, error) {
	reachable := map[*yaml.Node]bool{}
	incoming := map[*yaml.Node]bool{}
	for _, n := range imported {
		incoming[n] = true
	}
	rawWalk(root, func(n *yaml.Node) { reachable[n] = true })
	used := map[string]bool{}
	for n := range reachable {
		if !incoming[n] && n.Anchor != "" {
			used[n.Anchor] = true
		}
	}
	// Reserve imported anchors in traversal order, ensuring deterministic names.
	rawWalk(root, func(n *yaml.Node) {
		if incoming[n] && n.Anchor != "" {
			name := n.Anchor
			for j := 2; used[name]; j++ {
				name = fmt.Sprintf("%s_revert%d", n.Anchor, j)
			}
			n.Anchor = name
			used[name] = true
		}
	})
	w := &diffWork{left: diffWorkLimit}
	before, after := newDiffIdentity(base, w), newDiffIdentity(working, w)
	byLocation := map[string]*yaml.Node{}
	importsByLocation := map[string]*yaml.Node{}
	var addressErr error
	// Index only when references exist; no alias expansion is involved.
	hasAlias := false
	rawWalk(root, func(n *yaml.Node) {
		if n.Kind == yaml.AliasNode {
			hasAlias = true
		}
	})
	if !hasAlias {
		return false, nil
	}
	rawWalk(working.raw, func(n *yaml.Node) {
		if addressErr != nil {
			return
		}
		address, err := after.address(n, nil)
		if err != nil {
			addressErr = err
			return
		}
		byLocation[address] = current[n]
	})
	for original, copied := range imported {
		address, err := before.address(original, nil)
		if err != nil {
			return false, err
		}
		importsByLocation[address] = copied
	}
	if addressErr != nil {
		return false, addressErr
	}
	wider := false
	ensureAnchor := func(n *yaml.Node) {
		if n.Anchor == "" {
			name := "reverted_anchor"
			for j := 2; used[name]; j++ {
				name = fmt.Sprintf("reverted_anchor%d", j)
			}
			n.Anchor = name
			used[name] = true
			if !incoming[n] {
				wider = true
			}
		}
	}
	for original, copied := range imported {
		if original.Kind != yaml.AliasNode {
			continue
		}
		target := imported[original.Alias]
		if target == nil {
			address, err := before.address(original.Alias, nil)
			if err != nil {
				return false, err
			}
			target = byLocation[address]
		}
		if target == nil || !reachable[target] {
			return false, errors.New("restore the referenced anchor target before reverting this alias")
		}
		ensureAnchor(target)
		copied.Alias = target
		copied.Value = target.Anchor
	}
	reverse := map[*yaml.Node]*yaml.Node{}
	for original, copied := range current {
		reverse[copied] = original
	}
	var failure error
	rawWalk(root, func(n *yaml.Node) {
		if failure != nil || n.Kind != yaml.AliasNode || incoming[n] {
			return
		}
		if !reachable[n.Alias] {
			original := reverse[n.Alias]
			if original == nil {
				failure = errors.New("revert would leave an unresolved alias")
				return
			}
			address, err := after.address(original, nil)
			if err != nil {
				failure = err
				return
			}
			n.Alias = importsByLocation[address]
			if n.Alias == nil || !reachable[n.Alias] {
				failure = errors.New("revert would remove a referenced anchor; revert its references first")
				return
			}
		}
		ensureAnchor(n.Alias)
		if n.Value != n.Alias.Anchor {
			n.Value = n.Alias.Anchor
			wider = true
		}
	})
	return wider, failure
}
