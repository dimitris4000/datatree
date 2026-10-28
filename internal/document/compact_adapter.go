package document

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"datatree/internal/compact"
	"datatree/internal/textstore"
	"go.yaml.in/yaml/v3"
)

// ParseCompactWithLimitsContext makes retained compact state testable on small
// fixtures. Ordinary parsing selects it automatically above 1 MiB.
func ParseCompactWithLimitsContext(ctx context.Context, format string, source []byte, limits Limits) (*Document, error) {
	return parseWithStorageContext(ctx, format, source, limits, true)
}
func compactID(id string) uint32 {
	n, err := strconv.ParseUint(id, 10, 32)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != id {
		return 0
	}
	return uint32(n)
}
func publicID(id uint32) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(id), 10)
}
func rawWeight(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	count := 1
	for _, c := range n.Content {
		count += rawWeight(c)
	}
	return count
}
func rawDepth(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	depth := 0
	for _, c := range n.Content {
		depth = max(depth, 1+rawDepth(c))
	}
	return depth
}
func rawMetadata(n *yaml.Node) bool {
	return n != nil && (n.Anchor != "" || n.Alias != nil || n.HeadComment != "" || n.LineComment != "" || n.FootComment != "" || n.Style&yaml.TaggedStyle != 0)
}
func (d *Document) compactInputs(ctx context.Context) ([]compact.Input, bool, bool, error) {
	inputs := make([]compact.Input, 0, len(d.Nodes))
	complexKeys := false
	hasYAMLMetadata := false
	var csvSpans map[string]SourceRange
	if d.Format == "csv" {
		ranges := make([]SourceRange, 0, len(d.Nodes))
		for _, n := range d.Nodes {
			ranges = append(ranges, SourceRange{ID: n.ID, Start: n.Start, End: n.End})
		}
		d.csvRanges(ranges, lineStarts(d.Source))
		csvSpans = make(map[string]SourceRange, len(ranges))
		for _, r := range ranges {
			csvSpans[r.ID] = r
		}
	}
	var walk func(*Node) error
	walk = func(n *Node) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.Format == "yaml" {
			hasYAMLMetadata = hasYAMLMetadata || rawMetadata(n.raw) || rawMetadata(n.key)
		}
		start, end, keyStart := n.Start, n.End, -1
		if n.key != nil {
			keyStart = n.keyStart
		}
		if d.Format == "csv" {
			span := csvSpans[n.ID]
			start, end, keyStart = span.Start, span.End, -1
		}
		if n == d.Root {
			if d.Format == "jsonl" || d.Format == "csv" {
				start, end = 0, len(d.Source)
			}
			keyStart = -1
		}
		in := compact.Input{ID: compactID(n.ID), Kind: n.Kind, Label: n.Label, Value: n.Value, Start: start, KeyStart: keyStart, End: end, Pathable: n.Path != "", NativeIndent: 0, Weight: 1 + rawWeight(n.key), OwnDepth: rawDepth(n.key), HasMetadata: rawMetadata(n.raw) || rawMetadata(n.key)}
		if d.Format == "yaml" {
			in.HasMetadata = in.HasMetadata || yamlCompactMetadata(n.raw)
			in.NativeIndent = max(0, fragmentIndent(n))
		}
		if n.parent != nil {
			in.Parent = compactID(n.parent.ID)
		}
		in.Target = compactID(n.Target)
		if n.key != nil && (n.key.Kind != yaml.ScalarNode || n.key.Tag != "!!str") {
			complexKeys = true
			in.HasMetadata = true
		}
		for _, c := range n.Children {
			in.Children = append(in.Children, compactID(c.ID))
		}
		inputs = append(inputs, in)
		for _, c := range n.Children {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(d.Root); err != nil {
		return nil, false, false, err
	}
	return inputs, complexKeys, hasYAMLMetadata, nil
}
func (d *Document) compactify(ctx context.Context) (*Document, error) {
	if d.compact != nil {
		return d, nil
	}
	inputs, complexKeys, hasYAMLMetadata, err := d.compactInputs(ctx)
	if err != nil {
		return nil, err
	}
	tree, err := compact.New(ctx, textstore.FromImmutable(d.Source), inputs, compactID(d.Root.ID), d.Limits().MaxNodes, MaxDepth+1)
	if err != nil {
		return nil, err
	}
	next := compactDocument(d.Format, d.Source, d.headers, d.limits, tree, complexKeys)
	next.compactYAMLMetadata = hasYAMLMetadata
	return next, nil
}
func compactDocument(format string, source []byte, headers []string, limits Limits, tree *compact.Tree, complexKeys bool) *Document {
	d := &Document{Format: format, Source: source, headers: headers, limits: limits, compact: tree, compactComplexKeys: complexKeys}
	d.Root, _ = d.LookupNode(publicID(tree.Root()))
	return d
}
func (d *Document) NodeCount() int {
	if d.compact != nil {
		return d.compact.Count()
	}
	return len(d.Nodes)
}
func (d *Document) LookupNode(id string) (*Node, bool) {
	if d.compact == nil {
		n, ok := d.Nodes[id]
		return n, ok
	}
	r, ok := d.compact.Record(compactID(id))
	if !ok {
		return nil, false
	}
	pos, err := d.compact.Text().Position(r.Start)
	if err != nil {
		return nil, false
	}
	return &Node{ID: id, Label: r.Label, Kind: r.Kind, Value: r.Value, Path: d.compact.Path(r.ID), Location: fmt.Sprintf("line %d, column %d", pos.Line, pos.Column), Target: publicID(r.Target), Start: r.Start, End: r.End, keyStart: r.KeyStart}, true
}
func (d *Document) ChildPage(id string, offset, limit int) ([]*Node, int) {
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if d.compact != nil {
		r, ok := d.compact.Record(compactID(id))
		if !ok {
			return nil, 0
		}
		ids := d.compact.Children(r.ID, offset, limit)
		children := make([]*Node, 0, len(ids))
		for _, id := range ids {
			n, ok := d.LookupNode(publicID(id))
			if ok {
				children = append(children, n)
			}
		}
		return children, r.ChildCount
	}
	n, ok := d.Nodes[id]
	if !ok {
		return nil, 0
	}
	if offset > len(n.Children) {
		offset = len(n.Children)
	}
	end := min(len(n.Children), offset+limit)
	return append([]*Node(nil), n.Children[offset:end]...), len(n.Children)
}

// materialize is an explicit compatibility boundary, never a retained cache.
// IDs are remapped by parallel child order so schema/diff/edit callers keep the
// compact revision's identities even after local replacement changed positions.
func (d *Document) materialize(ctx context.Context) (*Document, error) {
	if d.compact == nil {
		return d, nil
	}
	legacy, err := parseLegacyWithLimitsContext(ctx, d.Format, d.Source, d.Limits())
	if err != nil {
		return nil, err
	}
	nodes := make(map[string]*Node, d.NodeCount())
	remap := make(map[string]string, d.NodeCount())
	var walk func(*Node, uint32) error
	walk = func(n *Node, id uint32) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		r, ok := d.compact.Record(id)
		if !ok || r.Kind != n.Kind || r.ChildCount != len(n.Children) {
			return errors.New("compact materialization topology mismatch")
		}
		oldID := n.ID
		n.ID = publicID(id)
		nodes[n.ID] = n
		remap[oldID] = n.ID
		children := d.compact.Children(id, 0, r.ChildCount)
		for i, c := range n.Children {
			if err := walk(c, children[i]); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(legacy.Root, d.compact.Root()); err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if n.Target != "" {
			n.Target = remap[n.Target]
		}
	}
	legacy.Nodes = nodes
	return legacy, nil
}
func (d *Document) compactFallbackResult(ctx context.Context, next *Document, err error) (*Document, error) {
	if err != nil {
		return nil, err
	}
	return next.compactify(ctx)
}
