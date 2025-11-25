package xcap

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestMarshalUnmarshal(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)

	// Create statistics
	bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)
	latency := NewStatisticFloat64("latency.ms", AggregationTypeMin)
	success := NewStatisticFlag("success")
	requests := NewStatisticInt64("requests", AggregationTypeSum)

	// Parent region with attributes and observations
	ctx, parentRegion := StartRegion(ctx, "parent", WithRegionAttributes(
		attribute.String("region.id", "parent-1"),
		attribute.String("operation", "query"),
	))
	parentRegion.Record(bytesRead.Observe(100))
	parentRegion.Record(latency.Observe(10.5))
	parentRegion.Record(success.Observe(true))
	parentRegion.Record(requests.Observe(1))

	// Child region with attributes and observations
	ctx, childRegion := StartRegion(ctx, "child", WithRegionAttributes(
		attribute.String("region.id", "child-1"),
		attribute.String("operation", "subquery"),
		attribute.Bool("cached", false),
	))
	childRegion.Record(bytesRead.Observe(50))
	childRegion.Record(latency.Observe(3.1))
	childRegion.Record(requests.Observe(2))
	childRegion.End()

	// Another sibling region
	_, siblingRegion := StartRegion(ctx, "sibling", WithRegionAttributes(
		attribute.String("region.id", "sibling-1"),
		attribute.Float64("weight", 0.5),
	))
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

// capturesEqual compares two captures for equality
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

	if r1.id != r2.id {
		return false
	}

	if r1.parentID != r2.parentID {
		return false
	}

	if !r1.startTime.Equal(r2.startTime) {
		return false
	}

	if !r1.endTime.Equal(r2.endTime) {
		return false
	}

	if r1.ended != r2.ended {
		return false
	}

	// Compare attributes
	if !attributesEqual(r1.attributes, r2.attributes) {
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

func attributesEqual(attrs1, attrs2 []attribute.KeyValue) bool {
	if len(attrs1) != len(attrs2) {
		return false
	}

	// Build maps for easier comparison
	attrs1Map := make(map[string]attribute.KeyValue)
	attrs2Map := make(map[string]attribute.KeyValue)

	for _, attr := range attrs1 {
		attrs1Map[string(attr.Key)] = attr
	}
	for _, attr := range attrs2 {
		attrs2Map[string(attr.Key)] = attr
	}

	for key, attr1 := range attrs1Map {
		attr2, ok := attrs2Map[key]
		if !ok {
			return false
		}

		if !attributeValuesEqual(attr1, attr2) {
			return false
		}
	}

	return true
}

func attributeValuesEqual(attr1, attr2 attribute.KeyValue) bool {
	if attr1.Key != attr2.Key {
		return false
	}

	if attr1.Value.Type() != attr2.Value.Type() {
		return false
	}

	switch attr1.Value.Type() {
	case attribute.STRING:
		return attr1.Value.AsString() == attr2.Value.AsString()
	case attribute.INT64:
		return attr1.Value.AsInt64() == attr2.Value.AsInt64()
	case attribute.FLOAT64:
		return attr1.Value.AsFloat64() == attr2.Value.AsFloat64()
	case attribute.BOOL:
		return attr1.Value.AsBool() == attr2.Value.AsBool()
	default:
		// For other types, compare string representation
		return attr1.Value.Emit() == attr2.Value.Emit()
	}
}

func observationsEqual(obs1, obs2 *AggregatedObservation) bool {
	if obs1.Count != obs2.Count {
		return false
	}

	if !statisticsEqual(obs1.Statistic, obs2.Statistic) {
		return false
	}

	// Compare values based on type
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

	// Use reflect for type-safe comparison
	return reflect.DeepEqual(v1, v2)
}
