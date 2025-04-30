package engine

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"

	"github.com/grafana/loki/pkg/push"
)

func newResultBuilder() *resultBuilder {
	return &resultBuilder{
		streams: make(map[string]int),
	}
}

type resultBuilder struct {
	streams map[string]int
	data    logqlmodel.Streams
	stats   stats.Result
	count   int
}

func (b *resultBuilder) add(lbs labels.Labels, entry logproto.Entry) {
	key := lbs.String()
	idx, ok := b.streams[key]
	if !ok {
		idx = len(b.data)
		b.streams[key] = idx
		b.data = append(b.data, push.Stream{Labels: key})
	}
	b.data[idx].Entries = append(b.data[idx].Entries, entry)
	b.count++
}

func (b *resultBuilder) setStats(s stats.Result) {
	b.stats = s
}

func (b *resultBuilder) build() logqlmodel.Result {
	return logqlmodel.Result{
		Data:       b.data,
		Statistics: b.stats,
	}
}

func (b *resultBuilder) empty() logqlmodel.Result {
	return logqlmodel.Result{}
}

func (b *resultBuilder) len() int {
	return b.count
}
