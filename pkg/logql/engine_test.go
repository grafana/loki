package logql

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	json "github.com/json-iterator/go"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/stats"
)

var testSize = int64(300)

func TestEngine_NewInstantQuery(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		qs        string
		ts        time.Time
		direction logproto.Direction
		limit     uint32

		// an array of streams per SelectParams will be returned by the querier.
		// This is to cover logql that requires multiple queries.
		streams [][]logproto.Stream
		params  []SelectParams

		expected promql.Value
	}{
		{
			`{app="foo"}`, time.Unix(30, 0), logproto.FORWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, identity, `{app="foo"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 10, Selector: `{app="foo"}`}},
			},
			Streams([]logproto.Stream{newStream(10, identity, `{app="foo"}`)}),
		},
		{
			`{app="bar"} |= "foo" |~ ".+bar"`, time.Unix(30, 0), logproto.BACKWARD, 30,
			[][]logproto.Stream{
				{newStream(testSize, identity, `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.BACKWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 30, Selector: `{app="bar"}|="foo"|~".+bar"`}},
			},
			Streams([]logproto.Stream{newStream(30, identity, `{app="bar"}`)}),
		},
		{
			`rate({app="foo"} |~".+bar" [1m])`, time.Unix(60, 0), logproto.BACKWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, identity, `{app="foo"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app="foo"}|~".+bar"`}},
			},
			promql.Vector{promql.Sample{Point: promql.Point{T: 60 * 1000, V: 1}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}}},
		},
		{
			`rate({app="foo"}[30s])`, time.Unix(60, 0), logproto.FORWARD, 10,
			[][]logproto.Stream{
				// 30s range the lower bound of the range is not inclusive only 15 samples will make it 60 included
				{newStream(testSize, offset(46, identity), `{app="foo"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(30, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app="foo"}`}},
			},
			promql.Vector{promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.5}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}}},
		},
		{
			`count_over_time({app="foo"} |~".+bar" [1m])`, time.Unix(60, 0), logproto.BACKWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`)}, // 10 , 20 , 30 .. 60 = 6 total
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app="foo"}|~".+bar"`}},
			},
			promql.Vector{promql.Sample{Point: promql.Point{T: 60 * 1000, V: 6}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}}},
		},
		{
			`count_over_time(({app="foo"} |~".+bar")[5m])`, time.Unix(5*60, 0), logproto.BACKWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`)}, // 10 , 20 , 30 .. 300 = 30 total
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(5*60, 0), Limit: 0, Selector: `{app="foo"}|~".+bar"`}},
			},
			promql.Vector{promql.Sample{Point: promql.Point{T: 5 * 60 * 1000, V: 30}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}}},
		},
		{
			`avg(count_over_time({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(10, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 6}, Metric: labels.Labels{}},
			},
		},
		{
			`min(rate({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(10, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.1}, Metric: labels.Labels{}},
			},
		},
		{
			`max by (app) (rate({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.2}, Metric: labels.Labels{labels.Label{Name: "app", Value: "bar"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.1}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}},
			},
		},
		{
			`max(rate({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.2}, Metric: labels.Labels{}},
			},
		},
		{
			`sum(rate({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(5, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.4}, Metric: labels.Labels{}},
			},
		},
		{
			`sum(count_over_time({app=~"foo|bar"} |~".+bar" [1m])) by (app)`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(10, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 6}, Metric: labels.Labels{labels.Label{Name: "app", Value: "bar"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 6}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}},
			},
		},
		{
			`count(count_over_time({app=~"foo|bar"} |~".+bar" [1m])) without (app)`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(10, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 2}, Metric: labels.Labels{}},
			},
		},
		{
			`stdvar without (app) (count_over_time(({app=~"foo|bar"} |~".+bar")[1m])) `, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 9}, Metric: labels.Labels{}},
			},
		},
		{
			`stddev(count_over_time(({app=~"foo|bar"} |~".+bar")[1m])) `, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(2, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 12}, Metric: labels.Labels{}},
			},
		},
		{
			`rate(({app=~"foo|bar"} |~".+bar")[1m])`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, offset(46, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.25}, Metric: labels.Labels{labels.Label{Name: "app", Value: "bar"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.1}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}},
			},
		},
		{
			`topk(2,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, offset(46, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.25}, Metric: labels.Labels{labels.Label{Name: "app", Value: "bar"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.1}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}},
			},
		},
		{
			`topk(1,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, offset(46, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.25}, Metric: labels.Labels{labels.Label{Name: "app", Value: "bar"}}},
			},
		},
		{
			`topk(1,rate(({app=~"foo|bar"} |~".+bar")[1m])) by (app)`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, offset(46, identity), `{app="bar"}`),
					newStream(testSize, factor(5, identity), `{app="fuzz"}`), newStream(testSize, identity, `{app="buzz"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.25}, Metric: labels.Labels{labels.Label{Name: "app", Value: "bar"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 1}, Metric: labels.Labels{labels.Label{Name: "app", Value: "buzz"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.1}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.2}, Metric: labels.Labels{labels.Label{Name: "app", Value: "fuzz"}}},
			},
		},
		{
			`bottomk(2,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, offset(46, identity), `{app="bar"}`),
					newStream(testSize, factor(5, identity), `{app="fuzz"}`), newStream(testSize, identity, `{app="buzz"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.1}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.2}, Metric: labels.Labels{labels.Label{Name: "app", Value: "fuzz"}}},
			},
		},
		{
			`bottomk(3,rate(({app=~"foo|bar"} |~".+bar")[1m])) without (app)`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, offset(46, identity), `{app="bar"}`),
					newStream(testSize, factor(5, identity), `{app="fuzz"}`), newStream(testSize, identity, `{app="buzz"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.25}, Metric: labels.Labels{labels.Label{Name: "app", Value: "bar"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.1}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 0.2}, Metric: labels.Labels{labels.Label{Name: "app", Value: "fuzz"}}},
			},
		},
		{
			`bottomk(3,rate(({app=~"foo|bar"} |~".+bar")[1m])) without (app) + 1`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, offset(46, identity), `{app="bar"}`),
					newStream(testSize, factor(5, identity), `{app="fuzz"}`), newStream(testSize, identity, `{app="buzz"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(60, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Vector{
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 1.25}, Metric: labels.Labels{labels.Label{Name: "app", Value: "bar"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 1.1}, Metric: labels.Labels{labels.Label{Name: "app", Value: "foo"}}},
				promql.Sample{Point: promql.Point{T: 60 * 1000, V: 1.2}, Metric: labels.Labels{labels.Label{Name: "app", Value: "fuzz"}}},
			},
		},
		{
			// healthcheck
			`1+1`, time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{},
			[]SelectParams{},
			promql.Scalar{T: 60 * 1000, V: 2},
		},
		{
			// single literal
			`2`,
			time.Unix(60, 0), logproto.FORWARD, 100,
			[][]logproto.Stream{},
			[]SelectParams{},
			promql.Scalar{T: 60 * 1000, V: 2},
		},
	} {
		test := test
		t.Run(fmt.Sprintf("%s %s", test.qs, test.direction), func(t *testing.T) {
			t.Parallel()

			eng := NewEngine(EngineOpts{}, newQuerierRecorder(test.streams, test.params))
			q := eng.NewInstantQuery(test.qs, test.ts, test.direction, test.limit)
			res, err := q.Exec(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, test.expected, res.Data)
		})
	}
}

func TestEngine_NewRangeQuery(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		qs        string
		start     time.Time
		end       time.Time
		step      time.Duration
		interval  time.Duration
		direction logproto.Direction
		limit     uint32

		// an array of streams per SelectParams will be returned by the querier.
		// This is to cover logql that requires multiple queries.
		streams [][]logproto.Stream
		params  []SelectParams

		expected promql.Value
	}{
		{
			`{app="foo"}`, time.Unix(0, 0), time.Unix(30, 0), time.Second, 0, logproto.FORWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, identity, `{app="foo"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 10, Selector: `{app="foo"}`}},
			},
			Streams([]logproto.Stream{newStream(10, identity, `{app="foo"}`)}),
		},
		{
			`{app="food"}`, time.Unix(0, 0), time.Unix(30, 0), 0, 2 * time.Second, logproto.FORWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, identity, `{app="food"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 10, Selector: `{app="food"}`}},
			},
			Streams([]logproto.Stream{newIntervalStream(10, 2*time.Second, identity, `{app="food"}`)}),
		},
		{
			`{app="fed"}`, time.Unix(0, 0), time.Unix(30, 0), 0, 2 * time.Second, logproto.BACKWARD, 10,
			[][]logproto.Stream{
				{newBackwardStream(testSize, identity, `{app="fed"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.BACKWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 10, Selector: `{app="fed"}`}},
			},
			Streams([]logproto.Stream{newBackwardIntervalStream(testSize, 10, 2*time.Second, identity, `{app="fed"}`)}),
		},
		{
			`{app="bar"} |= "foo" |~ ".+bar"`, time.Unix(0, 0), time.Unix(30, 0), time.Second, 0, logproto.BACKWARD, 30,
			[][]logproto.Stream{
				{newStream(testSize, identity, `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.BACKWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 30, Selector: `{app="bar"}|="foo"|~".+bar"`}},
			},
			Streams([]logproto.Stream{newStream(30, identity, `{app="bar"}`)}),
		},
		{
			`{app="barf"} |= "foo" |~ ".+bar"`, time.Unix(0, 0), time.Unix(30, 0), 0, 3 * time.Second, logproto.BACKWARD, 30,
			[][]logproto.Stream{
				{newBackwardStream(testSize, identity, `{app="barf"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.BACKWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 30, Selector: `{app="barf"}|="foo"|~".+bar"`}},
			},
			Streams([]logproto.Stream{newBackwardIntervalStream(testSize, 30, 3*time.Second, identity, `{app="barf"}`)}),
		},
		{
			`rate({app="foo"} |~".+bar" [1m])`, time.Unix(60, 0), time.Unix(120, 0), time.Minute, 0, logproto.BACKWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, identity, `{app="foo"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(120, 0), Limit: 0, Selector: `{app="foo"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 1}, {T: 120 * 1000, V: 1}},
				},
			},
		},
		{
			`rate({app="foo"}[30s])`, time.Unix(60, 0), time.Unix(120, 0), 15 * time.Second, 0, logproto.FORWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, factor(2, identity), `{app="foo"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(30, 0), End: time.Unix(120, 0), Limit: 0, Selector: `{app="foo"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.5}, {T: 75 * 1000, V: 0.5}, {T: 90 * 1000, V: 0.5}, {T: 105 * 1000, V: 0.5}, {T: 120 * 1000, V: 0.5}},
				},
			},
		},
		{
			`count_over_time({app="foo"} |~".+bar" [1m])`, time.Unix(60, 0), time.Unix(120, 0), 30 * time.Second, 0, logproto.BACKWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`)}, // 10 , 20 , 30 .. 60 = 6 total
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(120, 0), Limit: 0, Selector: `{app="foo"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 6}, {T: 90 * 1000, V: 6}, {T: 120 * 1000, V: 6}},
				},
			},
		},
		{
			`count_over_time(({app="foo"} |~".+bar")[5m])`, time.Unix(5*60, 0), time.Unix(5*120, 0), 30 * time.Second, 0, logproto.BACKWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`)}, // 10 , 20 , 30 .. 300 = 30 total
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(5*120, 0), Limit: 0, Selector: `{app="foo"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{
						{T: 300 * 1000, V: 30},
						{T: 330 * 1000, V: 30},
						{T: 360 * 1000, V: 30},
						{T: 390 * 1000, V: 30},
						{T: 420 * 1000, V: 30},
						{T: 450 * 1000, V: 30},
						{T: 480 * 1000, V: 30},
						{T: 510 * 1000, V: 30},
						{T: 540 * 1000, V: 30},
						{T: 570 * 1000, V: 30},
						{T: 600 * 1000, V: 30},
					},
				},
			},
		},
		{
			`avg(count_over_time({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(10, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{},
					Points: []promql.Point{{T: 60 * 1000, V: 6}, {T: 90 * 1000, V: 6}, {T: 120 * 1000, V: 6}, {T: 150 * 1000, V: 6}, {T: 180 * 1000, V: 6}},
				},
			},
		},
		{
			`min(rate({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(10, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{},
					Points: []promql.Point{{T: 60 * 1000, V: 0.1}, {T: 90 * 1000, V: 0.1}, {T: 120 * 1000, V: 0.1}, {T: 150 * 1000, V: 0.1}, {T: 180 * 1000, V: 0.1}},
				},
			},
		},
		{
			`max by (app) (rate({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.2}, {T: 90 * 1000, V: 0.2}, {T: 120 * 1000, V: 0.2}, {T: 150 * 1000, V: 0.2}, {T: 180 * 1000, V: 0.2}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.1}, {T: 90 * 1000, V: 0.1}, {T: 120 * 1000, V: 0.1}, {T: 150 * 1000, V: 0.1}, {T: 180 * 1000, V: 0.1}},
				},
			},
		},
		{
			`max(rate({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{},
					Points: []promql.Point{{T: 60 * 1000, V: 0.2}, {T: 90 * 1000, V: 0.2}, {T: 120 * 1000, V: 0.2}, {T: 150 * 1000, V: 0.2}, {T: 180 * 1000, V: 0.2}},
				},
			},
		},
		{
			`sum(rate({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(5, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{},
					Points: []promql.Point{{T: 60 * 1000, V: 0.4}, {T: 90 * 1000, V: 0.4}, {T: 120 * 1000, V: 0.4}, {T: 150 * 1000, V: 0.4}, {T: 180 * 1000, V: 0.4}},
				},
			},
		},
		{
			`sum(count_over_time({app=~"foo|bar"} |~".+bar" [1m])) by (app)`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 12}, {T: 90 * 1000, V: 12}, {T: 120 * 1000, V: 12}, {T: 150 * 1000, V: 12}, {T: 180 * 1000, V: 12}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 6}, {T: 90 * 1000, V: 6}, {T: 120 * 1000, V: 6}, {T: 150 * 1000, V: 6}, {T: 180 * 1000, V: 6}},
				},
			},
		},
		{
			`count(count_over_time({app=~"foo|bar"} |~".+bar" [1m])) without (app)`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(10, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{},
					Points: []promql.Point{{T: 60 * 1000, V: 2}, {T: 90 * 1000, V: 2}, {T: 120 * 1000, V: 2}, {T: 150 * 1000, V: 2}, {T: 180 * 1000, V: 2}},
				},
			},
		},
		{
			`stdvar without (app) (count_over_time(({app=~"foo|bar"} |~".+bar")[1m])) `, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{},
					Points: []promql.Point{{T: 60 * 1000, V: 9}, {T: 90 * 1000, V: 9}, {T: 120 * 1000, V: 9}, {T: 150 * 1000, V: 9}, {T: 180 * 1000, V: 9}},
				},
			},
		},
		{
			`stddev(count_over_time(({app=~"foo|bar"} |~".+bar")[1m])) `, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(2, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{},
					Points: []promql.Point{{T: 60 * 1000, V: 12}, {T: 90 * 1000, V: 12}, {T: 120 * 1000, V: 12}, {T: 150 * 1000, V: 12}, {T: 180 * 1000, V: 12}},
				},
			},
		},
		{
			`rate(({app=~"foo|bar"} |~".+bar")[1m])`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.2}, {T: 90 * 1000, V: 0.2}, {T: 120 * 1000, V: 0.2}, {T: 150 * 1000, V: 0.2}, {T: 180 * 1000, V: 0.2}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.1}, {T: 90 * 1000, V: 0.1}, {T: 120 * 1000, V: 0.1}, {T: 150 * 1000, V: 0.1}, {T: 180 * 1000, V: 0.1}},
				},
			},
		},
		{
			`topk(2,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`), newStream(testSize, factor(15, identity), `{app="boo"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.2}, {T: 90 * 1000, V: 0.2}, {T: 120 * 1000, V: 0.2}, {T: 150 * 1000, V: 0.2}, {T: 180 * 1000, V: 0.2}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.1}, {T: 90 * 1000, V: 0.1}, {T: 120 * 1000, V: 0.1}, {T: 150 * 1000, V: 0.1}, {T: 180 * 1000, V: 0.1}},
				},
			},
		},
		{
			`topk(1,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.2}, {T: 90 * 1000, V: 0.2}, {T: 120 * 1000, V: 0.2}, {T: 150 * 1000, V: 0.2}, {T: 180 * 1000, V: 0.2}},
				},
			},
		},
		{
			`topk(1,rate(({app=~"foo|bar"} |~".+bar")[1m])) by (app)`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(15, identity), `{app="fuzz"}`),
					newStream(testSize, factor(5, identity), `{app="fuzz"}`), newStream(testSize, identity, `{app="buzz"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "buzz"}},
					Points: []promql.Point{{T: 60 * 1000, V: 1}, {T: 90 * 1000, V: 1}, {T: 120 * 1000, V: 1}, {T: 150 * 1000, V: 1}, {T: 180 * 1000, V: 1}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.1}, {T: 90 * 1000, V: 0.1}, {T: 120 * 1000, V: 0.1}, {T: 150 * 1000, V: 0.1}, {T: 180 * 1000, V: 0.1}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "fuzz"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.2}, {T: 90 * 1000, V: 0.2}, {T: 120 * 1000, V: 0.2}, {T: 150 * 1000, V: 0.2}, {T: 180 * 1000, V: 0.2}},
				},
			},
		},
		{
			`bottomk(2,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{newStream(testSize, factor(10, identity), `{app="foo"}`), newStream(testSize, factor(20, identity), `{app="bar"}`),
					newStream(testSize, factor(5, identity), `{app="fuzz"}`), newStream(testSize, identity, `{app="buzz"}`)},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.05}, {T: 90 * 1000, V: 0.05}, {T: 120 * 1000, V: 0.05}, {T: 150 * 1000, V: 0.05}, {T: 180 * 1000, V: 0.05}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.1}, {T: 90 * 1000, V: 0.1}, {T: 120 * 1000, V: 0.1}, {T: 150 * 1000, V: 0.1}, {T: 180 * 1000, V: 0.1}},
				},
			},
		},
		{
			`bottomk(3,rate(({app=~"foo|bar|fuzz|buzz"} |~".+bar")[1m])) without (app)`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(10, identity), `{app="foo"}`),
					newStream(testSize, factor(20, identity), `{app="bar"}`),
					newStream(testSize, factor(5, identity), `{app="fuzz"}`),
					newStream(testSize, identity, `{app="buzz"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar|fuzz|buzz"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.05}, {T: 90 * 1000, V: 0.05}, {T: 120 * 1000, V: 0.05}, {T: 150 * 1000, V: 0.05}, {T: 180 * 1000, V: 0.05}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.1}, {T: 90 * 1000, V: 0.1}, {T: 120 * 1000, V: 0.1}, {T: 150 * 1000, V: 0.1}, {T: 180 * 1000, V: 0.1}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "fuzz"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.2}, {T: 90 * 1000, V: 0.2}, {T: 120 * 1000, V: 0.2}, {T: 150 * 1000, V: 0.2}, {T: 180 * 1000, V: 0.2}},
				},
			},
		},
		// binops
		{
			`rate({app="foo"}[1m]) or rate({app="bar"}[1m])`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
				},
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="foo"}`}},
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.2}, {T: 90 * 1000, V: 0.2}, {T: 120 * 1000, V: 0.2}, {T: 150 * 1000, V: 0.2}, {T: 180 * 1000, V: 0.2}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.2}, {T: 90 * 1000, V: 0.2}, {T: 120 * 1000, V: 0.2}, {T: 150 * 1000, V: 0.2}, {T: 180 * 1000, V: 0.2}},
				},
			},
		},
		{
			`
			rate({app=~"foo|bar"}[1m]) and
			rate({app="bar"}[1m])
			`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}`}},
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.2}, {T: 90 * 1000, V: 0.2}, {T: 120 * 1000, V: 0.2}, {T: 150 * 1000, V: 0.2}, {T: 180 * 1000, V: 0.2}},
				},
			},
		},
		{
			`
		rate({app=~"foo|bar"}[1m]) unless
		rate({app="bar"}[1m])
		`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}`}},
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.2}, {T: 90 * 1000, V: 0.2}, {T: 120 * 1000, V: 0.2}, {T: 150 * 1000, V: 0.2}, {T: 180 * 1000, V: 0.2}},
				},
			},
		},
		{
			`
		rate({app=~"foo|bar"}[1m]) +
		rate({app="bar"}[1m])
		`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}`}},
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.4}, {T: 90 * 1000, V: 0.4}, {T: 120 * 1000, V: 0.4}, {T: 150 * 1000, V: 0.4}, {T: 180 * 1000, V: 0.4}},
				},
			},
		},
		{
			`
		rate({app=~"foo|bar"}[1m]) -
		rate({app="bar"}[1m])
		`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}`}},
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0}, {T: 90 * 1000, V: 0}, {T: 120 * 1000, V: 0}, {T: 150 * 1000, V: 0}, {T: 180 * 1000, V: 0}},
				},
			},
		},
		{
			`
		count_over_time({app=~"foo|bar"}[1m]) *
		count_over_time({app="bar"}[1m])
		`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}`}},
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 144}, {T: 90 * 1000, V: 144}, {T: 120 * 1000, V: 144}, {T: 150 * 1000, V: 144}, {T: 180 * 1000, V: 144}},
				},
			},
		},
		{
			`
		count_over_time({app=~"foo|bar"}[1m]) *
		count_over_time({app="bar"}[1m])
		`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}`}},
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 144}, {T: 90 * 1000, V: 144}, {T: 120 * 1000, V: 144}, {T: 150 * 1000, V: 144}, {T: 180 * 1000, V: 144}},
				},
			},
		},
		{
			`
		count_over_time({app=~"foo|bar"}[1m]) /
		count_over_time({app="bar"}[1m])
		`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}`}},
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 1}, {T: 90 * 1000, V: 1}, {T: 120 * 1000, V: 1}, {T: 150 * 1000, V: 1}, {T: 180 * 1000, V: 1}},
				},
			},
		},
		{
			`
		count_over_time({app=~"foo|bar"}[1m]) %
		count_over_time({app="bar"}[1m])
		`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}`}},
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0}, {T: 90 * 1000, V: 0}, {T: 120 * 1000, V: 0}, {T: 150 * 1000, V: 0}, {T: 180 * 1000, V: 0}},
				},
			},
		},
		// tests precedence: should be x + (x/x)
		{
			`
		sum by (app) (rate({app=~"foo|bar"} |~".+bar" [1m])) +
		sum by (app) (rate({app=~"foo|bar"} |~".+bar" [1m])) /
		sum by (app) (rate({app=~"foo|bar"} |~".+bar" [1m]))
		`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 1.2}, {T: 90 * 1000, V: 1.2}, {T: 120 * 1000, V: 1.2}, {T: 150 * 1000, V: 1.2}, {T: 180 * 1000, V: 1.2}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 1.2}, {T: 90 * 1000, V: 1.2}, {T: 120 * 1000, V: 1.2}, {T: 150 * 1000, V: 1.2}, {T: 180 * 1000, V: 1.2}},
				},
			},
		},
		{
			`avg by (app) (
				sum by (app) (rate({app=~"foo|bar"} |~".+bar" [1m])) +
				sum by (app) (rate({app=~"foo|bar"} |~".+bar" [1m])) /
				sum by (app) (rate({app=~"foo|bar"} |~".+bar" [1m]))
			) * 2
		`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 2.4}, {T: 90 * 1000, V: 2.4}, {T: 120 * 1000, V: 2.4}, {T: 150 * 1000, V: 2.4}, {T: 180 * 1000, V: 2.4}},
				},
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "foo"}},
					Points: []promql.Point{{T: 60 * 1000, V: 2.4}, {T: 90 * 1000, V: 2.4}, {T: 120 * 1000, V: 2.4}, {T: 150 * 1000, V: 2.4}, {T: 180 * 1000, V: 2.4}},
				},
			},
		},
		{
			` sum (
					sum by (app) (rate({app=~"foo|bar"} |~".+bar" [1m])) +
					sum by (app) (rate({app=~"foo|bar"} |~".+bar" [1m])) /
					sum by (app) (rate({app=~"foo|bar"} |~".+bar" [1m]))
			) + 1
		`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="foo"}`),
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app=~"foo|bar"}|~".+bar"`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{},
					Points: []promql.Point{{T: 60 * 1000, V: 3.4}, {T: 90 * 1000, V: 3.4}, {T: 120 * 1000, V: 3.4}, {T: 150 * 1000, V: 3.4}, {T: 180 * 1000, V: 3.4}},
				},
			},
		},
		{
			`1+1--1`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{},
			[]SelectParams{},
			promql.Matrix{
				promql.Series{
					Points: []promql.Point{{T: 60000, V: 3}, {T: 90000, V: 3}, {T: 120000, V: 3}, {T: 150000, V: 3}, {T: 180000, V: 3}},
				},
			},
		},
		{
			`rate({app="bar"}[1m]) - 1`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: -0.8}, {T: 90 * 1000, V: -0.8}, {T: 120 * 1000, V: -0.8}, {T: 150 * 1000, V: -0.8}, {T: 180 * 1000, V: -0.8}},
				},
			},
		},
		{
			`1 - rate({app="bar"}[1m])`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: 0.8}, {T: 90 * 1000, V: 0.8}, {T: 120 * 1000, V: 0.8}, {T: 150 * 1000, V: 0.8}, {T: 180 * 1000, V: 0.8}},
				},
			},
		},
		{
			`rate({app="bar"}[1m]) - 1 / 2`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: -0.3}, {T: 90 * 1000, V: -0.3}, {T: 120 * 1000, V: -0.3}, {T: 150 * 1000, V: -0.3}, {T: 180 * 1000, V: -0.3}},
				},
			},
		},
		{
			`count_over_time({app="bar"}[1m]) ^ count_over_time({app="bar"}[1m])`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{
				{
					newStream(testSize, factor(5, identity), `{app="bar"}`),
				},
			},
			[]SelectParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(180, 0), Limit: 0, Selector: `{app="bar"}`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.Labels{{Name: "app", Value: "bar"}},
					Points: []promql.Point{{T: 60 * 1000, V: math.Pow(12, 12)}, {T: 90 * 1000, V: math.Pow(12, 12)}, {T: 120 * 1000, V: math.Pow(12, 12)}, {T: 150 * 1000, V: math.Pow(12, 12)}, {T: 180 * 1000, V: math.Pow(12, 12)}},
				},
			},
		},
		{
			`2`,
			time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Stream{},
			[]SelectParams{},
			promql.Matrix{
				promql.Series{
					Points: []promql.Point{{T: 60 * 1000, V: 2}, {T: 90 * 1000, V: 2}, {T: 120 * 1000, V: 2}, {T: 150 * 1000, V: 2}, {T: 180 * 1000, V: 2}},
				},
			},
		},
	} {
		test := test
		t.Run(fmt.Sprintf("%s %s", test.qs, test.direction), func(t *testing.T) {
			t.Parallel()

			eng := NewEngine(EngineOpts{}, newQuerierRecorder(test.streams, test.params))

			q := eng.NewRangeQuery(test.qs, test.start, test.end, test.step, test.interval, test.direction, test.limit)
			res, err := q.Exec(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, test.expected, res.Data)
		})
	}
}

func TestEngine_Stats(t *testing.T) {
	eng := NewEngine(EngineOpts{}, QuerierFunc(func(ctx context.Context, sp SelectParams) (iter.EntryIterator, error) {
		st := stats.GetChunkData(ctx)
		st.DecompressedBytes++
		return iter.NoopIterator, nil
	}))
	q := eng.NewInstantQuery(`{foo="bar"}`, time.Now(), logproto.BACKWARD, 1000)
	r, err := q.Exec(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), r.Statistics.Store.DecompressedBytes)
}

// go test -mod=vendor ./pkg/logql/ -bench=.  -benchmem -memprofile memprofile.out -cpuprofile cpuprofile.out
func BenchmarkRangeQuery100000(b *testing.B) {
	benchmarkRangeQuery(int64(100000), b)
}
func BenchmarkRangeQuery200000(b *testing.B) {
	benchmarkRangeQuery(int64(200000), b)
}
func BenchmarkRangeQuery500000(b *testing.B) {
	benchmarkRangeQuery(int64(500000), b)
}

func BenchmarkRangeQuery1000000(b *testing.B) {
	benchmarkRangeQuery(int64(1000000), b)
}

var result promql.Value

func benchmarkRangeQuery(testsize int64, b *testing.B) {
	b.ReportAllocs()
	eng := NewEngine(EngineOpts{}, getLocalQuerier(testsize))
	start := time.Unix(0, 0)
	end := time.Unix(testsize, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, test := range []struct {
			qs        string
			direction logproto.Direction
		}{
			{`{app="foo"}`, logproto.FORWARD},
			{`{app="bar"} |= "foo" |~ ".+bar"`, logproto.BACKWARD},
			{`rate({app="foo"} |~".+bar" [1m])`, logproto.BACKWARD},
			{`rate({app="foo"}[30s])`, logproto.FORWARD},
			{`count_over_time({app="foo"} |~".+bar" [1m])`, logproto.BACKWARD},
			{`count_over_time(({app="foo"} |~".+bar")[5m])`, logproto.BACKWARD},
			{`avg(count_over_time({app=~"foo|bar"} |~".+bar" [1m]))`, logproto.FORWARD},
			{`min(rate({app=~"foo|bar"} |~".+bar" [1m]))`, logproto.FORWARD},
			{`max by (app) (rate({app=~"foo|bar"} |~".+bar" [1m]))`, logproto.FORWARD},
			{`max(rate({app=~"foo|bar"} |~".+bar" [1m]))`, logproto.FORWARD},
			{`sum(rate({app=~"foo|bar"} |~".+bar" [1m]))`, logproto.FORWARD},
			{`sum(count_over_time({app=~"foo|bar"} |~".+bar" [1m])) by (app)`, logproto.FORWARD},
			{`count(count_over_time({app=~"foo|bar"} |~".+bar" [1m])) without (app)`, logproto.FORWARD},
			{`stdvar without (app) (count_over_time(({app=~"foo|bar"} |~".+bar")[1m])) `, logproto.FORWARD},
			{`stddev(count_over_time(({app=~"foo|bar"} |~".+bar")[1m])) `, logproto.FORWARD},
			{`rate(({app=~"foo|bar"} |~".+bar")[1m])`, logproto.FORWARD},
			{`topk(2,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, logproto.FORWARD},
			{`topk(1,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, logproto.FORWARD},
			{`topk(1,rate(({app=~"foo|bar"} |~".+bar")[1m])) by (app)`, logproto.FORWARD},
			{`bottomk(2,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, logproto.FORWARD},
			{`bottomk(3,rate(({app=~"foo|bar"} |~".+bar")[1m])) without (app)`, logproto.FORWARD},
		} {
			q := eng.NewRangeQuery(test.qs, start, end, 60*time.Second, 0, test.direction, 1000)
			res, err := q.Exec(context.Background())
			if err != nil {
				b.Fatal(err)
			}
			result = res.Data
			if result == nil {
				b.Fatal("unexpected nil result")
			}
		}
	}
}

