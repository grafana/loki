package planner

import (
	"context"
	"testing"
	"time"

	compactionpb "github.com/grafana/loki/v3/pkg/dataobj/compaction/proto"

	"github.com/stretchr/testify/require"
)

// mockSizer is a simple implementation of Sizer for testing.
type mockSizer struct {
	size int64
	id   string
}

func (m *mockSizer) GetSize() int64 { return m.size }

// Helper to create mock sizers
func newMockSizer(id string, size int64) *mockSizer {
	return &mockSizer{id: id, size: size}
}

// Helper to get total size of bins
func getTotalBinSize[G Sizer](bins []BinPackResult[G]) int64 {
	var total int64
	for _, bin := range bins {
		total += bin.Size
	}
	return total
}

// Helper to get total items across all bins
func getTotalItemCount[G Sizer](bins []BinPackResult[G]) int {
	var total int
	for _, bin := range bins {
		total += len(bin.Groups)
	}
	return total
}

// MockIndexStreamReader is a test implementation of IndexStreamReader.
type MockIndexStreamReader struct {
	// Results maps indexPath to the result to return
	Results map[string]*IndexStreamResult
	// Err is an optional error to return for all calls
	Err error
}

func (m *MockIndexStreamReader) ReadStreams(_ context.Context, indexPath, _ string, _, _ time.Time) (*IndexStreamResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if result, ok := m.Results[indexPath]; ok {
		return result, nil
	}
	return &IndexStreamResult{}, nil
}

// newTestPlanner creates a Planner with a mock index reader for testing.
func newTestPlanner(mockReader *MockIndexStreamReader) *Planner {
	return &Planner{
		indexReader: mockReader,
	}
}

