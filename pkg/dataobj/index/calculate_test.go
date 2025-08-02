package index

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

var testCalculatorConfig = indexobj.BuilderConfig{
	TargetPageSize:          2048,
	TargetObjectSize:        1 << 22, // 4 MiB
	TargetSectionSize:       256,     // 2 MiB
	BufferSize:              2048 * 8,
	SectionStripeMergeLimit: 2,
}

// createTestLogObject creates a test data object with both streams and logs sections
func createTestLogObject(t *testing.T) *dataobj.Object {
	t.Helper()

	builder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		TargetPageSize:          2048,
		TargetObjectSize:        1 << 22,
		TargetSectionSize:       1 << 21,
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	})
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

	for _, stream := range testStreams {
		err := builder.Append(stream)
		require.NoError(t, err)
	}

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	return obj
}

func TestCalculator_Calculate(t *testing.T) {
	t.Run("successful calculation", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(testCalculatorConfig)
		require.NoError(t, err)

		calculator := NewCalculator(indexBuilder)
		for i := 0; i < 10; i++ {
			obj := createTestLogObject(t)
			logger := log.NewNopLogger()

			err = calculator.Calculate(context.Background(), logger, obj, fmt.Sprintf("test/path-%d", i))
			require.NoError(t, err)
		}

		// Verify we can flush the results
		minTime, maxTime := calculator.TimeRange()
		obj, closer, err := calculator.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Greater(t, obj.Size(), int64(0))
		require.False(t, minTime.IsZero())
		require.Equal(t, minTime, time.Unix(10, 0).UTC())
		require.False(t, maxTime.IsZero())
		require.Equal(t, maxTime, time.Unix(25, 0).UTC())

		// Confirm we have multiple pointers sections
		count := obj.Sections().Count(pointers.CheckSection)
		require.Greater(t, count, 1)

		requireValidPointers(t, obj)
	})
}

func requireValidPointers(t *testing.T, obj *dataobj.Object) {
	totalPointers := 0
	for _, section := range obj.Sections().Filter(pointers.CheckSection) {
		sec, err := pointers.Open(context.Background(), section)
		require.NoError(t, err)

		reader := pointers.NewRowReader(sec)
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
}
