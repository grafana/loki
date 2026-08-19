package xcap

import (
	"context"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	internal "github.com/grafana/loki/v3/pkg/xcap/internal/proto"
)

func TestCaptureMergeDecodedMatchesUnmarshalThenMerge(t *testing.T) {
	ctx, src := NewCapture(context.Background(), nil)
	statSum := NewStatisticInt64("sum", AggregationTypeSum)
	statMin := NewStatisticFloat64("min", AggregationTypeMin)
	statFlag := NewStatisticFlag("flag")

	_, first := StartRegion(ctx, "worker.read")
	first.Record(statSum.Observe(10))
	first.Record(statMin.Observe(4.5))
	first.Record(statFlag.Observe(false))
	first.End()

	_, second := StartRegion(ctx, "worker.read")
	second.Record(statSum.Observe(20))
	second.Record(statMin.Observe(2.5))
	second.Record(statFlag.Observe(true))
	second.End()
	src.End()

	data, err := src.MarshalBinary()
	require.NoError(t, err)

	_, expected := NewCapture(context.Background(), nil)
	unmarshaled := &Capture{}
	require.NoError(t, unmarshaled.UnmarshalBinary(data))
	expected.Merge(nil, unmarshaled)

	decoded, err := DecodeBinary(data)
	require.NoError(t, err)
	_, actual := NewCapture(context.Background(), nil)
	actual.MergeDecoded(nil, decoded)

	require.True(t, capturesEqual(expected, actual))
}

func TestDecodeBinaryRejectsInvalidStatisticID(t *testing.T) {
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

	decoded, err := DecodeBinary(data)
	require.Error(t, err)
	require.Nil(t, decoded)
}