func TestProrateSize(t *testing.T) {
	tests := []struct {
		name            string
		totalSize       int64
		partialDuration time.Duration
		totalDuration   time.Duration
		expected        int64
	}{
		{
			name:            "full duration returns full size",
			totalSize:       1000,
			partialDuration: time.Hour,
			totalDuration:   time.Hour,
			expected:        1000,
		},
		{
			name:            "half duration returns half size",
			totalSize:       1000,
			partialDuration: 30 * time.Minute,
			totalDuration:   time.Hour,
			expected:        500,
		},
		{
			name:            "quarter duration returns quarter size",
			totalSize:       1000,
			partialDuration: 15 * time.Minute,
			totalDuration:   time.Hour,
			expected:        250,
		},
		{
			name:            "zero total duration returns full size",
			totalSize:       1000,
			partialDuration: 30 * time.Minute,
			totalDuration:   0,
			expected:        1000,
		},
		{
			name:            "negative total duration returns full size",
			totalSize:       1000,
			partialDuration: 30 * time.Minute,
			totalDuration:   -time.Hour,
			expected:        1000,
		},
		{
			name:            "zero partial duration returns zero",
			totalSize:       1000,
			partialDuration: 0,
			totalDuration:   time.Hour,
			expected:        0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := prorateSize(tc.totalSize, tc.partialDuration, tc.totalDuration)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestCalculateNumOutputObjects(t *testing.T) {
	tests := []struct {
		name     string
		size     int64
		expected int32
	}{
		{
			name:     "zero size",
			size:     0,
			expected: 0,
		},
		{
			name:     "size less than target",
			size:     targetUncompressedSize - 1,
			expected: 1,
		},
		{
			name:     "size equals target",
			size:     targetUncompressedSize,
			expected: 1,
		},
		{
			name:     "size slightly over target",
			size:     targetUncompressedSize + 1,
			expected: 2,
		},
		{
			name:     "size equals two targets",
			size:     targetUncompressedSize * 2,
			expected: 2,
		},
		{
			name:     "size slightly over two targets",
			size:     targetUncompressedSize*2 + 1,
			expected: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := calculateNumOutputObjects(tc.size)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestBinPack(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		result := BinPack([]*mockSizer{})
		require.Nil(t, result)
		require.Len(t, result, 0) // 0 bins expected
	})

	t.Run("single small item creates one bin", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize/2),
		}
		result := BinPack(items)

		require.Len(t, result, 1) // 1 bin expected
		require.Len(t, result[0].Groups, 1)
		require.Equal(t, targetUncompressedSize/2, result[0].Size)
	})

	t.Run("items fitting in one bin are grouped", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize/4),
			newMockSizer("b", targetUncompressedSize/4),
			newMockSizer("c", targetUncompressedSize/4),
		}
		result := BinPack(items)

		// All items should fit in one bin (75% fill)
		require.Len(t, result, 1) // 1 bin expected
		require.Equal(t, 3, getTotalItemCount(result))
		require.Equal(t, 3*targetUncompressedSize/4, getTotalBinSize(result))
	})

	t.Run("items exceeding target create multiple bins", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize/2),
			newMockSizer("b", targetUncompressedSize/2),
			newMockSizer("c", targetUncompressedSize/2),
		}
		result := BinPack(items)

		// Total is 1.5x target - first two items (1.0x) go in bin 1, third item (0.5x) in bin 2
		// After mergeUnderfilledBins, bin 2 gets merged in bin 1 since bin 2 is underfilled
		require.Len(t, result, 1) // 1 bin expected (merged)
		totalSize := getTotalBinSize(result)
		require.Equal(t, 3*targetUncompressedSize/2, totalSize)
		require.Equal(t, 3, getTotalItemCount(result))
	})

	t.Run("large item creates its own bin", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("large", targetUncompressedSize+1),
			newMockSizer("small", targetUncompressedSize/4),
		}
		result := BinPack(items)

		// Large item needs its own bin, small item can fit in the overflow space
		// (large bin is at 1x + 1 byte, so partial space is ~1x, small item fits)
		require.Len(t, result, 1) // 1 bin expected (small fits in overflow partial space)
		totalSize := getTotalBinSize(result)
		require.Equal(t, targetUncompressedSize+1+targetUncompressedSize/4, totalSize)
		require.Equal(t, 2, getTotalItemCount(result))
	})

	t.Run("sorting by size descending", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("small", 100),
			newMockSizer("large", targetUncompressedSize/2),
			newMockSizer("medium", targetUncompressedSize/4),
		}
		result := BinPack(items)

		// All items fit in one bin (75% + 100 bytes)
		require.Len(t, result, 1) // 1 bin expected
		require.Equal(t, 3, getTotalItemCount(result))
	})

	t.Run("preserves all items", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize/3),
			newMockSizer("b", targetUncompressedSize/3),
			newMockSizer("c", targetUncompressedSize/3),
			newMockSizer("d", targetUncompressedSize/3),
			newMockSizer("e", targetUncompressedSize/3),
		}
		result := BinPack(items)

		// Total is 5/3 = ~1.67x target, fits in 1 bin with upto 2x the target size
		require.Len(t, result, 1)

		// Verify all items are preserved
		require.Equal(t, 5, getTotalItemCount(result))

		// Verify total size is preserved
		var inputTotal int64
		for _, item := range items {
			inputTotal += item.GetSize()
		}
		require.Equal(t, inputTotal, getTotalBinSize(result))
	})

	t.Run("two full bins", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize),
			newMockSizer("b", targetUncompressedSize),
		}
		result := BinPack(items)

		// Each item is exactly target size, so 2 bins
		require.Len(t, result, 2) // 2 bins expected
		require.Equal(t, 2, getTotalItemCount(result))
		require.Equal(t, 2*targetUncompressedSize, getTotalBinSize(result))
	})

	t.Run("two full bins with many small items", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize/3),
			newMockSizer("b", targetUncompressedSize/3),
			newMockSizer("c", targetUncompressedSize/3),
			newMockSizer("d", targetUncompressedSize/3),
			newMockSizer("e", targetUncompressedSize/3),
			newMockSizer("f", targetUncompressedSize/3),
		}
		result := BinPack(items)

		// Each item is exactly target size, so 2 bins
		require.Len(t, result, 2) // 2 bins expected
		require.Equal(t, 6, getTotalItemCount(result))
		require.Equal(t, 2*targetUncompressedSize, getTotalBinSize(result))
	})

	t.Run("three bins with different fill levels", func(t *testing.T) {
		// Create items that will result in 3 well-filled bins
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize*80/100), // 80%
			newMockSizer("b", targetUncompressedSize*80/100), // 80%
			newMockSizer("c", targetUncompressedSize*80/100), // 80%
		}
		result := BinPack(items)

		// Total is 2.4x target, estimated bin count = 3.
		// Each 80% item can't fit with another (160% > 100%), so each gets its own bin.
		// mergeUnderfilledBins keeps them separate since each bin is >= 70% threshold.
		require.Len(t, result, 3) // 3 bins expected
		require.Equal(t, 3, getTotalItemCount(result))
	})
}

