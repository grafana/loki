package dataobjread

import (
	"flag"
	"fmt"
	"sync"
	"testing"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/memory"
)

// A rowsBatch holds the records that one read of a logs section returned, as arrays, and splits
// them into runs of consecutive records of one stream. A run carries the stream of its records, so
// the stream is known once for each run, not once for each record.
//
// The batch owns an Allocator, and the arrays use memory from it. The batch copies nothing, so a
// line is a view into that memory. The arrays, and every slice that points into them, stay valid
// until release. release reclaims the memory and returns the batch to its pool, and the next read
// into the batch reuses the memory. A value that must outlive release needs a copy.
//
// A batch has one owner at a time. A rowsBatch is not safe for concurrent use. It must not be
// copied, because it owns an Allocator.
type rowsBatch struct {
	// records holds the arrays. They use memory from alloc. After a successful init, records has a
	// timestamp array and a stream ID array.
	records *logs.RecordBatch

	// streamRuns split the records by stream, in record order. After a successful init, each run
	// holds at least one record, and together the runs cover every record.
	streamRuns []streamRun

	// hasLine is true when the task needs the line and the records have a message array. When it is
	// false, line returns nil.
	hasLine bool

	// hasMetadata is true when the records have at least one structured metadata column.
	hasMetadata bool

	// alloc owns the memory of records. The read allocates the arrays from it, and reset reclaims
	// the memory.
	alloc memory.Allocator

	pool *rowsBatchPool

	// pooled is true from a put until the next get.
	pooled bool
}

// A streamRun is the range [start, end) of consecutive records of a batch that belong to one
// stream.
type streamRun struct {
	stream     objectStream
	start, end int
}

// init stores the records that a read into b.alloc returned, and splits them into stream runs, each
// with the identity of its stream. The batch must come from the pool, with no records. init
// panics when the batch already holds records or runs, or sits in the pool.
//
// init fails when the records have no stream ID array or no timestamp array, because the loop and
// the iterator read them without a nil check. It also fails on a record whose stream the task did
// not plan to read. The read matches only the task's streams, so an unexpected ID means that the
// read broke its stream filter. Dropping the record would under-count the query without saying so.
//
// On error the batch holds no records, and it can hold some of the runs. The caller must release
// the batch, which drops them.
func (b *rowsBatch) init(task ReadTask, records *logs.RecordBatch) error {
	if b.pooled || b.records != nil || len(b.streamRuns) > 0 {
		panic("dataobjread: batch initialised twice")
	}
	if records.StreamID == nil || records.Timestamp == nil {
		return fmt.Errorf("data object %q logs section %d returned a batch without the stream ID or timestamp column", task.objectPath, task.sectionIdx)
	}

	// The layouts that the production builder writes keep the records of one stream together, so a
	// batch holds few runs, and the stream lookup runs once for each run. An interleaved layout gives
	// up to one run for each record. That costs time but stays correct.
	var lastStreamID int64
	for i := range records.Rows {
		streamID := records.StreamID.Get(i)
		if len(b.streamRuns) > 0 && streamID == lastStreamID {
			continue
		}

		stream, ok := task.streams.admittedByID[streamID]
		if !ok {
			return fmt.Errorf("data object %q logs section %d returned unexpected stream ID %d", task.objectPath, task.sectionIdx, streamID)
		}
		if len(b.streamRuns) > 0 {
			b.streamRuns[len(b.streamRuns)-1].end = i
		}
		b.streamRuns = append(b.streamRuns, streamRun{stream: stream, start: i})
		lastStreamID = streamID
	}
	if len(b.streamRuns) > 0 {
		b.streamRuns[len(b.streamRuns)-1].end = records.Rows
	}

	b.records = records
	b.hasLine = task.needReturningLine && records.Message != nil
	b.hasMetadata = len(records.Metadata) > 0
	return nil
}

// line returns the line of record i for the extractor. It returns nil when the batch has no line. It
// also returns nil for a null or empty message.
//
// The line is a view into the memory of the batch. Its capacity ends at its length, so a stage
// that appends to the line cannot overwrite the line of the next record.
func (b *rowsBatch) line(i int) []byte {
	if !b.hasLine {
		return nil
	}
	value := b.records.Message.Get(i)
	if len(value) == 0 {
		return nil
	}
	return value[:len(value):len(value)]
}

// reset drops the records and reclaims the memory of the batch for the next read into it. The batch
// keeps the regions that it allocated.
func (b *rowsBatch) reset() {
	// Reclaim keeps every region. Trim would free them, and the next read would allocate them again.
	b.alloc.Reclaim()
	b.records = nil
	b.hasLine = false
	b.hasMetadata = false
	// The runs hold the labels of a stream. Clear them, including the runs that a failed init left.
	clear(b.streamRuns)
	b.streamRuns = b.streamRuns[:0]
}

// release reclaims the memory of the batch and returns the batch to its pool. The consumer must not
// use the batch, its arrays, or any line of it afterwards.
//
// release panics when the batch is already in the pool, because two owners released it. The check
// runs before release touches the memory, so the second call changes nothing. It cannot catch a
// second release after the pool hands the batch out again. That case rests on the rule that a scan
// either sends a batch or releases it, never both, and the consumer releases each batch that it
// receives once.
func (b *rowsBatch) release() {
	if b.pooled {
		panic("dataobjread: batch released twice")
	}
	b.reset()
	b.pool.put(b)
}

// rowsBatchPool recycles batches between the scans and the consumer, so a scan reads into memory
// that an earlier scan allocated.
//
// Each [LogReader] owns its pool. A GC frees the batches that stay idle in the pool. So the pool
// gives memory back under pressure, and it keeps nothing alive after the reader is gone.
//
// rowsBatchPool is safe for concurrent use.
type rowsBatchPool struct {
	free sync.Pool

	// poisonReleased makes put overwrite the memory of the batch. A consumer that keeps a line past
	// release then reads garbage instead of a line that looks right. See poisonReleasedBatches.
	poisonReleased bool
}

func newRowsBatchPool() *rowsBatchPool {
	p := &rowsBatchPool{poisonReleased: poisonReleasedBatches()}
	p.free.New = func() any { return &rowsBatch{pool: p} }
	return p
}

// get returns a batch with no records.
func (p *rowsBatchPool) get() *rowsBatch {
	b := p.free.Get().(*rowsBatch)
	b.pooled = false
	return b
}

// put returns b to the pool. The caller must have reset b, and must not use b or its arrays after
// put.
func (p *rowsBatchPool) put(b *rowsBatch) {
	if p.poisonReleased {
		poisonFreeMemory(&b.alloc)
	}
	b.pooled = true
	p.free.Put(b)
}

// poisonReleasedBatches reports whether a pool overwrites the memory of the batches that it gets
// back.
//
// It does in a test binary, so a test that keeps a line past release reads garbage. It does not
// when the binary runs benchmarks, because the overwrite takes time in proportion to the memory of
// each batch, and that would distort the timings.
func poisonReleasedBatches() bool {
	if !testing.Testing() {
		return false
	}
	benchmarks := flag.Lookup("test.bench")
	return benchmarks == nil || benchmarks.Value.String() == ""
}

// poisonFreeMemory overwrites every free region of alloc, then makes the regions free again.
func poisonFreeMemory(alloc *memory.Allocator) {
	const poison = 0xA5

	for alloc.FreeBytes() > 0 {
		region := alloc.Allocate(1).Data()
		for i := range region {
			region[i] = poison
		}
	}
	alloc.Reclaim()
}
