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
	ErrOutOfOrderEntry           = "out of order entry %s was received before entries: %v\n"
	ErrEntryNotReceivedWs        = "websocket failed to receive entry %v within %f seconds\n"
	ErrEntryNotReceived          = "failed to receive entry %v within %f seconds\n"
	ErrSpotCheckEntryNotReceived = "failed to find entry %v in Loki when spot check querying %v after it was written\n"
	ErrDuplicateEntry            = "received a duplicate entry for ts %v\n"
	ErrUnexpectedEntry           = "received an unexpected entry with ts %v\n"
	DebugWebsocketMissingEntry   = "websocket missing entry: %v\n"
	DebugQueryResult             = "confirmation query result: %v\n"
	DebugEntryFound              = "missing websocket entry %v was found %v seconds after it was originally sent\n"
)

var (
	totalEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "entries_total",
		Help:      "counts log entries written to the file",
	})
	outOfOrderEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "out_of_order_entries_total",
		Help:      "counts log entries received with a timestamp more recent than the others in the queue",
	})
	wsMissingEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "websocket_missing_entries_total",
		Help:      "counts log entries not received within the wait duration via the websocket connection",
	})
	missingEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "missing_entries_total",
		Help:      "counts log entries not received within the maxWait duration via both websocket and direct query",
	})
	spotCheckMissing = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "spot_check_missing_entries_total",
		Help:      "counts log entries not received when directly queried as part of spot checking",
	})
	spotCheckEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "spot_check_entries_total",
		Help:      "total count of entries pot checked",
	})
	unexpectedEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "unexpected_entries_total",
		Help:      "counts a log entry received which was not expected (e.g. received after reported missing)",
	})
	duplicateEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "duplicate_entries_total",
		Help:      "counts a log entry received more than one time",
	})
	metricTestExpected = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "loki_canary",
		Name:      "metric_test_expected",
		Help:      "How many counts were expected by the metric test query",
	})
	metricTestActual = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "loki_canary",
		Name:      "metric_test_actual",
		Help:      "How many counts were actually received by the metric test query",
	})
	responseLatency prometheus.Histogram
)

type Comparator struct {
	entMtx              sync.Mutex // Locks access to []entries and []ackdEntries
	missingMtx          sync.Mutex // Locks access to []missingEntries
	spotEntMtx          sync.Mutex // Locks access to []spotCheck
	spotMtx             sync.Mutex // Locks spotcheckRunning for single threaded but async spotCheck()
	metTestMtx          sync.Mutex // Locks metricTestRunning for single threaded but async metricTest()
	pruneMtx            sync.Mutex // Locks pruneEntriesRunning for single threaded but async pruneEntries()
	w                   io.Writer
	entries             []*time.Time
	missingEntries      []*time.Time
	spotCheck           []*time.Time
	ackdEntries         []*time.Time
	wait                time.Duration
	maxWait             time.Duration
	pruneInterval       time.Duration
	pruneEntriesRunning bool
	spotCheckInterval   time.Duration
	spotCheckMax        time.Duration
	spotCheckQueryRate  time.Duration
	spotCheckRunning    bool
	metricTestInterval  time.Duration
	metricTestRange     time.Duration
	metricTestRunning   bool
	writeInterval       time.Duration
	confirmAsync        bool
	startTime           time.Time
	sent                chan time.Time
	recv                chan time.Time
	rdr                 reader.LokiReader
	quit                chan struct{}
	done                chan struct{}
}

