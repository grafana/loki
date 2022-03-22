package graceful

import (
	"os"
	"os/signal"
	"syscall"
)

var (
	handlers []Handler
)

// Handler represents a function that is executed when an kill signal is
// received.
type Handler func()

// OnSignal registers a handler to be executed upon a kill signal.
func OnSignal(handler Handler) {
	handlers = append(handlers, handler)
}

func init() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for range c {
			for _, handler := range handlers {
				handler()
			}
			break
		}
	}()
}
