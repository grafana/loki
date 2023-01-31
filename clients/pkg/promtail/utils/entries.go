package utils

import (
	"sync"

	"github.com/grafana/loki/clients/pkg/promtail/api"
)

// EntryHandlerFanouter implements api.EntryHandler, fanning out received entries to one or multiple channels.
type EntryHandlerFanouter struct {
	entries  chan api.Entry
	handlers []api.EntryHandler

	once sync.Once
	wg   sync.WaitGroup
}

func NewEntryHandlerFanouter(handlers ...api.EntryHandler) *EntryHandlerFanouter {
	multiplex := &EntryHandlerFanouter{
		entries:  make(chan api.Entry),
		handlers: handlers,
	}
	multiplex.wg.Add(1)
	go func() {
		defer multiplex.wg.Done()
		for e := range multiplex.entries {
			for _, handler := range multiplex.handlers {
				handler.Chan() <- e
			}
		}
	}()
	return multiplex
}

func (eh *EntryHandlerFanouter) Chan() chan<- api.Entry {
	return eh.entries
}

// Stop only stops the channel EntryHandlerFanouter exposes, not the ones it fans out to.
func (eh *EntryHandlerFanouter) Stop() {
	eh.once.Do(func() {
		close(eh.entries)
	})
	eh.wg.Wait()
}
