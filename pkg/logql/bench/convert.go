package bench

import (
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// ConvertResult converts a loghttp.ResultValue to a parser.Value.
// It dispatches based on the concrete type of the input.
func ConvertResult(v loghttp.ResultValue) (parser.Value, error) {
	switch r := v.(type) {
	case loghttp.Streams:
		return convertStreams(r), nil
	case loghttp.Matrix:
		return convertMatrix(r), nil
	case loghttp.Vector:
		return convertVector(r), nil
	case loghttp.Scalar:
		return convertScalar(r), nil
	default:
		return nil, fmt.Errorf("unknown result type: %T", v)
	}
}

// convertStreams converts a loghttp.Streams to logqlmodel.Streams.
func convertStreams(s loghttp.Streams) logqlmodel.Streams {
	return logqlmodel.Streams(s.ToProto())
}

// convertMatrix converts a loghttp.Matrix to promql.Matrix.
func convertMatrix(m loghttp.Matrix) promql.Matrix {
	result := make(promql.Matrix, len(m))
	for i, sampleStream := range m {
		floats := make([]promql.FPoint, len(sampleStream.Values))
		for j, pair := range sampleStream.Values {
			floats[j] = promql.FPoint{
				T: int64(pair.Timestamp),
				F: float64(pair.Value),
			}
		}
		result[i] = promql.Series{
			Metric: modelMetricToLabels(sampleStream.Metric),
			Floats: floats,
		}
	}
	return result
}

// convertVector converts a loghttp.Vector to promql.Vector.
func convertVector(v loghttp.Vector) promql.Vector {
	result := make(promql.Vector, len(v))
	for i, sample := range v {
		result[i] = promql.Sample{
			Metric: modelMetricToLabels(sample.Metric),
			T:      int64(sample.Timestamp),
			F:      float64(sample.Value),
		}
	}
	return result
}

// convertScalar converts a loghttp.Scalar to promql.Scalar.
func convertScalar(s loghttp.Scalar) promql.Scalar {
	return promql.Scalar{
		T: int64(s.Timestamp),
		V: float64(s.Value),
	}
}

// modelMetricToLabels converts a model.Metric to labels.Labels.
// model.Metric is map[model.LabelName]model.LabelValue where both are string types.
func modelMetricToLabels(metric model.Metric) labels.Labels {
	m := make(map[string]string, len(metric))
	for k, v := range metric {
		m[string(k)] = string(v)
	}
	return labels.FromMap(m)
}
