package tsdb

import (
	"context"
	"io"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	tsdbIndex "github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

func Test_BuildFromHead_HasCorrectIndexVersion(t *testing.T) {
	secondStoreDate := parseDate("2023-01-02")
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				IndexType:  config.TSDBType,
				ObjectType: config.StorageTypeFileSystem,
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				TSDBIndexVersion: tsdbIndex.FormatV2,
			},
			{
				From:       config.DayTime{Time: timeToModelTime(secondStoreDate)},
				IndexType:  config.TSDBType,
				ObjectType: config.StorageTypeFileSystem,
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				TSDBIndexVersion: tsdbIndex.FormatV3,
			},
		},
	}

	ls := mustParseLabels(`{foo="bar"}`)
	fakeShipper := &fakeShipper{}
	dir := t.TempDir()

	tt := []struct {
		tableRange config.TableRange
		version    int
		chunks     []tsdbIndex.ChunkMeta
	}{
		{

			tableRange: schemaCfg.Configs[0].GetIndexTableNumberRange(config.DayTime{Time: timeToModelTime(secondStoreDate.Add(-time.Millisecond))}),
			version:    tsdbIndex.FormatV2,
			chunks: []tsdbIndex.ChunkMeta{
				{
					Checksum: 0,
					MinTime:  1,
					MaxTime:  10,
					KB:       2,
					Entries:  30,
				},
			},
		},
		{

			tableRange: schemaCfg.Configs[1].GetIndexTableNumberRange(config.DayTime{Time: math.MaxInt64}),
			version:    tsdbIndex.FormatV3,
			chunks: []tsdbIndex.ChunkMeta{
				{
					Checksum: 0,
					MinTime:  secondStoreDate.Add(time.Millisecond).UnixMilli(),
					MaxTime:  secondStoreDate.Add(time.Minute).UnixMilli(),
					KB:       2,
					Entries:  30,
				},
			},
		},
	}

	for _, tc := range tt {
		tenantHeads := newTenantHeads(time.Now(), defaultHeadManagerStripeSize, NewMetrics(nil), log.NewNopLogger())
		tsdbManager := NewTSDBManager(
			"foo",
			"uploader",
			filepath.Join(dir, "active"),
			fakeShipper,
			tc.tableRange,
			schemaCfg,
			log.NewNopLogger(),
			NewMetrics(nil),
		)

		_ = tenantHeads.Append("fake", ls, ls.Hash(), tc.chunks)

		err := tsdbManager.BuildFromHead(tenantHeads)
		require.NoError(t, err)

		index := fakeShipper.index

		r, err := index.Reader()
		require.NoError(t, err)

		_, err = r.Seek(4, io.SeekStart)
		require.NoError(t, err)

		version := make([]byte, 1)
		_, err = r.Read(version)
		require.NoError(t, err)

		require.Equal(t, tc.version, int(version[0]))
	}
}

