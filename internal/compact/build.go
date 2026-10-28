package compact

import (
	"context"
	"errors"
	"fmt"

	"datatree/internal/textstore"
)

func New(ctx context.Context, text textstore.Text, nodes []Input, root uint32, maxNodes, maxDepth int) (*Tree, error) {
	return build(ctx, text, nodes, root, maxNodes, maxDepth, false)
}
func build(ctx context.Context, text textstore.Text, nodes []Input, root uint32, maxNodes, maxDepth int, fragment bool) (*Tree, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxNodes <= 0 || maxDepth < 0 {
		return nil, errors.New("compact limits must be positive (depth may be zero)")
	}
	if root == 0 || len(nodes) == 0 {
		return nil, errors.New("compact tree requires a nonzero root")
	}
	if len(nodes) > maxNodes {
		return nil, errors.New("compact document exceeds node limit")
	}
	// Parser imports use consecutive preorder IDs. Index these directly rather
	// than allocating two document-sized hash tables during initial loading.
	dense := true
	for i := range nodes {
		if nodes[i].ID != uint32(i+1) {
			dense = false
			break
		}
	}
	var inputs map[uint32]*Input
	if !dense {
		inputs = make(map[uint32]*Input, len(nodes))
	}
	for i := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		in := &nodes[i]
		if in.ID == 0 {
			return nil, errors.New("compact node ID zero is invalid")
		}
		if _, exists := inputs[in.ID]; exists {
			return nil, fmt.Errorf("duplicate compact ID %d", in.ID)
		}
		if !dense {
			inputs[in.ID] = in
		}
	}
	lookup := func(id uint32) *Input {
		if dense {
			if id == 0 || uint64(id) > uint64(len(nodes)) {
				return nil
			}
			return &nodes[id-1]
		}
		return inputs[id]
	}
	if lookup(root) == nil {
		return nil, errors.New("compact root is missing")
	}
	if !fragment && lookup(root).Parent != 0 {
		return nil, errors.New("compact root cannot have a parent")
	}
	tree := &Tree{text: text, anchors: text.AnchorIndex(), chunks: map[uint32]*chunk{}, root: root, maxNodes: maxNodes, maxDepth: maxDepth, nextID: 1, references: map[uint32]int{}}
	edit := mutation{tree: tree, touched: map[uint32]bool{}}
	var state map[uint32]bool
	var denseState []bool
	if dense {
		denseState = make([]bool, len(nodes))
	} else {
		state = make(map[uint32]bool, len(nodes))
	}
	visited := 0
	var visit func(uint32, uint32, int, int) (row, error)
	visit = func(id, parent uint32, index, depth int) (row, error) {
		if err := ctx.Err(); err != nil {
			return row{}, err
		}
		if depth > maxDepth {
			return row{}, errors.New("compact document exceeds depth limit")
		}
		in := lookup(id)
		if in == nil {
			return row{}, fmt.Errorf("missing compact child %d", id)
		}
		if dense && denseState[id-1] || !dense && state[id] {
			return row{}, errors.New("compact hierarchy contains a cycle or repeated child")
		}
		if id != root && in.Parent != parent {
			return row{}, fmt.Errorf("compact parent mismatch for %d", id)
		}
		if dense {
			denseState[id-1] = true
		} else {
			state[id] = true
		}
		visited++
		k, err := parseKind(in.Kind)
		if err != nil {
			return row{}, err
		}
		if len(in.Children) > 0 && k != objectKind && k != arrayKind && k != streamKind {
			return row{}, errors.New("compact scalar cannot have children")
		}
		key := in.KeyStart
		if fragment && id == root {
			key = in.Start
		}
		if key < 0 {
			key = in.Start
		}
		if in.Start < 0 || key < 0 || key > in.Start || in.End < in.Start || in.End > text.ByteLen() {
			return row{}, fmt.Errorf("invalid compact source span for %d", id)
		}
		startAnchor, err := text.AnchorAt(in.Start)
		if err != nil {
			return row{}, err
		}
		keyAnchor, err := text.AnchorAt(key)
		if err != nil {
			return row{}, err
		}
		endAnchor, err := text.AnchorEnd(in.End)
		if err != nil {
			return row{}, err
		}
		ownWeight := max(1, in.Weight)
		ownDepth := in.OwnDepth
		if fragment && id == root {
			ownWeight = 1
			ownDepth = 0
		}
		if ownDepth < 0 || ownDepth > maxDepth-depth {
			return row{}, errors.New("compact mapping-key depth exceeds global limit")
		}
		r := row{ownDepth: ownDepth, height: ownDepth, id: id, parent: parent, target: in.Target, kind: k, label: in.Label, value: in.Value, start: startAnchor, keyStart: keyAnchor, end: endAnchor, index: index, indent: in.NativeIndent, weight: ownWeight, pathable: in.Pathable, metadata: in.HasMetadata, hasKey: key < in.Start}
		if r.weight > maxNodes {
			return row{}, errors.New("compact document exceeds raw-node weight limit")
		}
		r.children = append([]uint32(nil), in.Children...)
		r.count = 1
		r.subweight = r.weight
		previousEnd := in.Start
		for i, childID := range in.Children {
			childInput := lookup(childID)
			if childInput == nil {
				return row{}, fmt.Errorf("missing compact child %d", childID)
			}
			childStart := childInput.KeyStart
			if childStart < 0 {
				childStart = childInput.Start
			}
			if childStart < previousEnd || childInput.End > in.End {
				return row{}, fmt.Errorf("compact child %d lies outside ordered parent span", childID)
			}
			child, err := visit(childID, id, i, depth+1)
			if err != nil {
				return row{}, err
			}
			if r.subweight > maxNodes-child.subweight {
				return row{}, errors.New("compact document exceeds raw-node weight limit")
			}
			r.subweight += child.subweight
			r.count += child.count
			r.height = max(r.height, child.height+1)
			previousEnd = childInput.End
		}
		if in.Target != 0 {
			if !fragment && lookup(in.Target) == nil {
				return row{}, fmt.Errorf("compact alias target %d is missing", in.Target)
			}
			tree.references[in.Target]++
		}
		edit.put(r)
		return r, nil
	}
	rootRow, err := visit(root, 0, 0, 0)
	if err != nil {
		return nil, err
	}
	if visited != len(nodes) {
		return nil, errors.New("compact tree contains unreachable records")
	}
	tree.count, tree.weight = rootRow.count, rootRow.subweight
	return tree, nil
}
