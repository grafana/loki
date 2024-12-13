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

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/util/annotations"
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

// Next iterates through a series set and implements storage.SeriesSet.
func (c *ConcreteSeriesSet) Next() bool {
	c.cur++
	return c.cur < len(c.series)
}

// At returns the current series and implements storage.SeriesSet.
func (c *ConcreteSeriesSet) At() storage.Series {
	return c.series[c.cur]
}

// Err implements storage.SeriesSet.
func (c *ConcreteSeriesSet) Err() error {
	return nil
}

// Warnings implements storage.SeriesSet.
func (c *ConcreteSeriesSet) Warnings() annotations.Annotations {
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

// Labels implements storage.Series
func (c *ConcreteSeries) Labels() labels.Labels {
	return c.labels
}

// Iterator implements storage.Series
func (c *ConcreteSeries) Iterator(_ chunkenc.Iterator) chunkenc.Iterator {
	return NewConcreteSeriesIterator(c)
}

// concreteSeriesIterator implements chunkenc.Iterator.
type concreteSeriesIterator struct {
	cur    int
	series *ConcreteSeries
}

// NewConcreteSeriesIterator instaniates an in memory chunkenc.Iterator
func NewConcreteSeriesIterator(series *ConcreteSeries) chunkenc.Iterator {
	return &concreteSeriesIterator{
		cur:    -1,
		series: series,
	}
}

func (c *concreteSeriesIterator) Seek(t int64) chunkenc.ValueType {
	c.cur = sort.Search(len(c.series.samples), func(n int) bool {
		return c.series.samples[n].Timestamp >= model.Time(t)
	})
	if c.cur < len(c.series.samples) {
		return chunkenc.ValFloat
	}
	return chunkenc.ValNone
}

func (c *concreteSeriesIterator) At() (t int64, v float64) {
	s := c.series.samples[c.cur]
	return int64(s.Timestamp), float64(s.Value)
}

func (c *concreteSeriesIterator) AtHistogram(*histogram.Histogram) (int64, *histogram.Histogram) {
	// TODO: support native histograms.
	// This method is called when Next() returns ValHistogram
	return 0, nil
}

func (c *concreteSeriesIterator) AtFloatHistogram(*histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	// TODO: support native histograms.
	// This method may be called when Next() returns ValHistogram or ValFloatHistogram
	return 0, nil
}

func (c *concreteSeriesIterator) AtT() (t int64) {
	s := c.series.samples[c.cur]
	return int64(s.Timestamp)
}

func (c *concreteSeriesIterator) Next() chunkenc.ValueType {
	c.cur++
	if c.cur < len(c.series.samples) {
		return chunkenc.ValFloat
	}
	return chunkenc.ValNone
}

func (c *concreteSeriesIterator) Err() error {
	return nil
}

// NewErrIterator instantiates an errIterator
func NewErrIterator(err error) chunkenc.Iterator {
	return errIterator{err}
}

// errIterator implements chunkenc.Iterator, just returning an error.
type errIterator struct {
	err error
}

func (errIterator) Seek(int64) chunkenc.ValueType {
	return chunkenc.ValNone
}

func (errIterator) Next() chunkenc.ValueType {
	return chunkenc.ValNone
}

func (errIterator) At() (t int64, v float64) {
	return 0, 0
}

func (errIterator) AtHistogram(*histogram.Histogram) (int64, *histogram.Histogram) {
	return 0, nil
}

func (errIterator) AtFloatHistogram(*histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	return 0, nil
}

func (errIterator) AtT() (t int64) {
	return 0
}

func (e errIterator) Err() error {
	return e.err
}

type byLabels []storage.Series

func (b byLabels) Len() int           { return len(b) }
func (b byLabels) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byLabels) Less(i, j int) bool { return labels.Compare(b[i].Labels(), b[j].Labels()) < 0 }

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

func (d DeletedSeries) Iterator(_ chunkenc.Iterator) chunkenc.Iterator {
	return NewDeletedSeriesIterator(d.series.Iterator(nil), d.deletedIntervals)
}

type DeletedSeriesIterator struct {
	itr              chunkenc.Iterator
	deletedIntervals []model.Interval
}

func NewDeletedSeriesIterator(itr chunkenc.Iterator, deletedIntervals []model.Interval) chunkenc.Iterator {
	return &DeletedSeriesIterator{
		itr:              itr,
		deletedIntervals: deletedIntervals,
	}
}

func (d DeletedSeriesIterator) Seek(t int64) chunkenc.ValueType {
	ty := d.itr.Seek(t)
	if ty == chunkenc.ValNone {
		return chunkenc.ValNone
	}

	seekedTs, _ := d.itr.At()
	if d.isDeleted(seekedTs) {
		// point we have seeked into is deleted, Next() should find a new non-deleted sample which is after t and seekedTs
		return d.Next()
	}

	return ty
}

func (d DeletedSeriesIterator) At() (t int64, v float64) {
	return d.itr.At()
}

func (d DeletedSeriesIterator) AtHistogram(*histogram.Histogram) (int64, *histogram.Histogram) {
	// TODO: support native histograms.
	// This method is called when Next() returns ValHistogram
	return 0, nil
}

func (d DeletedSeriesIterator) AtFloatHistogram(*histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	// TODO: support native histograms.
	// This method may be called when Next() returns ValHistogram or ValFloatHistogram
	return 0, nil
}

func (d DeletedSeriesIterator) AtT() (t int64) {
	ts, _ := d.itr.At()
	return ts
}

func (d DeletedSeriesIterator) Next() chunkenc.ValueType {
	for {
		ty := d.itr.Next()
		if ty == chunkenc.ValNone {
			break
		}

		ts, _ := d.itr.At()
		if d.isDeleted(ts) {
			continue
		}

		return ty
	}
	return chunkenc.ValNone
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

func (emptySeries) Iterator(_ chunkenc.Iterator) chunkenc.Iterator {
	return NewEmptySeriesIterator()
}

type emptySeriesIterator struct {
}

func NewEmptySeriesIterator() chunkenc.Iterator {
	return emptySeriesIterator{}
}

func (emptySeriesIterator) Seek(_ int64) chunkenc.ValueType {
	return chunkenc.ValNone
}

func (emptySeriesIterator) At() (t int64, v float64) {
	return 0, 0
}

func (emptySeriesIterator) AtHistogram(*histogram.Histogram) (int64, *histogram.Histogram) {
	return 0, nil
}

func (emptySeriesIterator) AtFloatHistogram(*histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	return 0, nil
}

func (emptySeriesIterator) AtT() (t int64) {
	return 0
}

func (emptySeriesIterator) Next() chunkenc.ValueType {
	return chunkenc.ValNone
}

func (emptySeriesIterator) Err() error {
	return nil
}