func Test_BuildFromWAL_HasCorrectIndexVersion(t *testing.T) {
	secondStoreDate := parseDate("2023-01-02")
	walTime := secondStoreDate.Add(-time.Minute)
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				IndexType:  config.TSDBType,
				ObjectType: config.StorageTypeFileSystem,
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				TSDBIndexVersion: tsdbIndex.FormatV2,
			},
			{
				From:       config.DayTime{Time: timeToModelTime(secondStoreDate)},
				IndexType:  config.TSDBType,
				ObjectType: config.StorageTypeFileSystem,
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				TSDBIndexVersion: tsdbIndex.FormatV3,
			},
		},
	}

	fakeShipper := &fakeShipper{}
	dir := t.TempDir()

	storeName := "store_2010-10-10"

	for _, d := range managerRequiredDirs(storeName, dir) {
		require.Nil(t, util.EnsureDirectory(d))
	}

	now := time.Now()
	w, err := newHeadWAL(log.NewNopLogger(), walPath(storeName, dir, walTime), now)
	require.Nil(t, err)

	cases := []struct {
		Labels      labels.Labels
		Fingerprint uint64
		Chunks      []tsdbIndex.ChunkMeta
		User        string
	}{
		{
			User:        "tenant1",
			Labels:      mustParseLabels(`{foo="bar", bazz="buzz"}`),
			Fingerprint: mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash(),
			Chunks: []tsdbIndex.ChunkMeta{
				{
					Checksum: 0,
					MinTime:  secondStoreDate.Add(-time.Minute).UnixMilli(),
					MaxTime:  secondStoreDate.Add(-time.Millisecond).UnixMilli(),
					KB:       2,
					Entries:  30,
				},
			},
		},
		{
			User:        "tenant2",
			Labels:      mustParseLabels(`{foo="bard", bazz="bozz", bonk="borb"}`),
			Fingerprint: 1, // Different fingerprint should be preserved
			Chunks: []tsdbIndex.ChunkMeta{
				{
					Checksum: 0,
					MinTime:  secondStoreDate.Add(time.Millisecond).UnixMilli(),
					MaxTime:  secondStoreDate.Add(time.Minute).UnixMilli(),
					KB:       2,
					Entries:  30,
				},
			},
		},
	}

	for i, c := range cases {
		require.Nil(t, w.Log(&WALRecord{
			UserID:      c.User,
			Fingerprint: c.Fingerprint,
			Series: record.RefSeries{
				Ref:    chunks.HeadSeriesRef(i),
				Labels: c.Labels,
			},
			Chks: ChunkMetasRecord{
				Chks: c.Chunks,
				Ref:  uint64(i),
			},
		}))
	}

	tt := []struct {
		tableRange config.TableRange
		version    int
	}{
		{
			tableRange: schemaCfg.Configs[0].GetIndexTableNumberRange(config.DayTime{Time: timeToModelTime(secondStoreDate.Add(-time.Millisecond))}),
			version:    tsdbIndex.FormatV2,
		},
		{

			tableRange: schemaCfg.Configs[1].GetIndexTableNumberRange(config.DayTime{Time: math.MaxInt64}),
			version:    tsdbIndex.FormatV3,
		},
	}

	for _, tc := range tt {
		tsdbManager := NewTSDBManager(
			storeName,
			"uploader",
			dir,
			fakeShipper,
			tc.tableRange,
			schemaCfg,
			log.NewNopLogger(),
			NewMetrics(nil),
		)

		err := tsdbManager.BuildFromWALs(now, []WALIdentifier{{
			ts: walTime,
		}}, false)
		require.NoError(t, err)

		index := fakeShipper.index

		r, err := index.Reader()
		require.NoError(t, err)

		_, err = r.Seek(4, io.SeekStart)
		require.NoError(t, err)

		version := make([]byte, 1)
		_, err = r.Read(version)
		require.NoError(t, err)

		require.Equal(t, tc.version, int(version[0]))
	}
}

type fakeShipper struct {
	index index.Index
}

// AddIndex adds an immutable index to a logical table which would eventually get uploaded to the object store.
func (f *fakeShipper) AddIndex(tableName string, userID string, index index.Index) error {
	f.index = index
	return nil
}

// ForEach lets us iterates through each index file in a table for a specific user.
// On the write path, it would iterate on the files given to the shipper for uploading, until they eventually get dropped from local disk.
// On the read path, it would iterate through the files if already downloaded else it would download and iterate through them.
func (f *fakeShipper) ForEach(ctx context.Context, tableName string, userID string, callback index.ForEachIndexCallback) error {
	panic("not implemented") // TODO: Implement
}

func (f *fakeShipper) ForEachConcurrent(ctx context.Context, tableName string, userID string, callback index.ForEachIndexCallback) error {
	panic("not implemented") // TODO: Implement
}

func (f *fakeShipper) Stop() {
	panic("not implemented") // TODO: Implement
}
