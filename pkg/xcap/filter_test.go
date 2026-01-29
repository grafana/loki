package xcap

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create a test capture with regions.
func createTestCapture(t *testing.T, regionSpecs []regionSpec) *Capture {
	t.Helper()

	ctx, capture := NewCapture(context.Background(), nil)

	// Create regions and track them by name for parent lookup
	regionsByName := make(map[string]*Region)

	for _, spec := range regionSpecs {
		// Find parent context
		parentCtx := ctx
		if spec.parentName != "" {
			parent, ok := regionsByName[spec.parentName]
			require.True(t, ok, "parent %q not found for region %q", spec.parentName, spec.name)
			parentCtx = ContextWithRegion(ctx, parent)
		}

		// Start region
		_, region := StartRegion(parentCtx, spec.name)
		require.NotNil(t, region, "region %q should not be nil", spec.name)

		// Add observations if specified
		for _, obs := range spec.observations {
			region.Record(obs)
		}

		// Set end time to achieve desired duration
		region.endTime = region.startTime.Add(spec.duration)
		region.ended = true

		regionsByName[spec.name] = region
	}

	// Mark capture as ended but don't trigger filtering
	// (we manually set ended without calling End() to avoid auto-filtering)
	capture.ended = true

	return capture
}

type regionSpec struct {
	name         string
	parentName   string // empty for root
	duration     time.Duration
	observations []Observation
}

// Test helper to find a region by name in a capture.
func findRegionByName(c *Capture, name string) *Region {
	for _, r := range c.Regions() {
		if r.name == name {
			return r
		}
	}
	return nil
}

// PruneLeaves Strategy Tests

func TestPruneLeaves_DropsOnlyLeaves(t *testing.T) {
	// Tree structure (threshold: 5ms):
	//
	//   root (100ms) ✓ kept
	//     └── child (1ms) ✗ dropped (leaf, below threshold)
	//
	// Result: only root remains
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond},
		{name: "child", parentName: "root", duration: time.Millisecond},
	})

	strategy := &pruneLeavesStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	assert.Len(t, capture.Regions(), 1)
	root := findRegionByName(capture, "root")
	require.NotNil(t, root)
	assert.Equal(t, uint32(1), root.DroppedRegions())
}

func TestPruneLeaves_DoesNotDropNonLeaves(t *testing.T) {
	// Tree structure (threshold: 5ms):
	//
	//   root (100ms) ✓ kept
	//     └── parent (1ms) ✓ kept (has kept child)
	//           └── child (10ms) ✓ kept (above threshold)
	//
	// Result: all 3 remain
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond},
		{name: "parent", parentName: "root", duration: time.Millisecond},
		{name: "child", parentName: "parent", duration: 10 * time.Millisecond},
	})

	strategy := &pruneLeavesStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	// All 3 should remain because parent has a kept child
	assert.Len(t, capture.Regions(), 3)
}

func TestPruneLeaves_CascadesUpward(t *testing.T) {
	// Tree structure (threshold: 5ms):
	//
	//   root (100ms) ✓ kept
	//     └── A (2ms) ✗ dropped (becomes leaf after B dropped)
	//           └── B (1ms) ✗ dropped (leaf, below threshold)
	//
	// Result: only root remains, droppedRegions=2
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond},
		{name: "A", parentName: "root", duration: 2 * time.Millisecond},
		{name: "B", parentName: "A", duration: time.Millisecond},
	})

	strategy := &pruneLeavesStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	assert.Len(t, capture.Regions(), 1)
	root := findRegionByName(capture, "root")
	require.NotNil(t, root)
	assert.Equal(t, uint32(2), root.DroppedRegions()) // Both A and B dropped
}

// PromoteChildren Strategy Tests

