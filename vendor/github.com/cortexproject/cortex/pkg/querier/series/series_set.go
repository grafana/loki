// Some of the code in this file was adapted from Prometheus (https://github.com/prometheus/prometheus).
// The original license header is included below:
//
// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package series

import (
	"sort"

	"github.com/cortexproject/cortex/pkg/chunk/purger"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/cortexproject/cortex/pkg/prom1/storage/metric"
)

// ConcreteSeriesSet implements storage.SeriesSet.
type ConcreteSeriesSet struct {
	cur    int
	series []storage.Series
}

// NewConcreteSeriesSet instantiates an in-memory series set from a series
// Series will be sorted by labels.
func NewConcreteSeriesSet(series []storage.Series) storage.SeriesSet {
	sort.Sort(byLabels(series))
	return &ConcreteSeriesSet{
		cur:    -1,
		series: series,
	}
}

// Next iterates through a series set and impls storage.SeriesSet
func (c *ConcreteSeriesSet) Next() bool {
	c.cur++
	return c.cur < len(c.series)
}

// At returns the current series and impls storage.SeriesSet
func (c *ConcreteSeriesSet) At() storage.Series {
	return c.series[c.cur]
}

// Err impls storage.SeriesSet
func (c *ConcreteSeriesSet) Err() error {
	return nil
}

// ConcreteSeries implements storage.Series.
type ConcreteSeries struct {
	labels  labels.Labels
	samples []model.SamplePair
}

// NewConcreteSeries instantiates an in memory series from a list of samples & labels
func NewConcreteSeries(ls labels.Labels, samples []model.SamplePair) *ConcreteSeries {
	return &ConcreteSeries{
		labels:  ls,
		samples: samples,
	}
}

// Labels impls storage.Series
func (c *ConcreteSeries) Labels() labels.Labels {
	return c.labels
}

// Iterator impls storage.Series
func (c *ConcreteSeries) Iterator() storage.SeriesIterator {
	return NewConcreteSeriesIterator(c)
}

// concreteSeriesIterator implements storage.SeriesIterator.
type concreteSeriesIterator struct {
	cur    int
	series *ConcreteSeries
}

// NewConcreteSeriesIterator instaniates an in memory storage.SeriesIterator
func NewConcreteSeriesIterator(series *ConcreteSeries) storage.SeriesIterator {
	return &concreteSeriesIterator{
		cur:    -1,
		series: series,
	}
}

func (c *concreteSeriesIterator) Seek(t int64) bool {
	c.cur = sort.Search(len(c.series.samples), func(n int) bool {
		return c.series.samples[n].Timestamp >= model.Time(t)
	})
	return c.cur < len(c.series.samples)
}

func (c *concreteSeriesIterator) At() (t int64, v float64) {
	s := c.series.samples[c.cur]
	return int64(s.Timestamp), float64(s.Value)
}

func (c *concreteSeriesIterator) Next() bool {
	c.cur++
	return c.cur < len(c.series.samples)
}

func (c *concreteSeriesIterator) Err() error {
	return nil
}

// NewErrIterator instantiates an errIterator
func NewErrIterator(err error) storage.SeriesIterator {
	return errIterator{err}
}

// errIterator implements storage.SeriesIterator, just returning an error.
type errIterator struct {
	err error
}

func (errIterator) Seek(int64) bool {
	return false
}

func (errIterator) Next() bool {
	return false
}

func (errIterator) At() (t int64, v float64) {
	return 0, 0
}

func (e errIterator) Err() error {
	return e.err
}

// MatrixToSeriesSet creates a storage.SeriesSet from a model.Matrix
// Series will be sorted by labels.
func MatrixToSeriesSet(m model.Matrix) storage.SeriesSet {
	series := make([]storage.Series, 0, len(m))
	for _, ss := range m {
		series = append(series, &ConcreteSeries{
			labels:  metricToLabels(ss.Metric),
			samples: ss.Values,
		})
	}
	return NewConcreteSeriesSet(series)
}

// MetricsToSeriesSet creates a storage.SeriesSet from a []metric.Metric
func MetricsToSeriesSet(ms []metric.Metric) storage.SeriesSet {
	series := make([]storage.Series, 0, len(ms))
	for _, m := range ms {
		series = append(series, &ConcreteSeries{
			labels:  metricToLabels(m.Metric),
			samples: nil,
		})
	}
	return NewConcreteSeriesSet(series)
}

func metricToLabels(m model.Metric) labels.Labels {
	ls := make(labels.Labels, 0, len(m))
	for k, v := range m {
		ls = append(ls, labels.Label{
			Name:  string(k),
			Value: string(v),
		})
	}
	// PromQL expects all labels to be sorted! In general, anyone constructing
	// a labels.Labels list is responsible for sorting it during construction time.
	sort.Sort(ls)
	return ls
}

