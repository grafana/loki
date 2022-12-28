package client

import (
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wal"

	"github.com/grafana/loki/pkg/ingester"
)

const (
	readPeriod         = 10 * time.Millisecond
	checkpointPeriod   = 5 * time.Second
	segmentCheckPeriod = 100 * time.Millisecond
)

type WALReader interface {
	Next() bool
	Err() error
	// Record should not be used across multiple calls to Next()
	Record() []byte
}

type WALConsumer interface {
	ConsumeSeries(series record.RefSeries) error
	ConsumeEntries(entries ingester.RefEntries) error
	SegmentEnd(segmentNum int)
}

// Based in the implementation of prometheus wal watcher
// https://github.com/prometheus/prometheus/blob/main/tsdb/wlog/watcher.go

type WALWatcher struct {
	consumer   WALConsumer
	done       chan struct{}
	quit       chan struct{}
	walDir     string
	logger     log.Logger
	MaxSegment int
}

func NewWALWatcher(walDir string, consumer WALConsumer, logger log.Logger) *WALWatcher {
	return &WALWatcher{
		walDir:     walDir,
		consumer:   consumer,
		quit:       make(chan struct{}),
		done:       make(chan struct{}),
		MaxSegment: -1,
		logger:     logger,
	}
}

// Start runs the  watcher main loop.
func (w *WALWatcher) Start() {
	go w.loop()
}

func (w *WALWatcher) Stop() {
	// first close the quit channel to order main loop routine to stop
	close(w.quit)
	// upon calling stop, wait for main loop execution to stop
	<-w.done
}

