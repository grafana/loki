package pattern

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/aggregation"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type stream struct {
	fp                 model.Fingerprint
	labels             labels.Labels
	labelsString       string
	labelHash          uint64
	patterns           map[string]*drain.Drain
	mtx                sync.Mutex
	logger             log.Logger
	patternWriter      aggregation.EntryWriter
	aggregationMetrics *aggregation.Metrics
	instanceID         string

	lastTS                 int64
	persistenceGranularity time.Duration
	sampleInterval         time.Duration
	patternRateThreshold   float64
}

func newStream(
	fp model.Fingerprint,
	ls labels.Labels,
	metrics *ingesterMetrics,
	logger log.Logger,
	guessedFormat string,
	instanceID string,
	drainCfg *drain.Config,
	limits Limits,
	patternWriter aggregation.EntryWriter,
	aggregationMetrics *aggregation.Metrics,
) (*stream, error) {
	linesSkipped, err := metrics.linesSkipped.CurryWith(prometheus.Labels{"tenant": instanceID})
	if err != nil {
		return nil, err
	}

	patterns := make(map[string]*drain.Drain, len(constants.LogLevels))
	for _, lvl := range constants.LogLevels {
		patterns[lvl] = drain.New(instanceID, drainCfg, limits, guessedFormat, &drain.Metrics{
			PatternsEvictedTotal:  metrics.patternsDiscardedTotal.WithLabelValues(instanceID, guessedFormat, "false"),
			PatternsPrunedTotal:   metrics.patternsDiscardedTotal.WithLabelValues(instanceID, guessedFormat, "true"),
			PatternsDetectedTotal: metrics.patternsDetectedTotal.WithLabelValues(instanceID, guessedFormat),
			LinesSkipped:          linesSkipped,
			TokensPerLine:         metrics.tokensPerLine.WithLabelValues(instanceID, guessedFormat),
			StatePerLine:          metrics.statePerLine.WithLabelValues(instanceID, guessedFormat),
		})
	}

	// Get per-tenant persistence granularity (requires casting drainLimits to Limits interface)
	persistenceGranularity := limits.PersistenceGranularity(instanceID)
	if persistenceGranularity == 0 {
		persistenceGranularity = drainCfg.ChunkDuration
	}

	return &stream{
		fp:                     fp,
		labels:                 ls,
		labelsString:           ls.String(),
		labelHash:              labels.StableHash(ls),
		logger:                 logger,
		patterns:               patterns,
		patternWriter:          patternWriter,
		aggregationMetrics:     aggregationMetrics,
		instanceID:             instanceID,
		persistenceGranularity: persistenceGranularity,
		sampleInterval:         drainCfg.SampleInterval,
		patternRateThreshold:   limits.PatternRateThreshold(instanceID),
	}, nil
}

func (s *stream) Push(
	_ context.Context,
	entries []logproto.Entry,
) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, entry := range entries {
		if entry.Timestamp.UnixNano() < s.lastTS {
			continue
		}

		metadata := logproto.FromLabelAdaptersToLabels(entry.StructuredMetadata)
		lvl := constants.LogLevelUnknown
		if metadata.Has(constants.LevelLabel) {
			lvl = strings.ToLower(metadata.Get(constants.LevelLabel))
		}
		s.lastTS = entry.Timestamp.UnixNano()

		//TODO(twhitney): Can we reduce lock contention by locking by level rather than for the entire stream?
		if pattern, ok := s.patterns[lvl]; ok {
			pattern.Train(entry.Line, entry.Timestamp.UnixNano())
		} else {
			// since we're defaulting the level to unknown above, we should never get here.
			s.patterns[constants.LogLevelUnknown].Train(entry.Line, entry.Timestamp.UnixNano())
		}
	}
	return nil
}

// TODO(twhitney): Allow a level to be specified for the iterator. Requires a change to the query API.
func (s *stream) Iterator(_ context.Context, from, through, step model.Time) (iter.Iterator, error) {
	// todo we should improve locking.
	s.mtx.Lock()
	defer s.mtx.Unlock()

	iters := []iter.Iterator{}
	for lvl, pattern := range s.patterns {
		clusters := pattern.Clusters()
		for _, cluster := range clusters {
			if cluster.String() == "" {
				continue
			}
			iters = append(iters, cluster.Iterator(lvl, from, through, step, model.Time(s.sampleInterval.Milliseconds())))
		}
	}

	return iter.NewMerge(iters...), nil
}

func (s *stream) prune(olderThan time.Duration) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	totalClusters := 0
	for lvl, pattern := range s.patterns {
		clusters := pattern.Clusters()
		for _, cluster := range clusters {
			prunedSamples := cluster.Prune(olderThan)
			// Write patterns for pruned chunks with bucketed aggregation
			if len(prunedSamples) > 0 {
				s.writePatternsBucketed(prunedSamples, s.labels, cluster.String(), lvl)
			}
			if cluster.Size == 0 {
				pattern.Delete(cluster)
			}
		}
		// Clear empty branches after deleting chunks & clusters
		pattern.Prune()
		totalClusters += len(pattern.Clusters())
	}

	// Update active patterns gauge
	s.updatePatternsActiveGauge()

	return totalClusters == 0
}

