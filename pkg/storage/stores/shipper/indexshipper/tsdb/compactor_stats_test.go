package tsdb

import (
	"context"
	"math/rand"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// TestMergeStageSizes checks listed and distinct for two replicas' ingester files with known overlap.
func TestMergeStageSizes(t *testing.T) {
	a := replica(0, 1, 2, 3, 4, 5, 6, 7, 8, 9) // 100 KB
	other := replica(10, 11, 12, 13, 14, 15, 16, 17, 18, 19)
	half := replica(0, 1, 2, 3, 4, 15, 16, 17, 18, 19)

	tests := []struct {
		name string
		runs [][]entries // ingester files merged by each run
		want [][2]uint64 // listed and distinct KB of each run
	}{
		{"identical replicas", [][]entries{{a, a}}, [][2]uint64{{200, 100}}},
		{"no identical chunks", [][]entries{{a, other}}, [][2]uint64{{200, 200}}},
		{"half identical", [][]entries{{a, half}}, [][2]uint64{{200, 150}}},
		{"second replica in a later run", [][]entries{{a}, {a}}, [][2]uint64{{100, 100}, {100, 0}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var existing entries // the compacted index, carried from run to run
			for r, run := range tc.runs {
				var sizes mergeStageSizes
				sizes, existing = merge(existing, run...)
				listed, distinct := tc.want[r][0], tc.want[r][1]
				require.Equal(t, chunkTotals{kb: listed, lines: listed * 3 / 10}, sizes.listed, "run %d", r)
				require.Equal(t, chunkTotals{kb: distinct, lines: distinct * 3 / 10}, sizes.distinct(), "run %d", r)
			}
		})
	}
}

// TestMergeStageSizes_Random checks that, summed over runs, listed is both replicas' logs and
// distinct is the unique chunks, for random cuts, files and runs.
func TestMergeStageSizes_Random(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 50; iter++ {
		t.Run(strconv.Itoa(iter), func(t *testing.T) {
			replicas, logs, unique := randomReplicas(rng)
			var listed, distinct chunkTotals
			var existing entries
			for _, run := range randomRuns(rng, replicas) {
				var sizes mergeStageSizes
				sizes, existing = merge(existing, run...)
				listed, distinct = listed.plus(sizes.listed), distinct.plus(sizes.distinct())
			}
			require.Equal(t, chunkTotals{kb: 2 * logs.kb, lines: 2 * logs.lines}, listed)
			require.Equal(t, unique, distinct)
		})
	}
}

// TestDedupeTracker_RecordRunSkipsReuploads checks that a file counted before is taken out of listed.
func TestDedupeTracker_RecordRunSkipsReuploads(t *testing.T) {
	// An ingester file with two 10 KB chunks of 3 lines each, already counted by an earlier run.
	path := setupMultiTenantIndex(t, index.FormatV3, map[string][]stream{
		"tenant": {buildStream(labels.FromStrings("series", "0"), index.ChunkMetas{testChunk(0, 10), testChunk(1, 10)}, "")},
	}, t.TempDir(), time.Unix(1, 0))
	idx, err := OpenShippableTSDB(path, index.MmapOptions{})
	require.NoError(t, err)
	defer idx.Close()
	reuploaded := storage.IndexFile{Name: filepath.Base(path)}

	m := newDedupeTracker(prometheus.NewRegistry(), dedupeTrackedTables)
	m.record("table", []string{reuploaded.Name}, chunkTotals{}, chunkTotals{})

	// The re-uploaded file's 20 KB and 6 lines come out of listed.
	sizes := mergeStageSizes{listed: chunkTotals{kb: 50, lines: 15}, afterDedupe: chunkTotals{kb: 5, lines: 1}}
	files := []storage.IndexFile{reuploaded, {Name: "new"}}
	m.recordRun(context.Background(), log.NewNopLogger(), "table", files, []Index{idx.(Index), nil}, sizes)
	require.Equal(t, [4]float64{30, 5, 9, 1}, counters(m))

	// A run with only the re-uploaded file counts nothing.
	m.recordRun(context.Background(), log.NewNopLogger(), "table", files[:1], []Index{idx.(Index)}, sizes)
	require.Equal(t, [4]float64{30, 5, 9, 1}, counters(m))
}

// TestDedupeTracker_ForgetsOldestTable checks that the maxTables newest tables by date are kept,
// even when a run records the newest table first.
func TestDedupeTracker_ForgetsOldestTable(t *testing.T) {
	m := newDedupeTracker(prometheus.NewRegistry(), 2)
	for _, table := range []string{"index_20003", "index_20002", "index_20000", "index_20001"} {
		m.record(table, []string{table + "-file"}, chunkTotals{}, chunkTotals{})
	}

	require.True(t, m.isSeen("index_20003", "index_20003-file"))
	require.True(t, m.isSeen("index_20002", "index_20002-file"))
	require.False(t, m.isSeen("index_20001", "index_20001-file"))
	require.False(t, m.isSeen("index_20000", "index_20000-file"))
}

// TestNewIndexCompactorWithDedupeMetrics checks that the four counters are registered.
func TestNewIndexCompactorWithDedupeMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewIndexCompactorWithDedupeMetrics(reg)
	n, err := testutil.GatherAndCount(reg)
	require.NoError(t, err)
	require.Equal(t, 4, n)
}

