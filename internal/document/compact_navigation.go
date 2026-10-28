package document

import (
	"context"

	"datatree/internal/compact"
)

func (d *Document) compactSourceRange(id string) (SourceRange, bool) {
	r, ok := d.compact.Record(compactID(id))
	if !ok {
		return SourceRange{}, false
	}
	start, end := r.Start, r.End
	if r.KeyStart >= 0 {
		start = r.KeyStart
	}
	if r.ID == d.compact.Root() {
		start, end = 0, len(d.Source)
	}
	a, e1 := d.compact.Text().Position(start)
	b, e2 := d.compact.Text().Position(end)
	if e1 != nil || e2 != nil {
		return SourceRange{}, false
	}
	return SourceRange{ID: id, Parent: publicID(r.Parent), Index: r.Index, Start: a.UTF16, End: b.UTF16}, true
}
func (d *Document) compactLocate(start, end int) (SourceRange, bool) {
	if start < 0 || end < start {
		return SourceRange{}, false
	}
	a, e1 := d.compact.Text().ByteOffset(start)
	b, e2 := d.compact.Text().ByteOffset(end)
	if e1 != nil || e2 != nil {
		return SourceRange{}, false
	}
	id, ok := d.compact.LocateByte(a, b)
	if !ok {
		return d.compactSourceRange(publicID(d.compact.Root()))
	}
	return d.compactSourceRange(publicID(id))
}
func (d *Document) compactAncestors(id string, pageSize int) []Ancestor {
	if pageSize <= 0 {
		pageSize = 20
	}
	r, ok := d.compact.Record(compactID(id))
	result := []Ancestor{}
	if !ok {
		return result
	}
	for r.Parent != 0 {
		result = append(result, Ancestor{ID: publicID(r.Parent), Offset: r.Index / pageSize * pageSize})
		r, ok = d.compact.Record(r.Parent)
		if !ok {
			break
		}
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}
func (d *Document) compactRanges() []SourceRange {
	result := make([]SourceRange, 0, d.NodeCount())
	_ = d.compact.Preorder(context.Background(), func(r compact.Record) bool {
		span, ok := d.compactSourceRange(publicID(r.ID))
		if ok {
			result = append(result, span)
		}
		return true
	})
	return result
}
