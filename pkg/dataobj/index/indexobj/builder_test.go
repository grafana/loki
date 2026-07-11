package indexobj

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

var testBuilderConfig = logsobj.BuilderBaseConfig{
	TargetPageSize:    2048,
	TargetObjectSize:  1 << 22, // 4 MiB
	TargetSectionSize: 1 << 21, // 2 MiB

	BufferSize: 2048 * 8,

	SectionStripeMergeLimit: 2,
}

const testTenant = "test-tenant"

func TestBuilder(t *testing.T) {
	testStreams := []streams.Stream{
		{
			ID: 1,
			Labels: labels.New(
				labels.Label{Name: "cluster", Value: "test"},
				labels.Label{Name: "app", Value: "foo"},
			),
			Rows:             2,
			MinTimestamp:     time.Unix(10, 0).UTC(),
			MaxTimestamp:     time.Unix(20, 0).UTC(),
			UncompressedSize: 200,
		},
		{
			ID: 2,
			Labels: labels.New(
				labels.Label{Name: "cluster", Value: "test"},
				labels.Label{Name: "app", Value: "bar"},
			),
			Rows:             3,
			MinTimestamp:     time.Unix(15, 0).UTC(),
			MaxTimestamp:     time.Unix(25, 0).UTC(),
			UncompressedSize: 100,
		},
	}

	testPointers := []pointers.SectionPointer{
		{
			Path:              "test/path",
			Section:           1,
			ColumnName:        "foo",
			ColumnIndex:       1,
			ValuesBloomFilter: []byte{1, 2, 3},
		},
		{
			Path:              "test/path2",
			Section:           2,
			ColumnName:        "bar2",
			ColumnIndex:       2,
			ValuesBloomFilter: []byte{1, 2, 3, 4},
		},
	}

	t.Run("Build", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil)
		require.NoError(t, err)

		for _, stream := range testStreams {
			_, err := builder.AppendStream(testTenant, stream)
			require.NoError(t, err)
		}
		for _, pointer := range testPointers {
			err := builder.AppendColumnIndex(testTenant, pointer.Path, pointer.Section, pointer.ColumnName, pointer.ColumnIndex, pointer.ValuesBloomFilter)
			require.NoError(t, err)
		}

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, 1, obj.Sections().Count(streams.CheckSection))
		require.Equal(t, 1, obj.Sections().Count(pointers.CheckSection))
		require.Equal(t, 0, obj.Sections().Count(logs.CheckSection))
		require.Equal(t, 0, obj.Sections().Count(indexpointers.CheckSection))
	})

	t.Run("BuildMultiTenant", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil)
		require.NoError(t, err)

		tenants := []string{"test-tenant-1", "test-tenant-2"}

		for i, stream := range testStreams {
			tenant := tenants[i%len(tenants)]
			_, err := builder.AppendStream(tenant, stream)
			require.NoError(t, err)
		}
		for i, pointer := range testPointers {
			tenant := tenants[i%len(tenants)]
			err := builder.AppendColumnIndex(tenant, pointer.Path, pointer.Section, pointer.ColumnName, pointer.ColumnIndex, pointer.ValuesBloomFilter)
			require.NoError(t, err)
		}

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, len(tenants), obj.Sections().Count(streams.CheckSection))
		require.Equal(t, len(tenants), obj.Sections().Count(pointers.CheckSection))
		require.Equal(t, 0, obj.Sections().Count(logs.CheckSection))
		require.Equal(t, 0, obj.Sections().Count(indexpointers.CheckSection))
	})
}

// TestBuilder_Append ensures that appending to the buffer eventually reports
// that the buffer is full.
func TestBuilder_Append(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	builder, err := NewBuilder(testBuilderConfig, nil)
	require.NoError(t, err)

	i := 0
	for {
		require.NoError(t, ctx.Err())

		_, err := builder.AppendStream(testTenant, streams.Stream{
			ID: 1,
			Labels: labels.New(
				labels.Label{Name: "cluster", Value: "test"},
				labels.Label{Name: "app", Value: "foo"},
				labels.Label{Name: "i", Value: fmt.Sprintf("%d", i)},
			),
			Rows:         2,
			MinTimestamp: time.Unix(10, 0).UTC(),
			MaxTimestamp: time.Unix(20, 0).UTC(),
		})
		if builder.IsFull() {
			break
		}
		require.NoError(t, err)
		i++
	}
}

func TestBuilder_AppendIndexPointer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	builder, err := NewBuilder(testBuilderConfig, nil)
	require.NoError(t, err)

	i := 0
	for {
		require.NoError(t, ctx.Err())

		err := builder.AppendIndexPointer(testTenant, fmt.Sprintf("test/path-%d", i), time.Unix(10, 0).Add(time.Duration(i)*time.Second).UTC(), time.Unix(20, 0).Add(time.Duration(i)*time.Second).UTC())
		if builder.IsFull() {
			break
		}
		require.NoError(t, err)
		i++
	}
}

func TestBuilder_ObserveLogLine(t *testing.T) {
	builder, err := NewBuilder(testBuilderConfig, nil)
	require.NoError(t, err)

	err = builder.ObserveLogLine(testTenant, "test/path", 1, 1, 1, time.Unix(10, 0).UTC(), 100)
	require.NoError(t, err)

	require.Greater(t, builder.estimatedSize(), 0)
}

func BenchmarkIndexObjBuilder_ObserveLogLine(b *testing.B) {
	builder, err := NewBuilder(testBuilderConfig, nil)
	require.NoError(b, err)

	maxTenants := 1000
	tenants := make([]string, maxTenants)
	for i := range tenants {
		tenants[i] = fmt.Sprintf("test-tenant-%d", i)
	}

	for b.Loop() {
		for _, tenant := range tenants {
			err := builder.ObserveLogLine(tenant, "test/path", 1, 1, 1, time.Unix(10, 0).UTC(), 100)
			require.NoError(b, err)
		}
	}
}

