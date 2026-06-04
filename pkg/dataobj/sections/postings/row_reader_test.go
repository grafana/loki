package postings_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// TestRowReader_RoundTrip builds a postings section with one label and one
// bloom entry and verifies RowReader returns both in sort order.
func TestRowReader_RoundTrip(t *testing.T) {
	ctx := context.Background()

	b := postings.NewBuilder(nil, 0, 0)
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

	reader := postings.NewRowReader(ctx, sec)
	defer reader.Close()

	var rows []postings.Row
	for reader.Next() {
		rows = append(rows, reader.Value())
	}
	require.NoError(t, reader.Err())

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

	b := postings.NewBuilder(nil, 0, 0)
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

	reader := postings.NewRowReader(ctx, sec)
	defer reader.Close()

	var rows []postings.Row
	for reader.Next() {
		rows = append(rows, reader.Value())
	}
	require.NoError(t, reader.Err())

	require.Len(t, rows, 2)
	require.NotEmpty(t, rows[0].StreamIDBitmap, "first row should have non-empty bitmap")
	require.NotEmpty(t, rows[1].StreamIDBitmap, "second row should have non-empty bitmap")
	require.NotEqual(t, rows[0].StreamIDBitmap, rows[1].StreamIDBitmap, "bitmaps for different stream IDs should differ")
}

// TestRowReader_CloseIdempotent verifies Close can be called more than once.
func TestRowReader_CloseIdempotent(t *testing.T) {
	ctx := context.Background()

	b := postings.NewBuilder(nil, 0, 0)
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

	reader := postings.NewRowReader(ctx, sec)
	require.True(t, reader.Next())
	require.NoError(t, reader.Close())
	require.NoError(t, reader.Close(), "second Close must be a safe no-op")
	require.False(t, reader.Next(), "Next() after Close() must return false, not panic")
}