func TestFindBestFitBin(t *testing.T) {
	t.Run("empty bins returns -1", func(t *testing.T) {
		var bins []BinPackResult[*mockSizer]
		result := findBestFitBin(bins, 100, targetUncompressedSize)
		require.Equal(t, -1, result)
	})

	t.Run("item too large for any bin returns -1", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", targetUncompressedSize/2)}, Size: targetUncompressedSize / 2},
		}
		// Item that would exceed maxSize
		result := findBestFitBin(bins, targetUncompressedSize, targetUncompressedSize)
		require.Equal(t, -1, result)
	})

	t.Run("finds bin with best fit", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", targetUncompressedSize/4)}, Size: targetUncompressedSize / 4},       // 25% full
			{Groups: []*mockSizer{newMockSizer("b", targetUncompressedSize/2)}, Size: targetUncompressedSize / 2},       // 50% full
			{Groups: []*mockSizer{newMockSizer("c", targetUncompressedSize*3/4)}, Size: targetUncompressedSize * 3 / 4}, // 75% full
		}

		// Item of size 20% should best fit in bin at 75% (leaving 5% space)
		result := findBestFitBin(bins, targetUncompressedSize/5, targetUncompressedSize)
		require.Equal(t, 2, result) // bin at index 2 (75% full)
	})

	t.Run("handles bin already at maxSize boundary", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", targetUncompressedSize)}, Size: targetUncompressedSize}, // exactly at target
		}

		// Should not fit in a bin that's exactly at maxSize
		result := findBestFitBin(bins, 100, targetUncompressedSize)
		require.Equal(t, -1, result)
	})

	t.Run("handles overflowed bin with partial fill", func(t *testing.T) {
		// Bin is at 1.5x target (has partial object at 0.5x)
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", targetUncompressedSize*3/2)}, Size: targetUncompressedSize * 3 / 2},
		}

		// Small item should fit in the partial space
		result := findBestFitBin(bins, targetUncompressedSize/4, targetUncompressedSize)
		require.Equal(t, 0, result)
	})

	t.Run("skips bin at perfect multiple of target", func(t *testing.T) {
		// Bin is exactly at 2x target (no partial object)
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", targetUncompressedSize*2)}, Size: targetUncompressedSize * 2},
		}

		// Should skip this bin as it's at a perfect boundary
		result := findBestFitBin(bins, 100, targetUncompressedSize)
		require.Equal(t, -1, result)
	})
}

func TestMergeUnderfilledBins(t *testing.T) {
	minDesiredSize := targetUncompressedSize * minFillPercent / 100

	t.Run("single bin unchanged", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", minDesiredSize/2)}, Size: minDesiredSize / 2},
		}
		result := mergeUnderfilledBins(bins)

		require.Len(t, result, 1)
		require.Equal(t, minDesiredSize/2, result[0].Size)
	})

	t.Run("well-filled bins unchanged", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", minDesiredSize)}, Size: minDesiredSize},
			{Groups: []*mockSizer{newMockSizer("b", minDesiredSize)}, Size: minDesiredSize},
		}
		result := mergeUnderfilledBins(bins)

		require.Len(t, result, 2)
	})

	t.Run("underfilled bins are merged", func(t *testing.T) {
		// Two bins each at 30% (below 70% threshold)
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", minDesiredSize/2)}, Size: minDesiredSize / 2},
			{Groups: []*mockSizer{newMockSizer("b", minDesiredSize/2)}, Size: minDesiredSize / 2},
		}
		result := mergeUnderfilledBins(bins)

		// Should be merged into one bin
		require.Len(t, result, 1)
		require.Equal(t, minDesiredSize, result[0].Size)
		require.Equal(t, 2, len(result[0].Groups))
	})

	t.Run("preserves total size after merge", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", minDesiredSize/3)}, Size: minDesiredSize / 3},
			{Groups: []*mockSizer{newMockSizer("b", minDesiredSize/3)}, Size: minDesiredSize / 3},
			{Groups: []*mockSizer{newMockSizer("c", minDesiredSize/3)}, Size: minDesiredSize / 3},
		}

		inputTotal := getTotalBinSize(bins)
		result := mergeUnderfilledBins(bins)
		outputTotal := getTotalBinSize(result)

		require.Equal(t, inputTotal, outputTotal)
	})

	t.Run("preserves all items after merge", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", minDesiredSize/4)}, Size: minDesiredSize / 4},
			{Groups: []*mockSizer{newMockSizer("b", minDesiredSize/4)}, Size: minDesiredSize / 4},
			{Groups: []*mockSizer{newMockSizer("c", minDesiredSize/4)}, Size: minDesiredSize / 4},
		}

		inputCount := getTotalItemCount(bins)
		result := mergeUnderfilledBins(bins)
		outputCount := getTotalItemCount(result)

		require.Equal(t, inputCount, outputCount)
	})
}

