package postings

import (
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestResolvePerMatcherStreams_ReturnsPreIntersectionSets(t *testing.T) {
	// stream 1 has env=prod and app=foo; stream 2 has env=prod only.
	b := NewBuilder(nil, 0, 0, math.MaxInt)
	ts := time.Unix(0, 0).UTC()
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/o", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Timestamp: ts})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/o", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 2, Timestamp: ts})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/o", SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 1, Timestamp: ts})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)
	r := NewReader(ReaderOptions{Columns: sections[0].Columns(), Allocator: memory.DefaultAllocator})
	require.NoError(t, r.Open(t.Context()))

	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
	}
	perMatcher, err := r.ResolvePerMatcherStreams(t.Context(), matchers)
	require.NoError(t, err)
	require.Len(t, perMatcher, 2)
	require.Len(t, perMatcher[0], 2) // env=prod: streams 1,2
	require.Len(t, perMatcher[1], 1) // app=foo: stream 1
}
