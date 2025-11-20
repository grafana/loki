package xcap

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestCapture_AddRegion(t *testing.T) {
	tests := []struct {
		name            string
		setup           func() *Capture
		expectedRegions []string
		expectedParents map[string]string // child -> parent mapping
	}{
		{
			name: "empty capture returns no regions",
			setup: func() *Capture {
				_, capture := NewCapture(context.Background(), nil)
				return capture
			},
		},
		{
			name: "capture with single region", // validates capture to region link
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)
				_, _ = StartRegion(ctx, "region1")
				return capture
			},
			expectedRegions: []string{"region1"},
		},
		{
			name: "capture with multiple regions", // also validates parent-child region links
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)
				ctx, _ = StartRegion(ctx, "region1")
				ctx, _ = StartRegion(ctx, "region2")
				_, _ = StartRegion(ctx, "region3")
				return capture
			},
			expectedRegions: []string{"region1", "region2", "region3"},
			expectedParents: map[string]string{
				"region2": "region1", // child ->  parent
				"region3": "region2",
			},
		},
		{
			name: "capture with complex links", // also validates parent-child region links
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)
				ctx1, _ := StartRegion(ctx, "region1")
				ctx2, _ := StartRegion(ctx1, "region2")
				ctx3, _ := StartRegion(ctx1, "region3")
				_, _ = StartRegion(ctx2, "region4")
				_, _ = StartRegion(ctx3, "region5")
				return capture
			},
			expectedRegions: []string{"region1", "region2", "region3", "region4", "region5"},
			expectedParents: map[string]string{
				"region2": "region1", // child ->  parent
				"region3": "region1",
				"region4": "region2",
				"region5": "region3",
			},
		},
		{
			name: "regions not added after End",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)
				_, _ = StartRegion(ctx, "region1")
				capture.End()
				// regions are not added after calling End()
				_, _ = StartRegion(ctx, "region2")
				return capture
			},
			expectedRegions: []string{"region1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := tt.setup()
			regions := capture.Regions()

			require.Equal(t, len(tt.expectedRegions), len(regions), "number of regions mismatch")

			gotRegions := make(map[string]*Region)
			for _, r := range regions {
				gotRegions[r.name] = r
			}

			require.ElementsMatch(t, tt.expectedRegions, slices.Collect(maps.Keys(gotRegions)))

			// Validate all regions have valid IDs
			for name, region := range gotRegions {
				require.True(t, region.id.IsValid(), "region %q should have valid ID", name)
			}

			// Validate parent-child relationships
			for childName, parentName := range tt.expectedParents {
				child, ok := gotRegions[childName]
				require.True(t, ok, "child region %q should exist", childName)
				parent, ok := gotRegions[parentName]
				require.True(t, ok, "parent region %q should exist", parentName)

				require.Equal(t, parent.id, child.parentID, "region %q should have parentID matching %q's ID", childName, parentName)
			}

			// Validate root regions (not in expectedParents) have zero parentID
			for name, region := range gotRegions {
				if _, hasParent := tt.expectedParents[name]; !hasParent {
					require.True(t, region.parentID.IsZero(), "root region %q should have zero parentID", name)
				}
			}
		})
	}
}

