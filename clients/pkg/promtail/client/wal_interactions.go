package client

import (
	"sort"

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

// WALEntryWriter creates a new entry writer, which keeps in memory a single ingester.WALRecord object that's reused
// across every write.
type WALEntryWriter struct {
	reusableWALRecord *ingester.WALRecord
}

// NewWALEntryWriter creates a new WALEntryWriter.
func NewWALEntryWriter() *WALEntryWriter {
	return &WALEntryWriter{
		reusableWALRecord: &ingester.WALRecord{
			RefEntries: make([]ingester.RefEntries, 0, 1),
			Series:     make([]record.RefSeries, 0, 1),
		},
	}
}

// WriteEntry writes an api.Entry to a WAL. Note that since it's re-using the same ingester.WALRecord object for every
// write, it first has to be reset, and then overwritten accordingly. Because of this, WriteEntry IS NOT THREAD SAFE.
func (ew *WALEntryWriter) WriteEntry(entry api.Entry, wal WAL, logger log.Logger) {
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
