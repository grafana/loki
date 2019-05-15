package comparator

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	ErrOutOfOrderEntry  = "entry %s was received before entries: %v\n"
	ErrEntryNotReceived = "failed to receive entry %s within %f seconds\n"
)

var (
	totalEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "total_entries",
		Help:      "counts log entries written to the file",
	})
	outOfOrderEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "out_of_order_entries",
		Help:      "counts log entries received with a timestamp more recent than the others in the queue",
	})
	missingEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "missing_entries",
		Help:      "counts log entries not received within the maxWait duration and is reported as missing",
	})
	unexpectedEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "unexpected_entries",
		Help:      "counts a log entry received which was not expected (e.g. duplicate, received after reported missing)",
	})
	responseLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki_canary",
		Name:      "response_latency",
		Help:      "is how long it takes for log lines to be returned from Loki in seconds.",
		Buckets:   []float64{1, 5, 10, 30, 60, 90, 120, 300},
	})
)

type Comparator struct {
	entMtx        sync.Mutex
	w             io.Writer
	entries       []*time.Time
	maxWait       time.Duration
	pruneInterval time.Duration
	quit          chan struct{}
	done          chan struct{}
}

func NewComparator(writer io.Writer, maxWait time.Duration, pruneInterval time.Duration) *Comparator {
	c := &Comparator{
		w:             writer,
		entries:       []*time.Time{},
		maxWait:       maxWait,
		pruneInterval: pruneInterval,
		quit:          make(chan struct{}),
		done:          make(chan struct{}),
	}

	go c.run()

	return c
}

func (c *Comparator) Stop() {
	close(c.quit)
	<-c.done
}

func (c *Comparator) EntrySent(time time.Time) {
	c.entMtx.Lock()
	defer c.entMtx.Unlock()
	c.entries = append(c.entries, &time)
	totalEntries.Inc()
}

// EntryReceived removes the received entry from the buffer if it exists, reports on out of order entries received
func (c *Comparator) EntryReceived(ts time.Time) {
	c.entMtx.Lock()
	defer c.entMtx.Unlock()

	// Output index
	k := 0
	matched := false
	for i, e := range c.entries {
		if ts.Equal(*e) {
			matched = true
			// If this isn't the first item in the list we received it out of order
			if i != 0 {
				outOfOrderEntries.Inc()
				_, _ = fmt.Fprintf(c.w, ErrOutOfOrderEntry, e, c.entries[:i])
			}
			responseLatency.Observe(time.Now().Sub(ts).Seconds())
			// Do not increment output index, effectively causing this element to be dropped
		} else {
			// If the current index doesn't match the output index, update the array with the correct position
			if i != k {
				c.entries[k] = c.entries[i]
			}
			k++
		}
	}
	if !matched {
		unexpectedEntries.Inc()
	}
	// Nil out the pointers to any trailing elements which were removed from the slice
	for i := k; i < len(c.entries); i++ {
		c.entries[i] = nil // or the zero value of T
	}
	c.entries = c.entries[:k]
}

func (c *Comparator) Size() int {
	return len(c.entries)
}

func (c *Comparator) run() {
	t := time.NewTicker(c.pruneInterval)
	defer func() {
		t.Stop()
		close(c.done)
	}()

	for {
		select {
		case <-t.C:
			c.pruneEntries()
		case <-c.quit:
			return
		}
	}
}

func (c *Comparator) pruneEntries() {
	c.entMtx.Lock()
	defer c.entMtx.Unlock()

	k := 0
	for i, e := range c.entries {
		// If the time is outside our range, assume the entry has been lost report and remove it
		if e.Before(time.Now().Add(-c.maxWait)) {
			missingEntries.Inc()
			_, _ = fmt.Fprintf(c.w, ErrEntryNotReceived, e, c.maxWait.Seconds())
		} else {
			if i != k {
				c.entries[k] = c.entries[i]
			}
			k++
		}
	}
	// Nil out the pointers to any trailing elements which were removed from the slice
	for i := k; i < len(c.entries); i++ {
		c.entries[i] = nil // or the zero value of T
	}
	c.entries = c.entries[:k]
}
