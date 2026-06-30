package postings_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// buildTestSection builds the opened postings.Section(s) for an object holding
// one KindLabel row (env=prod, stream 1) and one KindBloom row (trace_id,
// stream 2). The builder emits bloom rows and label rows into separate
// sections, so callers receive every postings section in the object. The
// returned closer must be deferred by the caller.
func buildTestSection(t *testing.T) ([]*postings.Section, func()) {
	t.Helper()
	ctx := context.Background()

	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	ts := time.Unix(0, 0).UTC()
	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath: "/obj", SectionIndex: 0, ColumnName: "env", LabelValue: "prod",
		StreamID: 1, Timestamp: ts, UncompressedSize: 100,
	})
	b.PrepareBloomColumn("/obj", 0, "trace_id", 1000)
	require.NoError(t, b.ObserveBloomPosting(postings.BloomObservation{
		ObjectPath: "/obj", SectionIndex: 0, ColumnName: "trace_id",
		StreamID: 2, Timestamp: ts, UncompressedSize: 200,
	}))

	return openPostingsSections(ctx, t, b)
}

// openPostingsSections flushes b into an object and opens every postings
// section it contains.
func openPostingsSections(ctx context.Context, t *testing.T, b *postings.Builder) ([]*postings.Section, func()) {
	t.Helper()
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)

	var secs []*postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err := postings.Open(ctx, s)
		require.NoError(t, err)
		secs = append(secs, sec)
	}
	require.NotEmpty(t, secs, "no postings section in object")
	return secs, func() { _ = closer.Close() }
}

// getSectionColumn returns the section column of the requested exported type.
func getSectionColumn(t *testing.T, sec *postings.Section, ct postings.ColumnType) *postings.Column {
	t.Helper()
	for _, c := range sec.Columns() {
		if c.Type == ct {
			return c
		}
	}
	t.Fatalf("column type %s not found", ct)
	return nil
}
