package marshal

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"time"

	json "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	legacy "github.com/grafana/loki/v3/pkg/loghttp/legacy"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

const emptyStats = `{
	"index": {
		"postFilterChunks": 0,
		"totalChunks": 0,
		"shardsDuration": 0
	},
	"ingester" : {
		"store": {
			"chunksDownloadTime": 0,
			"congestionControlLatency": 0,
			"totalChunksRef": 0,
			"totalChunksDownloaded": 0,
			"chunkRefsFetchTime": 0,
			"queryReferencedStructuredMetadata": false,
			"pipelineWrapperFilteredLines": 0,
			"chunk" :{
				"compressedBytes": 0,
				"decompressedBytes": 0,
				"decompressedLines": 0,
				"decompressedStructuredMetadataBytes": 0,
				"headChunkBytes": 0,
				"headChunkLines": 0,
				"headChunkStructuredMetadataBytes": 0,
				"postFilterLines": 0,
				"totalDuplicates": 0
			}
		},
		"totalBatches": 0,
		"totalChunksMatched": 0,
		"totalLinesSent": 0,
		"totalReached": 0
	},
	"querier": {
		"store": {
			"chunksDownloadTime": 0,
			"congestionControlLatency": 0,
			"totalChunksRef": 0,
			"totalChunksDownloaded": 0,
			"chunkRefsFetchTime": 0,
			"queryReferencedStructuredMetadata": false,
			"pipelineWrapperFilteredLines": 0,
			"chunk" :{
				"compressedBytes": 0,
				"decompressedBytes": 0,
				"decompressedLines": 0,
				"decompressedStructuredMetadataBytes": 0,
				"headChunkBytes": 0,
				"headChunkLines": 0,
				"headChunkStructuredMetadataBytes": 0,
				"postFilterLines": 0,
				"totalDuplicates": 0
			}
		}
	},
	"cache": {
		"chunk": {
			"entriesFound": 0,
			"entriesRequested": 0,
			"entriesStored": 0,
			"bytesReceived": 0,
			"bytesSent": 0,
			"requests": 0,
			"downloadTime": 0,
			"queryLengthServed": 0
		},
		"index": {
			"entriesFound": 0,
			"entriesRequested": 0,
			"entriesStored": 0,
			"bytesReceived": 0,
			"bytesSent": 0,
			"requests": 0,
			"downloadTime": 0,
			"queryLengthServed": 0
		},
		"statsResult": {
			"entriesFound": 0,
			"entriesRequested": 0,
			"entriesStored": 0,
			"bytesReceived": 0,
			"bytesSent": 0,
			"requests": 0,
			"downloadTime": 0,
			"queryLengthServed": 0
		},
		"seriesResult": {
			"entriesFound": 0,
			"entriesRequested": 0,
			"entriesStored": 0,
			"bytesReceived": 0,
			"bytesSent": 0,
			"requests": 0,
			"downloadTime": 0,
			"queryLengthServed": 0
		},
		"labelResult": {
			"entriesFound": 0,
			"entriesRequested": 0,
			"entriesStored": 0,
			"bytesReceived": 0,
			"bytesSent": 0,
			"requests": 0,
			"downloadTime": 0,
			"queryLengthServed": 0
		},
		"volumeResult": {
			"entriesFound": 0,
			"entriesRequested": 0,
			"entriesStored": 0,
			"bytesReceived": 0,
			"bytesSent": 0,
			"requests": 0,
			"downloadTime": 0,
			"queryLengthServed": 0
		},
		"instantMetricResult": {
			"entriesFound": 0,
			"entriesRequested": 0,
			"entriesStored": 0,
			"bytesReceived": 0,
			"bytesSent": 0,
			"requests": 0,
			"downloadTime": 0,
			"queryLengthServed": 0
		},
		"result": {
			"entriesFound": 0,
			"entriesRequested": 0,
			"entriesStored": 0,
			"bytesReceived": 0,
			"bytesSent": 0,
			"requests": 0,
			"downloadTime": 0,
			"queryLengthServed": 0
		}
	},
	"summary": {
		"bytesProcessedPerSecond": 0,
		"execTime": 0,
		"linesProcessedPerSecond": 0,
		"queueTime": 0,
		"shards": 0,
		"splits": 0,
		"subqueries": 0,
		"totalBytesProcessed": 0,
		"totalEntriesReturned": 0,
		"totalLinesProcessed": 0,
		"totalStructuredMetadataBytesProcessed": 0,
		"totalPostFilterLines": 0
	}
}`

