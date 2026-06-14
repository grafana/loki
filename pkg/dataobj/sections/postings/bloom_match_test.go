package postings_test

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

func TestMatchSections_AND_Semantics(t *testing.T) {
	fx := buildBloomFixture(t, []bloomFixtureEntry{
		{objectPath: "/objA", sectionIndex: 0, columnName: "env", values: []string{"prod"}},
		{objectPath: "/objA", sectionIndex: 0, columnName: "app", values: []string{"foo"}},
		{objectPath: "/objB", sectionIndex: 0, columnName: "env", values: []string{"prod"}},
		{objectPath: "/objB", sectionIndex: 0, columnName: "app", values: []string{"baz"}},
		{objectPath: "/objC", sectionIndex: 0, columnName: "env", values: []string{"dev"}},
		{objectPath: "/objC", sectionIndex: 0, columnName: "app", values: []string{"foo"}},
	})

	batches := readAllBloomBatches(t, fx.sec)

	result, err := postings.MatchSections(t.Context(), batches, []*labels.Matcher{
		equalMatcher(t, "env", "prod"),
		equalMatcher(t, "app", "foo"),
	})
	require.NoError(t, err)

	require.Len(t, result, 1, "exactly one section should match both env=prod AND app=foo")
	_, ok := result[postings.Key{ObjectPath: "/objA", SectionIndex: 0}]
	require.True(t, ok, "section A (/objA, 0) should be the sole match")
}

func TestMatchSections_EqualMatcherOnly_FilterApplied(t *testing.T) {
	fx := buildBloomFixture(t, []bloomFixtureEntry{
		{objectPath: "/obj", sectionIndex: 0, columnName: "env", values: []string{"prod"}},
	})

	batches := readAllBloomBatches(t, fx.sec)

	result, err := postings.MatchSections(t.Context(), batches, []*labels.Matcher{
		equalMatcher(t, "env", "prod"),
		regexMatcher(t, "app", ".*"),
	})
	require.NoError(t, err)

	require.Len(t, result, 1, "Equal matcher alone should match the section; Regex matcher is filtered out")
	_, ok := result[postings.Key{ObjectPath: "/obj", SectionIndex: 0}]
	require.True(t, ok)
}

type bloomFixtureEntry struct {
	objectPath   string
	sectionIndex int64
	columnName   string
	values       []string
}

type bloomFixture struct {
	sec *postings.Section
}

func buildBloomFixture(t *testing.T, entries []bloomFixtureEntry) bloomFixture {
	t.Helper()

	pb := postings.NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 1000).UTC()
	for _, e := range entries {
		pb.PrepareBloomColumn(e.objectPath, e.sectionIndex, e.columnName, uint(len(e.values)))
		for streamIdx, v := range e.values {
			require.NoError(t, pb.ObserveBloomPosting(postings.BloomObservation{
				ObjectPath:       e.objectPath,
				SectionIndex:     e.sectionIndex,
				ColumnName:       e.columnName,
				Value:            v,
				StreamID:         int64(streamIdx + 1),
				Timestamp:        ts,
				UncompressedSize: 1,
			}))
		}
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(pb))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var fx bloomFixture
	for _, sec := range obj.Sections() {
		if !postings.CheckSection(sec) {
			continue
		}
		s, err := postings.Open(t.Context(), sec)
		require.NoError(t, err)
		fx.sec = s
		break
	}
	require.NotNil(t, fx.sec, "postings section missing from fixture")
	return fx
}

func readAllBloomBatches(t *testing.T, sec *postings.Section) []arrow.RecordBatch {
	t.Helper()

	r := postings.NewReader(postings.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { _ = r.Close() })

	var batches []arrow.RecordBatch
	for {
		rb, err := r.ReadBloomRows(t.Context())
		if rb != nil && rb.NumRows() > 0 {
			batches = append(batches, rb)
		}
		if err == nil {
			break
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	return batches
}

func equalMatcher(t *testing.T, name, value string) *labels.Matcher {
	t.Helper()
	m, err := labels.NewMatcher(labels.MatchEqual, name, value)
	require.NoError(t, err)
	return m
}

func regexMatcher(t *testing.T, name, value string) *labels.Matcher {
	t.Helper()
	m, err := labels.NewMatcher(labels.MatchRegexp, name, value)
	require.NoError(t, err)
	return m
}
