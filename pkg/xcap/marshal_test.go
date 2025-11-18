package xcap

import (
	"context"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() *Capture
		wantErr bool
	}{
		{
			name: "empty capture",
			setup: func() *Capture {
				_, capture := NewCapture(context.Background(), nil)
				capture.End()
				return capture
			},
		},
		{
			name: "capture with single region",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), []attribute.KeyValue{
					attribute.String("capture.attr", "value"),
				})

				bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)
				ctx, region := StartRegion(ctx, "read", WithRegionAttributes(
					attribute.String("region.name", "read_operation"),
					attribute.Int64("region.id", 123),
				))
				region.Record(bytesRead.Observe(1024))
				region.Record(bytesRead.Observe(2048))
				region.End()
				capture.End()
				return capture
			},
		},
		{
			name: "capture with multiple regions and parent-child relationships",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)

				bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)
				latency := NewStatisticFloat64("latency.ms", AggregationTypeMin)
				success := NewStatisticFlag("success")

				// Parent region
				ctx, parentRegion := StartRegion(ctx, "parent", WithRegionAttributes(
					attribute.String("region.id", "parent-1"),
				))
				parentRegion.Record(bytesRead.Observe(100))
				parentRegion.Record(latency.Observe(10.5))
				parentRegion.Record(success.Observe(true))

				// Child region
				ctx, childRegion := StartRegion(ctx, "child", WithRegionAttributes(
					attribute.String("region.id", "child-1"),
				))
				childRegion.Record(bytesRead.Observe(50))
				childRegion.Record(latency.Observe(5.2))
				childRegion.End()

				parentRegion.End()
				capture.End()
				return capture
			},
		},
		{
			name: "capture with all data types and aggregation types",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)

				statSum := NewStatisticInt64("sum", AggregationTypeSum)
				statMin := NewStatisticInt64("min", AggregationTypeMin)
				statMax := NewStatisticInt64("max", AggregationTypeMax)
				statLast := NewStatisticInt64("last", AggregationTypeLast)
				statFirst := NewStatisticInt64("first", AggregationTypeFirst)
				statFloat := NewStatisticFloat64("float", AggregationTypeSum)
				statBool := NewStatisticFlag("flag")

				ctx, region := StartRegion(ctx, "all_types", WithRegionAttributes(
					attribute.String("str", "test"),
					attribute.Int64("int", 42),
					attribute.Float64("float", 3.14),
					attribute.Bool("bool", true),
				))

				region.Record(statSum.Observe(10))
				region.Record(statSum.Observe(20))
				region.Record(statMin.Observe(30))
				region.Record(statMin.Observe(15))
				region.Record(statMax.Observe(5))
				region.Record(statMax.Observe(25))
				region.Record(statLast.Observe(100))
				region.Record(statLast.Observe(200))
				region.Record(statFirst.Observe(300))
				region.Record(statFirst.Observe(400))
				region.Record(statFloat.Observe(1.5))
				region.Record(statFloat.Observe(2.5))
				region.Record(statBool.Observe(false))
				region.Record(statBool.Observe(true))

				region.End()
				capture.End()
				return capture
			},
		},
		{
			name: "capture with multiple regions sharing statistics",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)

				sharedStat := NewStatisticInt64("shared", AggregationTypeSum)

				ctx, region1 := StartRegion(ctx, "region1")
				region1.Record(sharedStat.Observe(100))
				region1.End()

				ctx, region2 := StartRegion(ctx, "region2")
				region2.Record(sharedStat.Observe(200))
				region2.End()

				capture.End()
				return capture
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := tt.setup()

			// Marshal to proto
			proto, err := ToProtoCapture(original)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToProtoCapture() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			// Unmarshal from proto
			unmarshaled, err := FromProtoCapture(proto)
			if err != nil {
				t.Errorf("FromProtoCapture() error = %v", err)
				return
			}

			// Compare captures
			if !capturesEqual(original, unmarshaled) {
				t.Errorf("captures are not equal after round-trip")
				t.Logf("Original regions: %d", len(original.Regions()))
				t.Logf("Unmarshaled regions: %d", len(unmarshaled.Regions()))
			}
		})
	}
}

func TestMarshalNilCapture(t *testing.T) {
	proto, err := ToProtoCapture(nil)
	if err != nil {
		t.Errorf("ToProtoCapture(nil) error = %v", err)
	}
	if proto != nil {
		t.Errorf("ToProtoCapture(nil) = %v, want nil", proto)
	}
}

func TestUnmarshalNilCapture(t *testing.T) {
	capture, err := FromProtoCapture(nil)
	if err != nil {
		t.Errorf("FromProtoCapture(nil) error = %v", err)
	}
	if capture != nil {
		t.Errorf("FromProtoCapture(nil) = %v, want nil", capture)
	}
}

