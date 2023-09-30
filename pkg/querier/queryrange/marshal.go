// Package contains methods to marshal logqmodel types to queryrange Protobuf types.
// Its cousing is util/marshal which converts them to JSON.
package queryrange

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"

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

func WriteResponse(req *http.Request, params logql.Params, v any, w http.ResponseWriter) error {
	if req.Header.Get("Accept") == ProtobufType {
		w.Header().Add("Content-Type", ProtobufType)
		return WriteResponseProtobuf(req, params, v, w)
	}

	w.Header().Add("Content-Type", JSONType)
	return marshal.WriteResponseJSON(req, v, w)
}

func WriteResponseProtobuf(req *http.Request, params logql.Params, v any, w http.ResponseWriter) error {
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
		return WriteVolumeResponseProtobuf(result, w)
	}
	return fmt.Errorf("unknown response type %T", v)
}

// WriteQueryResponseProtobuf marshals the promql.Value to queryrange QueryResonse and then
// writes it to the provided io.Writer.
func WriteQueryResponseProtobuf(params logql.Params, v logqlmodel.Result, w io.Writer) error {
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
func WriteVolumeResponseProtobuf(r *logproto.VolumeResponse, w io.Writer) error {
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
func ResultToResponse(result logqlmodel.Result, params logql.Params) (*QueryResponse, error) {
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
	case sketch.QuantileSketchMatrix:
		return &QueryResponse{
			Response: &QueryResponse_QuantileSketches{
				QuantileSketches: &QuantileSketchResponse{Response: data.ToProto()},
			},
		}, nil
	}

	return nil, fmt.Errorf("unsupported data type: %t", result.Data)
}

func ValueToResponse(v parser.Value, params logql.Params) (queryrangebase.Response, error) {
	switch data := v.(type) {
	case promql.Vector:
		sampleStream, err := queryrangebase.FromValue(data)
		if err != nil {
			return nil, err
		}

		return &LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: "success",
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeVector,
					Result:     sampleStream,
				},
			},
		}, nil
	case promql.Matrix:
		sampleStream, err := queryrangebase.FromValue(data)
		if err != nil {
			return nil, err
		}
		return &LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: "success",
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeMatrix,
					Result:     sampleStream,
				},
			},
		}, nil
	case promql.Scalar:
		sampleStream, err := queryrangebase.FromValue(data)
		if err != nil {
			return nil, err
		}

		return &LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: "success",
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeScalar,
					Result:     sampleStream,
				},
			},
		}, nil
	case logqlmodel.Streams:
		return &LokiResponse{
			Direction: params.Direction(),
			Limit:     params.Limit(),
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result:     data,
			},
			Status: "success",
		}, nil
	default:
		return nil, fmt.Errorf("unexpected praser.Value type: %T", v)
	}
}

func ResponseToQueryResponse(ctx context.Context, res queryrangebase.Response) (QueryResponse, error) {
	p := QueryResponse{}

	switch response := res.(type) {
	case *LokiPromResponse:
		p.Response = &QueryResponse_Prom{response}
	case *LokiResponse:
		p.Response = &QueryResponse_Streams{response}
	case *LokiSeriesResponse:
		p.Response = &QueryResponse_Series{response}
	case *MergedSeriesResponseView:
		mat, err := response.Materialize()
		if err != nil {
			return p, err
		}
		p.Response = &QueryResponse_Series{mat}
	case *LokiLabelNamesResponse:
		p.Response = &QueryResponse_Labels{response}
	case *IndexStatsResponse:
		p.Response = &QueryResponse_Stats{response}
	case *TopKSketchesResponse:
		p.Response = &QueryResponse_TopkSketches{response}
	case *QuantileSketchResponse:
		p.Response = &QueryResponse_QuantileSketches{response}
	default:
		return p, fmt.Errorf("invalid response format, got (%T)", res)
	}

	return p, nil
}
