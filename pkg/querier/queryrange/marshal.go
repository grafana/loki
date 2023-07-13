// Package contains methods to marshal logqmodel types to queryrange Protobuf types.
// Its cousing is util/marshal which converts them to JSON.
package queryrange

import (
	"fmt"
	"io"
	"net/http"

	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/sketch"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/util/marshal"
)

const (
	JSONType     = `application/json; charset=utf-8`
	ProtobufType = `application/vnd.google.protobuf`
)

func WriteResponse(req *http.Request, params *logql.LiteralParams, v any, w http.ResponseWriter) error {
	if req.Header.Get("Accept") == ProtobufType {
		w.Header().Add("Content-Type", ProtobufType)
		return WriteResponseProtobuf(req, params, v, w)
	}

	w.Header().Add("Content-Type", JSONType)
	return marshal.WriteResponseJSON(req, v, w)
}

func WriteResponseProtobuf(req *http.Request, params *logql.LiteralParams, v any, w http.ResponseWriter) error {
	switch result := v.(type) {
	case logqlmodel.Result:
		return WriteQueryResponseProtobuf(params, result, w)
	case *logproto.LabelResponse:
		version := loghttp.GetVersion(req.RequestURI)
		return WriteLabelResponseProtobuf(version, *result, w)
	case *logproto.SeriesResponse:
		version := loghttp.GetVersion(req.RequestURI)
		return WriteSeriesResponseProtobuf(version, *result, w)
	case *stats.Stats:
		return WriteIndexStatsResponseProtobuf(result, w)
	case *logproto.VolumeResponse:
		return WriteSeriesVolumeResponseProtobuf(result, w)
	}
	return fmt.Errorf("unknown response type %T", v)
}

// WriteQueryResponseProtobuf marshals the promql.Value to queryrange QueryResonse and then
// writes it to the provided io.Writer.
func WriteQueryResponseProtobuf(params *logql.LiteralParams, v logqlmodel.Result, w io.Writer) error {
	p, err := ResultToResponse(v, params)
	if err != nil {
		return err
	}

	buf, err := p.Marshal()
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

// WriteLabelResponseProtobuf marshals a logproto.LabelResponse to queryrange LokiLabelNamesResponse
// and then writes it to the provided io.Writer.
func WriteLabelResponseProtobuf(version loghttp.Version, l logproto.LabelResponse, w io.Writer) error {
	p := QueryResponse{
		Response: &QueryResponse_Labels{
			Labels: &LokiLabelNamesResponse{
				Status:  "success",
				Data:    l.Values,
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
				Status:  "success",
				Version: uint32(version),
				Data:    r.Series,
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
func ResultToResponse(result logqlmodel.Result, params *logql.LiteralParams) (*QueryResponse, error) {
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
				// Note: we are omitting the Version here because the Protobuf already defines a schema.
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
	case sketch.TopKMatrix:
		sk, err := data.ToProto()
		if err != nil {
			return nil, err
		}
		return &QueryResponse{
			Response: &QueryResponse_TopkSketches{
				TopkSketches: &TopKSketchesResponse{Response: sk},
			},
		}, nil
	}

	return nil, fmt.Errorf("unsupported data type: %t", result.Data)
}
