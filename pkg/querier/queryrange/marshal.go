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
	r, err := ResultToResponse(v, params)
	if err != nil {
		return err
	}

	p, err := QueryResponseWrap(r)
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
				Status: "success",
				Data:   l.Values,
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

// ResultToResponse is the reverse of ResponseToResult below.
func ResultToResponse(result logqlmodel.Result, params logql.Params) (queryrangebase.Response, error) {
	switch data := result.Data.(type) {
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
			Status:     "success",
			Statistics: result.Statistics,
		}, nil
	case sketch.TopKMatrix:
		sk, err := data.ToProto()
		return &TopKSketchesResponse{Response: sk}, err
	case sketch.QuantileSketchMatrix:
		return &QuantileSketchResponse{Response: data.ToProto()}, nil
	}

	return nil, fmt.Errorf("unsupported data type: %t", result.Data)
}

// TODO: we probably should get rid off logqlmodel.Result and user queryrangebase.Response instead.
func ResponseToResult(resp queryrangebase.Response) (logqlmodel.Result, error) {
	switch r := resp.(type) {
	case *LokiResponse:
		if r.Error != "" {
			return logqlmodel.Result{}, fmt.Errorf("%s: %s", r.ErrorType, r.Error)
		}

		streams := make(logqlmodel.Streams, 0, len(r.Data.Result))

		for _, stream := range r.Data.Result {
			streams = append(streams, stream)
		}

		return logqlmodel.Result{
			Statistics: r.Statistics,
			Data:       streams,
			Headers:    resp.GetHeaders(),
		}, nil

	case *LokiPromResponse:
		if r.Response.Error != "" {
			return logqlmodel.Result{}, fmt.Errorf("%s: %s", r.Response.ErrorType, r.Response.Error)
		}
		if r.Response.Data.ResultType == loghttp.ResultTypeVector {
			return logqlmodel.Result{
				Statistics: r.Statistics,
				Data:       sampleStreamToVector(r.Response.Data.Result),
				Headers:    resp.GetHeaders(),
			}, nil
		}
		return logqlmodel.Result{
			Statistics: r.Statistics,
			Data:       sampleStreamToMatrix(r.Response.Data.Result),
			Headers:    resp.GetHeaders(),
		}, nil
	case *TopKSketchesResponse:
		matrix, err := sketch.TopKMatrixFromProto(r.Response)
		if err != nil {
			return logqlmodel.Result{}, fmt.Errorf("cannot decode topk sketch: %w", err)
		}

		return logqlmodel.Result{
			Data:    matrix,
			Headers: resp.GetHeaders(),
		}, nil
	case *QuantileSketchResponse:
		matrix, err := sketch.QuantileSketchMatrixFromProto(r.Response)
		if err != nil {
			return logqlmodel.Result{}, fmt.Errorf("cannot decode quantile sketch: %w", err)
		}
		return logqlmodel.Result{
			Data:    matrix,
			Headers: resp.GetHeaders(),
		}, nil
	default:
		return logqlmodel.Result{}, fmt.Errorf("cannot decode (%T)", resp)
	}
}

func QueryResponseWrap(res queryrangebase.Response) (*QueryResponse, error) {
	p := &QueryResponse{}

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

func QueryRequestUnwrap(req *QueryRequest) (queryrangebase.Request, error) {
	if req == nil {
		return nil, nil
	}

	switch concrete := req.Request.(type) {
	case *QueryRequest_Series:
		return concrete.Series, nil
	case *QueryRequest_Instant:
		return concrete.Instant, nil
	case *QueryRequest_Stats:
		return concrete.Stats, nil
	case *QueryRequest_Volume:
		return concrete.Volume, nil
	case *QueryRequest_Streams:
		return concrete.Streams, nil
	case *QueryRequest_Labels:
		return concrete.Labels, nil
	default:
		return nil, fmt.Errorf("unsupported request type, got (%t)", req.Request)
	}
}

func QueryRequestWrap(r queryrangebase.Request) (*QueryRequest, error) {
	switch req := r.(type) {
	case *LokiSeriesRequest:
		return &QueryRequest{
			Request: &QueryRequest_Series{
				Series: req,
			},
		}, nil
	case *LokiLabelNamesRequest:
		return &QueryRequest{
			Request: &QueryRequest_Labels{
				Labels: req,
			},
		}, nil
	case *logproto.IndexStatsRequest:
		return &QueryRequest{
			Request: &QueryRequest_Stats{
				Stats: req,
			},
		}, nil
	case *logproto.VolumeRequest:
		return &QueryRequest{
			Request: &QueryRequest_Volume{
				Volume: req,
			},
		}, nil
	case *LokiInstantRequest:
		return &QueryRequest{
			Request: &QueryRequest_Instant{
				Instant: req,
			},
		}, nil
	case *LokiRequest:
		return &QueryRequest{
			Request: &QueryRequest_Streams{
				Streams: req,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported request type, got (%t)", r)
	}
}
