package objtest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v4/pkg/dataobj"
	"github.com/grafana/loki/v4/pkg/dataobj/metastore"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v4/pkg/logproto"
	"github.com/grafana/loki/v4/pkg/logql/syntax"

	"github.com/grafana/loki/pkg/push"
)

// logsSectionsPerObject writes eight streams into one object and returns the highest number of
// logs sections any single object ended up holding for the tenant.
func logsSectionsPerObject(t *testing.T, opts ...Option) int {
	t.Helper()

	builder := NewBuilder(t, opts...)
	ctx := user.InjectOrgID(t.Context(), Tenant)
	for i := 0; i < 8; i++ {
		builder.Append(ctx, logproto.Stream{
			Labels:  fmt.Sprintf(`{app="many", idx="%d"}`, i),
			Entries: []push.Entry{{Timestamp: time.Unix(1, 0).UTC(), Line: "line"}},
		})
	}
	builder.Close()

	resp, err := builder.Metastore().Sections(ctx, metastore.SectionsRequest{
		Start:    time.Unix(0, 0).UTC(),
		End:      time.Unix(100, 0).UTC(),
		Matchers: syntax.MustParseLogSelector(`{app=~".+"}`, true).Matchers(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Sections, "the metastore resolved no section for the streams written")

	sectionsByObject := map[string]map[int64]struct{}{}
	for _, descriptor := range resp.Sections {
		if sectionsByObject[descriptor.ObjectPath] == nil {
			sectionsByObject[descriptor.ObjectPath] = map[int64]struct{}{}
		}
		sectionsByObject[descriptor.ObjectPath][descriptor.SectionIdx] = struct{}{}
	}

	var most int
	for _, sections := range sectionsByObject {
		most = max(most, len(sections))
	}
	return most
}

// TestWithTargetSectionSize guards the layout the option promises. A caller asks for a tiny
// section to exercise a multi-section object, and would otherwise get a single-section object
// that tests less than it claims to.
func TestWithTargetSectionSize(t *testing.T) {
	t.Run("a tiny target size splits one object's streams across several logs sections", func(t *testing.T) {
		require.Greater(t, logsSectionsPerObject(t, WithTargetSectionSize(flagext.Bytes(1))), 1)
	})

	t.Run("the default target size holds them all in one section", func(t *testing.T) {
		require.Equal(t, 1, logsSectionsPerObject(t))
	})
}

func TestBuilderClose(t *testing.T) {
	t.Run("indexes logs whose time range crosses a ToC window boundary", func(t *testing.T) {
		boundary := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		require.Equal(t, boundary, boundary.Truncate(metastore.MetastoreWindowSize))

		builder := NewBuilder(t)
		ctx := user.InjectOrgID(t.Context(), Tenant)
		builder.Append(ctx, logproto.Stream{
			Labels: `{app="boundary"}`,
			Entries: []push.Entry{
				{Timestamp: boundary.Add(-time.Minute), Line: "before"},
				{Timestamp: boundary.Add(time.Minute), Line: "after"},
			},
		})
		builder.Close()

		for _, window := range []struct{ start, end time.Time }{
			{boundary.Add(-time.Hour), boundary.Add(-time.Second)},
			{boundary.Add(time.Second), boundary.Add(time.Hour)},
		} {
			resp, err := builder.Metastore().Sections(ctx, metastore.SectionsRequest{
				Start:    window.start,
				End:      window.end,
				Matchers: syntax.MustParseLogSelector(`{app="boundary"}`, true).Matchers(),
			})
			require.NoError(t, err)
			require.NotEmpty(t, resp.Sections, "no section for %s to %s", window.start, window.end)
		}
	})

	t.Run("writes one index when the objects of one layout are fewer than one index covers", func(t *testing.T) {
		const count = objectsPerIndex - 1
		builder := NewBuilder(t)
		ctx := user.InjectOrgID(t.Context(), Tenant)
		for i := range count {
			builder.Append(ctx, logproto.Stream{
				Labels:  fmt.Sprintf(`{app="objects", idx="%d"}`, i),
				Entries: []push.Entry{{Timestamp: time.Unix(1, 0).UTC(), Line: "line"}},
			})
			builder.Flush(ctx)
		}
		builder.Close()

		require.Equal(t, 1, countIndexObjects(t, builder))
		require.Len(t, resolveSections(t, builder), count)
	})

	t.Run("writes one index when the objects of one layout fill exactly one index", func(t *testing.T) {
		const count = objectsPerIndex
		builder := NewBuilder(t)
		ctx := user.InjectOrgID(t.Context(), Tenant)
		for i := range count {
			builder.Append(ctx, logproto.Stream{
				Labels:  fmt.Sprintf(`{app="objects", idx="%d"}`, i),
				Entries: []push.Entry{{Timestamp: time.Unix(1, 0).UTC(), Line: "line"}},
			})
			builder.Flush(ctx)
		}
		builder.Close()

		require.Equal(t, 1, countIndexObjects(t, builder))
		require.Len(t, resolveSections(t, builder), count)
	})

	t.Run("writes a second index for the object of one layout that does not fit in the first", func(t *testing.T) {
		const count = objectsPerIndex + 1
		builder := NewBuilder(t)
		ctx := user.InjectOrgID(t.Context(), Tenant)
		for i := range count {
			builder.Append(ctx, logproto.Stream{
				Labels:  fmt.Sprintf(`{app="objects", idx="%d"}`, i),
				Entries: []push.Entry{{Timestamp: time.Unix(1, 0).UTC(), Line: "line"}},
			})
			builder.Flush(ctx)
		}
		builder.Close()

		require.Equal(t, 2, countIndexObjects(t, builder))
		require.Len(t, resolveSections(t, builder), count)
	})
}

func TestWithTenantOverrides(t *testing.T) {
	t.Run("writes each object with the shard layout the overrides gave when it was written, and indexes them all", func(t *testing.T) {
		overrides := &shardOverrides{}
		builder := NewBuilder(t, WithTenantOverrides(overrides))
		ctx := user.InjectOrgID(t.Context(), Tenant)

		wantBuckets := []uint32{8, 16, 8, 16}
		for i, buckets := range wantBuckets {
			overrides.layout = streams.MustShardLayoutFromBuckets(buckets)
			builder.Append(ctx, logproto.Stream{
				Labels:  fmt.Sprintf(`{app="objects", idx="%d"}`, i),
				Entries: []push.Entry{{Timestamp: time.Unix(1, 0).UTC(), Line: "line"}},
			})
			builder.Flush(ctx)
		}
		builder.Close()

		var gotBuckets []uint32
		for _, descriptor := range resolveSections(t, builder) {
			obj, err := dataobj.FromBucket(t.Context(), builder.Location().Bucket, descriptor.ObjectPath, 0)
			require.NoError(t, err)
			layout, err := streamsLayout(t.Context(), obj)
			require.NoError(t, err)
			gotBuckets = append(gotBuckets, layout.ShardLayout().Buckets())
		}
		require.ElementsMatch(t, wantBuckets, gotBuckets)
	})

	t.Run("indexes every object when one object of a full index has another shard layout", func(t *testing.T) {
		const count = objectsPerIndex
		overrides := &shardOverrides{}
		builder := NewBuilder(t, WithTenantOverrides(overrides))
		ctx := user.InjectOrgID(t.Context(), Tenant)

		for i := range count {
			overrides.layout = streams.MustShardLayoutFromBuckets(16)
			if i == 5 {
				overrides.layout = streams.MustShardLayoutFromBuckets(8)
			}
			builder.Append(ctx, logproto.Stream{
				Labels:  fmt.Sprintf(`{app="objects", idx="%d"}`, i),
				Entries: []push.Entry{{Timestamp: time.Unix(1, 0).UTC(), Line: "line"}},
			})
			builder.Flush(ctx)
		}
		builder.Close()

		require.Len(t, resolveSections(t, builder), count)
	})

	t.Run("indexes every object when the sort schema changes and the shard layout does not", func(t *testing.T) {
		overrides := &shardOverrides{layout: streams.MustShardLayoutFromBuckets(16)}
		builder := NewBuilder(t, WithTenantOverrides(overrides))
		ctx := user.InjectOrgID(t.Context(), Tenant)

		for i := range 4 {
			overrides.schema = nil
			if i%2 == 1 {
				overrides.schema = []string{"label:app"}
			}
			builder.Append(ctx, logproto.Stream{
				Labels:  fmt.Sprintf(`{app="objects", idx="%d"}`, i),
				Entries: []push.Entry{{Timestamp: time.Unix(1, 0).UTC(), Line: "line"}},
			})
			builder.Flush(ctx)
		}
		builder.Close()

		require.Len(t, resolveSections(t, builder), 4)
	})
}

// shardOverrides gives every tenant the shard layout and the sort schema in its fields.
type shardOverrides struct {
	layout streams.ShardLayout
	schema []string
}

func (o *shardOverrides) SortSchemaLabels(string) []string { return o.schema }

func (o *shardOverrides) DataObjShardLayout(string) streams.ShardLayout { return o.layout }

// resolveSections returns the sections that the metastore of builder resolves for the streams
// with the label app="objects".
func resolveSections(t *testing.T, builder *Builder) metastore.DataobjSectionDescriptors {
	t.Helper()

	ctx := user.InjectOrgID(t.Context(), Tenant)
	resp, err := builder.Metastore().Sections(ctx, metastore.SectionsRequest{
		Start:    time.Unix(0, 0).UTC(),
		End:      time.Unix(100, 0).UTC(),
		Matchers: syntax.MustParseLogSelector(`{app="objects"}`, true).Matchers(),
	})
	require.NoError(t, err)
	return resp.Sections
}

// countIndexObjects returns the number of index objects in the bucket of builder.
func countIndexObjects(t *testing.T, builder *Builder) int {
	t.Helper()

	var count int
	require.NoError(t, builder.Location().Bucket.Iter(t.Context(), indexPrefix, func(name string) error {
		if strings.Contains(name, "/indexes/") {
			count++
		}
		return nil
	}, objstore.WithRecursiveIter()))
	return count
}
