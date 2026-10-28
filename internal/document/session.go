package document

import (
	"context"
	"errors"
	"unicode/utf8"

	"datatree/internal/compact"
	"datatree/internal/textstore"
)

type snapshot struct {
	tree         *compact.Tree
	headers      []string
	complexKeys  bool
	yamlMetadata bool
	format       string
	text         textstore.Text
	cost         int
}
type Session struct {
	Doc            *Document
	Original       []byte
	OriginalFormat string
	Revision       int
	undo, redo     []snapshot
	Limits         Limits
	text           textstore.Text
	textDoc        *Document
}

func NewSession(options ...Limits) *Session { return &Session{Limits: limitOption(options)} }

// Clone takes a private revision snapshot. Documents and source snapshots are
// immutable; history slice headers need separate backing arrays for append.
func (s *Session) Clone() *Session {
	copy := *s
	copy.undo = append([]snapshot(nil), s.undo...)
	copy.redo = append([]snapshot(nil), s.redo...)
	return &copy
}
func (s *Session) Parse(format string, source []byte) (*Document, error) {
	return s.ParseContext(context.Background(), format, source)
}
func (s *Session) ParseContext(ctx context.Context, format string, source []byte) (*Document, error) {
	return ParseWithLimitsContext(ctx, format, source, s.Limits)
}
func (s *Session) check(rev int) error {
	if rev != s.Revision {
		return errors.New("stale revision; reload the current document before editing")
	}
	return nil
}
func (s *Session) Import(format string, source []byte, revision int) error {
	return s.ImportContext(context.Background(), format, source, revision)
}
func (s *Session) ImportContext(ctx context.Context, format string, source []byte, revision int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.check(revision); err != nil {
		return err
	}
	d, err := s.ParseContext(ctx, format, source)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.Doc = d
	s.Original = d.Source
	s.OriginalFormat = d.Format
	s.undo = nil
	s.redo = nil
	if d.compact != nil {
		s.text = d.compact.Text()
	} else {
		s.text = textstore.FromImmutable(d.Source)
	}
	s.textDoc = d
	s.Revision++
	return nil
}
func (s *Session) commit(next *Document) {
	if next.compact != nil && next.localCost > 0 {
		s.commitText(next, next.compact.Text(), next.localCost)
		return
	}
	old := s.sourceText()
	a, b := s.Doc.Source, next.Source
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	for start > 0 && start < len(a) && !utf8.RuneStart(a[start]) {
		start--
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	for endA < len(a) && !utf8.RuneStart(a[endA]) {
		endA++
		endB++
	}
	text, err := old.ReplaceBytes(start, endA, b[start:endB])
	if err != nil {
		panic("validated document produced invalid source boundary: " + err.Error())
	}
	s.commitText(next, text, endA-start+endB-start)
}

// sourceText lazily initializes sessions restored from an immutable document.
func (s *Session) sourceText() textstore.Text {
	if s.textDoc != s.Doc {
		if s.Doc.compact != nil {
			s.text = s.Doc.compact.Text()
		} else {
			s.text = textstore.FromImmutable(s.Doc.Source)
		}
		s.textDoc = s.Doc
	}
	return s.text
}

func (s *Session) commitText(next *Document, text textstore.Text, cost int) {
	s.undo = append(s.undo, s.currentSnapshot(cost))
	size := 0
	for _, v := range s.undo {
		size += v.cost
	}
	for len(s.undo) > 0 && (len(s.undo) > 30 || size > 64*1024*1024) {
		size -= s.undo[0].cost
		s.undo[0] = snapshot{}
		s.undo = s.undo[1:]
	}
	s.redo = nil
	s.Doc = next
	s.text, s.textDoc = text, next
	s.Revision++
}

// Patch replaces ordered, non-overlapping normalized UTF-16 source ranges from
// one base revision. A parse failure leaves document, history and text untouched.
type Patch struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

func (s *Session) Patch(patches []Patch, revision int) error {
	return s.PatchContext(context.Background(), patches, revision)
}
func (s *Session) PatchContext(ctx context.Context, patches []Patch, revision int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.check(revision); err != nil {
		return err
	}
	if s.Doc == nil {
		return errors.New("open a file first")
	}
	if len(patches) == 0 {
		return errors.New("provide at least one patch")
	}
	baseText := s.sourceText()
	text := baseText
	for i, p := range patches {
		if p.Start < 0 || p.End < p.Start || (i > 0 && p.Start < patches[i-1].End) {
			return errors.New("patches must be ascending and non-overlapping")
		}
	}
	cost := 0
	for i := len(patches) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		p := patches[i]
		before := text.ByteLen()
		next, err := text.Replace(p.Start, p.End, []byte(p.Text))
		if err != nil {
			return err
		}
		cost += before - next.ByteLen() + 2*len(p.Text)
		text = next
	}
	if text.ByteLen() > s.Limits.Normalized().MaxBytes {
		return errors.New("patched source exceeds configured size limit")
	}
	var next *Document
	var err error
	if len(patches) == 1 {
		p := patches[0]
		start, e1 := baseText.ByteOffset(p.Start)
		end, e2 := baseText.ByteOffset(p.End)
		if e1 != nil {
			return e1
		}
		if e2 != nil {
			return e2
		}
		var handled bool
		next, handled, err = s.Doc.TryScalarPatchContext(ctx, start, end, p.Text)
		if err != nil {
			return err
		}
		if !handled {
			next = nil
		}
	}
	if next == nil {
		if s.Doc.compact != nil {
			next, err = ParseCompactWithLimitsContext(ctx, s.Doc.Format, text.Bytes(), s.Limits)
		} else {
			next, err = s.ParseContext(ctx, s.Doc.Format, text.Bytes())
		}
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if next.compact != nil {
		text = next.compact.Text()
	}
	s.commitText(next, text, cost)
	return nil
}
func (s *Session) Mutate(a Action, rev int) error {
	return s.MutateContext(context.Background(), a, rev)
}
func (s *Session) MutateContext(ctx context.Context, a Action, rev int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.check(rev); err != nil {
		return err
	}
	if s.Doc == nil {
		return errors.New("open a file first")
	}
	next, err := s.Doc.EditContext(ctx, a)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.commit(next)
	return nil
}
func (s *Session) Apply(format string, text []byte, rev int) error {
	return s.ApplyContext(context.Background(), format, text, rev)
}
func (s *Session) ApplyContext(ctx context.Context, format string, text []byte, rev int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.check(rev); err != nil {
		return err
	}
	if s.Doc == nil {
		return errors.New("open a file first")
	}
	if format != s.Doc.Format {
		return errors.New("use explicit conversion to change format")
	}
	next, err := s.ParseContext(ctx, format, text)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.commit(next)
	return nil
}
func (s *Session) Convert(format string, rev int) error {
	return s.ConvertWithSave(format, rev, nil)
}

// ConvertWithSave validates conversion before saving and commits only if saving
// succeeds. The callback receives the validated output and must not mutate it.
func (s *Session) ConvertWithSave(format string, rev int, save func([]byte) error) error {
	if err := s.check(rev); err != nil {
		return err
	}
	if s.Doc == nil {
		return errors.New("open a file first")
	}
	if format != "json" && format != "yaml" {
		return errors.New("conversion supports JSON and YAML")
	}
	b, err := s.Doc.conversionBytes(format)
	if err != nil {
		return err
	}
	next, err := s.Parse(format, b)
	if err != nil {
		return err
	}
	if save != nil {
		if err := save(next.Source); err != nil {
			return err
		}
	}
	s.commit(next)
	return nil
}
func (s *Session) Preview(format string) (string, error) {
	if s.Doc == nil {
		return "", errors.New("open a file first")
	}
	if format != "json" && format != "yaml" {
		return "", errors.New("conversion supports JSON and YAML")
	}
	b, err := s.Doc.conversionBytes(format)
	return string(b), err
}
func (s *Session) History(direction string, rev int) error {
	return s.HistoryContext(context.Background(), direction, rev)
}
func (s *Session) HistoryContext(ctx context.Context, direction string, rev int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.check(rev); err != nil {
		return err
	}
	if s.Doc == nil {
		return errors.New("open a file first")
	}
	from, to := &s.undo, &s.redo
	if direction == "redo" {
		from, to = &s.redo, &s.undo
	} else if direction != "undo" {
		return errors.New("invalid history direction")
	}
	if len(*from) == 0 {
		return errors.New("no history in that direction")
	}
	previous := (*from)[len(*from)-1]
	var d *Document
	var err error
	if previous.tree != nil {
		d = compactDocument(previous.format, previous.text.Bytes(), previous.headers, s.Limits, previous.tree, previous.complexKeys)
		d.compactYAMLMetadata = previous.yamlMetadata
	} else {
		d, err = s.ParseContext(ctx, previous.format, previous.text.Bytes())
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	(*from)[len(*from)-1] = snapshot{}
	*from = (*from)[:len(*from)-1]
	*to = append(*to, s.currentSnapshot(previous.cost))
	s.Doc = d
	s.text, s.textDoc = previous.text, d
	s.Revision++
	return nil
}
func (s *Session) CanUndo() bool { return len(s.undo) > 0 }
func (s *Session) CanRedo() bool { return len(s.redo) > 0 }

// RevertDiff commits one validated inverse and leaves the comparison baseline alone.
func (s *Session) RevertDiff(base *Document, index, revision int) error {
	if err := s.check(revision); err != nil {
		return err
	}
	if s.Doc == nil {
		return errors.New("open a file first")
	}
	next, err := s.Doc.RevertDiff(base, index)
	if err != nil {
		return err
	}
	s.commit(next)
	return nil
}

func (s *Session) currentSnapshot(cost int) snapshot {
	return snapshot{format: s.Doc.Format, text: s.sourceText(), cost: cost, tree: s.Doc.compact, headers: s.Doc.headers, complexKeys: s.Doc.compactComplexKeys, yamlMetadata: s.Doc.compactYAMLMetadata}
}
