package executor

import (
	"context"
	"encoding/binary"
	"hash"
	"hash/fnv"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// newDedupPipeline wraps an input pipeline with row-level deduplication.
// Rows are considered duplicates when they share the same (timestamp, message,
// label columns) tuple. The dedup key is hashed with FNV-64a and tracked in a
// set; only the first occurrence of each key is emitted.
//
// This removes duplicate log entries produced by the Merge pipeline when the
// same entry exists in multiple overlapping data object sections. Without this,
// metric aggregations (rate, count_over_time, etc.) over-count entries.
//
// The implementation is optimised for the common case (no duplicates): it runs
// a hash-only pass first and only triggers the expensive filterBatch rebuild
// when at least one duplicate is detected.
func newDedupPipeline(input Pipeline) Pipeline {
	var (
		seen      = make(map[uint64]struct{})
		intra     = make(map[uint64]struct{}) // reused per batch for intra-batch dup detection
		hasher    = fnv.New64a()
		buf       [8]byte
		hashes    []uint64 // scratch space for per-batch hashes, reused across calls
		lblCols   []*array.String
		lastNCols int64 // track schema changes by column count (cheap proxy)
		tsIdx     int
		msgIdx    int
		lblIdxs   []int
	)

	return newGenericPipeline(func(ctx context.Context, inputs []Pipeline) (arrow.RecordBatch, error) {
		batch, err := inputs[0].Read(ctx)
		if err != nil {
			return nil, err
		}
		nRows := int(batch.NumRows())
		if nRows == 0 {
			return batch, nil
		}

		// Re-resolve column indices when the schema changes. Different
		// partition workers can send batches with different schemas.
		if nCols := batch.NumCols(); nCols != lastNCols {
			tsIdx, msgIdx, lblIdxs = resolveDedupColumns(batch)
			lastNCols = nCols
		}

		if tsIdx < 0 || msgIdx < 0 {
			return batch, nil
		}

		tsCol, ok := batch.Column(tsIdx).(*array.Timestamp)
		if !ok {
			return batch, nil
		}
		msgCol, ok := batch.Column(msgIdx).(*array.String)
		if !ok {
			return batch, nil
		}

		lblCols = lblCols[:0]
		canDedup := true
		for _, idx := range lblIdxs {
			col, ok := batch.Column(idx).(*array.String)
			if !ok {
				canDedup = false
				break
			}
			lblCols = append(lblCols, col)
		}
		if !canDedup {
			return batch, nil
		}

		// Compute all row hashes into a reusable scratch slice and check for
		// duplicates in a single pass (both inter-batch via seen, and
		// intra-batch via intra).
		if cap(hashes) < nRows {
			hashes = make([]uint64, nRows)
		} else {
			hashes = hashes[:nRows]
		}
		clear(intra)
		hasDups := false
		for i := range nRows {
			h := rowHash(hasher, &buf, tsCol, msgCol, lblCols, i)
			hashes[i] = h
			if !hasDups {
				if _, dup := seen[h]; dup {
					hasDups = true
				} else if _, dup := intra[h]; dup {
					hasDups = true
				} else {
					intra[h] = struct{}{}
				}
			}
		}

		// Fast path: no duplicates. Commit all hashes and return the batch as-is.
		if !hasDups {
			for _, h := range hashes {
				seen[h] = struct{}{}
			}
			return batch, nil
		}

		// Slow path: at least one duplicate. Use filterBatch to rebuild without
		// duplicate rows, using the pre-computed hashes.
		return filterBatch(batch, func(i int) bool {
			h := hashes[i]
			if _, dup := seen[h]; dup {
				return false
			}
			seen[h] = struct{}{}
			return true
		}), nil
	}, input)
}

// resolveDedupColumns finds the column indices for the timestamp, message, and
// label columns in the batch schema. Returns -1 for tsIdx/msgIdx if not found.
func resolveDedupColumns(batch arrow.RecordBatch) (tsIdx, msgIdx int, lblIdxs []int) {
	tsIdx, msgIdx = -1, -1
	for i, field := range batch.Schema().Fields() {
		ident, err := semconv.ParseFQN(field.Name)
		if err != nil {
			continue
		}
		switch {
		case ident.Equal(semconv.ColumnIdentTimestamp):
			tsIdx = i
		case ident.Equal(semconv.ColumnIdentMessage):
			msgIdx = i
		case ident.ColumnType() == types.ColumnTypeLabel:
			lblIdxs = append(lblIdxs, i)
		}
	}
	return tsIdx, msgIdx, lblIdxs
}

// rowHash computes an FNV-64a hash over the dedup key columns for a single row.
// The hasher and buf are caller-owned and reused across rows to avoid allocations.
func rowHash(h hash.Hash64, buf *[8]byte, tsCol *array.Timestamp, msgCol *array.String, lblCols []*array.String, row int) uint64 {
	h.Reset()

	binary.LittleEndian.PutUint64(buf[:], uint64(tsCol.Value(row)))
	h.Write(buf[:]) //nolint:errcheck

	msg := msgCol.Value(row)
	h.Write(unsafe.Slice(unsafe.StringData(msg), len(msg))) //nolint:errcheck

	for _, col := range lblCols {
		h.Write(buf[:1]) //nolint:errcheck
		v := col.Value(row)
		h.Write(unsafe.Slice(unsafe.StringData(v), len(v))) //nolint:errcheck
	}

	return h.Sum64()
}
