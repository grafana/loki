package client

import (
	"bytes"
	"errors"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileClient_QueryRangeLogQueries(t *testing.T) {
	input := []string{
		`level=info event="loki started" caller=main.go ts=1625995076`,
		`level=info event="runtime loader started" caller=main.go ts=1625995077`,
		`level=error event="unable to read rules directory" file="/tmp/rules" caller=rules.go ts=1625995090`,
		`level=error event="failed to apply wal" error="/tmp/wal/ corrupted" caller=wal.go ts=1625996090`,
		`level=info event="loki ready" caller=main.go ts=1625996095`,
	}

	reversed := make([]string, len(input))
	copy(reversed, input)
	sort.Slice(reversed, func(i, j int) bool {
		return i > j
	})

	now := time.Now()

	cases := []struct {
		name           string
		limit          int
		start, end     time.Time
		direction      logproto.Direction
		step, interval time.Duration
		expectedStatus loghttp.QueryStatus
		expected       []string
	}{
		{
			name:           "return-all-logs-backward",
			limit:          10, // more than input
			start:          now.Add(-1 * time.Hour),
			end:            now,
			direction:      logproto.BACKWARD,
			step:           0, // let client decide based on start and end
			interval:       0,
			expectedStatus: loghttp.QueryStatusSuccess,
			expected:       reversed,
		},
		{
			name:           "return-all-logs-forward",
			limit:          10, // more than input
			start:          now.Add(-1 * time.Hour),
			end:            now,
			direction:      logproto.FORWARD,
			step:           0, // let the client decide based on start and end
			interval:       0,
			expectedStatus: loghttp.QueryStatusSuccess,
			expected:       input,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := NewFileClient(io.NopCloser(strings.NewReader(strings.Join(input, "\n"))))
			resp, err := client.QueryRange(
				`{foo="bar"}`, // label matcher doesn't matter.
				c.limit,
				c.start,
				c.end,
				c.direction,
				c.step,
				c.interval,
				true,
			)

			require.NoError(t, err)
			require.Equal(t, loghttp.QueryStatusSuccess, resp.Status)
			assert.Equal(t, string(resp.Data.ResultType), loghttp.ResultTypeStream)
			assertStreams(t, resp.Data.Result, c.expected)
		})
	}
}

func TestFileClient_Query(t *testing.T) {
	input := []string{
		`level=info event="loki started" caller=main.go ts=1625995076`,
		`level=info event="runtime loader started" caller=main.go ts=1625995077`,
		`level=error event="unable to read rules directory" file="/tmp/rules" caller=rules.go ts=1625995090`,
		`level=error event="failed to apply wal" error="/tmp/wal/ corrupted" caller=wal.go ts=1625996090`,
		`level=info event="loki ready" caller=main.go ts=1625996095`,
	}

	reversed := make([]string, len(input))
	copy(reversed, input)
	sort.Slice(reversed, func(i, j int) bool {
		return i > j
	})

	now := time.Now()

	cases := []struct {
		name           string
		limit          int
		ts             time.Time
		direction      logproto.Direction
		expectedStatus loghttp.QueryStatus
		expected       []string
	}{
		{
			name:           "return-all-logs-backward",
			limit:          10, // more than input
			ts:             now.Add(-1 * time.Hour),
			direction:      logproto.BACKWARD,
			expectedStatus: loghttp.QueryStatusSuccess,
			expected:       reversed,
		},
		{
			name:           "return-all-logs-forward",
			limit:          10, // more than input
			ts:             now.Add(-1 * time.Hour),
			direction:      logproto.FORWARD,
			expectedStatus: loghttp.QueryStatusSuccess,
			expected:       input,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := NewFileClient(io.NopCloser(strings.NewReader(strings.Join(input, "\n"))))
			resp, err := client.Query(
				`{foo="bar"}`, // label matcher doesn't matter.
				c.limit,
				c.ts,
				c.direction,
				true,
			)

			require.NoError(t, err)
			require.Equal(t, loghttp.QueryStatusSuccess, resp.Status)
			assert.Equal(t, string(resp.Data.ResultType), loghttp.ResultTypeStream)
			assertStreams(t, resp.Data.Result, c.expected)
		})
	}
}

func TestFileClient_ListLabelNames(t *testing.T) {
	c := newEmptyClient(t)
	values, err := c.ListLabelNames(true, time.Now(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, &loghttp.LabelResponse{
		Data:   []string{defaultLabelKey},
		Status: loghttp.QueryStatusSuccess,
	}, values)
}

func TestFileClient_ListLabelValues(t *testing.T) {
	c := newEmptyClient(t)
	values, err := c.ListLabelValues(defaultLabelKey, true, time.Now(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, &loghttp.LabelResponse{
		Data:   []string{defaultLabelValue},
		Status: loghttp.QueryStatusSuccess,
	}, values)

}

func TestFileClient_Series(t *testing.T) {
	c := newEmptyClient(t)
	got, err := c.Series(nil, time.Now(), time.Now(), true)
	require.NoError(t, err)

	exp := &loghttp.SeriesResponse{
		Data: []loghttp.LabelSet{
			{defaultLabelKey: defaultLabelValue},
		},
		Status: loghttp.QueryStatusSuccess,
	}

	assert.Equal(t, exp, got)
}

func TestFileClient_LiveTail(t *testing.T) {
	c := newEmptyClient(t)
	x, err := c.LiveTailQueryConn("", time.Second, 0, time.Now(), true)
	require.Error(t, err)
	require.Nil(t, x)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestFileClient_GetOrgID(t *testing.T) {
	c := newEmptyClient(t)
	assert.Equal(t, defaultOrgID, c.GetOrgID())
}

func newEmptyClient(t *testing.T) *FileClient {
	t.Helper()
	return NewFileClient(io.NopCloser(&bytes.Buffer{}))
}

func assertStreams(t *testing.T, result loghttp.ResultValue, logLines []string) {
	t.Helper()

	streams, ok := result.(loghttp.Streams)
	require.True(t, ok, "response type should be `loghttp.Streams`")

	require.Len(t, streams, 1, "there should be only one stream for FileClient")

	got := streams[0]
	sort.Slice(got.Entries, func(i, j int) bool {
		return got.Entries[i].Timestamp.UnixNano() < got.Entries[j].Timestamp.UnixNano()
	})
	require.Equal(t, len(got.Entries), len(logLines))
	for i, entry := range got.Entries {
		assert.Equal(t, entry.Line, logLines[i])
	}
}
