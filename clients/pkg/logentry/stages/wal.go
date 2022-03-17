package stages

import (
	"sort"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/util"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
)

// WAL interface allows us to have a no-op WAL when the WAL is disabled.
type WAL interface {
	Start()
	// Log marshalls the records and writes it into the WAL.
	Log(*ingester.WALRecord) error
	// Stop stops all the WAL operations.
	Stop() error
}

type walStage struct {
	logger log.Logger
	wal    WAL

	buf []byte // buffer used to compute fps.
}

func newWalStage(logger log.Logger, wal WAL) *walStage {
	s := &walStage{
		logger: logger,
		wal:    wal,

		buf: make([]byte, 64), // fingerprints are uint64
	}

	return s
}

func (w *walStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	w.wal.Start()
	go func() {
		defer func() {
			close(out)
			// log the error
			w.wal.Stop()
		}()
		r := &ingester.WALRecord{
			Series:     make([]record.RefSeries, 0, 1),
			RefEntries: make([]ingester.RefEntries, 0, 1),
		}
		for e := range in {
			r.Reset()
			lbs := labels.FromMap(util.ModelLabelSetToMap(e.Labels))
			sort.Sort(lbs)
			var fp uint64
			fp, w.buf = lbs.HashWithoutLabels(w.buf, []string(nil)...)
			r.Series = []record.RefSeries{
				{
					Labels: lbs,
					Ref:    chunks.HeadSeriesRef(fp),
				},
			}
			// todo think about the counter.
			r.AddEntries(fp, 0, e.Entry.Entry)
			w.wal.Log(r)
			out <- e

		}
	}()
	return out
}
