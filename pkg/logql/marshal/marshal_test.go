package marshal

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	json "github.com/json-iterator/go"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/loghttp"
	legacy "github.com/grafana/loki/pkg/loghttp/legacy"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)

// covers responses from /loki/api/v1/query_range and /loki/api/v1/query
var queryTests = []struct {
	actual   parser.Value
	expected string
}{
	{
		logql.Streams{
			logproto.Stream{
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
			"status": "success",
			"data": {
				"resultType": "streams",
				"result": [
					{
						"stream": {
							"test": "test"
						},
						"values":[
							[ "123456789012345", "super line" ]
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
			}
		}`,
	},
	// vector test
	{
		promql.Vector{
			{
				Point: promql.Point{
					T: 1568404331324,
					V: 0.013333333333333334,
				},
				Metric: []labels.Label{
					{
						Name:  "filename",
						Value: `/var/hostlog/apport.log`,
					},
					{
						Name:  "job",
						Value: "varlogs",
					},
				},
			},
			{
				Point: promql.Point{
					T: 1568404331324,
					V: 3.45,
				},
				Metric: []labels.Label{
					{
						Name:  "filename",
						Value: `/var/hostlog/syslog`,
					},
					{
						Name:  "job",
						Value: "varlogs",
					},
				},
			},
		},
		`{
			"data": {
			  "resultType": "vector",
			  "result": [
				{
				  "metric": {
					"filename": "\/var\/hostlog\/apport.log",
					"job": "varlogs"
				  },
				  "value": [
					1568404331.324,
					"0.013333333333333334"
				  ]
				},
				{
				  "metric": {
					"filename": "\/var\/hostlog\/syslog",
					"job": "varlogs"
				  },
				  "value": [
					1568404331.324,
					"3.45"
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
			},
			"status": "success"
		  }`,
	},
	// matrix test
	{
		promql.Matrix{
			{
				Points: []promql.Point{
					{
						T: 1568404331324,
						V: 0.013333333333333334,
					},
				},
				Metric: []labels.Label{
					{
						Name:  "filename",
						Value: `/var/hostlog/apport.log`,
					},
					{
						Name:  "job",
						Value: "varlogs",
					},
				},
			},
			{
				Points: []promql.Point{
					{
						T: 1568404331324,
						V: 3.45,
					},
					{
						T: 1568404331339,
						V: 4.45,
					},
				},
				Metric: []labels.Label{
					{
						Name:  "filename",
						Value: `/var/hostlog/syslog`,
					},
					{
						Name:  "job",
						Value: "varlogs",
					},
				},
			},
		},
		`{
			"data": {
			  "resultType": "matrix",
			  "result": [
				{
				  "metric": {
					"filename": "\/var\/hostlog\/apport.log",
					"job": "varlogs"
				  },
				  "values": [
					  [
						1568404331.324,
						"0.013333333333333334"
					  ]
					]
				},
				{
				  "metric": {
					"filename": "\/var\/hostlog\/syslog",
					"job": "varlogs"
				  },
				  "values": [
						[
							1568404331.324,
							"3.45"
						],
						[
							1568404331.339,
							"4.45"
						]
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
			},
			"status": "success"
		  }`,
	},
}

// covers responses from /loki/api/v1/labels and /loki/api/v1/label/{name}/values
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
		`{"status": "success", "data": ["label1", "test", "value"]}`,
	},
}

// covers responses from /loki/api/v1/tail
var tailTests = []struct {
	actual   legacy.TailResponse
	expected string
}{
	{
		legacy.TailResponse{
			Streams: []logproto.Stream{
				{
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 123456789012345),
							Line:      "super line",
						},
					},
					Labels: "{test=\"test\"}",
				},
			},
			DroppedEntries: []legacy.DroppedEntry{
				{
					Timestamp: time.Unix(0, 123456789022345),
					Labels:    "{test=\"test\"}",
				},
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
			],
			"dropped_entries": [
				{
					"timestamp": "123456789022345",
					"labels": {
						"test": "test"
					}
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
		// convert logproto to model objects
		model, err := NewTailResponse(tailTest.actual)
		require.NoError(t, err)

		// marshal model object
		bytes, err := json.Marshal(model)
		require.NoError(t, err)

		testJSONBytesEqual(t, []byte(tailTest.expected), bytes, "Tail Test %d failed", i)
	}
}

func Test_QueryResponseMarshalLoop(t *testing.T) {
	for i, queryTest := range queryTests {
		value, err := NewResultValue(queryTest.actual)
		require.NoError(t, err)

		q := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: value.Type(),
				Result:     value,
			},
		}
		var expected loghttp.QueryResponse

		bytes, err := json.Marshal(q)
		require.NoError(t, err)

		err = json.Unmarshal(bytes, &expected)
		require.NoError(t, err)

		require.Equalf(t, q, expected, "Query Marshal Loop %d failed", i)
	}
}

func Test_QueryResponseResultType(t *testing.T) {
	for i, queryTest := range queryTests {
		value, err := NewResultValue(queryTest.actual)
		require.NoError(t, err)

		switch value.Type() {
		case loghttp.ResultTypeStream:
			require.IsTypef(t, loghttp.Streams{}, value, "Incorrect type %d", i)
		case loghttp.ResultTypeMatrix:
			require.IsTypef(t, loghttp.Matrix{}, value, "Incorrect type %d", i)
		case loghttp.ResultTypeVector:
			require.IsTypef(t, loghttp.Vector{}, value, "Incorrect type %d", i)
		default:
			require.Fail(t, "Unknown result type %s", value.Type())
		}
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

func Test_WriteSeriesResponseJSON(t *testing.T) {

	for i, tc := range []struct {
		input    logproto.SeriesResponse
		expected string
	}{
		{
			logproto.SeriesResponse{
				Series: []logproto.SeriesIdentifier{
					{
						Labels: map[string]string{
							"a": "1",
							"b": "2",
						},
					},
					{
						Labels: map[string]string{
							"c": "3",
							"d": "4",
						},
					},
				},
			},
			`{"status":"success","data":[{"a":"1","b":"2"},{"c":"3","d":"4"}]}`,
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			var b bytes.Buffer
			err := WriteSeriesResponseJSON(tc.input, &b)
			require.NoError(t, err)

			testJSONBytesEqual(t, []byte(tc.expected), b.Bytes(), "Label Test %d failed", i)
		})
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