var queryTestWithEncodingFlags = []struct {
	actual        parser.Value
	encodingFlags httpreq.EncodingFlags
	expected      string
}{
	{
		actual: logqlmodel.Streams{
			logproto.Stream{
				Entries: []logproto.Entry{
					{
						Timestamp: time.Unix(0, 123456789012345),
						Line:      "super line",
					},
					{
						Timestamp: time.Unix(0, 123456789012346),
						Line:      "super line with labels",
						StructuredMetadata: []logproto.LabelAdapter{
							{Name: "foo", Value: "a"},
							{Name: "bar", Value: "b"},
						},
					},
					{
						Timestamp: time.Unix(0, 123456789012347),
						Line:      "super line with labels msg=text",
						StructuredMetadata: []logproto.LabelAdapter{
							{Name: "foo", Value: "a"},
							{Name: "bar", Value: "b"},
						},
						Parsed: []logproto.LabelAdapter{
							{Name: "msg", Value: "text"},
						},
					},
				},
				Labels: `{test="test"}`,
			},
		},
		encodingFlags: httpreq.NewEncodingFlags(httpreq.FlagCategorizeLabels),
		expected: fmt.Sprintf(`{
			"status": "success",
			"warnings": ["this is a warning"],
			"data": {
				"resultType": "streams",
				"encodingFlags": ["%s"],
				"result": [
					{
						"stream": {
							"test": "test"
						},
						"values":[
							[ "123456789012345", "super line", {}],
							[ "123456789012346", "super line with labels", {
								"structuredMetadata": {
									"foo": "a",
									"bar": "b"
								}
							}],
							[ "123456789012347", "super line with labels msg=text", {
								"structuredMetadata": {
									"foo": "a",
									"bar": "b"
								},
								"parsed": {
									"msg": "text"
								}
							}]
						]
					}
				],
				"stats" : %s
			}
		}`, httpreq.FlagCategorizeLabels, emptyStats),
	},
}

