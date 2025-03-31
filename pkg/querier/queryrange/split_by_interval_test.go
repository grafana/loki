package queryrange

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/util"
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

var (
	// 62697274686461792063616b65
	refTime  = time.Date(2023, 1, 15, 8, 5, 30, 123456789, time.UTC)
	tenantID = "1"
)

func Test_splitQuery(t *testing.T) {
	type interval struct {
		start, end time.Time
	}

	for requestType, tc := range map[string]struct {
		requestBuilderFunc func(start, end time.Time) queryrangebase.Request
		endTimeInclusive   bool
	}{
		"logs request": {
			requestBuilderFunc: func(start, end time.Time) queryrangebase.Request {
				return &LokiRequest{
					Query:     `{app="foo"}`,
					Limit:     1,
					Step:      2,
					StartTs:   start,
					EndTs:     end,
					Direction: logproto.BACKWARD,
					Path:      "/query",
					Plan: &plan.QueryPlan{
						AST: syntax.MustParseExpr(`{app="foo"}`),
					},
				}
			},
		},
		"logs request with interval": {
			requestBuilderFunc: func(start, end time.Time) queryrangebase.Request {
				return &LokiRequest{
					Query:     `{app="foo"}`,
					Limit:     1,
					Interval:  2,
					StartTs:   start,
					EndTs:     end,
					Direction: logproto.BACKWARD,
					Path:      "/query",
					Plan: &plan.QueryPlan{
						AST: syntax.MustParseExpr(`{app="foo"}`),
					},
				}
			},
		},
		"series request": {
			requestBuilderFunc: func(start, end time.Time) queryrangebase.Request {
				return &LokiSeriesRequest{
					Match:   []string{"match1"},
					StartTs: start,
					EndTs:   end,
					Path:    "/series",
					Shards:  []string{"shard1"},
				}
			},
			endTimeInclusive: true,
		},
		"label names request": {
			requestBuilderFunc: func(start, end time.Time) queryrangebase.Request {
				return NewLabelRequest(start, end, `{foo="bar"}`, "", "/labels")
			},
			endTimeInclusive: true,
		},
		"label values request": {
			requestBuilderFunc: func(start, end time.Time) queryrangebase.Request {
				return NewLabelRequest(start, end, `{foo="bar"}`, "test", "/label/test/values")
			},
			endTimeInclusive: true,
		},
		"index stats request": {
			requestBuilderFunc: func(start, end time.Time) queryrangebase.Request {
				return &logproto.IndexStatsRequest{
					From:     model.TimeFromUnix(start.Unix()),
					Through:  model.TimeFromUnix(end.Unix()),
					Matchers: `{host="agent"}`,
				}
			},
			endTimeInclusive: true,
		},
		"volume request": {
			requestBuilderFunc: func(start, end time.Time) queryrangebase.Request {
				return &logproto.VolumeRequest{
					From:        model.TimeFromUnix(start.Unix()),
					Through:     model.TimeFromUnix(end.Unix()),
					Matchers:    `{host="agent"}`,
					Limit:       5,
					AggregateBy: seriesvolume.Series,
				}
			},
			endTimeInclusive: true,
		},
	} {
		expectedSplitGap := time.Duration(0)
		if tc.endTimeInclusive {
			expectedSplitGap = util.SplitGap
		}

		t.Run(requestType, func(t *testing.T) {
			for name, intervals := range map[string]struct {
				input                            interval
				expected                         []interval
				expectedWithoutIngesterSplits    []interval
				splitInterval                    time.Duration
				splitter                         splitter
				recentMetadataQueryWindowEnabled bool
			}{
				"no change": {
					input: interval{
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
				"align start": {
					input: interval{
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
				"align end": {
					input: interval{
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
				"align both": {
					input: interval{
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
				"no align": {
					input: interval{
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
				"wholly within ingester query window": {
					input: interval{
						start: refTime.Add(-time.Hour).Truncate(time.Second),
						end:   refTime,
					},
					expected: []interval{
						{
							start: refTime.Add(-time.Hour).Truncate(time.Second),
							end:   time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC),
							end:   refTime,
						},
					},
					expectedWithoutIngesterSplits: []interval{
						{
							start: refTime.Add(-time.Hour).Truncate(time.Second),
							end:   time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC),
							end:   refTime,
						},
					},
					splitInterval: time.Hour,
					splitter: newDefaultSplitter(
						fakeLimits{ingesterSplitDuration: map[string]time.Duration{tenantID: 90 * time.Minute}},
						ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour},
					),
				},
				"partially within ingester query window": {
					input: interval{
						// overlapping `query_ingesters_within` window of 3h
						start: refTime.Add(-4 * time.Hour).Add(-30 * time.Minute).Truncate(time.Second),
						end:   refTime,
					},
					expected: []interval{
						// regular intervals until `query_ingesters_within` window
						{
							start: refTime.Add(-4 * time.Hour).Add(-30 * time.Minute).Truncate(time.Second),
							end:   time.Date(2023, 1, 15, 4, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 4, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 5, 5, 30, 123456789, time.UTC).Add(-expectedSplitGap),
						},
						// and then different intervals for queries to ingesters
						{
							start: time.Date(2023, 1, 15, 5, 5, 30, 123456789, time.UTC),
							end:   time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC),
							end:   refTime,
						},
					},
					expectedWithoutIngesterSplits: []interval{
						{
							start: refTime.Add(-4 * time.Hour).Add(-30 * time.Minute).Truncate(time.Second),
							end:   time.Date(2023, 1, 15, 4, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 4, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC),
							end:   refTime,
						},
					},
					splitInterval: time.Hour,
					splitter: newDefaultSplitter(
						fakeLimits{ingesterSplitDuration: map[string]time.Duration{tenantID: 90 * time.Minute}},
						ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour},
					),
				},
				"not within ingester query window": {
					input: interval{
						// outside `query_ingesters_within` range of 3h
						start: refTime.Add(-5 * time.Hour).Truncate(time.Second),
						end:   refTime.Add(-4 * time.Hour).Truncate(time.Second),
					},
					expected: []interval{
						// regular intervals outside `query_ingesters_within` window
						{
							start: refTime.Add(-5 * time.Hour).Truncate(time.Second),
							end:   time.Date(2023, 1, 15, 4, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 4, 0, 0, 0, time.UTC),
							end:   refTime.Add(-4 * time.Hour).Truncate(time.Second),
						},
					},
					splitInterval: time.Hour,
					splitter: newDefaultSplitter(
						fakeLimits{ingesterSplitDuration: map[string]time.Duration{tenantID: 90 * time.Minute}},
						ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour},
					),
				},
				"ingester query split by disabled": {
					input: interval{
						// overlapping `query_ingesters_within` range of 3h
						start: refTime.Add(-4 * time.Hour).Truncate(time.Second),
						end:   refTime,
					},
					expected: []interval{
						// regular intervals only, since ingester split duration is 0
						{
							start: refTime.Add(-4 * time.Hour).Truncate(time.Second),
							end:   time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC),
							end:   refTime,
						},
					},
					splitInterval: time.Hour,
					splitter: newDefaultSplitter(
						fakeLimits{ingesterSplitDuration: map[string]time.Duration{tenantID: 0}},
						ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour},
					),
				},
				"ingester query split enabled but query_store_only enabled too": {
					input: interval{
						// overlapping `query_ingesters_within` range of 3h
						start: refTime.Add(-4 * time.Hour).Truncate(time.Second),
						end:   refTime,
					},
					expected: []interval{
						// regular intervals only, since ingester split duration is 0
						{
							start: refTime.Add(-4 * time.Hour).Truncate(time.Second),
							end:   time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC),
							end:   refTime,
						},
					},
					splitInterval: time.Hour,
					splitter: newDefaultSplitter(
						fakeLimits{ingesterSplitDuration: map[string]time.Duration{tenantID: 90 * time.Minute}},
						ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour, queryStoreOnly: true},
					),
				},
				"metadata recent query window should not affect other query types": {
					input: interval{
						start: refTime.Add(-4 * time.Hour).Truncate(time.Second),
						end:   refTime,
					},
					expected: []interval{
						{
							start: refTime.Add(-4 * time.Hour).Truncate(time.Second),
							end:   time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC),
							end:   refTime,
						},
					},
					splitInterval: time.Hour,
					splitter: newDefaultSplitter(
						fakeLimits{
							recentMetadataSplitDuration: map[string]time.Duration{tenantID: 30 * time.Minute},
							recentMetadataQueryWindow:   map[string]time.Duration{tenantID: 3 * time.Hour},
						}, nil,
					),
					recentMetadataQueryWindowEnabled: true,
				},
			} {
				t.Run(name, func(t *testing.T) {
					req := tc.requestBuilderFunc(intervals.input.start, intervals.input.end)
					var want []queryrangebase.Request

					// ingester splits do not apply for metadata queries
					var expected []interval
					switch req.(type) {
					case *LabelRequest, *LokiSeriesRequest:
						expected = intervals.expectedWithoutIngesterSplits

						if intervals.recentMetadataQueryWindowEnabled {
							t.Skip("this flow is tested in Test_splitRecentMetadataQuery")
						}
					}

					if expected == nil {
						expected = intervals.expected
					}

					for _, exp := range expected {
						want = append(want, tc.requestBuilderFunc(exp.start, exp.end))
					}

					if intervals.splitInterval == 0 {
						intervals.splitInterval = time.Hour
					}

					if intervals.splitter == nil {
						intervals.splitter = newDefaultSplitter(fakeLimits{}, nil)
					}

					splits := intervals.splitter.split(refTime, []string{tenantID}, req, intervals.splitInterval)
					assertSplits(t, want, splits)
				})
			}
		})
	}
}

