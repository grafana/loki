package marshal

import (
	"bytes"
	"log"
	"testing"
	"time"

	json "github.com/json-iterator/go"
	"github.com/stretchr/testify/require"

	loghttp "github.com/grafana/loki/pkg/loghttp/legacy"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)

// covers responses from /api/prom/query
var queryTests = []struct {
	actual   logql.Streams
	expected string
}{
	{
		logql.Streams{
			logproto.Stream{
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
			],
			"stats" : {
				"ingester" : {
					"compressedBytes": 0,
					"decompressedBytes": 0,
					"decompressedLines": 0,
					"headChunkBytes": 0,
					"headChunkLines": 0,
					"totalBatches": 0,
					"totalChunksMatched": 0,
					"totalDuplicates": 0,
					"totalLinesSent": 0,
					"totalReached": 0
				},
				"store": {
					"compressedBytes": 0,
					"decompressedBytes": 0,
					"decompressedLines": 0,
					"headChunkBytes": 0,
					"headChunkLines": 0,
					"chunksDownloadTime": 0,
					"totalChunksRef": 0,
					"totalChunksDownloaded": 0,
					"totalDuplicates": 0
				},
				"summary": {
					"bytesProcessedPerSecond": 0,
					"execTime": 0,
					"linesProcessedPerSecond": 0,
					"totalBytesProcessed":0,
					"totalLinesProcessed":0
				}
			}
		}`,
	},
}

// covers responses from /api/prom/label and /api/prom/label/{name}/values
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

// covers responses from /api/prom/tail and /api/prom/tail
var tailTests = []struct {
	actual   loghttp.TailResponse
	expected string
}{
	{
		loghttp.TailResponse{
			Streams: []logproto.Stream{
				{
					Entries: []logproto.Entry{
						{
							Timestamp: mustParse(time.RFC3339Nano, "2019-09-13T18:32:22.380001319Z"),
							Line:      "super line",
						},
					},
					Labels: "{test=\"test\"}",
				},
			},
			DroppedEntries: []loghttp.DroppedEntry{
				{
					Timestamp: mustParse(time.RFC3339Nano, "2019-09-13T18:32:22.380001319Z"),
					Labels:    "{test=\"test\"}",
				},
			},
		},
		`{
			"streams": [
				{
					"labels": "{test=\"test\"}",
					"entries": [
						{
							"ts": "2019-09-13T18:32:22.380001319Z",
							"line": "super line"
						}
					]
				}
			],
			"dropped_entries": [
				{
					"Timestamp": "2019-09-13T18:32:22.380001319Z",
					"Labels": "{test=\"test\"}"
				}
			]
		}`,
	},
}

func Test_WriteQueryResponseJSON(t *testing.T) {

	for i, queryTest := range queryTests {
		var b bytes.Buffer
		err := WriteQueryResponseJSON(logql.Result{Data: queryTest.actual}, &b)
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
		// marshal model object
		bytes, err := json.Marshal(tailTest.actual)
		require.NoError(t, err)

		testJSONBytesEqual(t, []byte(tailTest.expected), bytes, "Tail Test %d failed", i)
	}
}

func Test_QueryResponseMarshalLoop(t *testing.T) {
	for i, queryTest := range queryTests {
		var r map[string]interface{}

		err := json.Unmarshal([]byte(queryTest.expected), &r)
		require.NoError(t, err)

		jsonOut, err := json.Marshal(r)
		require.NoError(t, err)

		testJSONBytesEqual(t, []byte(queryTest.expected), jsonOut, "Query Marshal Loop %d failed", i)
	}
}

func Test_LabelResponseMarshalLoop(t *testing.T) {
	for i, labelTest := range labelTests {
		var r loghttp.LabelResponse

		err := json.Unmarshal([]byte(labelTest.expected), &r)
		require.NoError(t, err)

		jsonOut, err := json.Marshal(r)
		require.NoError(t, err)

		testJSONBytesEqual(t, []byte(labelTest.expected), jsonOut, "Label Marshal Loop %d failed", i)
	}
}

func Test_TailResponseMarshalLoop(t *testing.T) {
	for i, tailTest := range tailTests {
		var r loghttp.TailResponse

		err := json.Unmarshal([]byte(tailTest.expected), &r)
		require.NoError(t, err)

		jsonOut, err := json.Marshal(r)
		require.NoError(t, err)

		testJSONBytesEqual(t, []byte(tailTest.expected), jsonOut, "Tail Marshal Loop %d failed", i)
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
