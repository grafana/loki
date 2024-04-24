package api

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// Entry is a log entry with labels.
type Entry struct {
	Labels model.LabelSet
	logproto.Entry
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

// EntryMiddleware takes an EntryHandler and returns another one that will intercept and forward entries.
// The newly created EntryHandler should be Stopped independently from the original one.
type EntryMiddleware interface {
	Wrap(EntryHandler) EntryHandler
}

// EntryMiddlewareFunc allows to create EntryMiddleware via a function.
type EntryMiddlewareFunc func(EntryHandler) EntryHandler

func (e EntryMiddlewareFunc) Wrap(next EntryHandler) EntryHandler {
	return e(next)
}

// EntryMutatorFunc is a function that can mutate an entry
type EntryMutatorFunc func(Entry) Entry

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

// NewEntryMutatorHandler creates a EntryHandler that mutates incoming entries from another EntryHandler.
func NewEntryMutatorHandler(next EntryHandler, f EntryMutatorFunc) EntryHandler {
	in, wg, once := make(chan Entry), sync.WaitGroup{}, sync.Once{}
	nextChan := next.Chan()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for e := range in {
			nextChan <- f(e)
		}
	}()
	return NewEntryHandler(in, func() {
		once.Do(func() { close(in) })
		wg.Wait()
	})
}

// AddLabelsMiddleware is an EntryMiddleware that adds some labels.
func AddLabelsMiddleware(additionalLabels model.LabelSet) EntryMiddleware {
	return EntryMiddlewareFunc(func(eh EntryHandler) EntryHandler {
		return NewEntryMutatorHandler(eh, func(e Entry) Entry {
			e.Labels = additionalLabels.Merge(e.Labels)
			return e
		})
	})
}