func TestAggregateLeftovers(t *testing.T) {
	t.Run("empty groups does nothing", func(t *testing.T) {
		dest := make(map[uint64]*LeftoverStreamGroup)
		aggregateLeftovers([]*LeftoverStreamGroup{}, dest)
		require.Empty(t, dest)
	})

	t.Run("new group is added", func(t *testing.T) {
		dest := make(map[uint64]*LeftoverStreamGroup)
		groups := []*LeftoverStreamGroup{
			{
				LabelsHash:            123,
				Streams:               []LeftoverStreamInfo{{UncompressedSize: 100}},
				TotalUncompressedSize: 100,
			},
		}

		aggregateLeftovers(groups, dest)

		require.Len(t, dest, 1)
		require.Equal(t, uint64(123), dest[123].LabelsHash)
		require.Equal(t, int64(100), dest[123].TotalUncompressedSize)
	})

	t.Run("existing group is merged", func(t *testing.T) {
		dest := make(map[uint64]*LeftoverStreamGroup)
		dest[123] = &LeftoverStreamGroup{
			LabelsHash:            123,
			Streams:               []LeftoverStreamInfo{{UncompressedSize: 100}},
			TotalUncompressedSize: 100,
		}

		groups := []*LeftoverStreamGroup{
			{
				LabelsHash:            123,
				Streams:               []LeftoverStreamInfo{{UncompressedSize: 200}},
				TotalUncompressedSize: 200,
			},
		}

		aggregateLeftovers(groups, dest)

		require.Len(t, dest, 1)
		require.Equal(t, int64(300), dest[123].TotalUncompressedSize)
		require.Len(t, dest[123].Streams, 2)
	})

	t.Run("multiple groups with different hashes", func(t *testing.T) {
		dest := make(map[uint64]*LeftoverStreamGroup)
		groups := []*LeftoverStreamGroup{
			{LabelsHash: 1, Streams: []LeftoverStreamInfo{{UncompressedSize: 100}}, TotalUncompressedSize: 100},
			{LabelsHash: 2, Streams: []LeftoverStreamInfo{{UncompressedSize: 200}}, TotalUncompressedSize: 200},
			{LabelsHash: 3, Streams: []LeftoverStreamInfo{{UncompressedSize: 300}}, TotalUncompressedSize: 300},
		}

		aggregateLeftovers(groups, dest)

		require.Len(t, dest, 3)
		require.Equal(t, int64(100), dest[1].TotalUncompressedSize)
		require.Equal(t, int64(200), dest[2].TotalUncompressedSize)
		require.Equal(t, int64(300), dest[3].TotalUncompressedSize)
	})
}

