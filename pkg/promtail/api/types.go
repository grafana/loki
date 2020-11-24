package api

import (
	"time"

	"github.com/prometheus/common/model"
)

type Entry struct {
	Labels    model.LabelSet
	Timestamp time.Time
	Line      string
}

type InstrumentedEntryHandler interface {
	EntryHandler
	UnregisterLatencyMetric(labels model.LabelSet)
}

// EntryHandler is something that can "handle" entries.
type EntryHandler interface {
	Run(chan<- Entry)
}

// EntryMiddleware is something that takes on EntryHandler and produces another.
type EntryMiddleware interface {
	Wrap(next EntryHandler) EntryHandler
}

// EntryMiddlewareFunc is modelled on http.HandlerFunc.
type EntryMiddlewareFunc func(next EntryHandler) EntryHandler

// Wrap implements EntryMiddleware.
func (e EntryMiddlewareFunc) Wrap(next EntryHandler) EntryHandler {
	return e(next)
}

// AddLabelsMiddleware is an EntryMiddleware that adds some labels.
func AddLabelsMiddleware(additionalLabels model.LabelSet) EntryMiddleware {
	return EntryMiddlewareFunc(func(next EntryHandler) EntryHandler {
		return EntryHandlerFunc(func(labels model.LabelSet, time time.Time, entry string) error {
			labels = additionalLabels.Merge(labels) // Add the additionalLabels but preserves the original labels.
			return next.Handle(labels, time, entry)
		})
	})
}
