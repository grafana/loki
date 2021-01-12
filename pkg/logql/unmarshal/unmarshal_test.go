package unmarshal

import (
	"fmt"
	"io/ioutil"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
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
		closer := ioutil.NopCloser(strings.NewReader(pushTest.actual))

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
