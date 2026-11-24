package tsdb

import (
	"context"
	"fmt"
	"math/rand"
	"os"
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

	"github.com/grafana/loki/v3/pkg/compactor"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// kbEntry is one chunk entry of a multi-tenant index file, belonging to a series of a tenant.
type kbEntry struct {
	tenant string
	series int
	chk    index.ChunkMeta
}

// kbChunk returns a chunk of kb KB identified by id. Chunks with the same id have the same bounds and checksum.
func kbChunk(id int, kb uint32) index.ChunkMeta {
	return index.ChunkMeta{
		MinTime:  int64(id) * 1000,
		MaxTime:  int64(id)*1000 + 999,
		Checksum: uint32(id) + 1,
		Entries:  1,
		KB:       kb,
	}
}

// kbZone returns 10 KB chunks for tenant, one per id, alternating between two series.
func kbZone(tenant string, ids ...int) []kbEntry {
	out := make([]kbEntry, 0, len(ids))
	for i, id := range ids {
		out = append(out, kbEntry{tenant: tenant, series: i % 2, chk: kbChunk(id, 10)})
	}
	return out
}

func concatEntries(parts ...[]kbEntry) []kbEntry {
	var out []kbEntry
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// kbEnv is an object storage with one table, used to run CompactTable over real index files.
type kbEnv struct {
	t            *testing.T
	periodConfig config.PeriodConfig
	indexFormat  int
	tableName    string
	objectsDir   string
	workDir      string
	objectClient client.ObjectClient
	tenants      []string
	ts           int64
}

func newKBEnv(t *testing.T, tenants ...string) *kbEnv {
	t.Helper()
	periodConfig := config.PeriodConfig{
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod}},
		Schema: "v12",
	}
	now := model.Now()
	buckets := IndexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})

	indexFormat, err := periodConfig.TSDBFormat()
	require.NoError(t, err)

	tempDir := t.TempDir()
	e := &kbEnv{
		t:            t,
		periodConfig: periodConfig,
		indexFormat:  indexFormat,
		tableName:    buckets[0].Prefix,
		objectsDir:   filepath.Join(tempDir, objectsStorageDirName),
		workDir:      filepath.Join(tempDir, workingDirName, buckets[0].Prefix),
		tenants:      tenants,
	}
	require.NoError(t, util.EnsureDirectory(e.tablePath()))
	require.NoError(t, util.EnsureDirectory(e.workDir))
	for _, tenant := range tenants {
		require.NoError(t, util.EnsureDirectory(filepath.Join(e.tablePath(), tenant)))
	}

	e.objectClient, err = local.NewFSObjectClient(local.FSConfig{Directory: e.objectsDir})
	require.NoError(t, err)
	return e
}

func (e *kbEnv) tablePath() string {
	return filepath.Join(e.objectsDir, e.tableName)
}

// writeMultiTenantFile writes entries as one multi-tenant index file, as an ingester would upload it,
// and returns the file name.
func (e *kbEnv) writeMultiTenantFile(entries []kbEntry) string {
	e.t.Helper()
	grouped := map[string]map[int]index.ChunkMetas{}
	for _, en := range entries {
		if grouped[en.tenant] == nil {
			grouped[en.tenant] = map[int]index.ChunkMetas{}
		}
		grouped[en.tenant][en.series] = append(grouped[en.tenant][en.series], en.chk)
	}

	userStreams := map[string][]stream{}
	for tenant, bySeries := range grouped {
		for series, chks := range bySeries {
			lbls := labels.FromStrings("foo", "bar", "series", strconv.Itoa(series))
			userStreams[tenant] = append(userStreams[tenant], buildStream(lbls, chks, ""))
		}
	}

	e.ts++
	p := setupMultiTenantIndex(e.t, e.indexFormat, userStreams, e.tablePath(), time.Unix(e.ts, 0))
	return filepath.Base(p)
}

// compact runs CompactTable over the multi-tenant files with the given names, using metrics.
// The compacted result of every tenant is then stored as its per-tenant index files, so the next
// call sees it as the existing compacted index.
func (e *kbEnv) compact(metrics *dedupeTracker, names []string) {
	e.t.Helper()
	files := make([]storage.IndexFile, 0, len(names))
	for _, n := range names {
		files = append(files, storage.IndexFile{Name: n})
	}
	commonIndexSet := &mockIndexSet{
		tableName:    e.tableName,
		workingDir:   e.workDir,
		sourceFiles:  files,
		objectClient: e.objectClient,
	}

	existing := map[string]compactor.IndexSet{}
	for _, tenant := range e.tenants {
		set, err := newMockIndexSet(tenant, e.tableName, filepath.Join(e.workDir, tenant), e.objectClient)
		require.NoError(e.t, err)
		existing[tenant] = set
	}

	tc := newTableCompactor(
		context.Background(),
		commonIndexSet,
		existing,
		func(userID string) (compactor.IndexSet, error) {
			return newMockIndexSet(userID, e.tableName, filepath.Join(e.workDir, userID), e.objectClient)
		},
		e.periodConfig,
		metrics,
	)
	require.NoError(e.t, tc.CompactTable())
	e.persist(tc)
}

