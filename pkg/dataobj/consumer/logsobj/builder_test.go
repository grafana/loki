package logsobj

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
)

var testBuilderConfig = BuilderConfig{
	TargetPageSize:    2048,
	TargetObjectSize:  1 << 20, // 1 MiB
	TargetSectionSize: 8 << 10, // 8 KiB

	BufferSize:              2048 * 8,
	SectionStripeMergeLimit: 2,
}

func TestBuilder(t *testing.T) {
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
		builder, err := NewBuilder(testBuilderConfig, nil)
		require.NoError(t, err)

		for _, entry := range testStreams {
			require.NoError(t, builder.Append("tenant", entry))
		}
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, 1, obj.Sections().Count(streams.CheckSection))
		require.Equal(t, 1, obj.Sections().Count(logs.CheckSection))
	})
}

// TestBuilder_Append ensures that appending to the buffer eventually reports
// that the buffer is full.
func TestBuilder_Append(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	builder, err := NewBuilder(testBuilderConfig, nil)
	require.NoError(t, err)

	tenant := "test"

	for {
		require.NoError(t, ctx.Err())

		err := builder.Append(tenant, logproto.Stream{
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

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	// When a section builder is reset, which happens on ErrBuilderFull, the
	// tenant is reset too. We must check that the tenant is added back
	// to the section builder otherwise tenant will be absent from successive
	// sections.
	secs := obj.Sections()
	require.Equal(t, 1, secs.Count(streams.CheckSection))
	require.Greater(t, secs.Count(logs.CheckSection), 1)
	for _, section := range secs.Filter(logs.CheckSection) {
		require.Equal(t, tenant, section.Tenant)
	}
}

func TestBuilder_CopyAndSort(t *testing.T) {
	builder, _ := NewBuilder(testBuilderConfig, nil)

	now := time.Date(2025, time.September, 17, 0, 0, 0, 0, time.UTC)
	numRows := 16 // 16 rows with 1KiB each line and 8KiB section size ~> 2 logs sections per tenant

	for _, tenant := range []string{"tenant-a", "tenant-b", "tenant-c"} {
		for i := range numRows {
			err := builder.Append(tenant, logproto.Stream{
				Labels: `{cluster="test",app="foo"}`,
				Entries: []push.Entry{{
					Timestamp: now.Add(time.Duration(i%8) * time.Second),
					Line:      strings.Repeat("a", 1024), // 1KiB log line
				}},
			})
			require.NoError(t, err)
		}
	}

	obj1, closer1, err := builder.Flush()
	require.NoError(t, err)
	defer closer1.Close()

	newBuilder, _ := NewBuilder(testBuilderConfig, nil)

	obj2, closer2, err := newBuilder.CopyAndSort(obj1)
	require.NoError(t, err)
	defer closer2.Close()

	for i, obj := range []*dataobj.Object{obj1, obj2} {
		t.Log(" === dataobj", i)
		t.Log("Size:   ", obj.Size())
		t.Log("Tenants:", obj.Tenants())
		for i, section := range obj.Sections() {
			t.Log("Section:", i, section.Tenant, section.Type.String())
		}
	}

	require.Equal(
		t,
		obj1.Sections().Count(streams.CheckSection),
		obj2.Sections().Count(streams.CheckSection),
		"objects have different amount of streams sections",
	)
	require.Equal(
		t,
		obj1.Sections().Count(logs.CheckSection),
		obj2.Sections().Count(logs.CheckSection),
		"objects have different amount of logs sections",
	)

	// Assert DESC timestamp ordering across sections of a tenant
	for _, tenant := range []string{"tenant-a", "tenant-b", "tenant-c"} {
		prevTs := time.Unix(0, math.MaxInt64)
		for _, sec := range obj2.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == tenant
		}) {
			for res := range iterLogsSection(t, sec) {
				val, _ := res.Value()
				require.LessOrEqual(t, val.Timestamp, prevTs)
				prevTs = val.Timestamp
			}
		}
	}
}

func iterLogsSection(t *testing.T, section *dataobj.Section) result.Seq[logs.Record] {
	t.Helper()
	ctx := t.Context()

	return result.Iter(func(yield func(logs.Record) bool) error {
		logsSection, err := logs.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}
		for result := range logs.IterSection(ctx, logsSection) {
			if result.Err() != nil || !yield(result.MustValue()) {
				return result.Err()
			}
		}
		return nil
	})
}