func NewComparator(writer io.Writer,
	wait time.Duration,
	maxWait time.Duration,
	pruneInterval time.Duration,
	spotCheckInterval, spotCheckMax, spotCheckQueryRate time.Duration,
	metricTestInterval time.Duration,
	metricTestRange time.Duration,
	writeInterval time.Duration,
	buckets int,
	sentChan chan time.Time,
	receivedChan chan time.Time,
	reader reader.LokiReader,
	confirmAsync bool) *Comparator {
	c := &Comparator{
		w:                   writer,
		entries:             []*time.Time{},
		spotCheck:           []*time.Time{},
		wait:                wait,
		maxWait:             maxWait,
		pruneInterval:       pruneInterval,
		pruneEntriesRunning: false,
		spotCheckInterval:   spotCheckInterval,
		spotCheckMax:        spotCheckMax,
		spotCheckQueryRate:  spotCheckQueryRate,
		spotCheckRunning:    false,
		metricTestInterval:  metricTestInterval,
		metricTestRange:     metricTestRange,
		metricTestRunning:   false,
		writeInterval:       writeInterval,
		confirmAsync:        confirmAsync,
		startTime:           time.Now(),
		sent:                sentChan,
		recv:                receivedChan,
		rdr:                 reader,
		quit:                make(chan struct{}),
		done:                make(chan struct{}),
	}

	if responseLatency == nil {
		responseLatency = promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki_canary",
			Name:      "response_latency",
			Help:      "is how long it takes for log lines to be returned from Loki in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.5, 2, buckets),
		})
	}

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
	c.entries = append(c.entries, &time)
	totalEntries.Inc()
	c.entMtx.Unlock()
	//If this entry equals or exceeds the spot check interval from the last entry in the spot check array, add it.
	c.spotEntMtx.Lock()
	if len(c.spotCheck) == 0 || time.Sub(*c.spotCheck[len(c.spotCheck)-1]) >= c.spotCheckInterval {
		c.spotCheck = append(c.spotCheck, &time)
	}
	c.spotEntMtx.Unlock()

}

// entryReceived removes the received entry from the buffer if it exists, reports on out of order entries received
func (c *Comparator) entryReceived(ts time.Time) {
	c.entMtx.Lock()
	defer c.entMtx.Unlock()

	matched := false
	c.entries = pruneList(c.entries,
		func(_ int, t *time.Time) bool {
			return ts.Equal(*t)
		},
		func(i int, t *time.Time) {
			matched = true
			// If this isn't the first item in the list we received it out of order
			if i != 0 {
				outOfOrderEntries.Inc()
				fmt.Fprintf(c.w, ErrOutOfOrderEntry, t, c.entries[:i])
			}
			responseLatency.Observe(time.Since(ts).Seconds())
			// Put this element in the acknowledged entries list so we can use it to check for duplicates
			c.ackdEntries = append(c.ackdEntries, c.entries[i])
		})

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
}

func (c *Comparator) Size() int {
	c.entMtx.Lock()
	defer c.entMtx.Unlock()
	return len(c.entries)
}

func (c *Comparator) run() {
	t := time.NewTicker(c.pruneInterval)
	mt := time.NewTicker(c.metricTestInterval)
	sc := time.NewTicker(c.spotCheckQueryRate)
	defer func() {
		t.Stop()
		mt.Stop()
		sc.Stop()
		close(c.done)
	}()

	for {
		select {
		case e := <-c.recv:
			c.entryReceived(e)
		case e := <-c.sent:
			c.entrySent(e)
		case <-t.C:
			// Only run one instance of prune entries at a time.
			c.pruneMtx.Lock()
			if !c.pruneEntriesRunning {
				c.pruneEntriesRunning = true
				go c.pruneEntries(time.Now())
			}
			c.pruneMtx.Unlock()
		case <-sc.C:
			// Only run one instance of spot check at a time.
			c.spotMtx.Lock()
			if !c.spotCheckRunning {
				c.spotCheckRunning = true
				go c.spotCheckEntries(time.Now())
			}
			c.spotMtx.Unlock()
		case <-mt.C:
			// Only run one intstance of metric tests at a time.
			c.metTestMtx.Lock()
			if !c.metricTestRunning {
				c.metricTestRunning = true
				go c.metricTest(time.Now())
			}
			c.metTestMtx.Unlock()
		case <-c.quit:
			return
		}
	}
}