// entries holds chunks per series id.
type entries map[int]index.ChunkMetas

// testChunk returns a chunk of kb KB and 3 lines. Chunks with the same id are identical.
func testChunk(id int, kb uint32) index.ChunkMeta {
	return index.ChunkMeta{MinTime: int64(id) * 1000, MaxTime: int64(id)*1000 + 999, Checksum: uint32(id) + 1, Entries: 3, KB: kb}
}

// replica returns one 10 KB chunk per id, over two series.
func replica(ids ...int) entries {
	e := entries{}
	for i, id := range ids {
		e[i%2] = append(e[i%2], testChunk(id, 10))
	}
	return e
}

// merge takes the same readings as setupBuilder on a real Builder: listed entries, then the
// existing index, then dedupe. It also returns the merged chunks, the existing index of a next run.
func merge(existing entries, listed ...entries) (mergeStageSizes, entries) {
	b := NewBuilder(index.FormatV3)
	add := func(e entries) {
		for s, chks := range e {
			b.AddSeries(labels.FromStrings("series", strconv.Itoa(s)), model.Fingerprint(s), chks)
		}
	}
	var sizes mergeStageSizes
	for _, l := range listed {
		add(l)
	}
	sizes.listed = b.totalChunks()
	add(existing)
	sizes.existing = b.totalChunks().minus(sizes.listed)
	b.FinalizeChunks()
	sizes.afterDedupe = b.totalChunks()

	merged := entries{}
	for s := range b.allStreams() {
		merged[int(s.fp)] = s.chunks
	}
	return sizes, merged
}

// randomReplicas cuts random logs of 5 series into chunks for two replicas, about half the
// series identically. It returns both replicas, the logs' totals and the totals of the unique chunks.
func randomReplicas(rng *rand.Rand) (replicas [2]entries, logs, unique chunkTotals) {
	cuts := func(n int) []int { // random chunk boundaries from 0 to n
		b := []int{0}
		for i := 1; i < n; i++ {
			if rng.Intn(2) == 0 {
				b = append(b, i)
			}
		}
		return append(b, n)
	}
	replicas = [2]entries{{}, {}}
	for series := 0; series < 5; series++ {
		// The series' logs, as blocks of random size. A chunk covers a range of blocks.
		blockKB := make([]uint32, 10+rng.Intn(30)) // one line per block
		for b := range blockKB {
			blockKB[b] = uint32(1 + rng.Intn(50))
			logs.kb += uint64(blockKB[b])
		}
		logs.lines += uint64(len(blockKB))

		// Each replica cuts the logs into chunks; about half the series are cut identically.
		replicaCuts := [2][]int{cuts(len(blockKB)), cuts(len(blockKB))}
		if rng.Intn(2) == 0 {
			replicaCuts[1] = replicaCuts[0]
		}
		seen := map[[2]int]bool{} // block ranges already counted as unique
		for r, bounds := range replicaCuts {
			for k := 0; k+1 < len(bounds); k++ {
				i, j := bounds[k], bounds[k+1]
				chk := index.ChunkMeta{MinTime: int64(i), MaxTime: int64(j), Checksum: uint32(i<<16 | j), Entries: uint32(j - i)}
				for _, kb := range blockKB[i:j] {
					chk.KB += kb
				}
				replicas[r][series] = append(replicas[r][series], chk)
				if !seen[[2]int{i, j}] {
					seen[[2]int{i, j}] = true
					unique.kb += uint64(chk.KB)
					unique.lines += uint64(chk.Entries)
				}
			}
		}
	}
	return replicas, logs, unique
}

// randomRuns splits each replica into 1-3 ingester files and spreads the files over 1-3 runs.
func randomRuns(rng *rand.Rand, replicas [2]entries) [][]entries {
	runs := make([][]entries, 1+rng.Intn(3))
	for _, z := range replicas {
		files := make([]entries, 1+rng.Intn(3))
		for f := range files {
			files[f] = entries{}
		}
		for s := 0; s < len(z); s++ {
			for _, chk := range z[s] {
				f := rng.Intn(len(files))
				files[f][s] = append(files[f][s], chk)
			}
		}
		for _, f := range files {
			r := rng.Intn(len(runs))
			runs[r] = append(runs[r], f)
		}
	}
	return runs
}

// counters returns listed KB, distinct KB, listed lines and distinct lines.
func counters(m *dedupeTracker) [4]float64 {
	return [4]float64{
		testutil.ToFloat64(m.listedKB), testutil.ToFloat64(m.distinctKB),
		testutil.ToFloat64(m.listedLines), testutil.ToFloat64(m.distinctLines),
	}
}
