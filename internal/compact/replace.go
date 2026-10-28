package compact

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"datatree/internal/textstore"
)

func (t *Tree) Replace(ctx context.Context, target uint32, replacement []byte, fragment []Input, fragmentRoot uint32) (*Tree, Change, error) {
	fail := func(err error) (*Tree, Change, error) { return nil, Change{}, err }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if !utf8.Valid(replacement) {
		return fail(errors.New("compact replacement must be UTF-8"))
	}
	old, ok := t.get(target)
	if !ok {
		return fail(errors.New("compact replacement target is missing"))
	}
	oldPublic, ok := t.resolve(old)
	if !ok {
		return fail(errors.New("compact target anchors are invalid"))
	}
	depth := 0
	affected := []uint32{target}
	for id := old.parent; id != 0; {
		r, ok := t.get(id)
		if !ok {
			return fail(errors.New("compact parent is missing"))
		}
		affected = append(affected, id)
		depth++
		id = r.parent
	}
	incoming, err := build(ctx, textstore.FromImmutable(replacement), fragment, fragmentRoot, t.maxNodes, t.maxDepth-depth, true)
	if err != nil {
		return fail(err)
	}
	rootRow, _ := incoming.get(fragmentRoot)
	remaining := t.weight - old.subweight
	keyWeight := old.weight - 1
	if remaining > t.maxNodes-rootRow.subweight || remaining+rootRow.subweight > t.maxNodes-keyWeight {
		return fail(errors.New("compact replacement exceeds global raw-node weight limit"))
	}
	newWeight := remaining + rootRow.subweight + keyWeight
	text, err := t.text.ReplaceBytes(oldPublic.Start, oldPublic.End, replacement)
	if err != nil {
		return fail(err)
	}
	next := &Tree{text: text, anchors: text.AnchorIndex(), chunks: make(map[uint32]*chunk, len(t.chunks)), root: t.root, maxNodes: t.maxNodes, maxDepth: t.maxDepth, count: t.count - old.count + rootRow.count, weight: newWeight, nextID: t.nextID, free: append([]uint32(nil), t.free...), references: make(map[uint32]int, len(t.references))}
	for id, c := range t.chunks {
		next.chunks[id] = c
	}
	for id, n := range t.references {
		next.references[id] = n
	}
	edit := mutation{tree: next, touched: map[uint32]bool{}}
	removed := map[uint32]bool{}
	stack := append([]uint32(nil), old.children...)
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		r, ok := t.get(id)
		if !ok {
			return fail(errors.New("compact descendant is missing"))
		}
		removed[id] = true
		stack = append(stack, r.children...)
		if r.target != 0 {
			next.references[r.target]--
		}
		edit.remove(id)
		next.free = append(next.free, id)
	}
	if old.target != 0 {
		next.references[old.target]--
	}
	for id := range removed {
		if next.references[id] > 0 {
			return fail(fmt.Errorf("compact replacement removes referenced target %d", id))
		}
	}
	remap := map[uint32]uint32{fragmentRoot: target}
	// Allocate all identities first so forward/cyclic aliases can be remapped.
	for _, input := range fragment {
		if input.ID == fragmentRoot {
			continue
		}
		id, err := edit.allocate()
		if err != nil {
			return fail(err)
		}
		remap[input.ID] = id
	}
	var insertErr error
	err = incoming.Preorder(ctx, func(r Record) bool {
		raw, _ := incoming.get(r.ID)
		raw.id = remap[r.ID]
		if r.ID == fragmentRoot {
			raw.parent = old.parent
			raw.index = old.index
			raw.label = old.label
			raw.pathable = old.pathable
			raw.ownDepth = old.ownDepth
			raw.height = max(raw.height, old.ownDepth)
			raw.weight += old.weight - 1
			raw.subweight += old.weight - 1
			raw.metadata = raw.metadata || old.metadata
		} else {
			raw.parent = remap[r.Parent]
		}
		raw.children = make([]uint32, len(raw.children))
		for i, id := range incoming.Children(r.ID, 0, r.ChildCount) {
			raw.children[i] = remap[id]
		}
		if raw.target != 0 {
			if mapped, ok := remap[raw.target]; ok {
				raw.target = mapped
			} else if _, ok := next.get(raw.target); !ok || removed[raw.target] {
				insertErr = errors.New("compact fragment alias target is missing")
				return false
			}
			next.references[raw.target]++
		}
		start, key, end := oldPublic.Start+r.Start, oldPublic.Start+r.KeyStart, oldPublic.Start+r.End
		raw.start, insertErr = text.AnchorAt(start)
		if insertErr != nil {
			return false
		}
		raw.keyStart, insertErr = text.AnchorAt(key)
		if insertErr != nil {
			return false
		}
		raw.end, insertErr = text.AnchorEnd(end)
		if insertErr != nil {
			return false
		}
		if r.ID == fragmentRoot {
			raw.hasKey = old.hasKey
			if old.hasKey {
				raw.keyStart = old.keyStart
			} else {
				raw.keyStart = raw.start
			}
		}
		edit.put(raw)
		return true
	})
	if err != nil {
		return fail(err)
	}
	if insertErr != nil {
		return fail(insertErr)
	}
	delta := len(replacement) - (oldPublic.End - oldPublic.Start)
	// Refresh ancestors even when an exclusive endpoint originally belonged to
	// a removed piece. Other rows' anchors move without changing their chunks.
	for ancestorIndex, id := range affected[1:] {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		original, _ := t.get(id)
		public, ok := t.resolve(original)
		if !ok {
			return fail(errors.New("compact ancestor anchors are invalid"))
		}
		moved := original
		start, key, end := public.Start, public.KeyStart, public.End
		if start > oldPublic.Start && start >= oldPublic.End {
			start += delta
		}
		if key > oldPublic.Start && key >= oldPublic.End {
			key += delta
		}
		if end >= oldPublic.End {
			end += delta
		}
		moved.start, err = text.AnchorAt(start)
		if err != nil {
			return fail(err)
		}
		moved.keyStart, err = text.AnchorAt(key)
		if err != nil {
			return fail(err)
		}
		moved.end, err = text.AnchorEnd(end)
		if err != nil {
			return fail(err)
		}
		replacedChild := affected[ancestorIndex]
		beforeChild, _ := t.get(replacedChild)
		afterChild, _ := next.get(replacedChild)
		moved.count = original.count + (afterChild.count - beforeChild.count)
		moved.subweight = original.subweight + (afterChild.subweight - beforeChild.subweight)
		moved.height = original.height
		if afterChild.height > beforeChild.height {
			moved.height = max(moved.height, afterChild.height+1)
		} else if afterChild.height < beforeChild.height && beforeChild.height+1 == original.height {
			moved.height = moved.ownDepth
			for _, childID := range moved.children {
				child, ok := next.get(childID)
				if !ok {
					return fail(errors.New("compact child disappeared during replacement"))
				}
				moved.height = max(moved.height, child.height+1)
			}
		}

		edit.put(moved)
	}
	newRoot, _ := next.get(next.root)
	if newRoot.height > t.maxDepth || newRoot.subweight > t.maxNodes {
		return fail(errors.New("compact replacement exceeds global depth or weight limit"))
	}
	next.count, next.weight = newRoot.count, newRoot.subweight
	for id, n := range next.references {
		if n <= 0 {
			delete(next.references, id)
		}
	}
	current, _ := next.Record(target)
	oldStart, e1 := t.text.Position(oldPublic.Start)
	oldEnd, e2 := t.text.Position(oldPublic.End)
	newStart, e3 := text.Position(current.Start)
	newEnd, e4 := text.Position(current.End)
	stable := e1 == nil && e2 == nil && e3 == nil && e4 == nil && current.Start == oldPublic.Start && current.End == oldPublic.End && oldStart.Line == oldEnd.Line && newStart.Line == newEnd.Line && oldStart.UTF16 == newStart.UTF16 && oldEnd.UTF16 == newEnd.UTF16 && oldEnd.Column-oldStart.Column == newEnd.Column-newStart.Column
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return next, Change{Affected: affected, RemovedRoots: append([]uint32(nil), old.children...), StableGeometry: stable}, nil
}
