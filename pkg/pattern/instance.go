package pattern

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/ring"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/ingester/index"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/aggregation"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
)

const indexShards = 32

// instance is a tenant instance of the pattern ingester.
type instance struct {
	instanceID  string
	buf         []byte             // buffer used to compute fps.
	mapper      *ingester.FpMapper // using of mapper no longer needs mutex because reading from streams is lock-free
	streams     *streamsMap
	index       *index.BitPrefixInvertedIndex
	logger      log.Logger
	metrics     *ingesterMetrics
	drainCfg    *drain.Config
	drainLimits drain.Limits
	ringClient  RingClient
	ingesterID  string

	aggMetricsLock             sync.Mutex
	aggMetricsByStreamAndLevel map[string]map[string]*aggregatedMetrics

	writer aggregation.EntryWriter
}

type aggregatedMetrics struct {
	bytes uint64
	count uint64
}

func newInstance(
	instanceID string,
	logger log.Logger,
	metrics *ingesterMetrics,
	drainCfg *drain.Config,
	drainLimits drain.Limits,
	ringClient RingClient,
	ingesterID string,
	writer aggregation.EntryWriter,
) (*instance, error) {
	index, err := index.NewBitPrefixWithShards(indexShards)
	if err != nil {
		return nil, err
	}
	i := &instance{
		buf:                        make([]byte, 0, 1024),
		logger:                     logger,
		instanceID:                 instanceID,
		streams:                    newStreamsMap(),
		index:                      index,
		metrics:                    metrics,
		drainCfg:                   drainCfg,
		drainLimits:                drainLimits,
		ringClient:                 ringClient,
		ingesterID:                 ingesterID,
		aggMetricsByStreamAndLevel: make(map[string]map[string]*aggregatedMetrics),
		writer:                     writer,
	}
	i.mapper = ingester.NewFPMapper(i.getLabelsFromFingerprint)
	return i, nil
}

// Push pushes the log entries in the given PushRequest to the appropriate streams.
// It returns an error if any error occurs during the push operation.
func (i *instance) Push(ctx context.Context, req *logproto.PushRequest) error {
	appendErr := multierror.New()

	for _, reqStream := range req.Streams {
		// All streams are observed for metrics
		// TODO(twhitney): this would be better as a queue that drops in response to backpressure
		i.Observe(ctx, reqStream.Labels, reqStream.Entries)

		// But only owned streamed are processed for patterns
		ownedStream, err := i.isOwnedStream(i.ingesterID, reqStream.Labels)
		if err != nil {
			appendErr.Add(err)
		}

		if ownedStream {
			if len(reqStream.Entries) == 0 {
				continue
			}
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
	}

	return appendErr.Err()
}

func (i *instance) isOwnedStream(ingesterID string, stream string) (bool, error) {
	var descs [1]ring.InstanceDesc
	replicationSet, err := i.ringClient.Ring().Get(
		lokiring.TokenFor(i.instanceID, stream),
		ring.WriteNoExtend,
		descs[:0],
		nil,
		nil,
	)
	if err != nil {
		return false, fmt.Errorf(
			"error getting replication set for stream %s: %v",
			stream,
			err,
		)
	}

	if replicationSet.Instances == nil {
		return false, errors.New("no instances found")
	}

	for _, instanceDesc := range replicationSet.Instances {
		if instanceDesc.Id == ingesterID {
			return true, nil
		}
	}
	return false, nil
}

// Iterator returns an iterator of pattern samples matching the given query patterns request.
func (i *instance) Iterator(ctx context.Context, req *logproto.QueryPatternsRequest) (iter.Iterator, error) {
	matchers, err := syntax.ParseMatchers(req.Query, true)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}
	from, through := util.RoundToMilliseconds(req.Start, req.End)
	step := model.Time(req.Step)
	if step < drain.TimeResolution {
		step = drain.TimeResolution
	}

	var iters []iter.Iterator
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
	return iter.NewMerge(iters...), nil
}

// forMatchingStreams will execute a function for each stream that matches the given matchers.
func (i *instance) forMatchingStreams(
	matchers []*labels.Matcher,
	fn func(*stream) error,
) error {
	filters, matchers := util.SplitFiltersAndMatchers(matchers)
	ids, err := i.index.Lookup(matchers, nil)
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
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}
	fp := i.getHashForLabels(labels)
	sortedLabels := i.index.Add(logproto.FromLabelsToLabelAdapters(labels), fp)
	firstEntryLine := pushReqStream.Entries[0].Line
	s, err := newStream(fp, sortedLabels, i.metrics, i.logger, drain.DetectLogFormat(firstEntryLine), i.instanceID, i.drainCfg, i.drainLimits)
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

func (i *instance) Observe(ctx context.Context, stream string, entries []logproto.Entry) {
	i.aggMetricsLock.Lock()
	defer i.aggMetricsLock.Unlock()

	sp, _ := opentracing.StartSpanFromContext(
		ctx,
		"patternIngester.Observe",
	)
	defer sp.Finish()

	sp.LogKV(
		"event", "observe stream for metrics",
		"stream", stream,
		"entries", len(entries),
	)

	for _, entry := range entries {
		lvl := constants.LogLevelUnknown
		structuredMetadata := logproto.FromLabelAdaptersToLabels(entry.StructuredMetadata)
		if structuredMetadata.Has(constants.LevelLabel) {
			lvl = strings.ToLower(structuredMetadata.Get(constants.LevelLabel))
		}

		streamMetrics, ok := i.aggMetricsByStreamAndLevel[stream]

		if !ok {
			streamMetrics = map[string]*aggregatedMetrics{}
		}

		if _, ok := streamMetrics[lvl]; !ok {
			streamMetrics[lvl] = &aggregatedMetrics{}
		}

		streamMetrics[lvl].bytes += uint64(len(entry.Line))
		streamMetrics[lvl].count++

		i.aggMetricsByStreamAndLevel[stream] = streamMetrics
	}
}

func (i *instance) Downsample(now model.Time) {
	i.aggMetricsLock.Lock()
	defer func() {
		i.aggMetricsByStreamAndLevel = make(map[string]map[string]*aggregatedMetrics)
		i.aggMetricsLock.Unlock()
	}()

	for stream, metricsByLevel := range i.aggMetricsByStreamAndLevel {
		lbls, err := syntax.ParseLabels(stream)
		if err != nil {
			continue
		}

		for level, metrics := range metricsByLevel {
			// we start with an empty bucket for each level, so only write if we have metrics
			if metrics.count > 0 {
				i.writeAggregatedMetrics(now, lbls, level, metrics.bytes, metrics.count)
			}
		}
	}
}

func (i *instance) writeAggregatedMetrics(
	now model.Time,
	streamLbls labels.Labels,
	level string,
	totalBytes, totalCount uint64,
) {
	service := streamLbls.Get(push.LabelServiceName)
	if service == "" {
		service = push.ServiceUnknown
	}

	newLbls := labels.Labels{
		labels.Label{Name: push.AggregatedMetricLabel, Value: service},
		labels.Label{Name: "level", Value: level},
	}

	if i.writer != nil {
		i.writer.WriteEntry(
			now.Time(),
			aggregation.AggregatedMetricEntry(now, totalBytes, totalCount, service, streamLbls),
			newLbls,
		)

		i.metrics.samples.WithLabelValues(service).Inc()
	}
}
