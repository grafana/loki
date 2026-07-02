package postings_test

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// TestRowReader_RoundTrip builds a postings section with one label and one
// bloom entry and verifies RowReader returns both in sort order.
func TestRowReader_RoundTrip(t *testing.T) {
	ctx := context.Background()

	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	ts := time.Unix(0, 0).UTC()

	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "prod",
		StreamID:         1,
		Timestamp:        ts,
		UncompressedSize: 100,
	})

	b.PrepareBloomColumn("/obj", 0, "trace_id", 1000)
	require.NoError(t, b.ObserveBloomPosting(postings.BloomObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "trace_id",
		StreamID:         2,
		Timestamp:        ts,
		UncompressedSize: 200,
	}))

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	var rows []postings.Row
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err := postings.Open(ctx, s)
		require.NoError(t, err)

		reader := postings.NewRowReader(ctx, sec, nil)
		for reader.Next() {
			rows = append(rows, reader.At())
		}
		require.NoError(t, reader.Err())
		reader.Close()
	}

	require.Len(t, rows, 2)

	require.Equal(t, postings.KindBloom, rows[0].Kind)
	require.Equal(t, "/obj", rows[0].ObjectPath)
	require.Equal(t, int64(0), rows[0].SectionIndex)
	require.Equal(t, "trace_id", rows[0].ColumnName)
	require.NotNil(t, rows[0].BloomFilter, "bloom row should have non-nil bloom filter")

	require.Equal(t, postings.KindLabel, rows[1].Kind)
	require.Equal(t, "/obj", rows[1].ObjectPath)
	require.Equal(t, int64(0), rows[1].SectionIndex)
	require.Equal(t, "env", rows[1].ColumnName)
	require.Equal(t, "prod", rows[1].LabelValue)
	require.NotEmpty(t, rows[1].StreamIDBitmap, "label row should have non-empty stream id bitmap")
}

// TestRowReader_RoundTrip_BitLevelAssertion verifies stream-id bitmaps survive
// the round trip and differ for distinct stream IDs.
func TestRowReader_RoundTrip_BitLevelAssertion(t *testing.T) {
	ctx := context.Background()

	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	ts := time.Unix(0, 0).UTC()

	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "prod",
		StreamID:         5,
		Timestamp:        ts,
		UncompressedSize: 100,
	})
	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "staging",
		StreamID:         10,
		Timestamp:        ts,
		UncompressedSize: 100,
	})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	var rows []postings.Row
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err := postings.Open(ctx, s)
		require.NoError(t, err)

		reader := postings.NewRowReader(ctx, sec, nil)
		for reader.Next() {
			rows = append(rows, reader.At())
		}
		require.NoError(t, reader.Err())
		reader.Close()
	}

	require.Len(t, rows, 2)
	require.NotEmpty(t, rows[0].StreamIDBitmap, "first row should have non-empty bitmap")
	require.NotEmpty(t, rows[1].StreamIDBitmap, "second row should have non-empty bitmap")
	require.NotEqual(t, rows[0].StreamIDBitmap, rows[1].StreamIDBitmap, "bitmaps for different stream IDs should differ")
}

// TestRowReader_KindPredicate verifies a kind-filtered scan yields only rows of
// the requested kind, in both directions. This also guards that the kind
// predicate reaches the dataset layer (kind-block scan exclusivity). The
// builder emits bloom and label rows into separate sections, so the predicate
// is applied across every postings section in the object.
func TestRowReader_KindPredicate(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildTestSection(t)
	defer closer()

	for _, tc := range []struct {
		name string
		kind postings.PostingKind
	}{{"labels", postings.KindLabel}, {"blooms", postings.KindBloom}} {
		t.Run(tc.name, func(t *testing.T) {
			n := 0
			for _, sec := range secs {
				kindCol := getSectionColumn(t, sec, postings.ColumnTypeKind)
				rr := postings.NewRowReader(ctx, sec, []postings.Predicate{postings.EqualPredicate{
					Column: kindCol,
					Value:  scalar.NewInt64Scalar(int64(tc.kind)),
				}})
				for rr.Next() {
					require.Equal(t, tc.kind, rr.At().Kind)
					n++
				}
				require.NoError(t, rr.Err())
				rr.Close()
			}
			require.Positive(t, n)
		})
	}
}

