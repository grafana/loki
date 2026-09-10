package builder

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/shard"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type docRange struct{ min, max int64 }

// shardOf returns the shard for a term under the given shard count. Test-only:
// production code shards during the spill reorder (shardSortAndTrack).
func shardOf(fn shard.Func, term [8]byte, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	return fn(term, shardCount)
}

// roundTripConfig builds a minimal config at the given shard
// count, with a tiny postings buffer (bufferPairs pairs, 0.75 spill watermark)
// so a handful of entries force many spills + cross-run dedup + the
// incremental-fill merge. The tiny sizing is applied AFTER Validate on
// purpose: Validate floors postings_buffer_pairs at 1<<16, which would take
// megabytes of input to exercise the same paths.
func roundTripConfig(t *testing.T, shardCount, bufferPairs int) Config {
	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Logline: LoglineConfig{
			DocumentInterval: 100 * time.Millisecond,
			IndexVersion:     "v3",
			DensityThreshold: -1, // disable the MatchesAll sentinel so every term lists docs
			ShardCount:       shardCount,
			NgramLength:      6,
		},
		FlushOnIdle:   time.Hour,
		FlushOnMaxAge: time.Hour,
		ScratchDir:    t.TempDir(),
	}
	if shardCount > 1 {
		cfg.Logline.ShardAlgorithm = "murmur3_mix"
	}
	require.NoError(t, cfg.Validate())
	cfg.PostingsBufferPairs = bufferPairs
	cfg.PostingsSpillWatermark = 0.75
	return cfg
}

// referenceRanges computes, directly from the entries, the expected
// term(hex) → sorted set of document time-ranges for each (date, shard) file.
func referenceRanges(entries []logproto.Entry, extractFn logline.ExtractFunc, shardFn shard.Func, shardCount int) map[string]map[string]map[docRange]bool {
	interval := (100 * time.Millisecond).Nanoseconds()
	ticksPerDay := uint64((24 * time.Hour) / (100 * time.Millisecond))
	out := map[string]map[string]map[docRange]bool{}
	var scratch [][8]byte
	for i := range entries {
		ts := entries[i].Timestamp.UTC()
		absBucket := uint64(ts.UnixNano()) / uint64(interval)
		startNanos := int64(absBucket) * interval
		r := docRange{time.Unix(0, startNanos).UnixMilli(), time.Unix(0, startNanos+interval).UnixMilli()}
		day := absBucket / ticksPerDay
		date := time.Unix(0, int64(day*ticksPerDay)*interval).UTC().Format("2006-01-02")
		scratch = extractFn(6, entries[i].Line, entries[i].StructuredMetadata, nil, scratch[:0])
		for _, g := range scratch {
			sh := shardOf(shardFn, g, shardCount)
			key := fmt.Sprintf("%s_s%d", date, sh)
			if out[key] == nil {
				out[key] = map[string]map[docRange]bool{}
			}
			termHex := fmt.Sprintf("%x", g)
			if out[key][termHex] == nil {
				out[key][termHex] = map[docRange]bool{}
			}
			out[key][termHex][r] = true
		}
	}
	return out
}

