package sketch

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/vector"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

func TestQuantiles(t *testing.T) {
	// v controls the distribution of values along the curve, a greater v
	// value means there's a large distance between generated values
	vs := []float64{1.0, 5.0, 10.0}
	// s controls the exponential curve of the distribution
	// the higher the s values the faster the drop off from max value to lesser values
	// s must be > 1.0
	ss := []float64{1.01, 2.0, 3.0, 4.0}

	// T-Digest is too big for 1_000 samples. However, we did not optimize
	// the format for size.
	nSamples := []int{5_000, 10_000, 100_000, 1_000_000}

	factories := []struct {
		newSketch     QuantileSketchFactory
		name          string
		relativeError float64
	}{
		{newSketch: func() QuantileSketch { return NewDDSketch() }, name: "DDSketch", relativeError: 0.02},
		{newSketch: NewTDigestSketch, name: "T-Digest", relativeError: 0.05},
	}

	for _, tc := range factories {
		for _, samplesCount := range nSamples {
			for _, s := range ss {
				for _, v := range vs {
					t.Run(fmt.Sprintf("sketch=%s, s=%.2f, v=%.2f, events=%d", tc.name, s, v, samplesCount), func(t *testing.T) {
						sketch := tc.newSketch()

						r := rand.New(rand.NewSource(42))
						z := rand.NewZipf(r, s, v, 1_000)
						values := make(vector.HeapByMaxValue, 0)
						for i := 0; i < samplesCount; i++ {

							value := float64(z.Uint64())
							values = append(values, promql.Sample{F: value})
							sketch.Add(value)
						}
						sort.Sort(values)

						// Size
						var buf []byte
						var err error
						switch s := sketch.(type) {
						case *DDSketchQuantile:
							buf, err = proto.Marshal(s.DDSketch.ToProto())
							require.NoError(t, err)
						case *TDigestQuantile:
							buf, err = proto.Marshal(s.ToProto())
							require.NoError(t, err)
						}
						require.Less(t, len(buf), samplesCount*8)

						// Accuracy
						expected := logql.Quantile(0.99, values)
						actual, err := sketch.Quantile(0.99)
						require.NoError(t, err)
						require.InEpsilonf(t, expected, actual, tc.relativeError, "expected quantile %f, actual quantile %f", expected, actual)
					})
				}
			}
		}
	}
}
