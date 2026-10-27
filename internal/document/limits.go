package document

import (
	"errors"
	"math"
)

// Limits are copied into each document/session; independent servers never
// modify process-global parser settings. Zero fields select the defaults.
type Limits struct {
	MaxBytes int `json:"maxBytes"`
	MaxNodes int `json:"maxNodes"`
}

func DefaultLimits() Limits { return Limits{MaxBytes: MaxBytes, MaxNodes: MaxNodes} }
func (l Limits) Normalized() Limits {
	if l.MaxBytes == 0 {
		l.MaxBytes = MaxBytes
	}
	if l.MaxNodes == 0 {
		l.MaxNodes = MaxNodes
	}
	return l
}
func (l Limits) Validate() error {
	if l.MaxBytes <= 0 || l.MaxNodes <= 0 {
		return errors.New("file-size and parsed-node limits must be positive")
	}
	if int64(l.MaxBytes) > (math.MaxInt64-6*MaxSchemaBytes-8192)/6 {
		return errors.New("file-size limit is too large")
	}
	return nil
}
func limitOption(options []Limits) Limits {
	if len(options) > 0 {
		return options[0].Normalized()
	}
	return DefaultLimits()
}
func (d *Document) Limits() Limits { return d.limits.Normalized() }
func (d *Document) parse(format string, source []byte) (*Document, error) {
	return ParseWithLimits(format, source, d.Limits())
}
