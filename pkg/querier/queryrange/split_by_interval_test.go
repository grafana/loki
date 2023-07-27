package queryrange

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/config"
)

var nilMetrics = NewSplitByMetrics(nil)

var testSchemas = func() []config.PeriodConfig {
	confS := `
- from: "1950-01-01"
  store: boltdb-shipper
  object_store: gcs
  schema: v12
`

	var confs []config.PeriodConfig
	if err := yaml.Unmarshal([]byte(confS), &confs); err != nil {
		panic(err)
	}
	return confs
}()

var testSchemasTSDB = func() []config.PeriodConfig {
	confS := `
- from: "1950-01-01"
  store: tsdb
  object_store: gcs
  schema: v12
`

	var confs []config.PeriodConfig
	if err := yaml.Unmarshal([]byte(confS), &confs); err != nil {
		panic(err)
	}
	return confs
}()

func Test_splitQuery(t *testing.T) {
	buildLokiRequest := func(start, end time.Time) queryrangebase.Request {
		return &LokiRequest{
			Query:     "foo",
			Limit:     1,
			Step:      2,
			StartTs:   start,
			EndTs:     end,
			Direction: logproto.BACKWARD,
			Path:      "/path",
		}
	}

	buildLokiRequestWithInterval := func(start, end time.Time) queryrangebase.Request {
		return &LokiRequest{
			Query:     "foo",
			Limit:     1,
			Interval:  2,
			StartTs:   start,
			EndTs:     end,
			Direction: logproto.BACKWARD,
			Path:      "/path",
		}
	}

	buildLokiSeriesRequest := func(start, end time.Time) queryrangebase.Request {
		return &LokiSeriesRequest{
			Match:   []string{"match1"},
			StartTs: start,
			EndTs:   end,
			Path:    "/series",
			Shards:  []string{"shard1"},
		}
	}

	buildLokiLabelNamesRequest := func(start, end time.Time) queryrangebase.Request {
		return &LokiLabelNamesRequest{
			StartTs: start,
			EndTs:   end,
			Path:    "/labels",
		}
	}

	type interval struct {
		start, end time.Time
	}
	for requestType, tc := range map[string]struct {
		requestBuilderFunc func(start, end time.Time) queryrangebase.Request
		endTimeInclusive   bool
	}{
		"LokiRequest": {
			buildLokiRequest,
			false,
		},
		"LokiRequestWithInterval": {
			buildLokiRequestWithInterval,
			false,
		},
		"LokiSeriesRequest": {
			buildLokiSeriesRequest,
			true,
		},
		"LokiLabelNamesRequest": {
			buildLokiLabelNamesRequest,
			true,
		},
	} {
		expectedSplitGap := time.Duration(0)
		if tc.endTimeInclusive {
			expectedSplitGap = time.Millisecond
		}
		for name, intervals := range map[string]struct {
			inp      interval
			expected []interval
		}{
			"no_change": {
				inp: interval{
					start: time.Unix(0, 0),
					end:   time.Unix(0, (1 * time.Hour).Nanoseconds()),
				},
				expected: []interval{
					{
						start: time.Unix(0, 0),
						end:   time.Unix(0, (1 * time.Hour).Nanoseconds()),
					},
				},
			},
			"align_start": {
				inp: interval{
					start: time.Unix(0, (5 * time.Minute).Nanoseconds()),
					end:   time.Unix(0, (2 * time.Hour).Nanoseconds()),
				},
				expected: []interval{
					{
						start: time.Unix(0, (5 * time.Minute).Nanoseconds()),
						end:   time.Unix(0, (1 * time.Hour).Nanoseconds()).Add(-expectedSplitGap),
					},
					{
						start: time.Unix(0, (1 * time.Hour).Nanoseconds()),
						end:   time.Unix(0, (2 * time.Hour).Nanoseconds()),
					},
				},
			},
			"align_end": {
				inp: interval{
					start: time.Unix(0, 0),
					end:   time.Unix(0, (115 * time.Minute).Nanoseconds()),
				},
				expected: []interval{
					{
						start: time.Unix(0, 0),
						end:   time.Unix(0, (1 * time.Hour).Nanoseconds()).Add(-expectedSplitGap),
					},
					{
						start: time.Unix(0, (1 * time.Hour).Nanoseconds()),
						end:   time.Unix(0, (115 * time.Minute).Nanoseconds()),
					},
				},
			},
			"align_both": {
				inp: interval{
					start: time.Unix(0, (5 * time.Minute).Nanoseconds()),
					end:   time.Unix(0, (175 * time.Minute).Nanoseconds()),
				},
				expected: []interval{
					{
						start: time.Unix(0, (5 * time.Minute).Nanoseconds()),
						end:   time.Unix(0, (1 * time.Hour).Nanoseconds()).Add(-expectedSplitGap),
					},
					{
						start: time.Unix(0, (1 * time.Hour).Nanoseconds()),
						end:   time.Unix(0, (2 * time.Hour).Nanoseconds()).Add(-expectedSplitGap),
					},
					{
						start: time.Unix(0, (2 * time.Hour).Nanoseconds()),
						end:   time.Unix(0, (175 * time.Minute).Nanoseconds()),
					},
				},
			},
			"no_align": {
				inp: interval{
					start: time.Unix(0, (5 * time.Minute).Nanoseconds()),
					end:   time.Unix(0, (55 * time.Minute).Nanoseconds()),
				},
				expected: []interval{
					{
						start: time.Unix(0, (5 * time.Minute).Nanoseconds()),
						end:   time.Unix(0, (55 * time.Minute).Nanoseconds()),
					},
				},
			},
		} {
			t.Run(fmt.Sprintf("%s - %s", name, requestType), func(t *testing.T) {
				inp := tc.requestBuilderFunc(intervals.inp.start, intervals.inp.end)
				var want []queryrangebase.Request
				for _, interval := range intervals.expected {
					want = append(want, tc.requestBuilderFunc(interval.start, interval.end))
				}
				splits, err := splitByTime(inp, time.Hour)
				require.NoError(t, err)
				require.Equal(t, want, splits)
			})
		}
	}
}

