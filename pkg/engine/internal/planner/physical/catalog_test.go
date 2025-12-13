package physical

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestCatalog_TimeRangeValidate(t *testing.T) {
	tests := []struct {
		name      string
		start     time.Time
		end       time.Time
		expectErr bool
	}{
		{name: "Normal time range",
			start:     time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			expectErr: false,
		},
		{name: "Zero-width time range",
			start:     time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			expectErr: false,
		},
		{name: "Invalid time range",
			start:     time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			end:       time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			expectErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTimeRange(tt.start, tt.end)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCatalog_TimeRangeOverlaps(t *testing.T) {
	tests := []struct {
		name        string
		firstStart  time.Time
		firstEnd    time.Time
		secondStart time.Time
		secondEnd   time.Time
		want        bool
	}{
		{name: "Second contained in first",
			firstStart:  time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			firstEnd:    time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondStart: time.Date(2025, time.January, 1, 1, 0, 0, 0, time.UTC),
			secondEnd:   time.Date(2025, time.January, 1, 2, 0, 0, 0, time.UTC),
			want:        true,
		},
		{name: "Second completely after first",
			firstStart:  time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			firstEnd:    time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondStart: time.Date(2025, time.January, 1, 13, 0, 0, 0, time.UTC),
			secondEnd:   time.Date(2025, time.January, 1, 14, 0, 0, 0, time.UTC),
			want:        false,
		},
		{name: "Second starts in first",
			firstStart:  time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			firstEnd:    time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondStart: time.Date(2025, time.January, 1, 1, 0, 0, 0, time.UTC),
			secondEnd:   time.Date(2025, time.January, 1, 13, 0, 0, 0, time.UTC),
			want:        true,
		},
		{name: "Second ends in first",
			firstStart:  time.Date(2025, time.January, 1, 6, 0, 0, 0, time.UTC),
			firstEnd:    time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondStart: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			secondEnd:   time.Date(2025, time.January, 1, 9, 0, 0, 0, time.UTC),
			want:        true,
		},
		{name: "First end = second start",
			firstStart:  time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			firstEnd:    time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondStart: time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondEnd:   time.Date(2025, time.January, 1, 20, 0, 0, 0, time.UTC),
			want:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			firstRange, err := newTimeRange(tt.firstStart, tt.firstEnd)
			require.NoError(t, err)
			secondRange, err := newTimeRange(tt.secondStart, tt.secondEnd)
			require.NoError(t, err)
			got1 := firstRange.Overlaps(secondRange)
			got2 := secondRange.Overlaps(firstRange)
			require.Equal(t, tt.want, got1, got2)
		})
	}
}

func TestCatalog_FilterDescriptorsForShard(t *testing.T) {
	t.Run("", func(t *testing.T) {
		now := time.Now()
		start1 := now.Add(time.Second * -10)
		end1 := now.Add(time.Second * -5)
		start2 := now.Add(time.Second * -30)
		end2 := now.Add(time.Second * -20)
		start3 := now.Add(time.Second * -20)
		end3 := now.Add(time.Second * -10)
		shard := ShardInfo{Shard: 1, Of: 2}
		desc1 := metastore.DataobjSectionDescriptor{StreamIDs: []int64{1, 2}, RowCount: 10, Size: 10, Start: start1, End: end1}
		desc1.ObjectPath = "foo"
		desc1.SectionIdx = 1
		desc2 := metastore.DataobjSectionDescriptor{StreamIDs: []int64{3, 4}, RowCount: 10, Size: 10, Start: start2, End: end2}
		desc2.ObjectPath = "bar"
		desc2.SectionIdx = 2
		desc3 := metastore.DataobjSectionDescriptor{StreamIDs: []int64{1, 5}, RowCount: 10, Size: 10, Start: start3, End: end3}
		desc3.ObjectPath = "baz"
		desc3.SectionIdx = 3
		sectionDescriptors := []*metastore.DataobjSectionDescriptor{&desc1, &desc2, &desc3}
		res, err := FilterDescriptorsForShard(shard, sectionDescriptors)
		require.NoError(t, err)
		tr1, err := newTimeRange(start1, end1)
		require.NoError(t, err)
		tr3, err := newTimeRange(start3, end3)
		require.NoError(t, err)
		expected := []FilteredShardDescriptor{
			{Location: "foo", Streams: []int64{1, 2}, Sections: []int{1}, TimeRange: tr1},
			{Location: "baz", Streams: []int64{1, 5}, Sections: []int{3}, TimeRange: tr3},
		}
		require.ElementsMatch(t, res, expected)
	})
}

func TestUnresolvedCatalog_RequestCollection(t *testing.T) {
	t.Run("Collects unique requests", func(t *testing.T) {
		catalog := NewUnresolvedCatalog()
		selector := newTestLabelMatcher("app", "test")
		predicates := []Expression{newTestLabelMatcher("env", "prod")}
		shard := ShardInfo{Shard: 1, Of: 2}
		from := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
		through := time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)

		// First call should add the request
		_, err := catalog.ResolveShardDescriptorsWithShard(selector, predicates, shard, from, through)
		require.NoError(t, err)
		require.Equal(t, 1, catalog.RequestsCount())

		// Second call with same parameters should not add another request
		_, err = catalog.ResolveShardDescriptorsWithShard(selector, predicates, shard, from, through)
		require.NoError(t, err)
		require.Equal(t, 1, catalog.RequestsCount())
	})

	t.Run("Collects different requests", func(t *testing.T) {
		catalog := NewUnresolvedCatalog()
		selector := newTestLabelMatcher("app", "test")
		predicates := []Expression{newTestLabelMatcher("env", "prod")}
		shard1 := ShardInfo{Shard: 1, Of: 2}
		shard2 := ShardInfo{Shard: 2, Of: 2}
		from := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
		through := time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)

		// First request with shard 1
		_, err := catalog.ResolveShardDescriptorsWithShard(selector, predicates, shard1, from, through)
		require.NoError(t, err)
		require.Equal(t, 1, catalog.RequestsCount())

		// Second request with shard 2 should add another request
		_, err = catalog.ResolveShardDescriptorsWithShard(selector, predicates, shard2, from, through)
		require.NoError(t, err)
		require.Equal(t, 2, catalog.RequestsCount())
	})
}

