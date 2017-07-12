package chunk

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/metric"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
)

func TestLazySeriesIterator_Metric(t *testing.T) {
	store := newTestChunkStore(t, StoreConfig{})
	now := model.Now()
	sampleMetric := model.Metric{model.MetricNameLabel: "foo"}
	iterator, err := NewLazySeriesIterator(store, sampleMetric, now, now, userID)
	require.NoError(t, err)
	for _, c := range []struct {
		iterator       *LazySeriesIterator
		expectedMetric metric.Metric
	}{
		{
			iterator:       iterator,
			expectedMetric: metric.Metric{Metric: sampleMetric},
		},
	} {
		metric := c.iterator.Metric()
		require.Equal(t, c.expectedMetric, metric)
	}
}

func TestLazySeriesIterator_ValueAtOrBeforeTime(t *testing.T) {
	now := model.Now()
	ctx := user.InjectOrgID(context.Background(), userID)

	sampleMetric := model.Metric{model.MetricNameLabel: "foo"}
	dummyChunk := dummyChunkFor(sampleMetric)
	dummySamples, err := dummyChunk.Samples()
	require.NoError(t, err)

	schemas := []struct {
		name string
		fn   func(cfg SchemaConfig) Schema
	}{
		{"v8 schema", v8Schema},
	}

	for _, schema := range schemas {
		// Create store with the dummy chunk.
		store := newTestChunkStore(t, StoreConfig{schemaFactory: schema.fn})
		store.Put(ctx, []Chunk{dummyChunk})

		// Create the lazy series iterator
		iterator, err := NewLazySeriesIterator(store, sampleMetric, now, now, userID)
		require.NoError(t, err)
		for _, tc := range []struct {
			iterator       *LazySeriesIterator
			timestamp      model.Time
			expectedSample model.SamplePair
		}{
			{
				iterator:       iterator,
				timestamp:      now,
				expectedSample: dummySamples[0],
			},
		} {
			// sampleSeriesIterator should be created lazily only when RangeValues is called.
			require.Nil(t, tc.iterator.sampleSeriesIterator)
			sample := tc.iterator.ValueAtOrBeforeTime(tc.timestamp)
			require.NotNil(t, tc.iterator.sampleSeriesIterator)

			require.Equal(t, tc.expectedSample, sample)
		}
	}
}

func TestLazySeriesIterator_RangeValues(t *testing.T) {
	now := model.Now()
	ctx := user.InjectOrgID(context.Background(), userID)

	sampleMetric := model.Metric{model.MetricNameLabel: "foo"}
	dummyChunk := dummyChunkFor(sampleMetric)
	dummySamples, err := dummyChunk.Samples()
	require.NoError(t, err)

	schemas := []struct {
		name string
		fn   func(cfg SchemaConfig) Schema
	}{
		{"v8 schema", v8Schema},
	}

	for _, schema := range schemas {
		// Create store with the dummy chunk.
		store := newTestChunkStore(t, StoreConfig{schemaFactory: schema.fn})
		store.Put(ctx, []Chunk{dummyChunk})

		// Create the lazy series iterator
		iterator, err := NewLazySeriesIterator(store, sampleMetric, now, now, userID)
		require.NoError(t, err)
		for _, tc := range []struct {
			iterator        *LazySeriesIterator
			interval        metric.Interval
			expectedSamples []model.SamplePair
		}{
			{
				iterator:        iterator,
				interval:        metric.Interval{OldestInclusive: now, NewestInclusive: now},
				expectedSamples: dummySamples,
			},
		} {
			// sampleSeriesIterator should be created lazily only when RangeValues is called.
			require.Nil(t, tc.iterator.sampleSeriesIterator)
			samples := tc.iterator.RangeValues(tc.interval)
			require.NotNil(t, tc.iterator.sampleSeriesIterator)

			require.Equal(t, tc.expectedSamples, samples)
		}
	}
}