func Test_splitMetricQuery(t *testing.T) {
	const seconds = 1e3 // 1e3 milliseconds per second.

	for i, tc := range []struct {
		input    queryrangebase.Request
		expected []queryrangebase.Request
		interval time.Duration
	}{
		// the step is lower than the interval therefore we should split only once.
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(0, 60*time.Minute.Nanoseconds()),
				Step:    15 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(0, 60*time.Minute.Nanoseconds()),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 24 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(60*60, 0),
				Step:    15 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(60*60, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 3 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(24*3600, 0),
				Step:    15 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(24*3600, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 24 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(3*3600, 0),
				Step:    15 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(3*3600, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 3 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(2*24*3600, 0),
				Step:    15 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix((24*3600)-15, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix((24 * 3600), 0),
					EndTs:   time.Unix((2 * 24 * 3600), 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 24 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(2*3*3600, 0),
				Step:    15 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix((3*3600)-15, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix((3 * 3600), 0),
					EndTs:   time.Unix((2 * 3 * 3600), 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 3 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(3*3600, 0),
				EndTs:   time.Unix(3*24*3600, 0),
				Step:    15 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(3*3600, 0),
					EndTs:   time.Unix((24*3600)-15, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix(24*3600, 0),
					EndTs:   time.Unix((2*24*3600)-15, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix(2*24*3600, 0),
					EndTs:   time.Unix(3*24*3600, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 24 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(2*3600, 0),
				EndTs:   time.Unix(3*3*3600, 0),
				Step:    15 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(2*3600, 0),
					EndTs:   time.Unix((3*3600)-15, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix(3*3600, 0),
					EndTs:   time.Unix((2*3*3600)-15, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix(2*3*3600, 0),
					EndTs:   time.Unix(3*3*3600, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 3 * time.Hour,
		},

		// step not a multiple of interval
		// start time already step aligned
		{
			input: &LokiRequest{
				StartTs: time.Unix(2*3600-9, 0), // 2h mod 17s = 9s
				EndTs:   time.Unix(3*3*3600, 0),
				Step:    17 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(2*3600-9, 0),
					EndTs:   time.Unix((3*3600)-5, 0), // 3h mod 17s = 5s
					Step:    17 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix((3*3600)+12, 0),
					EndTs:   time.Unix((2*3*3600)-10, 0), // 6h mod 17s = 10s
					Step:    17 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix(2*3*3600+7, 0),
					EndTs:   time.Unix(3*3*3600+2, 0), // 9h mod 17s = 2s
					Step:    17 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 3 * time.Hour,
		},
		// end time already step aligned
		{
			input: &LokiRequest{
				StartTs: time.Unix(2*3600, 0),
				EndTs:   time.Unix(3*3*3600+2, 0), // 9h mod 17s = 2s
				Step:    17 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(2*3600-9, 0),   // 2h mod 17s = 9s
					EndTs:   time.Unix((3*3600)-5, 0), // 3h mod 17s = 5s
					Step:    17 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix((3*3600)+12, 0),
					EndTs:   time.Unix((2*3*3600)-10, 0), // 6h mod 17s = 10s
					Step:    17 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix(2*3*3600+7, 0),
					EndTs:   time.Unix(3*3*3600+2, 0),
					Step:    17 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 3 * time.Hour,
		},
		// start & end time not aligned with step
		{
			input: &LokiRequest{
				StartTs: time.Unix(2*3600, 0),
				EndTs:   time.Unix(3*3*3600, 0),
				Step:    17 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(2*3600-9, 0),   // 2h mod 17s = 9s
					EndTs:   time.Unix((3*3600)-5, 0), // 3h mod 17s = 5s
					Step:    17 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix((3*3600)+12, 0),
					EndTs:   time.Unix((2*3*3600)-10, 0), // 6h mod 17s = 10s
					Step:    17 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix(2*3*3600+7, 0),
					EndTs:   time.Unix(3*3*3600+2, 0), // 9h mod 17s = 2s
					Step:    17 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 3 * time.Hour,
		},

		// step larger than split interval
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(25*3600, 0),
				Step:    6 * 3600 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(6*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix(6*3600, 0),
					EndTs:   time.Unix(12*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix(12*3600, 0),
					EndTs:   time.Unix(18*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix(18*3600, 0),
					EndTs:   time.Unix(24*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
				&LokiRequest{
					StartTs: time.Unix(24*3600, 0),
					EndTs:   time.Unix(30*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 15 * time.Minute,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(1*3600, 0),
				EndTs:   time.Unix(3*3600, 0),
				Step:    6 * 3600 * seconds,
				Query:   `rate({app="foo"}[1m])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(6*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   `rate({app="foo"}[1m])`,
				},
			},
			interval: 15 * time.Minute,
		},
		// reduce split by to 6h instead of 1h
		{
			input: &LokiRequest{
				StartTs: time.Unix(2*3600, 0),
				EndTs:   time.Unix(3*3*3600, 0),
				Step:    15 * seconds,
				Query:   `rate({app="foo"}[6h])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(2*3600, 0),
					EndTs:   time.Unix((6*3600)-15, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[6h])`,
				},
				&LokiRequest{
					StartTs: time.Unix(6*3600, 0),
					EndTs:   time.Unix(3*3*3600, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[6h])`,
				},
			},
			interval: 1 * time.Hour,
		},
		// range vector too large we don't want to split it
		{
			input: &LokiRequest{
				StartTs: time.Unix(2*3600, 0),
				EndTs:   time.Unix(3*3*3600, 0),
				Step:    15 * seconds,
				Query:   `rate({app="foo"}[7d])`,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(2*3600, 0),
					EndTs:   time.Unix(3*3*3600, 0),
					Step:    15 * seconds,
					Query:   `rate({app="foo"}[7d])`,
				},
			},
			interval: 15 * time.Minute,
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			splits, err := splitMetricByTime(tc.input, tc.interval)
			require.NoError(t, err)
			for i, s := range splits {
				s := s.(*LokiRequest)
				t.Logf(" want: %d start:%s end:%s \n", i, s.StartTs, s.EndTs)
			}
			require.Equal(t, tc.expected, splits)
		})

	}
}

func Test_splitByInterval_Do(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	next := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		return &LokiResponse{
			Status:    loghttp.QueryStatusSuccess,
			Direction: r.(*LokiRequest).Direction,
			Limit:     r.(*LokiRequest).Limit,
			Version:   uint32(loghttp.VersionV1),
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result: []logproto.Stream{
					{
						Labels: `{foo="bar", level="debug"}`,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(0, r.(*LokiRequest).StartTs.UnixNano()), Line: fmt.Sprintf("%d", r.(*LokiRequest).StartTs.UnixNano())},
						},
					},
				},
			},
		}, nil
	})

	l := WithSplitByLimits(fakeLimits{maxQueryParallelism: 1}, time.Hour)
	split := SplitByIntervalMiddleware(
		testSchemas,
		l,
		DefaultCodec,
		splitByTime,
		nilMetrics,
		false,
	).Wrap(next)

	tests := []struct {
		name string
		req  *LokiRequest
		want *LokiResponse
	}{
		{
			"backward",
			&LokiRequest{
				StartTs:   time.Unix(0, 0),
				EndTs:     time.Unix(0, (4 * time.Hour).Nanoseconds()),
				Query:     "",
				Limit:     1000,
				Step:      1,
				Direction: logproto.BACKWARD,
				Path:      "/api/prom/query_range",
			},
			&LokiResponse{
				Status:     loghttp.QueryStatusSuccess,
				Direction:  logproto.BACKWARD,
				Limit:      1000,
				Version:    1,
				Statistics: stats.Result{Summary: stats.Summary{Splits: 4}},
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: []logproto.Stream{
						{
							Labels: `{foo="bar", level="debug"}`,
							Entries: []logproto.Entry{
								{Timestamp: time.Unix(0, 3*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 3*time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, 2*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 2*time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, 0), Line: fmt.Sprintf("%d", 0)},
							},
						},
					},
				},
			},
		},
		{
			"forward",
			&LokiRequest{
				StartTs:   time.Unix(0, 0),
				EndTs:     time.Unix(0, (4 * time.Hour).Nanoseconds()),
				Query:     "",
				Limit:     1000,
				Step:      1,
				Direction: logproto.FORWARD,
				Path:      "/api/prom/query_range",
			},
			&LokiResponse{
				Status:     loghttp.QueryStatusSuccess,
				Direction:  logproto.FORWARD,
				Statistics: stats.Result{Summary: stats.Summary{Splits: 4}},
				Limit:      1000,
				Version:    1,
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: []logproto.Stream{
						{
							Labels: `{foo="bar", level="debug"}`,
							Entries: []logproto.Entry{
								{Timestamp: time.Unix(0, 0), Line: fmt.Sprintf("%d", 0)},
								{Timestamp: time.Unix(0, time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, 2*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 2*time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, 3*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 3*time.Hour.Nanoseconds())},
							},
						},
					},
				},
			},
		},
		{
			"forward limited",
			&LokiRequest{
				StartTs:   time.Unix(0, 0),
				EndTs:     time.Unix(0, (4 * time.Hour).Nanoseconds()),
				Query:     "",
				Limit:     2,
				Step:      1,
				Direction: logproto.FORWARD,
				Path:      "/api/prom/query_range",
			},
			&LokiResponse{
				Status:     loghttp.QueryStatusSuccess,
				Direction:  logproto.FORWARD,
				Limit:      2,
				Version:    1,
				Statistics: stats.Result{Summary: stats.Summary{Splits: 2}},
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: []logproto.Stream{
						{
							Labels: `{foo="bar", level="debug"}`,
							Entries: []logproto.Entry{
								{Timestamp: time.Unix(0, 0), Line: fmt.Sprintf("%d", 0)},
								{Timestamp: time.Unix(0, time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", time.Hour.Nanoseconds())},
							},
						},
					},
				},
			},
		},
		{
			"backward limited",
			&LokiRequest{
				StartTs:   time.Unix(0, 0),
				EndTs:     time.Unix(0, (4 * time.Hour).Nanoseconds()),
				Query:     "",
				Limit:     2,
				Step:      1,
				Direction: logproto.BACKWARD,
				Path:      "/api/prom/query_range",
			},
			&LokiResponse{
				Status:     loghttp.QueryStatusSuccess,
				Direction:  logproto.BACKWARD,
				Limit:      2,
				Version:    1,
				Statistics: stats.Result{Summary: stats.Summary{Splits: 2}},
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: []logproto.Stream{
						{
							Labels: `{foo="bar", level="debug"}`,
							Entries: []logproto.Entry{
								{Timestamp: time.Unix(0, 3*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 3*time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, 2*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 2*time.Hour.Nanoseconds())},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := split.Do(ctx, tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.want, res)
		})
	}
}

func Test_series_splitByInterval_Do(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	next := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		return &LokiSeriesResponse{
			Status:  "success",
			Version: uint32(loghttp.VersionV1),
			Data: []logproto.SeriesIdentifier{
				{
					Labels: map[string]string{"filename": "/var/hostlog/apport.log", "job": "varlogs"},
				},
				{
					Labels: map[string]string{"filename": "/var/hostlog/test.log", "job": "varlogs"},
				},
				{
					Labels: map[string]string{"filename": "/var/hostlog/test.log", "job": "varlogs"},
				},
			},
		}, nil
	})

	l := WithSplitByLimits(fakeLimits{maxQueryParallelism: 1}, time.Hour)
	split := SplitByIntervalMiddleware(
		testSchemas,
		l,
		DefaultCodec,
		splitByTime,
		nilMetrics,
		false,
	).Wrap(next)

	tests := []struct {
		name string
		req  *LokiSeriesRequest
		want *LokiSeriesResponse
	}{
		{
			"backward",
			&LokiSeriesRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(0, (4 * time.Hour).Nanoseconds()),
				Match:   []string{`{job="varlogs"}`},
				Path:    "/loki/api/v1/series",
			},
			&LokiSeriesResponse{
				Statistics: stats.Result{Summary: stats.Summary{Splits: 4}},
				Status:     "success",
				Version:    1,
				Data: []logproto.SeriesIdentifier{
					{
						Labels: map[string]string{"filename": "/var/hostlog/apport.log", "job": "varlogs"},
					},
					{
						Labels: map[string]string{"filename": "/var/hostlog/test.log", "job": "varlogs"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := split.Do(ctx, tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.want, res)
		})
	}
}

func Test_seriesvolume_splitByInterval_Do(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	setup := func(next queryrangebase.Handler, l Limits) queryrangebase.Handler {
		return SplitByIntervalMiddleware(
			testSchemas,
			l,
			DefaultCodec,
			splitByTime,
			nilMetrics,
			false,
		).Wrap(next)
	}

	t.Run("volumes", func(t *testing.T) {
		from := model.TimeFromUnixNano(start.UnixNano())
		through := model.TimeFromUnixNano(end.UnixNano())

		next := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			return &VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `{foo="bar"}`, Volume: 38},
						{Name: `{bar="baz"}`, Volume: 28},
					},
					Limit: 2},
				Headers: nil,
			}, nil
		})

		l := WithSplitByLimits(fakeLimits{maxQueryParallelism: 1}, time.Hour)
		split := setup(next, l)
		req := &logproto.VolumeRequest{
			From:     from,
			Through:  through,
			Matchers: "{}",
			Limit:    2,
		}

		res, err := split.Do(ctx, req)
		require.NoError(t, err)

		response := res.(*VolumeResponse)
		require.Len(t, response.Response.Volumes, 2)
		require.Equal(t, response.Response, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{Name: "{foo=\"bar\"}", Volume: 76},
				{Name: "{bar=\"baz\"}", Volume: 56},
			},
			Limit: 2,
		})
	})

	t.Run("volumes with limits", func(t *testing.T) {
		from := model.TimeFromUnixNano(start.UnixNano())
		through := model.TimeFromUnixNano(end.UnixNano())
		next := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			return &VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `{foo="bar"}`, Volume: 38},
						{Name: `{bar="baz"}`, Volume: 28},
						{Name: `{foo="bar"}`, Volume: 38},
						{Name: `{fizz="buzz"}`, Volume: 28},
					},
					Limit: 1},
				Headers: nil,
			}, nil
		})

		l := WithSplitByLimits(fakeLimits{maxQueryParallelism: 1}, time.Hour)
		split := setup(next, l)
		req := &logproto.VolumeRequest{
			From:     from,
			Through:  through,
			Matchers: "{}",
			Limit:    1,
		}

		res, err := split.Do(ctx, req)
		require.NoError(t, err)

		response := res.(*VolumeResponse)
		require.Len(t, response.Response.Volumes, 1)
		require.Equal(t, response.Response, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{Name: "{foo=\"bar\"}", Volume: 152},
			},
			Limit: 1,
		})
	})

	// This will never happen because we hardcode 24h spit by for this code path
	// in the middleware. However, that split by is not validated here, so we either
	// need to support this case or error.
	t.Run("volumes with a query split by of 0", func(t *testing.T) {
		from := model.TimeFromUnixNano(start.UnixNano())
		through := model.TimeFromUnixNano(end.UnixNano())
		next := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			return &VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `{foo="bar"}`, Volume: 38},
						{Name: `{bar="baz"}`, Volume: 28},
					},
					Limit: 2},
				Headers: nil,
			}, nil
		})

		l := WithSplitByLimits(fakeLimits{maxQueryParallelism: 1}, 0)
		split := setup(next, l)
		req := &logproto.VolumeRequest{
			From:     from,
			Through:  through,
			Matchers: "{}",
			Limit:    2,
		}

		res, err := split.Do(ctx, req)
		require.NoError(t, err)

		response := res.(*VolumeResponse)

		require.Len(t, response.Response.Volumes, 2)
		require.Contains(t, response.Response.Volumes, logproto.Volume{Name: `{foo="bar"}`, Volume: 38})
		require.Contains(t, response.Response.Volumes, logproto.Volume{Name: `{bar="baz"}`, Volume: 28})
	})
}

