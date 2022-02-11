package base

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/scrape"
	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/prom1/storage/metric"
	"github.com/grafana/loki/pkg/querier/batch"
	"github.com/grafana/loki/pkg/querier/iterators"
	"github.com/grafana/loki/pkg/storage/chunk"
	promchunk "github.com/grafana/loki/pkg/storage/chunk/encoding"
)

const (
	userID          = "userID"
	fp              = 1
	chunkOffset     = 1 * time.Hour
	chunkLength     = 3 * time.Hour
	sampleRate      = 15 * time.Second
	samplesPerChunk = chunkLength / sampleRate
)

type query struct {
	query    string
	labels   labels.Labels
	samples  func(from, through time.Time, step time.Duration) int
	expected func(t int64) (int64, float64)
	step     time.Duration
}

var (
	testcases = []struct {
		name string
		f    chunkIteratorFunc
	}{
		{"matrixes", mergeChunks},
		{"iterators", iterators.NewChunkMergeIterator},
		{"batches", batch.NewChunkMergeIterator},
	}

	encodings = []struct {
		name string
		e    promchunk.Encoding
	}{
		{"DoubleDelta", promchunk.DoubleDelta},
		{"Varbit", promchunk.Varbit},
		{"Bigchunk", promchunk.Bigchunk},
		{"PrometheusXorChunk", promchunk.PrometheusXorChunk},
	}

	queries = []query{
		// Windowed rates with small step;  This will cause BufferedIterator to read
		// all the samples.
		{
			query:  "rate(foo[1m])",
			step:   sampleRate * 4,
			labels: labels.Labels{},
			samples: func(from, through time.Time, step time.Duration) int {
				return int(through.Sub(from) / step)
			},
			expected: func(t int64) (int64, float64) {
				return t + int64((sampleRate*4)/time.Millisecond), 1000.0
			},
		},

		// Very simple single-point gets, with low step.  Performance should be
		// similar to above.
		{
			query: "foo",
			step:  sampleRate * 4,
			labels: labels.Labels{
				labels.Label{Name: model.MetricNameLabel, Value: "foo"},
			},
			samples: func(from, through time.Time, step time.Duration) int {
				return int(through.Sub(from)/step) + 1
			},
			expected: func(t int64) (int64, float64) {
				return t, float64(t)
			},
		},

		// Rates with large step; excersise everything.
		{
			query:  "rate(foo[1m])",
			step:   sampleRate * 4 * 10,
			labels: labels.Labels{},
			samples: func(from, through time.Time, step time.Duration) int {
				return int(through.Sub(from) / step)
			},
			expected: func(t int64) (int64, float64) {
				return t + int64((sampleRate*4)/time.Millisecond)*10, 1000.0
			},
		},

		// Single points gets with large step; excersise Seek performance.
		{
			query: "foo",
			step:  sampleRate * 4 * 10,
			labels: labels.Labels{
				labels.Label{Name: model.MetricNameLabel, Value: "foo"},
			},
			samples: func(from, through time.Time, step time.Duration) int {
				return int(through.Sub(from)/step) + 1
			},
			expected: func(t int64) (int64, float64) {
				return t, float64(t)
			},
		},
	}
)

type errDistributor struct{}

var errDistributorError = fmt.Errorf("errDistributorError")

func (m *errDistributor) Query(ctx context.Context, from, to model.Time, matchers ...*labels.Matcher) (model.Matrix, error) {
	return nil, errDistributorError
}
func (m *errDistributor) QueryStream(ctx context.Context, from, to model.Time, matchers ...*labels.Matcher) (*client.QueryStreamResponse, error) {
	return nil, errDistributorError
}
func (m *errDistributor) QueryExemplars(ctx context.Context, from, to model.Time, matchers ...[]*labels.Matcher) (*client.ExemplarQueryResponse, error) {
	return nil, errDistributorError
}
func (m *errDistributor) LabelValuesForLabelName(context.Context, model.Time, model.Time, model.LabelName, ...*labels.Matcher) ([]string, error) {
	return nil, errDistributorError
}
func (m *errDistributor) LabelNames(context.Context, model.Time, model.Time) ([]string, error) {
	return nil, errDistributorError
}
func (m *errDistributor) MetricsForLabelMatchers(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]metric.Metric, error) {
	return nil, errDistributorError
}

func (m *errDistributor) MetricsMetadata(ctx context.Context) ([]scrape.MetricMetadata, error) {
	return nil, errDistributorError
}

type emptyChunkStore struct {
	sync.Mutex
	called bool
}

func (c *emptyChunkStore) Get(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]chunk.Chunk, error) {
	c.Lock()
	defer c.Unlock()
	c.called = true
	return nil, nil
}

func (c *emptyChunkStore) IsCalled() bool {
	c.Lock()
	defer c.Unlock()
	return c.called
}

type emptyDistributor struct{}

func (d *emptyDistributor) Query(ctx context.Context, from, to model.Time, matchers ...*labels.Matcher) (model.Matrix, error) {
	return nil, nil
}

func (d *emptyDistributor) QueryStream(ctx context.Context, from, to model.Time, matchers ...*labels.Matcher) (*client.QueryStreamResponse, error) {
	return &client.QueryStreamResponse{}, nil
}

func (d *emptyDistributor) QueryExemplars(ctx context.Context, from, to model.Time, matchers ...[]*labels.Matcher) (*client.ExemplarQueryResponse, error) {
	return nil, nil
}

func (d *emptyDistributor) LabelValuesForLabelName(context.Context, model.Time, model.Time, model.LabelName, ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

func (d *emptyDistributor) LabelNames(context.Context, model.Time, model.Time) ([]string, error) {
	return nil, nil
}

func (d *emptyDistributor) MetricsForLabelMatchers(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]metric.Metric, error) {
	return nil, nil
}

func (d *emptyDistributor) MetricsMetadata(ctx context.Context) ([]scrape.MetricMetadata, error) {
	return nil, nil
}

func TestConfig_Validate(t *testing.T) {
	tests := map[string]struct {
		setup    func(cfg *Config)
		expected error
	}{
		"should pass with default config": {
			setup: func(cfg *Config) {},
		},
		"should pass if 'query store after' is enabled and shuffle-sharding is disabled": {
			setup: func(cfg *Config) {
				cfg.QueryStoreAfter = time.Hour
			},
		},
		"should pass if 'query store after' is enabled and shuffle-sharding is enabled with greater value": {
			setup: func(cfg *Config) {
				cfg.QueryStoreAfter = time.Hour
				cfg.ShuffleShardingIngestersLookbackPeriod = 2 * time.Hour
			},
		},
		"should fail if 'query store after' is enabled and shuffle-sharding is enabled with lesser value": {
			setup: func(cfg *Config) {
				cfg.QueryStoreAfter = time.Hour
				cfg.ShuffleShardingIngestersLookbackPeriod = time.Minute
			},
			expected: errShuffleShardingLookbackLessThanQueryStoreAfter,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			cfg := &Config{}
			flagext.DefaultValues(cfg)
			testData.setup(cfg)

			assert.Equal(t, testData.expected, cfg.Validate())
		})
	}
}
