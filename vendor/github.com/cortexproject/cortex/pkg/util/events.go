package util

import (
	"os"
	"sync/atomic"

	"github.com/go-kit/kit/log"
)

// Provide an "event" interface for observability

// Temporary hack implementation to go via logger to stderr

var (
	// interface{} vars to avoid allocation on every call
	key   interface{} = "level" // masquerade as a level like debug, warn
	event interface{} = "event"

	eventLogger = log.NewNopLogger()
)

// Event is the log-like API for event sampling
func Event() log.Logger {
	return eventLogger
}

// InitEvents initializes event sampling, with the given frequency. Zero=off.
func InitEvents(freq int) {
	if freq <= 0 {
		eventLogger = log.NewNopLogger()
	} else {
		eventLogger = newEventLogger(freq)
	}
}

func newEventLogger(freq int) log.Logger {
	l := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	l = log.WithPrefix(l, key, event)
	l = log.With(l, "ts", log.DefaultTimestampUTC)
	return &samplingFilter{next: l, freq: freq}
}

type samplingFilter struct {
	next  log.Logger
	freq  int
	count int64
}

func (e *samplingFilter) Log(keyvals ...interface{}) error {
	count := atomic.AddInt64(&e.count, 1)
	if count%int64(e.freq) == 0 {
		return e.next.Log(keyvals...)
	}
	return nil
}
