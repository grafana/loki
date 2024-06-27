package pattern

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/pattern/chunk"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/metric"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"

	loki_iter "github.com/grafana/loki/v3/pkg/iter"
	pattern_iter "github.com/grafana/loki/v3/pkg/pattern/iter"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
)

type stream struct {
	fp           model.Fingerprint
	labels       labels.Labels
	labelsString string
	labelHash    uint64
	patterns     *drain.Drain
	mtx          sync.Mutex

	cfg    metric.AggregationConfig
	chunks *metric.Chunks

	evaluator metric.SampleEvaluatorFactory

	lastTs int64

	logger log.Logger
}

func newStream(
	fp model.Fingerprint,
	labels labels.Labels,
	metrics *ingesterMetrics,
	chunkMetrics *metric.ChunkMetrics,
	cfg metric.AggregationConfig,
	logger log.Logger,
) (*stream, error) {
	stream := &stream{
		fp:           fp,
		labels:       labels,
		labelsString: labels.String(),
		labelHash:    labels.Hash(),
		patterns: drain.New(drain.DefaultConfig(), &drain.Metrics{
			PatternsEvictedTotal:  metrics.patternsDiscardedTotal,
			PatternsDetectedTotal: metrics.patternsDetectedTotal,
			TokensPerLine:         metrics.tokensPerLine,
			MetadataPerLine:       metrics.metadataPerLine,
		}),
		cfg:    cfg,
		logger: logger,
	}

	chunks := metric.NewChunks(labels, chunkMetrics, logger)
	stream.chunks = chunks
	stream.evaluator = metric.NewDefaultEvaluatorFactory(chunks)

	return stream, nil
}

func (s *stream) Push(
	_ context.Context,
	entries []logproto.Entry,
) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	bytes := float64(0)
	count := float64(len(entries))
	for _, entry := range entries {
		if entry.Timestamp.UnixNano() < s.lastTs {
			continue
		}

		bytes += float64(len(entry.Line))

		s.lastTs = entry.Timestamp.UnixNano()
		s.patterns.Train(entry.Line, entry.Timestamp.UnixNano())
	}

	if s.cfg.Enabled && s.chunks != nil {
		if s.cfg.LogPushObservations {
			level.Debug(s.logger).
				Log("msg", "observing pushed log entries",
					"stream", s.labelsString,
					"bytes", bytes,
					"count", count,
					"sample_ts_ns", s.lastTs,
				)
		}
		s.chunks.Observe(bytes, count, model.TimeFromUnixNano(s.lastTs))
	}
	return nil
}

func (s *stream) Iterator(_ context.Context, from, through, step model.Time) (pattern_iter.Iterator, error) {
	// todo we should improve locking.
	s.mtx.Lock()
	defer s.mtx.Unlock()

	clusters := s.patterns.Clusters()
	iters := make([]pattern_iter.Iterator, 0, len(clusters))

	for _, cluster := range clusters {
		if cluster.String() == "" {
			continue
		}
		iters = append(iters, cluster.Iterator(from, through, step))
	}
	return pattern_iter.NewMerge(iters...), nil
}

func (s *stream) SampleIterator(
	ctx context.Context,
	expr syntax.SampleExpr,
	from, through, step model.Time,
) (loki_iter.SampleIterator, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	from = chunk.TruncateTimestamp(from, step)
	through = chunk.TruncateTimestamp(through, step)

	stepEvaluator, err := s.evaluator.NewStepEvaluator(
		ctx,
		s.evaluator,
		expr,
		from,
		through,
		step,
	)
	if err != nil {
		return nil, err
	}

	next, ts, r := stepEvaluator.Next()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	// TODO(twhitney): actually get max series from limits
	// this is only 1 series since we're already on a stream
	// this this limit needs to also be enforced higher up
	maxSeries := 1000
	matrix, err := s.joinSampleVectors(
		next,
		ts,
		r,
		stepEvaluator,
		maxSeries,
		from, through, step)
	if err != nil {
		return nil, err
	}

	spanLogger := spanlogger.FromContext(ctx)
	if spanLogger != nil {
		level.Debug(spanLogger).Log(
			"msg", "sample iterator for stream",
			"stream", s.labelsString,
			"num_results", len(matrix),
			"matrix", fmt.Sprintf("%v", matrix),
		)
	} else {
		level.Debug(s.logger).Log(
			"msg", "sample iterator for stream",
			"stream", s.labelsString,
			"num_results", len(matrix),
			"matrix", fmt.Sprintf("%v", matrix),
		)
	}

	return loki_iter.NewMultiSeriesIterator(matrix), nil
}

func (s *stream) joinSampleVectors(
	next bool,
	ts int64,
	r logql.StepResult,
	stepEvaluator logql.StepEvaluator,
	maxSeries int,
	from, through, step model.Time,
) ([]logproto.Series, error) {
	stepCount := int(math.Ceil(float64(through.Sub(from).Nanoseconds()) / float64(step.UnixNano())))
	if stepCount <= 0 {
		stepCount = 1
	}

	vec := promql.Vector{}
	if next {
		vec = r.SampleVector()
	}

	// fail fast for the first step or instant query
	if len(vec) > maxSeries {
		return nil, logqlmodel.NewSeriesLimitError(maxSeries)
	}

	series := map[uint64]logproto.Series{}

	// step evaluator logic is slightly different than the normal contract in Loki
	// when evaluating a selection range, it's counts datapoints within (start, end]
	// so an additional condition of ts < through is needed to make sure this loop
	// doesn't go beyond the through time. the contract for Loki queries is [start, end),
	// and the samples to evaluate are selected based on [start, end) so the last result
	// is likely incorrect anyway
	for next && ts < int64(through) {
		vec = r.SampleVector()
		for _, p := range vec {
			hash := p.Metric.Hash()
			s, ok := series[hash]
			if !ok {
				s = logproto.Series{
					Labels:     p.Metric.String(),
					Samples:    make([]logproto.Sample, 0, stepCount),
					StreamHash: hash,
				}
				series[hash] = s
			}

			s.Samples = append(s.Samples, logproto.Sample{
				Timestamp: ts * 1e6, // convert milliseconds to nanoseconds
				Value:     p.F,
			})
			series[hash] = s
		}

		next, ts, r = stepEvaluator.Next()
		if stepEvaluator.Error() != nil {
			return nil, stepEvaluator.Error()
		}
	}

	matrix := make([]logproto.Series, 0, len(series))
	for _, s := range series {
		s := s
		matrix = append(matrix, s)
	}

	level.Debug(s.logger).Log(
		"msg", "joined sample vectors",
		"num_series", len(matrix),
		"matrix", fmt.Sprintf("%v", matrix),
		"from", from,
		"through", through,
		"step", step,
	)

	return matrix, stepEvaluator.Error()
}

func (s *stream) prune(olderThan time.Duration) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	clusters := s.patterns.Clusters()
	for _, cluster := range clusters {
		cluster.Prune(olderThan)
		if cluster.Size == 0 {
			s.patterns.Delete(cluster)
		}
	}

	chunksPruned := true
	if s.chunks != nil {
		chunksPruned = s.chunks.Prune(olderThan)
	}

	return len(s.patterns.Clusters()) == 0 && chunksPruned
}