// persist replaces the per-tenant index files of every compacted tenant with the compacted result,
// one file per series. The files consumed by the run are removed.
func (e *kbEnv) persist(tc *tableCompactor) {
	e.t.Helper()
	for userID, ci := range tc.compactedIndexes {
		dir := filepath.Join(e.tablePath(), userID)
		entries, err := os.ReadDir(dir)
		require.NoError(e.t, err)
		for _, de := range entries {
			require.NoError(e.t, os.Remove(filepath.Join(dir, de.Name())))
		}

		for s := range ci.(*compactedIndex).builder.allStreams() {
			e.ts++
			setupPerTenantIndex(e.t, e.indexFormat, []stream{{labels: s.labels, fp: s.fp, chunks: s.chunks}}, dir, time.Unix(e.ts, 0))
		}
	}
}

func kbCounters(m *dedupeTracker) (listed, distinct float64) {
	return testutil.ToFloat64(m.listedKB), testutil.ToFloat64(m.distinctKB)
}

func lineCounters(m *dedupeTracker) (listed, distinct float64) {
	return testutil.ToFloat64(m.listedLines), testutil.ToFloat64(m.distinctLines)
}

// TestDedupeTracker_CompactTable runs CompactTable over real index files. Each zone is one multi-tenant
// file holding 100 KB of chunks. Runs list the indexes of the files they ingest.
func TestDedupeTracker_CompactTable(t *testing.T) {
	zoneA := concatEntries(kbZone("t1", 0, 1, 2, 3, 4), kbZone("t2", 0, 1, 2, 3, 4))

	type counters [2]float64 // listed, distinct
	tests := []struct {
		name string
		// files holds the entries of each multi-tenant file.
		files [][]kbEntry
		// runs lists, for each compaction run, the indexes into files it ingests.
		runs [][]int
		// want holds the cumulative listed and distinct KB expected after each run.
		want []counters
	}{
		{
			name:  "identical zones",
			files: [][]kbEntry{zoneA, zoneA},
			runs:  [][]int{{0, 1}},
			want:  []counters{{200, 100}},
		},
		{
			name: "different zones",
			files: [][]kbEntry{
				zoneA,
				concatEntries(kbZone("t1", 10, 11, 12, 13, 14), kbZone("t2", 10, 11, 12, 13, 14)),
			},
			runs: [][]int{{0, 1}},
			want: []counters{{200, 200}},
		},
		{
			name: "half of the chunks identical",
			files: [][]kbEntry{
				zoneA,
				concatEntries(kbZone("t1", 0, 1, 2, 3, 4), kbZone("t2", 10, 11, 12, 13, 14)),
			},
			runs: [][]int{{0, 1}},
			want: []counters{{200, 150}},
		},
		{
			// Both tenants have chunks with the same bounds and checksums in each zone. Deduplication
			// must not merge them across tenants.
			name: "two tenants in the same files",
			files: [][]kbEntry{
				zoneA,
				concatEntries(kbZone("t1", 0, 1, 2, 3, 4), kbZone("t2", 5, 6, 7, 8, 9)),
			},
			runs: [][]int{{0, 1}},
			want: []counters{{200, 150}},
		},
		{
			name:  "second zone arrives in a later run with identical chunks",
			files: [][]kbEntry{zoneA, zoneA},
			runs:  [][]int{{0}, {1}},
			want:  []counters{{100, 100}, {200, 100}},
		},
		{
			name:  "already recorded file appears again alone",
			files: [][]kbEntry{zoneA},
			runs:  [][]int{{0}, {0}},
			want:  []counters{{100, 100}, {100, 100}},
		},
		{
			name: "already recorded file appears with one new file",
			files: [][]kbEntry{
				zoneA,
				concatEntries(kbZone("t1", 10, 11, 12, 13, 14), kbZone("t2", 10, 11, 12, 13, 14)),
			},
			runs: [][]int{{0}, {0, 1}},
			want: []counters{{100, 100}, {200, 200}},
		},
		{
			name:  "tenants with only existing compacted files",
			files: [][]kbEntry{zoneA},
			runs:  [][]int{{0}, {}},
			want:  []counters{{100, 100}, {100, 100}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newKBEnv(t, "t1", "t2")
			names := make([]string, len(tc.files))
			for i, f := range tc.files {
				names[i] = e.writeMultiTenantFile(f)
			}

			m := newDedupeTracker(prometheus.NewRegistry(), 8)
			require.Len(t, tc.runs, len(tc.want))
			for r, run := range tc.runs {
				runNames := make([]string, 0, len(run))
				for _, idx := range run {
					runNames = append(runNames, names[idx])
				}
				e.compact(m, runNames)

				listed, distinct := kbCounters(m)
				require.Equal(t, tc.want[r], counters{listed, distinct}, "after run %d", r)
			}
		})
	}
}