// covers responses from /loki/api/v1/query_range and /loki/api/v1/query
var queryTests = []struct {
	actual   parser.Value
	expected string
}{
	{
		logqlmodel.Streams{
			logproto.Stream{
				Entries: []logproto.Entry{
					{
						Timestamp: time.Unix(0, 123456789012345),
						Line:      "super line",
					},
					{
						Timestamp: time.Unix(0, 123456789012346),
						Line:      "super line with labels",
						StructuredMetadata: []logproto.LabelAdapter{
							{Name: "foo", Value: "a"},
							{Name: "bar", Value: "b"},
						},
					},
				},
				Labels: `{test="test"}`,
			},
		},
		fmt.Sprintf(`{
			"status": "success",
			"warnings": ["this is a warning"],
			"data": {
				"resultType": "streams",
				"result": [
					{
						"stream": {
							"test": "test"
						},
						"values":[
							[ "123456789012345", "super line"],
							[ "123456789012346", "super line with labels" ]
						]
					}
				],
				"stats" : %s
			}
		}`, emptyStats),
	},
	// vector test
	{
		promql.Vector{
			{
				T: 1568404331324,
				F: 0.013333333333333334,
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
				T: 1568404331324,
				F: 3.45,
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
		fmt.Sprintf(`{
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
			  "stats" : %s
            },
			"status": "success",
			"warnings": ["this is a warning"]
		  }`, emptyStats),
	},
	// matrix test
	{
		promql.Matrix{
			{
				Floats: []promql.FPoint{
					{
						T: 1568404331324,
						F: 0.013333333333333334,
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
				Floats: []promql.FPoint{
					{
						T: 1568404331324,
						F: 3.45,
					},
					{
						T: 1568404331339,
						F: 4.45,
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
		fmt.Sprintf(`{
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
			  "stats" : %s
			},
			"status": "success",
			"warnings": ["this is a warning"]
		  }`, emptyStats),
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
						{
							Timestamp: time.Unix(0, 123456789012346),
							Line:      "super line with labels",
							StructuredMetadata: []logproto.LabelAdapter{
								{Name: "foo", Value: "a"},
								{Name: "bar", Value: "b"},
							},
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
						[ "123456789012345", "super line"],
						[ "123456789012346", "super line with labels" ]
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

var tailTestWithEncodingFlags = []struct {
	actual        legacy.TailResponse
	encodingFlags httpreq.EncodingFlags
	expected      string
}{
	{
		actual: legacy.TailResponse{
			Streams: []logproto.Stream{
				{
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 123456789012345),
							Line:      "super line",
						},
						{
							Timestamp: time.Unix(0, 123456789012346),
							Line:      "super line with labels",
							StructuredMetadata: []logproto.LabelAdapter{
								{Name: "foo", Value: "a"},
								{Name: "bar", Value: "b"},
							},
						},
						{
							Timestamp: time.Unix(0, 123456789012347),
							Line:      "super line with labels msg=text",
							StructuredMetadata: []logproto.LabelAdapter{
								{Name: "foo", Value: "a"},
								{Name: "bar", Value: "b"},
							},
							Parsed: []logproto.LabelAdapter{
								{Name: "msg", Value: "text"},
							},
						},
					},
					Labels: `{test="test"}`,
				},
			},
			DroppedEntries: []legacy.DroppedEntry{
				{
					Timestamp: time.Unix(0, 123456789022345),
					Labels:    "{test=\"test\"}",
				},
			},
		},
		encodingFlags: httpreq.NewEncodingFlags(httpreq.FlagCategorizeLabels),
		expected: fmt.Sprintf(`{
			"streams": [
				{
					"stream": {
						"test": "test"
					},
					"values":[
						[ "123456789012345", "super line", {}],
						[ "123456789012346", "super line with labels", {
							"structuredMetadata": {
								"foo": "a",
								"bar": "b"
							}
						}],
						[ "123456789012347", "super line with labels msg=text", {
							"structuredMetadata": {
								"foo": "a",
								"bar": "b"
							},
							"parsed": {
								"msg": "text"
							}
						}]
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
			],
			"encodingFlags": ["%s"]
		}`, httpreq.FlagCategorizeLabels),
	},
}

func Test_WriteQueryResponseJSON(t *testing.T) {
	for i, queryTest := range queryTests {
		var b bytes.Buffer
		err := WriteQueryResponseJSON(queryTest.actual, []string{"this is a warning"}, stats.Result{}, &b, nil)
		require.NoError(t, err)

		require.JSONEqf(t, queryTest.expected, b.String(), "Query Test %d failed", i)
	}
	for i, queryTest := range queryTestWithEncodingFlags {
		var b bytes.Buffer
		err := WriteQueryResponseJSON(queryTest.actual, []string{"this is a warning"}, stats.Result{}, &b, queryTest.encodingFlags)
		require.NoError(t, err)

		require.JSONEqf(t, queryTest.expected, b.String(), "Query Test %d failed", i)
	}
}

func Test_WriteLabelResponseJSON(t *testing.T) {
	for i, labelTest := range labelTests {
		var b bytes.Buffer
		err := WriteLabelResponseJSON(labelTest.actual.GetValues(), &b)
		require.NoError(t, err)

		require.JSONEqf(t, labelTest.expected, b.String(), "Label Test %d failed", i)
	}
}

func Test_WriteQueryResponseJSONWithError(t *testing.T) {
	broken := logqlmodel.Result{
		Data: logqlmodel.Streams{
			logproto.Stream{
				Entries: []logproto.Entry{
					{
						Timestamp: time.Unix(0, 123456789012345),
						Line:      "super line",
					},
				},
				Labels: `{testtest"}`,
			},
		},
	}
	var b bytes.Buffer
	err := WriteQueryResponseJSON(broken.Data, nil, stats.Result{}, &b, nil)
	require.Error(t, err)
}

func Test_MarshalTailResponse(t *testing.T) {
	for i, tailTest := range tailTests {
		var b bytes.Buffer
		err := WriteTailResponseJSON(tailTest.actual, &b, nil)
		require.NoError(t, err)

		require.JSONEqf(t, tailTest.expected, b.String(), "Tail Test %d failed", i)
	}
	for i, tailTest := range tailTestWithEncodingFlags {
		var b bytes.Buffer
		err := WriteTailResponseJSON(tailTest.actual, &b, tailTest.encodingFlags)
		require.NoError(t, err)

		require.JSONEqf(t, tailTest.expected, b.String(), "Tail Test %d failed", i)
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

		require.JSONEqf(t, labelTest.expected, string(jsonOut), "Label Marshal Loop %d failed", i)
	}
}

func Test_TailResponseMarshalLoop(t *testing.T) {
	for i, tailTest := range tailTests {
		var r loghttp.TailResponse

		err := json.Unmarshal([]byte(tailTest.expected), &r)
		require.NoError(t, err)

		jsonOut, err := json.Marshal(r)
		require.NoError(t, err)

		require.JSONEqf(t, tailTest.expected, string(jsonOut), "Tail Marshal Loop %d failed", i)
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
						Labels: logproto.MustNewSeriesEntries("a", "1", "b", "2"),
					},
					{
						Labels: logproto.MustNewSeriesEntries("c", "3", "d", "4"),
					},
				},
			},
			`{"status":"success","data":[{"a":"1","b":"2"},{"c":"3","d":"4"}]}`,
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			var b bytes.Buffer
			err := WriteSeriesResponseJSON(tc.input.GetSeries(), &b)
			require.NoError(t, err)

			require.JSONEqf(t, tc.expected, b.String(), "Series Test %d failed", i)
		})
	}
}

func Test_WriteQueryResponseJSON_EncodeFlags(t *testing.T) {
	inputStream := logqlmodel.Streams{
		logproto.Stream{
			Labels: `{test="test"}`,
			Entries: []logproto.Entry{
				{
					Timestamp: time.Unix(0, 123456789012346),
					Line:      "super line",
				},
			},
		},
		logproto.Stream{
			Labels: `{test="test", foo="a", bar="b"}`,
			Entries: []logproto.Entry{
				{
					Timestamp:          time.Unix(0, 123456789012346),
					Line:               "super line with labels",
					StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "a", "bar", "b")),
				},
			},
		},
		logproto.Stream{
			Labels: `{test="test", foo="a", bar="b", msg="baz"}`,
			Entries: []logproto.Entry{
				{
					Timestamp:          time.Unix(0, 123456789012346),
					Line:               "super line with labels msg=baz",
					StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "a", "bar", "b")),
					Parsed:             logproto.FromLabelsToLabelAdapters(labels.FromStrings("msg", "baz")),
				},
			},
		},
	}

	for _, tc := range []struct {
		name        string
		encodeFlags httpreq.EncodingFlags
		expected    string
	}{
		{
			name: "uncategorized labels",
			expected: fmt.Sprintf(`{
				"status": "success",
				"data": {
					"resultType": "streams",
					"result": [
						{
							"stream": {
								"test": "test"
							},
							"values":[
								[ "123456789012346", "super line"]
							]
						},
						{
							"stream": {
								"test": "test",
								"foo": "a",
								"bar": "b"
							},
							"values":[
								[ "123456789012346", "super line with labels"]
							]
						},
						{
							"stream": {
								"test": "test",
								"foo": "a",
								"bar": "b",
								"msg": "baz"
							},
							"values":[
								[ "123456789012346", "super line with labels msg=baz"]
							]
						}
					],
					"stats" : %s
				}
			}`, emptyStats),
		},
		{
			name:        "categorized labels",
			encodeFlags: httpreq.NewEncodingFlags(httpreq.FlagCategorizeLabels),
			expected: fmt.Sprintf(`{
				"status": "success",
				"data": {
					"resultType": "streams",
					"encodingFlags": ["%s"],
					"result": [
						{
							"stream": {
								"test": "test"
							},
							"values":[
								[ "123456789012346", "super line", {}]
							]
						},
						{
							"stream": {
								"test": "test",
								"foo": "a",
								"bar": "b"
							},
							"values":[
								[ "123456789012346", "super line with labels", {
									"structuredMetadata": {
										"foo": "a",
										"bar": "b"
									}
								}]
							]
						},
						{
							"stream": {
								"test": "test",
								"foo": "a",
								"bar": "b",
								"msg": "baz"
							},
							"values":[
								[ "123456789012346", "super line with labels msg=baz", {
									"structuredMetadata": {
										"foo": "a",
										"bar": "b"
									},
                                    "parsed": {
										"msg": "baz"
									}
								}]
							]
						}
					],
					"stats" : %s
				}
			}`, httpreq.FlagCategorizeLabels, emptyStats),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			err := WriteQueryResponseJSON(inputStream, nil, stats.Result{}, &b, tc.encodeFlags)
			require.NoError(t, err)
			require.JSONEq(t, tc.expected, b.String())
		})
	}
}

