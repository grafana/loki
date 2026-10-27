package compactor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	schedulerapi "github.com/grafana/loki/v3/pkg/logline/scheduler/api"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

const (
	v5TestInterval  = 16 * time.Second
	v5TestShardBits = 5
)

// writeV5Index uploads a v5 index under date whose footer covers layoutDay,
// with "testterm" posted to cells.
func writeV5Index(t *testing.T, s *store.Store, date, hash string, layoutDay time.Time, cells []uint32) store.Meta {
	t.Helper()
	path := filepath.Join(t.TempDir(), hash+".idx")
	w, err := logline.NewWriter("v5", path, nil, &format.WriterConfig{
		DocumentInterval:  v5TestInterval,
		DocumentShardBits: v5TestShardBits,
		DayStart:          layoutDay,
	})
	require.NoError(t, err)
	var term [8]byte
	copy(term[:], "testterm")
	require.NoError(t, w.WriteTermDocIDs(term, cells, len(cells)))
	require.NoError(t, w.Close())

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	day, err := time.Parse(time.DateOnly, date)
	require.NoError(t, err)
	meta := store.Meta{
		Date: date, StorageID: hash, Version: "v5",
		MinLogTs: day, MaxLogTs: day.Add(time.Hour),
		MinRecordTs: day, MaxRecordTs: day.Add(time.Hour),
		DocumentInterval:  v5TestInterval,
		DocumentShardBits: v5TestShardBits,
	}
	require.NoError(t, populateFileInfo(f, &meta))
	require.NoError(t, s.PutIndex(context.Background(), f, meta))
	return meta
}

// TestCompact_V5DocumentShards checks that sharded v5 indexes compact into one
// index of the same layout whose postings are the union of the sources' cells.
func TestCompact_V5DocumentShards(t *testing.T) {
	s := newTestCompactorStore(t, objstore.NewInMemBucket())
	svc := newTestCompactor(t, s)

	day := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	meta1 := writeV5Index(t, s, "2026-03-20", "1111111111111111", day, []uint32{3, 64, 172799})
	meta2 := writeV5Index(t, s, "2026-03-20", "2222222222222222", day, []uint32{3, 65})

	job := &schedulerapi.JobResponse{
		ID:        "testjob1",
		SourceIDs: []string{meta1.ID(), meta2.ID()},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	require.NoError(t, svc.compact(context.Background(), job))

	require.NoError(t, s.Poll(context.Background()))
	active := s.Snapshot().Active()
	require.Len(t, active, 1)
	merged := active[0]
	require.Equal(t, "v5", merged.Version)
	// The merged meta's date and the footer's day both come from the sources'
	// Date, so checkDocumentLayout accepts the result at query time.
	require.Equal(t, "2026-03-20", merged.Date)
	require.Equal(t, v5TestInterval, merged.DocumentInterval)
	require.Equal(t, v5TestShardBits, merged.DocumentShardBits)
	require.NotNil(t, merged.IndexHeader)
	require.NoError(t, logline.ValidateIndexHeader(merged.Version, *merged.IndexHeader))

	rc, err := s.GetIndex(context.Background(), merged.ID())
	require.NoError(t, err)
	mergedPath := filepath.Join(t.TempDir(), "merged.idx")
	mf, err := os.Create(mergedPath)
	require.NoError(t, err)
	_, err = mf.ReadFrom(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	require.NoError(t, mf.Close())

	reader, _, err := logline.OpenFile(mergedPath)
	require.NoError(t, err)
	defer reader.Close()
	layout, ok := logline.DocumentLayout(reader)
	require.True(t, ok)
	require.Equal(t, format.DocumentLayout{Interval: v5TestInterval, ShardBits: v5TestShardBits, DayStart: day}, layout)

	cells, err := queryReader(reader, "testterm")
	require.NoError(t, err)
	require.Equal(t, []uint32{3, 64, 65, 172799}, cells)
}

// TestCompact_V5FooterDayMustMatchDate checks that a v5 source whose footer
// covers another day than its meta date fails the job instead of merging
// cells of different days.
func TestCompact_V5FooterDayMustMatchDate(t *testing.T) {
	s := newTestCompactorStore(t, objstore.NewInMemBucket())
	svc := newTestCompactor(t, s)

	day := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	meta1 := writeV5Index(t, s, "2026-03-20", "1111111111111111", day.AddDate(0, 0, 1), []uint32{3})
	meta2 := writeV5Index(t, s, "2026-03-20", "2222222222222222", day.AddDate(0, 0, 1), []uint32{4})

	job := &schedulerapi.JobResponse{
		ID:        "testjob1",
		SourceIDs: []string{meta1.ID(), meta2.ID()},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	err := svc.compact(context.Background(), job)
	require.ErrorContains(t, err, "configured day start 2026-03-20T00:00:00Z differs from the inputs' 2026-03-21T00:00:00Z")
}
