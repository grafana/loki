package logsobj

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
)

var testBuilderConfig = BuilderConfig{
	TargetPageSize:    2048,
	TargetObjectSize:  1 << 22, // 4 MiB
	TargetSectionSize: 1 << 21, // 2 MiB

	BufferSize: 2048 * 8,

	SectionStripeMergeLimit: 2,
}

func TestBuilder(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	dirtyBuf := bytes.NewBuffer([]byte("dirty"))

	testStreams := []logproto.Stream{
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

		for _, entry := range testStreams {
			require.NoError(t, builder.Append(entry))
		}
		_, err = builder.Flush(buf)
		require.NoError(t, err)
	})

	t.Run("Read", func(t *testing.T) {
		obj, err := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		require.NoError(t, err)
		require.Equal(t, 1, obj.Sections().Count(streams.CheckSection))
		require.Equal(t, 1, obj.Sections().Count(logs.CheckSection))
	})

	t.Run("BuildWithDirtyBuffer", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig)
		require.NoError(t, err)

		for _, entry := range testStreams {
			require.NoError(t, builder.Append(entry))
		}

		_, err = builder.Flush(dirtyBuf)
		require.NoError(t, err)

		require.Equal(t, buf.Len(), dirtyBuf.Len()-5)
	})

	t.Run("ReadFromDirtyBuffer", func(t *testing.T) {
		obj, err := dataobj.FromReaderAt(bytes.NewReader(dirtyBuf.Bytes()[5:]), int64(dirtyBuf.Len()-5))
		require.NoError(t, err)
		require.Equal(t, 1, obj.Sections().Count(streams.CheckSection))
		require.Equal(t, 1, obj.Sections().Count(logs.CheckSection))
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

// TestBuilder_Append_MaxAge ensures that the buffer eventually reports full
// once max age is reached.
func TestBuilder_Append_MaxAge(t *testing.T) {
	clock := quartz.NewMock(t)
	cfg := testBuilderConfig
	cfg.MaxAge = time.Minute
	builder, err := NewBuilder(cfg)
	require.NoError(t, err)
	builder.clock = clock
	stream := logproto.Stream{
		Labels: `{cluster="test",app="foo"}`,
		Entries: []push.Entry{{
			Timestamp: time.Now().UTC(),
			Line:      "a",
		}},
	}
	require.NoError(t, builder.Append(stream))
	// Advance the clock. This should not fail as we have not exceeded the
	// max age.
	clock.Advance(time.Minute)
	require.NoError(t, builder.Append(stream))
	// Advance the clock once more, we should now have exceeded the max age.
	clock.Advance(time.Second)
	require.EqualError(t, builder.Append(stream), "builder full")
}
