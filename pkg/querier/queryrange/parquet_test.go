package queryrange

import (
	"os"
	"testing"

	"github.com/parquet-go/parquet-go"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func TestEncodeMetricsParquet(t *testing.T) {
	resp := &LokiPromResponse{
		Response: &queryrangebase.PrometheusResponse{
			Status: loghttp.QueryStatusSuccess,
			Data: queryrangebase.PrometheusData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     sampleStreams,
			},
		},
	}

	f, err := os.CreateTemp("", "metrics-*.parquet")
	defer f.Close() // nolint:staticcheck

	require.NoError(t, err)
	err = encodeMetricsParquetTo(resp, f)
	require.NoError(t, err)

	rows, err := parquet.ReadFile[MetricRowType](f.Name())
	require.NoError(t, err)

	require.Len(t, rows, 3)
}

func TestEncodeLogsParquet(t *testing.T) {
	resp := &LokiResponse{
		Status:    loghttp.QueryStatusSuccess,
		Direction: logproto.FORWARD,
		Limit:     100,
		Version:   uint32(loghttp.VersionV1),
		Data: LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result:     logStreams,
		},
	}

	f, err := os.CreateTemp("", "logs-*.parquet")
	defer f.Close() // nolint:staticcheck

	require.NoError(t, err)
	err = encodeLogsParquetTo(resp, f)
	require.NoError(t, err)

	rows, err := parquet.ReadFile[LogStreamRowType](f.Name())
	require.NoError(t, err)

	require.Len(t, rows, 3)
}