func TestPlanLeftovers(t *testing.T) {
	planner := &Planner{}

	t.Run("nil inputs returns nil", func(t *testing.T) {
		result := planner.planLeftovers(nil, nil)
		require.Nil(t, result)
	})

	t.Run("empty inputs returns nil", func(t *testing.T) {
		result := planner.planLeftovers([]*LeftoverStreamGroup{}, []*LeftoverStreamGroup{})
		require.Nil(t, result)
	})

	t.Run("only before streams", func(t *testing.T) {
		beforeStreams := []*LeftoverStreamGroup{
			{
				LabelsHash:            1,
				Streams:               []LeftoverStreamInfo{{UncompressedSize: 100}},
				TotalUncompressedSize: 100,
			},
		}

		result := planner.planLeftovers(beforeStreams, nil)

		require.NotNil(t, result)
		require.Len(t, result.BeforeWindow, 1)
		require.Equal(t, int64(100), result.BeforeWindowSize)
		require.Empty(t, result.AfterWindow)
		require.Equal(t, int64(0), result.AfterWindowSize)
	})

	t.Run("only after streams", func(t *testing.T) {
		afterStreams := []*LeftoverStreamGroup{
			{
				LabelsHash:            2,
				Streams:               []LeftoverStreamInfo{{UncompressedSize: 200}},
				TotalUncompressedSize: 200,
			},
		}

		result := planner.planLeftovers(nil, afterStreams)

		require.NotNil(t, result)
		require.Empty(t, result.BeforeWindow)
		require.Equal(t, int64(0), result.BeforeWindowSize)
		require.Len(t, result.AfterWindow, 1)
		require.Equal(t, int64(200), result.AfterWindowSize)
	})

	t.Run("both before and after streams", func(t *testing.T) {
		beforeStreams := []*LeftoverStreamGroup{
			{
				LabelsHash:            1,
				Streams:               []LeftoverStreamInfo{{UncompressedSize: 100}},
				TotalUncompressedSize: 100,
			},
		}
		afterStreams := []*LeftoverStreamGroup{
			{
				LabelsHash:            2,
				Streams:               []LeftoverStreamInfo{{UncompressedSize: 200}},
				TotalUncompressedSize: 200,
			},
		}

		result := planner.planLeftovers(beforeStreams, afterStreams)

		require.NotNil(t, result)
		require.Len(t, result.BeforeWindow, 1)
		require.Equal(t, int64(100), result.BeforeWindowSize)
		require.Len(t, result.AfterWindow, 1)
		require.Equal(t, int64(200), result.AfterWindowSize)
	})
}