func (c *Comparator) metricTest(currTime time.Time) {
	// Always make sure to set the running state back to false
	defer func() {
		c.metTestMtx.Lock()
		c.metricTestRunning = false
		c.metTestMtx.Unlock()
	}()
	adjustedRange := c.metricTestRange

	// Adjust the query range to not be longer than the canary has been running.
	// We can't query for 24 hours of counts if it's only been running for 10m,
	// so we adjusted the range to the run time until we reach the desired lookback time.
	if currTime.Add(-c.metricTestRange).Before(c.startTime) {
		adjustedRange = currTime.Sub(c.startTime)
	}
	actualCount, err := c.rdr.QueryCountOverTime(fmt.Sprintf("%.0fs", adjustedRange.Seconds()))
	if err != nil {
		fmt.Fprintf(c.w, "error running metric query test: %s\n", err.Error())
		return
	}
	expectedCount := float64(adjustedRange.Milliseconds()) / float64(c.writeInterval.Milliseconds())
	metricTestExpected.Set(expectedCount)
	metricTestActual.Set(actualCount)
}

func (c *Comparator) spotCheckEntries(currTime time.Time) {
	// Always make sure to set the running state back to false
	defer func() {
		c.spotMtx.Lock()
		c.spotCheckRunning = false
		c.spotMtx.Unlock()
	}()
	c.spotEntMtx.Lock()

	// Remove any entries from the spotcheck list which are too old
	c.spotCheck = pruneList(c.spotCheck,
		func(_ int, t *time.Time) bool {
			return t.Before(currTime.Add(-c.spotCheckMax))
		},
		func(_ int, t *time.Time) {

		})

	// Make a copy so we don't have to hold the lock to verify entries
	cpy := make([]*time.Time, len(c.spotCheck))
	copy(cpy, c.spotCheck)
	c.spotEntMtx.Unlock()

	for _, sce := range cpy {
		spotCheckEntries.Inc()
		// Because we are querying loki timestamps vs the timestamp in the log,
		// make the range +/- 10 seconds to allow for clock inaccuracies
		start := *sce
		adjustedStart := start.Add(-10 * time.Second)
		adjustedEnd := start.Add(10 * time.Second)
		recvd, err := c.rdr.Query(adjustedStart, adjustedEnd)
		if err != nil {
			fmt.Fprintf(c.w, "error querying loki: %s\n", err)
			return
		}

		found := false
		for _, r := range recvd {
			if (*sce).Equal(r) {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(c.w, ErrSpotCheckEntryNotReceived, sce.UnixNano(), currTime.Sub(*sce))
			for _, r := range recvd {
				fmt.Fprintf(c.w, DebugQueryResult, r.UnixNano())
			}
			spotCheckMissing.Inc()
		}
	}

}

func (c *Comparator) pruneEntries(currentTime time.Time) {
	// Always make sure to set the running state back to false
	defer func() {
		c.pruneMtx.Lock()
		c.pruneEntriesRunning = false
		c.pruneMtx.Unlock()
	}()
	c.entMtx.Lock()
	defer c.entMtx.Unlock()

	missing := []*time.Time{}
	// Prune entry list of anything older than c.wait and add it to missing list
	c.entries = pruneList(c.entries,
		func(_ int, t *time.Time) bool {
			return t.Before(currentTime.Add(-c.wait)) || t.Equal(currentTime.Add(-c.wait))
		},
		func(_ int, t *time.Time) {
			missing = append(missing, t)
			wsMissingEntries.Inc()
			fmt.Fprintf(c.w, ErrEntryNotReceivedWs, t.UnixNano(), c.wait.Seconds())
		})

	// Add the list of missing entries to the list for which we will attempt to query Loki for
	c.missingMtx.Lock()
	c.missingEntries = append(c.missingEntries, missing...)
	c.missingMtx.Unlock()
	if len(c.missingEntries) > 0 {
		if c.confirmAsync {
			go c.confirmMissing(currentTime)
		} else {
			c.confirmMissing(currentTime)
		}

	}

	// Prune c.ackdEntries list of old acknowledged entries which we were using to find duplicates
	c.ackdEntries = pruneList(c.ackdEntries,
		func(_ int, t *time.Time) bool {
			return t.Before(currentTime.Add(-c.wait))
		},
		func(_ int, t *time.Time) {

		})
}

func (c *Comparator) confirmMissing(currentTime time.Time) {
	// Because we are querying loki timestamps vs the timestamp in the log,
	// make the range +/- 10 seconds to allow for clock inaccuracies
	start := *c.missingEntries[0]
	start = start.Add(-10 * time.Second)
	end := *c.missingEntries[len(c.missingEntries)-1]
	end = end.Add(10 * time.Second)
	recvd, err := c.rdr.Query(start, end)
	if err != nil {
		fmt.Fprintf(c.w, "error querying loki: %s\n", err)
		return
	}
	// This is to help debug some missing log entries when queried,
	// let's print exactly what we are missing and what Loki sent back
	for _, r := range c.missingEntries {
		fmt.Fprintf(c.w, DebugWebsocketMissingEntry, r.UnixNano())
	}
	for _, r := range recvd {
		fmt.Fprintf(c.w, DebugQueryResult, r.UnixNano())
	}

	// Now that query has returned, take out the lock on the missingEntries list so we can modify it
	// It's possible more entries were added to this list but that's ok, if they match something in the
	// query result we will remove them, if they don't they won't be old enough yet to remove.
	c.missingMtx.Lock()
	defer c.missingMtx.Unlock()
	k := 0
	for i, m := range c.missingEntries {
		found := false
		for _, r := range recvd {
			if (*m).Equal(r) {
				// Entry was found in loki, this can be dropped from the list of missing
				// which is done by NOT incrementing the output index k
				fmt.Fprintf(c.w, DebugEntryFound, (*m).UnixNano(), currentTime.Sub(*m).Seconds())
				found = true
			}
		}
		if !found {
			// Item is still missing
			if i != k {
				c.missingEntries[k] = c.missingEntries[i]
			}
			k++
		}
	}
	// Nil out the pointers to any trailing elements which were removed from the slice
	for i := k; i < len(c.missingEntries); i++ {
		c.missingEntries[i] = nil // or the zero value of T
	}
	c.missingEntries = c.missingEntries[:k]

	// Remove entries from missing list which are older than maxWait
	removed := []*time.Time{}
	c.missingEntries = pruneList(c.missingEntries,
		func(_ int, t *time.Time) bool {
			return t.Before(currentTime.Add(-c.maxWait))
		},
		func(_ int, t *time.Time) {
			removed = append(removed, t)
		})

	// Record the entries which were removed and never received
	for _, e := range removed {
		missingEntries.Inc()
		fmt.Fprintf(c.w, ErrEntryNotReceived, e.UnixNano(), c.maxWait.Seconds())
	}
}

func pruneList(list []*time.Time, shouldRemove func(int, *time.Time) bool, handleRemoved func(int, *time.Time)) []*time.Time {
	// Prune the acknowledged list, remove anything older than our maxwait
	k := 0
	for i, e := range list {
		if shouldRemove(i, e) {
			handleRemoved(i, e)
			// Do not increment output index k, causing this entry to be dropped
		} else {
			// If items were skipped, assign the kth element to the current item which is not skipped
			if i != k {
				list[k] = list[i]
			}
			// Increment k for the next output item
			k++
		}
	}
	// Nil out the pointers to any trailing elements which were removed from the slice
	for i := k; i < len(list); i++ {
		list[i] = nil // or the zero value of T
	}
	// Reslice the list to the new size k
	return list[:k]

}
