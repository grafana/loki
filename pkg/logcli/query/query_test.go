package query

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	"github.com/grafana/loki/pkg/util/marshal"
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
				[]loghttp.LabelSet{mustParseLabels(t, `{foo="bar", bar="foo"}`), mustParseLabels(t, `{bar="foo", foo="foo", baz="baz"}`)},
			},
			mustParseLabels(t, `{bar="foo"}`),
		},
		{
			"Extract common labels source > target",
			args{
				[]loghttp.LabelSet{mustParseLabels(t, `{foo="bar", bar="foo"}`), mustParseLabels(t, `{bar="foo", foo="bar", baz="baz"}`)},
			},
			mustParseLabels(t, `{foo="bar", bar="foo"}`),
		},
		{
			"Extract common labels source < target",
			args{
				[]loghttp.LabelSet{mustParseLabels(t, `{foo="bar", bar="foo"}`), mustParseLabels(t, `{bar="foo"}`)},
			},
			mustParseLabels(t, `{bar="foo"}`),
		},
		{
			"Extract common labels source < target no common",
			args{
				[]loghttp.LabelSet{mustParseLabels(t, `{foo="bar", bar="foo"}`), mustParseLabels(t, `{fo="bar"}`)},
			},
			loghttp.LabelSet{},
		},
		{
			"Extract common labels source = target no common",
			args{
				[]loghttp.LabelSet{mustParseLabels(t, `{foo="bar"}`), mustParseLabels(t, `{fooo="bar"}`)},
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
				mustParseLabels(t, `{foo="bar", bar="foo"}`),
				mustParseLabels(t, `{bar="foo", foo="foo", baz="baz"}`),
			},
			mustParseLabels(t, `{foo="bar"}`),
		},
		{
			"Subtract labels source < target",
			args{
				mustParseLabels(t, `{foo="bar", bar="foo"}`),
				mustParseLabels(t, `{bar="foo"}`),
			},
			mustParseLabels(t, `{foo="bar"}`),
		},
		{
			"Subtract labels source < target no sub",
			args{
				mustParseLabels(t, `{foo="bar", bar="foo"}`),
				mustParseLabels(t, `{fo="bar"}`),
			},
			mustParseLabels(t, `{bar="foo", foo="bar"}`),
		},
		{
			"Subtract labels source = target no sub",
			args{
				mustParseLabels(t, `{foo="bar"}`),
				mustParseLabels(t, `{fiz="buz"}`),
			},
			mustParseLabels(t, `{foo="bar"}`),
		},
		{
			"Subtract labels source > target no sub",
			args{
				mustParseLabels(t, `{foo="bar"}`),
				mustParseLabels(t, `{fiz="buz", foo="baz"}`),
			},
			mustParseLabels(t, `{foo="bar"}`),
		},
		{
			"Subtract labels source > target no sub",
			args{
				mustParseLabels(t, `{a="b", foo="bar", baz="baz", fizz="fizz"}`),
				mustParseLabels(t, `{foo="bar", baz="baz", buzz="buzz", fizz="fizz"}`),
			},
			mustParseLabels(t, `{a="b"}`),
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
				{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "line1"},
						{Timestamp: time.Unix(2, 0), Line: "line2"},
						{Timestamp: time.Unix(3, 0), Line: "line3"}, // End timestamp is exclusive
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
				{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "line1"},
						{Timestamp: time.Unix(2, 0), Line: "line2"},
						{Timestamp: time.Unix(3, 0), Line: "line3"}, // End timestamp is exclusive
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
				{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "line1"},
						{Timestamp: time.Unix(2, 0), Line: "line2"},
						{Timestamp: time.Unix(3, 0), Line: "line3"},
						{Timestamp: time.Unix(4, 0), Line: "line4"},
						{Timestamp: time.Unix(5, 0), Line: "line5"},
						{Timestamp: time.Unix(6, 0), Line: "line6"},
						{Timestamp: time.Unix(7, 0), Line: "line7"},
						{Timestamp: time.Unix(8, 0), Line: "line8"},
						{Timestamp: time.Unix(9, 0), Line: "line9"},
						{Timestamp: time.Unix(10, 0), Line: "line10"},
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
				{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "line1"},
						{Timestamp: time.Unix(2, 0), Line: "line2"},
						{Timestamp: time.Unix(3, 0), Line: "line3"},
						{Timestamp: time.Unix(4, 0), Line: "line4"},
						{Timestamp: time.Unix(5, 0), Line: "line5"},
						{Timestamp: time.Unix(6, 0), Line: "line6"},
						{Timestamp: time.Unix(7, 0), Line: "line7"},
						{Timestamp: time.Unix(8, 0), Line: "line8"},
						{Timestamp: time.Unix(9, 0), Line: "line9"},
						{Timestamp: time.Unix(10, 0), Line: "line10"},
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
				{
					Labels: "{test=\"one\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "line1"},
						{Timestamp: time.Unix(2, 0), Line: "line2"},
						{Timestamp: time.Unix(3, 0), Line: "line3"},
						{Timestamp: time.Unix(4, 0), Line: "line4"},
						{Timestamp: time.Unix(5, 0), Line: "line5"},
						{Timestamp: time.Unix(6, 0), Line: "line6"},
						{Timestamp: time.Unix(7, 0), Line: "line7"},
						{Timestamp: time.Unix(8, 0), Line: "line8"},
						{Timestamp: time.Unix(9, 0), Line: "line9"},
						{Timestamp: time.Unix(10, 0), Line: "line10"},
					},
				},
				{
					Labels: "{test=\"two\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(1, 1000), Line: "s2line1"},
						{Timestamp: time.Unix(2, 1000), Line: "s2line2"},
						{Timestamp: time.Unix(3, 1000), Line: "s2line3"},
						{Timestamp: time.Unix(4, 1000), Line: "s2line4"},
						{Timestamp: time.Unix(5, 1000), Line: "s2line5"},
						{Timestamp: time.Unix(6, 1000), Line: "s2line6"},
						{Timestamp: time.Unix(7, 1000), Line: "s2line7"},
						{Timestamp: time.Unix(8, 1000), Line: "s2line8"},
						{Timestamp: time.Unix(9, 1000), Line: "s2line9"},
						{Timestamp: time.Unix(10, 1000), Line: "s2line10"},
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
				{
					Labels: "{test=\"one\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "line1"},
						{Timestamp: time.Unix(2, 0), Line: "line2"},
						{Timestamp: time.Unix(3, 0), Line: "line3"},
						{Timestamp: time.Unix(4, 0), Line: "line4"},
						{Timestamp: time.Unix(5, 0), Line: "line5"},
						{Timestamp: time.Unix(6, 0), Line: "line6"},
						{Timestamp: time.Unix(7, 0), Line: "line7"},
						{Timestamp: time.Unix(8, 0), Line: "line8"},
						{Timestamp: time.Unix(9, 0), Line: "line9"},
						{Timestamp: time.Unix(10, 0), Line: "line10"},
					},
				},
				{
					Labels: "{test=\"two\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(1, 1000), Line: "s2line1"},
						{Timestamp: time.Unix(2, 1000), Line: "s2line2"},
						{Timestamp: time.Unix(3, 1000), Line: "s2line3"},
						{Timestamp: time.Unix(4, 1000), Line: "s2line4"},
						{Timestamp: time.Unix(5, 1000), Line: "s2line5"},
						{Timestamp: time.Unix(6, 1000), Line: "s2line6"},
						{Timestamp: time.Unix(7, 1000), Line: "s2line7"},
						{Timestamp: time.Unix(8, 1000), Line: "s2line8"},
						{Timestamp: time.Unix(9, 1000), Line: "s2line9"},
						{Timestamp: time.Unix(10, 1000), Line: "s2line10"},
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
				{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "line1"},
						{Timestamp: time.Unix(2, 0), Line: "line2"},
						{Timestamp: time.Unix(3, 0), Line: "line3"},
						{Timestamp: time.Unix(4, 0), Line: "line4"},
						{Timestamp: time.Unix(5, 0), Line: "line5"},
						{Timestamp: time.Unix(6, 0), Line: "line6"},
						{Timestamp: time.Unix(6, 0), Line: "line6a"},
						{Timestamp: time.Unix(7, 0), Line: "line7"},
						{Timestamp: time.Unix(8, 0), Line: "line8"},
						{Timestamp: time.Unix(9, 0), Line: "line9"},
						{Timestamp: time.Unix(10, 0), Line: "line10"},
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
				{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "line1"},
						{Timestamp: time.Unix(2, 0), Line: "line2"},
						{Timestamp: time.Unix(3, 0), Line: "line3"},
						{Timestamp: time.Unix(4, 0), Line: "line4"},
						{Timestamp: time.Unix(5, 0), Line: "line5"},
						{Timestamp: time.Unix(6, 0), Line: "line6"},
						{Timestamp: time.Unix(6, 0), Line: "line6a"},
						{Timestamp: time.Unix(6, 0), Line: "line6b"},
						{Timestamp: time.Unix(7, 0), Line: "line7"},
						{Timestamp: time.Unix(8, 0), Line: "line8"},
						{Timestamp: time.Unix(9, 0), Line: "line9"},
						{Timestamp: time.Unix(10, 0), Line: "line10"},
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
		{
			name: "single stream backward batch identical timestamps without limit",
			streams: []logproto.Stream{
				{
					Labels: "{test=\"simple\"}",
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "line1"},
						{Timestamp: time.Unix(2, 0), Line: "line2"},
						{Timestamp: time.Unix(3, 0), Line: "line3"},
						{Timestamp: time.Unix(4, 0), Line: "line4"},
						{Timestamp: time.Unix(5, 0), Line: "line5"},
						{Timestamp: time.Unix(6, 0), Line: "line6"},
						{Timestamp: time.Unix(6, 0), Line: "line6a"},
						{Timestamp: time.Unix(6, 0), Line: "line6b"},
						{Timestamp: time.Unix(7, 0), Line: "line7"},
						{Timestamp: time.Unix(8, 0), Line: "line8"},
						{Timestamp: time.Unix(9, 0), Line: "line9"},
						{Timestamp: time.Unix(10, 0), Line: "line10"},
					},
				},
			},
			start:        time.Unix(1, 0),
			end:          time.Unix(11, 0),
			limit:        0,
			batch:        4,
			labelMatcher: "{test=\"simple\"}",
			forward:      false,
			// Our batchsize is 2 but each query will also return the overlapping last element from the
			// previous batch, as such we only get one item per call so we make a lot of calls
			// Call one:   line10 line9 line8 line7
			// Call two:   line7 line6b line6a line6
			// Call three: line6b line6a line6 line5
			// Call four:  line5 line5 line3 line2
			// Call five:  line1
			// Call six:   -
			expectedCalls: 6,
			expected: []string{
				"line10", "line9", "line8", "line7", "line6b", "line6a", "line6", "line5", "line4", "line3", "line2", "line1",
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

func mustParseLabels(t *testing.T, s string) loghttp.LabelSet {
	t.Helper()
	l, err := marshal.NewLabelSet(s)
	require.NoErrorf(t, err, "Failed to parse %q", s)

	return l
}

type testQueryClient struct {
	engine          *logql.Engine
	queryRangeCalls int
	orgID           string
}

func newTestQueryClient(testStreams ...logproto.Stream) *testQueryClient {
	q := logql.NewMockQuerier(0, testStreams)
	e := logql.NewEngine(logql.EngineOpts{}, q, logql.NoLimits, log.NewNopLogger())
	return &testQueryClient{
		engine:          e,
		queryRangeCalls: 0,
	}
}

func (t *testQueryClient) Query(queryStr string, limit int, time time.Time, direction logproto.Direction, quiet bool) (*loghttp.QueryResponse, error) {
	panic("implement me")
}

func (t *testQueryClient) QueryRange(queryStr string, limit int, from, through time.Time, direction logproto.Direction, step, interval time.Duration, quiet bool) (*loghttp.QueryResponse, error) {
	ctx := user.InjectOrgID(context.Background(), "fake")

	params := logql.NewLiteralParams(queryStr, from, through, step, interval, direction, uint32(limit), nil)

	v, err := t.engine.Query(params).Exec(ctx)
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

func (t *testQueryClient) LiveTailQueryConn(queryStr string, delayFor time.Duration, limit int, start time.Time, quiet bool) (*websocket.Conn, error) {
	panic("implement me")
}

func (t *testQueryClient) GetOrgID() string {
	panic("implement me")
}

var schemaConfigContents = `schema_config:
  configs:
  - from: 2020-05-15
    store: boltdb-shipper
    object_store: gcs
    schema: v10
    index:
      prefix: index_
      period: 168h
  - from: 2020-07-31
    store: boltdb-shipper
    object_store: gcs
    schema: v11
    index:
      prefix: index_
      period: 24h
`

func TestLoadFromURL(t *testing.T) {
	tmpDir := t.TempDir()

	conf := loki.Config{
		StorageConfig: storage.Config{
			FSConfig: local.FSConfig{
				Directory: tmpDir,
			},
		},
	}

	// Missing SharedStoreType should error
	cm := storage.NewClientMetrics()
	client, err := GetObjectClient(conf, cm)
	require.Error(t, err)
	require.Nil(t, client)

	conf.StorageConfig.BoltDBShipperConfig = shipper.Config{
		Config: indexshipper.Config{
			SharedStoreType: config.StorageTypeFileSystem,
		},
	}

	client, err = GetObjectClient(conf, cm)
	require.NoError(t, err)
	require.NotNil(t, client)

	filename := "schemaconfig.yaml"

	// Missing schemaconfig.yaml file should error
	schemaConfig, err := LoadSchemaUsingObjectClient(client, filename)
	require.Error(t, err)
	require.Nil(t, schemaConfig)

	err = os.WriteFile(
		filepath.Join(tmpDir, filename),
		[]byte(schemaConfigContents),
		0666,
	)
	require.NoError(t, err)

	// Load single schemaconfig.yaml
	schemaConfig, err = LoadSchemaUsingObjectClient(client, filename)

	require.NoError(t, err)
	require.NotNil(t, schemaConfig)

	// Load multiple schemaconfig files
	schemaConfig, err = LoadSchemaUsingObjectClient(client, "foo.yaml", filename, "bar.yaml")

	require.NoError(t, err)
	require.NotNil(t, schemaConfig)
}

func TestDurationCeilDiv(t *testing.T) {
	tests := []struct {
		name   string
		d      time.Duration
		m      time.Duration
		expect int64
	}{
		{
			"10m / 5m = 2",
			10 * time.Minute,
			5 * time.Minute,
			2,
		},
		{
			"11m / 5m = 3",
			11 * time.Minute,
			5 * time.Minute,
			3,
		},
		{
			"1h / 15m = 4",
			1 * time.Hour,
			15 * time.Minute,
			4,
		},
		{
			"1h / 14m = 5",
			1 * time.Hour,
			14 * time.Minute,
			5,
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				require.Equal(t, tt.expect, ceilingDivision(tt.d, tt.m))
			},
		)
	}
}

func mustParseTime(value string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		panic(fmt.Errorf("invalid timestamp: %w", err))
	}

	return t
}

