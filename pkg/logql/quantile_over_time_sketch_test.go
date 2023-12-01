package logql

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/sketch"
)

func TestProbabilisticMQuantileMatrixSerialization(t *testing.T) {
	emptySketch := sketch.NewDDSketch()
	ddsketchBytes := make([]byte, 0)
	emptySketch.Encode(&ddsketchBytes, false)

	matrix := ProbabilisticQuantileMatrix([]ProbabilisticQuantileVector{
		[]ProbabilisticQuantileSample{
			{T: 0, F: emptySketch, Metric: []labels.Label{{Name: "foo", Value: "bar"}}},
		},
	})

	proto := &logproto.QuantileSketchMatrix{
		Values: []*logproto.QuantileSketchVector{
			{
				Samples: []*logproto.QuantileSketchSample{
					{
						TimestampMs: 0,
						F:           &logproto.QuantileSketch{Sketch: &logproto.QuantileSketch_Ddsketch{Ddsketch: ddsketchBytes}},
						Metric:      []*logproto.LabelPair{{Name: "foo", Value: "bar"}},
					},
				},
			},
		},
	}

	actual := matrix.ToProto()
	require.Equal(t, proto, actual)

	_, err := ProbabilisticQuantileMatrixFromProto(actual)
	require.NoError(t, err)
}
