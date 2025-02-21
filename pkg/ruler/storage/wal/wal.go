// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package wal

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/metadata"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wlog"
	"go.uber.org/atomic"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// ErrWALClosed is an error returned when a WAL operation can't run because the
// storage has already been closed.
var ErrWALClosed = fmt.Errorf("WAL storage closed")

// Storage implements storage.Storage, and just writes to the WAL.
type Storage struct {
	// Embed Queryable/ChunkQueryable for compatibility, but don't actually implement it.
	storage.Queryable
	storage.ChunkQueryable

	// Operations against the WAL must be protected by a mutex so it doesn't get
	// closed in the middle of an operation. Other operations are concurrency-safe, so we
	// use a RWMutex to allow multiple usages of the WAL at once. If the WAL is closed, all
	// operations that change the WAL must fail.
	walMtx    sync.RWMutex
	walClosed bool

	path   string
	wal    *wlog.WL
	logger log.Logger

	appenderPool sync.Pool
	bufPool      sync.Pool

	ref    *atomic.Uint64
	series *stripeSeries

	deletedMtx sync.Mutex
	deleted    map[chunks.HeadSeriesRef]int // Deleted series, and what WAL segment they must be kept until.

	metrics *Metrics

	writeNotified wlog.WriteNotified
}

// NewStorage makes a new Storage.
func NewStorage(logger log.Logger, metrics *Metrics, registerer prometheus.Registerer, path string) (*Storage, error) {
	w, err := wlog.NewSize(util_log.SlogFromGoKit(logger), registerer, SubDirectory(path), wlog.DefaultSegmentSize, wlog.CompressionSnappy)
	if err != nil {
		return nil, err
	}

	storage := &Storage{
		path:    path,
		wal:     w,
		logger:  logger,
		deleted: map[chunks.HeadSeriesRef]int{},
		series:  newStripeSeries(),
		metrics: metrics,
		ref:     atomic.NewUint64(0),
	}

	storage.bufPool.New = func() interface{} {
		b := make([]byte, 0, 1024)
		return b
	}

	storage.appenderPool.New = func() interface{} {
		var notify func()

		if storage.writeNotified != nil {
			notify = storage.writeNotified.Notify
		}
		return &appender{
			w:         storage,
			notify:    notify,
			series:    make([]record.RefSeries, 0, 100),
			samples:   make([]record.RefSample, 0, 100),
			exemplars: make([]record.RefExemplar, 0, 10),
		}
	}

	start := time.Now()
	if err := storage.replayWAL(); err != nil {
		metrics.TotalCorruptions.Inc()

		level.Warn(storage.logger).Log("msg", "encountered WAL read error, attempting repair", "err", err)
		if err := w.Repair(err); err != nil {
			metrics.TotalFailedRepairs.Inc()
			metrics.ReplayDuration.Observe(time.Since(start).Seconds())
			return nil, errors.Wrap(err, "repair corrupted WAL")
		}

		metrics.TotalSucceededRepairs.Inc()
	}

	metrics.ReplayDuration.Observe(time.Since(start).Seconds())

	go storage.recordSize()

	return storage, nil
}

func (w *Storage) SetWriteNotified(writeNotified wlog.WriteNotified) {
	w.writeNotified = writeNotified
}

func (w *Storage) replayWAL() error {
	w.walMtx.RLock()
	defer w.walMtx.RUnlock()

	if w.walClosed {
		return ErrWALClosed
	}

	level.Info(w.logger).Log("msg", "replaying WAL, this may take a while", "dir", w.wal.Dir())
	dir, startFrom, err := wlog.LastCheckpoint(w.wal.Dir())
	if err != nil && err != record.ErrNotFound {
		return errors.Wrap(err, "find last checkpoint")
	}

	if err == nil {
		sr, err := wlog.NewSegmentsReader(dir)
		if err != nil {
			return errors.Wrap(err, "open checkpoint")
		}
		defer func() {
			if err := sr.Close(); err != nil {
				level.Warn(w.logger).Log("msg", "error while closing the wal segments reader", "err", err)
			}
		}()

		// A corrupted checkpoint is a hard error for now and requires user
		// intervention. There's likely little data that can be recovered anyway.
		if err := w.loadWAL(wlog.NewReader(sr)); err != nil {
			return errors.Wrap(err, "backfill checkpoint")
		}
		startFrom++
		level.Info(w.logger).Log("msg", "WAL checkpoint loaded")
	}

	// Find the last segment.
	_, last, err := wlog.Segments(w.wal.Dir())
	if err != nil {
		return errors.Wrap(err, "finding WAL segments")
	}

	// Backfill segments from the most recent checkpoint onwards.
	for i := startFrom; i <= last; i++ {
		s, err := wlog.OpenReadSegment(wlog.SegmentName(w.wal.Dir(), i))
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("open WAL segment: %d", i))
		}

		sr := wlog.NewSegmentBufReader(s)
		err = w.loadWAL(wlog.NewReader(sr))
		if err := sr.Close(); err != nil {
			level.Warn(w.logger).Log("msg", "error while closing the wal segments reader", "err", err)
		}
		if err != nil {
			return err
		}
		level.Info(w.logger).Log("msg", "WAL segment loaded", "segment", i, "maxSegment", last)
	}

	return nil
}

