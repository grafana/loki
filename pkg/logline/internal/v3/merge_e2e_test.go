package v3

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

func testStreamingMergeE2EComprehensive(t *testing.T, mergeCfg IndexWriteConfig) {
	t.Helper()
	rng := rand.New(rand.NewSource(42))

	// ---------------------------------------------------------------
	// 1. Generate source data
	// ---------------------------------------------------------------
	const (
		numDocsA  = 50
		numDocsB  = 80
		numShared = 15 // docs with identical time bounds across both sources

		numTermsOnlyA  = 100
		numTermsOnlyB  = 120
		numTermsShared = 60
	)

	// Build doc lists. The first numShared docs in each source share time bounds
	// so they should deduplicate during merge.
	docsA := make([]format.DocumentMetadata, numDocsA)
	docsB := make([]format.DocumentMetadata, numDocsB)
	for i := range numDocsA {
		docsA[i] = format.DocumentMetadata{
			ID:          uint32(i),
			MinTimeUnix: int64(i * 1000),
			MaxTimeUnix: int64(i*1000 + 500),
		}
	}
	for i := range numDocsB {
		if i < numShared {
			// Mirror source A's time bounds for deduplication testing.
			docsB[i] = format.DocumentMetadata{
				ID:          uint32(i),
				MinTimeUnix: docsA[i].MinTimeUnix,
				MaxTimeUnix: docsA[i].MaxTimeUnix,
			}
		} else {
			docsB[i] = format.DocumentMetadata{
				ID:          uint32(i),
				MinTimeUnix: int64((numDocsA + i) * 1000),
				MaxTimeUnix: int64((numDocsA+i)*1000 + 500),
			}
		}
	}

	expectedUniqueDocs := numDocsA + numDocsB - numShared

	// Generate term keys. NgramLength == 6, so each term is 6 printable chars.
	makeTerm := func(prefix string, idx int) [8]byte {
		var tk [8]byte
		copy(tk[:], fmt.Sprintf("%s%04d", prefix, idx))
		return tk
	}

	termsOnlyA := make([][8]byte, numTermsOnlyA)
	for i := range termsOnlyA {
		termsOnlyA[i] = makeTerm("A_", i)
	}
	termsOnlyB := make([][8]byte, numTermsOnlyB)
	for i := range termsOnlyB {
		termsOnlyB[i] = makeTerm("B_", i)
	}
	termsShared := make([][8]byte, numTermsShared)
	for i := range termsShared {
		termsShared[i] = makeTerm("S_", i)
	}

	// Assign pseudo-random doc IDs to each term.
	randDocIDs := func(r *rand.Rand, numDocs, maxDocs int) []uint32 {
		count := min(r.Intn(maxDocs/2)+1, numDocs)
		ids := make(map[uint32]struct{}, count)
		for len(ids) < count {
			ids[uint32(r.Intn(numDocs))] = struct{}{}
		}
		result := make([]uint32, 0, len(ids))
		for id := range ids {
			result = append(result, id)
		}
		slices.Sort(result)
		return result
	}

	postingsA := make(map[[8]byte][]uint32, numTermsOnlyA+numTermsShared)
	postingsB := make(map[[8]byte][]uint32, numTermsOnlyB+numTermsShared)

	for _, tk := range termsOnlyA {
		postingsA[tk] = randDocIDs(rng, numDocsA, numDocsA)
	}
	for _, tk := range termsOnlyB {
		postingsB[tk] = randDocIDs(rng, numDocsB, numDocsB)
	}
	for _, tk := range termsShared {
		postingsA[tk] = randDocIDs(rng, numDocsA, numDocsA)
		postingsB[tk] = randDocIDs(rng, numDocsB, numDocsB)
	}

	// ---------------------------------------------------------------
	// 2. Write source indexes
	// ---------------------------------------------------------------
	dir := t.TempDir()
	pathA := writeTestIndex(t, dir, "src_a.idx", docsA, postingsA)
	pathB := writeTestIndex(t, dir, "src_b.idx", docsB, postingsB)

	// ---------------------------------------------------------------
	// 3. Verify source indexes are readable
	// ---------------------------------------------------------------
	for _, p := range []string{pathA, pathB} {
		r, err := OpenIndexFile(p)
		require.NoError(t, err)
		r.Close()
	}

	// ---------------------------------------------------------------
	// 4. Merge
	// ---------------------------------------------------------------
	streamOut := filepath.Join(dir, "stream_merged.idx")
	streamOutFile, err := os.Create(streamOut)
	require.NoError(t, err)
	_, err = mergeFilesTo(t, context.Background(), []string{pathA, pathB}, streamOutFile, mergeCfg)
	require.NoError(t, err)
	require.NoError(t, streamOutFile.Close())

	// ---------------------------------------------------------------
	// 5. Open merged index
	// ---------------------------------------------------------------
	rs, err := OpenIndexFile(streamOut)
	require.NoError(t, err)
	defer rs.Close()

	// ---------------------------------------------------------------
	// 6. Header validation
	// ---------------------------------------------------------------
	h := rs.Header()
	require.Equal(t, IndexMagic, h.Magic, "magic")
	require.Equal(t, IndexVersion, h.Version, "version")
	require.Equal(t, uint32(expectedUniqueDocs), h.DocumentCount, "document count")
	require.True(t, h.PostingsDataSize > 0, "postings data size")
	require.True(t, h.TermDataSize > 0, "term data size")
	require.True(t, h.DocMetadataSize > 0, "doc metadata size")

	// ---------------------------------------------------------------
	// 7. Document metadata validation
	// ---------------------------------------------------------------
	{
		docs := rs.Documents()
		require.Len(t, docs, expectedUniqueDocs, "document list length")

		// IDs must be contiguous 0..N-1.
		for i, d := range docs {
			require.Equal(t, uint32(i), d.ID, "doc[%d].ID", i)
		}

		// Every source time bucket must be present.
		timeBuckets := make(map[docTimeKey]bool, len(docs))
		for _, d := range docs {
			timeBuckets[docTimeKey{d.MinTimeUnix, d.MaxTimeUnix}] = true
		}
		for _, d := range docsA {
			key := docTimeKey{d.MinTimeUnix, d.MaxTimeUnix}
			require.True(t, timeBuckets[key], "missing time bucket from source A: %v", key)
		}
		for _, d := range docsB {
			key := docTimeKey{d.MinTimeUnix, d.MaxTimeUnix}
			require.True(t, timeBuckets[key], "missing time bucket from source B: %v", key)
		}
	}

	// ---------------------------------------------------------------
	// 8. Term count validation
	// ---------------------------------------------------------------
	expectedTermCount := numTermsOnlyA + numTermsOnlyB + numTermsShared
	require.Equal(t, uint64(expectedTermCount), rs.Header().TermCount, "term count")

	// ---------------------------------------------------------------
	// 9. Build expected merged bitmaps using the same dedup/remap logic
	//    that the merge should perform, so we can verify every bitmap.
	// ---------------------------------------------------------------

	// Reproduce document deduplication.
	uniqueDocsMap := make(map[docTimeKey]uint32)
	var nextID uint32
	remapA := make([]uint32, numDocsA)
	for _, d := range docsA {
		key := docTimeKey{d.MinTimeUnix, d.MaxTimeUnix}
		if _, ok := uniqueDocsMap[key]; !ok {
			uniqueDocsMap[key] = nextID
			nextID++
		}
		remapA[d.ID] = uniqueDocsMap[key]
	}
	remapB := make([]uint32, numDocsB)
	for _, d := range docsB {
		key := docTimeKey{d.MinTimeUnix, d.MaxTimeUnix}
		if _, ok := uniqueDocsMap[key]; !ok {
			uniqueDocsMap[key] = nextID
			nextID++
		}
		remapB[d.ID] = uniqueDocsMap[key]
	}
	require.Equal(t, uint32(expectedUniqueDocs), nextID, "remap produced wrong doc count")

	// Build expected bitmaps per term.
	expectedBitmaps := make(map[[8]byte]*roaring.Bitmap)
	addRemapped := func(tk [8]byte, ids []uint32, remap []uint32) {
		bm, ok := expectedBitmaps[tk]
		if !ok {
			bm = roaring.New()
			expectedBitmaps[tk] = bm
		}
		for _, id := range ids {
			bm.Add(remap[id])
		}
	}
	for tk, ids := range postingsA {
		addRemapped(tk, ids, remapA)
	}
	for tk, ids := range postingsB {
		addRemapped(tk, ids, remapB)
	}

	// ---------------------------------------------------------------
	// 10. Iterator-based validation: every term and bitmap
	// ---------------------------------------------------------------
	{
		it, err := rs.NewTermIterator()
		require.NoError(t, err)

		seen := make(map[[8]byte]bool)
		for it.Next() {
			tk := it.Term()
			require.False(t, seen[tk], "duplicate term %x", tk)
			seen[tk] = true

			expected, ok := expectedBitmaps[tk]
			require.True(t, ok, "unexpected term %x in merged index", tk)

			actual := it.Bitmap()
			require.False(t, actual.MatchesAll, "unexpected sentinel for term %x", tk)
			require.True(t, expected.Equals(actual.Roaring),
				"bitmap mismatch for term %x\n  expected: %v\n  actual:   %v",
				tk, expected.ToArray(), actual.Roaring.ToArray())
		}
		require.NoError(t, it.Err())
		require.Len(t, seen, expectedTermCount, "wrong number of terms iterated")
	}

	// ---------------------------------------------------------------
	// 11. Query()-based validation: spot-check all term categories
	// ---------------------------------------------------------------
	allTerms := make([][8]byte, 0, expectedTermCount)
	allTerms = append(allTerms, termsOnlyA...)
	allTerms = append(allTerms, termsOnlyB...)
	allTerms = append(allTerms, termsShared...)

	for _, tk := range allTerms {
		queryStr := string(tk[:NgramLength])
		docIDs, err := rs.query(queryStr)
		require.NoError(t, err, "Query(%q)", queryStr)

		expected := expectedBitmaps[tk]
		require.Equal(t, int(expected.GetCardinality()), len(docIDs),
			"Query(%q) returned wrong number of docs", queryStr)

		for _, id := range docIDs {
			require.True(t, expected.Contains(id),
				"Query(%q) returned unexpected doc %d", queryStr, id)
		}
	}

	// A term that doesn't exist should return empty.
	missing, err := rs.query("ZZZZZZ")
	require.NoError(t, err, "Query(missing)")
	require.Empty(t, missing, "expected no results for missing term")

	// ---------------------------------------------------------------
	// 13. Re-merge: merged output can serve as input to another merge
	// ---------------------------------------------------------------
	reMergeOut := filepath.Join(dir, "re_merged.idx")
	reMergeOutFile, err := os.Create(reMergeOut)
	require.NoError(t, err)
	_, err = mergeFilesTo(t, context.Background(), []string{streamOut, pathA}, reMergeOutFile, mergeCfg)
	require.NoError(t, err)
	require.NoError(t, reMergeOutFile.Close())

	rr, err := OpenIndexFile(reMergeOut)
	require.NoError(t, err)
	defer rr.Close()

	// Document count should stay the same (all time buckets already present).
	require.Equal(t, uint32(expectedUniqueDocs), rr.Header().DocumentCount, "re-merge: document count")
	// Term count unchanged (no new terms introduced).
	require.Equal(t, uint64(expectedTermCount), rr.Header().TermCount, "re-merge: term count")

	// Bitmaps after re-merge should be identical (idempotent merge).
	reIt, err := rr.NewTermIterator()
	require.NoError(t, err)
	origIt, err := rs.NewTermIterator()
	require.NoError(t, err)
	for reIt.Next() {
		require.True(t, origIt.Next())
		require.Equal(t, reIt.Term(), origIt.Term(), "re-merge: term key mismatch")
		reItBm, origItBm := reIt.Bitmap(), origIt.Bitmap()
		require.Equal(t, reItBm.MatchesAll, origItBm.MatchesAll, "re-merge: sentinel mismatch for term %x", reIt.Term())
		if !reItBm.MatchesAll {
			require.True(t, reItBm.Roaring.Equals(origItBm.Roaring),
				"re-merge: bitmap mismatch for term %x", reIt.Term())
		}
	}
	require.False(t, origIt.Next())
	require.NoError(t, reIt.Err())
	require.NoError(t, origIt.Err())
}
