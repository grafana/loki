package wal

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"

	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

const (
	minimumCleanSegmentsEvery = time.Second
)

// CleanupEventSubscriber is an interface that objects that want to receive events from the wal Writer can implement. After
// they can subscribe to events by adding themselves as subscribers on the Writer with writer.SubscribeCleanup.
type CleanupEventSubscriber interface {
	WriteCleanup
}

// WriteEventSubscriber is an interface that objects that want to receive an event when Writer writes to the WAL can
// implement, and later subscribe to the Writer via writer.SubscribeWrite.
type WriteEventSubscriber interface {
	// NotifyWrite allows others to be notifier when Writer writes to the underlying WAL.
	NotifyWrite()
}

// Writer implements api.EntryHandler, exposing a channel were scraping targets can write to. Reading from there, it
// writes incoming entries to a WAL.
// Also, since Writer is responsible for all changing operations over the WAL, therefore a routine is run for cleaning
// old segments.
type Writer struct {
	entries     chan api.Entry
	log         log.Logger
	wg          sync.WaitGroup
	once        sync.Once
	wal         WAL
	entryWriter *entryWriter

	cleanupSubscribersLock sync.RWMutex
	cleanupSubscribers     []CleanupEventSubscriber

	writeSubscribersLock sync.RWMutex
	writeSubscribers     []WriteEventSubscriber

	reclaimedOldSegmentsSpaceCounter *prometheus.CounterVec

	closeCleaner chan struct{}
}

// NewWriter creates a new Writer.
func NewWriter(walCfg Config, logger log.Logger, reg prometheus.Registerer) (*Writer, error) {
	// Start WAL
	wl, err := New(Config{
		Dir:     walCfg.Dir,
		Enabled: true,
	}, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("error starting WAL: %w", err)
	}

	wrt := &Writer{
		entries:      make(chan api.Entry),
		log:          logger,
		wg:           sync.WaitGroup{},
		wal:          wl,
		entryWriter:  newEntryWriter(),
		closeCleaner: make(chan struct{}, 1),
	}

	wrt.reclaimedOldSegmentsSpaceCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Subsystem: "wal_writer",
		Name:      "reclaimed_space",
		Help:      "Number of bytes reclaimed from storage.",
	}, []string{})

	if reg != nil {
		_ = reg.Register(wrt.reclaimedOldSegmentsSpaceCounter)
	}

	wrt.start(walCfg.MaxSegmentAge)
	return wrt, nil
}

func (wrt *Writer) start(maxSegmentAge time.Duration) {
	wrt.wg.Add(1)
	// main WAL writer routine
	go func() {
		defer wrt.wg.Done()
		for e := range wrt.entries {
			if err := wrt.entryWriter.WriteEntry(e, wrt.wal, wrt.log); err != nil {
				level.Error(wrt.log).Log("msg", "failed to write entry", "err", err)
				// if an error occurred while writing the wal, go to next entry and don't notify write subscribers
				continue
			}

			wrt.writeSubscribersLock.RLock()
			for _, s := range wrt.writeSubscribers {
				s.NotifyWrite()
			}
			wrt.writeSubscribersLock.RUnlock()
		}
	}()
	// WAL cleanup routine that cleans old segments
	wrt.wg.Add(1)
	go func() {
		defer wrt.wg.Done()
		// By cleaning every 10th of the configured threshold for considering a segment old, we are allowing a maximum slip
		// of 10%. If the configured time is 1 hour, that'd be 6 minutes.
		triggerEvery := maxSegmentAge / 10
		if triggerEvery < minimumCleanSegmentsEvery {
			triggerEvery = minimumCleanSegmentsEvery
		}
		trigger := time.NewTicker(triggerEvery)
		for {
			select {
			case <-trigger.C:
				level.Debug(wrt.log).Log("msg", "Running wal old segments cleanup")
				if err := wrt.cleanSegments(maxSegmentAge); err != nil {
					level.Error(wrt.log).Log("msg", "Error cleaning old segments", "err", err)
				}
				break
			case <-wrt.closeCleaner:
				trigger.Stop()
				return
			}
		}
	}()
}

func (wrt *Writer) Chan() chan<- api.Entry {
	return wrt.entries
}

func (wrt *Writer) Stop() {
	wrt.once.Do(func() {
		close(wrt.entries)
	})
	// close cleaner routine
	wrt.closeCleaner <- struct{}{}
	// Wait for routine to write to wal all pending entries
	wrt.wg.Wait()
	// Close WAL to finalize all pending writes
	wrt.wal.Close()
}