func Test_ExitEarly(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")

	var callCt int
	var mtx sync.Mutex

	next := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(time.Millisecond) // artificial delay to minimize race condition exposure in test

		mtx.Lock()
		defer mtx.Unlock()
		callCt++

		return &LokiResponse{
			Status:    loghttp.QueryStatusSuccess,
			Direction: r.(*LokiRequest).Direction,
			Limit:     r.(*LokiRequest).Limit,
			Version:   uint32(loghttp.VersionV1),
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result: []logproto.Stream{
					{
						Labels: `{foo="bar", level="debug"}`,
						Entries: []logproto.Entry{
							{
								Timestamp: time.Unix(0, r.(*LokiRequest).StartTs.UnixNano()),
								Line:      fmt.Sprintf("%d", r.(*LokiRequest).StartTs.UnixNano()),
							},
						},
					},
				},
			},
		}, nil
	})

	l := WithSplitByLimits(fakeLimits{maxQueryParallelism: 1}, time.Hour)
	split := SplitByIntervalMiddleware(
		testSchemas,
		l,
		DefaultCodec,
		splitByTime,
		nilMetrics,
		false,
	).Wrap(next)

	req := &LokiRequest{
		StartTs:   time.Unix(0, 0),
		EndTs:     time.Unix(0, (4 * time.Hour).Nanoseconds()),
		Query:     "",
		Limit:     2,
		Step:      1,
		Direction: logproto.FORWARD,
		Path:      "/api/prom/query_range",
	}

	expected := &LokiResponse{
		Status:    loghttp.QueryStatusSuccess,
		Direction: logproto.FORWARD,
		Limit:     2,
		Version:   1,
		Statistics: stats.Result{
			Summary: stats.Summary{
				Splits: 2,
			},
		},
		Data: LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result: []logproto.Stream{
				{
					Labels: `{foo="bar", level="debug"}`,
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 0),
							Line:      fmt.Sprintf("%d", 0),
						},
						{
							Timestamp: time.Unix(0, time.Hour.Nanoseconds()),
							Line:      fmt.Sprintf("%d", time.Hour.Nanoseconds()),
						},
					},
				},
			},
		},
	}

	res, err := split.Do(ctx, req)

	require.Equal(t, int(req.Limit), callCt)
	require.NoError(t, err)
	require.Equal(t, expected, res)
}

