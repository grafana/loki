package stats_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

func TestFromRecordBatch_RoundTrip(t *testing.T) {
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
		SectionIndex:     1,
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

	reader := stats.NewReader(stats.ReaderOptions{Columns: sec.Columns()})
	require.NoError(t, reader.Open(ctx))
	defer reader.Close()

	rec, err := reader.Read(ctx, 8192)
	require.NoError(t, err)
	require.NotNil(t, rec)
	defer rec.Release()

	dest := make([]stats.Stat, rec.NumRows())
	n, err := stats.FromRecordBatch(rec, dest)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Len(t, dest, 2)

	byPath := make(map[string]stats.Stat, len(dest))
	for _, row := range dest {
		byPath[row.ObjectPath] = row
	}

	r1, ok := byPath["/obj1"]
	require.True(t, ok, "expected a row for /obj1")
	require.Equal(t, int64(0), r1.SectionIndex)
	require.Equal(t, "service_name,job", r1.SortSchema)
	require.Equal(t, "svc1", r1.Labels["service_name"])
	require.Equal(t, "job1", r1.Labels["job"])
	require.Equal(t, int64(100), r1.MinTimestamp)
	require.Equal(t, int64(200), r1.MaxTimestamp)
	require.Equal(t, int64(5), r1.RowCount)
	require.Equal(t, int64(50), r1.UncompressedSize)

	r2, ok := byPath["/obj2"]
	require.True(t, ok, "expected a row for /obj2")
	require.Equal(t, int64(1), r2.SectionIndex)
	require.Equal(t, "service_name,job", r2.SortSchema)
	require.Equal(t, "svc2", r2.Labels["service_name"])
	require.Equal(t, "job2", r2.Labels["job"])
	require.Equal(t, int64(150), r2.MinTimestamp)
	require.Equal(t, int64(250), r2.MaxTimestamp)
	require.Equal(t, int64(10), r2.RowCount)
	require.Equal(t, int64(100), r2.UncompressedSize)
}

func TestFromRecordBatch_ParityWithDecodeRow(t *testing.T) {
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
		SectionIndex:     1,
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

	reader := stats.NewReader(stats.ReaderOptions{Columns: sec.Columns()})
	require.NoError(t, reader.Open(ctx))
	defer reader.Close()

	rec, err := reader.Read(ctx, 8192)
	require.NoError(t, err)
	require.NotNil(t, rec)
	defer rec.Release()

	dest := make([]stats.Stat, rec.NumRows())
	n, err := stats.FromRecordBatch(rec, dest)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	colIdx := stats.BuildColumnIndex(rec.Schema())

	for i := 0; i < int(rec.NumRows()); i++ {
		decoded := stats.DecodeRow(rec, colIdx, i)
		require.Equal(t, decoded, dest[i], "FromRecordBatch should match DecodeRow for row %d", i)
	}
}

func TestFromRecordBatch_DestShorterThanBatch(t *testing.T) {
	ctx := context.Background()

	b := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(1024*1024, 10000))

	b.Append(stats.Stat{
		ObjectPath:       "/obj1",
		SectionIndex:     0,
		SortSchema:       "service_name",
		Labels:           map[string]string{"service_name": "svc1"},
		MinTimestamp:     100,
		MaxTimestamp:     200,
		RowCount:         5,
		UncompressedSize: 50,
	})
	b.Append(stats.Stat{
		ObjectPath:       "/obj2",
		SectionIndex:     1,
		SortSchema:       "service_name",
		Labels:           map[string]string{"service_name": "svc2"},
		MinTimestamp:     150,
		MaxTimestamp:     250,
		RowCount:         10,
		UncompressedSize: 100,
	})
	b.Append(stats.Stat{
		ObjectPath:       "/obj3",
		SectionIndex:     2,
		SortSchema:       "service_name",
		Labels:           map[string]string{"service_name": "svc3"},
		MinTimestamp:     160,
		MaxTimestamp:     260,
		RowCount:         15,
		UncompressedSize: 150,
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

	reader := stats.NewReader(stats.ReaderOptions{Columns: sec.Columns()})
	require.NoError(t, reader.Open(ctx))
	defer reader.Close()

	rec, err := reader.Read(ctx, 8192)
	require.NoError(t, err)
	require.NotNil(t, rec)
	defer rec.Release()

	require.Equal(t, int64(3), rec.NumRows())

	dest := make([]stats.Stat, 1)
	n, err := stats.FromRecordBatch(rec, dest)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, "/obj1", dest[0].ObjectPath)
}

func TestFromRecordBatch_ReusedDestNoStaleValues(t *testing.T) {
	ctx := context.Background()

	b := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(1024*1024, 10000))

	b.Append(stats.Stat{
		ObjectPath:       "/obj1",
		SectionIndex:     0,
		SortSchema:       "service_name",
		Labels:           map[string]string{"service_name": "svc1"},
		MinTimestamp:     100,
		MaxTimestamp:     200,
		RowCount:         5,
		UncompressedSize: 50,
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

	reader := stats.NewReader(stats.ReaderOptions{Columns: sec.Columns()})
	require.NoError(t, reader.Open(ctx))
	defer reader.Close()

	rec, err := reader.Read(ctx, 8192)
	require.NoError(t, err)
	require.NotNil(t, rec)
	defer rec.Release()

	dest := []stats.Stat{
		{
			ObjectPath:   "STALE",
			SectionIndex: 999,
			Labels:       map[string]string{"old": "x"},
		},
	}

	n, err := stats.FromRecordBatch(rec, dest)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	require.Equal(t, "/obj1", dest[0].ObjectPath)
	require.Equal(t, int64(0), dest[0].SectionIndex)
	require.Equal(t, int64(100), dest[0].MinTimestamp)
	require.Equal(t, int64(200), dest[0].MaxTimestamp)
	require.Equal(t, int64(5), dest[0].RowCount)
	require.Equal(t, int64(50), dest[0].UncompressedSize)
	require.Equal(t, "service_name", dest[0].SortSchema)
	require.Equal(t, "svc1", dest[0].Labels["service_name"])
	require.NotContains(t, dest[0].Labels, "old")
}
