package planner

import (
	"context"
	"testing"
	"time"

	compactionpb "github.com/grafana/loki/v3/pkg/dataobj/compaction/proto"

	"github.com/stretchr/testify/require"
)

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
				Streams:               []*compactionpb.TenantStream{{Tenant: "t1"}},
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
				Streams:               []*compactionpb.TenantStream{{Tenant: "t1"}},
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
				Streams:               []*compactionpb.TenantStream{{Tenant: "t1"}},
				TotalUncompressedSize: 100,
			},
		}
		afterStreams := []*LeftoverStreamGroup{
			{
				Streams:               []*compactionpb.TenantStream{{Tenant: "t2"}},
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
							Stream:           compactionpb.Stream{ID: 1, Index: "index1"},
							LabelsHash:       100,
							UncompressedSize: 1000,
						},
						{
							Stream:           compactionpb.Stream{ID: 2, Index: "index1"},
							LabelsHash:       200,
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
			streamIDs[s.ID] = s.Index
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
							Stream:           compactionpb.Stream{ID: 1, Index: "index1"},
							LabelsHash:       100, // Same labels hash
							UncompressedSize: 1000,
						},
					},
				},
				"index2": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{ID: 1, Index: "index2"},
							LabelsHash:       100, // Same labels hash - should be grouped together
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
			require.Equal(t, int64(1), s.ID) // Same stream ID
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
							Stream:           compactionpb.Stream{ID: 1, Index: "index1"},
							LabelsHash:       100,
							UncompressedSize: 1000,
						},
					},
				},
				"index2": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{ID: 2, Index: "index2"},
							LabelsHash:       200,
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
		require.Equal(t, int64(1), plans["tenant1"].OutputObjects[0].Streams[0].ID)
		require.Equal(t, "index1", plans["tenant1"].OutputObjects[0].Streams[0].Index)

		// Verify tenant2 has the correct stream identifier
		require.Len(t, plans["tenant2"].OutputObjects, 1)
		require.Len(t, plans["tenant2"].OutputObjects[0].Streams, 1)
		require.Equal(t, int64(2), plans["tenant2"].OutputObjects[0].Streams[0].ID)
		require.Equal(t, "index2", plans["tenant2"].OutputObjects[0].Streams[0].Index)
	})

	t.Run("with leftover streams", func(t *testing.T) {
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{ID: 1, Index: "index1"},
							LabelsHash:       100,
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

	t.Run("leftover streams collected from multiple tenants", func(t *testing.T) {
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{
							Stream:           compactionpb.Stream{ID: 1, Index: "index1"},
							LabelsHash:       100,
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
							Stream:           compactionpb.Stream{ID: 2, Index: "index2"},
							LabelsHash:       200,
							UncompressedSize: 2000,
						},
					},
					LeftoverBeforeStreams: []LeftoverStreamInfo{
						{
							TenantStream:     compactionpb.TenantStream{Tenant: "tenant2", Stream: &compactionpb.Stream{ID: 2}},
							LabelsHash:       100,
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

		// Leftover streams from all tenants are collected
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
							Stream:           compactionpb.Stream{ID: 1, Index: "index1"},
							LabelsHash:       100,
							UncompressedSize: 1000,
						},
						{
							Stream:           compactionpb.Stream{ID: 2, Index: "index1"},
							LabelsHash:       200,
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
			streamIDs[s.ID] = s.Index
		}
		require.Equal(t, "index1", streamIDs[1])
		require.Equal(t, "index1", streamIDs[2])
	})
}

func TestCollectStreams(t *testing.T) {
	ctx := context.Background()
	windowStart := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("groups streams by labels hash", func(t *testing.T) {
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{Stream: compactionpb.Stream{ID: 1, Index: "index1"}, LabelsHash: 100, UncompressedSize: 1000},
						{Stream: compactionpb.Stream{ID: 2, Index: "index1"}, LabelsHash: 200, UncompressedSize: 2000},
						{Stream: compactionpb.Stream{ID: 3, Index: "index1"}, LabelsHash: 300, UncompressedSize: 3000},
					},
				},
				"index2": {
					Streams: []StreamInfo{
						{Stream: compactionpb.Stream{ID: 1, Index: "index2"}, LabelsHash: 100, UncompressedSize: 1500}, // Same labels as index1 stream 1
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{
			{Path: "index1", Tenant: "tenant1"},
			{Path: "index2", Tenant: "tenant1"},
		}

		result, err := planner.collectStreams(ctx, "tenant1", indexes, windowStart, windowEnd)

		require.NoError(t, err)

		// Should have 3 stream groups (LabelsHash 100, 200, 300)
		require.Len(t, result.StreamGroups, 3)
		// Total size: 1000 + 2000 + 3000 + 1500 = 7500
		require.Equal(t, int64(7500), result.TotalUncompressedSize)

		// Find the stream group with 2 streams (has streams from both indexes - LabelsHash 100)
		var hash100Group *StreamGroup
		for _, sg := range result.StreamGroups {
			if len(sg.Streams) == 2 {
				hash100Group = sg
				break
			}
		}
		require.NotNil(t, hash100Group, "should have stream group with 2 streams")

		// Verify stream identifiers - both have StreamID 1 but different indexes
		indexesFound := make(map[string]bool)
		for _, s := range hash100Group.Streams {
			require.Equal(t, int64(1), s.ID)
			indexesFound[s.Index] = true
		}
		require.True(t, indexesFound["index1"])
		require.True(t, indexesFound["index2"])
	})

	t.Run("collects leftover streams", func(t *testing.T) {
		mockReader := &MockIndexStreamReader{
			Results: map[string]*IndexStreamResult{
				"index1": {
					Streams: []StreamInfo{
						{Stream: compactionpb.Stream{ID: 1, Index: "index1"}, LabelsHash: 100, UncompressedSize: 1000},
					},
					LeftoverBeforeStreams: []LeftoverStreamInfo{
						{TenantStream: compactionpb.TenantStream{Tenant: "tenant1", Stream: &compactionpb.Stream{ID: 1}}, LabelsHash: 100, UncompressedSize: 500},
						{TenantStream: compactionpb.TenantStream{Tenant: "tenant1", Stream: &compactionpb.Stream{ID: 2}}, LabelsHash: 200, UncompressedSize: 300},
					},
					LeftoverAfterStreams: []LeftoverStreamInfo{
						{TenantStream: compactionpb.TenantStream{Tenant: "tenant1", Stream: &compactionpb.Stream{ID: 1}}, LabelsHash: 100, UncompressedSize: 400},
					},
				},
			},
		}

		planner := newTestPlanner(mockReader)
		indexes := []IndexInfo{{Path: "index1", Tenant: "tenant1"}}

		result, err := planner.collectStreams(ctx, "tenant1", indexes, windowStart, windowEnd)

		require.NoError(t, err)

		// Leftover before: 2 groups (LabelsHash 100 and 200)
		require.Len(t, result.LeftoverBeforeStreams, 2)

		// Leftover after: 1 group (LabelsHash 100)
		require.Len(t, result.LeftoverAfterStreams, 1)
	})
}