func TestPromoteChildren_ReparentsCorrectly(t *testing.T) {
	// Tree structure (threshold: 5ms):
	//
	//   root (100ms) ✓ kept
	//     └── A (1ms) ✗ dropped
	//           ├── B (10ms) ✓ kept (re-parented to root)
	//           └── C (15ms) ✓ kept (re-parented to root)
	//
	// Result: root, B, C remain; B and C become children of root
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond},
		{name: "A", parentName: "root", duration: time.Millisecond},
		{name: "B", parentName: "A", duration: 10 * time.Millisecond},
		{name: "C", parentName: "A", duration: 15 * time.Millisecond},
	})

	strategy := &promoteChildrenStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	assert.Len(t, capture.Regions(), 3) // root, B, C

	root := findRegionByName(capture, "root")
	require.NotNil(t, root)
	assert.Equal(t, uint32(1), root.DroppedRegions())

	B := findRegionByName(capture, "B")
	require.NotNil(t, B)
	assert.Equal(t, root.id, B.parentID, "B should be re-parented to root")

	C := findRegionByName(capture, "C")
	require.NotNil(t, C)
	assert.Equal(t, root.id, C.parentID, "C should be re-parented to root")
}

func TestPromoteChildren_MultiLevelReparenting(t *testing.T) {
	// Tree structure (threshold: 5ms):
	//
	//   root (100ms) ✓ kept
	//     └── A (1ms) ✗ dropped
	//           └── B (2ms) ✗ dropped
	//                 └── C (10ms) ✓ kept (re-parented to root)
	//
	// Result: root, C remain; C becomes child of root
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond},
		{name: "A", parentName: "root", duration: time.Millisecond},
		{name: "B", parentName: "A", duration: 2 * time.Millisecond},
		{name: "C", parentName: "B", duration: 10 * time.Millisecond},
	})

	strategy := &promoteChildrenStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	assert.Len(t, capture.Regions(), 2) // root, C

	root := findRegionByName(capture, "root")
	require.NotNil(t, root)
	assert.Equal(t, uint32(2), root.DroppedRegions()) // A and B dropped

	C := findRegionByName(capture, "C")
	require.NotNil(t, C)
	assert.Equal(t, root.id, C.parentID, "C should be re-parented to root")
}

// DropSubtree Strategy Tests

func TestDropSubtree_DropsEntireSubtree(t *testing.T) {
	// Tree structure (threshold: 5ms):
	//
	//   root (100ms) ✓ kept
	//     └── A (1ms) ✗ dropped (below threshold)
	//           ├── B (20ms) ✗ dropped (parent dropped)
	//           └── C (15ms) ✗ dropped (parent dropped)
	//
	// Result: only root remains, droppedRegions=3
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond},
		{name: "A", parentName: "root", duration: time.Millisecond},
		{name: "B", parentName: "A", duration: 20 * time.Millisecond},
		{name: "C", parentName: "A", duration: 15 * time.Millisecond},
	})

	strategy := &dropSubtreeStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	assert.Len(t, capture.Regions(), 1) // only root

	root := findRegionByName(capture, "root")
	require.NotNil(t, root)
	assert.Equal(t, uint32(3), root.DroppedRegions()) // A, B, C all dropped
}

func TestDropSubtree_KeepsParallelSubtrees(t *testing.T) {
	// Tree structure (threshold: 5ms):
	//
	//   root (100ms) ✓ kept
	//     ├── A (1ms) ✗ dropped
	//     │     └── B (20ms) ✗ dropped (parent dropped)
	//     └── D (10ms) ✓ kept
	//           └── E (15ms) ✓ kept
	//
	// Result: root, D, E remain
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond},
		{name: "A", parentName: "root", duration: time.Millisecond},
		{name: "B", parentName: "A", duration: 20 * time.Millisecond},
		{name: "D", parentName: "root", duration: 10 * time.Millisecond},
		{name: "E", parentName: "D", duration: 15 * time.Millisecond},
	})

	strategy := &dropSubtreeStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	assert.Len(t, capture.Regions(), 3) // root, D, E

	root := findRegionByName(capture, "root")
	require.NotNil(t, root)
	assert.Equal(t, uint32(2), root.DroppedRegions()) // A and B dropped

	D := findRegionByName(capture, "D")
	require.NotNil(t, D)

	E := findRegionByName(capture, "E")
	require.NotNil(t, E)
}

// Observation Rollup Tests