// TestDedupeTracker_MatchesReference compares the counters with a reference computed from the
// generated entries, over random zones, files and runs.
func TestDedupeTracker_MatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))

	for iter := 0; iter < 20; iter++ {
		t.Run(fmt.Sprintf("iteration-%d", iter), func(t *testing.T) {
			numTenants := 2 + rng.Intn(2)
			tenants := make([]string, numTenants)
			for i := range tenants {
				tenants[i] = fmt.Sprintf("tenant-%d", i)
			}
			e := newKBEnv(t, tenants...)

			nextID := 0
			newEntry := func() kbEntry {
				en := kbEntry{
					tenant: tenants[rng.Intn(numTenants)],
					series: rng.Intn(3),
					chk:    kbChunk(nextID, uint32(1+rng.Intn(50))),
				}
				// Lines independent of KB, so a mix-up between the two shows.
				en.chk.Entries = uint32(1 + rng.Intn(200))
				nextID++
				return en
			}

			// Chunks shared by both zones, and chunks unique to one zone.
			var shared []kbEntry
			for i := 3 + rng.Intn(6); i > 0; i-- {
				shared = append(shared, newEntry())
			}
			var zones [2][]kbEntry
			for z := range zones {
				zones[z] = append(zones[z], shared...)
				for i := 2 + rng.Intn(5); i > 0; i-- {
					zones[z] = append(zones[z], newEntry())
				}
			}

			// Split each zone over 1-3 multi-tenant files.
			var files [][]kbEntry
			for _, zone := range zones {
				buckets := make([][]kbEntry, 1+rng.Intn(3))
				for _, en := range zone {
					b := rng.Intn(len(buckets))
					buckets[b] = append(buckets[b], en)
				}
				for _, b := range buckets {
					if len(b) > 0 {
						files = append(files, b)
					}
				}
			}

			names := make([]string, len(files))
			for i, f := range files {
				names[i] = e.writeMultiTenantFile(f)
			}

			// Ingest the files over 1-3 runs.
			numRuns := 1 + rng.Intn(3)
			runNames := make([][]string, numRuns)
			for i := range names {
				r := rng.Intn(numRuns)
				runNames[r] = append(runNames[r], names[i])
			}

			m := newDedupeTracker(prometheus.NewRegistry(), 8)
			for _, rn := range runNames {
				e.compact(m, rn)
			}

			// Reference: listed is every entry; distinct is every unique (tenant, series, bounds, checksum).
			var wantListed, wantDistinct, wantListedLines, wantDistinctLines uint64
			seen := map[string]struct{}{}
			for _, f := range files {
				for _, en := range f {
					wantListed += uint64(en.chk.KB)
					wantListedLines += uint64(en.chk.Entries)
					key := fmt.Sprintf("%s|%d|%d|%d|%d", en.tenant, en.series, en.chk.MinTime, en.chk.MaxTime, en.chk.Checksum)
					if _, ok := seen[key]; !ok {
						seen[key] = struct{}{}
						wantDistinct += uint64(en.chk.KB)
						wantDistinctLines += uint64(en.chk.Entries)
					}
				}
			}

			listed, distinct := kbCounters(m)
			require.Equal(t, float64(wantListed), listed, "listed KB")
			require.Equal(t, float64(wantDistinct), distinct, "distinct KB")
			listedLines, distinctLines := lineCounters(m)
			require.Equal(t, float64(wantListedLines), listedLines, "listed lines")
			require.Equal(t, float64(wantDistinctLines), distinctLines, "distinct lines")
		})
	}
}

