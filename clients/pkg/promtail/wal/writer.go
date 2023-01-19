package wal

import (
	"sort"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
)

type Writer struct {
	entries     chan api.Entry
	log         log.Logger
	wg          sync.WaitGroup
	once        sync.Once
	wal         WAL
	entryWriter *entryWriter
}

func NewWriter(wal WAL, logger log.Logger) *Writer {
	return &Writer{
		entries:     make(chan api.Entry),
		log:         logger,
		wg:          sync.WaitGroup{},
		wal:         wal,
		entryWriter: newEntryWriter(),
	}
}

func (wrt *Writer) start() {
	wrt.wg.Add(1)
	go func() {
		defer wrt.wg.Done()
		for e := range wrt.entries {
			wrt.entryWriter.WriteEntry(e, wrt.wal, wrt.log)
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
	wrt.wg.Wait()
}

// entryWriter creates a new entry writer, which keeps in memory a single ingester.WALRecord object that's reused
// across every write.
type entryWriter struct {
	reusableWALRecord *ingester.WALRecord
}

// newEntryWriter creates a new entryWriter.
func newEntryWriter() *entryWriter {
	return &entryWriter{
		reusableWALRecord: &ingester.WALRecord{
			RefEntries: make([]ingester.RefEntries, 0, 1),
			Series:     make([]record.RefSeries, 0, 1),
		},
	}
}

// WriteEntry writes an api.Entry to a WAL. Note that since it's re-using the same ingester.WALRecord object for every
// write, it first has to be reset, and then overwritten accordingly. Because of this, WriteEntry IS NOT THREAD SAFE.
func (ew *entryWriter) WriteEntry(entry api.Entry, wal WAL, logger log.Logger) {
	// Reset wal record slices
	ew.reusableWALRecord.RefEntries = ew.reusableWALRecord.RefEntries[:0]
	ew.reusableWALRecord.Series = ew.reusableWALRecord.Series[:0]

	defer func() {
		err := wal.Log(ew.reusableWALRecord)
		if err != nil {
			level.Error(logger).Log("msg", "failed to write to WAL", "err", err)
		}
	}()

	var fp uint64
	lbs := labels.FromMap(util.ModelLabelSetToMap(entry.Labels))
	sort.Sort(lbs)
	fp, _ = lbs.HashWithoutLabels(nil, []string(nil)...)

	// Append the entry to an already existing stream (if any)
	ew.reusableWALRecord.RefEntries = append(ew.reusableWALRecord.RefEntries, ingester.RefEntries{
		Ref: chunks.HeadSeriesRef(fp),
		Entries: []logproto.Entry{
			entry.Entry,
		},
	})
	ew.reusableWALRecord.Series = append(ew.reusableWALRecord.Series, record.RefSeries{
		Ref:    chunks.HeadSeriesRef(fp),
		Labels: lbs,
	})
}

type NoopWriter struct {
	noopChan chan api.Entry
	wg       sync.WaitGroup
	once     sync.Once
}

func (n *NoopWriter) Chan() chan<- api.Entry {
	return n.noopChan
}

func (n *NoopWriter) Stop() {
	n.once.Do(func() { close(n.noopChan) })
}

func NewNoopWriter() *NoopWriter {
	c := &NoopWriter{noopChan: make(chan api.Entry)}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for range c.noopChan {
			// noop
		}
	}()
	return c
}
