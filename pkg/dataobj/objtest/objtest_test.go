package objtest

import (
	"fmt"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"

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
