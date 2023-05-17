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
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/loghttp"
	legacy "github.com/grafana/loki/pkg/loghttp/legacy"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
)

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
						"store": {
							"chunksDownloadTime": 0,
							"totalChunksRef": 0,
							"totalChunksDownloaded": 0,
							"chunk" :{
								"compressedBytes": 0,
								"decompressedBytes": 0,
								"decompressedLines": 0,
								"headChunkBytes": 0,
								"headChunkLines": 0,
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
							"totalChunksRef": 0,
							"totalChunksDownloaded": 0,
							"chunk" :{
								"compressedBytes": 0,
								"decompressedBytes": 0,
								"decompressedLines": 0,
								"headChunkBytes": 0,
								"headChunkLines": 0,
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
							"downloadTime": 0
						},
						"index": {
							"entriesFound": 0,
							"entriesRequested": 0,
							"entriesStored": 0,
							"bytesReceived": 0,
							"bytesSent": 0,
							"requests": 0,
							"downloadTime": 0
						},
						"statsResult": {
							"entriesFound": 0,
							"entriesRequested": 0,
							"entriesStored": 0,
							"bytesReceived": 0,
							"bytesSent": 0,
							"requests": 0,
							"downloadTime": 0
						},
						"result": {
							"entriesFound": 0,
							"entriesRequested": 0,
							"entriesStored": 0,
							"bytesReceived": 0,
							"bytesSent": 0,
							"requests": 0,
							"downloadTime": 0
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
						"totalBytesProcessed":0,
                                                "totalEntriesReturned":0,
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
					"store": {
						"chunksDownloadTime": 0,
						"totalChunksRef": 0,
						"totalChunksDownloaded": 0,
						"chunk" :{
							"compressedBytes": 0,
							"decompressedBytes": 0,
							"decompressedLines": 0,
							"headChunkBytes": 0,
							"headChunkLines": 0,
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
						"totalChunksRef": 0,
						"totalChunksDownloaded": 0,
						"chunk" :{
							"compressedBytes": 0,
							"decompressedBytes": 0,
							"decompressedLines": 0,
							"headChunkBytes": 0,
							"headChunkLines": 0,
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
						"downloadTime": 0
					},
					"index": {
						"entriesFound": 0,
						"entriesRequested": 0,
						"entriesStored": 0,
						"bytesReceived": 0,
						"bytesSent": 0,
						"requests": 0,
						"downloadTime": 0
					},
					"statsResult": {
						"entriesFound": 0,
						"entriesRequested": 0,
						"entriesStored": 0,
						"bytesReceived": 0,
						"bytesSent": 0,
						"requests": 0,
						"downloadTime": 0
					},
					"result": {
						"entriesFound": 0,
						"entriesRequested": 0,
						"entriesStored": 0,
						"bytesReceived": 0,
						"bytesSent": 0,
						"requests": 0,
						"downloadTime": 0
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
					"totalBytesProcessed":0,
                                        "totalEntriesReturned":0,
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
					"store": {
						"chunksDownloadTime": 0,
						"totalChunksRef": 0,
						"totalChunksDownloaded": 0,
						"chunk" :{
							"compressedBytes": 0,
							"decompressedBytes": 0,
							"decompressedLines": 0,
							"headChunkBytes": 0,
							"headChunkLines": 0,
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
						"totalChunksRef": 0,
						"totalChunksDownloaded": 0,
						"chunk" :{
							"compressedBytes": 0,
							"decompressedBytes": 0,
							"decompressedLines": 0,
							"headChunkBytes": 0,
							"headChunkLines": 0,
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
						"downloadTime": 0
					},
					"index": {
						"entriesFound": 0,
						"entriesRequested": 0,
						"entriesStored": 0,
						"bytesReceived": 0,
						"bytesSent": 0,
						"requests": 0,
						"downloadTime": 0
					},
					"statsResult": {
						"entriesFound": 0,
						"entriesRequested": 0,
						"entriesStored": 0,
						"bytesReceived": 0,
						"bytesSent": 0,
						"requests": 0,
						"downloadTime": 0
					},
					"result": {
						"entriesFound": 0,
						"entriesRequested": 0,
						"entriesStored": 0,
						"bytesReceived": 0,
						"bytesSent": 0,
						"requests": 0,
						"downloadTime": 0
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
					"totalBytesProcessed":0,
                                        "totalEntriesReturned":0,
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
		err := WriteQueryResponseJSON(logqlmodel.Result{Data: queryTest.actual}, &b)
		require.NoError(t, err)

		require.JSONEqf(t, queryTest.expected, b.String(), "Query Test %d failed", i)
	}
}

func Test_WriteLabelResponseJSON(t *testing.T) {
	for i, labelTest := range labelTests {
		var b bytes.Buffer
		err := WriteLabelResponseJSON(labelTest.actual, &b)
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
	err := WriteQueryResponseJSON(broken, &b)
	require.Error(t, err)
}

func Test_MarshalTailResponse(t *testing.T) {
	for i, tailTest := range tailTests {
		// convert logproto to model objects
		model, err := NewTailResponse(tailTest.actual)
		require.NoError(t, err)

		// marshal model object
		bytes, err := json.Marshal(model)
		require.NoError(t, err)

		require.JSONEqf(t, tailTest.expected, string(bytes), "Tail Test %d failed", i)
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

			require.JSONEqf(t, tc.expected, b.String(), "Label Test %d failed", i)
		})
	}
}

// wrappedValue and its Generate method is used by quick to generate a random
// parser.Value.
type wrappedValue struct {
	parser.Value
}

func (w wrappedValue) Generate(rand *rand.Rand, size int) reflect.Value {
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
	for i := 0; i < rand.Intn(100); i++ {
		labels = append(labels, randLabel(rand))
	}

	return labels
}

func randEntries(rand *rand.Rand) []logproto.Entry {
	var entries []logproto.Entry
	for i := 0; i < rand.Intn(100); i++ {
		l, _ := quick.Value(reflect.TypeOf(""), rand)
		entries = append(entries, logproto.Entry{Timestamp: time.Now(), Line: l.Interface().(string)})
	}

	return entries
}

func Test_EncodeResult_And_ResultValue_Parity(t *testing.T) {
	f := func(w wrappedValue) bool {
		var buf bytes.Buffer
		js := json.NewStream(json.ConfigFastest, &buf, 0)
		err := encodeResult(w.Value, js)
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
			require.NoError(b, WriteQueryResponseJSON(logqlmodel.Result{Data: queryTest.actual}, buf))
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
			WebsocketWriterFunc(func(i int, b []byte) error {
				require.Equal(t, `{"streams":[{"stream":{"app":"foo"},"values":[["1","foobar"]]}],"dropped_entries":[{"timestamp":"2","labels":{"app":"dropped"}}]}`, string(b))
				return nil
			}),
		),
	)
}
