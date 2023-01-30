package utils

import (
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"sync"
)

// EntryHandlerFanouter implements api.EntryHandler, fanning out received entries to one or multiple channels.
type EntryHandlerFanouter struct {
	entries  chan api.Entry
	handlers []api.EntryHandler

	once sync.Once
	wg   sync.WaitGroup
}

func (x *EntryHandlerFanouter) Chan() chan<- api.Entry {
	return x.entries
}

// Stop only stops the channel EntryHandlerFanouter exposes, not the ones it fans out to.
func (x *EntryHandlerFanouter) Stop() {
	x.once.Do(func() {
		close(x.entries)
	})
	x.wg.Wait()
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
