package compactor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	schedulerapi "github.com/grafana/loki/v3/pkg/logline/scheduler/api"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// populateFileInfo sets the size, hash and header of meta from the index file f.
func populateFileInfo(f *os.File, meta *store.Meta) error {
	if err := meta.SetFileInfo(f); err != nil {
		return err
	}
	header, err := logline.ReadHeaderAt(f, meta.SizeBytes)
	if err != nil {
		return err
	}
	meta.IndexHeader = &header
	return nil
}

// ---- Config tests ----

func TestConfig_DownloadConcurrency_DefaultsFour(t *testing.T) {
	cfg := Config{Compaction: logline.CompactionConfig{SchedulerAddress: "http://s:3100", CompactorID: "w1"}}
	require.NoError(t, cfg.Validate())
	require.Equal(t, 4, cfg.Compaction.DownloadConcurrency)
}

// ---- Metrics tests ----

func TestMetrics_NewMetrics_IncludesPhaseHistograms(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	require.NotNil(t, m.mergeDuration)
	require.NotNil(t, m.uploadDuration)
	require.NotNil(t, m.bytesUploaded)
}

// ---- compact() helpers ----

// streamingInMemBucket wraps objstore.Bucket so Upload reads from r into a
// buffer before taking any internal locks. Real S3/GCS backends behave this
// way, but objstore.InMemBucket takes its write lock first and blocks any
// concurrent GetRange, which deadlocks tests that read sources and upload
// output concurrently via io.Pipe (e.g. PutIndexStreaming).
type streamingInMemBucket struct {
	objstore.Bucket
}

func (b *streamingInMemBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return b.Bucket.Upload(ctx, name, bytes.NewReader(buf))
}

// newTestCompactorStore creates a pkg/store Store wrapping an InMemBucket.
// The bucket is wrapped so concurrent reads and streaming uploads don't
// deadlock on InMemBucket's upload-time write lock.
func newTestCompactorStore(t *testing.T, bucket objstore.Bucket) *store.Store {
	t.Helper()
	cfg := store.Config{MinDate: "0001-01-01"}
	require.NoError(t, cfg.Validate())
	s, err := store.NewStore(&streamingInMemBucket{Bucket: bucket}, cfg, log.NewNopLogger(), nil)
	require.NoError(t, err)
	return s
}

// writeRealIndex creates a real LOGL index file with a few test documents and
// uploads it to the store. Returns the store.Meta describing it.
// The now parameter is the MaxLogTs for the written index; MinLogTs is now-1h.
func writeRealIndex(t *testing.T, s *store.Store, date, hash string, now time.Time) store.Meta {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, hash+".idx")

	docs := []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: now.Add(-1 * time.Hour).UnixMilli(), MaxTimeUnix: now.UnixMilli()},
	}
	w, err := logline.NewWriter("v3", path, docs, nil)
	require.NoError(t, err)
	var term [8]byte
	copy(term[:], "testterm")
	bm := roaring.New()
	bm.Add(0)
	require.NoError(t, w.WriteTermBitmap(term, format.Bitmap{Roaring: bm}))
	require.NoError(t, w.Close())

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	meta := store.Meta{
		Date: date, StorageID: hash, Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-1 * time.Hour), MaxRecordTs: now,
	}
	require.NoError(t, populateFileInfo(f, &meta))
	err = s.PutIndex(context.Background(), f, meta)
	require.NoError(t, err)
	return meta
}

