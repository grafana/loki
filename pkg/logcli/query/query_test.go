package query

import (
	"bytes"
	"context"
	"log"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/marshal"
)

func Test_commonLabels(t *testing.T) {
	type args struct {
		lss []loghttp.LabelSet
	}
	tests := []struct {
		name string
		args args
		want loghttp.LabelSet
	}{
		{
			"Extract common labels source > target",
			args{
				[]loghttp.LabelSet{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{bar="foo", foo="foo", baz="baz"}`)},
			},
			mustParseLabels(`{bar="foo"}`),
		},
		{
			"Extract common labels source > target",
			args{
				[]loghttp.LabelSet{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{bar="foo", foo="bar", baz="baz"}`)},
			},
			mustParseLabels(`{foo="bar", bar="foo"}`),
		},
		{
			"Extract common labels source < target",
			args{
				[]loghttp.LabelSet{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{bar="foo"}`)},
			},
			mustParseLabels(`{bar="foo"}`),
		},
		{
			"Extract common labels source < target no common",
			args{
				[]loghttp.LabelSet{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{fo="bar"}`)},
			},
			loghttp.LabelSet{},
		},
		{
			"Extract common labels source = target no common",
			args{
				[]loghttp.LabelSet{mustParseLabels(`{foo="bar"}`), mustParseLabels(`{fooo="bar"}`)},
			},
			loghttp.LabelSet{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var streams []loghttp.Stream

			for _, lss := range tt.args.lss {
				streams = append(streams, loghttp.Stream{
					Entries: nil,
					Labels:  lss,
				})
			}

			if got := commonLabels(streams); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("commonLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_subtract(t *testing.T) {
	type args struct {
		a loghttp.LabelSet
		b loghttp.LabelSet
	}
	tests := []struct {
		name string
		args args
		want loghttp.LabelSet
	}{
		{
			"Subtract labels source > target",
			args{
				mustParseLabels(`{foo="bar", bar="foo"}`),
				mustParseLabels(`{bar="foo", foo="foo", baz="baz"}`),
			},
			mustParseLabels(`{foo="bar"}`),
		},
		{
			"Subtract labels source < target",
			args{
				mustParseLabels(`{foo="bar", bar="foo"}`),
				mustParseLabels(`{bar="foo"}`),
			},
			mustParseLabels(`{foo="bar"}`),
		},
		{
			"Subtract labels source < target no sub",
			args{
				mustParseLabels(`{foo="bar", bar="foo"}`),
				mustParseLabels(`{fo="bar"}`),
			},
			mustParseLabels(`{bar="foo", foo="bar"}`),
		},
		{
			"Subtract labels source = target no sub",
			args{
				mustParseLabels(`{foo="bar"}`),
				mustParseLabels(`{fiz="buz"}`),
			},
			mustParseLabels(`{foo="bar"}`),
		},
		{
			"Subtract labels source > target no sub",
			args{
				mustParseLabels(`{foo="bar"}`),
				mustParseLabels(`{fiz="buz", foo="baz"}`),
			},
			mustParseLabels(`{foo="bar"}`),
		},
		{
			"Subtract labels source > target no sub",
			args{
				mustParseLabels(`{a="b", foo="bar", baz="baz", fizz="fizz"}`),
				mustParseLabels(`{foo="bar", baz="baz", buzz="buzz", fizz="fizz"}`),
			},
			mustParseLabels(`{a="b"}`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subtract(tt.args.a, tt.args.b); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subtract() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_batch(t *testing.T) {
	tests := []struct {
		name          string
		streams       []logproto.Stream
		start, end    time.Time
		limit, batch  int
		labelMatcher  string
		forward       bool
		expectedCalls int
		expected      []string
	}{
		{
			name: "super simple forward",
			streams: []logproto.Stream{
				logproto.Stream{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						logproto.Entry{Timestamp: time.Unix(1, 0), Line: "line1"},
						logproto.Entry{Timestamp: time.Unix(2, 0), Line: "line2"},
						logproto.Entry{Timestamp: time.Unix(3, 0), Line: "line3"}, // End timestmap is exclusive
					},
				},
			},
			start:         time.Unix(1, 0),
			end:           time.Unix(3, 0),
			limit:         10,
			batch:         10,
			labelMatcher:  "{test=\"simple\"}",
			forward:       true,
			expectedCalls: 2, // Client doesn't know if the server hit a limit or there were no results so we have to query until there is no results, in this case 2 calls
			expected: []string{
				"line1",
				"line2",
			},
		},
		{
			name: "super simple backward",
			streams: []logproto.Stream{
				logproto.Stream{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						logproto.Entry{Timestamp: time.Unix(1, 0), Line: "line1"},
						logproto.Entry{Timestamp: time.Unix(2, 0), Line: "line2"},
						logproto.Entry{Timestamp: time.Unix(3, 0), Line: "line3"}, // End timestmap is exclusive
					},
				},
			},
			start:         time.Unix(1, 0),
			end:           time.Unix(3, 0),
			limit:         10,
			batch:         10,
			labelMatcher:  "{test=\"simple\"}",
			forward:       false,
			expectedCalls: 2,
			expected: []string{
				"line2",
				"line1",
			},
		},
		{
			name: "single stream forward batch",
			streams: []logproto.Stream{
				logproto.Stream{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						logproto.Entry{Timestamp: time.Unix(1, 0), Line: "line1"},
						logproto.Entry{Timestamp: time.Unix(2, 0), Line: "line2"},
						logproto.Entry{Timestamp: time.Unix(3, 0), Line: "line3"},
						logproto.Entry{Timestamp: time.Unix(4, 0), Line: "line4"},
						logproto.Entry{Timestamp: time.Unix(5, 0), Line: "line5"},
						logproto.Entry{Timestamp: time.Unix(6, 0), Line: "line6"},
						logproto.Entry{Timestamp: time.Unix(7, 0), Line: "line7"},
						logproto.Entry{Timestamp: time.Unix(8, 0), Line: "line8"},
						logproto.Entry{Timestamp: time.Unix(9, 0), Line: "line9"},
						logproto.Entry{Timestamp: time.Unix(10, 0), Line: "line10"},
					},
				},
			},
			start:        time.Unix(1, 0),
			end:          time.Unix(11, 0),
			limit:        9,
			batch:        2,
			labelMatcher: "{test=\"simple\"}",
			forward:      true,
			// Our batchsize is 2 but each query will also return the overlapping last element from the
			// previous batch, as such we only get one item per call so we make a lot of calls
			// Call one:   line1 line2
			// Call two:   line2 line3
			// Call three: line3 line4
			// Call four:  line4 line5
			// Call five:  line5 line6
			// Call six:   line6 line7
			// Call seven: line7 line8
			// Call eight: line8 line9
			expectedCalls: 8,
			expected: []string{
				"line1", "line2", "line3", "line4", "line5", "line6", "line7", "line8", "line9",
			},
		},
		{
			name: "single stream backward batch",
			streams: []logproto.Stream{
				logproto.Stream{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						logproto.Entry{Timestamp: time.Unix(1, 0), Line: "line1"},
						logproto.Entry{Timestamp: time.Unix(2, 0), Line: "line2"},
						logproto.Entry{Timestamp: time.Unix(3, 0), Line: "line3"},
						logproto.Entry{Timestamp: time.Unix(4, 0), Line: "line4"},
						logproto.Entry{Timestamp: time.Unix(5, 0), Line: "line5"},
						logproto.Entry{Timestamp: time.Unix(6, 0), Line: "line6"},
						logproto.Entry{Timestamp: time.Unix(7, 0), Line: "line7"},
						logproto.Entry{Timestamp: time.Unix(8, 0), Line: "line8"},
						logproto.Entry{Timestamp: time.Unix(9, 0), Line: "line9"},
						logproto.Entry{Timestamp: time.Unix(10, 0), Line: "line10"},
					},
				},
			},
			start:         time.Unix(1, 0),
			end:           time.Unix(11, 0),
			limit:         9,
			batch:         2,
			labelMatcher:  "{test=\"simple\"}",
			forward:       false,
			expectedCalls: 8,
			expected: []string{
				"line10", "line9", "line8", "line7", "line6", "line5", "line4", "line3", "line2",
			},
		},
		{
			name: "two streams forward batch",
			streams: []logproto.Stream{
				logproto.Stream{
					Labels: "{test=\"one\"}",
					Entries: []logproto.Entry{
						logproto.Entry{Timestamp: time.Unix(1, 0), Line: "line1"},
						logproto.Entry{Timestamp: time.Unix(2, 0), Line: "line2"},
						logproto.Entry{Timestamp: time.Unix(3, 0), Line: "line3"},
						logproto.Entry{Timestamp: time.Unix(4, 0), Line: "line4"},
						logproto.Entry{Timestamp: time.Unix(5, 0), Line: "line5"},
						logproto.Entry{Timestamp: time.Unix(6, 0), Line: "line6"},
						logproto.Entry{Timestamp: time.Unix(7, 0), Line: "line7"},
						logproto.Entry{Timestamp: time.Unix(8, 0), Line: "line8"},
						logproto.Entry{Timestamp: time.Unix(9, 0), Line: "line9"},
						logproto.Entry{Timestamp: time.Unix(10, 0), Line: "line10"},
					},
				},
				logproto.Stream{
					Labels: "{test=\"two\"}",
					Entries: []logproto.Entry{
						logproto.Entry{Timestamp: time.Unix(1, 1000), Line: "s2line1"},
						logproto.Entry{Timestamp: time.Unix(2, 1000), Line: "s2line2"},
						logproto.Entry{Timestamp: time.Unix(3, 1000), Line: "s2line3"},
						logproto.Entry{Timestamp: time.Unix(4, 1000), Line: "s2line4"},
						logproto.Entry{Timestamp: time.Unix(5, 1000), Line: "s2line5"},
						logproto.Entry{Timestamp: time.Unix(6, 1000), Line: "s2line6"},
						logproto.Entry{Timestamp: time.Unix(7, 1000), Line: "s2line7"},
						logproto.Entry{Timestamp: time.Unix(8, 1000), Line: "s2line8"},
						logproto.Entry{Timestamp: time.Unix(9, 1000), Line: "s2line9"},
						logproto.Entry{Timestamp: time.Unix(10, 1000), Line: "s2line10"},
					},
				},
			},
			start:        time.Unix(1, 0),
			end:          time.Unix(11, 0),
			limit:        12,
			batch:        3,
			labelMatcher: "{test=~\"one|two\"}",
			forward:      true,
			// Six calls
			// 1 line1, s2line1, line2
			// 2 line2, s2line2, line3
			// 3 line3, s2line3, line4
			// 4 line4, s2line4, line5
			// 5 line5, s2line5, line6
			// 6 line6, s2line6
			expectedCalls: 6,
			expected: []string{
				"line1", "s2line1", "line2", "s2line2", "line3", "s2line3", "line4", "s2line4", "line5", "s2line5", "line6", "s2line6",
			},
		},
		{
			name: "two streams backward batch",
			streams: []logproto.Stream{
				logproto.Stream{
					Labels: "{test=\"one\"}",
					Entries: []logproto.Entry{
						logproto.Entry{Timestamp: time.Unix(1, 0), Line: "line1"},
						logproto.Entry{Timestamp: time.Unix(2, 0), Line: "line2"},
						logproto.Entry{Timestamp: time.Unix(3, 0), Line: "line3"},
						logproto.Entry{Timestamp: time.Unix(4, 0), Line: "line4"},
						logproto.Entry{Timestamp: time.Unix(5, 0), Line: "line5"},
						logproto.Entry{Timestamp: time.Unix(6, 0), Line: "line6"},
						logproto.Entry{Timestamp: time.Unix(7, 0), Line: "line7"},
						logproto.Entry{Timestamp: time.Unix(8, 0), Line: "line8"},
						logproto.Entry{Timestamp: time.Unix(9, 0), Line: "line9"},
						logproto.Entry{Timestamp: time.Unix(10, 0), Line: "line10"},
					},
				},
				logproto.Stream{
					Labels: "{test=\"two\"}",
					Entries: []logproto.Entry{
						logproto.Entry{Timestamp: time.Unix(1, 1000), Line: "s2line1"},
						logproto.Entry{Timestamp: time.Unix(2, 1000), Line: "s2line2"},
						logproto.Entry{Timestamp: time.Unix(3, 1000), Line: "s2line3"},
						logproto.Entry{Timestamp: time.Unix(4, 1000), Line: "s2line4"},
						logproto.Entry{Timestamp: time.Unix(5, 1000), Line: "s2line5"},
						logproto.Entry{Timestamp: time.Unix(6, 1000), Line: "s2line6"},
						logproto.Entry{Timestamp: time.Unix(7, 1000), Line: "s2line7"},
						logproto.Entry{Timestamp: time.Unix(8, 1000), Line: "s2line8"},
						logproto.Entry{Timestamp: time.Unix(9, 1000), Line: "s2line9"},
						logproto.Entry{Timestamp: time.Unix(10, 1000), Line: "s2line10"},
					},
				},
			},
			start:         time.Unix(1, 0),
			end:           time.Unix(11, 0),
			limit:         12,
			batch:         3,
			labelMatcher:  "{test=~\"one|two\"}",
			forward:       false,
			expectedCalls: 6,
			expected: []string{
				"s2line10", "line10", "s2line9", "line9", "s2line8", "line8", "s2line7", "line7", "s2line6", "line6", "s2line5", "line5",
			},
		},
		{
			name: "single stream forward batch identical timestamps",
			streams: []logproto.Stream{
				logproto.Stream{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						logproto.Entry{Timestamp: time.Unix(1, 0), Line: "line1"},
						logproto.Entry{Timestamp: time.Unix(2, 0), Line: "line2"},
						logproto.Entry{Timestamp: time.Unix(3, 0), Line: "line3"},
						logproto.Entry{Timestamp: time.Unix(4, 0), Line: "line4"},
						logproto.Entry{Timestamp: time.Unix(5, 0), Line: "line5"},
						logproto.Entry{Timestamp: time.Unix(6, 0), Line: "line6"},
						logproto.Entry{Timestamp: time.Unix(6, 0), Line: "line6a"},
						logproto.Entry{Timestamp: time.Unix(7, 0), Line: "line7"},
						logproto.Entry{Timestamp: time.Unix(8, 0), Line: "line8"},
						logproto.Entry{Timestamp: time.Unix(9, 0), Line: "line9"},
						logproto.Entry{Timestamp: time.Unix(10, 0), Line: "line10"},
					},
				},
			},
			start:        time.Unix(1, 0),
			end:          time.Unix(11, 0),
			limit:        9,
			batch:        4,
			labelMatcher: "{test=\"simple\"}",
			forward:      true,
			// Our batchsize is 2 but each query will also return the overlapping last element from the
			// previous batch, as such we only get one item per call so we make a lot of calls
			// Call one:   line1 line2 line3 line4
			// Call two:   line4 line5 line6 line6a
			// Call three: line6 line6a line7 line8  <- notice line 6 and 6a share the same timestamp so they get returned as overlap in the next query.
			expectedCalls: 3,
			expected: []string{
				"line1", "line2", "line3", "line4", "line5", "line6", "line6a", "line7", "line8",
			},
		},
		{
			name: "single stream backward batch identical timestamps",
			streams: []logproto.Stream{
				logproto.Stream{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						logproto.Entry{Timestamp: time.Unix(1, 0), Line: "line1"},
						logproto.Entry{Timestamp: time.Unix(2, 0), Line: "line2"},
						logproto.Entry{Timestamp: time.Unix(3, 0), Line: "line3"},
						logproto.Entry{Timestamp: time.Unix(4, 0), Line: "line4"},
						logproto.Entry{Timestamp: time.Unix(5, 0), Line: "line5"},
						logproto.Entry{Timestamp: time.Unix(6, 0), Line: "line6"},
						logproto.Entry{Timestamp: time.Unix(6, 0), Line: "line6a"},
						logproto.Entry{Timestamp: time.Unix(6, 0), Line: "line6b"},
						logproto.Entry{Timestamp: time.Unix(7, 0), Line: "line7"},
						logproto.Entry{Timestamp: time.Unix(8, 0), Line: "line8"},
						logproto.Entry{Timestamp: time.Unix(9, 0), Line: "line9"},
						logproto.Entry{Timestamp: time.Unix(10, 0), Line: "line10"},
					},
				},
			},
			start:        time.Unix(1, 0),
			end:          time.Unix(11, 0),
			limit:        11,
			batch:        4,
			labelMatcher: "{test=\"simple\"}",
			forward:      false,
			// Our batchsize is 2 but each query will also return the overlapping last element from the
			// previous batch, as such we only get one item per call so we make a lot of calls
			// Call one:   line10 line9 line8 line7
			// Call two:   line7 line6b line6a line6
			// Call three: line6b line6a line6 line5
			// Call four:  line5 line5 line3 line2
			expectedCalls: 4,
			expected: []string{
				"line10", "line9", "line8", "line7", "line6b", "line6a", "line6", "line5", "line4", "line3", "line2",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := newTestQueryClient(tt.streams...)
			writer := &bytes.Buffer{}
			out := output.NewRaw(writer, nil)
			q := Query{
				QueryString:     tt.labelMatcher,
				Start:           tt.start,
				End:             tt.end,
				Limit:           tt.limit,
				BatchSize:       tt.batch,
				Forward:         tt.forward,
				Step:            0,
				Interval:        0,
				Quiet:           false,
				NoLabels:        false,
				IgnoreLabelsKey: nil,
				ShowLabelsKey:   nil,
				FixedLabelsLen:  0,
				LocalConfig:     "",
			}
			q.DoQuery(tc, out, false)
			split := strings.Split(writer.String(), "\n")
			// Remove the last entry because there is always a newline after the last line which
			// leaves an entry element in the list of lines.
			if len(split) > 0 {
				split = split[:len(split)-1]
			}
			assert.Equal(t, tt.expected, split)
			assert.Equal(t, tt.expectedCalls, tc.queryRangeCalls)
		})
	}
}