// cleanSegments will remove segments older than maxAge from the WAL directory. If there's just one segment, none will be
// deleted since it's likely there's active readers on it. In case there's multiple segments, each will be deleted if:
// - It's not the last (highest numbered) segment
// - It's last modified date is older than the max allowed age
func (wrt *Writer) cleanSegments(maxAge time.Duration) error {
	maxModifiedAt := time.Now().Add(-maxAge)
	walDir := wrt.wal.Dir()
	segments, err := listSegments(walDir)
	if err != nil {
		return fmt.Errorf("error reading segments in wal directory: %w", err)
	}
	// Only clean if there's more than one segment
	if len(segments) <= 1 {
		return nil
	}
	// find the most recent, or head segment to avoid cleaning it up
	lastSegment := -1
	maxReclaimed := -1
	for _, segment := range segments {
		if lastSegment < segment.number {
			lastSegment = segment.number
		}
	}
	for _, segment := range segments {
		if segment.lastModified.Before(maxModifiedAt) && segment.number != lastSegment {
			// segment is older than allowed age, cleaning up
			if err := os.Remove(filepath.Join(walDir, segment.name)); err != nil {
				level.Error(wrt.log).Log("msg", "Error old wal segment", "err", err, "segmentNum", segment.number)
			}
			level.Debug(wrt.log).Log("msg", "Deleted old wal segment", "segmentNum", segment.number)
			wrt.reclaimedOldSegmentsSpaceCounter.WithLabelValues().Add(float64(segment.size))
			// keep track of the largest segment number reclaimed
			if segment.number > maxReclaimed {
				maxReclaimed = segment.number
			}
		}
	}
	// if we reclaimed at least one segment, notify all subscribers
	if maxReclaimed != -1 {
		wrt.cleanupSubscribersLock.RLock()
		defer wrt.cleanupSubscribersLock.RUnlock()
		for _, subscriber := range wrt.cleanupSubscribers {
			subscriber.SeriesReset(maxReclaimed)
		}
	}
	return nil
}

// SubscribeCleanup adds a new CleanupEventSubscriber that will receive cleanup events.
func (wrt *Writer) SubscribeCleanup(subscriber CleanupEventSubscriber) {
	wrt.cleanupSubscribersLock.Lock()
	defer wrt.cleanupSubscribersLock.Unlock()
	wrt.cleanupSubscribers = append(wrt.cleanupSubscribers, subscriber)
}

// SubscribeWrite adds a new WriteEventSubscriber that will receive write events.
func (wrt *Writer) SubscribeWrite(subscriber WriteEventSubscriber) {
	wrt.writeSubscribersLock.Lock()
	defer wrt.writeSubscribersLock.Unlock()
	wrt.writeSubscribers = append(wrt.writeSubscribers, subscriber)
}

// entryWriter writes api.Entry to a WAL, keeping in memory a single Record object that's reused
// across every write.
type entryWriter struct {
	reusableWALRecord *wal.Record
}

// newEntryWriter creates a new entryWriter.
func newEntryWriter() *entryWriter {
	return &entryWriter{
		reusableWALRecord: &wal.Record{
			RefEntries: make([]wal.RefEntries, 0, 1),
			Series:     make([]record.RefSeries, 0, 1),
		},
	}
}

// WriteEntry writes an api.Entry to a WAL. Note that since it's re-using the same Record object for every
// write, it first has to be reset, and then overwritten accordingly. Therefore, WriteEntry is not thread-safe.
func (ew *entryWriter) WriteEntry(entry api.Entry, wl WAL, _ log.Logger) error {
	// Reset wal record slices
	ew.reusableWALRecord.RefEntries = ew.reusableWALRecord.RefEntries[:0]
	ew.reusableWALRecord.Series = ew.reusableWALRecord.Series[:0]

	var fp uint64
	lbs := labels.FromMap(util.ModelLabelSetToMap(entry.Labels))
	sort.Sort(lbs)
	fp, _ = lbs.HashWithoutLabels(nil, []string(nil)...)

	// Append the entry to an already existing stream (if any)
	ew.reusableWALRecord.RefEntries = append(ew.reusableWALRecord.RefEntries, wal.RefEntries{
		Ref: chunks.HeadSeriesRef(fp),
		Entries: []logproto.Entry{
			entry.Entry,
		},
	})
	ew.reusableWALRecord.Series = append(ew.reusableWALRecord.Series, record.RefSeries{
		Ref:    chunks.HeadSeriesRef(fp),
		Labels: lbs,
	})

	return wl.Log(ew.reusableWALRecord)
}

type segmentRef struct {
	name         string
	number       int
	size         int64
	lastModified time.Time
}

// listSegments list wal segments under the given directory, alongside with some file system information for each.
func listSegments(dir string) (refs []segmentRef, err error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	// the following will attempt to get segments info in a best effort manner, omitting file if error
	for _, f := range files {
		fn := f.Name()
		k, err := strconv.Atoi(fn)
		if err != nil {
			continue
		}
		fileInfo, err := f.Info()
		if err != nil {
			continue
		}
		refs = append(refs, segmentRef{
			name:         fn,
			number:       k,
			lastModified: fileInfo.ModTime(),
			size:         fileInfo.Size(),
		})
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].number < refs[j].number
	})
	for i := 0; i < len(refs)-1; i++ {
		if refs[i].number+1 != refs[i+1].number {
			return nil, fmt.Errorf("segments are not sequential")
		}
	}
	return refs, nil
}
