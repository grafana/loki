package queryrange

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

var emptyStats = `"stats": {
	"index": {
		"postFilterChunks": 0,
		"totalChunks": 0,
		"shardsDuration": 0,
		"usedBloomFilters": false
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
		"splits": 0,
		"shards": 0,
		"subqueries": 0,
		"totalBytesProcessed":0,
		"totalEntriesReturned":0,
		"totalLinesProcessed":0,
		"totalStructuredMetadataBytesProcessed": 0,
        "totalPostFilterLines": 0
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
					Status:   queryrangebase.StatusSuccess,
					Warnings: []string{"this is a warning"},
					Data: queryrangebase.PrometheusData{
						ResultType: loghttp.ResultTypeMatrix,
						Result: []queryrangebase.SampleStream{
							{
								Labels: []logproto.LabelAdapter{
									{Name: "foo", Value: "bar"},
								},
								Samples: []logproto.LegacySample{
									{Value: 1, TimestampMs: 1000},
									{Value: 1, TimestampMs: 2000},
								},
							},
							{
								Labels: []logproto.LabelAdapter{
									{Name: "foo", Value: "buzz"},
								},
								Samples: []logproto.LegacySample{
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
				"warnings": ["this is a warning"],
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
					Status:   queryrangebase.StatusSuccess,
					Warnings: []string{"this is a warning"},
					Data: queryrangebase.PrometheusData{
						ResultType: loghttp.ResultTypeVector,
						Result: []queryrangebase.SampleStream{
							{
								Labels: []logproto.LabelAdapter{
									{Name: "foo", Value: "bar"},
								},
								Samples: []logproto.LegacySample{
									{Value: 1, TimestampMs: 1000},
								},
							},
							{
								Labels: []logproto.LabelAdapter{
									{Name: "foo", Value: "buzz"},
								},
								Samples: []logproto.LegacySample{
									{Value: 4, TimestampMs: 1000},
								},
							},
						},
					},
				},
			},
			`{
				"status": "success",
				"warnings": ["this is a warning"],
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
