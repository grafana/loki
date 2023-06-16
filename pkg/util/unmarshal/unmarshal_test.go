package unmarshal

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/loghttp"
	legacy_loghttp "github.com/grafana/loki/pkg/loghttp/legacy"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util/marshal"
)

// covers requests to /loki/api/v1/push
var pushTests = []struct {
	expected []logproto.Stream
	actual   string
}{
	{
		[]logproto.Stream{
			{
				Entries: []logproto.Entry{
					{
						Timestamp: time.Unix(0, 123456789012345),
						Line:      "super line",
					},
				},
				Labels: `{test="test"}`,
			},
		},
		`{
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
}

func Test_DecodePushRequest(t *testing.T) {
	for i, pushTest := range pushTests {
		var actual logproto.PushRequest
		closer := io.NopCloser(strings.NewReader(pushTest.actual))

		err := DecodePushRequest(closer, &actual)
		require.NoError(t, err)

		require.Equalf(t, pushTest.expected, actual.Streams, "Push Test %d failed", i)
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
	require.NoError(t, marshal.WriteTailResponseJSON(legacy_loghttp.TailResponse{
		Streams: []logproto.Stream{
			{Labels: `{app="bar"}`, Entries: []logproto.Entry{{Timestamp: time.Unix(0, 2), Line: "2"}}},
		},
		DroppedEntries: []legacy_loghttp.DroppedEntry{
			{Timestamp: time.Unix(0, 1), Labels: `{app="foo"}`},
		},
	}, ws))
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
