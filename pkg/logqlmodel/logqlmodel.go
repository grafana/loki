package logqlmodel

import (
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase/definitions"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
)

// ValueTypeStreams promql.ValueType for log streams
const ValueTypeStreams = "streams"

// PackedEntryKey is a special JSON key used by the pack promtail stage and unpack parser
const PackedEntryKey = "_entry"

const ValueTypeExemplars = "exemplars"

// Result is the result of a query execution.
type Result struct {
	Data       parser.Value
	Statistics stats.Result
	Headers    []*definitions.PrometheusResponseHeader
}

// Streams is promql.Value
type Streams []logproto.Stream

func (streams Streams) Len() int      { return len(streams) }
func (streams Streams) Swap(i, j int) { streams[i], streams[j] = streams[j], streams[i] }
func (streams Streams) Less(i, j int) bool {
	return streams[i].Labels <= streams[j].Labels
}

// Type implements `promql.Value`
func (Streams) Type() parser.ValueType { return ValueTypeStreams }

// String implements `promql.Value`
func (Streams) String() string {
	return ""
}

func (streams Streams) Lines() int64 {
	var res int64
	for _, s := range streams {
		res += int64(len(s.Entries))
	}
	return res
}

// Exemplars is promql.Value
type Exemplars []exemplar.QueryResult

func (exemplars Exemplars) Len() int      { return len(exemplars) }
func (exemplars Exemplars) Swap(i, j int) { exemplars[i], exemplars[j] = exemplars[j], exemplars[i] }
func (exemplars Exemplars) Less(i, j int) bool {
	return exemplars[i].SeriesLabels.String() <= exemplars[j].SeriesLabels.String()
}

// Type implements `promql.Value`
func (Exemplars) Type() parser.ValueType { return ValueTypeExemplars }

// String implements `promql.Value`
func (Exemplars) String() string {
	return ""
}
func (exemplars Exemplars) SeriesLabels() int64 {
	var res int64
	for _, s := range exemplars {
		res += int64(len(s.SeriesLabels))
	}
	return res
}
