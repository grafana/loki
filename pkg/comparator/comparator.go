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
	outOfOrderEntry = promauto.NewCounter(prometheus.CounterOpts{
		Name: "out_of_order_entry",
		Help: "The total number of processed events",
	})
	missingEntry = promauto.NewCounter(prometheus.CounterOpts{
		Name: "missing_entry",
		Help: "The total number of processed events",
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
}

func (c *Comparator) EntryReceived(ts time.Time) {
	c.entMtx.Lock()
	defer c.entMtx.Unlock()

	// Output index
	k := 0
	for i, e := range c.entries {
		if ts.Equal(*e) {
			// If this isn't the first item in the list we received it out of order
			if i != 0 {
				outOfOrderEntry.Inc()
				_, _ = fmt.Fprintf(c.w, ErrOutOfOrderEntry, e, c.entries[:i])
			}
			// Do not increment output index, effectively causing this element to be dropped
		} else {
			// If the current index doesn't match the output index, update the array with the correct position
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
			missingEntry.Inc()
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
