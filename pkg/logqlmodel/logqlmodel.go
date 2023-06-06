package logqlmodel

import (
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase/definitions"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/util/sketch"
)

// ValueTypeStreams promql.ValueType for log streams
const ValueTypeStreams = "streams"

// ValueTypeSketches promql.ValueType for sketches
const ValueTypeSketches = "sketches"

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

// Sketch is promql.Value
type Sketches []sketch.Topk

// Type implements `promql.Value`
func (Sketches) Type() parser.ValueType { return ValueTypeSketches }

// String implements `promql.Value`
func (Sketches) String() string {
	return ""
}