func cmpParallelJobSlice(t *testing.T, expected, actual []*parallelJob) {
	require.Equal(t, len(expected), len(actual), "job slice lengths don't match")

	for i, jobE := range expected {
		jobA := actual[i]

		require.Equal(t, jobE.q.Start, jobA.q.Start, "i=%d: job start not equal", i)
		require.Equal(t, jobE.q.End, jobA.q.End, "i=%d: job end not equal", i)
		require.Equal(t, jobE.q.Forward, jobA.q.Forward, "i=%d: job direction not equal", i)
	}
}

func TestParallelJobs(t *testing.T) {
	mkQuery := func(start, end string, d time.Duration, forward bool) *Query {
		return &Query{
			Start:            mustParseTime(start),
			End:              mustParseTime(end),
			ParallelDuration: d,
			Forward:          forward,
		}
	}

	mkParallelJob := func(start, end string, forward bool) *parallelJob {
		return &parallelJob{
			q: mkQuery(start, end, time.Minute, forward),
		}
	}

	tests := []struct {
		name string
		q    *Query
		jobs []*parallelJob
	}{
		{
			"1h range, 30m period, forward",
			mkQuery(
				"2023-02-10 15:00:00",
				"2023-02-10 16:00:00",
				30*time.Minute,
				true,
			),
			[]*parallelJob{
				mkParallelJob(
					"2023-02-10 15:00:00",
					"2023-02-10 15:30:00",
					true,
				),
				mkParallelJob(
					"2023-02-10 15:30:00",
					"2023-02-10 16:00:00",
					true,
				),
			},
		},
		{
			"1h range, 30m period, reverse",
			mkQuery(
				"2023-02-10 15:00:00",
				"2023-02-10 16:00:00",
				30*time.Minute,
				false,
			),
			[]*parallelJob{
				mkParallelJob(
					"2023-02-10 15:30:00",
					"2023-02-10 16:00:00",
					false,
				),
				mkParallelJob(
					"2023-02-10 15:00:00",
					"2023-02-10 15:30:00",
					false,
				),
			},
		},
		{
			"1h1m range, 30m period, forward",
			mkQuery(
				"2023-02-10 15:00:00",
				"2023-02-10 16:01:00",
				30*time.Minute,
				true,
			),
			[]*parallelJob{
				mkParallelJob(
					"2023-02-10 15:00:00",
					"2023-02-10 15:30:00",
					true,
				),
				mkParallelJob(
					"2023-02-10 15:30:00",
					"2023-02-10 16:00:00",
					true,
				),
				mkParallelJob(
					"2023-02-10 16:00:00",
					"2023-02-10 16:01:00",
					true,
				),
			},
		},
		{
			"1h1m range, 30m period, reverse",
			mkQuery(
				"2023-02-10 15:00:00",
				"2023-02-10 16:01:00",
				30*time.Minute,
				false,
			),
			[]*parallelJob{
				mkParallelJob(
					"2023-02-10 15:31:00",
					"2023-02-10 16:01:00",
					false,
				),
				mkParallelJob(
					"2023-02-10 15:01:00",
					"2023-02-10 15:31:00",
					false,
				),
				mkParallelJob(
					"2023-02-10 15:00:00",
					"2023-02-10 15:01:00",
					false,
				),
			},
		},
		{
			"15m range, 30m period, forward",
			mkQuery(
				"2023-02-10 15:00:00",
				"2023-02-10 15:15:00",
				30*time.Minute,
				true,
			),
			[]*parallelJob{
				mkParallelJob(
					"2023-02-10 15:00:00",
					"2023-02-10 15:15:00",
					true,
				),
			},
		},
		{
			"15m range, 30m period, reverse",
			mkQuery(
				"2023-02-10 15:00:00",
				"2023-02-10 15:15:00",
				30*time.Minute,
				false,
			),
			[]*parallelJob{
				mkParallelJob(
					"2023-02-10 15:00:00",
					"2023-02-10 15:15:00",
					false,
				),
			},
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(
			tt.name,
			func(t *testing.T) {
				jobs := tt.q.parallelJobs()
				cmpParallelJobSlice(t, tt.jobs, jobs)
			},
		)
	}
}
