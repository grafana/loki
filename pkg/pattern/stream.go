package pattern

import (
	"context"
	"slices"
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
	volumeThreshold        float64
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
	volumeThreshold float64,
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
		persistenceGranularity = drainCfg.MaxChunkAge
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
		volumeThreshold:        volumeThreshold,
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

// Collect all clusters with their metadata for filtering
type clusterWithMeta struct {
	cluster       *drain.LogCluster
	level         string
	drainInstance *drain.Drain
	prunedSamples []*logproto.PatternSample
}

func (s *stream) prune(olderThan time.Duration) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	var allClusters []clusterWithMeta

	// First pass: collect all clusters and prune samples
	totalClusters := 0
	for lvl, pattern := range s.patterns {
		clusters := pattern.Clusters()
		for _, cluster := range clusters {
			prunedSamples := cluster.Prune(olderThan)
			if len(prunedSamples) > 0 {
				allClusters = append(allClusters, clusterWithMeta{
					cluster:       cluster,
					level:         lvl,
					drainInstance: pattern,
					prunedSamples: prunedSamples,
				})
			}
			if cluster.Size == 0 {
				pattern.Delete(cluster)
			}
			// Clear empty branches and track total clusters
			pattern.Prune()
			totalClusters += len(pattern.Clusters())
		}
	}

	// Filter clusters by volume if volumeThreshold is set (< 1.0)
	var clustersToWrite []clusterWithMeta
	if s.volumeThreshold > 0 && s.volumeThreshold < 1.0 && len(allClusters) > 0 {
		// Sort clusters by volume, and keep only the top threshold of clusters by volume
		// To optimize memory, filterClustersByVolume will mutate the input slice, the slice we get
		// in rerturn uses the same underlying array as the input slice.
		clustersToWrite = filterClustersByVolume(allClusters, s.volumeThreshold)
	} else {
		// No filtering, write all clusters
		clustersToWrite = allClusters
	}

	// Write patterns for filtered clusters
	for _, cm := range clustersToWrite {
		s.writePatternsBucketed(cm.prunedSamples, s.labels, cm.cluster.String(), cm.level)
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

	// Count total clusters across all levels
	totalClusters := 0
	for _, pattern := range s.patterns {
		totalClusters += len(pattern.Clusters())
	}

	s.aggregationMetrics.PatternsActive.WithLabelValues(s.instanceID).Set(float64(totalClusters))
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
			s.aggregationMetrics.PatternWritesTotal.WithLabelValues(s.instanceID).Inc()

			// Record pattern entry size
			entrySize := len(patternEntry)
			s.aggregationMetrics.PatternBytesWrittenTotal.WithLabelValues(s.instanceID).Add(float64(entrySize))
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

// filterClustersByVolume sorts clusters in-place by volume and returns the number of clusters
// to keep to represent the top X% of total volume. This mutates the input slice, and returns
// a filtered slice that utilizes the same underlying array as the input slice.
func filterClustersByVolume(clusters []clusterWithMeta, threshold float64) []clusterWithMeta {
	if len(clusters) == 0 {
		return []clusterWithMeta{}
	}

	// Handle threshold of 0 - keep no clusters
	if threshold == 0 {
		return []clusterWithMeta{}
	}

	var totalVolume int64
	for _, cluster := range clusters {
		totalVolume += cluster.cluster.Volume
	}

	// Sort clusters by volume in descending order (in-place)
	slices.SortFunc(clusters, func(i, j clusterWithMeta) int {
		if i.cluster.Volume > j.cluster.Volume {
			return -1 // Higher volume first
		}
		if i.cluster.Volume < j.cluster.Volume {
			return 1 // Lower volume last
		}
		return 0
	})

	if totalVolume == 0 {
		return []clusterWithMeta{}
	}

	// Find how many clusters to keep for the threshold
	targetVolume := int64(float64(totalVolume) * threshold)
	var cumulativeVolume int64

	var i int
	for ; i < len(clusters); i++ {
		cumulativeVolume += clusters[i].cluster.Volume
		if cumulativeVolume >= targetVolume {
			i++ // Include this cluster that pushed us over the threshold
			break
		}
	}

	return clusters[0:i]
}
