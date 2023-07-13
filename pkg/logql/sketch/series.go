package sketch

import (
	"github.com/axiomhq/hyperloglog"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/logproto"
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
		cms := &logproto.CountMinSketch{
			Depth: uint32(point.topk.sketch.depth),
			Width: uint32(point.topk.sketch.width),
		}
		cms.Counters = make([]uint32, 0, cms.Depth*cms.Width)
		for row := uint32(0); row < cms.Depth; row++ {
			cms.Counters = append(cms.Counters, point.topk.sketch.counters[row]...)
		}

		hllBytes, err := point.topk.hll.MarshalBinary()
		if err != nil {
			return nil, err
		}

		list := make([]*logproto.TopK_Pair, 0, len(*point.topk.heap))
		for _, node := range *point.topk.heap {
			pair := &logproto.TopK_Pair{
				Event: node.event,
				Count: node.count,
			}
			list = append(list, pair)
		}

		topk := &logproto.TopK{
			Cms:         cms,
			Hyperloglog: hllBytes,
			List:        list,
		}

		points = append(points, &logproto.TopKMatrix_Vector{Topk: topk, TimestampMs: int64(point.ts)})
	}

	return &logproto.TopKMatrix{Values: points}, nil
}

func FromProto(proto *logproto.TopKMatrix) (TopKMatrix, error) {
	values := make(TopKMatrix, 0, len(proto.Values))
	for _, vector := range proto.Values {
		cms := &CountMinSketch{
			depth: int(vector.Topk.Cms.Depth),
			width: int(vector.Topk.Cms.Width),
		}
		for row := 0; row < cms.depth; row++ {
			s := row * cms.width
			e := s + cms.width
			cms.counters = append(cms.counters, vector.Topk.Cms.Counters[s:e])
		}

		hll := hyperloglog.New()
		err := hll.UnmarshalBinary(vector.Topk.Hyperloglog)
		if err != nil {
			return nil, err
		}

		heap := &MinHeap{}
		for _, p := range vector.Topk.List {
			node := &node{
				event: p.Event,
				count: p.Count,
			}
			heap.Push(node)
		}

		// TODO(karsten): should we set expected cardinality as well?
		topk := &Topk{
			sketch: cms,
			hll:    hll,
			heap:   heap,
		}

		values = append(values, TopKVector{topk, uint64(vector.TimestampMs)})

	}

	return values, nil
}
