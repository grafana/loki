package xcap

import (
	"context"
	"reflect"
	"testing"

	"github.com/grafana/loki/v3/pkg/xcap/statid"
	"github.com/stretchr/testify/require"
)

func TestMarshalUnmarshal(t *testing.T) {
	withTestStatisticRegistry(t)

	ctx, capture := NewCapture(context.Background(), nil)

	// Create statistics
	bytesRead := NewStatisticInt64(statid.ID(1), "bytes.read", AggregationTypeSum)
	latency := NewStatisticFloat64(statid.ID(2), "latency.ms", AggregationTypeMin)
	success := NewStatisticFlag(statid.ID(3), "success")
	requests := NewStatisticInt64(statid.ID(4), "requests", AggregationTypeSum)

	// Parent region with attributes and observations
	ctx, parentRegion := StartRegion(ctx, "parent")
	parentRegion.Record(bytesRead.Observe(100))
	parentRegion.Record(latency.Observe(10.5))
	parentRegion.Record(success.Observe(true))
	parentRegion.Record(requests.Observe(1))

	// Child region with attributes and observations
	ctx, childRegion := StartRegion(ctx, "child")
	childRegion.Record(bytesRead.Observe(50))
	childRegion.Record(latency.Observe(3.1))
	childRegion.Record(requests.Observe(2))
	childRegion.End()

	// Another sibling region
	_, siblingRegion := StartRegion(ctx, "sibling")
	siblingRegion.Record(bytesRead.Observe(300))
	siblingRegion.Record(success.Observe(false))
	siblingRegion.Record(requests.Observe(1))
	siblingRegion.End()

	parentRegion.End()
	capture.End()

	// Marshal to proto
	proto, err := toProtoCapture(capture)
	require.NoError(t, err)

	// Unmarshal from proto
	unmarshaled := &Capture{}
	require.NoError(t, fromProtoCapture(proto, unmarshaled))

	require.True(t, capturesEqual(capture, unmarshaled), "captures should be equal after marshal/unmarshal")
}

func TestMarshalAggregatesRegionsByName(t *testing.T) {
	withTestStatisticRegistry(t)

	ctx, capture := NewCapture(context.Background(), nil)

	bytesRead := NewStatisticInt64(statid.ID(1), "bytes.read", AggregationTypeSum)
	latency := NewStatisticFloat64(statid.ID(2), "latency.ms", AggregationTypeMin)
	requests := NewStatisticInt64(statid.ID(4), "requests", AggregationTypeSum)

	_, first := StartRegion(ctx, "reader")
	first.Record(bytesRead.Observe(100))
	first.Record(latency.Observe(10.5))
	first.End()

	_, second := StartRegion(ctx, "reader")
	second.Record(bytesRead.Observe(50))
	second.Record(latency.Observe(3.1))
	second.Record(requests.Observe(2))
	second.End()

	capture.End()

	protoCapture, err := toProtoCapture(capture)
	require.NoError(t, err)
	require.Len(t, protoCapture.Regions, 1)
	require.Equal(t, "reader", protoCapture.Regions[0].Name)

	unmarshaled := &Capture{}
	require.NoError(t, fromProtoCapture(protoCapture, unmarshaled))
	require.Len(t, unmarshaled.Regions(), 1)

	region := unmarshaled.Regions()[0]
	bytesReadObservation := region.observations[bytesRead.Key()]
	require.NotNil(t, bytesReadObservation)
	require.EqualValues(t, 150, bytesReadObservation.Value())
	require.Equal(t, 2, bytesReadObservation.Count)

	latencyObservation := region.observations[latency.Key()]
	require.NotNil(t, latencyObservation)
	require.Equal(t, 3.1, latencyObservation.Value())
	require.Equal(t, 2, latencyObservation.Count)

	requestsObservation := region.observations[requests.Key()]
	require.NotNil(t, requestsObservation)
	require.EqualValues(t, 2, requestsObservation.Value())
	require.Equal(t, 1, requestsObservation.Count)
}

func TestMarshalDropsRegionsWithoutObservations(t *testing.T) {
	withTestStatisticRegistry(t)

	ctx, capture := NewCapture(context.Background(), nil)

	_, empty := StartRegion(ctx, "empty")
	empty.End()

	_, observed := StartRegion(ctx, "observed")
	stat := NewStatisticInt64(statid.ID(1), "requests", AggregationTypeSum)
	observed.Record(stat.Observe(1))
	observed.End()

	capture.End()

	protoCapture, err := toProtoCapture(capture)
	require.NoError(t, err)
	require.Len(t, protoCapture.Regions, 1)
	require.Equal(t, "observed", protoCapture.Regions[0].Name)
}

