package logsobj

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
)

var testMultiTenantBuilderConfig = BuilderConfig{
	TargetPageSize:    2048,
	TargetObjectSize:  1 << 22, // 4 MiB
	TargetSectionSize: 1 << 21, // 2 MiB

	BufferSize: 2048 * 8,

	SectionStripeMergeLimit: 2,
}

func TestMultiTenantBuilder(t *testing.T) {
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
		builder, err := NewMultiTenantBuilder(testMultiTenantBuilderConfig)
		require.NoError(t, err)

		for _, entry := range testStreams {
			require.NoError(t, builder.Append("test", entry))
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
		builder, err := NewMultiTenantBuilder(testMultiTenantBuilderConfig)
		require.NoError(t, err)

		for _, entry := range testStreams {
			require.NoError(t, builder.Append("test", entry))
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
func TestMultiTenantBuilder_Append(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	builder, err := NewMultiTenantBuilder(testMultiTenantBuilderConfig)
	require.NoError(t, err)

	for {
		require.NoError(t, ctx.Err())

		err := builder.Append("test", logproto.Stream{
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

func TestMultiTenantBuilder_MultiTenants(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	tenant1Streams := []logproto.Stream{{
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
	}}
	tenant2Streams := []logproto.Stream{{
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
	}}

	t.Run("Build", func(t *testing.T) {
		builder, err := NewMultiTenantBuilder(testMultiTenantBuilderConfig)
		require.NoError(t, err)
		for _, entry := range tenant1Streams {
			require.NoError(t, builder.Append("tenant1", entry))
		}
		for _, entry := range tenant2Streams {
			require.NoError(t, builder.Append("tenant2", entry))
		}
		_, err = builder.Flush(buf)
		require.NoError(t, err)
	})

	t.Run("Read", func(t *testing.T) {
		obj, err := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		require.NoError(t, err)
		// There should be two sections, one for each tenant.
		require.Equal(t, 2, obj.Sections().Count(streams.CheckSection))
		require.Equal(t, 2, obj.Sections().Count(logs.CheckSection))
		// Check that the metadata for each section contains the tenant.
		var (
			streamSections []*streams.Section
			logsSections   []*logs.Section
		)
		for _, section := range obj.Sections() {
			switch {
			case streams.CheckSection(section):
				sec, err := streams.Open(context.TODO(), section)
				require.NoError(t, err)
				streamSections = append(streamSections, sec)
			case logs.CheckSection(section):
				sec, err := logs.Open(context.TODO(), section)
				require.NoError(t, err)
				logsSections = append(logsSections, sec)
			}
		}
		require.Equal(t, "tenant1", streamSections[0].TenantID())
		require.Equal(t, "tenant2", streamSections[1].TenantID())
		require.Equal(t, "tenant1", logsSections[0].TenantID())
		require.Equal(t, "tenant2", logsSections[1].TenantID())
	})
}
