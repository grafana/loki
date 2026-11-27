package postings_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// rowCount is the number of rows of [openLabelRowsSection]. It spans several
// batches of a RowReader.
const rowCount = 3000

// rowsPerColumnName is the number of consecutive rows of [openLabelRowsSection]
// that have the same column name.
const rowsPerColumnName = rowCount / 3

// columnNames are the column names of [openLabelRowsSection], from the first row
// to the last. They have different lengths.
var columnNames = []string{"a", "env", "service_name"}

func newRowReader(ctx context.Context, t *testing.T, sec *postings.Section, predicates []postings.Predicate) *postings.RowReader {
	t.Helper()
	reader := postings.NewReader(postings.ReaderOptions{
		Columns:    sec.Columns(),
		Predicates: predicates,
	})
	require.NoError(t, reader.Open(ctx))
	return postings.NewRowReader(ctx, reader)
}

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
		Time:             postings.At(ts),
		UncompressedSize: 100,
	})

	b.PrepareBloomColumn("/obj", 0, "trace_id", 1000, 0)
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

		reader := newRowReader(ctx, t, sec, nil)
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
		Time:             postings.At(ts),
		UncompressedSize: 100,
	})
	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "staging",
		StreamID:         10,
		Time:             postings.At(ts),
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

		reader := newRowReader(ctx, t, sec, nil)
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
				rr := newRowReader(ctx, t, sec, []postings.Predicate{postings.EqualPredicate{
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
		Time:         postings.At(time.Unix(0, 0).UTC()),
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

	reader := newRowReader(ctx, t, sec, nil)
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

	b.PrepareBloomColumn("/obj", 0, "pod", 1000, 0)
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
		rr := newRowReader(ctx, t, sec, []postings.Predicate{postings.BloomMatchPredicate{Column: bloomCol, Value: []byte(value)}})
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

func TestRowReader_NotBloomMatchPredicate(t *testing.T) {
	ctx := context.Background()

	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	ts := time.Unix(0, 0).UTC()

	b.PrepareBloomColumn("/obj", 0, "pod", 1000, 0)
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
		rr := newRowReader(ctx, t, sec, []postings.Predicate{
			postings.NotPredicate{Inner: postings.BloomMatchPredicate{Column: bloomCol, Value: []byte(value)}},
		})
		defer func() { _ = rr.Close() }()
		var got int
		for rr.Next() {
			got++
		}
		require.NoError(t, rr.Err())
		return got
	}

	require.Equal(t, 0, countMatches("foo"))
	require.Equal(t, 1, countMatches("absent-value"))
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
		Time:             postings.At(ts),
		UncompressedSize: 100,
	})

	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "pod",
		LabelValue:       "baz",
		StreamID:         2,
		Time:             postings.At(ts),
		UncompressedSize: 100,
	})

	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "pod",
		LabelValue:       "foo",
		StreamID:         3,
		Time:             postings.At(ts),
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
		rr := newRowReader(ctx, t, sec, []postings.Predicate{postings.RegexMatchPredicate{Column: lvCol, Matcher: re}})
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

func TestRowReader_NotRegexMatchPredicate(t *testing.T) {
	ctx := context.Background()

	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	ts := time.Unix(0, 0).UTC()

	for i, lv := range []string{"foobar", "baz", "foo"} {
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:       "/obj",
			SectionIndex:     0,
			ColumnName:       "pod",
			LabelValue:       lv,
			StreamID:         int64(i + 1),
			Time:             postings.At(ts),
			UncompressedSize: 100,
		})
	}

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

	re, err := labels.NewFastRegexMatcher("foo.*")
	require.NoError(t, err)

	rr := newRowReader(ctx, t, sec, []postings.Predicate{
		postings.NotPredicate{Inner: postings.RegexMatchPredicate{Column: lvCol, Matcher: re}},
	})
	defer func() { _ = rr.Close() }()

	var got []string
	for rr.Next() {
		got = append(got, rr.At().LabelValue)
	}
	require.NoError(t, rr.Err())

	require.Equal(t, []string{"baz"}, got)
}

