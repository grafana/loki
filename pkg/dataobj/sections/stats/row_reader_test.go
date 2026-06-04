package stats_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

// TestRowReader_RoundTrip builds a stats section with two rows and verifies
// RowReader returns them.
func TestRowReader_RoundTrip(t *testing.T) {
	ctx := context.Background()

	b := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(1024*1024, 10000))

	b.Append(stats.Stat{
		ObjectPath:       "/obj1",
		SectionIndex:     0,
		SortSchema:       "service_name,job",
		Labels:           map[string]string{"service_name": "svc1", "job": "job1"},
		MinTimestamp:     100,
		MaxTimestamp:     200,
		RowCount:         5,
		UncompressedSize: 50,
	})
	b.Append(stats.Stat{
		ObjectPath:       "/obj2",
		SectionIndex:     0,
		SortSchema:       "service_name,job",
		Labels:           map[string]string{"service_name": "svc2", "job": "job2"},
		MinTimestamp:     150,
		MaxTimestamp:     250,
		RowCount:         10,
		UncompressedSize: 100,
	})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	var sec *stats.Section
	for _, s := range obj.Sections() {
		if !stats.CheckSection(s) {
			continue
		}
		sec, err = stats.Open(ctx, s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	reader := stats.NewRowReader(ctx, sec)
	defer reader.Close()

	var rows []stats.Stat
	for reader.Next() {
		rows = append(rows, reader.Value())
	}
	require.NoError(t, reader.Err())

	require.Len(t, rows, 2)
}

// TestRowReader_CloseIdempotent verifies Close can be called more than once.
func TestRowReader_CloseIdempotent(t *testing.T) {
	ctx := context.Background()

	b := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(1024*1024, 10000))
	b.Append(stats.Stat{
		ObjectPath:   "/obj1",
		SectionIndex: 0,
		SortSchema:   "service_name",
		Labels:       map[string]string{"service_name": "svc1"},
		MinTimestamp: 100,
		MaxTimestamp: 200,
	})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	var sec *stats.Section
	for _, s := range obj.Sections() {
		if !stats.CheckSection(s) {
			continue
		}
		sec, err = stats.Open(ctx, s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	reader := stats.NewRowReader(ctx, sec)
	require.True(t, reader.Next())
	require.NoError(t, reader.Close())
	require.NoError(t, reader.Close(), "second Close must be a safe no-op")
	require.False(t, reader.Next(), "Next() after Close() must return false, not panic")
}