func TestUnresolvedCatalog_Resolve(t *testing.T) {
	t.Run("Successfully resolves all requests", func(t *testing.T) {
		catalog := NewUnresolvedCatalog()
		selector := newTestLabelMatcher("app", "test")
		predicates := []Expression{newTestLabelMatcher("env", "prod")}
		shard := ShardInfo{Shard: 1, Of: 2}
		from := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
		through := time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)

		_, err := catalog.ResolveShardDescriptorsWithShard(selector, predicates, shard, from, through)
		require.NoError(t, err)

		tr, err := newTimeRange(from, through)
		require.NoError(t, err)

		// Resolve with a function that returns mock descriptors
		resolved, err := catalog.Resolve(func(_ CatalogRequest) (CatalogResponse, error) {
			return CatalogResponse{
				Kind: CatalogRequestKindResolveShardDescriptorsWithShard,
				Descriptors: []FilteredShardDescriptor{
					{Location: "test-location", Streams: []int64{1}, Sections: []int{1}, TimeRange: tr},
				},
			}, nil
		})
		require.NoError(t, err)

		// Verify we can retrieve the resolved descriptors
		descriptors, err := resolved.ResolveShardDescriptorsWithShard(selector, predicates, shard, from, through)
		require.NoError(t, err)
		require.Len(t, descriptors, 1)
		require.Equal(t, DataObjLocation("test-location"), descriptors[0].Location)
	})

	t.Run("Returns error when resolve function fails", func(t *testing.T) {
		catalog := NewUnresolvedCatalog()
		selector := newTestLabelMatcher("app", "test")
		shard := ShardInfo{Shard: 1, Of: 2}
		from := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
		through := time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)

		_, err := catalog.ResolveShardDescriptorsWithShard(selector, nil, shard, from, through)
		require.NoError(t, err)

		// Resolve with a function that returns an error
		_, err = catalog.Resolve(func(_ CatalogRequest) (CatalogResponse, error) {
			return CatalogResponse{}, fmt.Errorf("mock error")
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to resolve catalog response")
	})
}