// TestRowReader_CloseIdempotent verifies Close can be called more than once.
func TestRowReader_CloseIdempotent(t *testing.T) {
	ctx := context.Background()

	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:   "/obj",
		SectionIndex: 0,
		ColumnName:   "env",
		LabelValue:   "prod",
		StreamID:     1,
		Timestamp:    time.Unix(0, 0).UTC(),
	})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	var sec *postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err = postings.Open(ctx, s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	reader := postings.NewRowReader(ctx, sec, nil)
	require.True(t, reader.Next())
	require.NoError(t, reader.Close())
	require.NoError(t, reader.Close(), "second Close must be a safe no-op")
	require.False(t, reader.Next(), "Next() after Close() must return false, not panic")
}

// TestRowReader_BloomMatchPredicate verifies BloomMatchPredicate filters bloom
// rows to only those matching a target value.
func TestRowReader_BloomMatchPredicate(t *testing.T) {
	ctx := context.Background()

	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	ts := time.Unix(0, 0).UTC()

	b.PrepareBloomColumn("/obj", 0, "pod", 1000)
	require.NoError(t, b.ObserveBloomPosting(postings.BloomObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "pod",
		Value:            "foo",
		StreamID:         1,
		Timestamp:        ts,
		UncompressedSize: 100,
	}))

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	var sec *postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err = postings.Open(ctx, s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	bloomCol := getSectionColumn(t, sec, postings.ColumnTypeBloomFilter)
	require.NotNil(t, bloomCol)

	countMatches := func(value string) int {
		rr := postings.NewRowReader(ctx, sec, []postings.Predicate{postings.BloomMatchPredicate{Column: bloomCol, Value: []byte(value)}})
		defer func() { _ = rr.Close() }()
		var got int
		for rr.Next() {
			got++
		}
		require.NoError(t, rr.Err())
		return got
	}

	require.Equal(t, 1, countMatches("foo"))
	require.Equal(t, 0, countMatches("absent-value"))
}

// TestRowReader_RegexMatchPredicate verifies RegexMatchPredicate filters rows
// to only those matching a target regex.
func TestRowReader_RegexMatchPredicate(t *testing.T) {
	ctx := context.Background()

	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	ts := time.Unix(0, 0).UTC()

	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "pod",
		LabelValue:       "foobar",
		StreamID:         1,
		Timestamp:        ts,
		UncompressedSize: 100,
	})

	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "pod",
		LabelValue:       "baz",
		StreamID:         2,
		Timestamp:        ts,
		UncompressedSize: 100,
	})

	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "pod",
		LabelValue:       "foo",
		StreamID:         3,
		Timestamp:        ts,
		UncompressedSize: 100,
	})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	var sec *postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err = postings.Open(ctx, s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	lvCol := getSectionColumn(t, sec, postings.ColumnTypeLabelValue)
	require.NotNil(t, lvCol)

	countMatches := func(pattern string) int {
		re, err := labels.NewFastRegexMatcher(pattern)
		require.NoError(t, err)
		rr := postings.NewRowReader(ctx, sec, []postings.Predicate{postings.RegexMatchPredicate{Column: lvCol, Matcher: re}})
		defer func() { _ = rr.Close() }()
		var got int
		for rr.Next() {
			got++
		}
		require.NoError(t, rr.Err())
		return got
	}

	require.Equal(t, 2, countMatches("foo.*")) // foobar, foo
	require.Equal(t, 0, countMatches("nope.*"))
}