func Test_splitRecentMetadataQuery(t *testing.T) {
	type interval struct {
		start, end time.Time
	}

	expectedSplitGap := util.SplitGap

	for requestType, tc := range map[string]struct {
		requestBuilderFunc func(start, end time.Time) queryrangebase.Request
	}{
		"series request": {
			requestBuilderFunc: func(start, end time.Time) queryrangebase.Request {
				return &LokiSeriesRequest{
					Match:   []string{"match1"},
					StartTs: start,
					EndTs:   end,
					Path:    "/series",
					Shards:  []string{"shard1"},
				}
			},
		},
		"label names request": {
			requestBuilderFunc: func(start, end time.Time) queryrangebase.Request {
				return NewLabelRequest(start, end, `{foo="bar"}`, "", "/labels")
			},
		},
		"label values request": {
			requestBuilderFunc: func(start, end time.Time) queryrangebase.Request {
				return NewLabelRequest(start, end, `{foo="bar"}`, "test", "/label/test/values")
			},
		},
	} {
		t.Run(requestType, func(t *testing.T) {
			for name, intervals := range map[string]struct {
				input         interval
				expected      []interval
				splitInterval time.Duration
				splitter      splitter
			}{
				"wholly within recent metadata query window": {
					input: interval{
						start: refTime.Add(-time.Hour),
						end:   refTime,
					},
					expected: []interval{
						{
							start: refTime.Add(-time.Hour),
							end:   time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC),
							end:   refTime,
						},
					},
					splitInterval: time.Hour,
					splitter: newDefaultSplitter(
						fakeLimits{
							recentMetadataSplitDuration: map[string]time.Duration{tenantID: 30 * time.Minute},
							recentMetadataQueryWindow:   map[string]time.Duration{tenantID: 2 * time.Hour},
						}, nil,
					),
				},
				"start aligns with recent metadata query window": {
					input: interval{
						start: refTime.Add(-1 * time.Hour),
						end:   refTime,
					},
					expected: []interval{
						{
							start: refTime.Add(-time.Hour),
							end:   time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC),
							end:   refTime,
						},
					},
					splitInterval: time.Hour,
					splitter: newDefaultSplitter(
						fakeLimits{
							recentMetadataSplitDuration: map[string]time.Duration{tenantID: 30 * time.Minute},
							recentMetadataQueryWindow:   map[string]time.Duration{tenantID: 1 * time.Hour},
						}, nil,
					),
				},
				"partially within recent metadata query window": {
					input: interval{
						start: refTime.Add(-3 * time.Hour),
						end:   refTime,
					},
					expected: []interval{
						{
							start: refTime.Add(-3 * time.Hour),
							end:   time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC),
							end:   refTime.Add(-time.Hour).Add(-expectedSplitGap),
						},
						// apply split_recent_metadata_queries_by_interval for recent metadata queries
						{
							start: refTime.Add(-time.Hour),
							end:   time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC),
							end:   refTime,
						},
					},
					splitInterval: time.Hour,
					splitter: newDefaultSplitter(
						fakeLimits{
							recentMetadataSplitDuration: map[string]time.Duration{tenantID: 30 * time.Minute},
							recentMetadataQueryWindow:   map[string]time.Duration{tenantID: 1 * time.Hour},
						}, nil,
					),
				},
				"outside recent metadata query window": {
					input: interval{
						start: refTime.Add(-4 * time.Hour),
						end:   refTime.Add(-2 * time.Hour),
					},
					expected: []interval{
						{
							start: refTime.Add(-4 * time.Hour),
							end:   time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
							end:   refTime.Add(-2 * time.Hour),
						},
					},
					splitInterval: time.Hour,
					splitter: newDefaultSplitter(
						fakeLimits{
							recentMetadataSplitDuration: map[string]time.Duration{tenantID: 30 * time.Minute},
							recentMetadataQueryWindow:   map[string]time.Duration{tenantID: 1 * time.Hour},
						}, nil,
					),
				},
				"end aligns with recent metadata query window": {
					input: interval{
						start: refTime.Add(-3 * time.Hour),
						end:   refTime.Add(-1 * time.Hour),
					},
					expected: []interval{
						{
							start: refTime.Add(-3 * time.Hour),
							end:   time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC),
							end:   refTime.Add(-1 * time.Hour),
						},
					},
					splitInterval: time.Hour,
					splitter: newDefaultSplitter(
						fakeLimits{
							recentMetadataSplitDuration: map[string]time.Duration{tenantID: 30 * time.Minute},
							recentMetadataQueryWindow:   map[string]time.Duration{tenantID: 1 * time.Hour},
						}, nil,
					),
				},
				"recent metadata window not configured": {
					input: interval{
						start: refTime.Add(-3 * time.Hour),
						end:   refTime,
					},
					expected: []interval{
						{
							start: refTime.Add(-3 * time.Hour),
							end:   time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC),
							end:   time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC).Add(-expectedSplitGap),
						},
						{
							start: time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC),
							end:   refTime,
						},
					},
					splitInterval: time.Hour,
				},
			} {
				t.Run(name, func(t *testing.T) {
					req := tc.requestBuilderFunc(intervals.input.start, intervals.input.end)
					var want []queryrangebase.Request

					for _, exp := range intervals.expected {
						want = append(want, tc.requestBuilderFunc(exp.start, exp.end))
					}

					if intervals.splitInterval == 0 {
						intervals.splitInterval = time.Hour
					}

					if intervals.splitter == nil {
						intervals.splitter = newDefaultSplitter(fakeLimits{}, nil)
					}

					splits := intervals.splitter.split(refTime, []string{tenantID}, req, intervals.splitInterval)
					assertSplits(t, want, splits)
				})
			}
		})
	}
}

