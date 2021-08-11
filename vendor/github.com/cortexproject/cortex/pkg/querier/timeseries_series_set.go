package querier

import (
	"sort"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"

	"github.com/cortexproject/cortex/pkg/cortexpb"
)

// timeSeriesSeriesSet is a wrapper around a cortexpb.TimeSeries slice to implement to SeriesSet interface
type timeSeriesSeriesSet struct {
	ts []cortexpb.TimeSeries
	i  int
}

func newTimeSeriesSeriesSet(series []cortexpb.TimeSeries) *timeSeriesSeriesSet {
	sort.Sort(byTimeSeriesLabels(series))
	return &timeSeriesSeriesSet{
		ts: series,
		i:  -1,
	}
}

// Next implements storage.SeriesSet interface.
func (t *timeSeriesSeriesSet) Next() bool { t.i++; return t.i < len(t.ts) }

// At implements storage.SeriesSet interface.
func (t *timeSeriesSeriesSet) At() storage.Series {
	if t.i < 0 {
		return nil
	}
	return &timeseries{series: t.ts[t.i]}
}

// Err implements storage.SeriesSet interface.
func (t *timeSeriesSeriesSet) Err() error { return nil }

// Warnings implements storage.SeriesSet interface.
func (t *timeSeriesSeriesSet) Warnings() storage.Warnings { return nil }

// timeseries is a type wrapper that implements the storage.Series interface
type timeseries struct {
	series cortexpb.TimeSeries
}

// timeSeriesSeriesIterator is a wrapper around a cortexpb.TimeSeries to implement the SeriesIterator interface
type timeSeriesSeriesIterator struct {
	ts *timeseries
	i  int
}

type byTimeSeriesLabels []cortexpb.TimeSeries

func (b byTimeSeriesLabels) Len() int      { return len(b) }
func (b byTimeSeriesLabels) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byTimeSeriesLabels) Less(i, j int) bool {
	return labels.Compare(cortexpb.FromLabelAdaptersToLabels(b[i].Labels), cortexpb.FromLabelAdaptersToLabels(b[j].Labels)) < 0
}

// Labels implements the storage.Series interface.
// Conversion is safe because ingester sets these by calling client.FromLabelsToLabelAdapters which guarantees labels are sorted.
func (t *timeseries) Labels() labels.Labels {
	return cortexpb.FromLabelAdaptersToLabels(t.series.Labels)
}

// Iterator implements the storage.Series interface
func (t *timeseries) Iterator() chunkenc.Iterator {
	return &timeSeriesSeriesIterator{
		ts: t,
		i:  -1,
	}
}

// Seek implements SeriesIterator interface
func (t *timeSeriesSeriesIterator) Seek(s int64) bool {
	offset := 0
	if t.i > 0 {
		offset = t.i // only advance via Seek
	}

	t.i = sort.Search(len(t.ts.series.Samples[offset:]), func(i int) bool {
		return t.ts.series.Samples[offset+i].TimestampMs >= s
	}) + offset

	return t.i < len(t.ts.series.Samples)
}

// At implements the SeriesIterator interface
func (t *timeSeriesSeriesIterator) At() (int64, float64) {
	if t.i < 0 || t.i >= len(t.ts.series.Samples) {
		return 0, 0
	}
	return t.ts.series.Samples[t.i].TimestampMs, t.ts.series.Samples[t.i].Value
}

// Next implements the SeriesIterator interface
func (t *timeSeriesSeriesIterator) Next() bool { t.i++; return t.i < len(t.ts.series.Samples) }

// Err implements the SeriesIterator interface
func (t *timeSeriesSeriesIterator) Err() error { return nil }
