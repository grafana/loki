package dataobjread

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/util/labelpool"
)

// A row is one record of a rowsBatch, as a value. A [rowsBatchIterator] builds it.
//
// The line is a view into the memory of the batch. It stays valid until the iterator closes. A
// consumer that needs it for longer must copy it. The metadata owns its data.
type row struct {
	// timestamp is the timestamp of the record, in Unix nanoseconds.
	timestamp int64

	// line is the log line of the record. It is nil when the batch does not return lines, and for a
	// null or empty message. See [rowsBatch.line].
	line []byte

	// metadata holds the structured metadata of the record. It is empty when the batch has no
	// metadata column.
	metadata labels.Labels

	// stream is the stream of the run that holds the record.
	stream objectStream

	// startsRun is true for the first row of each run of rows of one stream. A consumer that does
	// work once for each run can do it on these rows only.
	startsRun bool
}

// A rowsBatchIterator iterates over the rows of one [rowsBatch], in record order.
//
// It owns the batch. Close releases the batch. After Close, Next returns false and At returns the
// zero row. The zero value owns no batch, so it behaves the same.
//
// A rowsBatchIterator is for a single goroutine. Store the value that newRowsBatchIterator returns,
// and do not copy it again, because a copy would share the ownership of the batch.
type rowsBatchIterator struct {
	batch *rowsBatch

	// scratch builds the structured metadata labels of a row. The iterator takes it from the pool
	// when the batch has metadata, and gives it back in Close. It is nil otherwise.
	scratch *labels.ScratchBuilder

	// runIdx is the index of the current run in batch.streamRuns, next is the next record to read,
	// and runEnd is the end of the current run.
	runIdx       int
	next, runEnd int

	// hasLine and hasMetadata copy the flags of the batch, so Next reads them without going through
	// the batch. When one is false, Next leaves that field of curr at its zero value instead of
	// writing it for every row.
	hasLine, hasMetadata bool

	curr row
}

// newRowsBatchIterator returns an iterator over the rows of batch, and takes ownership of the
// batch. The caller must call Close.
func newRowsBatchIterator(batch *rowsBatch) rowsBatchIterator {
	it := rowsBatchIterator{
		batch:       batch,
		runIdx:      -1,
		hasLine:     batch.hasLine,
		hasMetadata: batch.hasMetadata,
	}
	if it.hasMetadata {
		it.scratch = labelpool.Get()
	}
	return it
}

// Next moves to the next row. It returns false after the last row, and after Close.
func (it *rowsBatchIterator) Next() bool {
	if it.batch == nil {
		return false
	}

	it.curr.startsRun = it.next >= it.runEnd
	if it.curr.startsRun {
		if it.runIdx+1 >= len(it.batch.streamRuns) {
			it.curr = row{}
			return false
		}
		it.runIdx++
		run := it.batch.streamRuns[it.runIdx]
		it.curr.stream = run.stream
		it.next, it.runEnd = run.start, run.end
	}

	i := it.next
	it.next++
	it.curr.timestamp = it.batch.records.Timestamp.Get(i)
	if it.hasLine {
		it.curr.line = it.batch.line(i)
	}
	if it.hasMetadata {
		it.curr.metadata = it.batch.records.MetadataLabels(i, it.scratch)
	}
	return true
}

// At returns the current row. The row is the zero row before the first Next, after the last row,
// and after Close.
//
// At returns a pointer so that a read of one row copies nothing. The row is valid until the next
// call of Next or Close. The caller must not modify it.
func (it *rowsBatchIterator) At() *row { return &it.curr }

// Close releases the batch, and gives the scratch builder back to the pool. A second Close does
// nothing.
//
// Close also zeroes the current row. Metadata that the caller kept stays valid, because the labels
// own their data. A line that the caller kept does not.
func (it *rowsBatchIterator) Close() {
	if it.batch == nil {
		return
	}
	it.batch.release()
	if it.scratch != nil {
		labelpool.Put(it.scratch)
	}
	it.batch, it.scratch = nil, nil
	it.curr = row{}
}