// wrappedValue and its Generate method is used by quick to generate a random
// parser.Value.
type wrappedValue struct {
	parser.Value
}

func (w wrappedValue) Generate(rand *rand.Rand, _ int) reflect.Value {
	types := []string{
		loghttp.ResultTypeMatrix,
		loghttp.ResultTypeScalar,
		loghttp.ResultTypeStream,
		loghttp.ResultTypeVector,
	}
	t := types[rand.Intn(len(types))]

	switch t {
	case loghttp.ResultTypeMatrix:
		s, _ := quick.Value(reflect.TypeOf(promql.Series{}), rand)
		series, _ := s.Interface().(promql.Series)

		l, _ := quick.Value(reflect.TypeOf(labels.Labels{}), rand)
		series.Metric = l.Interface().(labels.Labels)

		matrix := promql.Matrix{series}
		return reflect.ValueOf(wrappedValue{matrix})
	case loghttp.ResultTypeScalar:
		q, _ := quick.Value(reflect.TypeOf(promql.Scalar{}), rand)
		return reflect.ValueOf(wrappedValue{q.Interface().(parser.Value)})
	case loghttp.ResultTypeStream:
		var streams logqlmodel.Streams
		for i := 0; i < rand.Intn(100); i++ {
			stream := logproto.Stream{
				Labels:  randLabels(rand).String(),
				Entries: randEntries(rand),
				Hash:    0,
			}

			streams = append(streams, stream)
		}
		return reflect.ValueOf(wrappedValue{streams})
	case loghttp.ResultTypeVector:
		var vector promql.Vector
		for i := 0; i < rand.Intn(100); i++ {
			v, _ := quick.Value(reflect.TypeOf(promql.Sample{}), rand)
			sample, _ := v.Interface().(promql.Sample)

			l, _ := quick.Value(reflect.TypeOf(labels.Labels{}), rand)
			sample.Metric = l.Interface().(labels.Labels)
			vector = append(vector, sample)
		}
		return reflect.ValueOf(wrappedValue{vector})

	}
	return reflect.ValueOf(nil)
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_")

func randLabel(rand *rand.Rand) labels.Label {
	var label labels.Label
	b := make([]rune, 10)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	b[0] = 'n'
	label.Name = string(b)

	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	b[0] = 'v'
	label.Value = string(b)

	return label
}

func randLabels(rand *rand.Rand) labels.Labels {
	var labels labels.Labels
	nLabels := rand.Intn(100)
	for i := 0; i < nLabels; i++ {
		labels = append(labels, randLabel(rand))
	}

	return labels
}

func randEntries(rand *rand.Rand) []logproto.Entry {
	var entries []logproto.Entry
	nEntries := rand.Intn(100)
	for i := 0; i < nEntries; i++ {
		l, _ := quick.Value(reflect.TypeOf(""), rand)
		entries = append(entries, logproto.Entry{Timestamp: time.Now(), Line: l.Interface().(string)})
	}

	return entries
}

func Test_EncodeResult_And_ResultValue_Parity(t *testing.T) {
	f := func(w wrappedValue) bool {
		var buf bytes.Buffer
		js := json.NewStream(json.ConfigFastest, &buf, 0)
		err := encodeResult(w.Value, js, nil)
		require.NoError(t, err)
		js.Flush()
		actual := buf.String()

		buf.Reset()
		v, err := NewResultValue(w.Value)
		require.NoError(t, err)
		js.WriteVal(v)
		js.Flush()
		expected := buf.String()

		require.JSONEq(t, expected, actual)
		return true
	}

	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

func Benchmark_Encode(b *testing.B) {
	buf := bytes.NewBuffer(nil)

	for n := 0; n < b.N; n++ {
		for _, queryTest := range queryTests {
			require.NoError(b, WriteQueryResponseJSON(queryTest.actual, nil, stats.Result{}, buf, nil))
			buf.Reset()
		}
	}
}

type WebsocketWriterFunc func(int, []byte) error

func (w WebsocketWriterFunc) WriteMessage(t int, d []byte) error { return w(t, d) }

func Test_WriteTailResponseJSON(t *testing.T) {
	require.NoError(t,
		WriteTailResponseJSON(legacy.TailResponse{
			Streams: []logproto.Stream{
				{Labels: `{app="foo"}`, Entries: []logproto.Entry{{Timestamp: time.Unix(0, 1), Line: `foobar`}}},
			},
			DroppedEntries: []legacy.DroppedEntry{
				{Timestamp: time.Unix(0, 2), Labels: `{app="dropped"}`},
			},
		},
			NewWebsocketJSONWriter(WebsocketWriterFunc(func(_ int, b []byte) error {
				require.Equal(t, `{"streams":[{"stream":{"app":"foo"},"values":[["1","foobar"]]}],"dropped_entries":[{"timestamp":"2","labels":{"app":"dropped"}}]}`, string(b))
				return nil
			})),
			nil,
		),
	)
}

func Test_WriteQueryPatternsResponseJSON(t *testing.T) {
	for i, tc := range []struct {
		input    *logproto.QueryPatternsResponse
		expected string
	}{
		{
			&logproto.QueryPatternsResponse{},
			`{"status":"success","data":[]}`,
		},
		{
			&logproto.QueryPatternsResponse{
				Series: []*logproto.PatternSeries{
					{
						Pattern: "foo <*> bar",
						Samples: []*logproto.PatternSample{
							{Timestamp: model.TimeFromUnix(1), Value: 1},
							{Timestamp: model.TimeFromUnix(2), Value: 2},
						},
					},
				},
			},
			`{"status":"success","data":[{"pattern":"foo <*> bar","samples":[[1,1],[2,2]]}]}`,
		},
		{
			&logproto.QueryPatternsResponse{
				Series: []*logproto.PatternSeries{
					{
						Pattern: "foo <*> bar",
						Samples: []*logproto.PatternSample{
							{Timestamp: model.TimeFromUnix(1), Value: 1},
							{Timestamp: model.TimeFromUnix(2), Value: 2},
						},
					},
					{
						Pattern: "foo <*> buzz",
						Samples: []*logproto.PatternSample{
							{Timestamp: model.TimeFromUnix(3), Value: 1},
							{Timestamp: model.TimeFromUnix(3), Value: 2},
						},
					},
				},
			},
			`{"status":"success","data":[{"pattern":"foo <*> bar","samples":[[1,1],[2,2]]},{"pattern":"foo <*> buzz","samples":[[3,1],[3,2]]}]}`,
		},
		{
			&logproto.QueryPatternsResponse{
				Series: []*logproto.PatternSeries{
					{
						Pattern: "foo <*> bar",
						Samples: []*logproto.PatternSample{},
					},
					{
						Pattern: "foo <*> buzz",
						Samples: []*logproto.PatternSample{},
					},
				},
			},
			`{"status":"success","data":[{"pattern":"foo <*> bar","samples":[]},{"pattern":"foo <*> buzz","samples":[]}]}`,
		},
	} {
		tc := tc
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			var b bytes.Buffer
			err := WriteQueryPatternsResponseJSON(tc.input, &b)
			require.NoError(t, err)
			got := b.String()
			require.JSONEqf(t, tc.expected, got, "Patterns Test %d failed", i)
		})
	}
}