func (w *Storage) loadWAL(r *wlog.Reader) (err error) {
	var dec record.Decoder

	var (
		decoded    = make(chan interface{}, 10)
		errCh      = make(chan error, 1)
		seriesPool = sync.Pool{
			New: func() interface{} {
				return []record.RefSeries{}
			},
		}
		samplesPool = sync.Pool{
			New: func() interface{} {
				return []record.RefSample{}
			},
		}
	)

	go func() {
		defer close(decoded)
		for r.Next() {
			rec := r.Record()
			switch dec.Type(rec) {
			case record.Series:
				series := seriesPool.Get().([]record.RefSeries)[:0]
				series, err = dec.Series(rec, series)
				if err != nil {
					errCh <- &wlog.CorruptionErr{
						Err:     errors.Wrap(err, "decode series"),
						Segment: r.Segment(),
						Offset:  r.Offset(),
					}
					return
				}
				decoded <- series
			case record.Samples:
				samples := samplesPool.Get().([]record.RefSample)[:0]
				samples, err = dec.Samples(rec, samples)
				if err != nil {
					errCh <- &wlog.CorruptionErr{
						Err:     errors.Wrap(err, "decode samples"),
						Segment: r.Segment(),
						Offset:  r.Offset(),
					}
				}
				decoded <- samples
			case record.Tombstones, record.Exemplars:
				// We don't care about decoding tombstones or exemplars
				continue
			default:
				errCh <- &wlog.CorruptionErr{
					Err:     errors.Errorf("invalid record type %v", dec.Type(rec)),
					Segment: r.Segment(),
					Offset:  r.Offset(),
				}
				return
			}
		}
	}()

	biggestRef := chunks.HeadSeriesRef(w.ref.Load())

	for d := range decoded {
		switch v := d.(type) {
		case []record.RefSeries:
			for _, s := range v {
				// If this is a new series, create it in memory without a timestamp.
				// If we read in a sample for it, we'll use the timestamp of the latest
				// sample. Otherwise, the series is stale and will be deleted once
				// the truncation is performed.
				if w.series.getByID(s.Ref) == nil {
					series := &memSeries{ref: s.Ref, lset: s.Labels, lastTs: 0}
					w.series.set(s.Labels.Hash(), series)

					w.metrics.NumActiveSeries.Inc()
					w.metrics.TotalCreatedSeries.Inc()

					if biggestRef <= s.Ref {
						biggestRef = s.Ref
					}
				}
			}

			//nolint:staticcheck
			seriesPool.Put(v)
		case []record.RefSample:
			for _, s := range v {
				// Update the lastTs for the series based
				series := w.series.getByID(s.Ref)
				if series == nil {
					level.Warn(w.logger).Log("msg", "found sample referencing non-existing series, skipping")
					continue
				}

				series.Lock()
				if s.T > series.lastTs {
					series.lastTs = s.T
				}
				series.Unlock()
			}

			//nolint:staticcheck
			samplesPool.Put(v)
		default:
			panic(fmt.Errorf("unexpected decoded type: %T", d))
		}
	}

	w.ref.Store(uint64(biggestRef))

	select {
	case err := <-errCh:
		return err
	default:
	}

	if r.Err() != nil {
		return errors.Wrap(r.Err(), "read records")
	}

	return nil
}

