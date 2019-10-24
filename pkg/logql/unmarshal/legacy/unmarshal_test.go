package unmarshal

import (
	"io/ioutil"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
)

// covers requests to /api/prom/push
var pushTests = []struct {
	expected []*logproto.Stream
	actual   string
}{
	{
		[]*logproto.Stream{
			{
				Entries: []logproto.Entry{
					{
						Timestamp: mustParse(time.RFC3339Nano, "2019-09-13T18:32:22.380001319Z"),
						Line:      "super line",
					},
				},
				Labels: `{test="test"}`,
			},
		},
		`{
			"streams":[
				{
					"labels":"{test=\"test\"}",
					"entries":[
						{
							"ts": "2019-09-13T18:32:22.380001319Z",
							"line": "super line"	
						}
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

func mustParse(l string, t string) time.Time {
	ret, err := time.Parse(l, t)
	if err != nil {
		log.Fatalf("Failed to parse %s", t)
	}

	return ret
}
