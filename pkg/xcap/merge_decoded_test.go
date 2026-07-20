package xcap

import (
	"context"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	internal "github.com/grafana/loki/v3/pkg/xcap/internal/proto"
	"github.com/grafana/loki/v3/pkg/xcap/statid"
)

func TestCaptureMergeDecodedMatchesUnmarshalThenMerge(t *testing.T) {
	withTestStatisticRegistry(t)

	ctx, src := NewCapture(context.Background(), nil)
	statSum := NewStatisticInt64(statid.ID(1), "sum", AggregationTypeSum)
	statMin := NewStatisticFloat64(statid.ID(2), "min", AggregationTypeMin)
	statFlag := NewStatisticFlag(statid.ID(3), "flag")

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

func TestMergeDecodedSkipsUnknownStatisticID(t *testing.T) {
	wireCapture := &internal.Capture{
		Regions: []internal.Region{{
			Name: "worker.read",
			ObservationsV2: []internal.ObservationV2{{
				StatId:    uint32(statid.Count),
				Count:     1,
				ValueBits: 1,
			}},
		}},
	}
	data, err := proto.Marshal(wireCapture)
	require.NoError(t, err)

	decoded, err := DecodeBinary(data)
	require.NoError(t, err)

	before := UnknownStatisticObservations()
	_, capture := NewCapture(context.Background(), nil)
	capture.MergeDecoded(nil, decoded)
	require.Empty(t, capture.Value(NewStatisticInt64(statid.Invalid, "unknown", AggregationTypeSum)))
	require.Equal(t, before+1, UnknownStatisticObservations())
}

func TestMergeDecodedSkipsLegacyStatisticIndex(t *testing.T) {
	// Capture -> Region("worker.read") -> ObservationV2. The observation uses
	// the legacy field 1 statistic_id index, which is reserved in the ID-based
	// protocol. The new decoder retains count/value_bits but leaves StatId at
	// zero, causing the observation to be skipped.
	legacyWire := []byte{
		0x0a, 0x1c,
		0x0a, 0x0b, 'w', 'o', 'r', 'k', 'e', 'r', '.', 'r', 'e', 'a', 'd',
		0x52, 0x0d,
		0x08, 0x01,
		0x10, 0x01,
		0x19, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}

	decoded, err := DecodeBinary(legacyWire)
	require.NoError(t, err)

	before := UnknownStatisticObservations()
	_, capture := NewCapture(context.Background(), nil)
	capture.MergeDecoded(nil, decoded)
	require.Len(t, capture.Regions(), 1)
	require.Empty(t, capture.Regions()[0].Observations())
	require.Equal(t, before+1, UnknownStatisticObservations())
}