func TestMarshalUnmarshalAllAggregationTypes(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)

	aggregations := []struct {
		name string
		agg  AggregationType
	}{
		{"sum", AggregationTypeSum},
		{"min", AggregationTypeMin},
		{"max", AggregationTypeMax},
		{"last", AggregationTypeLast},
		{"first", AggregationTypeFirst},
	}

	for _, agg := range aggregations {
		stat := NewStatisticInt64(agg.name, agg.agg)
		var region *Region
		ctx, region = StartRegion(ctx, "region_"+agg.name)
		region.Record(stat.Observe(10))
		region.Record(stat.Observe(20))
		region.End()
	}

	capture.End()

	proto, err := ToProtoCapture(capture)
	if err != nil {
		t.Fatalf("ToProtoCapture() error = %v", err)
	}

	unmarshaled, err := FromProtoCapture(proto)
	if err != nil {
		t.Fatalf("FromProtoCapture() error = %v", err)
	}

	if !capturesEqual(capture, unmarshaled) {
		t.Error("captures are not equal after round-trip")
	}
}

func TestMarshalUnmarshalAllDataTypes(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)

	intStat := NewStatisticInt64("int64", AggregationTypeSum)
	floatStat := NewStatisticFloat64("float64", AggregationTypeSum)
	boolStat := NewStatisticFlag("bool")

	ctx, region := StartRegion(ctx, "test")
	region.Record(intStat.Observe(42))
	region.Record(floatStat.Observe(3.14))
	region.Record(boolStat.Observe(true))
	region.End()

	capture.End()

	proto, err := ToProtoCapture(capture)
	if err != nil {
		t.Fatalf("ToProtoCapture() error = %v", err)
	}

	unmarshaled, err := FromProtoCapture(proto)
	if err != nil {
		t.Fatalf("FromProtoCapture() error = %v", err)
	}

	if !capturesEqual(capture, unmarshaled) {
		t.Error("captures are not equal after round-trip")
	}
}

func TestMarshalUnmarshalAllAttributeTypes(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)

	ctx, region := StartRegion(ctx, "test", WithRegionAttributes(
		attribute.String("str", "test"),
		attribute.Int64("int", 42),
		attribute.Float64("float", 3.14),
		attribute.Bool("bool", true),
	))
	region.End()

	capture.End()

	proto, err := ToProtoCapture(capture)
	if err != nil {
		t.Fatalf("ToProtoCapture() error = %v", err)
	}

	unmarshaled, err := FromProtoCapture(proto)
	if err != nil {
		t.Fatalf("FromProtoCapture() error = %v", err)
	}

	if !capturesEqual(capture, unmarshaled) {
		t.Error("captures are not equal after round-trip")
	}
}

func TestMarshalUnmarshalParentChildRelationships(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)

	// Create parent region
	ctx, parentRegion := StartRegion(ctx, "parent")
	parentID := parentRegion.id

	// Create child region
	_, childRegion := StartRegion(ctx, "child")

	parentRegion.End()
	childRegion.End()
	capture.End()

	// Verify parent-child relationship
	if childRegion.parentID != parentID {
		t.Errorf("childRegion.parentID = %v, want %v", childRegion.parentID, parentID)
	}

	proto, err := ToProtoCapture(capture)
	if err != nil {
		t.Fatalf("ToProtoCapture() error = %v", err)
	}

	unmarshaled, err := FromProtoCapture(proto)
	if err != nil {
		t.Fatalf("FromProtoCapture() error = %v", err)
	}

	// Find regions by name
	var unmarshaledParent, unmarshaledChild *Region
	for _, r := range unmarshaled.Regions() {
		if r.name == "parent" {
			unmarshaledParent = r
		} else if r.name == "child" {
			unmarshaledChild = r
		}
	}

	if unmarshaledParent == nil || unmarshaledChild == nil {
		t.Fatal("failed to find regions after unmarshaling")
	}

	// Verify parent-child relationship is preserved
	if unmarshaledChild.parentID != unmarshaledParent.id {
		t.Errorf("unmarshaledChild.parentID = %v, want %v", unmarshaledChild.parentID, unmarshaledParent.id)
	}
}

// capturesEqual compares two captures for equality, ignoring IDs which will be different
// but preserving parent-child relationships.
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
	idMap := make(map[identifier]identifier) // maps c1 IDs to c2 IDs

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

		// Map IDs
		idMap[r1.id] = r2.id

		if !regionsEqual(r1, r2, idMap) {
			return false
		}
	}

	// Verify parent-child relationships
	for name, r1 := range regions1Map {
		r2 := regions2Map[name]
		if r1.parentID.IsZero() != r2.parentID.IsZero() {
			return false
		}
		if !r1.parentID.IsZero() {
			expectedParentID := idMap[r1.parentID]
			if r2.parentID != expectedParentID {
				return false
			}
		}
	}

	return true
}

// regionsEqual compares two regions for equality, ignoring IDs.
func regionsEqual(r1, r2 *Region, idMap map[identifier]identifier) bool {
	if r1.name != r2.name {
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
