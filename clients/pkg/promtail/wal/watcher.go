package wal

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wlog"

	"github.com/grafana/loki/pkg/ingester/wal"
)

const (
	readPeriod         = 10 * time.Millisecond
	segmentCheckPeriod = 100 * time.Millisecond
)

// Based in the implementation of prometheus WAL watcher
// https://github.com/prometheus/prometheus/blob/main/tsdb/wlog/watcher.go. Includes some changes to make it suitable
// for log WAL entries, but also, the writeTo surface has been implemented according to the actions necessary for
// Promtail's WAL.

// Reader is a dependency interface to inject generic WAL readers into the Watcher.
type Reader interface {
	Next() bool
	Err() error
	Record() []byte
}

// WriteTo is responsible for doing the necessary work to process both series and entries while the Watcher
// is reading / tailing segments. The implementor of this interface is not expected to implement thread safety if used
// on a single watcher, since each callback is called synchronously.
//
// Based on https://github.com/prometheus/prometheus/blob/main/tsdb/wlog/watcher.go#L46
type WriteTo interface {
	StoreSeries([]record.RefSeries, int)
	AppendEntries(entries wal.RefEntries) error
}

type Watcher struct {
	// id identifies the Watcher. Used when one Watcher is instantiated per remote write client, to be able to track to whom
	// the metric/log line corresponds.
	id string

	writeTo    WriteTo
	done       chan struct{}
	quit       chan struct{}
	walDir     string
	logger     log.Logger
	MaxSegment int

	metrics *WatcherMetrics
}

// NewWatcher creates a new Watcher.
func NewWatcher(walDir, id string, metrics *WatcherMetrics, writeTo WriteTo, logger log.Logger) *Watcher {
	return &Watcher{
		walDir:     walDir,
		id:         id,
		writeTo:    writeTo,
		quit:       make(chan struct{}),
		done:       make(chan struct{}),
		MaxSegment: -1,
		logger:     logger,
		metrics:    metrics,
	}
}

// Start runs the watcher main loop.
func (w *Watcher) Start() {
	w.metrics.watchersRunning.WithLabelValues().Inc()
	go w.mainLoop()
}

