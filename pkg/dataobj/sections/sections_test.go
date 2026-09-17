package sections_test

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestForTenant(t *testing.T) {
	// Read the two section types off a real object, so the test does not restate values the
	// section packages own.
	logsBuilder := logs.NewBuilder(nil, logs.BuilderOptions{StripeMergeLimit: 2, SortOrder: logs.SortStreamASC})
	logsBuilder.Append(logs.Record{StreamID: 1, Timestamp: time.Unix(1, 0), Line: []byte("line")})

	streamsBuilder := streams.NewBuilder(nil, 1, 0)
	streamsBuilder.Record(labels.FromStrings("app", "foo"), time.Unix(1, 0), 4)

	builder := dataobj.NewBuilder(nil)
	require.NoError(t, builder.Append(logsBuilder))
	require.NoError(t, builder.Append(streamsBuilder))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	var logsType, streamsType dataobj.SectionType
	for _, sec := range obj.Sections() {
		switch {
		case logs.CheckSection(sec):
			logsType = sec.Type
		case streams.CheckSection(sec):
			streamsType = sec.Type
		}
	}
	require.NotZero(t, logsType.Kind, "expected a logs section in the built object")
	require.NotZero(t, streamsType.Kind, "expected a streams section in the built object")

	logsSection := func(tenant string) *dataobj.Section {
		return &dataobj.Section{Type: logsType, Tenant: tenant}
	}
	streamsSection := func(tenant string) *dataobj.Section {
		return &dataobj.Section{Type: streamsType, Tenant: tenant}
	}

	t.Run("the logs index counts every tenant's sections", func(t *testing.T) {
		// Tenant b's only logs section is the third in the object, so it is index 2 even
		// though it is b's first. A metastore descriptor for it carries 2.
		a1, b0, a2 := logsSection("a"), logsSection("b"), logsSection("a")
		all := dataobj.Sections{streamsSection("a"), a1, b0, a2, streamsSection("b")}

		got, err := sections.ForTenant(all, "a")
		require.NoError(t, err)
		require.Equal(t, map[int]*dataobj.Section{0: a1, 2: a2}, got.Logs)
		require.Same(t, all[0], got.Streams)

		got, err = sections.ForTenant(all, "b")
		require.NoError(t, err)
		require.Equal(t, map[int]*dataobj.Section{1: b0}, got.Logs)
		require.Same(t, all[4], got.Streams)
	})

	t.Run("a tenant absent from the object", func(t *testing.T) {
		got, err := sections.ForTenant(dataobj.Sections{streamsSection("a"), logsSection("a")}, "b")
		require.NoError(t, err)
		require.Nil(t, got.Streams)
		require.Empty(t, got.Logs)
	})

	t.Run("a tenant with logs but no streams section", func(t *testing.T) {
		got, err := sections.ForTenant(dataobj.Sections{logsSection("a")}, "a")
		require.NoError(t, err)
		require.Nil(t, got.Streams)
		require.Len(t, got.Logs, 1)
	})

	t.Run("two streams sections for one tenant", func(t *testing.T) {
		all := dataobj.Sections{streamsSection("a"), streamsSection("a")}
		_, err := sections.ForTenant(all, "a")
		require.ErrorContains(t, err, `multiple streams sections for tenant "a"`)
	})

	t.Run("two streams sections for different tenants", func(t *testing.T) {
		all := dataobj.Sections{streamsSection("a"), streamsSection("b")}
		got, err := sections.ForTenant(all, "a")
		require.NoError(t, err)
		require.Same(t, all[0], got.Streams)
	})

	t.Run("an unrecognized section type is ignored", func(t *testing.T) {
		other := &dataobj.Section{Type: dataobj.SectionType{Namespace: "test", Kind: "other"}, Tenant: "a"}
		first := logsSection("a")
		all := dataobj.Sections{other, first}

		got, err := sections.ForTenant(all, "a")
		require.NoError(t, err)
		require.Equal(t, map[int]*dataobj.Section{0: first}, got.Logs)
	})
}
