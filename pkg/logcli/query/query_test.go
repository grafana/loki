package query

import (
	"bytes"
	"context"
	"log"
	"reflect"
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
		name         string
		streams      []logproto.Stream
		start, end   time.Time
		limit, batch int
		labelMatcher string
		forward bool
		expected     []string
	}{
		{
			name: "super simple forward",
			streams: []logproto.Stream{
				logproto.Stream{
					Labels:  "{test=\"simple\"}",
					Entries: []logproto.Entry{
						logproto.Entry{
							Timestamp: time.Unix(1, 0),
							Line:      "line1",
						},
						logproto.Entry{
							Timestamp: time.Unix(2, 0),
							Line:      "line2",
						},
						logproto.Entry{
							Timestamp: time.Unix(3, 0),
							Line:      "line3",
						},
					},
				},
			},
			start: time.Unix(1, 0),
			end: time.Unix(3, 0),
			limit: 10,
			batch: 10,
			labelMatcher: "{test=\"simple\"}",
			forward: true,
			expected: []string{
				"line1",
				"line2",
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
			assert.Equal(t, tt.expected, writer.String())
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
	engine *logql.Engine
}

func newTestQueryClient(testStreams ...logproto.Stream) *testQueryClient {
	q := logql.NewMockQuerier(0, testStreams)
	e := logql.NewEngine(logql.EngineOpts{}, q)
	return &testQueryClient{engine: e}
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
