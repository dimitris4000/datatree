package document

import (
	"bytes"
	"errors"
)

type snapshot struct {
	format string
	source []byte
}
type Session struct {
	Doc            *Document
	Original       []byte
	OriginalFormat string
	Revision       int
	undo, redo     []snapshot
}

func NewSession() *Session { return &Session{} }
func (s *Session) check(rev int) error {
	if rev != s.Revision {
		return errors.New("stale revision; reload the current document before editing")
	}
	return nil
}
func (s *Session) Import(format string, source []byte, revision int) error {
	if err := s.check(revision); err != nil {
		return err
	}
	d, err := Parse(format, source)
	if err != nil {
		return err
	}
	s.Doc = d
	s.Original = bytes.Clone(source)
	s.OriginalFormat = d.Format
	s.undo = nil
	s.redo = nil
	s.Revision++
	return nil
}
func (s *Session) commit(next *Document) {
	s.undo = append(s.undo, snapshot{s.Doc.Format, s.Doc.Source})
	size := len(next.Source)
	for _, v := range s.undo {
		size += len(v.source)
	}
	for len(s.undo) > 0 && (len(s.undo) > 30 || size > 64*1024*1024) {
		size -= len(s.undo[0].source)
		s.undo = s.undo[1:]
	}
	s.redo = nil
	s.Doc = next
	s.Revision++
}
func (s *Session) Mutate(a Action, rev int) error {
	if err := s.check(rev); err != nil {
		return err
	}
	if s.Doc == nil {
		return errors.New("open a file first")
	}
	next, err := s.Doc.Edit(a)
	if err != nil {
		return err
	}
	s.commit(next)
	return nil
}
func (s *Session) Apply(format string, text []byte, rev int) error {
	if err := s.check(rev); err != nil {
		return err
	}
	if s.Doc == nil {
		return errors.New("open a file first")
	}
	if format != s.Doc.Format {
		return errors.New("use explicit conversion to change format")
	}
	next, err := Parse(format, text)
	if err != nil {
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
	next, err := Parse(format, b)
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
	d, err := Parse(previous.format, previous.source)
	if err != nil {
		return err
	}
	*from = (*from)[:len(*from)-1]
	*to = append(*to, snapshot{s.Doc.Format, s.Doc.Source})
	s.Doc = d
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
