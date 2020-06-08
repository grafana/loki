package unmarshal

import (
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
