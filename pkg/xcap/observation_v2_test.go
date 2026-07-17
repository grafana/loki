package xcap

import (
	"context"
	"math"
	"testing"

	"github.com/grafana/loki/v3/pkg/xcap/internal/proto"
	"github.com/stretchr/testify/require"
)

func TestMarshalWritesFlattenedObservationsV2(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)
	intStat := NewStatisticInt64("int", AggregationTypeSum)
	floatStat := NewStatisticFloat64("float", AggregationTypeSum)
	boolStat := NewStatisticFlag("bool")
	negativeValue := int64(-42)

	_, region := StartRegion(ctx, "region")
	region.Record(intStat.Observe(negativeValue))
	region.Record(floatStat.Observe(3.5))
	region.Record(boolStat.Observe(true))
	region.End()
	capture.End()

	protoCapture, err := toProtoCapture(capture)
	require.NoError(t, err)
	require.Len(t, protoCapture.Regions, 1)
	require.Len(t, protoCapture.Regions[0].ObservationsV2, 3)

	observations := make(map[string]proto.ObservationV2, len(protoCapture.Regions[0].ObservationsV2))
	for _, observation := range protoCapture.Regions[0].ObservationsV2 {
		observations[protoCapture.Statistics[observation.StatisticId].Name] = observation
	}

	require.Equal(t, uint64(negativeValue), observations["int"].ValueBits)
	require.Equal(t, math.Float64bits(3.5), observations["float"].ValueBits)
	require.Equal(t, uint64(1), observations["bool"].ValueBits)

	data, err := capture.MarshalBinary()
	require.NoError(t, err)

	decoded := &Capture{}
	require.NoError(t, decoded.UnmarshalBinary(data))
	require.Equal(t, negativeValue, Value[int64](decoded, intStat))
	require.Equal(t, 3.5, Value[float64](decoded, floatStat))
	require.Equal(t, true, Value[bool](decoded, boolStat))
}

func TestUnmarshalObservationsV2(t *testing.T) {
	intStat := NewStatisticInt64("int", AggregationTypeSum)
	floatStat := NewStatisticFloat64("float", AggregationTypeSum)
	boolStat := NewStatisticFlag("bool")
	negativeValue := int64(-42)
	protoCapture := &proto.Capture{
		Statistics: []proto.Statistic{
			{Name: intStat.Name(), DataType: proto.DATA_TYPE_INT64, AggregationType: proto.AGGREGATION_TYPE_SUM},
			{Name: floatStat.Name(), DataType: proto.DATA_TYPE_FLOAT64, AggregationType: proto.AGGREGATION_TYPE_SUM},
			{Name: boolStat.Name(), DataType: proto.DATA_TYPE_BOOL, AggregationType: proto.AGGREGATION_TYPE_MAX},
		},
		Regions: []proto.Region{{
			Name: "region",
			ObservationsV2: []proto.ObservationV2{
				{StatisticId: 0, Count: 2, ValueBits: uint64(negativeValue)},
				{StatisticId: 1, Count: 3, ValueBits: math.Float64bits(3.5)},
				{StatisticId: 2, Count: 4, ValueBits: 1},
			},
		}},
	}

	capture := &Capture{}
	require.NoError(t, fromProtoCapture(protoCapture, capture))
	require.Len(t, capture.Regions(), 1)

	observations := capture.Regions()[0].observations
	require.Equal(t, int64(-42), observations[intStat.Key()].Value)
	require.Equal(t, 2, observations[intStat.Key()].Count)
	require.Equal(t, 3.5, observations[floatStat.Key()].Value)
	require.Equal(t, 3, observations[floatStat.Key()].Count)
	require.Equal(t, true, observations[boolStat.Key()].Value)
	require.Equal(t, 4, observations[boolStat.Key()].Count)
}