func getLocalQuerier(size int64) Querier {
	iters := []iter.EntryIterator{
		iter.NewStreamIterator(newStream(size, identity, `{app="foo"}`)),
		iter.NewStreamIterator(newStream(size, identity, `{app="foo",bar="foo"}`)),
		iter.NewStreamIterator(newStream(size, identity, `{app="foo",bar="bazz"}`)),
		iter.NewStreamIterator(newStream(size, identity, `{app="foo",bar="fuzz"}`)),
		iter.NewStreamIterator(newStream(size, identity, `{app="bar"}`)),
		iter.NewStreamIterator(newStream(size, identity, `{app="bar",bar="foo"}`)),
		iter.NewStreamIterator(newStream(size, identity, `{app="bar",bar="bazz"}`)),
		iter.NewStreamIterator(newStream(size, identity, `{app="bar",bar="fuzz"}`)),
		// some duplicates
		iter.NewStreamIterator(newStream(size, identity, `{app="foo"}`)),
		iter.NewStreamIterator(newStream(size, identity, `{app="bar"}`)),
		iter.NewStreamIterator(newStream(size, identity, `{app="bar",bar="bazz"}`)),
		iter.NewStreamIterator(newStream(size, identity, `{app="bar"}`)),
	}
	return QuerierFunc(func(ctx context.Context, p SelectParams) (iter.EntryIterator, error) {
		return iter.NewHeapIterator(ctx, iters, p.Direction), nil
	})
}

