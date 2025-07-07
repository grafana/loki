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
	fp            model.Fingerprint
	labels        labels.Labels
	labelsString  string
	labelHash     uint64
	patterns      map[string]*drain.Drain
	mtx           sync.Mutex
	logger        log.Logger
	patternWriter aggregation.EntryWriter

	lastTs int64
}

func newStream(
	fp model.Fingerprint,
	labels labels.Labels,
	metrics *ingesterMetrics,
	logger log.Logger,
	guessedFormat string,
	instanceID string,
	drainCfg *drain.Config,
	drainLimits drain.Limits,
	patternWriter aggregation.EntryWriter,
) (*stream, error) {
	linesSkipped, err := metrics.linesSkipped.CurryWith(prometheus.Labels{"tenant": instanceID})
	if err != nil {
		return nil, err
	}

	patterns := make(map[string]*drain.Drain, len(constants.LogLevels))
	for _, lvl := range constants.LogLevels {
		patterns[lvl] = drain.New(instanceID, drainCfg, drainLimits, guessedFormat, &drain.Metrics{
			PatternsEvictedTotal:  metrics.patternsDiscardedTotal.WithLabelValues(instanceID, guessedFormat, "false"),
			PatternsPrunedTotal:   metrics.patternsDiscardedTotal.WithLabelValues(instanceID, guessedFormat, "true"),
			PatternsDetectedTotal: metrics.patternsDetectedTotal.WithLabelValues(instanceID, guessedFormat),
			LinesSkipped:          linesSkipped,
			TokensPerLine:         metrics.tokensPerLine.WithLabelValues(instanceID, guessedFormat),
			StatePerLine:          metrics.statePerLine.WithLabelValues(instanceID, guessedFormat),
		})
	}

	return &stream{
		fp:            fp,
		labels:        labels,
		labelsString:  labels.String(),
		labelHash:     labels.Hash(),
		logger:        logger,
		patterns:      patterns,
		patternWriter: patternWriter,
	}, nil
}

func (s *stream) Push(
	_ context.Context,
	entries []logproto.Entry,
) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, entry := range entries {
		if entry.Timestamp.UnixNano() < s.lastTs {
			continue
		}

		metadata := logproto.FromLabelAdaptersToLabels(entry.StructuredMetadata)
		lvl := constants.LogLevelUnknown
		if metadata.Has(constants.LevelLabel) {
			lvl = strings.ToLower(metadata.Get(constants.LevelLabel))
		}
		s.lastTs = entry.Timestamp.UnixNano()

		//TODO(twhitney): Can we reduce lock contention by locking by level rather than for the entire stream?
		if pattern, ok := s.patterns[lvl]; ok {
			pattern.Train(lvl, entry.Line, entry.Timestamp.UnixNano(), s.labels)
		} else {
			// since we're defaulting the level to unknown above, we should never get here.
			s.patterns[constants.LogLevelUnknown].Train(constants.LogLevelUnknown, entry.Line, entry.Timestamp.UnixNano(), s.labels)
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
			iters = append(iters, cluster.Iterator(lvl, from, through, step))
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
			// Write patterns for pruned chunks
			if len(prunedSamples) > 0 {
				var totalValue int64
				var latestTimestamp model.Time
				for _, sample := range prunedSamples {
					totalValue += sample.Value
					if sample.Timestamp > latestTimestamp {
						latestTimestamp = sample.Timestamp
					}
				}
				s.writePattern(latestTimestamp, s.labels, cluster.String(), totalValue, lvl)
			}
			if cluster.Size == 0 {
				pattern.Delete(cluster)
			}
		}
		// Clear empty branches after deleting chunks & clusters
		pattern.Prune()
		totalClusters += len(pattern.Clusters())
	}

	return totalClusters == 0
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

	newLbls := labels.Labels{
		labels.Label{Name: constants.PatternLabel, Value: service},
	}

	newStructuredMetadata := []logproto.LabelAdapter{
		{Name: constants.LevelLabel, Value: lvl},
	}

	if s.patternWriter != nil {
		s.patternWriter.WriteEntry(
			ts.Time(),
			aggregation.PatternEntry(ts.Time(), count, pattern, streamLbls),
			newLbls,
			newStructuredMetadata,
		)
	}
}
