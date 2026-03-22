package sketch

import (
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/logproto"
)

const ValueTypeTopKMatrix = "topk_matrix"

type TopKVector struct {
	topk *Topk
	ts   uint64
}

// TopkMatrix is `promql.Value` and `parser.Value`
type TopKMatrix []TopKVector

// Type implements `promql.Value` and `parser.Value`
func (TopKMatrix) Type() parser.ValueType { return ValueTypeTopKMatrix }

// String implements `promql.Value` and `parser.Value`
func (TopKMatrix) String() string {
	return ""
}

func (s TopKMatrix) ToProto() (*logproto.TopKMatrix, error) {
	points := make([]*logproto.TopKMatrix_Vector, 0, len(s))
	for _, point := range s {
		topk, err := point.topk.ToProto()
		if err != nil {
			return nil, err
		}

		points = append(points, &logproto.TopKMatrix_Vector{Topk: topk, TimestampMs: int64(point.ts)})
	}

	return &logproto.TopKMatrix{Values: points}, nil
}

func TopKMatrixFromProto(proto *logproto.TopKMatrix) (TopKMatrix, error) {
	values := make(TopKMatrix, 0, len(proto.Values))
	for _, vector := range proto.Values {
		topk, err := TopkFromProto(vector.Topk)
		if err != nil {
			return nil, err
		}

		values = append(values, TopKVector{topk, uint64(vector.TimestampMs)})

	}

	return values, nil
}
