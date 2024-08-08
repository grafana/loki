package unmarshal

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	legacy_loghttp "github.com/grafana/loki/v3/pkg/loghttp/legacy"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/marshal"
)

func Test_DecodePushRequest(t *testing.T) {
	// covers requests to /loki/api/v1/push
	for _, tc := range []struct {
		name        string
		expected    []logproto.Stream
		expectedErr bool
		actual      string
	}{
		{
			name: "basic",
			expected: []logproto.Stream{
				{
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 123456789012345),
							Line:      "super line",
						},
					},
					Labels: labels.FromStrings("test", "test").String(),
				},
			},
			actual: `{
			"streams": [
				{
					"stream": {
						"test": "test"
					},
					"values":[
						[ "123456789012345", "super line" ]
					]
				}
			]
		}`,
		},
		{
			name: "with structured metadata",
			expected: []logproto.Stream{
				{
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 123456789012345),
							Line:      "super line",
							StructuredMetadata: []logproto.LabelAdapter{
								{Name: "a", Value: "1"},
								{Name: "b", Value: "2"},
							},
						},
					},
					Labels: labels.FromStrings("test", "test").String(),
				},
			},
			actual: `{
			"streams": [
				{
					"stream": {
						"test": "test"
					},
					"values":[
						[ "123456789012345", "super line", { "a": "1", "b": "2" } ]
					]
				}
			]
		}`,
		},

		// The following test cases are added to cover a regression. Even though the Loki HTTP API
		// docs for the push endpoint state that the stream label values should be strings, we
		// previously didn't enforce this requirement.
		// With https://github.com/grafana/loki/pull/9694, we started enforcing this requirement
		// and that broke some users. We are adding these test cases to ensure that we don't
		// enforce this requirement in the future. Note that we may want to enforce this requirement
		// in a future major release, in which case we should modify these test cases.
		{
			name: "number in stream label value",
			expected: []logproto.Stream{
				{
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 123456789012345),
							Line:      "super line",
						},
					},
					Labels: labels.FromStrings("test", "test", "number", "123").String(),
				},
			},
			actual: `{
			"streams": [
				{
					"stream": {
						"test": "test",
						"number": 123
					},
					"values":[
						[ "123456789012345", "super line" ]
					]
				}
			]
		}`,
		},
		{
			name:        "string without quotes in stream label value",
			expectedErr: true,
			actual: `{
			"streams": [
				{
					"stream": {
						"test": "test",
						"text": None
					},
					"values":[
						[ "123456789012345", "super line" ]
					]
				}
			]
		}`,
		},
		{
			name: "json object in stream label value",
			expected: []logproto.Stream{
				{
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 123456789012345),
							Line:      "super line",
						},
					},
					Labels: labels.FromStrings("test", "test", "text", "{ \"a\": \"b\" }").String(),
				},
			},
			actual: `{
			"streams": [
				{
					"stream": {
						"test": "test",
						"text": { "a": "b" }
					},
					"values":[
						[ "123456789012345", "super line" ]
					]
				}
			]
		}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var actual logproto.PushRequest
			closer := io.NopCloser(strings.NewReader(tc.actual))

			err := DecodePushRequest(closer, &actual)
			if tc.expectedErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			require.Equal(t, tc.expected, actual.Streams)
		})
	}
}

func Benchmark_DecodePushRequest(b *testing.B) {
	requestFmt := `{
		"streams": [
			{
				"stream": {
					"test": "test",
					"foo" : "bar"
				},
				"values":[
					%s
				]
			}
		]
	}`
	var entries strings.Builder
	for i := 0; i < 10000; i++ {
		entries.WriteString(`[ "123456789012345", "WARN  [CompactionExecutor:61771] 2021-01-12 09:40:23,192 TimeWindowCompactionController.java:41 - You are running with sstables overlapping checks disabled, it can result in loss of data" ],`)
	}
	requestString := fmt.Sprintf(requestFmt, entries.String()[:len(entries.String())-1])
	r := strings.NewReader("")

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		var actual logproto.PushRequest
		r.Reset(requestString)
		err := DecodePushRequest(r, &actual)
		require.NoError(b, err)
		require.Equal(b, 10000, len(actual.Streams[0].Entries))
	}
}

type websocket struct {
	buf []byte
}

func (ws *websocket) WriteMessage(_ int, data []byte) error {
	ws.buf = append(ws.buf, data...)
	return nil
}

func (ws *websocket) ReadMessage() (int, []byte, error) {
	return 0, ws.buf, nil
}

func Test_ReadTailResponse(t *testing.T) {
	ws := &websocket{}
	wsJSON := marshal.NewWebsocketJSONWriter(ws)
	require.NoError(t, marshal.WriteTailResponseJSON(legacy_loghttp.TailResponse{
		Streams: []logproto.Stream{
			{Labels: `{app="bar"}`, Entries: []logproto.Entry{{Timestamp: time.Unix(0, 2), Line: "2"}}},
		},
		DroppedEntries: []legacy_loghttp.DroppedEntry{
			{Timestamp: time.Unix(0, 1), Labels: `{app="foo"}`},
		},
	}, wsJSON, nil))
	res := &loghttp.TailResponse{}
	require.NoError(t, ReadTailResponseJSON(res, ws))

	require.Equal(t, &loghttp.TailResponse{
		Streams: []loghttp.Stream{
			{
				Labels:  loghttp.LabelSet{"app": "bar"},
				Entries: []loghttp.Entry{{Timestamp: time.Unix(0, 2), Line: "2"}},
			},
		},
		DroppedStreams: []loghttp.DroppedStream{
			{Timestamp: time.Unix(0, 1), Labels: loghttp.LabelSet{"app": "foo"}},
		},
	}, res)
	// Do it twice to verify we reset correctly slices.
	require.NoError(t, ReadTailResponseJSON(res, ws))

	require.Equal(t, &loghttp.TailResponse{
		Streams: []loghttp.Stream{
			{
				Labels:  loghttp.LabelSet{"app": "bar"},
				Entries: []loghttp.Entry{{Timestamp: time.Unix(0, 2), Line: "2"}},
			},
		},
		DroppedStreams: []loghttp.DroppedStream{
			{Timestamp: time.Unix(0, 1), Labels: loghttp.LabelSet{"app": "foo"}},
		},
	}, res)
}
