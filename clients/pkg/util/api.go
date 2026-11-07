package util

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/push"
)

// Entry is a log entry with labels.
type Entry struct {
	Labels model.LabelSet
	push.Entry
}

type InstrumentedEntryHandler interface {
	EntryHandler
	UnregisterLatencyMetric(prometheus.Labels)
}

// EntryHandler is something that can "handle" entries via a channel.
// Stop must be called to gracefully shutdown the EntryHandler
type EntryHandler interface {
	Chan() chan<- Entry
	Stop()
}

type entryHandler struct {
	stop    func()
	entries chan<- Entry
}

func (e entryHandler) Chan() chan<- Entry {
	return e.entries
}

func (e entryHandler) Stop() {
	e.stop()
}

// NewEntryHandler creates a new EntryHandler using a input channel and a stop function.
func NewEntryHandler(entries chan<- Entry, stop func()) EntryHandler {
	return entryHandler{
		stop:    stop,
		entries: entries,
	}
}