func TestBuildPlanFromIndexes(t *testing.T) {
	ctx := context.Background()
	windowStart := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("single tenant single index", func(t *testing.T) {
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{StreamID: 1, Index: "index1"},
							LabelsHash:       100,
							SegmentKey:       "svc1",
							UncompressedSize: 1000,
						},
						{
							Stream:           compactionpb.Stream{StreamID: 2, Index: "index1"},
							LabelsHash:       200,
							SegmentKey:       "svc1",
							UncompressedSize: 2000,
						},
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{{Path: "index1", Tenant: "tenant1"}}

		plans, leftoverPlan, err := planner.buildPlanFromIndexes(ctx, indexes, windowStart, windowEnd)

		require.NoError(t, err)
		require.Len(t, plans, 1)
		require.Contains(t, plans, "tenant1")

		plan := plans["tenant1"]
		require.Equal(t, int64(3000), plan.TotalUncompressedSize)
		require.Len(t, plan.OutputObjects, 1) // Small tenant, single output object
		require.Nil(t, leftoverPlan)          // No leftover data

		// Verify stream identifiers
		streams := plan.OutputObjects[0].Streams
		require.Len(t, streams, 2)
		streamIDs := map[int64]string{}
		for _, s := range streams {
			streamIDs[s.StreamID] = s.Index
		}
		require.Equal(t, "index1", streamIDs[1])
		require.Equal(t, "index1", streamIDs[2])
	})

	t.Run("single tenant multiple indexes with same stream", func(t *testing.T) {
		// Same stream (same LabelsHash) appears in two indexes
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{StreamID: 1, Index: "index1"},
							LabelsHash:       100, // Same labels hash
							SegmentKey:       "svc1",
							UncompressedSize: 1000,
						},
					},
				},
				"index2": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{StreamID: 1, Index: "index2"},
							LabelsHash:       100, // Same labels hash - should be grouped together
							SegmentKey:       "svc1",
							UncompressedSize: 1500,
						},
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{
			{Path: "index1", Tenant: "tenant1"},
			{Path: "index2", Tenant: "tenant1"},
		}

		plans, _, err := planner.buildPlanFromIndexes(ctx, indexes, windowStart, windowEnd)

		require.NoError(t, err)
		require.Len(t, plans, 1)

		plan := plans["tenant1"]
		require.Equal(t, int64(2500), plan.TotalUncompressedSize)

		// Both stream entries should be in the same output object
		require.Len(t, plan.OutputObjects, 1)
		streams := plan.OutputObjects[0].Streams
		require.Len(t, streams, 2) // Two stream entries from different indexes

		// Verify both indexes are represented (same StreamID but different Index paths)
		indexesFound := make(map[string]bool)
		for _, s := range streams {
			require.Equal(t, int64(1), s.StreamID) // Same stream ID
			indexesFound[s.Index] = true
		}
		require.True(t, indexesFound["index1"], "index1 should be present")
		require.True(t, indexesFound["index2"], "index2 should be present")
	})

	t.Run("multiple tenants", func(t *testing.T) {
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{StreamID: 1, Index: "index1"},
							LabelsHash:       100,
							SegmentKey:       "svc1",
							UncompressedSize: 1000,
						},
					},
				},
				"index2": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{StreamID: 2, Index: "index2"},
							LabelsHash:       200,
							SegmentKey:       "svc2",
							UncompressedSize: 2000,
						},
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{
			{Path: "index1", Tenant: "tenant1"},
			{Path: "index2", Tenant: "tenant2"},
		}

		plans, _, err := planner.buildPlanFromIndexes(ctx, indexes, windowStart, windowEnd)

		require.NoError(t, err)
		require.Len(t, plans, 2)
		require.Contains(t, plans, "tenant1")
		require.Contains(t, plans, "tenant2")

		require.Equal(t, int64(1000), plans["tenant1"].TotalUncompressedSize)
		require.Equal(t, int64(2000), plans["tenant2"].TotalUncompressedSize)

		// Verify tenant1 has the correct stream identifier
		require.Len(t, plans["tenant1"].OutputObjects, 1)
		require.Len(t, plans["tenant1"].OutputObjects[0].Streams, 1)
		require.Equal(t, int64(1), plans["tenant1"].OutputObjects[0].Streams[0].StreamID)
		require.Equal(t, "index1", plans["tenant1"].OutputObjects[0].Streams[0].Index)

		// Verify tenant2 has the correct stream identifier
		require.Len(t, plans["tenant2"].OutputObjects, 1)
		require.Len(t, plans["tenant2"].OutputObjects[0].Streams, 1)
		require.Equal(t, int64(2), plans["tenant2"].OutputObjects[0].Streams[0].StreamID)
		require.Equal(t, "index2", plans["tenant2"].OutputObjects[0].Streams[0].Index)
	})

	t.Run("with leftover streams", func(t *testing.T) {
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{StreamID: 1, Index: "index1"},
							LabelsHash:       100,
							SegmentKey:       "svc1",
							UncompressedSize: 1000,
						},
					},
					LeftoverBeforeStreams: []LeftoverStreamInfo{
						{
							TenantStream:     compactionpb.TenantStream{Tenant: "tenant1"},
							LabelsHash:       100,
							UncompressedSize: 500,
						},
					},
					LeftoverAfterStreams: []LeftoverStreamInfo{
						{
							TenantStream:     compactionpb.TenantStream{Tenant: "tenant1"},
							LabelsHash:       100,
							UncompressedSize: 300,
						},
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{{Path: "index1", Tenant: "tenant1"}}

		plans, leftoverPlan, err := planner.buildPlanFromIndexes(ctx, indexes, windowStart, windowEnd)

		require.NoError(t, err)
		require.Len(t, plans, 1)

		// Verify leftover plan was created
		require.NotNil(t, leftoverPlan)
		require.Equal(t, int64(500), leftoverPlan.BeforeWindowSize)
		require.Equal(t, int64(300), leftoverPlan.AfterWindowSize)
	})

	t.Run("leftover streams aggregated across tenants", func(t *testing.T) {
		// Same stream (same LabelsHash) has leftover data from two tenants
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{StreamID: 1, Index: "index1"},
							LabelsHash:       100,
							SegmentKey:       "svc1",
							UncompressedSize: 1000,
						},
					},
					LeftoverBeforeStreams: []LeftoverStreamInfo{
						{
							TenantStream:     compactionpb.TenantStream{Tenant: "tenant1"},
							LabelsHash:       100,
							UncompressedSize: 500,
						},
					},
				},
				"index2": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{StreamID: 2, Index: "index2"},
							LabelsHash:       200,
							SegmentKey:       "svc1",
							UncompressedSize: 2000,
						},
					},
					LeftoverBeforeStreams: []LeftoverStreamInfo{
						{
							TenantStream:     compactionpb.TenantStream{Tenant: "tenant2"},
							LabelsHash:       100, // Same labels hash as tenant1's leftover
							UncompressedSize: 700,
						},
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{
			{Path: "index1", Tenant: "tenant1"},
			{Path: "index2", Tenant: "tenant2"},
		}

		_, leftoverPlan, err := planner.buildPlanFromIndexes(ctx, indexes, windowStart, windowEnd)

		require.NoError(t, err)
		require.NotNil(t, leftoverPlan)

		// Leftover data with same LabelsHash should be aggregated
		require.Equal(t, int64(1200), leftoverPlan.BeforeWindowSize) // 500 + 700
	})
}

