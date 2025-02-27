package dataobj

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
)

var testBuilderConfig = BuilderConfig{
	TargetPageSize:    2048,
	TargetObjectSize:  4096,
	TargetSectionSize: 4096,

	BufferSize: 2048 * 8,
}

func TestBuilder(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	dirtyBuf := bytes.NewBuffer([]byte("dirty"))

	streams := []logproto.Stream{
		{
			Labels: `{cluster="test",app="foo"}`,
			Entries: []push.Entry{
				{
					Timestamp: time.Unix(10, 0).UTC(),
					Line:      "hello",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "123"},
					},
				},
				{
					Timestamp: time.Unix(5, 0).UTC(),
					Line:      "hello again",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "456"},
						{Name: "span_id", Value: "789"},
					},
				},
			},
		},

		{
			Labels: `{cluster="test",app="bar"}`,
			Entries: []push.Entry{
				{
					Timestamp: time.Unix(15, 0).UTC(),
					Line:      "world",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "abc"},
					},
				},
				{
					Timestamp: time.Unix(20, 0).UTC(),
					Line:      "world again",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "def"},
						{Name: "span_id", Value: "ghi"},
					},
				},
			},
		},
	}

	t.Run("Build", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig)
		require.NoError(t, err)

		for _, entry := range streams {
			require.NoError(t, builder.Append(entry))
		}
		_, err = builder.Flush(buf)
		require.NoError(t, err)
	})

	t.Run("Read", func(t *testing.T) {
		obj := FromReaderAt(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		md, err := obj.Metadata(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, md.StreamsSections)
		require.Equal(t, 1, md.LogsSections)
	})

	t.Run("BuildWithDirtyBuffer", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig)
		require.NoError(t, err)

		for _, entry := range streams {
			require.NoError(t, builder.Append(entry))
		}

		_, err = builder.Flush(dirtyBuf)
		require.NoError(t, err)

		require.Equal(t, buf.Len(), dirtyBuf.Len()-5)
	})

	t.Run("ReadFromDirtyBuffer", func(t *testing.T) {
		obj := FromReaderAt(bytes.NewReader(dirtyBuf.Bytes()[5:]), int64(dirtyBuf.Len()-5))
		md, err := obj.Metadata(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, md.StreamsSections)
		require.Equal(t, 1, md.LogsSections)
	})
}

// TestBuilder_Append ensures that appending to the buffer eventually reports
// that the buffer is full.
func TestBuilder_Append(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	builder, err := NewBuilder(testBuilderConfig)
	require.NoError(t, err)

	for {
		require.NoError(t, ctx.Err())

		err := builder.Append(logproto.Stream{
			Labels: `{cluster="test",app="foo"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now().UTC(),
				Line:      strings.Repeat("a", 1024),
			}},
		})
		if errors.Is(err, ErrBuilderFull) {
			break
		}
		require.NoError(t, err)
	}
}
