package document

import (
	"bytes"
	"encoding/csv"
	"sort"
	"unicode/utf8"
)

// SourceRange uses normalized textarea UTF-16 offsets, including mapping keys.
// Parent and Index let the browser reveal a node without fetching the entire tree.
type SourceRange struct {
	ID     string `json:"id"`
	Parent string `json:"parent,omitempty"`
	Index  int    `json:"index"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
}

func (d *Document) buildSourceRanges() []SourceRange {
	result := make([]SourceRange, 0, len(d.Nodes))
	var starts []int
	if d.Format == "csv" {
		starts = lineStarts(d.Source)
	}
	var walk func(*Node, string, int)
	walk = func(n *Node, parent string, index int) {
		start, end := n.Start, n.End
		if n.key != nil {
			start = n.keyStart
		}
		if n == d.Root {
			start, end = 0, len(d.Source)
		}
		result = append(result, SourceRange{ID: n.ID, Parent: parent, Index: index, Start: start, End: end})
		for i, c := range n.Children {
			walk(c, n.ID, i)
		}
	}
	walk(d.Root, "", 0)
	if d.Format == "csv" {
		d.csvRanges(result, starts)
	}
	// Convert only requested boundaries in a single pass, avoiding a per-byte map.
	boundaries := make([]int, 0, len(result)*2)
	for _, r := range result {
		boundaries = append(boundaries, r.Start, r.End)
	}
	sort.Ints(boundaries)
	converted := make(map[int]int, len(boundaries))
	pos, units := 0, 0
	for _, boundary := range boundaries {
		for pos < boundary {
			r, size := utf8.DecodeRune(d.Source[pos:])
			if r == '\r' && pos+1 < len(d.Source) && d.Source[pos+1] == '\n' {
				pos++
				continue
			}
			units++
			if r > 0xffff {
				units++
			}
			pos += size
		}
		converted[boundary] = units
	}
	for i := range result {
		result[i].Start = converted[result[i].Start]
		result[i].End = converted[result[i].End]
	}
	return result
}

func (d *Document) csvRanges(result []SourceRange, starts []int) {
	byID := make(map[string]int, len(result))
	for i, r := range result {
		byID[r.ID] = i
	}
	reader := csv.NewReader(bytes.NewReader(d.Source))
	if _, err := reader.Read(); err != nil {
		return
	}
	for _, row := range d.Root.Children {
		if _, err := reader.Read(); err != nil {
			return
		}
		rowStart, rowEnd := len(d.Source), 0
		for j, cell := range row.Children {
			line, col := reader.FieldPos(j)
			start := starts[line-1] + col - 1
			end := start
			if start < len(d.Source) && d.Source[start] == '"' {
				end++
				for end < len(d.Source) {
					if d.Source[end] == '"' {
						end++
						if end < len(d.Source) && d.Source[end] == '"' {
							end++
							continue
						}
						break
					}
					end++
				}
			} else {
				for end < len(d.Source) && d.Source[end] != ',' && d.Source[end] != '\r' && d.Source[end] != '\n' {
					end++
				}
			}
			i := byID[cell.ID]
			result[i].Start = start
			result[i].End = end
			if start < rowStart {
				rowStart = start
			}
			if end > rowEnd {
				rowEnd = end
			}
		}
		i := byID[row.ID]
		result[i].Start = rowStart
		result[i].End = rowEnd
	}
}

// Ancestor identifies a collection and the child page needed to reveal a node.
type Ancestor struct {
	ID     string `json:"id"`
	Offset int    `json:"offset"`
}

type sourceIndex struct {
	ranges []SourceRange
	byID   map[string]SourceRange
	sorted []SourceRange
	maxEnd []int
}

func (d *Document) indexedSource() *sourceIndex {
	d.sourceIndexOnce.Do(func() {
		if d.sourceIndex.Load() != nil {
			return
		}
		ranges := d.buildSourceRanges()
		idx := &sourceIndex{ranges: ranges, byID: make(map[string]SourceRange, len(ranges)), sorted: append([]SourceRange(nil), ranges...)}
		for _, r := range ranges {
			idx.byID[r.ID] = r
		}
		sort.SliceStable(idx.sorted, func(i, j int) bool { return idx.sorted[i].Start < idx.sorted[j].Start })
		idx.maxEnd = make([]int, 4*len(ranges))
		var build func(int, int, int) int
		build = func(node, lo, hi int) int {
			if hi-lo == 1 {
				idx.maxEnd[node] = idx.sorted[lo].End
				return idx.maxEnd[node]
			}
			mid := (lo + hi) / 2
			idx.maxEnd[node] = max(build(node*2, lo, mid), build(node*2+1, mid, hi))
			return idx.maxEnd[node]
		}
		if len(ranges) > 0 {
			build(1, 0, len(ranges))
		}
		d.sourceIndex.Store(idx)
	})
	return d.sourceIndex.Load()
}

// SourceRanges returns a caller-owned compatibility snapshot. Prefer SourceRange
// and Locate for navigation so a request does not copy the whole source map.
func (d *Document) SourceRanges() []SourceRange {
	if d.compact != nil {
		return d.compactRanges()
	}
	return append([]SourceRange(nil), d.indexedSource().ranges...)
}
func (d *Document) SourceRange(id string) (SourceRange, bool) {
	if d.compact != nil {
		return d.compactSourceRange(id)
	}
	r, ok := d.indexedSource().byID[id]
	return r, ok
}

// Locate returns the narrowest node containing a normalized UTF-16 selection.
func (d *Document) Locate(start, end int) (SourceRange, bool) {
	if d.compact != nil {
		return d.compactLocate(start, end)
	}
	if start < 0 || end < start {
		return SourceRange{}, false
	}
	idx := d.indexedSource()
	var best SourceRange
	found := false
	var visit func(int, int, int)
	visit = func(node, lo, hi int) {
		if idx.maxEnd[node] < end || idx.sorted[lo].Start > start {
			return
		}
		if hi-lo == 1 {
			r := idx.sorted[lo]
			if r.Start <= start && r.End >= end && !(start == end && start == r.End && r.Start < r.End && r.ID != d.Root.ID) && (!found || r.End-r.Start < best.End-best.Start) {
				best = r
				found = true
			}
			return
		}
		mid := (lo + hi) / 2
		visit(node*2+1, mid, hi)
		visit(node*2, lo, mid)
	}
	if len(idx.sorted) > 0 {
		visit(1, 0, len(idx.sorted))
	}
	return best, found
}

func (d *Document) Ancestors(id string, pageSize int) []Ancestor {
	if d.compact != nil {
		return d.compactAncestors(id, pageSize)
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	idx := d.indexedSource()
	current, ok := idx.byID[id]
	if !ok {
		return []Ancestor{}
	}
	result := []Ancestor{}
	for current.Parent != "" {
		result = append(result, Ancestor{ID: current.Parent, Offset: current.Index / pageSize * pageSize})
		current = idx.byID[current.Parent]
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}
