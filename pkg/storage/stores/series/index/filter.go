package index

import "bytes"

// QueryFilter wraps a callback to ensure the results are filtered correctly;
// useful for the cache and Bigtable backend, which only ever fetches the whole
// row.
func QueryFilter(callback QueryPagesCallback) QueryPagesCallback {
	return func(query Query, batch ReadBatchResult) bool {
		return callback(query, &filteringBatch{
			query:           query,
			ReadBatchResult: batch,
		})
	}
}

type filteringBatch struct {
	query Query
	ReadBatchResult
}

func (f filteringBatch) Iterator() ReadBatchIterator {
	return &filteringBatchIter{
		query:             f.query,
		ReadBatchIterator: f.ReadBatchResult.Iterator(),
	}
}

type filteringBatchIter struct {
	query Query
	ReadBatchIterator
}

func (f *filteringBatchIter) Next() bool {
	for f.ReadBatchIterator.Next() {
		rangeValue, value := f.ReadBatchIterator.RangeValue(), f.ReadBatchIterator.Value()

		if len(f.query.RangeValuePrefix) != 0 && !bytes.HasPrefix(rangeValue, f.query.RangeValuePrefix) {
			continue
		}
		if len(f.query.RangeValueStart) != 0 && bytes.Compare(f.query.RangeValueStart, rangeValue) > 0 {
			continue
		}
		if len(f.query.ValueEqual) != 0 && !bytes.Equal(value, f.query.ValueEqual) {
			continue
		}

		return true
	}

	return false
}
