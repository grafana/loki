package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

const (
	baseDay = 20500 // days since epoch, ~2026

	// mirrors the unexported constant in the index package; only used to
	// construct day-aligned test fixtures
	ingestedAtDayMilliseconds = int64(24 * time.Hour / time.Millisecond)
)

func dayMillis(day int64) int64 { return day * ingestedAtDayMilliseconds }

func buildTSDB(t *testing.T, version int, series map[string][]index.ChunkMeta) string {
	t.Helper()

	b := tsdb.NewBuilder(version)
	for name, chks := range series {
		lbls := labels.FromStrings("__name__", "logs", "app", name)
		b.AddSeries(lbls, model.Fingerprint(labels.StableHash(lbls)), chks)
	}

	dir := t.TempDir()
	id, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) tsdb.Identifier {
		return tsdb.NewPrefixedIdentifier(tsdb.SingleTenantTSDBIdentifier{
			TS:       time.Unix(0, 0),
			From:     from,
			Through:  through,
			Checksum: checksum,
		}, dir, "")
	})
	require.NoError(t, err)
	return id.Path()
}

// TestIngestedAtBytesMatchFileSizeDiff cross-checks the tool's byte accounting
// against the real encoder: the same series built at FormatV3 and FormatV4
// must differ in file size by exactly the reported ingestedAt bytes.
//
// Series entries are padded to 16 bytes, so the fixture gives every series
// exactly 16 chunks whose ingestedAt field encodes to 1 byte each (deltas
// small enough for a single uvarint byte). Each series then grows by exactly
// 16 bytes and no padding is absorbed.
func TestIngestedAtBytesMatchFileSizeDiff(t *testing.T) {
	const numSeries = 5
	const chunksPerSeries = 16

	series := map[string][]index.ChunkMeta{}
	for i := range numSeries {
		var chks []index.ChunkMeta
		for j := range chunksPerSeries {
			maxT := dayMillis(baseDay) + int64(j+1)*time.Hour.Milliseconds()
			chk := index.ChunkMeta{
				MinTime:  dayMillis(baseDay) + int64(j)*time.Hour.Milliseconds(),
				MaxTime:  maxT,
				Checksum: uint32(i*chunksPerSeries + j),
				KB:       1024,
				Entries:  128,
			}
			// alternate between the zero sentinel and small day deltas,
			// all of which encode to a single uvarint byte
			if j%2 == 0 {
				chk.IngestedAt = dayMillis(baseDay + int64(j%4) + 1)
			}
			chks = append(chks, chk)
		}
		series[fmt.Sprintf("app-%d", i)] = chks
	}

	pathV3 := buildTSDB(t, index.FormatV3, series)
	pathV4 := buildTSDB(t, index.FormatV4, series)

	statsV3, err := analyzeFile(pathV3)
	require.NoError(t, err)
	statsV4, err := analyzeFile(pathV4)
	require.NoError(t, err)

	require.Equal(t, index.FormatV3, statsV3.Version)
	require.Equal(t, 0, statsV3.IngestedAtBytes)

	require.Equal(t, index.FormatV4, statsV4.Version)
	require.Equal(t, numSeries, statsV4.Series)
	require.Equal(t, numSeries*chunksPerSeries, statsV4.Chunks)
	require.Equal(t, numSeries*chunksPerSeries/2, statsV4.ChunksWithValue)
	require.Equal(t, numSeries*chunksPerSeries, statsV4.IngestedAtBytes)
	require.Equal(t, statsV4.IndexSize-statsV3.IndexSize, int64(statsV4.IngestedAtBytes))
}

// TestIngestedAtVaryingWidths checks the replicated encoding math against the
// package's decoder for deltas spanning several uvarint widths, including
// negative deltas (ingestion day before the chunk's maxt day).
func TestIngestedAtVaryingWidths(t *testing.T) {
	maxT := dayMillis(baseDay) + 12*time.Hour.Milliseconds()

	for _, tc := range []struct {
		deltaDays     int64 // ingestedAt day relative to maxT's day; 0 means sentinel
		sentinel      bool
		expectedBytes int
	}{
		{sentinel: true, expectedBytes: 1},            // encoded 0
		{deltaDays: 0, expectedBytes: 1},              // zigzag 0 -> 1
		{deltaDays: 1, expectedBytes: 1},              // zigzag 2 -> 3
		{deltaDays: -1, expectedBytes: 1},             // zigzag 1 -> 2
		{deltaDays: 63, expectedBytes: 1},             // 127, largest 1-byte value
		{deltaDays: 64, expectedBytes: 2},             // 129
		{deltaDays: -8191, expectedBytes: 2},          // 16382
		{deltaDays: 8192, expectedBytes: 3},           // 16385
		{deltaDays: -(baseDay - 1), expectedBytes: 3}, // earliest expressible day > 0
	} {
		name := fmt.Sprintf("delta_%d_sentinel_%t", tc.deltaDays, tc.sentinel)
		t.Run(name, func(t *testing.T) {
			var ingestedAt int64
			if !tc.sentinel {
				ingestedAt = dayMillis(baseDay + tc.deltaDays)
			}

			chk := index.ChunkMeta{
				MinTime:    dayMillis(baseDay),
				MaxTime:    maxT,
				Checksum:   1,
				KB:         1,
				Entries:    1,
				IngestedAt: ingestedAt,
			}
			path := buildTSDB(t, index.FormatV4, map[string][]index.ChunkMeta{"app": {chk}})

			// verify the value round-trips through the real encoder/decoder,
			// confirming decoded values are day-aligned as the tool assumes
			reader, err := index.NewFileReader(path)
			require.NoError(t, err)
			defer reader.Close()

			k, v := index.AllPostingsKey()
			postings, err := reader.Postings(k, nil, v)
			require.NoError(t, err)
			require.True(t, postings.Next())

			var chks []index.ChunkMeta
			_, err = reader.Series(postings.At(), 0, maxT+1, nil, &chks)
			require.NoError(t, err)
			require.Len(t, chks, 1)
			require.Equal(t, ingestedAt, chks[0].IngestedAt)

			s, err := analyzeFile(path)
			require.NoError(t, err)
			require.Equal(t, 1, s.Chunks)
			require.Equal(t, tc.expectedBytes, s.IngestedAtBytes)
		})
	}
}