func TestObservationRollup_MergesCorrectly(t *testing.T) {
	// Create a statistic for testing
	stat := NewStatisticInt64("bytes.read", AggregationTypeSum)

	// Tree structure:
	//   root (100ms, bytes.read=100) ✓ kept
	//     └── child (1ms, bytes.read=50) ✗ dropped
	//
	// After filtering: root has bytes.read=150 (merged)
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond, observations: []Observation{stat.Observe(100)}},
		{name: "child", parentName: "root", duration: time.Millisecond, observations: []Observation{stat.Observe(50)}},
	})

	strategy := &promoteChildrenStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	root := findRegionByName(capture, "root")
	require.NotNil(t, root)

	// Check that observations were merged
	obs := root.Observations()
	require.Len(t, obs, 1)
	val, ok := obs[0].Int64()
	require.True(t, ok)
	assert.Equal(t, int64(150), val) // 100 + 50
}

func TestObservationRollup_HandlesMultipleStats(t *testing.T) {
	bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)
	rowsProcessed := NewStatisticInt64("rows.processed", AggregationTypeSum)

	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond, observations: []Observation{
			bytesRead.Observe(100),
			rowsProcessed.Observe(10),
		}},
		{name: "child", parentName: "root", duration: time.Millisecond, observations: []Observation{
			bytesRead.Observe(50),
			rowsProcessed.Observe(5),
		}},
	})

	strategy := &promoteChildrenStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	root := findRegionByName(capture, "root")
	require.NotNil(t, root)

	// Check observations were merged correctly
	obs := root.Observations()
	assert.Len(t, obs, 2)

	obsMap := make(map[string]int64)
	for _, o := range obs {
		val, _ := o.Int64()
		obsMap[o.Statistic.Name()] = val
	}

	assert.Equal(t, int64(150), obsMap["bytes.read"])
	assert.Equal(t, int64(15), obsMap["rows.processed"])
}

func TestFilter_AllRegionsDropped(t *testing.T) {
	// All regions are below threshold
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: time.Millisecond},
		{name: "child", parentName: "root", duration: time.Millisecond},
	})

	strategy := &promoteChildrenStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	assert.Len(t, capture.Regions(), 0)
}

func TestFilter_NoRegionsDropped(t *testing.T) {
	// All regions are above threshold
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond},
		{name: "child", parentName: "root", duration: 10 * time.Millisecond},
	})

	strategy := &promoteChildrenStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	assert.Len(t, capture.Regions(), 2)

	root := findRegionByName(capture, "root")
	require.NotNil(t, root)
	assert.Equal(t, uint32(0), root.DroppedRegions())
}

func TestFilter_SingleRegion(t *testing.T) {
	// Single region below threshold
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: time.Millisecond},
	})

	strategy := &promoteChildrenStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	assert.Len(t, capture.Regions(), 0)
}

func TestFilter_ZeroDuration(t *testing.T) {
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond},
		{name: "child", parentName: "root", duration: time.Millisecond},
	})

	strategy := &promoteChildrenStrategy{}
	strategy.Filter(capture, 0) // Zero threshold should not filter

	assert.Len(t, capture.Regions(), 2)
}

// DroppedRegions Counter Tests

func TestDroppedRegions_CountsAccurately(t *testing.T) {
	// Tree structure (threshold: 5ms):
	//
	//   root (100ms) ✓ kept, droppedRegions=2 (A, B)
	//     ├── A (1ms) ✗ dropped
	//     ├── B (1ms) ✗ dropped
	//     └── C (10ms) ✓ kept, droppedRegions=1 (D)
	//           └── D (1ms) ✗ dropped
	//
	capture := createTestCapture(t, []regionSpec{
		{name: "root", duration: 100 * time.Millisecond},
		{name: "A", parentName: "root", duration: time.Millisecond},
		{name: "B", parentName: "root", duration: time.Millisecond},
		{name: "C", parentName: "root", duration: 10 * time.Millisecond},
		{name: "D", parentName: "C", duration: time.Millisecond},
	})

	strategy := &promoteChildrenStrategy{}
	strategy.Filter(capture, 5*time.Millisecond)

	// Should have: root, C
	assert.Len(t, capture.Regions(), 2)

	root := findRegionByName(capture, "root")
	require.NotNil(t, root)
	assert.Equal(t, uint32(2), root.DroppedRegions()) // A and B

	C := findRegionByName(capture, "C")
	require.NotNil(t, C)
	assert.Equal(t, uint32(1), C.DroppedRegions()) // D
}
