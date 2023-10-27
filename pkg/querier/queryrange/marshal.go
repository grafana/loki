// Package contains methods to marshal logqmodel types to queryrange Protobuf types.
// Its cousing is util/marshal which converts them to JSON.
package queryrange

import (
	"context"
	"fmt"
	"io"

	"github.com/gogo/googleapis/google/rpc"
	"github.com/gogo/status"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/sketch"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/util/httpreq"
	"github.com/grafana/loki/pkg/util/querylimits"
)

const (
	JSONType     = `application/json; charset=utf-8`
	ProtobufType = `application/vnd.google.protobuf`
)

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
			Statistics: result.Statistics,
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
			Statistics: result.Statistics,
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
			Statistics: result.Statistics,
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

	return nil, fmt.Errorf("unsupported data type: %T", result.Data)
}

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

func QueryResponseUnwrap(res *QueryResponse) (queryrangebase.Response, error) {
	if res.Status != nil && res.Status.Code != int32(rpc.OK) {
		return nil, status.ErrorProto(res.Status)
	}

	switch concrete := res.Response.(type) {
	case *QueryResponse_Series:
		return concrete.Series, nil
	case *QueryResponse_Labels:
		return concrete.Labels, nil
	case *QueryResponse_Stats:
		return concrete.Stats, nil
	case *QueryResponse_Prom:
		return concrete.Prom, nil
	case *QueryResponse_Streams:
		return concrete.Streams, nil
	case *QueryResponse_Volume:
		return concrete.Volume, nil
	case *QueryResponse_TopkSketches:
		return concrete.TopkSketches, nil
	case *QueryResponse_QuantileSketches:
		return concrete.QuantileSketches, nil
	default:
		return nil, fmt.Errorf("unsupported QueryResponse response type, got (%T)", res.Response)
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
		return nil, fmt.Errorf("invalid response format, got (%T)", res)
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
		return &LabelRequest{
			LabelRequest: *concrete.Labels,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported request type, got (%T)", req.Request)
	}
}

func QueryRequestWrap(ctx context.Context, r queryrangebase.Request) (*QueryRequest, error) {

	result := &QueryRequest{}

	switch req := r.(type) {
	case *LokiSeriesRequest:
		result.Request = &QueryRequest_Series{Series: req}
	case *LabelRequest:
		result.Request = &QueryRequest_Labels{Labels: &req.LabelRequest}
	case *logproto.IndexStatsRequest:
		result.Request = &QueryRequest_Stats{Stats: req}
	case *logproto.VolumeRequest:
		result.Request = &QueryRequest_Volume{Volume: req}
	case *LokiInstantRequest:
		result.Request = &QueryRequest_Instant{Instant: req}
	case *LokiRequest:
		result.Request = &QueryRequest_Streams{Streams: req}
	default:
		return nil, fmt.Errorf("unsupported request type, got (%T)", r)
	}

	// Add query tags
	queryTags := getQueryTags(ctx)
	if queryTags != "" {
		result.Metadata = append(result.Metadata, &Header{Key: string(httpreq.QueryTagsHTTPHeader), Values: []string{queryTags}})
	}

	// Add actor path
	actor := httpreq.ExtractHeader(ctx, httpreq.LokiActorPathHeader)
	if actor != "" {
		result.Metadata = append(result.Metadata, &Header{Key: httpreq.LokiActorPathHeader, Values: []string{actor}})
	}

	// Add limits
	limits := querylimits.ExtractQueryLimitsContext(ctx)
	if limits != nil {
		encodedLimits, err := querylimits.MarshalQueryLimits(limits)
		if err != nil {
			return nil, err
		}
		result.Metadata = append(result.Metadata, &Header{Key: querylimits.HTTPHeaderQueryLimitsKey, Values: []string{string(encodedLimits)}})
	}

	// Add org ID
	orgID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}
	result.Metadata = append(result.Metadata, &Header{Key: user.OrgIDHeaderName, Values: []string{orgID}})

	return result, nil
}
