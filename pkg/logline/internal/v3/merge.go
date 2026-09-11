package v3

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// mergeWriter is the common interface for writing merged index output.
// Both IndexWriter and StreamingIndexWriter satisfy this.
type mergeWriter interface {
	AddDocuments(docs []format.DocumentMetadata)
	WriteTermBitmap(term [8]byte, bm format.Bitmap) error
	WriteTermDocIDs(term [8]byte, docIDs []uint32, cardinality int) error
	Close() error
}

// StreamingMergeIndexReaders merges indexes accessed via io.ReaderAt into a
// single output stream using constant memory proportional to document and
// term count rather than bitmap size. At least 2 readers are required.
// The context is checked periodically during the k-way merge so that
// long-running merges can be cancelled (e.g. on worker shutdown).
// The caller owns out and is responsible for closing and flushing it.
// Returns the HeaderInfo describing the written index.
func StreamingMergeIndexReaders(ctx context.Context, readers []io.ReaderAt, sizes []int64, out io.Writer, cfg IndexWriteConfig) (format.HeaderInfo, error) {
	if len(readers) < 2 {
		return format.HeaderInfo{}, fmt.Errorf("streaming merge requires at least 2 readers, got %d", len(readers))
	}
	var sw *StreamingIndexWriter
	err := doMergeFromReaders(ctx, readers, sizes, func(docCount uint32) (mergeWriter, error) {
		w, err := newStreamingIndexWriterTo(out, cfg, docCount)
		if err != nil {
			return nil, err
		}
		sw = w
		return w, nil
	})
	if err != nil {
		return format.HeaderInfo{}, err
	}
	return sw.Info(), nil
}

// doMergeFromReaders opens inputs from readers, deduplicates documents, creates
// a writer via newWriter, runs the k-way merge, and closes the writer.
func doMergeFromReaders(ctx context.Context, readers []io.ReaderAt, sizes []int64, newWriter func(docCount uint32) (mergeWriter, error)) error {
	idxReaders, iterators, err := openMergeInputsFromReaders(readers, sizes)
	if err != nil {
		return err
	}
	defer func() {
		for _, r := range idxReaders {
			_ = r.Close()
		}
	}()

	allDocs, remaps := deduplicateDocuments(idxReaders)

	w, err := newWriter(uint32(len(allDocs)))
	if err != nil {
		return err
	}

	if err := mergeIndexes(ctx, w, allDocs, iterators, remaps); err != nil {
		return err
	}
	return w.Close()
}

// openMergeInputsFromReaders opens index readers and creates term iterators for each.
// On error, all previously opened readers are closed.
func openMergeInputsFromReaders(readers []io.ReaderAt, sizes []int64) ([]*IndexReader, []*IndexTermIterator, error) {
	if len(readers) != len(sizes) {
		return nil, nil, fmt.Errorf("readers and sizes length mismatch: %d vs %d", len(readers), len(sizes))
	}
	idxReaders := make([]*IndexReader, len(readers))
	iterators := make([]*IndexTermIterator, len(readers))

	for i, r := range readers {
		ir, err := OpenIndexAt(r, 0, sizes[i])
		if err != nil {
			for j := range i {
				idxReaders[j].Close()
			}
			return nil, nil, fmt.Errorf("open input index %d: %w", i, err)
		}
		idxReaders[i] = ir

		it, err := ir.newTermIterator()
		if err != nil {
			for j := 0; j <= i; j++ {
				idxReaders[j].Close()
			}
			return nil, nil, fmt.Errorf("create term iterator for input %d: %w", i, err)
		}
		iterators[i] = it
	}
	return idxReaders, iterators, nil
}

// deduplicateDocuments builds a unified document table from multiple readers.
// Documents with identical time bounds are collapsed into a single ID.
// Returns the deduplicated document list and per-source doc ID remap arrays.
func deduplicateDocuments(readers []*IndexReader) ([]format.DocumentMetadata, [][]uint32) {
	uniqueDocs := make(map[docTimeKey]uint32)
	var nextID uint32

	remaps := make([][]uint32, len(readers))
	for i, r := range readers {
		docs := r.Documents()

		var maxID uint32
		for _, doc := range docs {
			if doc.ID > maxID {
				maxID = doc.ID
			}
		}
		remaps[i] = make([]uint32, maxID+1)

		for _, doc := range docs {
			key := docTimeKey{doc.MinTimeUnix, doc.MaxTimeUnix}
			newID, exists := uniqueDocs[key]
			if !exists {
				newID = nextID
				nextID++
				uniqueDocs[key] = newID
			}
			remaps[i][doc.ID] = newID
		}
	}

	allDocs := make([]format.DocumentMetadata, nextID)
	for key, id := range uniqueDocs {
		allDocs[id] = format.DocumentMetadata{
			ID:          id,
			MinTimeUnix: key.minTimeUnix,
			MaxTimeUnix: key.maxTimeUnix,
		}
	}
	return allDocs, remaps
}

