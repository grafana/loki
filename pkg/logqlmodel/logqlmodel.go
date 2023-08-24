package logqlmodel

import (
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase/definitions"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
)

// ValueTypeStreams promql.ValueType for log streams
const ValueTypeStreams = "streams"

// PackedEntryKey is a special JSON key used by the pack promtail stage and unpack parser
const PackedEntryKey = "_entry"

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
	// TODO: Fix this. It's quite innefcient because we are creating a string from the groups
	// whereas we used to have the string already.
	// return logproto.FromLabelAdaptersToLabels(logproto.FromGroupedLabelsToUngroupedLabels(streams[i].Labels)).String() <= logproto.FromLabelAdaptersToLabels(logproto.FromGroupedLabelsToUngroupedLabels(streams[j].Labels)).String()
	return streams[i].Labels <= streams[j].Labels
}

// Type implements `promql.Value` and `parser.Value`
func (Streams) Type() parser.ValueType { return ValueTypeStreams }

// String implements `promql.Value` and `parser.Value`
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