func TestBuildTenantPlan(t *testing.T) {
	ctx := context.Background()
	windowStart := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("small tenant skips bin packing", func(t *testing.T) {
		// Total size < targetUncompressedSize
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{StreamID: 1, Index: "index1"},
							LabelsHash:       100,
							SegmentKey:       "svc1",
							UncompressedSize: 1000,
						},
						{
							Stream:           compactionpb.Stream{StreamID: 2, Index: "index1"},
							LabelsHash:       200,
							SegmentKey:       "svc2",
							UncompressedSize: 2000,
						},
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{{Path: "index1", Tenant: "tenant1"}}

		plan, err := planner.buildTenantPlan(ctx, "tenant1", indexes, windowStart, windowEnd)

		require.NoError(t, err)
		require.Equal(t, int64(3000), plan.TotalUncompressedSize)

		// Small tenant: single output object with all streams
		require.Len(t, plan.OutputObjects, 1)
		streams := plan.OutputObjects[0].Streams
		require.Len(t, streams, 2)
		require.Equal(t, int32(1), plan.OutputObjects[0].NumOutputObjects)

		// Verify stream identifiers
		streamIDs := map[int64]string{}
		for _, s := range streams {
			streamIDs[s.StreamID] = s.Index
		}
		require.Equal(t, "index1", streamIDs[1])
		require.Equal(t, "index1", streamIDs[2])
	})

	t.Run("streams grouped by segment key", func(t *testing.T) {
		// Create streams with different segment keys
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{StreamID: 1, Index: "index1"},
							LabelsHash:       100,
							SegmentKey:       "svc1",
							UncompressedSize: 1000,
						},
						{
							Stream:           compactionpb.Stream{StreamID: 2, Index: "index1"},
							LabelsHash:       200,
							SegmentKey:       "svc2",
							UncompressedSize: 2000,
						},
						{
							Stream:           compactionpb.Stream{StreamID: 3, Index: "index1"},
							LabelsHash:       300,
							SegmentKey:       "svc1", // Same segment as stream 1
							UncompressedSize: 1500,
						},
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{{Path: "index1", Tenant: "tenant1"}}

		plan, err := planner.buildTenantPlan(ctx, "tenant1", indexes, windowStart, windowEnd)

		require.NoError(t, err)
		require.Equal(t, int64(4500), plan.TotalUncompressedSize)
	})
}

