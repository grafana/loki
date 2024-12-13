package queryrange

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/opentracing/opentracing-go"
	"github.com/parquet-go/parquet-go"
	"github.com/prometheus/prometheus/promql/parser"

	serverutil "github.com/grafana/loki/v3/pkg/util/server"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func encodeResponseParquet(ctx context.Context, res queryrangebase.Response) (*http.Response, error) {
	sp, _ := opentracing.StartSpanFromContext(ctx, "codec.EncodeResponse")
	defer sp.Finish()

	var buf bytes.Buffer

	err := encodeResponseParquetTo(ctx, res, &buf)
	if err != nil {
		return nil, err
	}

	resp := http.Response{
		Header: http.Header{
			"Content-Type": []string{ParquetType},
		},
		Body:       io.NopCloser(&buf),
		StatusCode: http.StatusOK,
	}
	return &resp, nil
}

func encodeResponseParquetTo(_ context.Context, res queryrangebase.Response, w io.Writer) error {
	switch response := res.(type) {
	case *LokiPromResponse:
		return encodeMetricsParquetTo(response, w)
	case *LokiResponse:
		return encodeLogsParquetTo(response, w)
	default:
		return serverutil.UserError("request does not support Parquet responses")
	}
}

type MetricRowType struct {
	Timestamp int64             `parquet:"timestamp,timestamp(millisecond),delta"`
	Labels    map[string]string `parquet:"labels"`
	Value     float64           `parquet:"value"`
}

type LogStreamRowType struct {
	Timestamp int64             `parquet:"timestamp,timestamp(nanosecond),delta"`
	Labels    map[string]string `parquet:"labels"`
	Line      string            `parquet:"line,lz4"`
}

func encodeMetricsParquetTo(response *LokiPromResponse, w io.Writer) error {
	schema := parquet.SchemaOf(new(MetricRowType))
	writer := parquet.NewGenericWriter[MetricRowType](w, schema)

	for _, stream := range response.Response.Data.Result {
		lbls := make(map[string]string)
		for _, keyValue := range stream.Labels {
			lbls[keyValue.Name] = keyValue.Value
		}
		for _, sample := range stream.Samples {
			row := MetricRowType{
				Timestamp: sample.TimestampMs,
				Labels:    lbls,
				Value:     sample.Value,
			}
			if _, err := writer.Write([]MetricRowType{row}); err != nil {
				return err
			}
		}
	}
	return writer.Close()
}

func encodeLogsParquetTo(response *LokiResponse, w io.Writer) error {
	schema := parquet.SchemaOf(new(LogStreamRowType))
	writer := parquet.NewGenericWriter[LogStreamRowType](w, schema)

	for _, stream := range response.Data.Result {
		lbls, err := parser.ParseMetric(stream.Labels)
		if err != nil {
			return err
		}
		lblsMap := make(map[string]string)
		for _, keyValue := range lbls {
			lblsMap[keyValue.Name] = keyValue.Value
		}

		for _, entry := range stream.Entries {
			row := LogStreamRowType{
				Timestamp: entry.Timestamp.UnixNano(),
				Labels:    lblsMap,
				Line:      entry.Line,
			}
			if _, err := writer.Write([]LogStreamRowType{row}); err != nil {
				return err
			}
		}
	}

	return writer.Close()
}
