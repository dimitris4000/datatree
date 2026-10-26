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

func (d *Document) SourceRanges() []SourceRange {
	result := make([]SourceRange, 0, len(d.Nodes))
	starts := lineStarts(d.Source)
	columns := columnOffsets(d.Source, starts)
	var walk func(*Node, string, int)
	walk = func(n *Node, parent string, index int) {
		start, end := n.Start, n.End
		if n.key != nil {
			start = offset(d.Source, starts, columns, n.key.Line, n.key.Column)
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
