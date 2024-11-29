package pattern

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/iter"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type stream struct {
	fp           model.Fingerprint
	labels       labels.Labels
	labelsString string
	labelHash    uint64
	patterns     *drain.Drain
	mtx          sync.Mutex
	logger       log.Logger

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
) (*stream, error) {
	linesSkipped, err := metrics.linesSkipped.CurryWith(prometheus.Labels{"tenant": instanceID})
	if err != nil {
		return nil, err
	}
	return &stream{
		fp:           fp,
		labels:       labels,
		labelsString: labels.String(),
		labelHash:    labels.Hash(),
		logger:       logger,
		patterns: drain.New(instanceID, drainCfg, drainLimits, guessedFormat, &drain.Metrics{
			PatternsEvictedTotal:  metrics.patternsDiscardedTotal.WithLabelValues(instanceID, guessedFormat, "false"),
			PatternsPrunedTotal:   metrics.patternsDiscardedTotal.WithLabelValues(instanceID, guessedFormat, "true"),
			PatternsDetectedTotal: metrics.patternsDetectedTotal.WithLabelValues(instanceID, guessedFormat),
			LinesSkipped:          linesSkipped,
			TokensPerLine:         metrics.tokensPerLine.WithLabelValues(instanceID, guessedFormat),
			StatePerLine:          metrics.statePerLine.WithLabelValues(instanceID, guessedFormat),
		}),
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
		s.lastTs = entry.Timestamp.UnixNano()
		s.patterns.Train(entry.Line, entry.Timestamp.UnixNano())
	}
	return nil
}

func (s *stream) Iterator(_ context.Context, from, through, step model.Time) (iter.Iterator, error) {
	// todo we should improve locking.
	s.mtx.Lock()
	defer s.mtx.Unlock()

	clusters := s.patterns.Clusters()
	iters := make([]iter.Iterator, 0, len(clusters))

	for _, cluster := range clusters {
		if cluster.String() == "" {
			continue
		}
		iters = append(iters, cluster.Iterator(from, through, step))
	}
	return iter.NewMerge(iters...), nil
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
	// Clear empty branches after deleting chunks & clusters
	s.patterns.Prune()

	return len(s.patterns.Clusters()) == 0
}
