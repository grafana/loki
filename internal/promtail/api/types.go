package api

import (
	"time"

	"github.com/prometheus/common/model"
)

// EntryHandler is something that can "handle" entries.
type EntryHandler interface {
	Handle(labels model.LabelSet, time time.Time, entry string) error
}

// EntryHandlerFunc is modelled on http.HandlerFunc.
type EntryHandlerFunc func(labels model.LabelSet, time time.Time, entry string) error

// Handle implements EntryHandler.
func (e EntryHandlerFunc) Handle(labels model.LabelSet, time time.Time, entry string) error {
	return e(labels, time, entry)
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