// Directory returns the path where the WAL storage is held.
func (w *Storage) Directory() string {
	return w.path
}

// Appender returns a new appender against the storage.
func (w *Storage) Appender(_ context.Context) storage.Appender {
	return w.appenderPool.Get().(storage.Appender)
}

// StartTime always returns 0, nil. It is implemented for compatibility with
// Prometheus, but is unused in the agent.
func (*Storage) StartTime() (int64, error) {
	return 0, nil
}

// Truncate removes all data from the WAL prior to the timestamp specified by
// mint.
func (w *Storage) Truncate(mint int64) error {
	w.walMtx.RLock()
	defer w.walMtx.RUnlock()

	if w.walClosed {
		return ErrWALClosed
	}

	start := time.Now()

	// Garbage collect series that haven't received an update since mint.
	w.gc(mint)
	level.Info(w.logger).Log("msg", "series GC completed", "duration", time.Since(start))

	first, last, err := wlog.Segments(w.wal.Dir())
	if err != nil {
		return errors.Wrap(err, "get segment range")
	}

	// Start a new segment, so low ingestion volume instance don't have more WAL
	// than needed.
	_, err = w.wal.NextSegment()
	if err != nil {
		return errors.Wrap(err, "next segment")
	}

	last-- // Never consider last segment for checkpoint.
	if last < 0 {
		return nil // no segments yet.
	}

	// The lower two thirds of segments should contain mostly obsolete samples.
	// If we have less than two segments, it's not worth checkpointing yet.
	last = first + (last-first)*2/3
	if last <= first {
		return nil
	}

	keep := func(id chunks.HeadSeriesRef) bool {
		if w.series.getByID(id) != nil {
			return true
		}

		w.deletedMtx.Lock()
		_, ok := w.deleted[id]
		w.deletedMtx.Unlock()
		return ok
	}
	if _, err = wlog.Checkpoint(util_log.SlogFromGoKit(w.logger), w.wal, first, last, keep, mint); err != nil {
		return errors.Wrap(err, "create checkpoint")
	}
	if err := w.wal.Truncate(last + 1); err != nil {
		// If truncating fails, we'll just try again at the next checkpoint.
		// Leftover segments will just be ignored in the future if there's a checkpoint
		// that supersedes them.
		level.Error(w.logger).Log("msg", "truncating segments failed", "err", err)
	}

	// The checkpoint is written and segments before it is truncated, so we no
	// longer need to track deleted series that are before it.
	w.deletedMtx.Lock()
	for ref, segment := range w.deleted {
		if segment < first {
			delete(w.deleted, ref)
			w.metrics.TotalRemovedSeries.Inc()
		}
	}
	w.metrics.NumDeletedSeries.Set(float64(len(w.deleted)))
	w.deletedMtx.Unlock()

	if err := wlog.DeleteCheckpoints(w.wal.Dir(), last); err != nil {
		// Leftover old checkpoints do not cause problems down the line beyond
		// occupying disk space.
		// They will just be ignored since a higher checkpoint exists.
		level.Error(w.logger).Log("msg", "delete old checkpoints", "err", err)
	}

	level.Info(w.logger).Log("msg", "WAL checkpoint complete",
		"first", first, "last", last, "duration", time.Since(start))
	return nil
}

// gc removes data before the minimum timestamp from the head.
func (w *Storage) gc(mint int64) {
	deleted := w.series.gc(mint)
	w.metrics.NumActiveSeries.Sub(float64(len(deleted)))

	_, last, _ := wlog.Segments(w.wal.Dir())
	w.deletedMtx.Lock()
	defer w.deletedMtx.Unlock()

	// We want to keep series records for any newly deleted series
	// until we've passed the last recorded segment. The WAL will
	// still contain samples records with all of the ref IDs until
	// the segment's samples has been deleted from the checkpoint.
	//
	// If the series weren't kept on startup when the WAL was replied,
	// the samples wouldn't be able to be used since there wouldn't
	// be any labels for that ref ID.
	for ref := range deleted {
		w.deleted[ref] = last
	}

	w.metrics.NumDeletedSeries.Set(float64(len(w.deleted)))
}

