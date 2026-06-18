package xcap

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
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

func TestCapture_Merge(t *testing.T) {
	t.Run("merges all regions from src into dst", func(t *testing.T) {
		ctxDst, dst := NewCapture(context.Background(), nil)
		ctxSrc, src := NewCapture(context.Background(), nil)

		_, _ = StartRegion(ctxDst, "dst-region")
		_, _ = StartRegion(ctxSrc, "src-region-1")
		_, _ = StartRegion(ctxSrc, "src-region-2")

		dst.Merge(nil, src)

		regions := dst.Regions()
		got := make([]string, 0, len(regions))
		for _, r := range regions {
			got = append(got, r.name)
		}
		require.ElementsMatch(t, []string{"dst-region", "src-region-1", "src-region-2"}, got)
	})

	t.Run("re-parents src root regions when parent is provided", func(t *testing.T) {
		ctxDst, dst := NewCapture(context.Background(), nil)
		_, dstParent := StartRegion(ctxDst, "dst-parent")

		ctxSrc, src := NewCapture(context.Background(), nil)
		_, srcRoot := StartRegion(ctxSrc, "src-root")

		require.True(t, srcRoot.parentID.IsZero(), "src root should have no parent before merge")

		dst.Merge(dstParent, src)

		require.Equal(t, dstParent.id, srcRoot.parentID, "src root should be re-parented onto dstParent")
	})

	t.Run("does not change parent for src non-root regions", func(t *testing.T) {
		ctxDst, dst := NewCapture(context.Background(), nil)
		_, dstParent := StartRegion(ctxDst, "dst-parent")

		ctxSrc, src := NewCapture(context.Background(), nil)
		ctxSrcRoot, srcRoot := StartRegion(ctxSrc, "src-root")
		_, srcChild := StartRegion(ctxSrcRoot, "src-child")

		require.Equal(t, srcRoot.id, srcChild.parentID, "src-child should be parented to src-root before merge")

		dst.Merge(dstParent, src)

		require.Equal(t, dstParent.id, srcRoot.parentID, "src-root should be re-parented onto dstParent")
		require.Equal(t, srcRoot.id, srcChild.parentID, "src-child should keep its original parent (src-root)")
	})

	t.Run("observations roll up across merged regions", func(t *testing.T) {
		stat := NewStatisticInt64("merged.value", AggregationTypeSum)

		ctxDst, dst := NewCapture(context.Background(), nil)
		_, dstRegion := StartRegion(ctxDst, "dst-region")
		dstRegion.Record(stat.Observe(10))
		dstRegion.End()

		ctxSrc, src := NewCapture(context.Background(), nil)
		_, srcRegion := StartRegion(ctxSrc, "src-region")
		srcRegion.Record(stat.Observe(7))
		srcRegion.End()

		dst.Merge(nil, src)

		value, ok := TryValue[int64](dst, stat)
		require.True(t, ok)
		require.Equal(t, int64(17), value, "dst should observe sum of both captures' regions after merge")
	})

	t.Run("nil src is a no-op", func(t *testing.T) {
		ctxDst, dst := NewCapture(context.Background(), nil)
		_, _ = StartRegion(ctxDst, "dst-region")

		require.NotPanics(t, func() { dst.Merge(nil, nil) })
		require.Len(t, dst.Regions(), 1)
	})

	t.Run("nil receiver is a no-op", func(t *testing.T) {
		_, src := NewCapture(context.Background(), nil)
		var dst *Capture
		require.NotPanics(t, func() { dst.Merge(nil, src) })
	})

	t.Run("merge into ended capture does not add regions", func(t *testing.T) {
		_, dst := NewCapture(context.Background(), nil)
		dst.End()

		ctxSrc, src := NewCapture(context.Background(), nil)
		_, _ = StartRegion(ctxSrc, "src-region")

		dst.Merge(nil, src)
		require.Empty(t, dst.Regions(), "ended dst should not absorb new regions")
	})
}

