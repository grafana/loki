package storage

import (
	"errors"

	"github.com/prometheus/prometheus/util/annotations"
)

// errLabelValues is an empty LabelValues iterator with an error.
type errLabelValues struct {
	err      error
	warnings annotations.Annotations
}

func (e errLabelValues) Next() bool                        { return false }
func (e errLabelValues) At() string                        { return "" }
func (e errLabelValues) Err() error                        { return e.err }
func (e errLabelValues) Warnings() annotations.Annotations { return e.warnings }
func (e errLabelValues) Close() error                      { return nil }

// ErrLabelValues returns a LabelValues with err.
func ErrLabelValues(err error) LabelValues {
	if err == nil {
		return errLabelValues{err: errors.New("nil error provided")}
	}
	return errLabelValues{err: err}
}

var emptyLabelValues = errLabelValues{}

// EmptyLabelValues returns an empty LabelValues.
func EmptyLabelValues() LabelValues {
	return emptyLabelValues
}

// ListLabelValues is an iterator over a slice of label values.
type ListLabelValues struct {
	cur    string
	values []string
}

func NewListLabelValues(values []string) *ListLabelValues {
	return &ListLabelValues{
		values: values,
	}
}

func (l *ListLabelValues) Next() bool {
	if len(l.values) == 0 {
		return false
	}

	l.cur = l.values[0]
	l.values = l.values[1:]
	return true
}

func (l *ListLabelValues) At() string {
	return l.cur
}

func (*ListLabelValues) Err() error {
	return nil
}

func (*ListLabelValues) Warnings() annotations.Annotations {
	return nil
}

func (*ListLabelValues) Close() error {
	return nil
}