type byLabels []storage.Series

func (b byLabels) Len() int           { return len(b) }
func (b byLabels) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byLabels) Less(i, j int) bool { return labels.Compare(b[i].Labels(), b[j].Labels()) < 0 }

type DeletedSeriesSet struct {
	seriesSet     storage.SeriesSet
	tombstones    *purger.TombstonesSet
	queryInterval model.Interval
}

func NewDeletedSeriesSet(seriesSet storage.SeriesSet, tombstones *purger.TombstonesSet, queryInterval model.Interval) storage.SeriesSet {
	return &DeletedSeriesSet{
		seriesSet:     seriesSet,
		tombstones:    tombstones,
		queryInterval: queryInterval,
	}
}

func (d DeletedSeriesSet) Next() bool {
	return d.seriesSet.Next()
}

func (d DeletedSeriesSet) At() storage.Series {
	series := d.seriesSet.At()
	deletedIntervals := d.tombstones.GetDeletedIntervals(series.Labels(), d.queryInterval.Start, d.queryInterval.End)

	// series is deleted for whole query range so return empty series
	if len(deletedIntervals) == 1 && deletedIntervals[0] == d.queryInterval {
		return NewEmptySeries(series.Labels())
	}

	return NewDeletedSeries(series, deletedIntervals)
}

func (d DeletedSeriesSet) Err() error {
	return d.seriesSet.Err()
}

type DeletedSeries struct {
	series           storage.Series
	deletedIntervals []model.Interval
}

func NewDeletedSeries(series storage.Series, deletedIntervals []model.Interval) storage.Series {
	return &DeletedSeries{
		series:           series,
		deletedIntervals: deletedIntervals,
	}
}

func (d DeletedSeries) Labels() labels.Labels {
	return d.series.Labels()
}

func (d DeletedSeries) Iterator() storage.SeriesIterator {
	return NewDeletedSeriesIterator(d.series.Iterator(), d.deletedIntervals)
}

type DeletedSeriesIterator struct {
	itr              storage.SeriesIterator
	deletedIntervals []model.Interval
}

func NewDeletedSeriesIterator(itr storage.SeriesIterator, deletedIntervals []model.Interval) storage.SeriesIterator {
	return &DeletedSeriesIterator{
		itr:              itr,
		deletedIntervals: deletedIntervals,
	}
}

func (d DeletedSeriesIterator) Seek(t int64) bool {
	if found := d.itr.Seek(t); !found {
		return false
	}

	seekedTs, _ := d.itr.At()
	if d.isDeleted(seekedTs) {
		// point we have seeked into is deleted, Next() should find a new non-deleted sample which is after t and seekedTs
		return d.Next()
	}

	return true
}

func (d DeletedSeriesIterator) At() (t int64, v float64) {
	return d.itr.At()
}

func (d DeletedSeriesIterator) Next() bool {
	for d.itr.Next() {
		ts, _ := d.itr.At()

		if d.isDeleted(ts) {
			continue
		}
		return true
	}
	return false
}

func (d DeletedSeriesIterator) Err() error {
	return d.itr.Err()
}

// isDeleted removes intervals which are past ts while checking for whether ts happens to be in one of the deleted intervals
func (d *DeletedSeriesIterator) isDeleted(ts int64) bool {
	mts := model.Time(ts)

	for _, interval := range d.deletedIntervals {
		if mts > interval.End {
			d.deletedIntervals = d.deletedIntervals[1:]
			continue
		} else if mts < interval.Start {
			return false
		}

		return true
	}

	return false
}

type emptySeries struct {
	labels labels.Labels
}

func NewEmptySeries(labels labels.Labels) storage.Series {
	return emptySeries{labels}
}

func (e emptySeries) Labels() labels.Labels {
	return e.labels
}

func (emptySeries) Iterator() storage.SeriesIterator {
	return NewEmptySeriesIterator()
}

type emptySeriesIterator struct {
}

func NewEmptySeriesIterator() storage.SeriesIterator {
	return emptySeriesIterator{}
}

func (emptySeriesIterator) Seek(t int64) bool {
	return false
}

func (emptySeriesIterator) At() (t int64, v float64) {
	return 0, 0
}

func (emptySeriesIterator) Next() bool {
	return false
}

func (emptySeriesIterator) Err() error {
	return nil
}

type emptySeriesSet struct{}

func (emptySeriesSet) Next() bool         { return false }
func (emptySeriesSet) At() storage.Series { return nil }
func (emptySeriesSet) Err() error         { return nil }

// NewEmptySeriesSet returns a new series set that contains no series.
func NewEmptySeriesSet() storage.SeriesSet {
	return emptySeriesSet{}
}