// WriteStalenessMarkers appends a staleness sample for all active series.
func (w *Storage) WriteStalenessMarkers(remoteTsFunc func() int64) error {
	var lastErr error
	var lastTs int64

	app := w.Appender(context.Background())
	it := w.series.iterator()
	for series := range it.Channel() {
		var (
			ref  = series.ref
			lset = series.lset
		)

		ts := timestamp.FromTime(time.Now())
		_, err := app.Append(storage.SeriesRef(ref), lset, ts, math.Float64frombits(value.StaleNaN))
		if err != nil {
			lastErr = err
		}

		// Remove millisecond precision; the remote write timestamp we get
		// only has second precision.
		lastTs = (ts / 1000) * 1000
	}

	if lastErr == nil {
		if err := app.Commit(); err != nil {
			return fmt.Errorf("failed to commit staleness markers: %w", err)
		}

		// Wait for remote write to write the lastTs, but give up after 1m
		level.Info(w.logger).Log("msg", "waiting for remote write to write staleness markers...")

		stopCh := time.After(1 * time.Minute)
		start := time.Now()

	Outer:
		for {
			select {
			case <-stopCh:
				level.Error(w.logger).Log("msg", "timed out waiting for staleness markers to be written")
				break Outer
			default:
				writtenTs := remoteTsFunc()
				if writtenTs >= lastTs {
					duration := time.Since(start)
					level.Info(w.logger).Log("msg", "remote write wrote staleness markers", "duration", duration)
					break Outer
				}

				level.Info(w.logger).Log("msg", "remote write hasn't written staleness markers yet", "remoteTs", writtenTs, "lastTs", lastTs)

				// Wait a bit before reading again
				time.Sleep(5 * time.Second)
			}
		}
	}

	return lastErr
}

// Close closes the storage and all its underlying resources.
func (w *Storage) Close() error {
	w.walMtx.Lock()
	defer w.walMtx.Unlock()

	if w.walClosed {
		return fmt.Errorf("already closed")
	}
	w.walClosed = true

	if w.metrics != nil {
		w.metrics.Unregister()
	}
	return w.wal.Close()
}

func (w *Storage) recordSize() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		size, err := dirSize(w.path)
		if err != nil {
			level.Debug(w.logger).Log("msg", "could not calculate WAL disk size", "path", w.path, "err", err)
			continue
		}
		w.metrics.DiskSize.Set(float64(size))
	}
}

func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			size += info.Size()
		}

		return err
	})

	return size, err
}

type appender struct {
	w *Storage
	// Notify the underlying storage that some sample is written
	notify    func()
	series    []record.RefSeries
	samples   []record.RefSample
	exemplars []record.RefExemplar
}

var _ storage.Appender = (*appender)(nil)

func (a *appender) Append(ref storage.SeriesRef, l labels.Labels, t int64, v float64) (storage.SeriesRef, error) {
	series := a.w.series.getByID(chunks.HeadSeriesRef(ref))
	if series == nil {
		// Ensure no empty or duplicate labels have gotten through. This mirrors the
		// equivalent validation code in the TSDB's headAppender.
		l = l.WithoutEmpty()
		if len(l) == 0 {
			return 0, errors.Wrap(tsdb.ErrInvalidSample, "empty labelset")
		}

		if lbl, dup := l.HasDuplicateLabelNames(); dup {
			return 0, errors.Wrap(tsdb.ErrInvalidSample, fmt.Sprintf(`label name "%s" is not unique`, lbl))
		}

		var created bool
		series, created = a.getOrCreate(l)
		if created {
			a.series = append(a.series, record.RefSeries{
				Ref:    series.ref,
				Labels: l,
			})

			a.w.metrics.NumActiveSeries.Inc()
			a.w.metrics.TotalCreatedSeries.Inc()
		}
	}

	series.Lock()
	defer series.Unlock()

	// Update last recorded timestamp. Used by Storage.gc to determine if a
	// series is stale.
	series.updateTs(t)

	a.samples = append(a.samples, record.RefSample{
		Ref: series.ref,
		T:   t,
		V:   v,
	})

	a.w.metrics.TotalAppendedSamples.Inc()
	return storage.SeriesRef(series.ref), nil
}

func (a *appender) getOrCreate(l labels.Labels) (series *memSeries, created bool) {
	hash := l.Hash()

	series = a.w.series.getByHash(hash, l)
	if series != nil {
		return series, false
	}

	series = &memSeries{ref: chunks.HeadSeriesRef(a.w.ref.Inc()), lset: l}
	a.w.series.set(l.Hash(), series)
	return series, true
}

