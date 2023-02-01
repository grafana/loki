package utils

import (
	"context"
	"sync"
	"time"

	"github.com/grafana/loki/clients/pkg/promtail/api"
)

// FanoutEntryHandler implements api.EntryHandler, fanning out received entries to one or multiple channels.
type FanoutEntryHandler struct {
	entries  chan api.Entry
	handlers []api.EntryHandler

	once            sync.Once
	mainRoutineDone chan struct{}
	cancelTimeout   time.Duration
	cancel          context.CancelFunc
}

func NewFanoutEntryHandler(sendTimeoutOnStop time.Duration, handlers ...api.EntryHandler) *FanoutEntryHandler {
	ctx, cancel := context.WithCancel(context.Background())
	eh := &FanoutEntryHandler{
		entries:         make(chan api.Entry),
		handlers:        handlers,
		mainRoutineDone: make(chan struct{}),
		cancelTimeout:   sendTimeoutOnStop,
		cancel:          cancel,
	}
	eh.start(ctx)
	return eh
}

func (eh *FanoutEntryHandler) start(ctx context.Context) {
	go func() {
		defer func() {
			close(eh.mainRoutineDone)
		}()

		for e := range eh.entries {
			// To prevent a single channel from blocking all others, we run each channel send in a separate go routine.
			// This cause each entry to be sent in |eh.handlers| routines concurrently. When all finish, we know the entry
			// has been fanned out properly, and we can read the next received entry.
			var entryWG sync.WaitGroup
			entryWG.Add(len(eh.handlers))
			for _, handler := range eh.handlers {
				go func(ctx context.Context, eh api.EntryHandler) {
					defer entryWG.Done()
					select {
					case <-ctx.Done():
					case eh.Chan() <- e:
					}
				}(ctx, handler)
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
	// after closing channel, if the main go-routine was sending entries, wait for them to be sent, or a timeout fires
	select {
	case <-eh.mainRoutineDone:
		// graceful stop
	case <-time.After(eh.cancelTimeout):
		// sending pending entries timed out, cancel
		eh.cancel()
	}
}
