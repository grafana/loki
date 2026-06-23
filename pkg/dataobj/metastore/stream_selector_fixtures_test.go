package metastore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

type labelPosting struct {
	name, value  string
	streamID     int64
	obj          string
	section      int64
	minTs, maxTs int64 // unix nanos
}

type bloomPosting struct {
	columnName string
	values     []string
	streamID   int64
	obj        string
	section    int64
}

// buildLabelBloomSection builds the opened postings section(s) for an object
// holding the given explicit label and bloom postings. The builder emits bloom
// rows and label rows into separate sections, so every postings section is
// returned for the selector to scan. bloomsIn may be nil for label-only fixtures.
//
// When a label posting's maxTs exceeds its minTs, a second observation is
// recorded at maxTs so the posting's aggregated timestamp envelope spans
// [minTs, maxTs] rather than collapsing to a single point.
func buildLabelBloomSection(t *testing.T, labelsIn []labelPosting, bloomsIn []bloomPosting) ([]*postings.Section, func()) {
	t.Helper()
	ctx := context.Background()
	b := postings.NewBuilder(nil, 0, 0, 1<<20)

	for _, lp := range labelsIn {
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: lp.obj, SectionIndex: lp.section, ColumnName: lp.name, LabelValue: lp.value,
			StreamID: lp.streamID, Timestamp: time.Unix(0, lp.minTs).UTC(), UncompressedSize: 0,
		})
		if lp.maxTs > lp.minTs {
			b.ObserveLabelPosting(postings.LabelObservation{
				ObjectPath: lp.obj, SectionIndex: lp.section, ColumnName: lp.name, LabelValue: lp.value,
				StreamID: lp.streamID, Timestamp: time.Unix(0, lp.maxTs).UTC(), UncompressedSize: 0,
			})
		}
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

// openPostingsSections flushes the builders into a single object, each builder
// producing its own physical postings section, and opens every postings section
// the object contains.
func openPostingsSections(t *testing.T, ctx context.Context, builders ...*postings.Builder) ([]*postings.Section, func()) {
	t.Helper()
	objBuilder := dataobj.NewBuilder(nil)
	for _, b := range builders {
		require.NoError(t, objBuilder.Append(b))
	}
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

// buildSplitPostingsSections builds one object with len(perSection) physical
// postings sections, the i-th holding perSection[i]'s label postings. It lets a
// test place labels for the same logical (object, section) into different
// physical postings sections.
func buildSplitPostingsSections(t *testing.T, perSection ...[]labelPosting) ([]*postings.Section, func()) {
	t.Helper()
	builders := make([]*postings.Builder, 0, len(perSection))
	for _, lps := range perSection {
		b := postings.NewBuilder(nil, 0, 0, 1<<20)
		for _, lp := range lps {
			b.ObserveLabelPosting(postings.LabelObservation{
				ObjectPath: lp.obj, SectionIndex: lp.section, ColumnName: lp.name, LabelValue: lp.value,
				StreamID: lp.streamID, Timestamp: time.Unix(0, lp.minTs).UTC(), UncompressedSize: 0,
			})
		}
		builders = append(builders, b)
	}
	return openPostingsSections(t, context.Background(), builders...)
}
