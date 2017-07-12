package chunk

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/local"
	"github.com/prometheus/prometheus/storage/metric"
	"github.com/weaveworks/common/user"
)

// LazySeriesIterator is a struct and not just a renamed type because otherwise the Metric
// field and Metric() methods would clash.
type LazySeriesIterator struct {
	// The metric corresponding to the iterator.
	metric     model.Metric
	metricName model.LabelValue
	from       model.Time
	through    model.Time
	matchers   []*metric.LabelMatcher

	// The store used to fetch chunks and samples.
	chunkStore *Store
	// The sampleSeriesIterator is created on the first sample request. This
	// does not happen with promQL queries which do not require sample data to
	// be fetched. Use sync.Once to ensure the iterator is only created once.
	sampleSeriesIterator *local.SeriesIterator
	onceCreateIterator   sync.Once
	orgID                string
}

type byMatcherLabel metric.LabelMatchers

func (lms byMatcherLabel) Len() int           { return len(lms) }
func (lms byMatcherLabel) Swap(i, j int)      { lms[i], lms[j] = lms[j], lms[i] }
func (lms byMatcherLabel) Less(i, j int) bool { return lms[i].Name < lms[j].Name }

// NewLazySeriesIterator creates a LazySeriesIterator.
func NewLazySeriesIterator(chunkStore *Store, seriesMetric model.Metric, from model.Time, through model.Time, orgID string) (*LazySeriesIterator, error) {
	metricName, ok := seriesMetric[model.MetricNameLabel]
	if !ok {
		return nil, fmt.Errorf("series does not have a metric name")
	}

	var matchers metric.LabelMatchers
	for labelName, labelValue := range seriesMetric {
		if labelName == "__name__" {
			continue
		}

		matcher, err := metric.NewLabelMatcher(metric.Equal, labelName, labelValue)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, matcher)
	}
	sort.Sort(byMatcherLabel(matchers))

	return &LazySeriesIterator{
		chunkStore: chunkStore,
		metric:     seriesMetric,
		metricName: metricName,
		from:       from,
		through:    through,
		matchers:   matchers,
		orgID:      orgID,
	}, nil
}

// Metric implements the SeriesIterator interface.
func (it *LazySeriesIterator) Metric() metric.Metric {
	return metric.Metric{Metric: it.metric}
}

// ValueAtOrBeforeTime implements the SeriesIterator interface.
func (it *LazySeriesIterator) ValueAtOrBeforeTime(t model.Time) model.SamplePair {
	var err error
	it.onceCreateIterator.Do(func() {
		err = it.createSampleSeriesIterator()
	})
	if err != nil {
		// TODO: Handle error.
		return model.ZeroSamplePair
	}
	return (*it.sampleSeriesIterator).ValueAtOrBeforeTime(t)
}

// RangeValues implements the SeriesIterator interface.
func (it *LazySeriesIterator) RangeValues(in metric.Interval) []model.SamplePair {
	var err error
	it.onceCreateIterator.Do(func() {
		err = it.createSampleSeriesIterator()
	})
	if err != nil {
		// TODO: Handle error.
		fmt.Printf("ERROR %+v", err)
		return nil
	}
	return (*it.sampleSeriesIterator).RangeValues(in)
}

// Close implements the SeriesIterator interface.
func (it *LazySeriesIterator) Close() {}

func (it *LazySeriesIterator) createSampleSeriesIterator() error {
	ctx := user.InjectOrgID(context.Background(), it.orgID)
	sampleSeriesIterators, err := it.chunkStore.getMetricNameIterators(ctx, it.from, it.through, it.matchers, it.metricName)
	if err != nil {
		return err
	}

	// We should only expect one sampleSeriesIterator because we are dealing
	// with one series.
	if len(sampleSeriesIterators) != 1 {
		return fmt.Errorf("multiple series found in LazySeriesIterator chunks")
	}

	it.sampleSeriesIterator = &sampleSeriesIterators[0]
	return nil
}