func TestCollectStreamsBySegmentKey(t *testing.T) {
	ctx := context.Background()
	windowStart := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("groups streams by segment key and labels hash", func(t *testing.T) {
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{Stream: compactionpb.Stream{StreamID: 1, Index: "index1"}, LabelsHash: 100, SegmentKey: "svc1", UncompressedSize: 1000},
						{Stream: compactionpb.Stream{StreamID: 2, Index: "index1"}, LabelsHash: 200, SegmentKey: "svc1", UncompressedSize: 2000},
						{Stream: compactionpb.Stream{StreamID: 3, Index: "index1"}, LabelsHash: 300, SegmentKey: "svc2", UncompressedSize: 3000},
					},
				},
				"index2": {
					Streams: []StreamInfo{
						{Stream: compactionpb.Stream{StreamID: 1, Index: "index2"}, LabelsHash: 100, SegmentKey: "svc1", UncompressedSize: 1500}, // Same labels as index1 stream 1
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{
			{Path: "index1", Tenant: "tenant1"},
			{Path: "index2", Tenant: "tenant1"},
		}

		result, err := planner.collectStreamsBySegmentKey(ctx, "tenant1", indexes, windowStart, windowEnd)

		require.NoError(t, err)

		// Should have 2 segment groups: svc1 and svc2
		require.Len(t, result.SegmentGroups, 2)

		svc1Group := result.SegmentGroups["svc1"]
		require.NotNil(t, svc1Group)
		// svc1 should have 2 stream groups (LabelsHash 100 and 200)
		require.Len(t, svc1Group.StreamGroups, 2)
		// Total size: 1000 + 2000 + 1500 = 4500
		require.Equal(t, int64(4500), svc1Group.TotalUncompressedSize)

		// Find the stream group with LabelsHash 100 (has streams from both indexes)
		var hash100Group *StreamGroup
		for _, sg := range svc1Group.StreamGroups {
			if sg.LabelsHash == 100 {
				hash100Group = sg
				break
			}
		}
		require.NotNil(t, hash100Group, "should have stream group with LabelsHash 100")
		require.Len(t, hash100Group.Streams, 2) // One from each index

		// Verify stream identifiers - both have StreamID 1 but different indexes
		indexesFound := make(map[string]bool)
		for _, s := range hash100Group.Streams {
			require.Equal(t, int64(1), s.StreamID)
			indexesFound[s.Index] = true
		}
		require.True(t, indexesFound["index1"])
		require.True(t, indexesFound["index2"])

		svc2Group := result.SegmentGroups["svc2"]
		require.NotNil(t, svc2Group)
		require.Len(t, svc2Group.StreamGroups, 1)
		require.Equal(t, int64(3000), svc2Group.TotalUncompressedSize)

		// Verify svc2 stream identifier
		require.Len(t, svc2Group.StreamGroups[0].Streams, 1)
		require.Equal(t, int64(3), svc2Group.StreamGroups[0].Streams[0].StreamID)
		require.Equal(t, "index1", svc2Group.StreamGroups[0].Streams[0].Index)
	})

	t.Run("streams without segment key grouped under empty string", func(t *testing.T) {
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{Stream: compactionpb.Stream{StreamID: 1, Index: "index1"}, LabelsHash: 100, SegmentKey: "", UncompressedSize: 1000},
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{{Path: "index1", Tenant: "tenant1"}}

		result, err := planner.collectStreamsBySegmentKey(ctx, "tenant1", indexes, windowStart, windowEnd)

		require.NoError(t, err)
		require.Len(t, result.SegmentGroups, 1)

		// Empty segment key
		emptyGroup := result.SegmentGroups[""]
		require.NotNil(t, emptyGroup)
		require.Len(t, emptyGroup.StreamGroups, 1)

		// Verify stream identifier
		require.Len(t, emptyGroup.StreamGroups[0].Streams, 1)
		require.Equal(t, int64(1), emptyGroup.StreamGroups[0].Streams[0].StreamID)
		require.Equal(t, "index1", emptyGroup.StreamGroups[0].Streams[0].Index)
	})

	t.Run("collects leftover streams", func(t *testing.T) {
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{Stream: compactionpb.Stream{StreamID: 1, Index: "index1"}, LabelsHash: 100, SegmentKey: "svc1", UncompressedSize: 1000},
					},
					LeftoverBeforeStreams: []LeftoverStreamInfo{
						{TenantStream: compactionpb.TenantStream{Tenant: "tenant1"}, LabelsHash: 100, UncompressedSize: 500},
						{TenantStream: compactionpb.TenantStream{Tenant: "tenant1"}, LabelsHash: 200, UncompressedSize: 300},
					},
					LeftoverAfterStreams: []LeftoverStreamInfo{
						{TenantStream: compactionpb.TenantStream{Tenant: "tenant1"}, LabelsHash: 100, UncompressedSize: 400},
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{{Path: "index1", Tenant: "tenant1"}}

		result, err := planner.collectStreamsBySegmentKey(ctx, "tenant1", indexes, windowStart, windowEnd)

		require.NoError(t, err)

		// Leftover before: 2 groups (LabelsHash 100 and 200)
		require.Len(t, result.LeftoverBeforeStreams, 2)

		// Leftover after: 1 group (LabelsHash 100)
		require.Len(t, result.LeftoverAfterStreams, 1)
	})
}