// newTestCompactor creates a compactor Service with a real store for compaction tests.
func newTestCompactor(t *testing.T, s *store.Store) *Service {
	t.Helper()
	cfg := Config{Compaction: logline.CompactionConfig{
		SchedulerAddress: "http://localhost:9999",
		CompactorID:      "test-compactor",
	}}
	require.NoError(t, cfg.Validate())
	svc, err := New(s, cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	return svc
}

// ---- compact() tests ----

func TestCompact_HappyPath_TwoSources(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	now := time.Now().UTC().Truncate(time.Second)
	meta1 := writeRealIndex(t, s, "2026-02-25", "1111111111111111", now.Add(-2*time.Hour))
	meta2 := writeRealIndex(t, s, "2026-02-25", "2222222222222222", now.Add(-1*time.Hour))

	job := &schedulerapi.JobResponse{
		ID:        "testjob1",
		SourceIDs: []string{meta1.ID(), meta2.ID()},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	err := svc.compact(context.Background(), job)
	require.NoError(t, err)

	// Verify merged index was uploaded: poll and check snapshot
	require.NoError(t, s.Poll(context.Background()))
	snap := s.Snapshot()

	// Both sources must be compacted
	require.True(t, snap.IsCompacted(meta1.ID()), "src001 should be compacted")
	require.True(t, snap.IsCompacted(meta2.ID()), "src002 should be compacted")

	// One active index remains (the merged one)
	active := snap.Active()
	require.Len(t, active, 1)
	merged := active[0]

	require.NotEmpty(t, merged.StorageID)
	require.Equal(t, merged.ID(), "2026-02-25/"+merged.StorageID)
	require.NotEqual(t, merged.StorageID, merged.Hash)

	// CompactedFrom must list both sources
	require.ElementsMatch(t, []string{meta1.ID(), meta2.ID()}, merged.CompactedFrom)

	// Version is the resolved canonical version from source metas.
	require.Equal(t, "v3", merged.Version)

	// Time bounds must cover both sources
	require.True(t, !merged.MinLogTs.After(meta1.MinLogTs), "merged MinLogTs should be <= src1.MinLogTs")
	require.True(t, !merged.MaxLogTs.Before(meta2.MaxLogTs), "merged MaxLogTs should be >= src2.MaxLogTs")
}

func TestCompact_TooFewSourceIDs_ReturnsError(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	for _, tc := range []struct {
		name      string
		sourceIDs []string
	}{
		{"empty", []string{}},
		{"single", []string{"2026-02-25/0000000000000000"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := &schedulerapi.JobResponse{
				ID:        "fewjob",
				SourceIDs: tc.sourceIDs,
				ExpiresAt: time.Now().Add(5 * time.Minute),
			}
			err := svc.compact(context.Background(), job)
			require.Error(t, err)
			require.Contains(t, err.Error(), "at least 2 source_ids")
		})
	}
}

func TestCompact_CancelledContext_ReturnsContextError(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	now := time.Now().UTC()
	meta1 := writeRealIndex(t, s, "2026-02-25", "3333333333333333", now)
	meta2 := writeRealIndex(t, s, "2026-02-25", "3333333333333334", now)
	job := &schedulerapi.JobResponse{
		ID:        "ctxjob",
		SourceIDs: []string{meta1.ID(), meta2.ID()},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	err := svc.compact(ctx, job)
	require.Error(t, err)
}

func TestCompact_MissingSourceIndex_ReturnsError(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	now := time.Now().UTC()
	meta1 := writeRealIndex(t, s, "2026-02-25", "3333333333333335", now)
	job := &schedulerapi.JobResponse{
		ID:        "missingjob",
		SourceIDs: []string{meta1.ID(), "2026-02-25/0000000000000000"},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	err := svc.compact(context.Background(), job)
	require.Error(t, err)
}

func TestCompact_TimeBoundsUnion(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	// Write sources with non-overlapping time ranges
	now := time.Now().UTC().Truncate(time.Second)
	meta1 := writeRealIndex(t, s, "2026-02-25", "4444444444444444", now.Add(-6*time.Hour))
	meta2 := writeRealIndex(t, s, "2026-02-25", "5555555555555555", now)

	job := &schedulerapi.JobResponse{
		ID:        "tbjob",
		SourceIDs: []string{meta1.ID(), meta2.ID()},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	require.NoError(t, svc.compact(context.Background(), job))

	require.NoError(t, s.Poll(context.Background()))
	active := s.Snapshot().Active()
	require.Len(t, active, 1)
	merged := active[0]

	// MinLogTs should be the minimum of all sources
	expectedMin := meta1.MinLogTs
	if meta2.MinLogTs.Before(expectedMin) {
		expectedMin = meta2.MinLogTs
	}
	require.True(t, merged.MinLogTs.Equal(expectedMin), "merged MinLogTs should be union minimum")

	// MaxLogTs should be the maximum of all sources
	expectedMax := meta1.MaxLogTs
	if meta2.MaxLogTs.After(expectedMax) {
		expectedMax = meta2.MaxLogTs
	}
	require.True(t, merged.MaxLogTs.Equal(expectedMax), "merged MaxLogTs should be union maximum")
}

func TestCompact_SingleSource_ReturnsError(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	now := time.Now().UTC().Truncate(time.Second)
	meta1 := writeRealIndex(t, s, "2026-02-25", "6666666666666666", now)

	job := &schedulerapi.JobResponse{
		ID:        "singlejob",
		SourceIDs: []string{meta1.ID()},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	err := svc.compact(context.Background(), job)
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least 2 source_ids")
}

// TestCompact_E2E_AllTermsAndDocsSurvive builds N synthetic indexes with known
// terms and documents, compacts them through the full pipeline (range-read from
// object storage → merge → upload), then downloads the merged index and verifies
// that every term/document pair from every source is present in the output.
func TestCompact_E2E_AllTermsAndDocsSurvive(t *testing.T) {
	const (
		numSources   = 4
		docsPerIndex = 20
		termsPerDoc  = 50
		date         = "2026-02-25"
	)

	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	baseTime := time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)

	// sourceTerms[i] maps term → set of doc IDs that have that term in source i.
	// We track these to verify after compaction.
	type termKey = [8]byte
	sourceTerms := make([]map[termKey]*roaring.Bitmap, numSources)
	sourceDocs := make([][]format.DocumentMetadata, numSources)
	var sourceIDs []string

	for src := 0; src < numSources; src++ {
		hash := fmt.Sprintf("%016x", src+0xa0)
		dir := t.TempDir()
		path := filepath.Join(dir, hash+".idx")

		sourceTerms[src] = make(map[termKey]*roaring.Bitmap)

		// Each source has unique documents (non-overlapping time ranges).
		for d := 0; d < docsPerIndex; d++ {
			globalDoc := src*docsPerIndex + d
			minT := baseTime.Add(time.Duration(globalDoc) * time.Minute)
			maxT := minT.Add(time.Minute)
			sourceDocs[src] = append(sourceDocs[src], format.DocumentMetadata{
				ID:          uint32(d),
				MinTimeUnix: minT.UnixMilli(),
				MaxTimeUnix: maxT.UnixMilli(),
			})
		}

		w, err := logline.NewWriter("v3", path, sourceDocs[src], nil)
		require.NoError(t, err)

		// Generate terms. Some are unique per source, some are shared across
		// sources (same term bytes) to exercise bitmap union during merge.
		type termEntry struct {
			term termKey
			bm   *roaring.Bitmap
		}
		termEntries := make([]termEntry, termsPerDoc)
		for ti := 0; ti < termsPerDoc; ti++ {
			var term termKey
			if ti < termsPerDoc/2 {
				// Shared terms: same bytes across all sources.
				term[0] = byte(ti >> 8)
				term[1] = byte(ti)
				term[2] = 'S' // shared marker
				term[3] = byte('A' + (ti % 26))
				term[4] = byte('A' + ((ti / 26) % 26))
				term[5] = byte('A' + ((ti / 676) % 26))
			} else {
				// Unique terms: include source index in the key.
				term[0] = byte(src)
				term[1] = byte(ti >> 8)
				term[2] = byte(ti)
				term[3] = 'U' // unique marker
				term[4] = byte('A' + (ti % 26))
				term[5] = byte('A' + ((ti / 26) % 26))
			}
			bm := roaring.New()
			// Each term appears in a deterministic subset of docs.
			for d := ti % 5; d < docsPerIndex; d += 5 {
				bm.Add(uint32(d))
			}
			termEntries[ti] = termEntry{term: term, bm: bm}
			sourceTerms[src][term] = bm.Clone()
		}
		// StreamingIndexWriter requires terms in ascending lexicographic order.
		sort.Slice(termEntries, func(i, j int) bool {
			return bytes.Compare(termEntries[i].term[:], termEntries[j].term[:]) < 0
		})
		for _, te := range termEntries {
			require.NoError(t, w.WriteTermBitmap(te.term, format.Bitmap{Roaring: te.bm}))
		}
		require.NoError(t, w.Close())

		f, err := os.Open(path)
		require.NoError(t, err)
		meta := store.Meta{
			Date: date, StorageID: hash, Version: "v3",
			MinLogTs:    baseTime.Add(time.Duration(src*docsPerIndex) * time.Minute),
			MaxLogTs:    baseTime.Add(time.Duration((src+1)*docsPerIndex) * time.Minute),
			MinRecordTs: baseTime.Add(time.Duration(src*docsPerIndex) * time.Minute),
			MaxRecordTs: baseTime.Add(time.Duration((src+1)*docsPerIndex) * time.Minute),
		}
		require.NoError(t, populateFileInfo(f, &meta))
		require.NoError(t, s.PutIndex(context.Background(), f, meta))
		f.Close()
		sourceIDs = append(sourceIDs, meta.ID())
	}

	// Run compaction through the full pipeline.
	job := &schedulerapi.JobResponse{
		ID:        "e2e-job",
		SourceIDs: sourceIDs,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	require.NoError(t, svc.compact(context.Background(), job))

	// Find the merged index in the store.
	require.NoError(t, s.Poll(context.Background()))
	snap := s.Snapshot()
	active := snap.Active()
	require.Len(t, active, 1, "expected exactly 1 active (merged) index")
	merged := active[0]

	// Download the merged index to a local file and open it.
	rc, err := s.GetIndex(context.Background(), merged.ID())
	require.NoError(t, err)
	mergedDir := t.TempDir()
	mergedPath := filepath.Join(mergedDir, "merged.idx")
	mf, err := os.Create(mergedPath)
	require.NoError(t, err)
	_, err = mf.ReadFrom(rc)
	require.NoError(t, err)
	rc.Close()
	mf.Close()

	reader, _, err := logline.OpenFile(mergedPath)
	require.NoError(t, err)
	defer reader.Close()

	// Build a lookup of merged documents by time bounds → doc ID.
	mergedDocs := reader.Documents()
	type docKey struct{ min, max int64 }
	mergedDocMap := make(map[docKey]uint32, len(mergedDocs))
	for _, d := range mergedDocs {
		mergedDocMap[docKey{d.MinTimeUnix, d.MaxTimeUnix}] = d.ID
	}

	// Verify every document from every source exists in the merged index.
	for src := 0; src < numSources; src++ {
		for _, doc := range sourceDocs[src] {
			key := docKey{doc.MinTimeUnix, doc.MaxTimeUnix}
			_, exists := mergedDocMap[key]
			require.True(t, exists, "source %d doc %d (min=%d max=%d) missing from merged index",
				src, doc.ID, doc.MinTimeUnix, doc.MaxTimeUnix)
		}
	}

	// Verify every term→doc mapping from every source is present in the merged index.
	// For each source term, remap source doc IDs to merged doc IDs (via time bounds),
	// then query the merged index and confirm all expected doc IDs are present.
	for src := 0; src < numSources; src++ {
		for term, srcBitmap := range sourceTerms[src] {
			termStr := string(term[:6]) // Query takes a string of NgramLength
			mergedDocIDs, err := queryReader(reader, termStr)
			require.NoError(t, err, "query term %x from source %d", term, src)
			require.NotNil(t, mergedDocIDs, "term %x from source %d missing entirely from merged index", term, src)

			mergedSet := roaring.New()
			mergedSet.AddMany(mergedDocIDs)

			// Remap source doc IDs → merged doc IDs via time bounds.
			it := srcBitmap.Iterator()
			for it.HasNext() {
				srcDocID := it.Next()
				srcDoc := sourceDocs[src][srcDocID]
				key := docKey{srcDoc.MinTimeUnix, srcDoc.MaxTimeUnix}
				mergedDocID, exists := mergedDocMap[key]
				require.True(t, exists, "source %d doc %d not in merged doc map", src, srcDocID)
				require.True(t, mergedSet.Contains(mergedDocID),
					"source %d term %x doc %d (merged doc %d) not in merged bitmap",
					src, term, srcDocID, mergedDocID)
			}
		}
	}
}

// FuzzCompact_TermsAndDocsSurvive generates random source indexes with random
// ngrams, documents, and overlapping time ranges, compacts them through the full
// pipeline, and verifies every term/document pair survives and documents are
// correctly deduplicated.
func FuzzCompact_TermsAndDocsSurvive(f *testing.F) {
	f.Add(uint8(2), uint8(5), uint8(10), uint8(3), int64(1000))
	f.Add(uint8(4), uint8(20), uint8(50), uint8(10), int64(9999))
	f.Add(uint8(2), uint8(1), uint8(1), uint8(0), int64(42))
	f.Add(uint8(8), uint8(10), uint8(30), uint8(5), int64(7777))

	f.Fuzz(func(t *testing.T, numSourcesRaw, docsPerIndexRaw, termsPerIndexRaw, overlapDocsRaw uint8, seed int64) {
		// Clamp inputs to reasonable ranges.
		numSources := int(numSourcesRaw%7) + 2                  // 2..8
		docsPerIndex := int(docsPerIndexRaw%20) + 1             // 1..20
		termsPerIndex := int(termsPerIndexRaw%50) + 1           // 1..50
		overlapDocs := int(overlapDocsRaw) % (docsPerIndex + 1) // 0..docsPerIndex

		bucket := objstore.NewInMemBucket()
		s := newTestCompactorStore(t, bucket)
		svc := newTestCompactor(t, s)

		baseTime := time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)

		type termKey = [8]byte
		// allTermDocs maps term → set of (minTime, maxTime) doc keys that have the term.
		// This is the ground truth: union across all sources after dedup.
		type docKey struct{ min, max int64 }
		allTermDocs := make(map[termKey]map[docKey]struct{})
		allDocs := make(map[docKey]struct{})
		var sourceIDs []string

		for src := 0; src < numSources; src++ {
			hash := fmt.Sprintf("%016x", src+0xf0)
			dir := t.TempDir()
			path := filepath.Join(dir, hash+".idx")

			docs := make([]format.DocumentMetadata, docsPerIndex)
			for d := 0; d < docsPerIndex; d++ {
				var minT, maxT int64
				if d < overlapDocs {
					// Overlapping docs: same time bounds across all sources → deduped.
					minT = baseTime.Add(time.Duration(d) * time.Minute).UnixMilli()
					maxT = baseTime.Add(time.Duration(d+1) * time.Minute).UnixMilli()
				} else {
					// Unique docs per source.
					offset := time.Duration(src*docsPerIndex+d) * time.Minute
					minT = baseTime.Add(offset).UnixMilli()
					maxT = baseTime.Add(offset + time.Minute).UnixMilli()
				}
				docs[d] = format.DocumentMetadata{
					ID: uint32(d), MinTimeUnix: minT, MaxTimeUnix: maxT,
				}
				allDocs[docKey{minT, maxT}] = struct{}{}
			}

			w, err := logline.NewWriter("v3", path, docs, nil)
			require.NoError(t, err)

			// Generate terms using seed + source index for deterministic randomness.
			// Collect all terms first, then sort before writing (streaming writer requires order).
			type fuzzTermEntry struct {
				term termKey
				bm   *roaring.Bitmap
			}
			fuzzTerms := make([]fuzzTermEntry, termsPerIndex)
			for ti := 0; ti < termsPerIndex; ti++ {
				var term termKey
				// Mix seed, source, and term index to generate pseudo-random ngrams.
				v := uint64(seed) ^ (uint64(src) << 32) ^ (uint64(ti) << 16)
				term[0] = byte(v)
				term[1] = byte(v >> 8)
				term[2] = byte(v >> 16)
				term[3] = byte(v >> 24)
				term[4] = byte(v >> 32)
				term[5] = byte(v >> 40)

				bm := roaring.New()
				// Each term appears in a pseudo-random subset of docs.
				step := (ti % 5) + 1
				for d := ti % step; d < docsPerIndex; d += step {
					bm.Add(uint32(d))

					dk := docKey{docs[d].MinTimeUnix, docs[d].MaxTimeUnix}
					if allTermDocs[term] == nil {
						allTermDocs[term] = make(map[docKey]struct{})
					}
					allTermDocs[term][dk] = struct{}{}
				}
				fuzzTerms[ti] = fuzzTermEntry{term: term, bm: bm}
			}
			sort.Slice(fuzzTerms, func(i, j int) bool {
				return bytes.Compare(fuzzTerms[i].term[:], fuzzTerms[j].term[:]) < 0
			})
			for _, te := range fuzzTerms {
				require.NoError(t, w.WriteTermBitmap(te.term, format.Bitmap{Roaring: te.bm}))
			}
			require.NoError(t, w.Close())

			fl, err := os.Open(path)
			require.NoError(t, err)

			minTs := baseTime.Add(time.Duration(src*docsPerIndex) * time.Minute)
			maxTs := baseTime.Add(time.Duration((src+1)*docsPerIndex) * time.Minute)
			if overlapDocs > 0 && src > 0 {
				minTs = baseTime // overlapping docs pull min back
			}
			meta := store.Meta{
				Date: "2026-02-25", StorageID: hash, Version: "v3",
				MinLogTs: minTs, MaxLogTs: maxTs,
				MinRecordTs: minTs, MaxRecordTs: maxTs,
			}
			require.NoError(t, populateFileInfo(fl, &meta))
			require.NoError(t, s.PutIndex(context.Background(), fl, meta))
			fl.Close()
			sourceIDs = append(sourceIDs, meta.ID())
		}

		// Compact.
		job := &schedulerapi.JobResponse{
			ID:        "fuzz-job",
			SourceIDs: sourceIDs,
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}
		require.NoError(t, svc.compact(context.Background(), job))

		// Open merged index.
		require.NoError(t, s.Poll(context.Background()))
		snap := s.Snapshot()
		active := snap.Active()
		require.Len(t, active, 1)

		rc, err := s.GetIndex(context.Background(), active[0].ID())
		require.NoError(t, err)
		mergedDir := t.TempDir()
		mergedPath := filepath.Join(mergedDir, "merged.idx")
		mf, err := os.Create(mergedPath)
		require.NoError(t, err)
		_, err = mf.ReadFrom(rc)
		require.NoError(t, err)
		rc.Close()
		mf.Close()

		reader, _, err := logline.OpenFile(mergedPath)
		require.NoError(t, err)
		defer reader.Close()

		// Build merged doc lookup.
		mergedDocMap := make(map[docKey]uint32)
		for _, d := range reader.Documents() {
			mergedDocMap[docKey{d.MinTimeUnix, d.MaxTimeUnix}] = d.ID
		}

		// Every unique doc must be present (deduplication correctness).
		require.Equal(t, len(allDocs), len(mergedDocMap),
			"merged doc count should equal unique doc count")
		for dk := range allDocs {
			_, exists := mergedDocMap[dk]
			require.True(t, exists, "doc (min=%d, max=%d) missing from merged index", dk.min, dk.max)
		}

		// Every term→doc pair must survive.
		for term, expectedDocs := range allTermDocs {
			termStr := string(term[:6])
			mergedDocIDs, err := queryReader(reader, termStr)
			require.NoError(t, err)
			require.NotNil(t, mergedDocIDs, "term %x missing from merged index", term)

			mergedSet := roaring.New()
			mergedSet.AddMany(mergedDocIDs)

			for dk := range expectedDocs {
				mergedDocID, exists := mergedDocMap[dk]
				require.True(t, exists)
				require.True(t, mergedSet.Contains(mergedDocID),
					"term %x missing doc (min=%d, max=%d, merged_id=%d)",
					term, dk.min, dk.max, mergedDocID)
			}
		}
	})
}

// TestCompact_ByteIdenticalSubset_NoDuplicateStorageID verifies the scenario
// that caused the self-reference bug: when a large index and a strict subset
// are compacted, the merged output is byte-identical to the large input
// (same xxh3 hash) but gets a DIFFERENT StorageID (and thus a different object
// path). Before the fix, the merged index would collide with the source, hiding
// it from queries.
func TestCompact_ByteIdenticalSubset_NoDuplicateStorageID(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	now := time.Date(2026, 2, 25, 12, 0, 0, 0, time.UTC)
	date := "2026-02-25"

	// Build the "large" index with 3 docs and 2 terms.
	largeDir := t.TempDir()
	largePath := filepath.Join(largeDir, "large.idx")
	{
		largeDocs := []format.DocumentMetadata{
			{ID: 0, MinTimeUnix: now.Add(-3 * time.Hour).UnixMilli(), MaxTimeUnix: now.Add(-2 * time.Hour).UnixMilli()},
			{ID: 1, MinTimeUnix: now.Add(-2 * time.Hour).UnixMilli(), MaxTimeUnix: now.Add(-1 * time.Hour).UnixMilli()},
			{ID: 2, MinTimeUnix: now.Add(-1 * time.Hour).UnixMilli(), MaxTimeUnix: now.UnixMilli()},
		}
		w, err := logline.NewWriter("v3", largePath, largeDocs, nil)
		require.NoError(t, err)

		var termA, termB [8]byte
		copy(termA[:], "TERMAAA")
		copy(termB[:], "TERMBBB")

		bmA := roaring.New()
		bmA.AddMany([]uint32{0, 1, 2})
		require.NoError(t, w.WriteTermBitmap(termA, format.Bitmap{Roaring: bmA}))

		bmB := roaring.New()
		bmB.AddMany([]uint32{1, 2})
		require.NoError(t, w.WriteTermBitmap(termB, format.Bitmap{Roaring: bmB}))

		require.NoError(t, w.Close())
	}

	// Upload large index with a StorageID (post-PR behavior).
	largeStorageID, err := store.NewStorageID()
	require.NoError(t, err)
	largeMeta := store.Meta{
		StorageID:   largeStorageID,
		Date:        date,
		Version:     "v3",
		MinLogTs:    now.Add(-3 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-3 * time.Hour),
		MaxRecordTs: now,
	}
	{
		f, err := os.Open(largePath)
		require.NoError(t, err)
		require.NoError(t, populateFileInfo(f, &largeMeta))
		require.NoError(t, s.PutIndex(context.Background(), f, largeMeta))
		f.Close()
	}

	// Build the "subset" index: docs and terms are a strict subset of the large index.
	subsetDir := t.TempDir()
	subsetPath := filepath.Join(subsetDir, "subset.idx")
	{
		// Only docs 0 and 1 (both present in the large index).
		subsetDocs := []format.DocumentMetadata{
			{ID: 0, MinTimeUnix: now.Add(-3 * time.Hour).UnixMilli(), MaxTimeUnix: now.Add(-2 * time.Hour).UnixMilli()},
			{ID: 1, MinTimeUnix: now.Add(-2 * time.Hour).UnixMilli(), MaxTimeUnix: now.Add(-1 * time.Hour).UnixMilli()},
		}
		w, err := logline.NewWriter("v3", subsetPath, subsetDocs, nil)
		require.NoError(t, err)

		var termA [8]byte
		copy(termA[:], "TERMAAA")
		bmA := roaring.New()
		bmA.AddMany([]uint32{0, 1})
		require.NoError(t, w.WriteTermBitmap(termA, format.Bitmap{Roaring: bmA}))

		require.NoError(t, w.Close())
	}

	subsetStorageID, err := store.NewStorageID()
	require.NoError(t, err)
	subsetMeta := store.Meta{
		StorageID:   subsetStorageID,
		Date:        date,
		Version:     "v3",
		MinLogTs:    now.Add(-3 * time.Hour),
		MaxLogTs:    now.Add(-1 * time.Hour),
		MinRecordTs: now.Add(-3 * time.Hour),
		MaxRecordTs: now.Add(-1 * time.Hour),
	}
	{
		f, err := os.Open(subsetPath)
		require.NoError(t, err)
		require.NoError(t, populateFileInfo(f, &subsetMeta))
		require.NoError(t, s.PutIndex(context.Background(), f, subsetMeta))
		f.Close()
	}

	// Compact large + subset.
	job := &schedulerapi.JobResponse{
		ID:        "subset-job",
		SourceIDs: []string{largeMeta.ID(), subsetMeta.ID()},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	require.NoError(t, svc.compact(context.Background(), job))

	// Poll store to pick up the merged meta.
	require.NoError(t, s.Poll(context.Background()))
	snap := s.Snapshot()

	// Both sources must be marked compacted.
	require.True(t, snap.IsCompacted(largeMeta.ID()), "large source should be compacted")
	require.True(t, snap.IsCompacted(subsetMeta.ID()), "subset source should be compacted")

	// Exactly one active (merged) index.
	active := snap.Active()
	require.Len(t, active, 1, "expected exactly 1 active index after compaction")
	merged := active[0]

	// The merged output should be byte-identical to the large input
	// because the subset adds no new documents or terms.
	require.Equal(t, largeMeta.Hash, merged.Hash,
		"merged hash should equal large-index hash (byte-identical output)")

	// The merged index must have a DIFFERENT StorageID than the large source.
	// This is the key invariant: same content hash but different object path
	// prevents the self-reference collision.
	require.NotEqual(t, largeStorageID, merged.StorageID,
		"merged StorageID must differ from large source StorageID")
	require.NotEqual(t, subsetStorageID, merged.StorageID,
		"merged StorageID must differ from subset source StorageID")

	// The merged index must be reachable via its own unique path.
	require.NotEqual(t, largeMeta.ID(), merged.ID(),
		"merged ID must differ from large source ID")

	// Verify the merged index is queryable — download and check terms.
	rc, err := s.GetIndex(context.Background(), merged.ID())
	require.NoError(t, err)
	mergedDir := t.TempDir()
	mergedPath := filepath.Join(mergedDir, "merged.idx")
	mf, err := os.Create(mergedPath)
	require.NoError(t, err)
	_, err = mf.ReadFrom(rc)
	require.NoError(t, err)
	rc.Close()
	mf.Close()

	reader, _, err := logline.OpenFile(mergedPath)
	require.NoError(t, err)
	defer reader.Close()

	// All 3 docs must survive.
	require.Len(t, reader.Documents(), 3, "merged index should have all 3 documents")

	// Query TERMAAA — should match all 3 docs.
	termADocIDs, err := queryReader(reader, "TERMAAA")
	require.NoError(t, err)
	require.Len(t, termADocIDs, 3, "TERMAAA should match 3 docs in merged index")

	// Query TERMBBB — should match 2 docs.
	termBDocIDs, err := queryReader(reader, "TERMBBB")
	require.NoError(t, err)
	require.Len(t, termBDocIDs, 2, "TERMBBB should match 2 docs in merged index")
}

// ---- verifyCompactionJob tests ----

func TestVerifyCompactionJob_AllMatch(t *testing.T) {
	metas := []store.Meta{
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 2},
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 2},
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 2},
	}
	require.NoError(t, verifyCompactionJob(metas))
}

func TestVerifyCompactionJob_AllUnsharded(t *testing.T) {
	metas := []store.Meta{
		{Version: "v3", ShardCount: 0, ShardAlgorithm: "", ShardValue: 0},
		{Version: "v3", ShardCount: 0, ShardAlgorithm: "", ShardValue: 0},
	}
	require.NoError(t, verifyCompactionJob(metas))
}

func TestVerifyCompactionJob_MismatchShardCount(t *testing.T) {
	metas := []store.Meta{
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 2},
		{Version: "v3", ShardCount: 8, ShardAlgorithm: "first_byte", ShardValue: 2},
	}
	err := verifyCompactionJob(metas)
	require.Error(t, err)
	require.Contains(t, err.Error(), "shard_count")
}

func TestVerifyCompactionJob_MismatchAlgorithm(t *testing.T) {
	metas := []store.Meta{
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 2},
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "xxhash", ShardValue: 2},
	}
	err := verifyCompactionJob(metas)
	require.Error(t, err)
	require.Contains(t, err.Error(), "shard_algorithm")
}

