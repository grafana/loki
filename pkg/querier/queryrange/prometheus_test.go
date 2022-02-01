package queryrange

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

var emptyStats = `"stats": {
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
	"summary": {
		"bytesProcessedPerSecond": 0,
		"execTime": 0,
		"linesProcessedPerSecond": 0,
		"queueTime": 0,
		"totalBytesProcessed":0,
		"totalLinesProcessed":0
	}
}`

func Test_encodePromResponse(t *testing.T) {
	for _, tt := range []struct {
		name string
		resp *LokiPromResponse
		want string
	}{
		{
			"matrix",
			&LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: string(queryrangebase.StatusSuccess),
					Data: queryrangebase.PrometheusData{
						ResultType: loghttp.ResultTypeMatrix,
						Result: []queryrangebase.SampleStream{
							{
								Labels: []logproto.LabelAdapter{
									{Name: "foo", Value: "bar"},
								},
								Samples: []logproto.Sample{
									{Value: 1, Timestamp: 1000},
									{Value: 1, Timestamp: 2000},
								},
							},
							{
								Labels: []logproto.LabelAdapter{
									{Name: "foo", Value: "buzz"},
								},
								Samples: []logproto.Sample{
									{Value: 4, Timestamp: 1000},
									{Value: 5, Timestamp: 2000},
								},
							},
						},
					},
				},
			},
			`{
				"status": "success",
				"data": {
					"resultType": "matrix",
					"result": [
						{
							"metric": {"foo": "bar"},
							"values": [[1, "1"],[2, "1"]]
						},
						{
							"metric": {"foo": "buzz"},
							"values": [[1, "4"],[2, "5"]]
						}
					],
					` + emptyStats + `
				}
			}`,
		},
		{
			"vector",
			&LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: string(queryrangebase.StatusSuccess),
					Data: queryrangebase.PrometheusData{
						ResultType: loghttp.ResultTypeVector,
						Result: []queryrangebase.SampleStream{
							{
								Labels: []logproto.LabelAdapter{
									{Name: "foo", Value: "bar"},
								},
								Samples: []logproto.Sample{
									{Value: 1, Timestamp: 1000},
								},
							},
							{
								Labels: []logproto.LabelAdapter{
									{Name: "foo", Value: "buzz"},
								},
								Samples: []logproto.Sample{
									{Value: 4, Timestamp: 1000},
								},
							},
						},
					},
				},
			},
			`{
				"status": "success",
				"data": {
					"resultType": "vector",
					"result": [
						{
							"metric": {"foo": "bar"},
							"value": [1, "1"]
						},
						{
							"metric": {"foo": "buzz"},
							"value": [1, "4"]
						}
					],
					` + emptyStats + `
				}
			}`,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			r, err := tt.resp.encode(context.Background())
			require.NoError(t, err)
			b, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			got := string(b)
			require.JSONEq(t, tt.want, got)
		})
	}
}
