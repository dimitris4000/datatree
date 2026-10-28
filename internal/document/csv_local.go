package document

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"

	"datatree/internal/textstore"
)

// CSV cells are independent quoted fields. Patch only that field: regenerating
// every record would normalize unrelated line endings and exhaust large-file
// history for a one-cell change.
func (d *Document) replaceCSVCell(ctx context.Context, target *Node, a Action) (*Document, error) {
	format := a.Format
	if format == "" {
		format = "json"
	}
	incoming, err := ParseWithLimitsContext(ctx, format, []byte(a.Text), d.Limits())
	if err != nil {
		return nil, err
	}
	if incoming.Root.Kind != "string" {
		return nil, errors.New("CSV cells must remain strings")
	}
	var encoded bytes.Buffer
	w := csv.NewWriter(&encoded)
	if err := w.Write([]string{incoming.Root.Value}); err != nil {
		return nil, err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	field := encoded.Bytes()
	field = field[:len(field)-1] // single record's final LF
	span, ok := d.SourceRange(target.ID)
	if !ok {
		return nil, errors.New("CSV field not found")
	}
	text := textstore.FromImmutable(d.Source)
	patched, err := text.Replace(span.Start, span.End, field)
	if err != nil {
		return nil, err
	}
	return ParseWithLimitsContext(ctx, d.Format, patched.Bytes(), d.Limits())
}