// TestDedupeTracker_SeenEviction checks that at most maxTables tables are remembered, and that the
// least recently recorded table is the one forgotten.
func TestDedupeTracker_SeenEviction(t *testing.T) {
	m := newDedupeTracker(prometheus.NewRegistry(), 2)

	m.record("table-a", []string{"a-1"}, chunkTotals{kb: 10, lines: 3}, chunkTotals{kb: 5, lines: 2})
	m.record("table-b", []string{"b-1"}, chunkTotals{kb: 10, lines: 3}, chunkTotals{kb: 5, lines: 2})
	// Recording to table-a again makes table-b the least recently recorded table.
	m.record("table-a", []string{"a-2"}, chunkTotals{kb: 10, lines: 3}, chunkTotals{kb: 5, lines: 2})
	m.record("table-c", []string{"c-1"}, chunkTotals{kb: 10, lines: 3}, chunkTotals{kb: 5, lines: 2})

	require.False(t, m.isSeen("table-b", "b-1"), "table-b should have been evicted")
	require.True(t, m.isSeen("table-a", "a-1"))
	require.True(t, m.isSeen("table-a", "a-2"))
	require.True(t, m.isSeen("table-c", "c-1"))
	require.Len(t, m.seen, 2)

	listed, distinct := kbCounters(m)
	require.Equal(t, float64(40), listed)
	require.Equal(t, float64(20), distinct)
	listedLines, distinctLines := lineCounters(m)
	require.Equal(t, float64(12), listedLines)
	require.Equal(t, float64(8), distinctLines)
}

// TestDedupeTracker_ExtraStoredRatio checks that extra = distinct/(listed/2) - 1 equals the extra bytes
// stored per byte of logs, when two zones hold the same logs cut into chunks in different ways.
func TestDedupeTracker_ExtraStoredRatio(t *testing.T) {
	// cutsFunc returns the chunk boundaries of a series of n blocks: an ascending list from 0 to n.
	// The chunks are the block ranges between consecutive boundaries.
	type cutsFunc func(rng *rand.Rand, n int) []int

	randomCuts := func(rng *rand.Rand, n int) []int {
		bounds := []int{0}
		for i := 1; i < n; i++ {
			if rng.Intn(2) == 0 {
				bounds = append(bounds, i)
			}
		}
		return append(bounds, n)
	}
	singleBlockCuts := func(_ *rand.Rand, n int) []int {
		bounds := make([]int, 0, n+1)
		for i := 0; i <= n; i++ {
			bounds = append(bounds, i)
		}
		return bounds
	}
	wholeSeriesCuts := func(_ *rand.Rand, n int) []int {
		return []int{0, n}
	}

	// refKey identifies a chunk by its tenant, series and block range.
	type refKey struct {
		tenant string
		series int
		i, j   int
	}

	// run builds a random case and returns the measured and the reference extra ratio.
	// Zone A cuts every series with cutsA. Zone B reuses the cuts of zone A for a series with
	// probability reuse, and otherwise cuts the series with cutsB.
	run := func(t *testing.T, rng *rand.Rand, cutsA cutsFunc, reuse float64, cutsB cutsFunc) (got, want float64) {
		t.Helper()
		tenants := []string{"tenant-0", "tenant-1"}
		e := newKBEnv(t, tenants...)

		type seriesCase struct {
			tenant  string
			series  int
			blockKB []uint32
			cuts    [2][]int // per zone
		}
		var cases []seriesCase
		var logsKB uint64
		for _, tenant := range tenants {
			for s := 0; s < 2+rng.Intn(3); s++ {
				n := 10 + rng.Intn(31)
				sc := seriesCase{tenant: tenant, series: s, blockKB: make([]uint32, n)}
				for b := range sc.blockKB {
					sc.blockKB[b] = uint32(1 + rng.Intn(50))
					logsKB += uint64(sc.blockKB[b])
				}
				sc.cuts[0] = cutsA(rng, n)
				if rng.Float64() < reuse {
					sc.cuts[1] = sc.cuts[0]
				} else {
					sc.cuts[1] = cutsB(rng, n)
				}
				cases = append(cases, sc)
			}
		}

		// Each zone holds every chunk of its cuts. Reference: storedKB sums the unique chunks of both zones.
		var zones [2][]kbEntry
		stored := map[refKey]uint64{}
		for _, sc := range cases {
			for z := range zones {
				bounds := sc.cuts[z]
				for k := 0; k+1 < len(bounds); k++ {
					i, j := bounds[k], bounds[k+1]
					var kb uint32
					for _, b := range sc.blockKB[i:j] {
						kb += b
					}
					chk := index.ChunkMeta{
						MinTime:  int64(i),
						MaxTime:  int64(j),
						Checksum: uint32(i)<<16 | uint32(j),
						Entries:  1,
						KB:       kb,
					}
					zones[z] = append(zones[z], kbEntry{tenant: sc.tenant, series: sc.series, chk: chk})
					stored[refKey{sc.tenant, sc.series, i, j}] = uint64(kb)
				}
			}
		}
		var storedKB uint64
		for _, kb := range stored {
			storedKB += kb
		}
		want = float64(storedKB)/float64(logsKB) - 1

		// Spread each zone over 1-3 multi-tenant files.
		var files [][]kbEntry
		for _, zone := range zones {
			buckets := make([][]kbEntry, 1+rng.Intn(3))
			for _, en := range zone {
				b := rng.Intn(len(buckets))
				buckets[b] = append(buckets[b], en)
			}
			for _, b := range buckets {
				if len(b) > 0 {
					files = append(files, b)
				}
			}
		}
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = e.writeMultiTenantFile(f)
		}

		// Ingest the files over 1-3 runs.
		numRuns := 1 + rng.Intn(3)
		runNames := make([][]string, numRuns)
		for i := range names {
			r := rng.Intn(numRuns)
			runNames[r] = append(runNames[r], names[i])
		}
		m := newDedupeTracker(prometheus.NewRegistry(), 8)
		for _, rn := range runNames {
			e.compact(m, rn)
		}

		listed, distinct := kbCounters(m)
		require.Equal(t, float64(2*logsKB), listed, "listed KB")
		require.Equal(t, float64(storedKB), distinct, "distinct KB")
		got = distinct/(listed/2) - 1
		require.InDelta(t, want, got, 1e-12)
		return got, want
	}

	t.Run("identical cuts", func(t *testing.T) {
		rng := rand.New(rand.NewSource(2))
		got, _ := run(t, rng, randomCuts, 1, randomCuts)
		require.InDelta(t, 0, got, 1e-12)
	})

	t.Run("no identical chunks", func(t *testing.T) {
		rng := rand.New(rand.NewSource(2))
		got, _ := run(t, rng, singleBlockCuts, 0, wholeSeriesCuts)
		require.InDelta(t, 1, got, 1e-12)
	})

	t.Run("mixed", func(t *testing.T) {
		rng := rand.New(rand.NewSource(2))
		for iter := 0; iter < 20; iter++ {
			t.Run(fmt.Sprintf("iteration-%d", iter), func(t *testing.T) {
				p := rng.Float64()
				got, _ := run(t, rng, randomCuts, p, randomCuts)
				require.GreaterOrEqual(t, got, 0.0)
				require.LessOrEqual(t, got, 1.0)
			})
		}
	})
}

