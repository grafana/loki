package logql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/sketch"
)

func TestProbabilisticEngine(t *testing.T) {
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
		data   interface{}
		params interface{}

		expected promql_parser.Value
	}{
		// queries inherited from engine_test
		{
			`rate(({app=~"foo|bar"} |~".+bar")[1m])`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Series{
				{newSeries(testSize, factor(10, identity), `{app="foo"}`), newSeries(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectSampleParams{
				{&logproto.SampleQueryRequest{Start: time.Unix(0, 0), End: time.Unix(180, 0), Selector: `rate({app=~"foo|bar"}|~".+bar"[1m])`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.FromStrings("app", "bar"),
					Floats: []promql.FPoint{{T: 60 * 1000, F: 0.2}, {T: 90 * 1000, F: 0.2}, {T: 120 * 1000, F: 0.2}, {T: 150 * 1000, F: 0.2}, {T: 180 * 1000, F: 0.2}},
				},
				promql.Series{
					Metric: labels.FromStrings("app", "foo"),
					Floats: []promql.FPoint{{T: 60 * 1000, F: 0.1}, {T: 90 * 1000, F: 0.1}, {T: 120 * 1000, F: 0.1}, {T: 150 * 1000, F: 0.1}, {T: 180 * 1000, F: 0.1}},
				},
			},
		},
		{
			`max(rate({app=~"foo|bar"} |~".+bar" [1m]))`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Series{
				{newSeries(testSize, factor(10, identity), `{app="foo"}`), newSeries(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectSampleParams{
				{&logproto.SampleQueryRequest{Start: time.Unix(0, 0), End: time.Unix(180, 0), Selector: `rate({app=~"foo|bar"}|~".+bar"[1m])`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.EmptyLabels(),
					Floats: []promql.FPoint{{T: 60 * 1000, F: 0.2}, {T: 90 * 1000, F: 0.2}, {T: 120 * 1000, F: 0.2}, {T: 150 * 1000, F: 0.2}, {T: 180 * 1000, F: 0.2}},
				},
			},
		},
		{
			`count_over_time({app="foo"} |~".+bar" [1m])`, time.Unix(60, 0), time.Unix(120, 0), 30 * time.Second, 0, logproto.BACKWARD, 10,
			[][]logproto.Series{
				{newSeries(testSize, factor(10, identity), `{app="foo"}`)}, // 10 , 20 , 30 .. 60 = 6 total
			},
			[]SelectSampleParams{
				{&logproto.SampleQueryRequest{Start: time.Unix(0, 0), End: time.Unix(120, 0), Selector: `count_over_time({app="foo"}|~".+bar"[1m])`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.FromStrings("app", "foo"),
					Floats: []promql.FPoint{{T: 60 * 1000, F: 6}, {T: 90 * 1000, F: 6}, {T: 120 * 1000, F: 6}},
				},
			},
		},
		{
			`rate(({app=~"foo|bar"} |~".+bar")[1m])`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Series{
				{newSeries(testSize, factor(10, identity), `{app="foo"}`), newSeries(testSize, factor(5, identity), `{app="bar"}`)},
			},
			[]SelectSampleParams{
				{&logproto.SampleQueryRequest{Start: time.Unix(0, 0), End: time.Unix(180, 0), Selector: `rate({app=~"foo|bar"}|~".+bar"[1m])`}},
			},
			promql.Matrix{
				promql.Series{
					Metric: labels.FromStrings("app", "bar"),
					Floats: []promql.FPoint{{T: 60 * 1000, F: 0.2}, {T: 90 * 1000, F: 0.2}, {T: 120 * 1000, F: 0.2}, {T: 150 * 1000, F: 0.2}, {T: 180 * 1000, F: 0.2}},
				},
				promql.Series{
					Metric: labels.FromStrings("app", "foo"),
					Floats: []promql.FPoint{{T: 60 * 1000, F: 0.1}, {T: 90 * 1000, F: 0.1}, {T: 120 * 1000, F: 0.1}, {T: 150 * 1000, F: 0.1}, {T: 180 * 1000, F: 0.1}},
				},
			},
		},
		// probabilistic queries
		{
			`topk(2,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Series{
				{newSeries(testSize, factor(10, identity), `{app="foo"}`), newSeries(testSize, factor(5, identity), `{app="bar"}`), newSeries(testSize, factor(15, identity), `{app="boo"}`)},
			},
			[]SelectSampleParams{
				{&logproto.SampleQueryRequest{Start: time.Unix(0, 0), End: time.Unix(180, 0), Selector: `rate({app=~"foo|bar"}|~".+bar"[1m])`}},
			},
			sketch.TopKMatrix{
				sketch.TopKVector{
					Topk: &sketch.Topk{
						Heaps: map[string]*sketch.MinHeap{"17241709254077376921": {
							{Event: `{app="foo"}`},
							{Event: `{app="bar"}`},
							{Event: `{app="boo"}`},
						}},
					},
					TS: 60_000,
				},
				sketch.TopKVector{
					Topk: &sketch.Topk{
						Heaps: map[string]*sketch.MinHeap{"17241709254077376921": {
							{Event: `{app="foo"}`},
							{Event: `{app="bar"}`},
							{Event: `{app="boo"}`},
						}},
					},
					TS: 90_000,
				},
				sketch.TopKVector{
					Topk: &sketch.Topk{
						Heaps: map[string]*sketch.MinHeap{"17241709254077376921": {
							{Event: `{app="foo"}`},
							{Event: `{app="bar"}`},
							{Event: `{app="boo"}`},
						}},
					},
					TS: 12_0000,
				},
				sketch.TopKVector{
					Topk: &sketch.Topk{
						Heaps: map[string]*sketch.MinHeap{"17241709254077376921": {
							{Event: `{app="foo"}`},
							{Event: `{app="bar"}`},
							{Event: `{app="boo"}`},
						}},
					},
					TS: 15_0000,
				},
				sketch.TopKVector{
					Topk: &sketch.Topk{
						Heaps: map[string]*sketch.MinHeap{"17241709254077376921": {
							{Event: `{app="foo"}`},
							{Event: `{app="bar"}`},
							{Event: `{app="boo"}`},
						}},
					},
					TS: 18_0000,
				},
			},
		},
		{
			`topk(1,rate(({app=~"foo|bar"} |~".+bar")[1m])) by (app)`, time.Unix(60, 0), time.Unix(180, 0), 30 * time.Second, 0, logproto.FORWARD, 100,
			[][]logproto.Series{
				{
					newSeries(testSize, factor(10, identity), `{app="foo"}`),
					newSeries(testSize, factor(5, identity), `{app="fuzz"}`), newSeries(testSize, identity, `{app="buzz"}`),
				},
			},
			[]SelectSampleParams{
				{&logproto.SampleQueryRequest{Start: time.Unix(0, 0), End: time.Unix(180, 0), Selector: `rate({app=~"foo|bar"}|~".+bar"[1m])`}},
			},
			sketch.TopKMatrix{
				sketch.TopKVector{
					Topk: &sketch.Topk{
						Heaps: map[string]*sketch.MinHeap{
							"3939591336247135291":  {{Event: `{app="fuzz"}`}},
							"9576730571217736695":  {{Event: `{app="foo"}`}},
							"12597166300821646905": {{Event: `{app="buzz"}`}},
						},
					},
					TS: 60_000,
				},
				sketch.TopKVector{
					Topk: &sketch.Topk{
						Heaps: map[string]*sketch.MinHeap{
							"3939591336247135291":  {{Event: `{app="fuzz"}`}},
							"9576730571217736695":  {{Event: `{app="foo"}`}},
							"12597166300821646905": {{Event: `{app="buzz"}`}},
						},
					},
					TS: 90_000,
				},
				sketch.TopKVector{
					Topk: &sketch.Topk{
						Heaps: map[string]*sketch.MinHeap{
							"3939591336247135291":  {{Event: `{app="fuzz"}`}},
							"9576730571217736695":  {{Event: `{app="foo"}`}},
							"12597166300821646905": {{Event: `{app="buzz"}`}},
						},
					},
					TS: 12_0000,
				},
				sketch.TopKVector{
					Topk: &sketch.Topk{
						Heaps: map[string]*sketch.MinHeap{
							"3939591336247135291":  {{Event: `{app="fuzz"}`}},
							"9576730571217736695":  {{Event: `{app="foo"}`}},
							"12597166300821646905": {{Event: `{app="buzz"}`}},
						},
					},
					TS: 15_0000,
				},
				sketch.TopKVector{
					Topk: &sketch.Topk{
						Heaps: map[string]*sketch.MinHeap{
							"3939591336247135291":  {{Event: `{app="fuzz"}`}},
							"9576730571217736695":  {{Event: `{app="foo"}`}},
							"12597166300821646905": {{Event: `{app="buzz"}`}},
						},
					},
					TS: 18_0000,
				},
			},
		},
	} {
		test := test
		t.Run(fmt.Sprintf("%s %s", test.qs, test.direction), func(t *testing.T) {
			t.Parallel()

			eng := NewEngine(EngineOpts{}, newQuerierRecorder(t, test.data, test.params), NoLimits, log.NewNopLogger())

			q := eng.ProbabilisticQuery(LiteralParams{
				qs:        test.qs,
				start:     test.start,
				end:       test.end,
				step:      test.step,
				interval:  test.interval,
				direction: test.direction,
				limit:     test.limit,
			})
			res, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
			if err != nil {
				t.Fatal(err)
			}
			switch expected := test.expected.(type) {
			case sketch.TopKMatrix:
				actual := res.Data.(sketch.TopKMatrix)
				require.Len(t, actual, len(expected))

				actualLabels := make([]string, 0)
				expectedLabels := make([]string, 0)

				for i := range actual {
					assert.Equal(t, expected[i].TS, actual[i].TS)

					// Only the labels are tested here.
					require.ElementsMatchf(t, actual[i].Topk.GroupingKeys(), expected[i].Topk.GroupingKeys(), "at TS:%d", actual[i].TS)
					for _, key := range actual[i].Topk.GroupingKeys() {
						require.ElementsMatchf(t, actual[i].Topk.DistinctEventsForGroupingKey(key), expected[i].Topk.DistinctEventsForGroupingKey(key), "at TS:%d for key:%s", actual[i].TS, key)
					}
				}

				require.ElementsMatch(t, actualLabels, expectedLabels)
			default:
				assert.Equal(t, test.expected, res.Data)
			}

		})
	}
}
