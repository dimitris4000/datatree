package compact

import (
	"context"
	"errors"
	"fmt"

	"datatree/internal/textstore"
)

// Builder accepts children before their parents, retaining only final records.
// It is private to one import and must not be used concurrently. Finish publishes
// the immutable tree once; any invalid input permanently invalidates the builder.
type Builder struct {
	ctx  context.Context
	tree *Tree
	edit mutation
	err  error
}

func NewBuilder(ctx context.Context, text textstore.Text, maxNodes, maxDepth int) (*Builder, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxNodes <= 0 || maxDepth < 0 {
		return nil, errors.New("invalid compact limits")
	}
	tree := &Tree{text: text, anchors: text.AnchorIndex(), chunks: map[uint32]*chunk{}, maxNodes: maxNodes, maxDepth: maxDepth, nextID: 1, references: map[uint32]int{}}
	b := &Builder{ctx: ctx, tree: tree, edit: mutation{tree: tree, touched: map[uint32]bool{}}}
	return b, nil
}
func (b *Builder) Add(in Input) error {
	if b.tree == nil {
		return errors.New("compact builder is closed")
	}
	if b.err == nil {
		b.err = b.add(in)
	}
	return b.err
}
func (b *Builder) add(in Input) error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	t := b.tree
	if in.ID == 0 {
		return errors.New("compact node ID zero is invalid")
	}
	if _, exists := t.get(in.ID); exists {
		return fmt.Errorf("duplicate compact ID %d", in.ID)
	}
	if t.count >= t.maxNodes {
		return errors.New("compact document exceeds node limit")
	}
	k, err := parseKind(in.Kind)
	if err != nil {
		return err
	}
	if len(in.Children) > 0 && k != objectKind && k != arrayKind && k != streamKind {
		return errors.New("compact scalar cannot have children")
	}
	key := in.KeyStart
	if key < 0 {
		key = in.Start
	}
	if in.Start < 0 || key < 0 || key > in.Start || in.End < in.Start || in.End > t.text.ByteLen() {
		return errors.New("invalid compact source span")
	}
	if in.OwnDepth < 0 || in.OwnDepth > t.maxDepth {
		return errors.New("compact key exceeds depth limit")
	}
	startAnchor, err := t.text.AnchorAt(in.Start)
	if err != nil {
		return err
	}
	keyAnchor, err := t.text.AnchorAt(key)
	if err != nil {
		return err
	}
	endAnchor, err := t.text.AnchorEnd(in.End)
	if err != nil {
		return err
	}
	r := row{id: in.ID, parent: in.Parent, target: in.Target, kind: k, label: in.Label, value: in.Value, start: startAnchor, keyStart: keyAnchor, end: endAnchor, index: -1, indent: in.NativeIndent, weight: max(1, in.Weight), ownDepth: in.OwnDepth, height: in.OwnDepth, pathable: in.Pathable, metadata: in.HasMetadata, hasKey: key < in.Start, count: 1}
	r.subweight = r.weight
	if r.weight > t.maxNodes {
		return errors.New("compact document exceeds raw-node weight limit")
	}
	r.children = append([]uint32(nil), in.Children...)
	previousEnd := in.Start
	for i, id := range r.children {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		child, ok := t.get(id)
		if !ok {
			return fmt.Errorf("missing compact child %d", id)
		}
		if child.parent != in.ID || child.index != -1 {
			return errors.New("compact child has wrong or repeated parent")
		}
		public, ok := t.resolve(child)
		if !ok {
			return errors.New("invalid compact child anchors")
		}
		if public.KeyStart < previousEnd || public.End > in.End {
			return errors.New("compact child lies outside ordered parent span")
		}
		if r.subweight > t.maxNodes-child.subweight {
			return errors.New("compact document exceeds raw-node weight limit")
		}
		r.subweight += child.subweight
		r.count += child.count
		r.height = max(r.height, child.height+1)
		if r.height > t.maxDepth {
			return errors.New("compact document exceeds depth limit")
		}
		previousEnd = public.End
		child.index = i
		b.edit.put(child)
	}
	if in.Target != 0 {
		t.references[in.Target]++
	}
	b.edit.put(r)
	t.count++
	t.nextID = max(t.nextID, uint64(in.ID)+1)
	return nil
}
func (b *Builder) Finish(root uint32) (*Tree, error) {
	if b.tree == nil {
		return nil, errors.New("compact builder is closed")
	}
	t := b.tree
	b.tree = nil
	b.edit = mutation{}
	if b.err != nil {
		return nil, b.err
	}
	if err := b.ctx.Err(); err != nil {
		return nil, err
	}
	r, ok := t.get(root)
	if !ok || r.parent != 0 || r.index != -1 {
		return nil, errors.New("invalid compact root")
	}
	if r.count != t.count {
		return nil, errors.New("compact tree contains unreachable records")
	}
	for target := range t.references {
		if _, ok := t.get(target); !ok {
			return nil, errors.New("compact alias target is missing")
		}
	}
	r.index = 0
	t.chunks[root/chunkSize].rows[root%chunkSize] = r
	t.root, t.weight = root, r.subweight
	return t, nil
}