type querierRecorder struct {
	source map[string][]logproto.Stream
}

func newQuerierRecorder(streams [][]logproto.Stream, params []SelectParams) *querierRecorder {
	source := map[string][]logproto.Stream{}
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
	return iter.NewHeapIterator(ctx, iters, p.Direction), nil
}

func paramsID(p SelectParams) string {
	b, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return string(b)
}

type generator func(i int64) logproto.Entry

func newStream(n int64, f generator, labels string) logproto.Stream {
	entries := []logproto.Entry{}
	for i := int64(0); i < n; i++ {
		entries = append(entries, f(i))
	}
	return logproto.Stream{
		Entries: entries,
		Labels:  labels,
	}
}

func newIntervalStream(n int64, step time.Duration, f generator, labels string) logproto.Stream {
	entries := []logproto.Entry{}
	lastEntry := int64(-100) // Start with a really small value (negative) so we always output the first item
	for i := int64(0); int64(len(entries)) < n; i++ {
		if float64(lastEntry)+step.Seconds() <= float64(i) {
			entries = append(entries, f(i))
			lastEntry = i
		}
	}
	return logproto.Stream{
		Entries: entries,
		Labels:  labels,
	}
}

func newBackwardStream(n int64, f generator, labels string) logproto.Stream {
	entries := []logproto.Entry{}
	for i := n - 1; i > 0; i-- {
		entries = append(entries, f(i))
	}
	return logproto.Stream{
		Entries: entries,
		Labels:  labels,
	}
}

func newBackwardIntervalStream(n, expectedResults int64, step time.Duration, f generator, labels string) logproto.Stream {
	entries := []logproto.Entry{}
	lastEntry := int64(100000) //Start with some really big value so that we always output the first item
	for i := n - 1; int64(len(entries)) < expectedResults; i-- {
		if float64(lastEntry)-step.Seconds() >= float64(i) {
			entries = append(entries, f(i))
			lastEntry = i
		}
	}
	return logproto.Stream{
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

// nolint
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