func TestCapture_GetAllStatistics(t *testing.T) {
	tests := []struct {
		name      string
		setup     func() *Capture
		wantStats []StatisticKey
	}{
		{
			name: "empty capture returns no statistics",
			setup: func() *Capture {
				_, capture := NewCapture(context.Background(), nil)
				return capture
			},
		},
		{
			name: "single region with single statistic",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)
				bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)
				_, region := StartRegion(ctx, "read")
				region.Record(bytesRead.Observe(1024))
				region.End()
				return capture
			},
			wantStats: []StatisticKey{
				{Name: "bytes.read", DataType: DataTypeInt64, Aggregation: AggregationTypeSum},
			},
		},
		{
			name: "single region with multiple statistics",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)
				bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)
				latency := NewStatisticFloat64("latency.ms", AggregationTypeMin)
				success := NewStatisticFlag("success")
				_, region := StartRegion(ctx, "read")
				region.Record(bytesRead.Observe(1024))
				region.Record(latency.Observe(10.5))
				region.Record(success.Observe(true))
				region.End()
				return capture
			},
			wantStats: []StatisticKey{
				{Name: "bytes.read", DataType: DataTypeInt64, Aggregation: AggregationTypeSum},
				{Name: "latency.ms", DataType: DataTypeFloat64, Aggregation: AggregationTypeMin},
				{Name: "success", DataType: DataTypeBool, Aggregation: AggregationTypeMax},
			},
		},
		{
			name: "multiple regions with same statistic",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)
				bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)

				ctx, region1 := StartRegion(ctx, "read1")
				region1.Record(bytesRead.Observe(1024))
				region1.End()

				_, region2 := StartRegion(ctx, "read2")
				region2.Record(bytesRead.Observe(2048))
				region2.End()

				return capture
			},
			wantStats: []StatisticKey{
				{Name: "bytes.read", DataType: DataTypeInt64, Aggregation: AggregationTypeSum},
			},
		},
		{
			name: "multiple regions with different statistics",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)
				bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)
				latency := NewStatisticFloat64("latency.ms", AggregationTypeMin)

				ctx, region1 := StartRegion(ctx, "read1")
				region1.Record(bytesRead.Observe(1024))
				region1.End()

				_, region2 := StartRegion(ctx, "read2")
				region2.Record(latency.Observe(10.5))
				region2.End()

				return capture
			},
			wantStats: []StatisticKey{
				{Name: "bytes.read", DataType: DataTypeInt64, Aggregation: AggregationTypeSum},
				{Name: "latency.ms", DataType: DataTypeFloat64, Aggregation: AggregationTypeMin},
			},
		},
		{
			name: "statistics with same name but different aggregation types",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)
				statSum := NewStatisticInt64("value", AggregationTypeSum)
				statMin := NewStatisticInt64("value", AggregationTypeMin)
				statMax := NewStatisticInt64("value", AggregationTypeMax)

				ctx, region1 := StartRegion(ctx, "region1")
				region1.Record(statSum.Observe(10))
				region1.End()

				ctx, region2 := StartRegion(ctx, "region2")
				region2.Record(statMin.Observe(20))
				region2.End()

				_, region3 := StartRegion(ctx, "region3")
				region3.Record(statMax.Observe(30))
				region3.End()

				return capture
			},
			wantStats: []StatisticKey{
				{Name: "value", DataType: DataTypeInt64, Aggregation: AggregationTypeSum},
				{Name: "value", DataType: DataTypeInt64, Aggregation: AggregationTypeMin},
				{Name: "value", DataType: DataTypeInt64, Aggregation: AggregationTypeMax},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := tt.setup()
			stats := capture.getAllStatistics()

			gotStats := make([]StatisticKey, 0, len(stats))
			for _, stat := range stats {
				gotStats = append(gotStats, stat.Key())
			}

			require.ElementsMatch(t, tt.wantStats, gotStats)
		})
	}
}

func TestCapture_LinkRegions(t *testing.T) {
	tests := []struct {
		name            string
		setup           func() *Capture
		linkAttribute   string
		resolveParent   func(string) (string, bool)
		expectedParents map[string]string // child -> parent mapping
	}{
		{
			name: "link regions without parent using link attribute",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)
				// Create root regions with link attributes
				_, _ = StartRegion(ctx, "region1", WithRegionAttributes(
					attribute.String("link.id", "id1"),
				))
				_, _ = StartRegion(ctx, "region2", WithRegionAttributes(
					attribute.String("link.id", "id2"),
				))
				_, _ = StartRegion(ctx, "region3", WithRegionAttributes(
					attribute.String("link.id", "id3"),
				))
				return capture
			},
			linkAttribute: "link.id",
			resolveParent: func(id string) (string, bool) {
				// id2 and id3 link to id1
				if id == "id2" || id == "id3" {
					return "id1", true
				}
				return "", false
			},
			expectedParents: map[string]string{
				"region2": "region1",
				"region3": "region1",
			},
		},
		{
			name: "do not change parent if region already has one",
			setup: func() *Capture {
				ctx, capture := NewCapture(context.Background(), nil)
				// Create a root region first
				_, _ = StartRegion(ctx, "root", WithRegionAttributes(
					attribute.String("link.id", "root-id"),
				))
				// Create a region with a parent (via context chain)
				ctx, _ = StartRegion(ctx, "parent", WithRegionAttributes(
					attribute.String("link.id", "parent-id"),
				))
				// This region already has a parent
				_, _ = StartRegion(ctx, "child", WithRegionAttributes(
					attribute.String("link.id", "child-id"),
				))
				return capture
			},
			linkAttribute: "link.id",
			resolveParent: func(id string) (string, bool) {
				// Try to link child-id to root-id, but child already has a parent
				if id == "child-id" {
					return "root-id", true
				}
				return "", false
			},
			expectedParents: map[string]string{
				"child": "parent", // Should remain unchanged
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := tt.setup()
			capture.LinkRegions(tt.linkAttribute, tt.resolveParent)

			regions := capture.Regions()
			gotRegions := make(map[string]*Region)
			for _, r := range regions {
				gotRegions[r.name] = r
			}

			// Validate parent-child relationships
			for childName, parentName := range tt.expectedParents {
				child, ok := gotRegions[childName]
				require.True(t, ok, "child region %q should exist", childName)
				parent, ok := gotRegions[parentName]
				require.True(t, ok, "parent region %q should exist", parentName)

				require.Equal(t, parent.id, child.parentID, "region %q should have parentID matching %q's ID", childName, parentName)
			}
		})
	}
}
