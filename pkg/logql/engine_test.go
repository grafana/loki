package logql

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/assert"
)

var testSize = int64(100)

func TestEngine_NewInstantQuery(t *testing.T) {
	t.Parallel()
	eng := NewEngine(EngineOpts{})
	for _, test := range []struct {
		qs        string
		ts        time.Time
		direction logproto.Direction
		limit     uint32

		// an array of streams per SelectParams will be returned by the querier.
		// This is to cover logql that requires multiple queries.
		streams [][]*logproto.Stream
		params  []SelectParams

		expected promql.Value
	}{
		{
			`{app="foo"}`, time.Unix(30, 0), logproto.FORWARD, 10,
			[][]*logproto.Stream{
				{newStream(testSize, identity, `{app="foo"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 10, Selector: `{app="foo"}`}},
			},
			Streams([]*logproto.Stream{newStream(10, identity, `{app="foo"}`)}),
		},
		{
			`{app="bar"} |= "foo" |~ ".+bar"`, time.Unix(30, 0), logproto.BACKWARD, 30,
			[][]*logproto.Stream{
				{newStream(testSize, identity, `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.BACKWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 30, Selector: `{app="bar"}|="foo"|~".+bar"`}},
			},
			Streams([]*logproto.Stream{newStream(30, identity, `{app="bar"}`)}),
		},
		{
			`rate({app="foo"} |~".+bar" [1m])`, time.Unix(60, 0), logproto.BACKWARD, 10,
			[][]*logproto.Stream{
				{newStream(testSize, identity, `{app="foo"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app="foo"}|~".+bar"`}},
			},
			promql.Vector{promql.Sample{Point: promql.Point{T: 60000000000, V: 1}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}}},
		},
		{
			`rate({app="foo"}[30s])`, time.Unix(60, 0), logproto.FORWARD, 10,
			[][]*logproto.Stream{
				// 30s range the lower bound of the range is not inclusive only 15 samples will make it 60 included
				{newStream(testSize, offset(46, identity), `{app="foo"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(30, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app="foo"}`}},
			},
			promql.Vector{promql.Sample{Point: promql.Point{T: 60000000000, V: 0.5}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}}},
		},
		{
			`count_over_time({app="foo"} |~".+bar" [1m])`, time.Unix(60, 0), logproto.BACKWARD, 10,
			[][]*logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`)}, // 10 , 20 , 30 .. 60 = 6 total
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app="foo"}|~".+bar"`}},
			},
			promql.Vector{promql.Sample{Point: promql.Point{T: 60000000000, V: 6}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}}},
		},
		{
			`count_over_time(({app="foo"} |~".+bar")[5m])`, time.Unix(5*60, 0), logproto.BACKWARD, 10,
			[][]*logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`)}, // 10 , 20 , 30 .. 300 = 30 total
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(5*60, 0), Limit: 0, Selector: `{app="foo"}|~".+bar"`}},
			},
			promql.Vector{promql.Sample{Point: promql.Point{T: 5 * 60000000000, V: 30}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}}},
		},
		{
			`avg(count_over_time({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]*logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(10, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60000000000, V: 6}, Metric: labels.Labels{}},
			},
		},
		{
			`sum(count_over_time({app=~"foo|bar"} |~".+bar" [1m])) by (app)`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]*logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(10, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60000000000, V: 6}, Metric: labels.Labels{labels.Label{Name: "app", Value: "bar"}}},
				promql.Sample{Point: promql.Point{T: 60000000000, V: 6}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}},
			},
		},
	} {
		test := test
		t.Run(fmt.Sprintf("%s %s", test.qs, test.direction), func(t *testing.T) {
			t.Parallel()

			q := eng.NewInstantQuery(newQuerierRecorder(test.streams, test.params), test.qs, test.ts, test.direction, test.limit)
			res, err := q.Exec(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, test.expected, res)
		})
	}
}

type querierRecorder struct {
	source map[string][]*logproto.Stream
}

func newQuerierRecorder(streams [][]*logproto.Stream, params []SelectParams) *querierRecorder {
	source := map[string][]*logproto.Stream{}
	for i, p := range params {
		source[paramsID(p)] = streams[i]
	}
	return &querierRecorder{
		source: source,
	}
}

func (q *querierRecorder) Select(ctx context.Context, p SelectParams) (iter.EntryIterator, error) {
	recordID := paramsID(p)
	streams, ok := q.source[recordID]
	if !ok {
		return nil, fmt.Errorf("no streams found for id: %s has: %+v", recordID, q.source)
	}
	iters := make([]iter.EntryIterator, 0, len(streams))
	for _, s := range streams {
		iters = append(iters, iter.NewStreamIterator(s))
	}
	return iter.NewHeapIterator(iters, p.Direction), nil
}

func paramsID(p SelectParams) string {
	b, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return string(b)
}

type generator func(i int64) logproto.Entry

func newStream(n int64, f generator, labels string) *logproto.Stream {
	entries := []logproto.Entry{}
	for i := int64(0); i < n; i++ {
		entries = append(entries, f(i))
	}
	return &logproto.Stream{
		Entries: entries,
		Labels:  labels,
	}
}

func identity(i int64) logproto.Entry {
	return logproto.Entry{
		Timestamp: time.Unix(i, 0),
		Line:      fmt.Sprintf("%d", i),
	}
}

// nolint
func factor(j int64, g generator) generator {
	return func(i int64) logproto.Entry {
		return g(i * j)
	}
}

func offset(j int64, g generator) generator {
	return func(i int64) logproto.Entry {
		return g(i + j)
	}
}

// nolint
func constant(t int64) generator {
	return func(i int64) logproto.Entry {
		return logproto.Entry{
			Timestamp: time.Unix(t, 0),
			Line:      fmt.Sprintf("%d", i),
		}
	}
}

// nolint
func inverse(g generator) generator {
	return func(i int64) logproto.Entry {
		return g(-i)
	}
}
