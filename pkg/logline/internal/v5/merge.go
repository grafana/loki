package v5

import (
	"context"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// mergeWriter is the interface for writing merged index output.
// StreamingIndexWriter satisfies it.
type mergeWriter interface {
	WriteTermBitmap(term [8]byte, bm format.Bitmap) error
	WriteTermDocIDs(term [8]byte, docIDs []uint32, cardinality int) error
	Close() error
}

// StreamingMergeIndexReaders merges indexes accessed via io.ReaderAt into a
// single output stream using memory proportional to term count rather than
// bitmap size. At least 2 readers are required, and they must share one
// document layout: their document IDs are cells of the same day, so merged
// postings are the union of the inputs' with no remapping.
// The context is checked periodically during the k-way merge so that
// long-running merges can be cancelled (e.g. on worker shutdown).
// The caller owns out and is responsible for closing and flushing it.
// Returns the HeaderInfo describing the written index.
func StreamingMergeIndexReaders(ctx context.Context, readers []io.ReaderAt, sizes []int64, out io.Writer, cfg IndexWriteConfig) (format.HeaderInfo, error) {
	if len(readers) < 2 {
		return format.HeaderInfo{}, fmt.Errorf("streaming merge requires at least 2 readers, got %d", len(readers))
	}
	var sw *StreamingIndexWriter
	err := doMergeFromReaders(ctx, readers, sizes, func(layout documentLayout) (mergeWriter, error) {
		wcfg, err := cfg.withLayout(layout)
		if err != nil {
			return nil, err
		}
		w, err := newStreamingIndexWriterTo(out, wcfg)
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

// doMergeFromReaders opens inputs from readers, checks they share one document
// layout, creates a writer via newWriter, runs the k-way merge, and closes the
// writer.
func doMergeFromReaders(ctx context.Context, readers []io.ReaderAt, sizes []int64, newWriter func(layout documentLayout) (mergeWriter, error)) error {
	idxReaders, iterators, err := openMergeInputsFromReaders(readers, sizes)
	if err != nil {
		return err
	}
	defer func() {
		for _, r := range idxReaders {
			_ = r.Close()
		}
	}()

	layout, err := commonDocumentLayout(idxReaders)
	if err != nil {
		return err
	}

	w, err := newWriter(layout)
	if err != nil {
		return err
	}

	if err := mergeIndexes(ctx, w, iterators); err != nil {
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

// commonDocumentLayout returns the document layout shared by every input.
// Inputs with different intervals, shard bits, or days number their documents
// differently, so they cannot be merged.
func commonDocumentLayout(readers []*IndexReader) (documentLayout, error) {
	want := readers[0].header.documentLayout()
	for i, r := range readers[1:] {
		if got := r.header.documentLayout(); got != want {
			return documentLayout{}, fmt.Errorf(
				"merge: input %d has document interval %v, %d document shard bits, and day start %s; input 0 has %v, %d, and %s",
				i+1, got.interval, got.shardBits, got.dayStart().Format(time.RFC3339),
				want.interval, want.shardBits, want.dayStart().Format(time.RFC3339),
			)
		}
	}
	return want, nil
}

// mergeIndexes performs the k-way merge of term iterators into a writer,
// merging terms in sorted order. The context is checked every 1000 terms to
// allow cancellation of long-running merges without measurable overhead on
// the hot path.
//
// Per-term postings avoid a roaring round-trip: each input's doc IDs are
// sorted and unique, so a two-pointer merge-union feeds WriteTermDocIDs.
func mergeIndexes(ctx context.Context, w mergeWriter, iterators []*IndexTermIterator) error {
	active := make([]int, 0, len(iterators))
	for i, it := range iterators {
		if it.Next() {
			active = append(active, i)
		}
	}

	shouldAdvance := make([]bool, len(iterators))
	nextActive := make([]int, 0, len(active))

	// Reusable scratch for successive unions.
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

		// Union the doc IDs for this term. MatchesAll is absorbing: once any
		// source has a sentinel, the output is a sentinel.
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
			if first {
				unionA = append(unionA[:0], ids...)
				first = false
				continue
			}
			unionB = mergeUnionSorted(unionB[:0], unionA, ids)
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