// TestNewIndexCompactorWithDedupeMetrics_RegistersCounters checks that the dedupe counters are
// registered with the given registerer.
func TestNewIndexCompactorWithDedupeMetrics_RegistersCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewIndexCompactorWithDedupeMetrics(reg)

	families, err := reg.Gather()
	require.NoError(t, err)
	names := make(map[string]bool, len(families))
	for _, f := range families {
		names[f.GetName()] = true
	}
	require.True(t, names["loki_compactor_tsdb_listed_chunk_kb_total"])
	require.True(t, names["loki_compactor_tsdb_distinct_chunk_kb_total"])
	require.True(t, names["loki_compactor_tsdb_listed_chunk_lines_total"])
	require.True(t, names["loki_compactor_tsdb_distinct_chunk_lines_total"])

	// The plain constructor has a tracker too, with counters that are not registered.
	require.NotNil(t, NewIndexCompactor().(indexProcessor).dedupe)
}

// BenchmarkDedupeTracker_RecordRun times recordRun when every ingester file of the compaction
// scenario was re-uploaded, the worst case: each file is read to take its KB out of listed KB.
func BenchmarkDedupeTracker_RecordRun(b *testing.B) {
	s := setupCompactionScenario(b)
	ctx := context.Background()
	logger := log.NewNopLogger()

	files := make([]storage.IndexFile, len(s.multiTenant))
	names := make([]string, len(s.multiTenant))
	for i := range files {
		names[i] = fmt.Sprintf("ingester-%d", i)
		files[i] = storage.IndexFile{Name: names[i]}
	}
	m := newDedupeTracker(nil, 8) // nil registerer: counters are not exported
	m.record("bench-table", names, chunkTotals{}, chunkTotals{})

	b.ReportAllocs()
	for b.Loop() {
		m.recordRun(ctx, logger, "bench-table", files, s.multiTenant, mergeStageSizes{})
	}
}