// TestBuilder_RoundTrip verifies the builder's produced .lidx files reproduce,
// per (date, shard) and per term, the exact set of document time-ranges
// computed directly from the input — across a tiny buffer (many spills +
// cross-run dedup), both unsharded and sharded, spanning two dates.
func TestBuilder_RoundTrip(t *testing.T) {
	// Entries straddling a UTC midnight, with repeated lines so the same
	// (ngram, tick) recurs within and across spills.
	base := time.Date(2026, 3, 22, 23, 59, 30, 0, time.UTC)
	lines := []string{
		"error connecting to database server timeout",
		"user alice logged in from console device",
		"error connecting to database server timeout",
		"payment processed for order 12345 succeeded",
		"warning disk usage high on node seventeen",
	}
	var entries []logproto.Entry
	for i := range 60 {
		entries = append(entries, logproto.Entry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Line:      lines[i%len(lines)],
		})
	}

	extractFn, err := logline.ExtractorForVersion("v3")
	require.NoError(t, err)

	// 256 is the builder's shard ceiling (shard values travel as uint8); the
	// boundary must round-trip, not just validate.
	for _, shardCount := range []int{1, 4, 256} {
		t.Run(fmt.Sprintf("shard%d", shardCount), func(t *testing.T) {
			shardFn := shard.Noop
			if shardCount > 1 {
				shardFn, err = shard.New("murmur3_mix")
				require.NoError(t, err)
			}
			want := referenceRanges(entries, extractFn, shardFn, shardCount)

			cfg := roundTripConfig(t, shardCount, 32)
			reg := prometheus.NewRegistry()
			b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(reg))
			require.NoError(t, err)
			require.NoError(t, b.processStream(&logproto.Stream{Entries: entries}, nil, time.Now(), recordRef{}))

			files, err := b.prepareIndexes()
			require.NoError(t, err)
			defer b.clear()

			got := map[string]map[string]map[docRange]bool{}
			for _, f := range files {
				key := fmt.Sprintf("%s_s%d", f.date, f.shardValue)
				got[key] = readTermRanges(t, f.file.Name())
			}

			require.Equal(t, sortedKeys(want), sortedKeys(got), "(date,shard) file set mismatch")
			for key, wantTerms := range want {
				gotTerms := got[key]
				require.Equal(t, len(wantTerms), len(gotTerms), "term count for %s", key)
				for term, wantSet := range wantTerms {
					gotSet := gotTerms[term]
					require.Equal(t, len(wantSet), len(gotSet), "range count for term %s in %s", term, key)
					for r := range wantSet {
						require.True(t, gotSet[r], "missing range %v for term %s in %s", r, term, key)
					}
				}
			}

			// The per-file cardinality metrics must agree with the reference:
			// terms per file = distinct terms; docs per file = distinct doc
			// ranges across its terms. A regression stamping zeros would pass
			// the range checks above and only surface on dashboards.
			wantTermsTotal, wantDocsTotal := 0, 0
			for _, wantTerms := range want {
				wantTermsTotal += len(wantTerms)
				union := map[docRange]bool{}
				for _, set := range wantTerms {
					for r := range set {
						union[r] = true
					}
				}
				wantDocsTotal += len(union)
			}
			termsCount, termsSum := histogramTotals(t, reg, "logline_index_builder_index_file_terms_count")
			require.Equal(t, uint64(len(files)), termsCount)
			require.Equal(t, float64(wantTermsTotal), termsSum, "terms histogram must sum to the reference term count")
			docsCount, docsSum := histogramTotals(t, reg, "logline_index_builder_index_file_documents_count")
			require.Equal(t, uint64(len(files)), docsCount)
			require.Equal(t, float64(wantDocsTotal), docsSum, "documents histogram must sum to the reference document count")
		})
	}
}

// histogramTotals returns the sample count and sum of a registered histogram.
func histogramTotals(t *testing.T, reg *prometheus.Registry, name string) (uint64, float64) {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == name {
			h := mf.GetMetric()[0].GetHistogram()
			return h.GetSampleCount(), h.GetSampleSum()
		}
	}
	t.Fatalf("metric %s not registered", name)
	return 0, 0
}

// readTermRanges reads a .lidx into term(hex) → set of document time-ranges.
func readTermRanges(t *testing.T, path string) map[string]map[docRange]bool {
	t.Helper()
	reader, _, err := logline.OpenFile(path)
	require.NoError(t, err)
	defer reader.Close()

	idToRange := map[uint32]docRange{}
	for _, d := range reader.Documents() {
		idToRange[d.ID] = docRange{d.MinTimeUnix, d.MaxTimeUnix}
	}

	out := map[string]map[docRange]bool{}
	it, err := reader.NewTermIterator()
	require.NoError(t, err)
	for it.Next() {
		require.False(t, it.Bitmap().MatchesAll, "density must be disabled in this test")
		set := map[docRange]bool{}
		for _, id := range it.Bitmap().Roaring.ToArray() {
			r, ok := idToRange[id]
			require.True(t, ok, "docID %d missing metadata in %s", id, path)
			set[r] = true
		}
		out[fmt.Sprintf("%x", it.Term())] = set
	}
	require.NoError(t, it.Err())
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// TestBuilder_PrepareIndexesRetryable verifies that a second prepareIndexes
// (as happens when an upload fails and the flush is retried) reproduces the
// same files from the surviving scratch runs (invariant: runs are removed only
// by clear, never by prepareIndexes).
func TestBuilder_PrepareIndexesRetryable(t *testing.T) {
	cfg := roundTripConfig(t, 4, 64)
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)

	now := time.Now().UTC()
	var entries []logproto.Entry
	for i := range 40 {
		entries = append(entries, logproto.Entry{
			Timestamp: now.Add(time.Duration(i) * time.Second),
			Line:      fmt.Sprintf("request %d failed with status 500 at endpoint alpha", i%7),
		})
	}
	require.NoError(t, b.processStream(&logproto.Stream{Entries: entries}, nil, now, recordRef{}))

	first, err := b.prepareIndexes()
	require.NoError(t, err)
	require.NotEmpty(t, first)
	runsAfterFirst := append([]string(nil), b.ing.postings.runPaths...)
	require.NotEmpty(t, runsAfterFirst, "runs must survive prepareIndexes for retry")

	// Retry: same runs, same (date, shard) set produced.
	second, err := b.prepareIndexes()
	require.NoError(t, err)
	require.Equal(t, fileKeys(first), fileKeys(second), "retry must reproduce the same (date,shard) files")

	b.clear()
	require.Empty(t, b.ing.postings.runPaths, "clear must drop runs")
}

