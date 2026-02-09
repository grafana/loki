package xcap

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalUnmarshal(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)

	// Create statistics
	bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)
	latency := NewStatisticFloat64("latency.ms", AggregationTypeMin)
	success := NewStatisticFlag("success")
	requests := NewStatisticInt64("requests", AggregationTypeSum)

	ctx, r1 := StartRegion(ctx, "r1")
	r1.Record(bytesRead.Observe(100))
	r1.Record(latency.Observe(10.5))
	r1.Record(success.Observe(true))
	r1.Record(requests.Observe(1))
	r1.End()

	ctx, r2 := StartRegion(ctx, "r2")
	r2.Record(bytesRead.Observe(50))
	r2.Record(latency.Observe(3.1))
	r2.Record(requests.Observe(2))
	r2.End()

	_, r3 := StartRegion(ctx, "r3")
	r3.Record(bytesRead.Observe(300))
	r3.Record(success.Observe(false))
	r3.Record(requests.Observe(1))
	r3.End()

	capture.End()

	// Marshal to proto
	proto, err := toProtoCapture(capture)
	require.NoError(t, err)

	// Unmarshal from proto
	unmarshaled := &Capture{}
	require.NoError(t, fromProtoCapture(proto, unmarshaled))

	require.True(t, capturesEqual(capture, unmarshaled), "captures should be equal after marshal/unmarshal")
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

	return valuesEqual(obs1.Value, obs2.Value)
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
