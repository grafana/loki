package xcap

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/grafana/loki/v3/pkg/xcap/statid"
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
	t.Run("distinct-named regions are all present after merge", func(t *testing.T) {
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

	t.Run("all merged regions are parented onto provided parent", func(t *testing.T) {
		ctxDst, dst := NewCapture(context.Background(), nil)
		_, dstParent := StartRegion(ctxDst, "dst-parent")

		ctxSrc, src := NewCapture(context.Background(), nil)
		ctxSrcRoot, srcRoot := StartRegion(ctxSrc, "src-root")
		_, srcChild := StartRegion(ctxSrcRoot, "src-child")

		require.Equal(t, srcRoot.id, srcChild.parentID, "src-child should be parented to src-root before merge")

		dst.Merge(dstParent, src)

		// The accumulating merge collapses the src hierarchy: every merged
		// region is parented directly onto dstParent.
		dstRoots := dst.regionByName["src-root"]
		require.Len(t, dstRoots, 1)
		require.Equal(t, dstParent.id, dstRoots[0].parentID, "dst region for src root should be parented onto dstParent")

		dstChildren := dst.regionByName["src-child"]
		require.Len(t, dstChildren, 1)
		require.Equal(t, dstParent.id, dstChildren[0].parentID, "dst region for src non-root should also be parented onto dstParent")
	})

	t.Run("observations roll up across merged regions with distinct names", func(t *testing.T) {
		stat := NewStatisticInt64(statid.Invalid, "merged.value", AggregationTypeSum)

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

	t.Run("same-named region observations are accumulated across merges", func(t *testing.T) {
		stat := NewStatisticInt64(statid.Invalid, "rows.scanned", AggregationTypeSum)
		_, dst := NewCapture(context.Background(), nil)

		// Simulate N task completions, each contributing a capture with the
		// same region name. Expect the values to be collapsed into one region.
		const tasks = 5
		for i := 0; i < tasks; i++ {
			ctxSrc, src := NewCapture(context.Background(), nil)
			_, r := StartRegion(ctxSrc, "task.region")
			r.Record(stat.Observe(100))
			r.End()
			src.End()
			dst.Merge(nil, src)
		}

		// Only one region should exist regardless of the number of merges.
		require.Len(t, dst.Regions(), 1, "accumulating merge should collapse same-named regions")

		value, ok := TryValue[int64](dst, stat)
		require.True(t, ok)
		require.Equal(t, int64(tasks*100), value, "accumulated value should equal sum across all merged captures")
	})

	t.Run("multiple distinct region names each accumulate independently", func(t *testing.T) {
		statA := NewStatisticInt64(statid.Invalid, "bytes.a", AggregationTypeSum)
		statB := NewStatisticInt64(statid.Invalid, "bytes.b", AggregationTypeSum)

		_, dst := NewCapture(context.Background(), nil)

		const tasks = 3
		for i := 0; i < tasks; i++ {
			ctxSrc, src := NewCapture(context.Background(), nil)
			_, ra := StartRegion(ctxSrc, "region.a")
			ra.Record(statA.Observe(10))
			ra.End()
			_, rb := StartRegion(ctxSrc, "region.b")
			rb.Record(statB.Observe(20))
			rb.End()
			src.End()
			dst.Merge(nil, src)
		}

		require.Len(t, dst.Regions(), 2, "one collapsed region per unique name")
		require.Equal(t, int64(tasks*10), Value[int64](dst, statA))
		require.Equal(t, int64(tasks*20), Value[int64](dst, statB))
	})

	t.Run("accumulating merge handles Min and Max correctly", func(t *testing.T) {
		statMin := NewStatisticFloat64(statid.Invalid, "latency.min", AggregationTypeMin)
		statMax := NewStatisticInt64(statid.Invalid, "rows.max", AggregationTypeMax)

		_, dst := NewCapture(context.Background(), nil)

		for _, pair := range [][2]int64{{10, 50}, {3, 200}, {7, 100}} {
			ctxSrc, src := NewCapture(context.Background(), nil)
			_, r := StartRegion(ctxSrc, "task.region")
			r.Record(statMin.Observe(float64(pair[0])))
			r.Record(statMax.Observe(pair[1]))
			r.End()
			src.End()
			dst.Merge(nil, src)
		}

		require.Equal(t, float64(3), Value[float64](dst, statMin), "min should be the smallest across all merges")
		require.Equal(t, int64(200), Value[int64](dst, statMax), "max should be the largest across all merges")
	})

	t.Run("regionByName index is populated after merge", func(t *testing.T) {
		ctxSrc, src := NewCapture(context.Background(), nil)
		_, _ = StartRegion(ctxSrc, "alpha")
		_, _ = StartRegion(ctxSrc, "beta")
		src.End()

		_, dst := NewCapture(context.Background(), nil)
		dst.Merge(nil, src)

		require.Len(t, dst.regionByName["alpha"], 1, "index should contain alpha")
		require.Len(t, dst.regionByName["beta"], 1, "index should contain beta")
		require.Empty(t, dst.regionByName["gamma"], "index should not contain unknown name")
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
				region.Record(NewStatisticInt64(statid.Invalid, "other", AggregationTypeSum).Observe(1))
				region.End()
				return capture, NewStatisticInt64(statid.Invalid, "missing", AggregationTypeSum)
			},
			wantNil: true,
		},
		{
			name: "empty capture returns nil",
			setup: func() (*Capture, Statistic) {
				_, capture := NewCapture(context.Background(), nil)
				return capture, NewStatisticInt64(statid.Invalid, "missing", AggregationTypeSum)
			},
			wantNil: true,
		},
		{
			name: "single region with sum aggregation",
			setup: func() (*Capture, Statistic) {
				ctx, capture := NewCapture(context.Background(), nil)
				stat := NewStatisticInt64(statid.Invalid, "bytes.read", AggregationTypeSum)
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
				stat := NewStatisticInt64(statid.Invalid, "bytes.read", AggregationTypeSum)
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
				stat := NewStatisticFloat64(statid.Invalid, "latency.ms", AggregationTypeMin)
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
				stat := NewStatisticInt64(statid.Invalid, "rows.scanned", AggregationTypeMax)
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
				stat := NewStatisticFlag(statid.Invalid, "cache.hit")
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
				statSum := NewStatisticInt64(statid.Invalid, "value", AggregationTypeSum)
				statMax := NewStatisticInt64(statid.Invalid, "value", AggregationTypeMax)
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
			require.Equal(t, tt.wantValue, got.Value())
			require.Equal(t, tt.wantCount, got.Count)
			require.Equal(t, stat.Key(), got.Statistic.Key())
		})
	}
}

func TestCapture_ValueFromRegion(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)
	stat := NewStatisticInt64(statid.Invalid, "rows.scanned", AggregationTypeSum)

	_, logsOpen := StartRegion(ctx, "logs.Reader.Open")
	logsOpen.Record(stat.Observe(100))
	logsOpen.End()

	_, logsOpen2 := StartRegion(ctx, "logs.Reader.Open")
	logsOpen2.Record(stat.Observe(25))
	logsOpen2.End()

	_, logsRead := StartRegion(ctx, "logs.Reader.Read")
	logsRead.Record(stat.Observe(50))
	logsRead.End()

	_, streamsOpen := StartRegion(ctx, "streams.Reader.Open")
	streamsOpen.Record(stat.Observe(1000))
	streamsOpen.End()

	// Exact name matches only the Open region.
	gotOpen := capture.ValueFromRegion("logs.Reader.Open", stat)
	require.NotNil(t, gotOpen)
	require.Equal(t, int64(125), gotOpen.Value())
	require.Equal(t, 2, gotOpen.Count)

	// Exact name matches only the Read region.
	gotRead := capture.ValueFromRegion("logs.Reader.Read", stat)
	require.NotNil(t, gotRead)
	require.Equal(t, int64(50), gotRead.Value())
	require.Equal(t, 1, gotRead.Count)

	// Prefix is not a match — no region is named "logs.Reader." exactly.
	require.Nil(t, capture.ValueFromRegion("logs.Reader.", stat))
}