func TestBuilder_TimeRanges_PostingsOnly(t *testing.T) {
	b, err := NewBuilder(testBuilderConfig, nil)
	require.NoError(t, err)

	base := time.Unix(8000, 0).UTC()
	tenant := "tenant-a"

	b.ObserveLabelPosting(tenant, postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
		StreamID: 1, Timestamp: base,
	})
	b.ObserveLabelPosting(tenant, postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "y",
		StreamID: 2, Timestamp: base.Add(time.Hour),
	})

	ranges := b.TimeRanges()
	require.Len(t, ranges, 1)
	require.Equal(t, tenant, ranges[0].Tenant)
	require.Equal(t, base, ranges[0].MinTime)
	require.Equal(t, base.Add(time.Hour), ranges[0].MaxTime)
}

func TestBuilder_TimeRanges_MultiTenantUnion(t *testing.T) {
	b, err := NewBuilder(testBuilderConfig, nil)
	require.NoError(t, err)

	base := time.Unix(9000, 0).UTC()

	// tenant-a: postings only.
	b.ObserveLabelPosting("tenant-a", postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
		StreamID: 1, Timestamp: base,
	})
	// tenant-b: postings only, different window.
	b.ObserveLabelPosting("tenant-b", postings.LabelObservation{
		ObjectPath: "/b", SectionIndex: 0, ColumnName: "app", LabelValue: "z",
		StreamID: 1, Timestamp: base.Add(2 * time.Hour),
	})

	ranges := b.TimeRanges()
	require.Len(t, ranges, 2)

	byTenant := map[string]multitenancy.TimeRange{}
	for _, r := range ranges {
		byTenant[r.Tenant] = r
	}
	require.Equal(t, base, byTenant["tenant-a"].MinTime)
	require.Equal(t, base, byTenant["tenant-a"].MaxTime)
	require.Equal(t, base.Add(2*time.Hour), byTenant["tenant-b"].MinTime)
	require.Equal(t, base.Add(2*time.Hour), byTenant["tenant-b"].MaxTime)
}

func TestBuilder_TimeRanges_StreamsAndPostingsUnion(t *testing.T) {
	b, err := NewBuilder(testBuilderConfig, nil)
	require.NoError(t, err)

	base := time.Unix(10000, 0).UTC()
	tenant := "tenant-a"

	// Streams cover [base, base+1h]; postings extend the window on both ends.
	_, err = b.AppendStream(tenant, streams.Stream{
		Labels:           labels.FromStrings("app", "x"),
		MinTimestamp:     base,
		MaxTimestamp:     base.Add(time.Hour),
		UncompressedSize: 1,
	})
	require.NoError(t, err)

	b.ObserveLabelPosting(tenant, postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
		StreamID: 1, Timestamp: base.Add(-time.Hour),
	})
	b.ObserveLabelPosting(tenant, postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
		StreamID: 2, Timestamp: base.Add(2 * time.Hour),
	})

	ranges := b.TimeRanges()
	require.Len(t, ranges, 1)
	require.Equal(t, tenant, ranges[0].Tenant)
	require.Equal(t, base.Add(-time.Hour), ranges[0].MinTime)
	require.Equal(t, base.Add(2*time.Hour), ranges[0].MaxTime)
}

func TestUnionTimeRange(t *testing.T) {
	base := time.Unix(1000, 0).UTC()

	// Case 1: candidate has no data (candMin zero) -> accumulator returned unchanged.
	gotMin, gotMax := unionTimeRange(base, base.Add(time.Hour), time.Time{}, time.Time{})
	require.Equal(t, base, gotMin)
	require.Equal(t, base.Add(time.Hour), gotMax)

	// Case 2: accumulator zero, candidate non-zero -> candidate returned.
	gotMin, gotMax = unionTimeRange(time.Time{}, time.Time{}, base, base.Add(time.Hour))
	require.Equal(t, base, gotMin)
	require.Equal(t, base.Add(time.Hour), gotMax)

	// Case 3: both non-zero, candidate widens on both ends -> widened range.
	gotMin, gotMax = unionTimeRange(base.Add(time.Hour), base.Add(2*time.Hour), base, base.Add(3*time.Hour))
	require.Equal(t, base, gotMin)
	require.Equal(t, base.Add(3*time.Hour), gotMax)

	// Case 4: both non-zero, candidate inside accumulator -> accumulator unchanged.
	gotMin, gotMax = unionTimeRange(base, base.Add(3*time.Hour), base.Add(time.Hour), base.Add(2*time.Hour))
	require.Equal(t, base, gotMin)
	require.Equal(t, base.Add(3*time.Hour), gotMax)

	// Case 5: accumulator zero AND candidate zero -> both zero out.
	gotMin, gotMax = unionTimeRange(time.Time{}, time.Time{}, time.Time{}, time.Time{})
	require.True(t, gotMin.IsZero())
	require.True(t, gotMax.IsZero())
}

func TestBuilder_TimeRanges_AfterReset(t *testing.T) {
	b, err := NewBuilder(testBuilderConfig, nil)
	require.NoError(t, err)

	base := time.Unix(8000, 0).UTC()
	tenant := "test-tenant"

	// Observe a label posting so TimeRanges() is non-empty.
	b.ObserveLabelPosting(tenant, postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
		StreamID: 1, Timestamp: base,
	})

	ranges := b.TimeRanges()
	require.Len(t, ranges, 1)

	// After Reset, TimeRanges() must be empty.
	b.Reset()
	ranges = b.TimeRanges()
	require.Empty(t, ranges)
}