func (a *appender) AppendExemplar(ref storage.SeriesRef, _ labels.Labels, e exemplar.Exemplar) (storage.SeriesRef, error) {
	s := a.w.series.getByID(chunks.HeadSeriesRef(ref))
	if s == nil {
		return 0, fmt.Errorf("unknown series ref. when trying to add exemplar: %d", ref)
	}

	// Ensure no empty labels have gotten through.
	e.Labels = e.Labels.WithoutEmpty()

	if lbl, dup := e.Labels.HasDuplicateLabelNames(); dup {
		return 0, errors.Wrap(tsdb.ErrInvalidExemplar, fmt.Sprintf(`label name "%s" is not unique`, lbl))
	}

	// Exemplar label length does not include chars involved in text rendering such as quotes
	// equals sign, or commas. See definition of const ExemplarMaxLabelLength.
	labelSetLen := 0
	for _, l := range e.Labels {
		labelSetLen += utf8.RuneCountInString(l.Name)
		labelSetLen += utf8.RuneCountInString(l.Value)

		if labelSetLen > exemplar.ExemplarMaxLabelSetLength {
			return 0, storage.ErrExemplarLabelLength
		}
	}

	a.exemplars = append(a.exemplars, record.RefExemplar{
		Ref:    chunks.HeadSeriesRef(ref),
		T:      e.Ts,
		V:      e.Value,
		Labels: e.Labels,
	})

	return storage.SeriesRef(s.ref), nil
}

func (a *appender) UpdateMetadata(_ storage.SeriesRef, _ labels.Labels, _ metadata.Metadata) (storage.SeriesRef, error) {
	return 0, nil
}

func (a *appender) AppendHistogram(_ storage.SeriesRef, _ labels.Labels, _ int64, _ *histogram.Histogram, _ *histogram.FloatHistogram) (storage.SeriesRef, error) {
	// TODO: support native histograms
	return 0, nil
}

func (a *appender) AppendHistogramCTZeroSample(_ storage.SeriesRef, _ labels.Labels, _ int64, _ int64, _ *histogram.Histogram, _ *histogram.FloatHistogram) (storage.SeriesRef, error) {
	// TODO: support histogram created timestamps
	return 0, nil
}

func (a *appender) AppendCTZeroSample(_ storage.SeriesRef, _ labels.Labels, _ int64, _ int64) (storage.SeriesRef, error) {
	// TODO: support created timestamp
	return 0, nil
}

func (a *appender) SetOptions(_ *storage.AppendOptions) {}

// Commit submits the collected samples and purges the batch.
func (a *appender) Commit() error {
	a.w.walMtx.RLock()
	defer a.w.walMtx.RUnlock()

	if a.w.walClosed {
		return ErrWALClosed
	}

	var encoder record.Encoder
	buf := a.w.bufPool.Get().([]byte)

	if len(a.series) > 0 {
		buf = encoder.Series(a.series, buf)
		if err := a.w.wal.Log(buf); err != nil {
			return err
		}
		buf = buf[:0]
	}

	if len(a.samples) > 0 {
		buf = encoder.Samples(a.samples, buf)
		if err := a.w.wal.Log(buf); err != nil {
			return err
		}
		buf = buf[:0]
	}

	if len(a.exemplars) > 0 {
		buf = encoder.Exemplars(a.exemplars, buf)
		if err := a.w.wal.Log(buf); err != nil {
			return err
		}
		buf = buf[:0]
	}

	// Notify so that reader waiting for it can read without needing to wait for next read ticker.
	if a.notify != nil {
		a.notify()
	} else {
		level.Warn(a.w.logger).Log("msg", "not notifying about WAL writes because notifier is not set")
	}

	//nolint:staticcheck
	a.w.bufPool.Put(buf)

	for _, sample := range a.samples {
		series := a.w.series.getByID(sample.Ref)
		if series != nil {
			series.Lock()
			series.pendingCommit = false
			series.Unlock()
		}
	}

	return a.Rollback()
}

func (a *appender) Rollback() error {
	a.series = a.series[:0]
	a.samples = a.samples[:0]
	a.exemplars = a.exemplars[:0]
	a.w.appenderPool.Put(a)
	return nil
}
