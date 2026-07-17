package xcap

import (
	"context"
	"testing"

	"github.com/gogo/protobuf/proto"
	internal "github.com/grafana/loki/v3/pkg/xcap/internal/proto"
	"github.com/stretchr/testify/require"
)

func TestCaptureMergeBinaryMatchesUnmarshalThenMerge(t *testing.T) {
	ctx, src := NewCapture(context.Background(), nil)
	sum := NewStatisticInt64("sum", AggregationTypeSum)
	min := NewStatisticFloat64("min", AggregationTypeMin)
	flag := NewStatisticFlag("flag")

	_, first := StartRegion(ctx, "worker.read")
	first.Record(sum.Observe(10))
	first.Record(min.Observe(4.5))
	first.Record(flag.Observe(false))
	first.End()

	_, second := StartRegion(ctx, "worker.read")
	second.Record(sum.Observe(20))
	second.Record(min.Observe(2.5))
	second.Record(flag.Observe(true))
	second.End()
	src.End()

	data, err := src.MarshalBinary()
	require.NoError(t, err)

	_, expected := NewCapture(context.Background(), nil)
	decoded := &Capture{}
	require.NoError(t, decoded.UnmarshalBinary(data))
	expected.Merge(nil, decoded)

	_, actual := NewCapture(context.Background(), nil)
	require.NoError(t, actual.MergeBinary(nil, data))

	require.True(t, capturesEqual(expected, actual))
}

func TestCaptureMergeBinarySkipsLegacyV1Observations(t *testing.T) {
	// A Region named "worker" with an unknown V1 observations field (4).
	legacyV1Wire := []byte{
		0x0a, 0x0c,
		0x0a, 0x06, 'w', 'o', 'r', 'k', 'e', 'r',
		0x22, 0x02, 0x08, 0x00,
	}

	_, capture := NewCapture(context.Background(), nil)
	require.NoError(t, capture.MergeBinary(nil, legacyV1Wire))
	require.Len(t, capture.Regions(), 1)
	require.Empty(t, capture.Regions()[0].Observations())
}

func TestCaptureMergeBinaryDoesNotMutateOnInvalidCapture(t *testing.T) {
	_, capture := NewCapture(context.Background(), nil)
	wireCapture := &internal.Capture{
		Regions: []internal.Region{{
			Name: "worker.read",
			ObservationsV2: []internal.ObservationV2{{
				StatisticId: 1,
				Count:       1,
				ValueBits:   1,
			}},
		}},
	}
	data, err := proto.Marshal(wireCapture)
	require.NoError(t, err)

	require.Error(t, capture.MergeBinary(nil, data))
	require.Empty(t, capture.Regions())
}