func Test_splitMetricQuery(t *testing.T) {
	const seconds = 1e3 // 1e3 milliseconds per second.

	const shortRange = `rate({app="foo"}[1m])`
	const longRange = `rate({app="foo"}[7d])`

	for i, tc := range []struct {
		input         *LokiRequest
		expected      []queryrangebase.Request
		splitInterval time.Duration
		splitter      splitter
	}{
		// the step is lower than the interval therefore we should split only once.
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(0, 60*time.Minute.Nanoseconds()),
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(0, 60*time.Minute.Nanoseconds()),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 24 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(60*60, 0),
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(60*60, 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 3 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(24*3600, 0),
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(24*3600, 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 24 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(3*3600, 0),
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(3*3600, 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 3 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(2*24*3600, 0),
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix((24*3600)-15, 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix((24 * 3600), 0),
					EndTs:   time.Unix((2 * 24 * 3600), 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 24 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(2*3*3600, 0),
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix((3*3600)-15, 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix((3 * 3600), 0),
					EndTs:   time.Unix((2 * 3 * 3600), 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 3 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(3*3600, 0),
				EndTs:   time.Unix(3*24*3600, 0),
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(3*3600, 0),
					EndTs:   time.Unix((24*3600)-15, 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix(24*3600, 0),
					EndTs:   time.Unix((2*24*3600)-15, 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix(2*24*3600, 0),
					EndTs:   time.Unix(3*24*3600, 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 24 * time.Hour,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(2*3600, 0),
				EndTs:   time.Unix(3*3*3600, 0),
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(2*3600, 0),
					EndTs:   time.Unix((3*3600)-15, 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix(3*3600, 0),
					EndTs:   time.Unix((2*3*3600)-15, 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix(2*3*3600, 0),
					EndTs:   time.Unix(3*3*3600, 0),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 3 * time.Hour,
		},

		// step not a multiple of interval
		// start time already step aligned
		{
			input: &LokiRequest{
				StartTs: time.Unix(2*3600-9, 0), // 2h mod 17s = 9s
				EndTs:   time.Unix(3*3*3600, 0),
				Step:    17 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(2*3600-9, 0),
					EndTs:   time.Unix((3*3600)-5, 0), // 3h mod 17s = 5s
					Step:    17 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix((3*3600)+12, 0),
					EndTs:   time.Unix((2*3*3600)-10, 0), // 6h mod 17s = 10s
					Step:    17 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix(2*3*3600+7, 0),
					EndTs:   time.Unix(3*3*3600+2, 0), // 9h mod 17s = 2s
					Step:    17 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 3 * time.Hour,
		},
		// end time already step aligned
		{
			input: &LokiRequest{
				StartTs: time.Unix(2*3600, 0),
				EndTs:   time.Unix(3*3*3600+2, 0), // 9h mod 17s = 2s
				Step:    17 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(2*3600-9, 0),   // 2h mod 17s = 9s
					EndTs:   time.Unix((3*3600)-5, 0), // 3h mod 17s = 5s
					Step:    17 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix((3*3600)+12, 0),
					EndTs:   time.Unix((2*3*3600)-10, 0), // 6h mod 17s = 10s
					Step:    17 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix(2*3*3600+7, 0),
					EndTs:   time.Unix(3*3*3600+2, 0),
					Step:    17 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 3 * time.Hour,
		},
		// start & end time not aligned with step
		{
			input: &LokiRequest{
				StartTs: time.Unix(2*3600, 0),
				EndTs:   time.Unix(3*3*3600, 0),
				Step:    17 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(2*3600-9, 0),   // 2h mod 17s = 9s
					EndTs:   time.Unix((3*3600)-5, 0), // 3h mod 17s = 5s
					Step:    17 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix((3*3600)+12, 0),
					EndTs:   time.Unix((2*3*3600)-10, 0), // 6h mod 17s = 10s
					Step:    17 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix(2*3*3600+7, 0),
					EndTs:   time.Unix(3*3*3600+2, 0), // 9h mod 17s = 2s
					Step:    17 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 3 * time.Hour,
		},

		// step larger than split interval
		{
			input: &LokiRequest{
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(25*3600, 0),
				Step:    6 * 3600 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(6*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix(6*3600, 0),
					EndTs:   time.Unix(12*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix(12*3600, 0),
					EndTs:   time.Unix(18*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix(18*3600, 0),
					EndTs:   time.Unix(24*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Unix(24*3600, 0),
					EndTs:   time.Unix(30*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 15 * time.Minute,
		},
		{
			input: &LokiRequest{
				StartTs: time.Unix(1*3600, 0),
				EndTs:   time.Unix(3*3600, 0),
				Step:    6 * 3600 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(0, 0),
					EndTs:   time.Unix(6*3600, 0),
					Step:    6 * 3600 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: 15 * time.Minute,
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
			splitInterval: 1 * time.Hour,
		},
		// range vector too large we don't want to split it
		{
			input: &LokiRequest{
				StartTs: time.Unix(2*3600, 0),
				EndTs:   time.Unix(3*3*3600, 0),
				Step:    15 * seconds,
				Query:   longRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Unix(2*3600, 0),
					EndTs:   time.Unix(3*3*3600, 0),
					Step:    15 * seconds,
					Query:   longRange,
				},
			},
			splitInterval: 15 * time.Minute,
		},
		// query is wholly within ingester query window
		{
			input: &LokiRequest{
				StartTs: refTime.Add(-time.Hour),
				EndTs:   refTime,
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 7, 0o5, 30, 0, time.UTC), // start time is aligned down to step of 15s
					EndTs:   time.Date(2023, 1, 15, 7, 29, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 8, 5, 45, 0, time.UTC), // end time is aligned up to step of 15s
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: time.Hour,
			splitter: newMetricQuerySplitter(
				fakeLimits{ingesterSplitDuration: map[string]time.Duration{tenantID: 90 * time.Minute}},
				ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour},
			),
		},
		// query is partially within ingester query window
		{
			input: &LokiRequest{
				StartTs: refTime.Add(-4 * time.Hour).Add(-30 * time.Minute),
				EndTs:   refTime,
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				// regular intervals until `query_ingesters_within` window
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 3, 35, 30, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 3, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 4, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 4, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 5, 5, 15, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				// and then different intervals for queries to ingesters
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 5, 5, 30, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 5, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 7, 29, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 7, 30, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 8, 5, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: time.Hour,
			splitter: newMetricQuerySplitter(
				fakeLimits{ingesterSplitDuration: map[string]time.Duration{tenantID: 90 * time.Minute}},
				ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour},
			),
		},
		// not within ingester query window
		{
			input: &LokiRequest{
				StartTs: refTime.Add(-5 * time.Hour),
				EndTs:   refTime.Add(-4 * time.Hour),
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				// regular intervals until `query_ingesters_within` window
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 3, 5, 30, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 3, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 4, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 4, 5, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: time.Hour,
			splitter: newMetricQuerySplitter(
				fakeLimits{ingesterSplitDuration: map[string]time.Duration{tenantID: 90 * time.Minute}},
				ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour},
			),
		},
		// ingester query split by disabled
		{
			input: &LokiRequest{
				StartTs: refTime.Add(-4 * time.Hour),
				EndTs:   refTime,
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				// regular intervals only, since ingester split duration is 0
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 4, 5, 30, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 4, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 5, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 6, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 7, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 8, 5, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: time.Hour,
			splitter: newMetricQuerySplitter(
				fakeLimits{ingesterSplitDuration: map[string]time.Duration{tenantID: 0}},
				ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour},
			),
		},
		// ingester query split by enabled, but query_store_only is enabled too
		{
			input: &LokiRequest{
				StartTs: refTime.Add(-4 * time.Hour),
				EndTs:   refTime,
				Step:    15 * seconds,
				Query:   shortRange,
			},
			expected: []queryrangebase.Request{
				// regular intervals only, since ingester split duration is 0
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 4, 5, 30, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 4, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 5, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 5, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 6, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 6, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 7, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 7, 59, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
				&LokiRequest{
					StartTs: time.Date(2023, 1, 15, 8, 0, 0, 0, time.UTC),
					EndTs:   time.Date(2023, 1, 15, 8, 5, 45, 0, time.UTC),
					Step:    15 * seconds,
					Query:   shortRange,
				},
			},
			splitInterval: time.Hour,
			splitter: newMetricQuerySplitter(
				fakeLimits{ingesterSplitDuration: map[string]time.Duration{tenantID: 90 * time.Minute}},
				ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour, queryStoreOnly: true},
			),
		},
	} {
		// Set query plans
		tc.input.Plan = &plan.QueryPlan{
			AST: syntax.MustParseExpr(tc.input.Query),
		}

		for _, e := range tc.expected {
			e.(*LokiRequest).Plan = &plan.QueryPlan{
				AST: syntax.MustParseExpr(e.GetQuery()),
			}
		}

		t.Run(strconv.Itoa(i), func(t *testing.T) {
			ms := newMetricQuerySplitter(fakeLimits{}, nil)
			if tc.splitter != nil {
				ms = tc.splitter.(*metricQuerySplitter)
			}

			splits := ms.split(refTime, []string{tenantID}, tc.input, tc.splitInterval)
			if !assert.Equal(t, tc.expected, splits) {
				t.Logf("expected and actual do not match\n")
				defer t.Fail()

				if len(tc.expected) != len(splits) {
					t.Logf("expected %d splits, got %d\n", len(tc.expected), len(splits))
					return
				}

				for j := 0; j < len(tc.expected); j++ {
					exp := tc.expected[j]
					act := splits[j]
					equal := assert.Equal(t, exp, act)
					t.Logf("\t#%d [matches: %v]: expected %q/%q got %q/%q\n", j, equal, exp.GetStart(), exp.GetEnd(), act.GetStart(), act.GetEnd())
				}
			}
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

	defSplitter := newDefaultSplitter(fakeLimits{}, nil)
	l := WithSplitByLimits(fakeLimits{maxQueryParallelism: 1}, time.Hour)
	split := SplitByIntervalMiddleware(
		testSchemas,
		l,
		DefaultCodec,
		defSplitter,
		nilMetrics,
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
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return &LokiSeriesResponse{
			Status:  "success",
			Version: uint32(loghttp.VersionV1),
			Data: []logproto.SeriesIdentifier{
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "filename", Value: "/var/hostlog/apport.log"},
						{Key: "job", Value: "varlogs"},
					},
				},
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "filename", Value: "/var/hostlog/test.log"},
						{Key: "job", Value: "varlogs"},
					},
				},
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "filename", Value: "/var/hostlog/test.log"},
						{Key: "job", Value: "varlogs"},
					},
				},
			},
		}, nil
	})

	l := fakeLimits{
		maxQueryParallelism: 1,
		metadataSplitDuration: map[string]time.Duration{
			"1": time.Hour,
		},
	}
	defSplitter := newDefaultSplitter(fakeLimits{}, nil)
	split := SplitByIntervalMiddleware(
		testSchemas,
		l,
		DefaultCodec,
		defSplitter,
		nilMetrics,
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
						Labels: []logproto.SeriesIdentifier_LabelsEntry{
							{Key: "filename", Value: "/var/hostlog/apport.log"},
							{Key: "job", Value: "varlogs"},
						},
					},
					{
						Labels: []logproto.SeriesIdentifier_LabelsEntry{
							{Key: "filename", Value: "/var/hostlog/test.log"},
							{Key: "job", Value: "varlogs"},
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

func Test_seriesvolume_splitByInterval_Do(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	defSplitter := newDefaultSplitter(fakeLimits{}, nil)
	setup := func(next queryrangebase.Handler, l Limits) queryrangebase.Handler {
		return SplitByIntervalMiddleware(
			testSchemas,
			l,
			DefaultCodec,
			defSplitter,
			nilMetrics,
		).Wrap(next)
	}

	t.Run("volumes", func(t *testing.T) {
		from := model.TimeFromUnixNano(start.UnixNano())
		through := model.TimeFromUnixNano(end.UnixNano())

		next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
			return &VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `{foo="bar"}`, Volume: 38},
						{Name: `{bar="baz"}`, Volume: 28},
					},
					Limit: 2,
				},
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
		next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
			return &VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `{foo="bar"}`, Volume: 38},
						{Name: `{bar="baz"}`, Volume: 28},
						{Name: `{foo="bar"}`, Volume: 38},
						{Name: `{fizz="buzz"}`, Volume: 28},
					},
					Limit: 1,
				},
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
		next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
			return &VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `{foo="bar"}`, Volume: 38},
						{Name: `{bar="baz"}`, Volume: 28},
					},
					Limit: 2,
				},
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
	defSplitter := newDefaultSplitter(fakeLimits{}, nil)
	split := SplitByIntervalMiddleware(
		testSchemas,
		l,
		DefaultCodec,
		defSplitter,
		nilMetrics,
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

	mtx.Lock()
	count := callCt
	mtx.Unlock()

	require.Equal(t, int(req.Limit), count)
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
	defSplitter := newDefaultSplitter(fakeLimits{}, nil)
	split := SplitByIntervalMiddleware(
		testSchemas,
		l,
		DefaultCodec,
		defSplitter,
		nilMetrics,
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

func assertSplits(t *testing.T, want, splits []queryrangebase.Request) {
	t.Helper()

	if !assert.Equal(t, want, splits) {
		t.Logf("expected and actual do not match\n")
		defer t.Fail()

		if len(want) != len(splits) {
			t.Logf("expected %d splits, got %d\n", len(want), len(splits))
			return
		}

		for j := 0; j < len(want); j++ {
			exp := want[j]
			act := splits[j]
			equal := assert.Equal(t, exp, act)
			t.Logf("\t#%d [matches: %v]: expected %q/%q got %q/%q\n", j, equal, exp.GetStart(), exp.GetEnd(), act.GetStart(), act.GetEnd())
		}
	}
}
