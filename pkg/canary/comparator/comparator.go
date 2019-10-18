package comparator

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/canary/reader"
)

const (
	ErrOutOfOrderEntry         = "out of order entry %s was received before entries: %v\n"
	ErrEntryNotReceivedWs      = "websocket failed to receive entry %v within %f seconds\n"
	ErrEntryNotReceived        = "failed to receive entry %v within %f seconds\n"
	ErrDuplicateEntry          = "received a duplicate entry for ts %v\n"
	ErrUnexpectedEntry         = "received an unexpected entry with ts %v\n"
	DebugWebsocketMissingEntry = "websocket missing entry: %v\n"
	DebugQueryResult           = "confirmation query result: %v\n"
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
	wsMissingEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "websocket_missing_entries",
		Help:      "counts log entries not received within the maxWait duration via the websocket connection",
	})
	missingEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "missing_entries",
		Help:      "counts log entries not received within the maxWait duration via both websocket and direct query",
	})
	unexpectedEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "unexpected_entries",
		Help:      "counts a log entry received which was not expected (e.g. received after reported missing)",
	})
	duplicateEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "duplicate_entries",
		Help:      "counts a log entry received more than one time",
	})
	responseLatency prometheus.Histogram
)

type Comparator struct {
	entMtx        sync.Mutex
	w             io.Writer
	entries       []*time.Time
	ackdEntries   []*time.Time
	maxWait       time.Duration
	pruneInterval time.Duration
	confirmAsync  bool
	sent          chan time.Time
	recv          chan time.Time
	rdr           reader.LokiReader
	quit          chan struct{}
	done          chan struct{}
}

func NewComparator(writer io.Writer, maxWait time.Duration, pruneInterval time.Duration,
	buckets int, sentChan chan time.Time, receivedChan chan time.Time, reader reader.LokiReader, confirmAsync bool) *Comparator {
	c := &Comparator{
		w:             writer,
		entries:       []*time.Time{},
		maxWait:       maxWait,
		pruneInterval: pruneInterval,
		confirmAsync:  confirmAsync,
		sent:          sentChan,
		recv:          receivedChan,
		rdr:           reader,
		quit:          make(chan struct{}),
		done:          make(chan struct{}),
	}

	responseLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki_canary",
		Name:      "response_latency",
		Help:      "is how long it takes for log lines to be returned from Loki in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.5, 2, buckets),
	})

	go c.run()

	return c
}

func (c *Comparator) Stop() {
	if c.quit != nil {
		close(c.quit)
		<-c.done
		c.quit = nil
	}
}

func (c *Comparator) entrySent(time time.Time) {
	c.entMtx.Lock()
	defer c.entMtx.Unlock()
	c.entries = append(c.entries, &time)
	totalEntries.Inc()
}

// entryReceived removes the received entry from the buffer if it exists, reports on out of order entries received
func (c *Comparator) entryReceived(ts time.Time) {
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
				fmt.Fprintf(c.w, ErrOutOfOrderEntry, e, c.entries[:i])
			}
			responseLatency.Observe(time.Since(ts).Seconds())
			// Put this element in the acknowledged entries list so we can use it to check for duplicates
			c.ackdEntries = append(c.ackdEntries, c.entries[i])
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
		duplicate := false
		for _, e := range c.ackdEntries {
			if ts.Equal(*e) {
				duplicate = true
				duplicateEntries.Inc()
				fmt.Fprintf(c.w, ErrDuplicateEntry, ts.UnixNano())
				break
			}
		}
		if !duplicate {
			fmt.Fprintf(c.w, ErrUnexpectedEntry, ts.UnixNano())
			unexpectedEntries.Inc()
		}
	}
	// Nil out the pointers to any trailing elements which were removed from the slice
	for i := k; i < len(c.entries); i++ {
		c.entries[i] = nil // or the zero value of T
	}
	c.entries = c.entries[:k]
}

func (c *Comparator) Size() int {
	c.entMtx.Lock()
	defer c.entMtx.Unlock()
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
		case e := <-c.recv:
			c.entryReceived(e)
		case e := <-c.sent:
			c.entrySent(e)
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

	missing := []*time.Time{}
	k := 0
	for i, e := range c.entries {
		// If the time is outside our range, assume the entry has been lost report and remove it
		if e.Before(time.Now().Add(-c.maxWait)) {
			missing = append(missing, e)
			wsMissingEntries.Inc()
			fmt.Fprintf(c.w, ErrEntryNotReceivedWs, e.UnixNano(), c.maxWait.Seconds())
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
	if len(missing) > 0 {
		if c.confirmAsync {
			go c.confirmMissing(missing)
		} else {
			c.confirmMissing(missing)
		}

	}

	// Prune the acknowledged list, remove anything older than our maxwait
	k = 0
	for i, e := range c.ackdEntries {
		if e.Before(time.Now().Add(-c.maxWait)) {
			// Do nothing, if we don't increment the output index k, this will be dropped
		} else {
			if i != k {
				c.ackdEntries[k] = c.ackdEntries[i]
			}
			k++
		}
	}
	// Nil out the pointers to any trailing elements which were removed from the slice
	for i := k; i < len(c.ackdEntries); i++ {
		c.ackdEntries[i] = nil // or the zero value of T
	}
	c.ackdEntries = c.ackdEntries[:k]
}

func (c *Comparator) confirmMissing(missing []*time.Time) {
	// Because we are querying loki timestamps vs the timestamp in the log,
	// make the range +/- 10 seconds to allow for clock inaccuracies
	start := *missing[0]
	start = start.Add(-10 * time.Second)
	end := *missing[len(missing)-1]
	end = end.Add(10 * time.Second)
	recvd, err := c.rdr.Query(start, end)
	if err != nil {
		fmt.Fprintf(c.w, "error querying loki: %s\n", err)
		return
	}
	// This is to help debug some missing log entries when queried,
	// let's print exactly what we are missing and what Loki sent back
	for _, r := range missing {
		fmt.Fprintf(c.w, DebugWebsocketMissingEntry, r.UnixNano())
	}
	for _, r := range recvd {
		fmt.Fprintf(c.w, DebugQueryResult, r.UnixNano())
	}

	k := 0
	for i, m := range missing {
		found := false
		for _, r := range recvd {
			if (*m).Equal(r) {
				// Entry was found in loki, this can be dropped from the list of missing
				// which is done by NOT incrementing the output index k
				found = true
			}
		}
		if !found {
			// Item is still missing
			if i != k {
				missing[k] = missing[i]
			}
			k++
		}
	}
	// Nil out the pointers to any trailing elements which were removed from the slice
	for i := k; i < len(missing); i++ {
		missing[i] = nil // or the zero value of T
	}
	missing = missing[:k]
	for _, e := range missing {
		missingEntries.Inc()
		fmt.Fprintf(c.w, ErrEntryNotReceived, e.UnixNano(), c.maxWait.Seconds())
	}
}
