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

// TestFileClient_QueryRangeOutputOrder is a regression test: with --stdin,
// BACKWARD (the logcli default) must return lines in reverse input order and
// FORWARD must keep input order, also when the pipeline splits the lines into
// several streams.
func TestFileClient_QueryRangeOutputOrder(t *testing.T) {
	input := []string{
		`level=info msg="line 1"`,
		`level=error msg="line 2"`,
		`level=info msg="line 3"`,
		`level=error msg="line 4"`,
		`level=info msg="line 5"`,
		`level=error msg="line 6"`,
	}

	reversed := make([]string, len(input))
	for i, line := range input {
		reversed[len(input)-1-i] = line
	}

	now := time.Now()

	cases := []struct {
		name            string
		query           string
		direction       logproto.Direction
		expectedStreams int
		expected        []string
	}{
		{
			name:            "single-stream-forward",
			query:           `{source="logcli"}`,
			direction:       logproto.FORWARD,
			expectedStreams: 1,
			expected:        input,
		},
		{
			name:            "single-stream-backward",
			query:           `{source="logcli"}`,
			direction:       logproto.BACKWARD,
			expectedStreams: 1,
			expected:        reversed,
		},
		{
			// Only extract `level` so lines are grouped into an info and an
			// error stream.
			name:            "multi-stream-forward",
			query:           `{source="logcli"} | logfmt level`,
			direction:       logproto.FORWARD,
			expectedStreams: 2,
			expected:        input,
		},
		{
			name:            "multi-stream-backward",
			query:           `{source="logcli"} | logfmt level`,
			direction:       logproto.BACKWARD,
			expectedStreams: 2,
			expected:        reversed,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := NewFileClient(io.NopCloser(strings.NewReader(strings.Join(input, "\n"))))
			resp, err := client.QueryRange(
				c.query,
				100, // more than input
				now.Add(-1*time.Hour),
				now,
				c.direction,
				0,
				0,
				true,
			)
			require.NoError(t, err)
			require.Equal(t, loghttp.QueryStatusSuccess, resp.Status)

			streams, ok := resp.Data.Result.(loghttp.Streams)
			require.True(t, ok, "response type should be `loghttp.Streams`")
			require.Len(t, streams, c.expectedStreams)

			var merged []loghttp.Entry
			for _, stream := range streams {
				// Every stream must be ordered by the query direction, and
				// timestamps must never tie.
				for i := 1; i < len(stream.Entries); i++ {
					prev, cur := stream.Entries[i-1].Timestamp, stream.Entries[i].Timestamp
					if c.direction == logproto.FORWARD {
						require.True(t, cur.After(prev), "timestamps must strictly increase for FORWARD")
					} else {
						require.True(t, cur.Before(prev), "timestamps must strictly decrease for BACKWARD")
					}
				}
				merged = append(merged, stream.Entries...)
			}

			// Merge all streams by timestamp in the query direction, the same
			// way logcli prints them.
			sort.SliceStable(merged, func(i, j int) bool {
				if c.direction == logproto.FORWARD {
					return merged[i].Timestamp.Before(merged[j].Timestamp)
				}
				return merged[i].Timestamp.After(merged[j].Timestamp)
			})

			got := make([]string, 0, len(merged))
			for _, e := range merged {
				got = append(got, e.Line)
			}
			assert.Equal(t, c.expected, got)
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
	cases := []struct {
		name     string
		label    string
		expected *loghttp.LabelResponse
	}{
		{
			name:  "existing label returns its value",
			label: defaultLabelKey,
			expected: &loghttp.LabelResponse{
				Data:   []string{defaultLabelValue},
				Status: loghttp.QueryStatusSuccess,
			},
		},
		{
			// Sorts before defaultLabelKey ("source"); previously returned the
			// fabricated value defaultLabelValue instead of an empty response.
			name:     "missing label sorting before the existing label returns empty",
			label:    "aaa",
			expected: &loghttp.LabelResponse{},
		},
		{
			// Sorts after defaultLabelKey ("source"); previously panicked with an
			// index out of range because the search index equals len(f.labels).
			name:     "missing label sorting after the existing label returns empty",
			label:    "zzz",
			expected: &loghttp.LabelResponse{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := newEmptyClient(t)
			values, err := client.ListLabelValues(c.label, true, time.Now(), time.Now())
			require.NoError(t, err)
			assert.Equal(t, c.expected, values)
		})
	}
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

	// Entries are asserted in the order they are returned; they must already
	// follow the query direction.
	got := streams[0]
	require.Equal(t, len(got.Entries), len(logLines))
	for i, entry := range got.Entries {
		assert.Equal(t, entry.Line, logLines[i])
	}
}