func TestVerifyCompactionJob_MismatchShardValue(t *testing.T) {
	metas := []store.Meta{
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 0},
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 3},
	}
	err := verifyCompactionJob(metas)
	require.Error(t, err)
	require.Contains(t, err.Error(), "shard_value")
}

func TestVerifyCompactionJob_MismatchInThirdSource(t *testing.T) {
	metas := []store.Meta{
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 1},
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 1},
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 2},
	}
	err := verifyCompactionJob(metas)
	require.Error(t, err)
	require.Contains(t, err.Error(), "source 2")
}

func TestVerifyCompactionJob_UnsupportedVersion(t *testing.T) {
	metas := []store.Meta{
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 1},
		{Version: "v99", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 1},
	}
	err := verifyCompactionJob(metas)
	require.Error(t, err)
	require.Contains(t, err.Error(), "version")
}

func TestVerifyCompactionJob_SingleMeta(t *testing.T) {
	metas := []store.Meta{
		{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte", ShardValue: 2},
	}
	require.NoError(t, verifyCompactionJob(metas))
}

func queryReader(r logline.Reader, term string) ([]uint32, error) {
	pos, err := r.FindTerm(term)
	if err != nil {
		return nil, err
	}
	if pos < 0 {
		return nil, nil
	}
	res, err := r.GetBitmap(pos)
	if err != nil {
		return nil, err
	}
	if res.MatchesAll {
		docIDs := make([]uint32, len(r.Documents()))
		for i, doc := range r.Documents() {
			docIDs[i] = doc.ID
		}
		return docIDs, nil
	}
	return res.Roaring.ToArray(), nil
}
