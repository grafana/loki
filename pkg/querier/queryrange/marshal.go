// Package contains methods to marshal logqmodel types to queryrange Protobuf types.
// Its cousing is util/marshal which converts them to JSON.
package queryrange

import (
	"fmt"
	"io"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/prometheus/prometheus/promql"
)

const(
	ProtobufType                = `application/vnd.google.protobuf`
)

// WriteQueryResponseProtobuf marshals the promql.Value to v1 loghttp JSON and then
// writes it to the provided io.Writer.
func WriteQueryResponseProtobuf(params logql.LiteralParams, v logqlmodel.Result, w io.Writer) error {
	p, err := ResultToResponse(v, params)
	if err != nil {
		return err
	}

	buf, err := p.Marshal()
	if err != nil {
		return err
	}
	w.Write(buf)
	return nil
}

// WriteLabelResponseProtobuf marshals a logproto.LabelResponse to queryrange LokiLabelNamesResponse 
// and then writes it to the provided io.Writer.
func WriteLabelResponseProtobuf(version loghttp.Version, l logproto.LabelResponse, w io.Writer) error {
	p := QueryResponse{
		Response: &QueryResponse_Labels{
			Labels: &LokiLabelNamesResponse {
				Status:     "success",
				Data:       l.Values,
				Version: uint32(version),
				// Statistics: statResult,
			},
		},
	}
	buf, err := p.Marshal()
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

// WriteSeriesResponseProtobuf marshals a logproto.SeriesResponse to queryrange LokiSeriesResponse
// and then writes it to the provided io.Writer.
func WriteSeriesResponseProtobuf(version loghttp.Version, r logproto.SeriesResponse, w io.Writer) error {
	p := QueryResponse{
		Response: &QueryResponse_Series{
			Series: &LokiSeriesResponse{
				Status:     "success",
				Version:    uint32(version),
				Data:       r.Series,
				// Statistics: statResult,
			}},
	}
	buf, err := p.Marshal()
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

// WriteIndexStatsResponseProtobuf marshals a gatewaypb.Stats to queryrange IndexStatsResponse
// and then writes it to the provided io.Writer.
func WriteIndexStatsResponseProtobuf(r *stats.Stats, w io.Writer) error {
	p := QueryResponse{
		Response: &QueryResponse_Stats{
			Stats: &IndexStatsResponse{
				Response: r,
			}},
	}
	buf, err := p.Marshal()
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

// WriteIndexStatsResponseProtobuf marshals a logproto.VolumeResponse to queryrange.QueryResponse
// and then writes it to the provided io.Writer.
func WriteSeriesVolumeResponseProtobuf(r *logproto.VolumeResponse, w io.Writer) error {
	p := QueryResponse{
		Response: &QueryResponse_Volume{
			Volume: &VolumeResponse{
				Response: r,
			}},
	}
	buf, err := p.Marshal()
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

// ResultToResponse is the reverse of ResponseToResult in downstreamer.
func ResultToResponse(result logqlmodel.Result, params logql.LiteralParams) (*QueryResponse, error) {
	switch data := result.Data.(type) {
	case promql.Vector:
		sampleStream, err := queryrangebase.FromValue(data)
		if err != nil {
			return nil, err
		}

		return &QueryResponse{
			Response: &QueryResponse_Prom{
				Prom: &LokiPromResponse{
					Response: &queryrangebase.PrometheusResponse{
						Status: "success",
						Data: queryrangebase.PrometheusData{
							ResultType: loghttp.ResultTypeVector,
							Result:     sampleStream,
						},
					},
					Statistics: result.Statistics,
				},
				},
		}, nil
	case promql.Matrix:
		sampleStream, err := queryrangebase.FromValue(data)
		if err != nil {
			return nil, err
		}
		return &QueryResponse{
			Response: &QueryResponse_Prom{
				Prom: &LokiPromResponse{
					Response: &queryrangebase.PrometheusResponse{
						Status: "success",
						Data: queryrangebase.PrometheusData{
							ResultType: loghttp.ResultTypeMatrix,
							Result:     sampleStream,
						},
					},
					Statistics: result.Statistics,
				},
				},
		}, nil
	case promql.Scalar:
		sampleStream, err := queryrangebase.FromValue(data)
		if err != nil {
			return nil, err
		}

		return &QueryResponse{
			Response: &QueryResponse_Prom{
				Prom: &LokiPromResponse{
					Response: &queryrangebase.PrometheusResponse{
						Status: "success",
						Data: queryrangebase.PrometheusData{
							ResultType: loghttp.ResultTypeScalar,
							Result:     sampleStream,
						},
					},
					Statistics: result.Statistics,
				},
				},
		}, nil
	case logqlmodel.Streams:
		return &QueryResponse{
			Response: &QueryResponse_Streams{
				Streams: &LokiResponse{
					Direction: params.Direction(),
					Limit:     params.Limit(),
					Data: LokiData{
						ResultType: loghttp.ResultTypeStream,
						Result:     data,
					},
					Status:     "success",
					Statistics: result.Statistics,
				},
			},
		}, nil
	}

	return nil, fmt.Errorf("unsupported data type: %t", result.Data)
}