func Test_DoesntDeadlock(t *testing.T) {
	n := 10

	next := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		return &LokiResponse{
			Status:    loghttp.QueryStatusSuccess,
			Direction: r.(*LokiRequest).Direction,
			Limit:     r.(*LokiRequest).Limit,
			Version:   uint32(loghttp.VersionV1),
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result: []logproto.Stream{
					{
						Labels: `{foo="bar", level="debug"}`,
						Entries: []logproto.Entry{
							{
								Timestamp: time.Unix(0, r.(*LokiRequest).StartTs.UnixNano()),
								Line:      fmt.Sprintf("%d", r.(*LokiRequest).StartTs.UnixNano()),
							},
						},
					},
				},
			},
		}, nil
	})

	l := WithSplitByLimits(fakeLimits{maxQueryParallelism: n}, time.Hour)
	split := SplitByIntervalMiddleware(
		testSchemas,
		l,
		DefaultCodec,
		splitByTime,
		nilMetrics,
		false,
	).Wrap(next)

	// split into n requests w/ n/2 limit, ensuring unused responses are cleaned up properly
	req := &LokiRequest{
		StartTs:   time.Unix(0, 0),
		EndTs:     time.Unix(0, (time.Duration(n) * time.Hour).Nanoseconds()),
		Query:     "",
		Limit:     uint32(n / 2),
		Step:      1,
		Direction: logproto.FORWARD,
		Path:      "/api/prom/query_range",
	}

	ctx := user.InjectOrgID(context.Background(), "1")

	startingGoroutines := runtime.NumGoroutine()

	// goroutines shouldn't blow up across 100 rounds
	for i := 0; i < 100; i++ {
		res, err := split.Do(ctx, req)
		require.NoError(t, err)
		require.Equal(t, 1, len(res.(*LokiResponse).Data.Result))
		require.Equal(t, n/2, len(res.(*LokiResponse).Data.Result[0].Entries))

	}
	runtime.GC()
	endingGoroutines := runtime.NumGoroutine()

	// give runtime a bit of slack when catching up -- this isn't an exact science :(
	// Allow for 1% increase in goroutines
	require.LessOrEqual(t, endingGoroutines, startingGoroutines*101/100)
}