func mustParseLabels(s string) loghttp.LabelSet {
	l, err := marshal.NewLabelSet(s)

	if err != nil {
		log.Fatalf("Failed to parse %s", s)
	}

	return l
}

type testQueryClient struct {
	engine          *logql.Engine
	queryRangeCalls int
}

func newTestQueryClient(testStreams ...logproto.Stream) *testQueryClient {
	q := logql.NewMockQuerier(0, testStreams)
	e := logql.NewEngine(logql.EngineOpts{}, q)
	return &testQueryClient{
		engine:          e,
		queryRangeCalls: 0,
	}
}

func (t *testQueryClient) Query(queryStr string, limit int, time time.Time, direction logproto.Direction, quiet bool) (*loghttp.QueryResponse, error) {
	panic("implement me")
}

func (t *testQueryClient) QueryRange(queryStr string, limit int, from, through time.Time, direction logproto.Direction, step, interval time.Duration, quiet bool) (*loghttp.QueryResponse, error) {

	params := logql.NewLiteralParams(queryStr, from, through, step, interval, direction, uint32(limit), nil)

	v, err := t.engine.Query(params).Exec(context.Background())
	if err != nil {
		return nil, err
	}

	value, err := marshal.NewResultValue(v.Data)
	if err != nil {
		return nil, err
	}

	q := &loghttp.QueryResponse{
		Status: "success",
		Data: loghttp.QueryResponseData{
			ResultType: value.Type(),
			Result:     value,
			Statistics: v.Statistics,
		},
	}
	t.queryRangeCalls++
	return q, nil
}

func (t *testQueryClient) ListLabelNames(quiet bool, from, through time.Time) (*loghttp.LabelResponse, error) {
	panic("implement me")
}

func (t *testQueryClient) ListLabelValues(name string, quiet bool, from, through time.Time) (*loghttp.LabelResponse, error) {
	panic("implement me")
}

func (t *testQueryClient) Series(matchers []string, from, through time.Time, quiet bool) (*loghttp.SeriesResponse, error) {
	panic("implement me")
}

func (t *testQueryClient) LiveTailQueryConn(queryStr string, delayFor int, limit int, from int64, quiet bool) (*websocket.Conn, error) {
	panic("implement me")
}

func (t *testQueryClient) GetOrgID() string {
	panic("implement me")
}
