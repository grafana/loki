package queryrange

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/cortexproject/cortex/pkg/querier/series"
)

// genLabels will create a slice of labels where each label has an equal chance to occupy a value from [0,labelBuckets]. It returns a slice of length labelBuckets^len(labelSet)
func genLabels(
	labelSet []string,
	labelBuckets int,
) (result []labels.Labels) {
	if len(labelSet) == 0 {
		return result
	}

	l := labelSet[0]
	rest := genLabels(labelSet[1:], labelBuckets)

	for i := 0; i < labelBuckets; i++ {
		x := labels.Label{
			Name:  l,
			Value: fmt.Sprintf("%d", i),
		}
		if len(rest) == 0 {
			set := labels.Labels{x}
			result = append(result, set)
			continue
		}
		for _, others := range rest {
			set := append(others, x)
			result = append(result, set)
		}
	}
	return result

}

// NewMockShardedQueryable creates a shard-aware in memory queryable.
func NewMockShardedQueryable(
	nSamples int,
	labelSet []string,
	labelBuckets int,
	delayPerSeries time.Duration,
) *MockShardedQueryable {
	samples := make([]model.SamplePair, 0, nSamples)
	for i := 0; i < nSamples; i++ {
		samples = append(samples, model.SamplePair{
			Timestamp: model.Time(i * 1000),
			Value:     model.SampleValue(i),
		})
	}
	sets := genLabels(labelSet, labelBuckets)
	xs := make([]storage.Series, 0, len(sets))
	for _, ls := range sets {
		xs = append(xs, series.NewConcreteSeries(ls, samples))
	}

	return &MockShardedQueryable{
		series:         xs,
		delayPerSeries: delayPerSeries,
	}
}

// MockShardedQueryable is exported to be reused in the querysharding benchmarking
type MockShardedQueryable struct {
	series         []storage.Series
	delayPerSeries time.Duration
}

// Querier impls storage.Queryable
func (q *MockShardedQueryable) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return q, nil
}

// Select implements storage.Querier interface.
// The bool passed is ignored because the series is always sorted.
func (q *MockShardedQueryable) Select(_ bool, _ *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	tStart := time.Now()

	shard, _, err := astmapper.ShardFromMatchers(matchers)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	var (
		start int
		end   int
	)

	if shard == nil {
		start = 0
		end = len(q.series)
	} else {
		// return the series range associated with this shard
		seriesPerShard := len(q.series) / shard.Of
		start = shard.Shard * seriesPerShard
		end = start + seriesPerShard

		// if we're clipping an odd # of series, add the final series to the last shard
		if end == len(q.series)-1 && len(q.series)%2 == 1 {
			end = len(q.series)
		}
	}

	var name string
	for _, m := range matchers {
		if m.Type == labels.MatchEqual && m.Name == "__name__" {
			name = m.Value
		}
	}

	results := make([]storage.Series, 0, end-start)
	for i := start; i < end; i++ {
		results = append(results, &ShardLabelSeries{
			shard:  shard,
			name:   name,
			Series: q.series[i],
		})
	}

	// loosely enforce the assumption that an operation on 1/nth of the data
	// takes 1/nth of the time.
	duration := q.delayPerSeries * time.Duration(len(q.series))
	if shard != nil {
		duration = duration / time.Duration(shard.Of)
	}

	remaining := time.Until(tStart.Add(duration))
	if remaining > 0 {
		time.Sleep(remaining)
	}

	// sorted
	return series.NewConcreteSeriesSet(results)
}

// ShardLabelSeries allows extending a Series with new labels. This is helpful for adding cortex shard labels
type ShardLabelSeries struct {
	shard *astmapper.ShardAnnotation
	name  string
	storage.Series
}

// Labels impls storage.Series
func (s *ShardLabelSeries) Labels() labels.Labels {
	ls := s.Series.Labels()

	if s.name != "" {
		ls = append(ls, labels.Label{
			Name:  "__name__",
			Value: s.name,
		})
	}

	if s.shard != nil {
		ls = append(ls, s.shard.Label())
	}

	return ls
}

// LabelValues impls storage.Querier
func (q *MockShardedQueryable) LabelValues(name string, matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	return nil, nil, errors.Errorf("unimplemented")
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (q *MockShardedQueryable) LabelNames() ([]string, storage.Warnings, error) {
	return nil, nil, errors.Errorf("unimplemented")
}

// Close releases the resources of the Querier.
func (q *MockShardedQueryable) Close() error {
	return nil
}