func fileKeys(files []fileInfo) []string {
	ks := make([]string, 0, len(files))
	for _, f := range files {
		ks = append(ks, fmt.Sprintf("%s_s%d", f.date, f.shardValue))
	}
	sort.Strings(ks)
	return ks
}

// retryTestEntries is a fixed dataset shared by the failure-retry and clear
// tests: enough repeated lines to force multiple run spills under a tiny
// buffer, with deterministic timestamps.
func retryTestEntries() []logproto.Entry {
	base := time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)
	var entries []logproto.Entry
	for i := range 50 {
		entries = append(entries, logproto.Entry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Line:      fmt.Sprintf("request %d failed with status 500 at endpoint alpha", i%7),
		})
	}
	return entries
}

// termRangesByFile reads a prepared file set into key → term → ranges for
// semantic comparison.
func termRangesByFile(t *testing.T, files []fileInfo) map[string]map[string]map[docRange]bool {
	t.Helper()
	out := map[string]map[string]map[docRange]bool{}
	for _, f := range files {
		out[fmt.Sprintf("%s_s%d", f.date, f.shardValue)] = readTermRanges(t, f.file.Name())
	}
	return out
}

// TestBuilder_PrepareIndexesRetryAfterFailure is the new-architecture analogue
// of the old index_bucket_failure_test.go: a mid-flush failure must leave the
// builder retryable. It corrupts a run file's magic so the merge fails, then
// asserts the error surfaced, the runs survived, no .lidx fds or paths leaked
// (discardIndexes ran), and that after healing the corruption a retried
// prepareIndexes produces output semantically identical to a builder that
// never failed.
func TestBuilder_PrepareIndexesRetryAfterFailure(t *testing.T) {
	entries := retryTestEntries()

	// Control: identical input, never failed.
	ctl, err := newIndexBuilder(roundTripConfig(t, 4, 64), "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	defer ctl.clear()
	require.NoError(t, ctl.processStream(&logproto.Stream{Entries: entries}, nil, time.Now(), recordRef{}))
	ctlFiles, err := ctl.prepareIndexes()
	require.NoError(t, err)
	require.NotEmpty(t, ctlFiles)
	want := termRangesByFile(t, ctlFiles)

	b, err := newIndexBuilder(roundTripConfig(t, 4, 64), "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	defer b.clear()
	require.NoError(t, b.processStream(&logproto.Stream{Entries: entries}, nil, time.Now(), recordRef{}))
	require.NotEmpty(t, b.ing.postings.runPaths, "test requires spilled runs before prepareIndexes")

	// Corrupt one run's magic so the merge fails after finish().
	victim := b.ing.postings.runPaths[0]
	fh, err := os.OpenFile(victim, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = fh.WriteAt([]byte("XRUN"), 0)
	require.NoError(t, err)
	require.NoError(t, fh.Close())

	_, err = b.prepareIndexes()
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad run magic")

	// discardIndexes ran: no leaked .lidx fds/paths, no stray .lidx on disk.
	require.Empty(t, b.openFiles)
	require.Empty(t, b.lidxPaths)
	strays, err := filepath.Glob(filepath.Join(b.runDir, "*.lidx"))
	require.NoError(t, err)
	require.Empty(t, strays, "failed prepare must not leave .lidx files behind")

	// Runs survive the failure (invariant #6) so a retry can rebuild.
	for _, p := range b.ing.postings.runPaths {
		_, err := os.Stat(p)
		require.NoError(t, err, "run %s must survive a failed prepareIndexes", p)
	}

	// Heal the corruption and retry: output must match the never-failed builder.
	fh, err = os.OpenFile(victim, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = fh.WriteAt([]byte(runMagic), 0)
	require.NoError(t, err)
	require.NoError(t, fh.Close())

	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.Equal(t, want, termRangesByFile(t, files), "retried output must match a never-failed builder")
}

// corruptShardPayload overwrites the first bytes of the given shard's s2
// stream in the first run that holds records for it, located via the run's
// shard directory. Returns the victim path, the corrupted offset, and the
// original bytes so the caller can heal the corruption. The overwrite (0xFF
// chunk type with an impossible length) fails the very first s2 read of that
// shard, while every other shard's stream stays intact.
func corruptShardPayload(t *testing.T, runPaths []string, shardVal int) (path string, off int64, orig []byte) {
	t.Helper()
	for _, p := range runPaths {
		fh, err := os.OpenFile(p, os.O_RDWR, 0)
		require.NoError(t, err)
		hdr := make([]byte, runHeaderSize)
		_, err = fh.ReadAt(hdr, 0)
		require.NoError(t, err)
		if shardVal >= int(binary.LittleEndian.Uint16(hdr[6:])) {
			require.NoError(t, fh.Close())
			continue
		}
		de := make([]byte, runDirEntrySize)
		_, err = fh.ReadAt(de, int64(runHeaderSize+shardVal*runDirEntrySize))
		require.NoError(t, err)
		streamOff := int64(binary.LittleEndian.Uint64(de[:8]))
		if binary.LittleEndian.Uint64(de[8:]) == 0 {
			require.NoError(t, fh.Close())
			continue
		}
		orig = make([]byte, 16)
		_, err = fh.ReadAt(orig, streamOff)
		require.NoError(t, err)
		_, err = fh.WriteAt(bytes.Repeat([]byte{0xFF}, len(orig)), streamOff)
		require.NoError(t, err)
		require.NoError(t, fh.Close())
		return p, streamOff, orig
	}
	t.Fatalf("no run contains records for shard %d", shardVal)
	return "", 0, nil
}

// TestBuilder_PrepareIndexesRetryAfterMidMergeFailure strengthens the
// failure-retry coverage above: corrupting a run's magic fails at open, before
// any shard writes a .lidx, so that test's no-stray-.lidx assertion is
// vacuously true. Here shard 0 merges to completion (writing its .lidx files)
// before shard 1 hits a corrupted s2 payload — asserting that
// mergeShard/mergeRuns remove the files of already-completed shards on
// failure, the runs survive, and a healed retry matches a never-failed
// control.
func TestBuilder_PrepareIndexesRetryAfterMidMergeFailure(t *testing.T) {
	entries := retryTestEntries()

	// Control: identical input, never failed.
	ctl, err := newIndexBuilder(roundTripConfig(t, 4, 64), "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	defer ctl.clear()
	require.NoError(t, ctl.processStream(&logproto.Stream{Entries: entries}, nil, time.Now(), recordRef{}))
	ctlFiles, err := ctl.prepareIndexes()
	require.NoError(t, err)
	// Shard 0 must produce files, or "completed shards leave strays" is
	// untestable with this dataset.
	require.Contains(t, fileKeys(ctlFiles), "2026-03-22_s0")
	want := termRangesByFile(t, ctlFiles)

	b, err := newIndexBuilder(roundTripConfig(t, 4, 64), "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	defer b.clear()
	require.NoError(t, b.processStream(&logproto.Stream{Entries: entries}, nil, time.Now(), recordRef{}))

	// Spill the remaining head now so the run set is final before corrupting
	// (finish is idempotent — prepareIndexes below won't create new runs).
	require.NoError(t, b.ing.postings.finish())
	require.NotEmpty(t, b.ing.postings.runPaths)

	victim, off, orig := corruptShardPayload(t, b.ing.postings.runPaths, 1)

	_, err = b.prepareIndexes()
	require.Error(t, err)

	// Shard 0 completed before the failure, but its .lidx were never returned
	// to the builder — only the merge itself can clean them, and must.
	strays, err := filepath.Glob(filepath.Join(b.runDir, "*.lidx"))
	require.NoError(t, err)
	require.Empty(t, strays, "completed shards must not leave .lidx files after a failed merge")
	require.Empty(t, b.openFiles)
	require.Empty(t, b.lidxPaths)

	// Runs survive the failure (invariant #6) so a retry can rebuild.
	for _, p := range b.ing.postings.runPaths {
		_, err := os.Stat(p)
		require.NoError(t, err, "run %s must survive a failed prepareIndexes", p)
	}

	// Heal the corruption and retry: output must match the never-failed builder.
	fh, err := os.OpenFile(victim, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = fh.WriteAt(orig, off)
	require.NoError(t, err)
	require.NoError(t, fh.Close())

	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.Equal(t, want, termRangesByFile(t, files), "healed retry must match a never-failed builder")
}

// TestBuilder_SpillAndMergeMetrics verifies the run/merge instrumentation: a
// forced multi-spill cycle increments runs_spilled_total once per spilled run
// and run_spill_bytes_total by the bytes on disk (deltas observed by the
// indexBuilder after onFull/finish — the postings buffer itself has no metrics
// dependency), and the merge histograms observe one merge with the full k-way
// fan-in.
func TestBuilder_SpillAndMergeMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	b, err := newIndexBuilder(roundTripConfig(t, 4, 64), "2026-01-01", log.NewNopLogger(), NewMetrics(reg))
	require.NoError(t, err)
	defer b.clear()
	require.NoError(t, b.processStream(&logproto.Stream{Entries: retryTestEntries()}, nil, time.Now(), recordRef{}))

	_, err = b.prepareIndexes()
	require.NoError(t, err)

	// Runs survive prepareIndexes (invariant #6), so runPaths is the ground truth.
	runs := len(b.ing.postings.runPaths)
	require.Greater(t, runs, 1, "dataset must force multiple spills for this test to prove anything")
	require.Equal(t, float64(runs), testutil.ToFloat64(b.metrics.runsSpilledTotal))
	require.Equal(t, float64(b.ing.postings.runBytes.Load()), testutil.ToFloat64(b.metrics.runSpillBytesTotal))

	fanInCount, fanInSum := histogramTotals(t, reg, "logline_index_builder_runs_per_merge")
	require.Equal(t, uint64(1), fanInCount, "one flush = one merge observation")
	require.Equal(t, float64(runs), fanInSum, "runs_per_merge must observe the k-way fan-in")
	durCount, _ := histogramTotals(t, reg, "logline_index_builder_merge_duration_seconds")
	require.Equal(t, uint64(1), durCount)
}

// TestBuilder_Clear_FullReset verifies clear() is a complete reset: handles
// closed, the private scratch subdir removed wholesale, and every piece of
// postings-buffer run state zeroed (builders are single-cycle, but clear must
// not leave stale state as a trap).
func TestBuilder_Clear_FullReset(t *testing.T) {
	b, err := newIndexBuilder(roundTripConfig(t, 4, 64), "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	require.NoError(t, b.processStream(&logproto.Stream{Entries: retryTestEntries()}, nil, time.Now(), recordRef{}))

	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.NotEmpty(t, files)
	require.Positive(t, b.runDiskBytes())
	handles := append([]*os.File(nil), b.openFiles...)
	require.NotEmpty(t, handles)
	runDir := b.runDir

	b.clear()

	// Handles were closed by clear — a second Close reports ErrClosed.
	for _, fh := range handles {
		require.ErrorIs(t, fh.Close(), os.ErrClosed, "clear must close .lidx handles")
	}
	// The whole scratch subdir (runs + .lidx) is gone.
	_, statErr := os.Stat(runDir)
	require.True(t, os.IsNotExist(statErr), "clear must remove the builder's scratch subdir")

	// Builder and postings-buffer state fully reset.
	require.Zero(t, b.runDiskBytes())
	require.Zero(t, b.estimatedMemoryBytes())
	require.Empty(t, b.openFiles)
	require.Empty(t, b.lidxPaths)
	require.Empty(t, b.ing.dateRanges)
	require.True(t, b.firstAppend.IsZero())
	require.True(t, b.lastAppend.IsZero())
	require.Empty(t, b.ing.postings.runPaths)
	require.Zero(t, b.ing.postings.runBytes.Load())
	require.Zero(t, b.ing.postings.runSeq)
	require.False(t, b.ing.postings.dirCreated)
	require.Zero(t, b.ing.postings.sortedHead)
	require.Empty(t, b.ing.postings.refTicks)
}
