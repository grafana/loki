package api

import (
	"sync"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
)

type Entry struct {
	Labels model.LabelSet
	logproto.Entry
}

type InstrumentedEntryHandler interface {
	EntryHandler
	UnregisterLatencyMetric(labels model.LabelSet)
}

// EntryHandler is something that can "handle" entries via a channel.
type EntryHandler interface {
	Chan() chan<- Entry
	Stop()
}

// EntryMiddleware takes a entry channel to intercept then and returns another one.
// Closing the new channel is required but will not close the inner channel in case it still in used.
type EntryMiddleware interface {
	Wrap(EntryHandler) EntryHandler
}

type EntryMiddlewareFunc func(EntryHandler) EntryHandler

func (e EntryMiddlewareFunc) Wrap(next EntryHandler) EntryHandler {
	return e(next)
}

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

func NewEntryHandler(entries chan<- Entry, stop func()) EntryHandler {
	return entryHandler{
		stop:    stop,
		entries: entries,
	}
}

func NewEntryMutatorHandler(next EntryHandler, f EntryMutatorFunc) EntryHandler {
	out, wg, once := make(chan Entry), sync.WaitGroup{}, sync.Once{}
	nextChan := next.Chan()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for e := range out {
			nextChan <- f(e)
		}
	}()
	return NewEntryHandler(out, func() {
		once.Do(func() { close(out) })
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
