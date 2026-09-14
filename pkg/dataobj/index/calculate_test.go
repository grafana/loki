package index

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

var testCalculatorConfig = logsobj.BuilderBaseConfig{
	TargetPageSize:          2048,
	TargetObjectSize:        1 << 22, // 4 MiB
	BufferSize:              2048 * 8,
	SectionStripeMergeLimit: 2,

	// This is set low because Pointers & Streams sections ignore section size. There must be a single pointers section per tenant to maintain state.
	TargetSectionSize: 1,
}

// createTestLogObject creates a test data object with both streams and logs sections
func createTestLogObject(t *testing.T, tenants int) *dataobj.Object {
	t.Helper()

	builder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          2048,
			TargetObjectSize:        1 << 22,
			TargetSectionSize:       1 << 21,
			BufferSize:              2048 * 8,
			SectionStripeMergeLimit: 2,
		},
	}, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)

	// Add test streams with structured metadata
	testStreams := []logproto.Stream{
		{
			Labels: `{cluster="test",app="foo",env="prod"}`,
			Entries: []push.Entry{
				{
					Timestamp: time.Unix(10, 0).UTC(),
					Line:      "hello from foo",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "123"},
						{Name: "span_id", Value: "456"},
					},
				},
				{
					Timestamp: time.Unix(15, 0).UTC(),
					Line:      "another message from foo",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "789"},
					},
				},
			},
		},
		{
			Labels: `{cluster="test",app="bar",env="dev"}`,
			Entries: []push.Entry{
				{
					Timestamp: time.Unix(20, 0).UTC(),
					Line:      "hello from bar",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "abc"},
						{Name: "user_id", Value: "user123"},
					},
				},
				{
					Timestamp: time.Unix(25, 0).UTC(),
					Line:      "error message from bar",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "def"},
						{Name: "level", Value: "error"},
					},
				},
			},
		},
	}

	for i := range tenants {
		for _, stream := range testStreams {
			err := builder.Append(fmt.Sprintf("tenant-%d", i), stream, stream.Entries[0].Timestamp)
			require.NoError(t, err)
		}
	}

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	streamSections := obj.Sections().Count(streams.CheckSection)
	require.Equal(t, tenants, streamSections)

	logSections := obj.Sections().Count(logs.CheckSection)
	require.Equal(t, tenants, logSections)

	return obj
}

func TestCalculator_Calculate_StatsShardBuckets(t *testing.T) {
	// Two streams share service_name (the default stats grouping key) but
	// land in different shard buckets. Calculate must populate
	// streamShardBuckets from labels; a missing or zeroed map would either
	// error or collapse these into one row.
	first := labels.FromStrings("service_name", "api", "instance", "0")
	firstShard := streams.ShardBucket(first)
	var second labels.Labels
	for i := 1; i < 256; i++ {
		candidate := labels.FromStrings("service_name", "api", "instance", strconv.Itoa(i))
		if streams.ShardBucket(candidate) != firstShard {
			second = candidate
			break
		}
	}
	require.NotEmpty(t, second)

	logBuilder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          2048,
			TargetObjectSize:        1 << 22,
			TargetSectionSize:       1 << 21,
			BufferSize:              2048 * 8,
			SectionStripeMergeLimit: 2,
		},
	}, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)

	ts := time.Unix(10, 0).UTC()
	for _, lbls := range []labels.Labels{first, second} {
		require.NoError(t, logBuilder.Append("tenant-1", logproto.Stream{
			Labels: lbls.String(),
			Entries: []push.Entry{{
				Timestamp: ts,
				Line:      "hello",
			}},
		}, ts))
	}

	logObj, logCloser, err := logBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = logCloser.Close() })

	indexBuilder, err := indexobj.NewBuilder(testCalculatorConfig, nil)
	require.NoError(t, err)
	calculator := NewCalculator(indexBuilder)
	require.NoError(t, calculator.Calculate(context.Background(), log.NewNopLogger(), logObj, "test/path/obj1"))

	indexObj, indexCloser, _, err := calculator.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = indexCloser.Close() })

	rows := readAllStatsRows(t, indexObj)
	require.Len(t, rows, 2)
	gotShards := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		require.Equal(t, "api", row["service_name.label.utf8"])
		gotShards[row["__shard_bucket__.int64"].(int64)] = struct{}{}
	}
	require.Equal(t, map[int64]struct{}{
		int64(firstShard):                  {},
		int64(streams.ShardBucket(second)): {},
	}, gotShards)
}

