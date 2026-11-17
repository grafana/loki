package unmarshal

import (
	"encoding/json"
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
		expectedErr string
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
			expectedErr: "invalid character 'N' at start of value",
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
		{
			name: "null in stream label value",
			expected: []logproto.Stream{
				{
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 123456789012345),
							Line:      "super line",
						},
					},
					Labels: `{nothing="null", test="test"}`,
				},
			},
			actual: `{
			"streams": [
				{
					"stream": {
						"test": "test",
						"nothing": null
					},
					"values":[
						[ "123456789012345", "super line" ]
					]
				}
			]
		}`,
		},

		{
			name:        "null stream",
			expectedErr: "stream must be an object",
			actual: `{
			"streams": [
				{
					"stream": null,
					"values":[
						[ "123456789012345", "super line" ]
					]
				}
			]
		}`,
		},

		{
			name:        "invalid top level field",
			expected:    nil,
			expectedErr: "found unknown field",
			actual: `{
				"invalid": {}
			}`,
		},

		{
			name:        "invalid field",
			expected:    nil,
			expectedErr: "reading Stream: found unknown field",
			actual: `{
				"streams": [
				  {
						"stream": {
					    "job": "push-payload-test"
						},
						"values": [
						  ["123456789012345", "super line"]
						],
						"invalid": []
				  },
				]
			}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var actual logproto.PushRequest
			closer := io.NopCloser(strings.NewReader(tc.actual))

			err := DecodePushRequest(closer, &actual)
			if tc.expectedErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectedErr)
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

// Create a simple stream with a full entry including structured metadata
func createStream() string {
	// Create a JSON string that simulates the HTTP API interaction
	return `{
		"streams": [
			{
				"stream": {"service_name":"test"},
				"values": [["1723986725000000000", "{}", {"message": "a\nb\nc"}]]
			}
		]
	}`
}

// Test that simulates the full flow from HTTP API to response
func TestFullPushApiFlow(t *testing.T) {
	// 1. Create a test input that simulates pushing to /loki/api/v1/push
	jsonStr := createStream()

	// 2. Parse the raw values first and verify what's getting unmarshaled
	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &parsed)
	require.NoError(t, err)

	// 3. Log the parsed values for inspection
	t.Logf("Parsed JSON: %+v", parsed)

	// 4. Manually extract the structured metadata to see what's happening
	streams := parsed["streams"].([]interface{})
	values := streams[0].(map[string]interface{})["values"].([]interface{})
	entry := values[0].([]interface{})
	metadata := entry[2].(map[string]interface{})

	t.Logf("Raw metadata: %+v", metadata)
	require.Equal(t, "a\nb\nc", metadata["message"], "JSON unmarshal should preserve newlines")

	// 5. Now use our PushRequest implementation
	var request logproto.PushRequest
	err = DecodePushRequest(strings.NewReader(jsonStr), &request)
	require.NoError(t, err)

	// 6. Extract and verify the structured metadata
	require.NotEmpty(t, request.Streams)
	logEntry := request.Streams[0].Entries[0]
	t.Logf("Entry line: %s", logEntry.Line)
	t.Logf("Entry structured metadata: %v", logEntry.StructuredMetadata)

	// 7. Verify the structured metadata contains actual newlines
	require.Equal(t, "a\nb\nc", logEntry.StructuredMetadata[0].Value,
		"After unmarshaling, structured metadata should contain actual newlines")

	// 8. Marshal it back like the query would do
	marshaledData, err := json.Marshal(logEntry)
	require.NoError(t, err)
	t.Logf("Marshaled entry: %s", string(marshaledData))

	// 9. Verify no double escaping in the JSON representation
	// We can't easily check the exact JSON string since the order of fields might change
	// and timestamps might be formatted differently, so we'll just check that double
	// escaping doesn't happen
	jsonOutput := string(marshaledData)
	require.NotContains(t, jsonOutput, `\\n`, "JSON should not have double-escaped newlines")

	// 10. Unmarshal again and verify
	var reUnmarshaled logproto.Entry
	err = json.Unmarshal(marshaledData, &reUnmarshaled)
	require.NoError(t, err)

	// 11. Final verification
	require.Equal(t, "a\nb\nc", reUnmarshaled.StructuredMetadata[0].Value,
		"After round-trip marshaling, structured metadata should still contain actual newlines")
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
