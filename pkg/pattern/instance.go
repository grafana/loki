package pattern

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/multierror"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/ingester/index"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/chunk"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/metric"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"

	loki_iter "github.com/grafana/loki/v3/pkg/iter"
	pattern_iter "github.com/grafana/loki/v3/pkg/pattern/iter"
)

const indexShards = 32

// instance is a tenant instance of the pattern ingester.
type instance struct {
	instanceID     string
	buf            []byte             // buffer used to compute fps.
	mapper         *ingester.FpMapper // using of mapper no longer needs mutex because reading from streams is lock-free
	streams        *streamsMap
	index          *index.BitPrefixInvertedIndex
	logger         log.Logger
	metrics        *ingesterMetrics
	chunkMetrics   *metric.ChunkMetrics
	aggregationCfg metric.AggregationConfig
}

func newInstance(instanceID string, logger log.Logger, metrics *ingesterMetrics, chunkMetrics *metric.ChunkMetrics, aggCfg metric.AggregationConfig) (*instance, error) {
	index, err := index.NewBitPrefixWithShards(indexShards)
	if err != nil {
		return nil, err
	}
	i := &instance{
		buf:            make([]byte, 0, 1024),
		logger:         logger,
		instanceID:     instanceID,
		streams:        newStreamsMap(),
		index:          index,
		metrics:        metrics,
		chunkMetrics:   chunkMetrics,
		aggregationCfg: aggCfg,
	}
	i.mapper = ingester.NewFPMapper(i.getLabelsFromFingerprint)
	return i, nil
}

// Push pushes the log entries in the given PushRequest to the appropriate streams.
// It returns an error if any error occurs during the push operation.
func (i *instance) Push(ctx context.Context, req *logproto.PushRequest) error {
	appendErr := multierror.New()

	for _, reqStream := range req.Streams {
		s, _, err := i.streams.LoadOrStoreNew(reqStream.Labels,
			func() (*stream, error) {
				// add stream
				return i.createStream(ctx, reqStream)
			}, nil)
		if err != nil {
			appendErr.Add(err)
			continue
		}
		err = s.Push(ctx, reqStream.Entries)
		if err != nil {
			appendErr.Add(err)
			continue
		}
	}
	return appendErr.Err()
}

// Iterator returns an iterator of pattern samples matching the given query patterns request.
func (i *instance) Iterator(ctx context.Context, req *logproto.QueryPatternsRequest) (pattern_iter.Iterator, error) {
	matchers, err := syntax.ParseMatchers(req.Query, true)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
	from, through := util.RoundToMilliseconds(req.Start, req.End)
	step := model.Time(req.Step)
	if step < chunk.TimeResolution {
		step = chunk.TimeResolution
	}

	var iters []pattern_iter.Iterator
	err = i.forMatchingStreams(matchers, func(s *stream) error {
		iter, err := s.Iterator(ctx, from, through, step)
		if err != nil {
			return err
		}
		iters = append(iters, iter)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pattern_iter.NewMerge(iters...), nil
}

func (i *instance) QuerySample(
	ctx context.Context,
	expr syntax.SampleExpr,
	req *logproto.QuerySamplesRequest,
) (loki_iter.SampleIterator, error) {
	if !i.aggregationCfg.Enabled {
		// Should never get here, but this will prevent nil pointer panics in test
		return loki_iter.NoopIterator, nil
	}

	from, through := util.RoundToMilliseconds(req.Start, req.End)
	step := model.Time(req.Step)
	if step < chunk.TimeResolution {
		step = chunk.TimeResolution
	}

	selector, err := expr.Selector()
	if err != nil {
		return nil, err
	}

	var iters []loki_iter.SampleIterator
	err = i.forMatchingStreams(
		selector.Matchers(),
		func(stream *stream) error {
			var iter loki_iter.SampleIterator
			var err error
			iter, err = stream.SampleIterator(ctx, expr, from, through, step)
			if err != nil {
				return err
			}

			iters = append(iters, iter)
			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	spanLogger := spanlogger.FromContext(ctx)
	if spanLogger != nil {
		level.Debug(spanLogger).Log(
			"msg", "summing results of querying streams",
			"num_iters", len(iters),
			"iters", fmt.Sprintf("%v", iters),
		)
	} else {
		level.Debug(i.logger).Log(
			"msg", "summing results of querying streams",
			"num_iters", len(iters),
			"iters", fmt.Sprintf("%v", iters),
		)
	}

	return pattern_iter.NewSumMergeSampleIterator(iters), nil
}

// forMatchingStreams will execute a function for each stream that matches the given matchers.
func (i *instance) forMatchingStreams(
	matchers []*labels.Matcher,
	fn func(*stream) error,
) error {
	filters, matchers := util.SplitFiltersAndMatchers(matchers)
	ids, err := i.index.Lookup(matchers, nil)
	level.Debug(i.logger).Log("msg", "forMatchingStreams", "ids", fmt.Sprintf("%v", ids))
	if err != nil {
		return err
	}

outer:
	for _, streamID := range ids {
		stream, ok := i.streams.LoadByFP(streamID)
		if !ok {
			// If a stream is missing here, it has already been flushed
			// and is supposed to be picked up from storage by querier
			continue
		}
		for _, filter := range filters {
			if !filter.Matches(stream.labels.Get(filter.Name)) {
				continue outer
			}
		}
		err := fn(stream)
		if err != nil {
			return err
		}
	}
	return nil
}

func (i *instance) createStream(_ context.Context, pushReqStream logproto.Stream) (*stream, error) {
	labels, err := syntax.ParseLabels(pushReqStream.Labels)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
	fp := i.getHashForLabels(labels)
	sortedLabels := i.index.Add(logproto.FromLabelsToLabelAdapters(labels), fp)
	firstEntryLine := pushReqStream.Entries[0].Line
	format := drain.FormatUnknown
	if firstEntryLine[0] == '{' && firstEntryLine[len(firstEntryLine)-1] == '}' {
		format = drain.FormatJson
	} else if strings.Count(firstEntryLine, "=") > len(firstEntryLine)/20 {
		format = drain.FormatLogfmt
	}
	s, err := newStream(fp, sortedLabels, i.metrics, i.chunkMetrics, i.aggregationCfg, i.logger, format)

	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}
	return s, nil
}

func (i *instance) getHashForLabels(ls labels.Labels) model.Fingerprint {
	var fp uint64
	fp, i.buf = ls.HashWithoutLabels(i.buf, []string(nil)...)
	return i.mapper.MapFP(model.Fingerprint(fp), ls)
}

// Return labels associated with given fingerprint. Used by fingerprint mapper.
func (i *instance) getLabelsFromFingerprint(fp model.Fingerprint) labels.Labels {
	s, ok := i.streams.LoadByFP(fp)
	if !ok {
		return nil
	}
	return s.labels
}

// removeStream removes a stream from the instance.
func (i *instance) removeStream(s *stream) {
	if i.streams.Delete(s) {
		i.index.Delete(s.labels, s.fp)
	}
}