// mergeIndexes performs the k-way merge of term iterators into a writer.
// Adds documents, then merges terms in sorted order with doc ID remapping.
// The context is checked every 1000 terms to allow cancellation of
// long-running merges without measurable overhead on the hot path.
//
// Per-term postings avoid a roaring round-trip: remap → sort/unique →
// two-pointer merge-union → WriteTermDocIDs.
func mergeIndexes(ctx context.Context, w mergeWriter, allDocs []format.DocumentMetadata, iterators []*IndexTermIterator, remaps [][]uint32) error {
	w.AddDocuments(allDocs)

	active := make([]int, 0, len(iterators))
	for i, it := range iterators {
		if it.Next() {
			active = append(active, i)
		}
	}

	shouldAdvance := make([]bool, len(iterators))
	nextActive := make([]int, 0, len(active))

	// Reusable scratch for remap/sort and successive unions.
	var remapBuf []uint32
	var unionA, unionB []uint32

	var termCount int
	for len(active) > 0 {
		if termCount%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		termCount++
		// Only the term is needed below, not the iterator that owns it.
		minTerm := iterators[active[0]].Term()
		for _, idx := range active[1:] {
			if compareTerm8(iterators[idx].Term(), minTerm) < 0 {
				minTerm = iterators[idx].Term()
			}
		}

		// Union remapped doc IDs for this term. MatchesAll is absorbing: once
		// any source has a sentinel, the output is a sentinel.
		matchesAll := false
		unionA = unionA[:0]
		first := true
		for _, idx := range active {
			if iterators[idx].Term() != minTerm {
				continue
			}
			shouldAdvance[idx] = true
			if matchesAll {
				continue
			}
			ids, sourceMatchesAll := iterators[idx].DocIDs()
			if sourceMatchesAll {
				matchesAll = true
				continue
			}
			remapBuf = remapSortUnique(remapBuf[:0], ids, remaps[idx])
			if first {
				unionA = append(unionA[:0], remapBuf...)
				first = false
				continue
			}
			unionB = mergeUnionSorted(unionB[:0], unionA, remapBuf)
			unionA, unionB = unionB, unionA
		}

		if matchesAll {
			if err := w.WriteTermDocIDs(minTerm, nil, 0); err != nil {
				return err
			}
		} else {
			if err := w.WriteTermDocIDs(minTerm, unionA, len(unionA)); err != nil {
				return err
			}
		}

		nextActive = nextActive[:0]
		for _, idx := range active {
			if shouldAdvance[idx] {
				shouldAdvance[idx] = false
				if iterators[idx].Next() {
					nextActive = append(nextActive, idx)
				}
			} else {
				nextActive = append(nextActive, idx)
			}
		}
		active, nextActive = nextActive, active
	}

	for i, it := range iterators {
		if itErr := it.Err(); itErr != nil {
			return fmt.Errorf("iterator %d: %w", i, itErr)
		}
	}
	return nil
}

// remapSortUnique remaps src doc IDs through remap, then sorts and uniques
// into dst (reusing dst capacity). Remapping is not order-preserving.
func remapSortUnique(dst, src []uint32, remap []uint32) []uint32 {
	dst = slices.Grow(dst[:0], len(src))
	for _, id := range src {
		dst = append(dst, remap[id])
	}
	slices.Sort(dst)
	return dedupeSorted(dst)
}

// dedupeSorted removes adjacent duplicates from a sorted slice in place.
func dedupeSorted(ids []uint32) []uint32 {
	if len(ids) < 2 {
		return ids
	}
	w := 1
	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[w-1] {
			ids[w] = ids[i]
			w++
		}
	}
	return ids[:w]
}

// mergeUnionSorted appends the sorted union of a and b into dst (reusing dst
// capacity). a and b must be sorted and unique.
func mergeUnionSorted(dst, a, b []uint32) []uint32 {
	dst = slices.Grow(dst[:0], len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			dst = append(dst, a[i])
			i++
		case a[i] > b[j]:
			dst = append(dst, b[j])
			j++
		default:
			dst = append(dst, a[i])
			i++
			j++
		}
	}
	dst = append(dst, a[i:]...)
	dst = append(dst, b[j:]...)
	return dst
}