func TestCapture_Value(t *testing.T) {
	tests := []struct {
		name      string
		setup     func() (*Capture, Statistic)
		wantNil   bool
		wantValue any
		wantCount int
	}{
		{
			name: "statistic not present returns nil",
			setup: func() (*Capture, Statistic) {
				ctx, capture := NewCapture(context.Background(), nil)
				_, region := StartRegion(ctx, "region1")
				region.Record(NewStatisticInt64("other", AggregationTypeSum).Observe(1))
				region.End()
				return capture, NewStatisticInt64("missing", AggregationTypeSum)
			},
			wantNil: true,
		},
		{
			name: "empty capture returns nil",
			setup: func() (*Capture, Statistic) {
				_, capture := NewCapture(context.Background(), nil)
				return capture, NewStatisticInt64("missing", AggregationTypeSum)
			},
			wantNil: true,
		},
		{
			name: "single region with sum aggregation",
			setup: func() (*Capture, Statistic) {
				ctx, capture := NewCapture(context.Background(), nil)
				stat := NewStatisticInt64("bytes.read", AggregationTypeSum)
				_, region := StartRegion(ctx, "region1")
				region.Record(stat.Observe(100))
				region.Record(stat.Observe(50))
				region.End()
				return capture, stat
			},
			wantValue: int64(150),
			wantCount: 2,
		},
		{
			name: "multiple regions sum across regions",
			setup: func() (*Capture, Statistic) {
				ctx, capture := NewCapture(context.Background(), nil)
				stat := NewStatisticInt64("bytes.read", AggregationTypeSum)
				_, region1 := StartRegion(ctx, "region1")
				region1.Record(stat.Observe(100))
				region1.End()
				_, region2 := StartRegion(ctx, "region2")
				region2.Record(stat.Observe(200))
				region2.Record(stat.Observe(50))
				region2.End()
				return capture, stat
			},
			wantValue: int64(350),
			wantCount: 3,
		},
		{
			name: "multiple regions min across regions",
			setup: func() (*Capture, Statistic) {
				ctx, capture := NewCapture(context.Background(), nil)
				stat := NewStatisticFloat64("latency.ms", AggregationTypeMin)
				_, region1 := StartRegion(ctx, "region1")
				region1.Record(stat.Observe(10.0))
				region1.End()
				_, region2 := StartRegion(ctx, "region2")
				region2.Record(stat.Observe(5.0))
				region2.End()
				_, region3 := StartRegion(ctx, "region3")
				region3.Record(stat.Observe(20.0))
				region3.End()
				return capture, stat
			},
			wantValue: float64(5.0),
			wantCount: 3,
		},
		{
			name: "multiple regions max across regions",
			setup: func() (*Capture, Statistic) {
				ctx, capture := NewCapture(context.Background(), nil)
				stat := NewStatisticInt64("rows.scanned", AggregationTypeMax)
				_, region1 := StartRegion(ctx, "region1")
				region1.Record(stat.Observe(10))
				region1.End()
				_, region2 := StartRegion(ctx, "region2")
				region2.Record(stat.Observe(50))
				region2.End()
				_, region3 := StartRegion(ctx, "region3")
				region3.Record(stat.Observe(20))
				region3.End()
				return capture, stat
			},
			wantValue: int64(50),
			wantCount: 3,
		},
		{
			name: "flag aggregates as max (any true wins)",
			setup: func() (*Capture, Statistic) {
				ctx, capture := NewCapture(context.Background(), nil)
				stat := NewStatisticFlag("cache.hit")
				_, region1 := StartRegion(ctx, "region1")
				region1.Record(stat.Observe(false))
				region1.End()
				_, region2 := StartRegion(ctx, "region2")
				region2.Record(stat.Observe(true))
				region2.End()
				return capture, stat
			},
			wantValue: true,
			wantCount: 2,
		},
		{
			name: "same name different aggregation does not collide",
			setup: func() (*Capture, Statistic) {
				ctx, capture := NewCapture(context.Background(), nil)
				statSum := NewStatisticInt64("value", AggregationTypeSum)
				statMax := NewStatisticInt64("value", AggregationTypeMax)
				_, region1 := StartRegion(ctx, "region1")
				region1.Record(statSum.Observe(10))
				region1.Record(statMax.Observe(100))
				region1.End()
				_, region2 := StartRegion(ctx, "region2")
				region2.Record(statSum.Observe(20))
				region2.Record(statMax.Observe(50))
				region2.End()
				return capture, statMax
			},
			wantValue: int64(100),
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture, stat := tt.setup()
			got := capture.Value(stat)

			if tt.wantNil {
				require.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			require.Equal(t, tt.wantValue, got.Value)
			require.Equal(t, tt.wantCount, got.Count)
			require.Equal(t, stat.Key(), got.Statistic.Key())
		})
	}
}

func TestCapture_ValueFromRegion(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)
	stat := NewStatisticInt64("rows.scanned", AggregationTypeSum)

	_, logsOpen := StartRegion(ctx, "logs.Reader.Open")
	logsOpen.Record(stat.Observe(100))
	logsOpen.End()

	_, logsRead := StartRegion(ctx, "logs.Reader.Read")
	logsRead.Record(stat.Observe(50))
	logsRead.End()

	_, streamsOpen := StartRegion(ctx, "streams.Reader.Open")
	streamsOpen.Record(stat.Observe(1000))
	streamsOpen.End()

	got := capture.ValueFromRegion("logs.Reader.", stat)
	require.NotNil(t, got)
	require.Equal(t, int64(150), got.Value)
	require.Equal(t, 2, got.Count)
	require.Equal(t, stat.Key(), got.Statistic.Key())

	require.Equal(t, int64(150), ValueFromRegion[int64](capture, "logs.Reader", stat))
	require.Zero(t, ValueFromRegion[int64](capture, "missing.Reader", stat))
	require.Zero(t, ValueFromRegion[float64](capture, "logs.Reader", stat))
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
