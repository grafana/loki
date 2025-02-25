package wal

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wlog"

	"github.com/grafana/loki/v3/pkg/ingester/wal"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	segmentCheckPeriod = 100 * time.Millisecond

	// debug flag used for developing and testing the watcher code. Using this instead of level.Debug to avoid checking
	// the log level inwards into the logger code, and just making the compiler omit this code.
	debug = false
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

// WriteCleanup is responsible for cleaning up resources used in the process of reading the WAL.
type WriteCleanup interface {
	// SeriesReset is called to notify that segments have been deleted. The argument of the call
	// means that all segments with a number lower or equal than segmentNum are safe to be reclaimed.
	SeriesReset(segmentNum int)
}

// WriteTo is an interface used by the Watcher to send the samples it's read from the WAL on to
// somewhere else, or clean them up. It's the intermediary between all information read by the Watcher
// and the final destination.
//
// Based on https://github.com/prometheus/prometheus/blob/main/tsdb/wlog/watcher.go#L46
type WriteTo interface {
	// WriteCleanup is used to allow the Watcher to react upon being notified of WAL cleanup events, such as segments
	// being reclaimed.
	WriteCleanup

	// StoreSeries is called when series are found in WAL entries by the watcher, alongside with the segmentNum they were
	// found in.
	StoreSeries(series []record.RefSeries, segmentNum int)

	AppendEntries(entries wal.RefEntries) error
}

type Watcher struct {
	// id identifies the Watcher. Used when one Watcher is instantiated per remote write client, to be able to track to whom
	// the metric/log line corresponds.
	id string

	actions    WriteTo
	readNotify chan struct{}
	done       chan struct{}
	quit       chan struct{}
	walDir     string
	logger     log.Logger
	MaxSegment int

	metrics     *WatcherMetrics
	minReadFreq time.Duration
	maxReadFreq time.Duration
}

// NewWatcher creates a new Watcher.
func NewWatcher(walDir, id string, metrics *WatcherMetrics, writeTo WriteTo, logger log.Logger, config WatchConfig) *Watcher {
	return &Watcher{
		walDir:      walDir,
		id:          id,
		actions:     writeTo,
		readNotify:  make(chan struct{}),
		quit:        make(chan struct{}),
		done:        make(chan struct{}),
		MaxSegment:  -1,
		logger:      logger,
		metrics:     metrics,
		minReadFreq: config.MinReadFrequency,
		maxReadFreq: config.MaxReadFrequency,
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

	reader := wlog.NewLiveReader(util_log.SlogFromGoKit(w.logger), nil, segment)

	readTimer := newBackoffTimer(w.minReadFreq, w.maxReadFreq)

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
			_, err = w.readSegment(reader, segmentNum)
			if debug {
				level.Warn(w.logger).Log("msg", "Error reading segment inside segmentTicker", "segment", segmentNum, "read", reader.Offset(), "err", err)
			}

			// io.EOF error are non-fatal since we are tailing the wal
			if errors.Cause(err) != io.EOF {
				return err
			}

			// return after reading the whole segment for creating a new LiveReader from the newly created segment
			return nil

		// the cases below will unlock the select block, and execute the block below
		// https://github.com/golang/go/issues/23196#issuecomment-353169837
		case <-readTimer.C:
			w.metrics.segmentRead.WithLabelValues(w.id, "timer").Inc()
			if debug {
				level.Debug(w.logger).Log("msg", "Segment read triggered by backup timer", "segment", segmentNum)

			}
		case <-w.readNotify:
			w.metrics.segmentRead.WithLabelValues(w.id, "notification").Inc()
		}

		// read from open segment routine
		ok, err := w.readSegment(reader, segmentNum)
		if debug {
			level.Warn(w.logger).Log("msg", "Error reading segment inside readTicker", "segment", segmentNum, "read", reader.Offset(), "err", err)
		}

		// io.EOF error are non-fatal since we are tailing the wal
		if errors.Cause(err) != io.EOF {
			return err
		}

		if ok {
			// read ok, reset readTimer to minimum interval
			readTimer.reset()
			continue
		}

		readTimer.backoff()
	}
}

// Read entries from a segment, decode them and dispatch them.
func (w *Watcher) readSegment(r *wlog.LiveReader, segmentNum int) (bool, error) {
	var readData bool

	for r.Next() && !isClosed(w.quit) {
		rec := r.Record()
		w.metrics.recordsRead.WithLabelValues(w.id).Inc()
		read, err := w.decodeAndDispatch(rec, segmentNum)
		// keep true if data was read at least once
		readData = readData || read
		if err != nil {
			return readData, errors.Wrapf(err, "error decoding record")
		}
	}
	return readData, errors.Wrapf(r.Err(), "segment %d: %v", segmentNum, r.Err())
}

// decodeAndDispatch first decodes a WAL record. Upon reading either Series or Entries from the WAL record, call the
// appropriate callbacks in the writeTo.
func (w *Watcher) decodeAndDispatch(b []byte, segmentNum int) (bool, error) {
	var readData bool

	rec := recordPool.GetRecord()
	if err := wal.DecodeRecord(b, rec); err != nil {
		w.metrics.recordDecodeFails.WithLabelValues(w.id).Inc()
		return readData, err
	}

	// First process all series to ensure we don't write entries to non-existent series.
	var firstErr error
	w.actions.StoreSeries(rec.Series, segmentNum)
	readData = true

	for _, entries := range rec.RefEntries {
		if err := w.actions.AppendEntries(entries); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return readData, firstErr
}

func (w *Watcher) Stop() {
	// first close the quit channel to order main mainLoop routine to stop
	close(w.quit)
	// upon calling stop, wait for main mainLoop execution to stop
	<-w.done

	w.metrics.watchersRunning.WithLabelValues().Dec()
}

// firstAndLast finds the first and last segment number for a WAL directory.
func (w *Watcher) firstAndLast() (int, int, error) {
	refs, err := readSegmentNumbers(w.walDir)
	if err != nil {

		return -1, -1, err
	}

	if len(refs) == 0 {
		return -1, -1, nil
	}

	// Start with sentinel values and walk back to the first and last (min and max)
	var first = math.MaxInt32
	var last = -1
	for _, segmentReg := range refs {
		if segmentReg < first {
			first = segmentReg
		}
		if segmentReg > last {
			last = segmentReg
		}
	}
	return first, last, nil
}

// NotifyWrite allows the Watcher to subscribe to write events published by the Writer. When a write event is received
// we emit the signal to trigger a segment read on the watcher main routine. If the readNotify channel already is not being
// listened on, that means the main routine is processing a segment,  or waiting because a non-handled error occurred.
// In that case we drop the signal and make the Watcher wait for the next one.
func (w *Watcher) NotifyWrite() {
	select {
	case w.readNotify <- struct{}{}:
		// written notification to the channel
		return
	default:
		// drop wal written signal if the channel is not being listened
		w.metrics.droppedWriteNotifications.WithLabelValues(w.id).Inc()
	}
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

// readSegmentNumbers reads the given directory and returns all segment identifiers, that is, the index of each segment
// file.
func readSegmentNumbers(dir string) ([]int, error) {
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
	return refs, nil
}
