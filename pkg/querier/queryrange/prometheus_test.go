package queryrange

import (
	"context"
	"io"
	"testing"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/loghttp"
)

var emptyStats = `"stats": {
	"summary": {
		"bytesProcessedPerSecond": 0,
		"linesProcessedPerSecond": 0,
		"totalBytesProcessed": 0,
		"totalLinesProcessed": 0,
		"execTime": 0.0
	},
	"store": {
		"totalChunksRef": 0,
		"totalChunksDownloaded": 0,
		"chunksDownloadTime": 0,
		"headChunkBytes": 0,
		"headChunkLines": 0,
		"decompressedBytes": 0,
		"decompressedLines": 0,
		"compressedBytes": 0,
		"totalDuplicates": 0
	},
	"ingester": {
		"totalReached": 0,
		"totalChunksMatched": 0,
		"totalBatches": 0,
		"totalLinesSent": 0,
		"headChunkBytes": 0,
		"headChunkLines": 0,
		"decompressedBytes": 0,
		"decompressedLines": 0,
		"compressedBytes": 0,
		"totalDuplicates": 0
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
				Response: &queryrange.PrometheusResponse{
					Status: string(queryrange.StatusSuccess),
					Data: queryrange.PrometheusData{
						ResultType: loghttp.ResultTypeMatrix,
						Result: []queryrange.SampleStream{
							{
								Labels: []cortexpb.LabelAdapter{
									{Name: "foo", Value: "bar"},
								},
								Samples: []cortexpb.Sample{
									{Value: 1, TimestampMs: 1000},
									{Value: 1, TimestampMs: 2000},
								},
							},
							{
								Labels: []cortexpb.LabelAdapter{
									{Name: "foo", Value: "buzz"},
								},
								Samples: []cortexpb.Sample{
									{Value: 4, TimestampMs: 1000},
									{Value: 5, TimestampMs: 2000},
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
				Response: &queryrange.PrometheusResponse{
					Status: string(queryrange.StatusSuccess),
					Data: queryrange.PrometheusData{
						ResultType: loghttp.ResultTypeVector,
						Result: []queryrange.SampleStream{
							{
								Labels: []cortexpb.LabelAdapter{
									{Name: "foo", Value: "bar"},
								},
								Samples: []cortexpb.Sample{
									{Value: 1, TimestampMs: 1000},
								},
							},
							{
								Labels: []cortexpb.LabelAdapter{
									{Name: "foo", Value: "buzz"},
								},
								Samples: []cortexpb.Sample{
									{Value: 4, TimestampMs: 1000},
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
