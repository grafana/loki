package query

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gorilla/websocket"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logcli/output"
	"github.com/grafana/loki/v3/pkg/logcli/volume"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/marshal"
)

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

type testQueryClient struct {
	engine          *logql.Engine
	queryRangeCalls int
}

func newTestQueryClient(testStreams ...logproto.Stream) *testQueryClient {
	q := logql.NewMockQuerier(0, testStreams)
	e := logql.NewEngine(logql.EngineOpts{}, q, logql.NoLimits, log.NewNopLogger())
	return &testQueryClient{
		engine:          e,
		queryRangeCalls: 0,
	}
}

func (t *testQueryClient) Query(_ string, _ int, _ time.Time, _ logproto.Direction, _ bool) (*loghttp.QueryResponse, error) {
	panic("implement me")
}

func (t *testQueryClient) QueryRange(queryStr string, limit int, from, through time.Time, direction logproto.Direction, step, interval time.Duration, _ bool) (*loghttp.QueryResponse, error) {
	ctx := user.InjectOrgID(context.Background(), "fake")

	params, err := logql.NewLiteralParams(queryStr, from, through, step, interval, direction, uint32(limit), nil, nil)
	if err != nil {
		return nil, err
	}

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

func (t *testQueryClient) ListLabelNames(_ bool, _, _ time.Time) (*loghttp.LabelResponse, error) {
	panic("implement me")
}

func (t *testQueryClient) ListLabelValues(_ string, _ bool, _, _ time.Time) (*loghttp.LabelResponse, error) {
	panic("implement me")
}

func (t *testQueryClient) Series(_ []string, _, _ time.Time, _ bool) (*loghttp.SeriesResponse, error) {
	panic("implement me")
}

func (t *testQueryClient) LiveTailQueryConn(_ string, _ time.Duration, _ int, _ time.Time, _ bool) (*websocket.Conn, error) {
	panic("implement me")
}

func (t *testQueryClient) GetOrgID() string {
	panic("implement me")
}

func (t *testQueryClient) GetStats(_ string, _, _ time.Time, _ bool) (*logproto.IndexStatsResponse, error) {
	panic("not implemented")
}

func (t *testQueryClient) GetVolume(_ *volume.Query) (*loghttp.QueryResponse, error) {
	panic("not implemented")
}

func (t *testQueryClient) GetVolumeRange(_ *volume.Query) (*loghttp.QueryResponse, error) {
	panic("not implemented")
}

func (t *testQueryClient) GetDetectedFields(
	_, _ string,
	_, _ int,
	_, _ time.Time,
	_ time.Duration,
	_ bool,
) (*loghttp.DetectedFieldsResponse, error) {
	panic("not implemented")
}

var legacySchemaConfigContents = `schema_config:
  configs:
  - from: 2020-05-15
    store: boltdb-shipper
    object_store: gcs
    schema: v10
    index:
      prefix: index_
      period: 168h
`

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
var schemaConfigContents2 = `schema_config:
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
  - from: 2020-09-30
    store: boltdb-shipper
    object_store: gcs
    schema: v12
    index:
      prefix: index_
      period: 24h
`
var cm = storage.NewClientMetrics()

func setupTestEnv(t *testing.T) (string, client.ObjectClient) {
	t.Helper()
	tmpDir := t.TempDir()
	conf := loki.Config{
		StorageConfig: storage.Config{
			FSConfig: local.FSConfig{
				Directory: tmpDir,
			},
		},
	}

	client, err := GetObjectClient(types.StorageTypeFileSystem, conf, cm)
	require.NoError(t, err)
	require.NotNil(t, client)

	_, err = getLatestConfig(client, "456")
	require.Error(t, err)
	require.True(t, errors.Is(err, errNotExists))

	return tmpDir, client
}

func TestLoadFromURL(t *testing.T) {
	tmpDir, client := setupTestEnv(t)
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
}

func TestMultipleConfigs(t *testing.T) {
	tmpDir, client := setupTestEnv(t)

	err := os.WriteFile(
		filepath.Join(tmpDir, "456-schemaconfig.yaml"),
		[]byte(schemaConfigContents),
		0666,
	)
	require.NoError(t, err)

	config, err := getLatestConfig(client, "456")
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Configs, 2)

	err = os.WriteFile(
		filepath.Join(tmpDir, "456-schemaconfig-1.yaml"),
		[]byte(schemaConfigContents2),
		0666,
	)
	require.NoError(t, err)

	config, err = getLatestConfig(client, "456")
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Configs, 3)
}

func TestMultipleConfigsIncludingLegacy(t *testing.T) {
	tmpDir, client := setupTestEnv(t)

	err := os.WriteFile(
		filepath.Join(tmpDir, "schemaconfig.yaml"),
		[]byte(legacySchemaConfigContents),
		0666,
	)
	require.NoError(t, err)

	err = os.WriteFile(
		filepath.Join(tmpDir, "456-schemaconfig.yaml"),
		[]byte(schemaConfigContents),
		0666,
	)
	require.NoError(t, err)

	config, err := getLatestConfig(client, "456")
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Configs, 2)

	err = os.WriteFile(
		filepath.Join(tmpDir, "456-schemaconfig-1.yaml"),
		[]byte(schemaConfigContents2),
		0666,
	)
	require.NoError(t, err)

	config, err = getLatestConfig(client, "456")
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Configs, 3)
}

func TestLegacyConfigOnly(t *testing.T) {
	tmpDir, client := setupTestEnv(t)

	err := os.WriteFile(
		filepath.Join(tmpDir, "schemaconfig.yaml"),
		[]byte(legacySchemaConfigContents),
		0666,
	)
	require.NoError(t, err)

	config, err := getLatestConfig(client, "456")
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Configs, 1)
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
		t.Run(
			tt.name,
			func(t *testing.T) {
				jobs := tt.q.parallelJobs()
				cmpParallelJobSlice(t, tt.jobs, jobs)
			},
		)
	}
}
