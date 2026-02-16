package indexobj

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
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