func TestMarshalRejectsWireStatisticWithoutID(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)
	stat := NewStatisticInt64(statid.Invalid, "unregistered", AggregationTypeSum)

	_, region := StartRegion(ctx, "region")
	region.Record(stat.Observe(1))
	region.End()
	capture.End()

	_, err := toProtoCapture(capture)
	require.ErrorContains(t, err, "has no ID")
}

func TestMarshalOmitsLocalStatistics(t *testing.T) {
	withTestStatisticRegistry(t)

	ctx, capture := NewCapture(context.Background(), nil)
	wireStat := NewStatisticInt64(statid.ID(1), "wire", AggregationTypeSum)
	localStat := NewStatisticInt64(statid.Invalid, "local", AggregationTypeSum, Local())

	_, mixed := StartRegion(ctx, "mixed")
	mixed.Record(wireStat.Observe(1))
	mixed.Record(localStat.Observe(2))
	mixed.End()

	_, localOnly := StartRegion(ctx, "local-only")
	localOnly.Record(localStat.Observe(3))
	localOnly.End()

	capture.End()

	protoCapture, err := toProtoCapture(capture)
	require.NoError(t, err)
	require.Len(t, protoCapture.Regions, 1)
	require.Equal(t, "mixed", protoCapture.Regions[0].Name)
	require.Len(t, protoCapture.Regions[0].ObservationsV2, 1)
	require.Equal(t, uint32(wireStat.ID()), protoCapture.Regions[0].ObservationsV2[0].StatId)

	require.Equal(t, int64(5), Value[int64](capture, localStat))

	unmarshaled := &Capture{}
	require.NoError(t, fromProtoCapture(protoCapture, unmarshaled))
	require.Equal(t, int64(1), Value[int64](unmarshaled, wireStat))
	require.Equal(t, int64(0), Value[int64](unmarshaled, localStat))
}

// capturesEqual compares two captures for equality.
func capturesEqual(c1, c2 *Capture) bool {
	if c1 == nil && c2 == nil {
		return true
	}
	if c1 == nil || c2 == nil {
		return false
	}

	regions1 := c1.Regions()
	regions2 := c2.Regions()

	if len(regions1) != len(regions2) {
		return false
	}

	// Build a map of region names to regions for easier comparison
	regions1Map := make(map[string]*Region)
	regions2Map := make(map[string]*Region)

	for _, r := range regions1 {
		regions1Map[r.name] = r
	}
	for _, r := range regions2 {
		regions2Map[r.name] = r
	}

	// Compare regions by name
	for name, r1 := range regions1Map {
		r2, ok := regions2Map[name]
		if !ok {
			return false
		}

		if !regionsEqual(r1, r2) {
			return false
		}
	}

	return true
}

// regionsEqual compares two regions for equality.
func regionsEqual(r1, r2 *Region) bool {
	if r1.name != r2.name {
		return false
	}

	if r1.ended != r2.ended {
		return false
	}

	// Compare observations
	if len(r1.observations) != len(r2.observations) {
		return false
	}

	for key, obs1 := range r1.observations {
		obs2, ok := r2.observations[key]
		if !ok {
			return false
		}

		if !observationsEqual(obs1, obs2) {
			return false
		}
	}

	return true
}

func observationsEqual(obs1, obs2 *AggregatedObservation) bool {
	if obs1.Count != obs2.Count {
		return false
	}

	if !statisticsEqual(obs1.Statistic, obs2.Statistic) {
		return false
	}

	return valuesEqual(obs1.Value(), obs2.Value())
}

func statisticsEqual(s1, s2 Statistic) bool {
	if s1 == nil && s2 == nil {
		return true
	}
	if s1 == nil || s2 == nil {
		return false
	}

	return s1.Name() == s2.Name() &&
		s1.DataType() == s2.DataType() &&
		s1.Aggregation() == s2.Aggregation()
}

func valuesEqual(v1, v2 any) bool {
	if v1 == nil && v2 == nil {
		return true
	}
	if v1 == nil || v2 == nil {
		return false
	}

	return reflect.DeepEqual(v1, v2)
}