func TestRowReader_Next(t *testing.T) {
	t.Run("returns rows that stay valid after the reader reads later batches", func(t *testing.T) {
		var rows []postings.Row
		rr := newRowReader(t.Context(), t, openLabelRowsSection(t), nil)
		for rr.Next() {
			rows = append(rows, rr.At())
		}
		require.NoError(t, rr.Err())
		require.NoError(t, rr.Close())

		require.Len(t, rows, rowCount)
		for i, row := range rows {
			require.Equal(t, fmt.Sprintf("value-%05d", i), row.LabelValue)
			require.Equal(t, fmt.Sprintf("/obj/%d", i%13), row.ObjectPath)
			require.Equal(t, columnNames[i/rowsPerColumnName], row.ColumnName)
			require.Equal(t, int64(i), row.MinTimestamp)
			require.Equal(t, []int{i}, setBits(row.StreamIDBitmap), "row %d", i)
		}
	})

	t.Run("returns false with the error of the context when the context is canceled after the first batch", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)

		rr := newRowReader(ctx, t, openLabelRowsSection(t), nil)
		require.True(t, rr.Next())
		cancel()

		rows := 1
		for rr.Next() {
			rows++
		}
		require.Less(t, rows, rowCount)
		require.ErrorIs(t, rr.Err(), context.Canceled)
		require.False(t, rr.Next())
		require.NoError(t, rr.Close())
	})
}

func TestRowReader_ReadsRowsAfterBatchesWithNoMatch(t *testing.T) {
	ctx := t.Context()

	b := postings.NewBuilder(nil, 0, 10000, 1<<24)
	ts := time.Unix(0, 0).UTC()

	// Make several batches of values that do not match the predicate.
	for i := range 8192 {
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:       "/obj",
			SectionIndex:     0,
			ColumnName:       "pod",
			LabelValue:       fmt.Sprintf("pod-%d", i),
			StreamID:         int64(i + 1),
			Time:             postings.At(ts),
			UncompressedSize: 100,
		})
	}

	b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "pod",
		LabelValue:       "pod-z", // lexigraphically after 'a' so it is sorted after the previous batch
		StreamID:         int64(8193),
		Time:             postings.At(ts),
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

	rr := newRowReader(ctx, t, sec, []postings.Predicate{
		postings.EqualPredicate{Column: lvCol, Value: scalar.MakeScalar("pod-z")},
	})
	defer func() { _ = rr.Close() }()

	var got []string
	for rr.Next() {
		got = append(got, rr.At().LabelValue)
	}
	require.NoError(t, rr.Err())

	require.Equal(t, []string{"pod-z"}, got)
}

// setBits returns the positions of the bits that are set in a bitmap, where bit n
// is bit n%8 of byte n/8.
func setBits(bitmap []byte) []int {
	var set []int
	for n := range len(bitmap) * 8 {
		if bitmap[n/8]>>(n%8)&1 == 1 {
			set = append(set, n)
		}
	}
	return set
}

// openLabelRowsSection returns a section of rowCount label rows. Each row has
// its own label value, its own stream ID bitmap and one of a few object paths.
// The rows have the column names in columnNames, in blocks of rowsPerColumnName
// rows.
func openLabelRowsSection(t *testing.T) *postings.Section {
	t.Helper()

	b := postings.NewBuilder(nil, 0, 10000, 1<<24)
	for i := range rowCount {
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:   fmt.Sprintf("/obj/%d", i%13),
			SectionIndex: 0,
			ColumnName:   columnNames[i/rowsPerColumnName],
			LabelValue:   fmt.Sprintf("value-%05d", i),
			StreamID:     int64(i),
			Time:         postings.At(time.Unix(0, int64(i)).UTC()),
		})
	}
	secs, closer := openPostingsSections(t.Context(), t, b)
	t.Cleanup(closer)
	require.Len(t, secs, 1)
	return secs[0]
}
