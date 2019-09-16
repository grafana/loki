package legacy

import (
	"bytes"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/stretchr/testify/require"
)

var queryTests = []struct {
	actual   logql.Streams
	expected string
}{
	// basic test
	{
		logql.Streams{
			&logproto.Stream{
				Entries: []logproto.Entry{
					logproto.Entry{
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

var labelTests = []struct {
	actual   logproto.LabelResponse
	expected string
}{
	{
		logproto.LabelResponse{
			Values: []string{
				"label1",
				"test",
				"value",
			},
		},
		`{"values": ["label1", "test", "value"]}`,
	},
}

var tailTests = []struct {
	actual   logproto.TailResponse
	expected string
}{
	{
		logproto.TailResponse{
			Stream: &logproto.Stream{
				Entries: []logproto.Entry{
					logproto.Entry{
						Timestamp: mustParse(time.RFC3339Nano, "2019-09-13T18:32:22.380001319Z"),
						Line:      "super line",
					},
				},
				Labels: "{test=\"test\"}",
			},
			DroppedStreams: []*logproto.DroppedStream{
				&logproto.DroppedStream{
					From:   mustParse(time.RFC3339Nano, "2019-09-13T18:32:22.380001319Z"),
					To:     mustParse(time.RFC3339Nano, "2019-09-13T18:32:22.38000132Z"),
					Labels: "{test=\"test\"}",
				},
			},
		},
		// jpe confirm tail response format
		`{
			"stream": {
				"labels": "{test=\"test\"}",
				"entries": [
					{
						"ts": "2019-09-13T18:32:22.380001319Z",
						"line": "super line"	
					}
				]
			},
			"dropped_entries": [
				{
					"from": "2019-09-13T18:32:22.380001319Z",
					"to": "2019-09-13T18:32:22.38000132Z",
					"labels": "{test=\"test\"}"
				}
			]
		}`,
	},
}

func init() {

}

func Test_WriteQueryResponseJSON(t *testing.T) {

	for i, queryTest := range queryTests {
		var b bytes.Buffer
		err := WriteQueryResponseJSON(queryTest.actual, &b)
		require.NoError(t, err)

		testJSONBytesEqual(t, []byte(queryTest.expected), b.Bytes(), "Query Test %d failed", i)
	}
}

func Test_WriteLabelResponseJSON(t *testing.T) {

	for i, labelTest := range labelTests {
		var b bytes.Buffer
		err := WriteLabelResponseJSON(labelTest.actual, &b)
		require.NoError(t, err)

		testJSONBytesEqual(t, []byte(labelTest.expected), b.Bytes(), "Label Test %d failed", i)
	}
}

func Test_MarshalTailResponse(t *testing.T) {

	for i, tailTest := range tailTests {
		// convert logproto to model objects
		model := NewTailResponse(tailTest.actual)

		// marshal model object
		bytes, err := json.Marshal(model)
		require.NoError(t, err)

		testJSONBytesEqual(t, []byte(tailTest.expected), bytes, "Tail Test %d failed", i)
	}
}

func testJSONBytesEqual(t *testing.T, expected []byte, actual []byte, msg string, args ...interface{}) {
	var expectedValue map[string]interface{}
	err := json.Unmarshal(expected, &expectedValue)
	require.NoError(t, err)

	var actualValue map[string]interface{}
	err = json.Unmarshal(actual, &actualValue)
	require.NoError(t, err)

	require.Equalf(t, expectedValue, actualValue, msg, args)
}

func mustParse(l string, t string) time.Time {
	ret, err := time.Parse(l, t)
	if err != nil {
		log.Fatalf("Failed to parse %s", t)
	}

	return ret
}
