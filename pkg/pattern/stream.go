package pattern

import (
	"context"
	"sync"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/pattern/drain"
	"github.com/grafana/loki/pkg/pattern/iter"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// TODO(kolesnikovae):
//
// This is crucial for Drain to ensure that the first LogClusterDepth tokens
// are constant (see https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf).
// We should remove any variables such as timestamps, IDs, IPs, counters, etc.
// from these tokens.
//
// Moreover, Drain is not designed for structured logs. Therefore, we should
// handle logfmt (and, probably, JSON) logs in a special way:
//
// The parse tree should have a fixed length, and the depth should be
// determined by the number of fields in the logfmt message.
// A parsing tree should be maintained for each unique field set.

var drainConfig = &drain.Config{
	// At training, if at the depth of LogClusterDepth there is a cluster with
	// similarity coefficient greater that SimTh, then the log message is added
	// to that cluster. Otherwise, a new cluster is created.
	//
	// LogClusterDepth should be equal to the number of constant tokens from
	// the beginning of the message that likely determine the message contents.
	//
	//  > In this step, Drain traverses from a 1-st layer node, which
	//  > is searched in step 2, to a leaf node. This step is based on
	//  > the assumption that tokens in the beginning positions of a log
	//  > message are more likely to be constants. Specifically, Drain
	//  > selects the next internal node by the tokens in the beginning
	//  > positions of the log message
	LogClusterDepth: 8,
	// SimTh is basically a ratio of matching/total in the cluster.
	// Cluster tokens: "foo <*> bar fred"
	//       Log line: "foo bar baz qux"
	//                  *   *   *   x
	// Similarity of these sequences is 0.75 (the distance)
	// Both SimTh and MaxClusterDepth impact branching factor: the greater
	// MaxClusterDepth and SimTh, the less the chance that there will be
	// "similar" clusters, but the greater the footprint.
	SimTh:       0.3,
	MaxChildren: 100,
	ParamString: "<_>",
	MaxClusters: 300,
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
		iters = append(iters, cluster.Iterator(from, through))
	}
	return iter.NewMerge(iters...), nil
}
