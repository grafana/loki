// Package contains methods to marshal logqmodel types to queryrange Protobuf types.
// Its cousing is util/marshal which converts them to JSON.
package queryrange

import (
	"fmt"
	"io"

	"github.com/gogo/googleapis/google/rpc"
	"github.com/gogo/status"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/sketch"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
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

	return nil, fmt.Errorf("unsupported data type: %t", result.Data)
}