func TestCalculator_Calculate(t *testing.T) {
	logger := log.NewNopLogger()
	tenants := 4
	objects := 10

	t.Run("successful calculation from readerAt", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(testCalculatorConfig, nil)
		require.NoError(t, err)

		calculator := NewCalculator(indexBuilder)
		for i := 0; i < objects; i++ {
			obj := createTestLogObject(t, tenants)

			path := fmt.Sprintf("test/path-%d", i)
			err = calculator.Calculate(context.Background(), logger, obj, path)
			require.NoError(t, err)
		}

		// Verify we can flush the results
		obj, closer, timeRanges, err := calculator.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Greater(t, obj.Size(), int64(0))
		require.Equal(t, len(timeRanges), tenants)
		for _, timeRange := range timeRanges {
			require.NotEmpty(t, timeRange.Tenant)
			require.Equal(t, time.Unix(10, 0).UTC(), timeRange.MinTime)
			require.Equal(t, time.Unix(25, 0).UTC(), timeRange.MaxTime)
		}

		// Confirm we have multiple pointers sections
		count := obj.Sections().Count(pointers.CheckSection)
		require.GreaterOrEqual(t, count, tenants)

		requireValidPointers(t, obj)
	})

	t.Run("successful calculation from FS bucket", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(testCalculatorConfig, nil)
		require.NoError(t, err)

		bucket, err := filesystem.NewBucket(t.TempDir())
		require.NoError(t, err)

		calculator := NewCalculator(indexBuilder)
		for i := 0; i < objects; i++ {
			obj := createTestLogObject(t, tenants)

			// Upload to bucket
			reader, err := obj.Reader(context.Background())
			require.NoError(t, err)
			err = bucket.Upload(context.Background(), fmt.Sprintf("obj-%d", i), reader)
			require.NoError(t, err)
			bucketObj, err := dataobj.FromBucket(context.Background(), bucket, fmt.Sprintf("obj-%d", i), 0)
			require.NoError(t, err)

			err = calculator.Calculate(context.Background(), logger, bucketObj, fmt.Sprintf("test/path-%d", i))
			require.NoError(t, err)
		}

		// Verify we can flush the results
		obj, closer, timeRanges, err := calculator.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Greater(t, obj.Size(), int64(0))
		require.Equal(t, len(timeRanges), tenants)
		for _, timeRange := range timeRanges {
			require.NotEmpty(t, timeRange.Tenant)
			require.False(t, timeRange.MinTime.IsZero())
			require.Equal(t, timeRange.MinTime, time.Unix(10, 0).UTC())
			require.False(t, timeRange.MaxTime.IsZero())
			require.Equal(t, timeRange.MaxTime, time.Unix(25, 0).UTC())
		}

		// Confirm we have multiple pointers sections
		count := obj.Sections().Count(pointers.CheckSection)
		require.GreaterOrEqual(t, count, tenants)

		requireValidPointers(t, obj)
	})
}

func requireValidPointers(t *testing.T, obj *dataobj.Object) {
	totalPointers := 0
	pointersByTenant := make(map[string]int)
	for _, section := range obj.Sections().Filter(pointers.CheckSection) {
		require.NotEmpty(t, section.Tenant)

		sec, err := pointers.Open(context.Background(), section)
		require.NoError(t, err)

		reader := pointers.NewRowReader(sec)
		require.NoError(t, reader.Open(context.Background()))

		buf := make([]pointers.SectionPointer, 1024)
		for {
			n, err := reader.Read(context.Background(), buf)
			if !errors.Is(err, io.EOF) {
				require.NoError(t, err)
			}
			if n == 0 && errors.Is(err, io.EOF) {
				break
			}
			for _, pointer := range buf[:n] {
				require.NotEqual(t, pointer.Path, "")
				require.Greater(t, pointer.PointerKind, pointers.PointerKind(0))
				if pointer.PointerKind == pointers.PointerKindStreamIndex {
					key := fmt.Sprintf("%s:%s:%d", section.Tenant, pointer.Path, pointer.Section)
					pointersByTenant[key]++
					require.Greater(t, pointer.StreamIDRef, int64(0))
					require.Greater(t, pointer.StreamID, int64(0))
					require.Greater(t, pointer.StartTs, time.Unix(0, 0))
					require.Greater(t, pointer.EndTs, time.Unix(0, 0))
					require.Greater(t, pointer.LineCount, int64(0))
					require.Greater(t, pointer.UncompressedSize, int64(0))
				} else {
					require.Greater(t, pointer.ColumnIndex, int64(0))
					require.Greater(t, len(pointer.ValuesBloomFilter), 0)
				}
				totalPointers++
			}
		}
		require.Greater(t, totalPointers, 0)
	}

	// Expect two pointers for each object section, per tenant. This is because we write two streams to the log objects for every tenant.
	for _, count := range pointersByTenant {
		require.Equal(t, 2, count)
	}
}
