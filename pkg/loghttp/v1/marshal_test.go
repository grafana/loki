package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/stretchr/testify/require"
)

var queryTests = []struct {
	actual   promql.Value
	expected string
}{
	// streams test
	{
		logql.Streams{
			&logproto.Stream{
				Entries: []logproto.Entry{
					logproto.Entry{
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
				]
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
			  ]
			},
			"status": "success"
		  }`,
	},
	// matrix test
	{
		promql.Matrix{
			{
				Points: []promql.Point{
					promql.Point{
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
					promql.Point{
						T: 1568404331324,
						V: 3.45,
					},
					promql.Point{
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
			  ]
			},
			"status": "success"
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
						Timestamp: time.Unix(0, 123456789012345),
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
				"stream": {
					"test": "test"
				},
				"values":[
					[ "123456789012345", "super line" ]
				]
			},
			"droppedStreams": [
				{
					"from": "2019-09-13T18:32:22.380001319Z",
					"to": "2019-09-13T18:32:22.38000132Z",
					"labels": {
						"test": "test"
					}
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

//
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

func testStream(t *testing.T, expectedValue *logproto.Stream, actualValue map[string]interface{}) {
	expectedStream := expectedValue

	expectedLabels, err := promql.ParseMetric(expectedStream.Labels)
	require.NoErrorf(t, err, "Failed to convert string labels to map")

	actualLabels, ok := actualValue["labels"].(map[string]interface{})
	require.Truef(t, ok, "Failed to convert labels object")

	require.Equalf(t, len(expectedLabels), len(actualLabels), "Labels have different lengths")
	for _, l := range expectedLabels {
		require.Equalf(t, l.Value, actualLabels[l.Name], "Label %s has different values", l.Name)
	}

	entries, ok := actualValue["entries"].([]interface{})
	require.Truef(t, ok, "Failed to convert entries object on stream")
	require.Equalf(t, len(expectedStream.Entries), len(entries), "Entries count different on stream")

	for j, entry := range entries {
		actualEntry, ok := entry.([]interface{})
		require.Truef(t, ok, "Failed to convert entry object on entry %d", j)

		expectedEntry := expectedStream.Entries[j]
		require.Equalf(t, expectedEntry.Line, actualEntry[1], "Lines not equal on stream %d", j)
		require.Equalf(t, fmt.Sprintf("%d", expectedEntry.Timestamp.UnixNano()), actualEntry[0], "Timestamps not equal on stream %d", j)
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
