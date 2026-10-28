package document

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"datatree/internal/compact"
	"datatree/internal/textstore"
)

// parseCompactCSV streams validated rows directly into final records. The
// current source remains immutable; no YAML or application graph is created.
func parseCompactCSV(ctx context.Context, source []byte, limits Limits) (*Document, error) {
	owned := bytes.Clone(source)
	builder, err := compact.NewBuilder(ctx, textstore.FromImmutable(owned), limits.MaxNodes, MaxDepth+1)
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(contextReader{ctx, bytes.NewReader(source)})
	reader.ReuseRecord = true
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("CSV header: %w", err)
	}
	headers = append([]string(nil), headers...)
	seen := make(map[string]bool, len(headers))
	for _, header := range headers {
		if strings.TrimSpace(header) == "" || seen[header] {
			return nil, errors.New("CSV headers must be nonempty and unique")
		}
		seen[header] = true
	}
	starts := lineStarts(source)
	var rows []uint32
	nextID, rawCount := uint64(2), 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		rowWeight := 1 + 2*len(headers)
		if rowWeight > limits.MaxNodes-rawCount {
			return nil, fmt.Errorf("document exceeds configured node limit (%d)", limits.MaxNodes)
		}
		rawCount += rowWeight
		if nextID+uint64(len(headers)) > uint64(^uint32(0)) {
			return nil, errors.New("compact node ID limit exceeded")
		}
		rowID := uint32(nextID)
		nextID++
		children := make([]uint32, len(headers))
		rowStart, rowEnd := len(source), 0
		for i, value := range record {
			line, column := reader.FieldPos(i)
			start := starts[line-1] + column - 1
			end := csvFieldEnd(source, start)
			id := uint32(nextID)
			nextID++
			children[i] = id
			in := compact.Input{ID: id, Parent: rowID, Kind: "string", Label: headers[i], Value: value, Start: start, KeyStart: -1, End: end, Pathable: true, Weight: 2}
			if err := builder.Add(in); err != nil {
				return nil, err
			}
			rowStart = min(rowStart, start)
			rowEnd = max(rowEnd, end)
		}
		if err := builder.Add(compact.Input{ID: rowID, Parent: 1, Kind: "object", Label: "[" + strconv.Itoa(len(rows)) + "]", Start: rowStart, KeyStart: -1, End: rowEnd, Children: children, Pathable: true, Weight: 1}); err != nil {
			return nil, err
		}
		rows = append(rows, rowID)
	}
	if err := builder.Add(compact.Input{ID: 1, Kind: "array", Label: "$", Start: 0, KeyStart: -1, End: len(source), Children: rows, Pathable: true, Weight: 1}); err != nil {
		return nil, err
	}
	tree, err := builder.Finish(1)
	if err != nil {
		return nil, err
	}
	return compactDocument("csv", owned, headers, limits, tree, false), nil
}

func csvFieldEnd(source []byte, start int) int {
	end := start
	if start < len(source) && source[start] == '"' {
		end++
		for end < len(source) {
			if source[end] == '"' {
				end++
				if end < len(source) && source[end] == '"' {
					end++
					continue
				}
				break
			}
			end++
		}
	} else {
		for end < len(source) && source[end] != ',' && source[end] != '\r' && source[end] != '\n' {
			end++
		}
	}
	return end
}