// mainLoop retries when there's an error reading a specific segment or advancing one, but leaving a bigger time in-between
// retries.
func (w *Watcher) mainLoop() {
	defer close(w.done)
	for !isClosed(w.quit) {
		if err := w.run(); err != nil {
			level.Error(w.logger).Log("msg", "error tailing WAL", "err", err)
		}

		select {
		case <-w.quit:
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// Run the watcher, which will tail the WAL until the quit channel is closed
// or an error case is hit.
func (w *Watcher) run() error {
	_, lastSegment, err := w.firstAndLast()
	if err != nil {
		return fmt.Errorf("wal.Segments: %w", err)
	}

	currentSegment := lastSegment
	level.Debug(w.logger).Log("msg", "Tailing WAL", "currentSegment", currentSegment, "lastSegment", lastSegment)
	for !isClosed(w.quit) {
		w.metrics.currentSegment.WithLabelValues(w.id).Set(float64(currentSegment))
		level.Debug(w.logger).Log("msg", "Processing segment", "currentSegment", currentSegment)

		// On start, we have a pointer to what is the latest segment. On subsequent calls to this function,
		// currentSegment will have been incremented, and we should open that segment.
		if err := w.watch(currentSegment); err != nil {
			return err
		}

		// For testing: stop when you hit a specific segment.
		if currentSegment == w.MaxSegment {
			return nil
		}

		currentSegment++
	}

	return nil
}

// watch will start reading from the segment identified by segmentNum. If an EOF is reached, it will keep
// reading for more WAL records with a wlog.LiveReader. Periodically, it will check if there's a new segment, and if positive
// read the remaining from the current one and return.
func (w *Watcher) watch(segmentNum int) error {
	segment, err := wlog.OpenReadSegment(wlog.SegmentName(w.walDir, segmentNum))
	if err != nil {
		return err
	}
	defer segment.Close()

	reader := wlog.NewLiveReader(w.logger, nil, segment)

	readTicker := time.NewTicker(readPeriod)
	defer readTicker.Stop()

	segmentTicker := time.NewTicker(segmentCheckPeriod)
	defer segmentTicker.Stop()

	for {
		select {
		case <-w.quit:
			return nil

		case <-segmentTicker.C:
			_, last, err := w.firstAndLast()
			if err != nil {
				return fmt.Errorf("segments: %w", err)
			}

			// Check if new segments exists.
			if last <= segmentNum {
				continue
			}

			// Since we know last > segmentNum, there must be a new segment. Read the remaining from the segmentNum segment
			// and return from `watch` to read the next one
			err = w.readSegment(reader, segmentNum)

			// When we are tailing, non-EOFs are fatal.
			if errors.Cause(err) != io.EOF {
				return err
			}

			return nil

		case <-readTicker.C:
			err = w.readSegment(reader, segmentNum)

			// Otherwise, when we are tailing, non-EOFs are fatal.
			if errors.Cause(err) != io.EOF {
				return err
			}
		}
	}
}

func (w *Watcher) logIgnoredErrorWhileReplaying(err error, readerOffset, size int64, segmentNum int) {
	if err != nil && errors.Cause(err) != io.EOF {
		level.Warn(w.logger).Log("msg", "Ignoring error reading to end of segment, may have dropped data", "segment", segmentNum, "err", err)
	} else if readerOffset != size {
		level.Warn(w.logger).Log("msg", "Expected to have read whole segment, may have dropped data", "segment", segmentNum, "read", readerOffset, "size", size)
	}
}

// Read entries from a segment, decode them and dispatch them.
func (w *Watcher) readSegment(r *wlog.LiveReader, segmentNum int) error {
	for r.Next() && !isClosed(w.quit) {
		b := r.Record()
		if err := r.Err(); err != nil {
			return err
		}
		w.metrics.recordsRead.WithLabelValues(w.id).Inc()

		if err := w.decodeAndDispatch(b, segmentNum); err != nil {
			return err
		}
	}
	return errors.Wrapf(r.Err(), "segment %d: %v", segmentNum, r.Err())
}

// decodeAndDispatch first decodes a WAL record. Upon reading either Series or Entries from the WAL record, call the
// appropriate callbacks in the writeTo.
func (w *Watcher) decodeAndDispatch(b []byte, segmentNum int) error {
	rec := recordPool.GetRecord()
	if err := wal.DecodeRecord(b, rec); err != nil {
		w.metrics.recordDecodeFails.WithLabelValues(w.id).Inc()
		return err
	}

	// First process all series to ensure we don't write entries to non-existent series.
	var firstErr error
	w.writeTo.StoreSeries(rec.Series, segmentNum)

	for _, entries := range rec.RefEntries {
		if err := w.writeTo.AppendEntries(entries); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (w *Watcher) Stop() {
	// first close the quit channel to order main mainLoop routine to stop
	close(w.quit)
	// upon calling stop, wait for main mainLoop execution to stop
	<-w.done

	w.metrics.watchersRunning.WithLabelValues().Dec()
}

// firstAndList finds the first and last segment number for a WAL directory.
func (w *Watcher) firstAndLast() (int, int, error) {
	refs, err := w.segments(w.walDir)
	if err != nil {
		return -1, -1, err
	}

	if len(refs) == 0 {
		return -1, -1, nil
	}
	return refs[0], refs[len(refs)-1], nil
}

// Copied from tsdb/wlog/wlog.go so we do not have to open a WAL.
// Plan is to move WAL watcher to TSDB and dedupe these implementations.
func (w *Watcher) segments(dir string) ([]int, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var refs []int
	for _, f := range files {
		k, err := strconv.Atoi(f.Name())
		if err != nil {
			continue
		}
		refs = append(refs, k)
	}
	sort.Ints(refs)
	for i := 0; i < len(refs)-1; i++ {
		if refs[i]+1 != refs[i+1] {
			return nil, fmt.Errorf("segments are not sequential")
		}
	}
	return refs, nil
}

// isClosed checks in a non-blocking manner if a channel is closed or not.
func isClosed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}