func TestResolvedCatalog_ResolveShardDescriptorsWithShard(t *testing.T) {
	t.Run("Returns descriptors for resolved request", func(t *testing.T) {
		catalog := NewUnresolvedCatalog()
		selector := newTestLabelMatcher("app", "test")
		predicates := []Expression{newTestLabelMatcher("env", "prod")}
		shard := ShardInfo{Shard: 1, Of: 2}
		from := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
		through := time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)

		_, err := catalog.ResolveShardDescriptorsWithShard(selector, predicates, shard, from, through)
		require.NoError(t, err)

		tr, err := newTimeRange(from, through)
		require.NoError(t, err)

		resolved, err := catalog.Resolve(func(_ CatalogRequest) (CatalogResponse, error) {
			return CatalogResponse{
				Kind: CatalogRequestKindResolveShardDescriptorsWithShard,
				Descriptors: []FilteredShardDescriptor{
					{Location: "test-location", Streams: []int64{1, 2, 3}, Sections: []int{1, 2}, TimeRange: tr},
				},
			}, nil
		})
		require.NoError(t, err)

		descriptors, err := resolved.ResolveShardDescriptorsWithShard(selector, predicates, shard, from, through)
		require.NoError(t, err)
		require.Len(t, descriptors, 1)
		require.Equal(t, DataObjLocation("test-location"), descriptors[0].Location)
		require.Equal(t, []int64{1, 2, 3}, descriptors[0].Streams)
		require.Equal(t, []int{1, 2}, descriptors[0].Sections)
	})

	t.Run("Returns error for missing request", func(t *testing.T) {
		catalog := NewUnresolvedCatalog()
		selector := newTestLabelMatcher("app", "test")
		shard := ShardInfo{Shard: 1, Of: 2}
		from := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
		through := time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)

		resolved, err := catalog.Resolve(func(_ CatalogRequest) (CatalogResponse, error) {
			return CatalogResponse{}, nil
		})
		require.NoError(t, err)

		// Try to resolve a request that was never added
		_, err = resolved.ResolveShardDescriptorsWithShard(selector, nil, shard, from, through)
		require.Error(t, err)
		require.Contains(t, err.Error(), "catalog response missing for request")
	})
}

func TestMetastoreCatalog_ResolveShardDescriptorsWithShard(t *testing.T) {
	t.Run("Successfully resolves descriptors", func(t *testing.T) {
		ctx := context.Background()
		now := time.Now()
		start := now.Add(time.Second * -10)
		end := now.Add(time.Second * -5)

		desc1 := &metastore.DataobjSectionDescriptor{
			SectionKey: metastore.SectionKey{
				ObjectPath: "test-path",
				SectionIdx: 1,
			},
			StreamIDs: []int64{1, 2},
			RowCount:  10,
			Size:      100,
			Start:     start,
			End:       end,
		}

		mockMetastore := &mockMetastore{
			sections: []*metastore.DataobjSectionDescriptor{desc1},
		}

		catalog := NewMetastoreCatalog(ctx, mockMetastore)
		selector := newTestLabelMatcher("app", "test")
		shard := ShardInfo{Shard: 1, Of: 2}

		descriptors, err := catalog.ResolveShardDescriptorsWithShard(selector, nil, shard, start, end)
		require.NoError(t, err)
		require.Len(t, descriptors, 1)
		require.Equal(t, DataObjLocation("test-path"), descriptors[0].Location)
		require.Equal(t, []int64{1, 2}, descriptors[0].Streams)
	})

	t.Run("Returns error when metastore is nil", func(t *testing.T) {
		ctx := context.Background()
		catalog := NewMetastoreCatalog(ctx, nil)
		selector := newTestLabelMatcher("app", "test")
		shard := ShardInfo{Shard: 1, Of: 2}
		now := time.Now()

		_, err := catalog.ResolveShardDescriptorsWithShard(selector, nil, shard, now, now)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no metastore to resolve objects")
	})

	t.Run("Returns error when metastore.Sections fails", func(t *testing.T) {
		ctx := context.Background()
		mockMetastore := &mockMetastore{
			err: fmt.Errorf("metastore error"),
		}

		catalog := NewMetastoreCatalog(ctx, mockMetastore)
		selector := newTestLabelMatcher("app", "test")
		shard := ShardInfo{Shard: 1, Of: 2}
		now := time.Now()

		_, err := catalog.ResolveShardDescriptorsWithShard(selector, nil, shard, now, now)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to resolve data object sections")
	})
}

// Test helper functions

// newTestLabelMatcher creates a BinaryExpr that represents a label matcher (label = "value")
func newTestLabelMatcher(label, value string) Expression {
	return &BinaryExpr{
		Op:    types.BinaryOpEq,
		Left:  &ColumnExpr{Ref: types.ColumnRef{Column: label, Type: types.ColumnTypeLabel}},
		Right: NewLiteral(value),
	}
}

// mockMetastore is a mock implementation of the metastore.Metastore interface for testing
type mockMetastore struct {
	sections []*metastore.DataobjSectionDescriptor
	err      error
}

func (m *mockMetastore) Sections(_ context.Context, _, _ time.Time, _ []*labels.Matcher, _ []*labels.Matcher) ([]*metastore.DataobjSectionDescriptor, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.sections, nil
}

func (m *mockMetastore) Labels(_ context.Context, _, _ time.Time, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

func (m *mockMetastore) Values(_ context.Context, _, _ time.Time, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}
