package pattern

import (
	"context"
	"sync"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/iter"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

const minClusterSize = 30

var drainConfig = &drain.Config{
	LogClusterDepth: 8,
	SimTh:           0.3,
	MaxChildren:     100,
	ParamString:     "<_>",
	MaxClusters:     300,
}

type stream struct {
	fp           model.Fingerprint
	labels       labels.Labels
	labelsString string
	labelHash    uint64
	patterns     *drain.Drain
	mtx          sync.Mutex

	lastTs int64
}

func newStream(
	fp model.Fingerprint,
	labels labels.Labels,
) (*stream, error) {
	return &stream{
		fp:           fp,
		labels:       labels,
		labelsString: labels.String(),
		labelHash:    labels.Hash(),
		patterns:     drain.New(drainConfig),
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

func (s *stream) Iterator(_ context.Context, from, through model.Time) (iter.Iterator, error) {
	// todo we should improve locking.
	s.mtx.Lock()
	defer s.mtx.Unlock()

	clusters := s.patterns.Clusters()
	iters := make([]iter.Iterator, 0, len(clusters))

	for _, cluster := range clusters {
		if cluster.Size < minClusterSize {
			continue
		}
		iters = append(iters, cluster.Iterator(from, through))
	}
	return iter.NewMerge(iters...), nil
}