func (w *WALWatcher) loop() {
	defer close(w.done)
	for !isClosed(w.quit) {
		//w.SetStartTime(time.Now())
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
func (w *WALWatcher) run() error {
	_, lastSegment, err := w.firstAndLast()
	if err != nil {
		return fmt.Errorf("wal.Segments: %w", err)
	}

	// We want to ensure this is false across iterations since
	// Run will be called again if there was a failure to read the WAL.
	//w.sendSamples = false

	//level.Info(w.logger).Log("msg", "Replaying WAL", "queue", w.name)

	// Backfill from the checkpoint first if it exists.
	//lastCheckpoint, checkpointIndex, err := LastCheckpoint(w.walDir)
	//if err != nil && err != record.ErrNotFound {
	//	return errors.Wrap(err, "tsdb.LastCheckpoint")
	//}
	//
	//if err == nil {
	//	if err = w.readCheckpoint(lastCheckpoint, (*Watcher).readSegment); err != nil {
	//		return errors.Wrap(err, "readCheckpoint")
	//	}
	//}
	//w.lastCheckpoint = lastCheckpoint

	//currentSegment, err := w.findSegmentForIndex(checkpointIndex)
	//if err != nil {
	//	return err
	//}

	// todo: change me when decided if we should do checkpoints
	currentSegment := lastSegment
	//level.Debug(w.logger).Log("msg", "Tailing WAL", "lastCheckpoint", lastCheckpoint, "checkpointIndex", checkpointIndex, "currentSegment", currentSegment, "lastSegment", lastSegment)
	level.Debug(w.logger).Log("msg", "Tailing WAL", "currentSegment", currentSegment, "lastSegment", lastSegment)
	for !isClosed(w.quit) {
		//w.currentSegmentMetric.Set(float64(currentSegment))
		level.Debug(w.logger).Log("msg", "Processing segment", "currentSegment", currentSegment)

		// On start, after reading the existing WAL for series records, we have a pointer to what is the latest segment.
		// On subsequent calls to this function, currentSegment will have been incremented and we should open that segment.
		if err := w.watch(currentSegment, currentSegment >= lastSegment); err != nil {
			return err
		}

		// For testing: stop when you hit a specific segment.
		if currentSegment == w.MaxSegment {
			return nil
		}

		// we now a new segment has been cut, upon advancing the segment pointer, emit the send batch call
		// the call to sending the batch will be locking, since the sender routine is a single one
		w.consumer.SegmentEnd(currentSegment)
		currentSegment++
	}

	return nil
}

// firstAndList finds the first and last segment number for a WAL directory.
func (w *WALWatcher) firstAndLast() (int, int, error) {
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
func (w *WALWatcher) segments(dir string) ([]int, error) {
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

// Use tail true to indicate that the reader is currently on a segment that is
// actively being written to. If false, assume it's a full segment, and we're
// replaying it on start to cache the series records.
func (w *WALWatcher) watch(segmentNum int, tail bool) error {
	segment, err := wal.OpenReadSegment(wal.SegmentName(w.walDir, segmentNum))
	if err != nil {
		return err
	}
	defer segment.Close()

	// todo: fix nil livereader metrics
	reader := wal.NewLiveReader(w.logger, nil, segment)

	readTicker := time.NewTicker(readPeriod)
	defer readTicker.Stop()

	checkpointTicker := time.NewTicker(checkpointPeriod)
	defer checkpointTicker.Stop()

	segmentTicker := time.NewTicker(segmentCheckPeriod)
	defer segmentTicker.Stop()

	// If we're replaying the segment we need to know the size of the file to know
	// when to return from watch and move on to the next segment.
	size := int64(math.MaxInt64)
	if !tail {
		segmentTicker.Stop()
		checkpointTicker.Stop()
		var err error
		size, err = getSegmentSize(w.walDir, segmentNum)
		if err != nil {
			return fmt.Errorf("getSegmentSize: %w", err)
		}
	}

	//gcSem := make(chan struct{}, 1)
	for {
		select {
		case <-w.quit:
			return nil

			// todo: commenting out casees when it's not sure if it's the case for promtail

		//case <-checkpointTicker.C:
		//	// Periodically check if there is a new checkpoint so we can garbage
		//	// collect labels. As this is considered an optimisation, we ignore
		//	// errors during checkpoint processing. Doing the process asynchronously
		//	// allows the current WAL segment to be processed while reading the
		//	// checkpoint.
		//	select {
		//	case gcSem <- struct{}{}:
		//		go func() {
		//			defer func() {
		//				<-gcSem
		//			}()
		//			if err := w.garbageCollectSeries(segmentNum); err != nil {
		//				level.Warn(w.logger).Log("msg", "Error process checkpoint", "err", err)
		//			}
		//		}()
		//	default:
		//		// Currently doing a garbage collect, try again later.
		//	}

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
			err = w.readSegment(reader, segmentNum, tail)

			// Ignore errors reading to end of segment whilst replaying the WAL.
			if !tail {
				w.logIgnoredErrorWhileReplaying(err, reader.Offset(), size, segmentNum)
				return nil
			}

			// Otherwise, when we are tailing, non-EOFs are fatal.
			if errors.Cause(err) != io.EOF {
				return err
			}

			return nil

		case <-readTicker.C:
			err = w.readSegment(reader, segmentNum, tail)

			// Ignore all errors reading to end of segment whilst replaying the WAL.
			if !tail {
				w.logIgnoredErrorWhileReplaying(err, reader.Offset(), size, segmentNum)
				return nil
			}

			// Otherwise, when we are tailing, non-EOFs are fatal.
			if errors.Cause(err) != io.EOF {
				return err
			}
		}
	}
}

func (w *WALWatcher) logIgnoredErrorWhileReplaying(err error, readerOffset, size int64, segmentNum int) {
	if err != nil && errors.Cause(err) != io.EOF {
		level.Warn(w.logger).Log("msg", "Ignoring error reading to end of segment, may have dropped data", "segment", segmentNum, "err", err)
	} else if readerOffset != size {
		level.Warn(w.logger).Log("msg", "Expected to have read whole segment, may have dropped data", "segment", segmentNum, "read", readerOffset, "size", size)
	}
}

// Read from a segment and pass the details to w.writer.
// Also used with readCheckpoint - implements segmentReadFn.
func (w *WALWatcher) readSegment(r *wal.LiveReader, segmentNum int, tail bool) error {
	for r.Next() && !isClosed(w.quit) {
		b := r.Record()
		if err := r.Err(); err != nil {
			return err
		}

		if err := w.decodeAndDispatch(b); err != nil {
			return err
		}
	}
	return errors.Wrapf(r.Err(), "segment %d: %v", segmentNum, r.Err())
}

func (w *WALWatcher) decodeAndDispatch(b []byte) error {
	rec := recordPool.GetRecord()
	if err := ingester.DecodeWALRecord(b, rec); err != nil {
		return err
	}

	// First process all series to ensure we don't write entries to nonexistant series.
	var firstErr error
	for _, s := range rec.Series {
		if err := w.consumer.ConsumeSeries(s); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}

	}

	for _, entries := range rec.RefEntries {
		if err := w.consumer.ConsumeEntries(entries); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// Get size of segment.
func getSegmentSize(dir string, index int) (int64, error) {
	i := int64(-1)
	fi, err := os.Stat(wal.SegmentName(dir, index))
	if err == nil {
		i = fi.Size()
	}
	return i, err
}
