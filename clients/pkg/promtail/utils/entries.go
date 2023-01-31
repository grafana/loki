package utils

import (
	"sync"

	"github.com/grafana/loki/clients/pkg/promtail/api"
)

// FanoutEntryHandler implements api.EntryHandler, fanning out received entries to one or multiple channels.
type FanoutEntryHandler struct {
	entries  chan api.Entry
	handlers []api.EntryHandler

	once sync.Once
	wg   sync.WaitGroup
}

func NewFanoutEntryHandler(handlers ...api.EntryHandler) *FanoutEntryHandler {
	eh := &FanoutEntryHandler{
		entries:  make(chan api.Entry),
		handlers: handlers,
	}
	eh.start()
	return eh
}

func (eh *FanoutEntryHandler) start() {
	eh.wg.Add(1)
	go func() {
		defer eh.wg.Done()
		for e := range eh.entries {
			// To prevent a single channel from blocking all others, we run each channel send in a separate go routine.
			// This cause each entry to be sent in |eh.handlers| routines concurrently. When all finish, we know the entry
			// has been fanned out properly, and we can read the next received entry.
			var entryWG sync.WaitGroup
			entryWG.Add(len(eh.handlers))
			for _, handler := range eh.handlers {
				go func(eh api.EntryHandler) {
					defer entryWG.Done()
					eh.Chan() <- e
				}(handler)
			}
			entryWG.Wait()
		}
	}()
}

func (eh *FanoutEntryHandler) Chan() chan<- api.Entry {
	return eh.entries
}

// Stop only stops the channel FanoutEntryHandler exposes, not the ones it fans out to.
func (eh *FanoutEntryHandler) Stop() {
	eh.once.Do(func() {
		close(eh.entries)
	})
	eh.wg.Wait()
}
