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

	return openPostingsSections(t, ctx, b)
}

// openPostingsSections flushes b into an object and opens every postings
// section it contains.
func openPostingsSections(t *testing.T, ctx context.Context, b *postings.Builder) ([]*postings.Section, func()) {
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

type labelPosting struct {
	name, value  string
	streamID     int64
	obj          string
	section      int64
	minTs, maxTs int64 // unix nanos; observation timestamp uses minTs
}

type bloomPosting struct {
	columnName string
	values     []string
	streamID   int64
	obj        string
	section    int64
}

// buildResolveTestSection builds the opened postings section(s) for an object
// holding the given explicit postings. The builder emits bloom rows and label
// rows into separate sections, so every postings section is returned for the
// resolver to scan. bloomsIn may be nil for label-only fixtures.
func buildResolveTestSection(t *testing.T, labelsIn []labelPosting, bloomsIn []bloomPosting) ([]*postings.Section, func()) {
	t.Helper()
	ctx := context.Background()
	b := postings.NewBuilder(nil, 0, 0, 1<<20)

	for _, lp := range labelsIn {
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: lp.obj, SectionIndex: lp.section, ColumnName: lp.name, LabelValue: lp.value,
			StreamID: lp.streamID, Timestamp: time.Unix(0, lp.minTs).UTC(), UncompressedSize: 0,
		})
	}
	for _, bp := range bloomsIn {
		b.PrepareBloomColumn(bp.obj, bp.section, bp.columnName, 1000)
		for _, v := range bp.values {
			require.NoError(t, b.ObserveBloomPosting(postings.BloomObservation{
				ObjectPath: bp.obj, SectionIndex: bp.section, ColumnName: bp.columnName,
				Value: v, StreamID: bp.streamID, Timestamp: time.Unix(0, 0).UTC(),
			}))
		}
	}

	return openPostingsSections(t, ctx, b)
}

// testSectionColumn returns the section column of the requested exported type.
func testSectionColumn(t *testing.T, sec *postings.Section, ct postings.ColumnType) *postings.Column {
	t.Helper()
	for _, c := range sec.Columns() {
		if c.Type == ct {
			return c
		}
	}
	t.Fatalf("column type %s not found", ct)
	return nil
}