// updatePatternsActiveGauge updates the active patterns gauge with the current cluster count
func (s *stream) updatePatternsActiveGauge() {
	if s.aggregationMetrics == nil {
		return
	}

	service := s.labels.Get(push.LabelServiceName)
	if service == "" {
		service = push.ServiceUnknown
	}

	// Count total clusters across all levels
	totalClusters := 0
	for _, pattern := range s.patterns {
		totalClusters += len(pattern.Clusters())
	}

	s.aggregationMetrics.PatternsActive.WithLabelValues(s.instanceID, service).Set(float64(totalClusters))
}

func (s *stream) flush() {
	// Flush all patterns by pruning everything older than 0 (i.e., everything)
	s.prune(0)
}

func (s *stream) writePattern(
	ts model.Time,
	streamLbls labels.Labels,
	pattern string,
	count int64,
	lvl string,
) {
	service := streamLbls.Get(push.LabelServiceName)
	if service == "" {
		service = push.ServiceUnknown
	}

	newLbls := labels.FromStrings(constants.PatternLabel, service)

	newStructuredMetadata := []logproto.LabelAdapter{
		{Name: constants.LevelLabel, Value: lvl},
	}

	if s.patternWriter != nil {
		patternEntry := aggregation.PatternEntry(ts.Time(), count, pattern, streamLbls)

		// Record metrics
		if s.aggregationMetrics != nil {
			// Increment pattern writes counter
			s.aggregationMetrics.PatternWritesTotal.WithLabelValues(s.instanceID, service).Inc()

			// Record pattern entry size
			entrySize := len(patternEntry)
			s.aggregationMetrics.PatternBytesWrittenTotal.WithLabelValues(s.instanceID, service).Add(float64(entrySize))
			s.aggregationMetrics.PatternPayloadBytes.WithLabelValues(s.instanceID, service).Observe(float64(entrySize))
		}

		s.patternWriter.WriteEntry(
			ts.Time(),
			patternEntry,
			newLbls,
			newStructuredMetadata,
		)
	}
}

func (s *stream) writePatternsBucketed(
	prunedSamples []*logproto.PatternSample,
	streamLbls labels.Labels,
	pattern string,
	lvl string,
) {
	if len(prunedSamples) == 0 {
		return
	}

	// Calculate bucket size
	bucketSize := s.persistenceGranularity

	// Process samples into buckets
	buckets := make(map[model.Time][]*logproto.PatternSample)

	for _, sample := range prunedSamples {
		// Calculate which bucket this sample belongs to
		sampleBucket := model.Time(sample.Timestamp.UnixNano() / bucketSize.Nanoseconds() * bucketSize.Nanoseconds() / 1e6)
		buckets[sampleBucket] = append(buckets[sampleBucket], sample)
	}

	// Write pattern entries for each bucket (apply rate threshold per bucket)
	for bucketTime, bucketSamples := range buckets {
		if len(bucketSamples) == 0 {
			continue
		}

		// Check if pattern rate meets threshold,
		// threshold of 0 means no rate threshold
		if s.patternRateThreshold > 0 {
			rate := s.calculatePatternRate(bucketSamples)
			if rate < s.patternRateThreshold {
				continue
			}
		}

		// Calculate total value for this bucket
		var totalValue int64
		for _, sample := range bucketSamples {
			totalValue += sample.Value
		}

		if totalValue > 0 {
			s.writePattern(bucketTime, streamLbls, pattern, totalValue, lvl)
		}
	}
}

// calculatePatternRate calculates a per second rate of samples in a bucket.
func (s *stream) calculatePatternRate(samples []*logproto.PatternSample) float64 {
	if len(samples) == 0 {
		return 0.0
	}

	if len(samples) == 1 {
		return 0.0
	}

	// Calculate total count and time span
	var totalCount int64
	var minTime, maxTime model.Time

	for i, sample := range samples {
		totalCount += sample.Value
		if i == 0 {
			minTime = sample.Timestamp
			maxTime = sample.Timestamp
		} else {
			if sample.Timestamp < minTime {
				minTime = sample.Timestamp
			}
			if sample.Timestamp > maxTime {
				maxTime = sample.Timestamp
			}
		}
	}

	// Calculate time span in seconds
	timeSpanSeconds := float64(maxTime.Sub(minTime)) / float64(time.Second)

	if timeSpanSeconds == 0 {
		return 0.0
	}

	// Return samples per second
	return float64(totalCount) / timeSpanSeconds
}
