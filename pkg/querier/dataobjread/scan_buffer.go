package dataobjread

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/memory"
)

// poisonOnPut makes a bufferPool overwrite the message and metadata bytes of a batch when the
// buffer goes back to the pool. A test sets it to catch a caller that keeps a line past the next
// call to [LogReader.Next]. The kept bytes then read as 0xFF.
//
// Set it before a reader starts. Reset it after the reader closes, because scans and the consumer
// both return buffers until then.
var poisonOnPut atomic.Bool

// streamRun is a run of consecutive rows of a batch that belong to one stream.
type streamRun struct {
	// end is the index of the row after the last row of the run.
	end int

	stream objectStream
}

// scanBuffer carries one read of a logs section from a scan goroutine to the consumer.
//
// A columnar read leaves its rows as arrays in batch. The arrays use the memory of alloc, so they
// stay valid until the bufferPool takes the buffer back. A row read leaves its rows in records.
//
// A buffer is in one mode at a time. Use setBatch and setRecords to fill it, because they keep
// rows, batch, runs and records consistent.
type scanBuffer struct {
	alloc memory.Allocator

	// rows is the number of rows in the buffer. A scan never sends a buffer without rows.
	rows int

	// batch, runs and needLine describe a columnar read. batch is nil for a row read.
	batch *logs.Batch

	// runs covers every row of batch, in order.
	runs []streamRun

	// needLine reports whether the consumer should expose the message column as the line.
	needLine bool

	// records holds the rows of a row read. It is nil for a columnar read.
	records []LogRecord

	// highWater is the most memory in bytes that one read of this buffer has used.
	highWater int

	// owned is true while a scan or the consumer holds the buffer, and false while it is free in
	// the pool. The channel hand-offs order every access, so it needs no lock.
	owned bool
}

// setBatch fills the buffer with a columnar read and drops any row read.
func (b *scanBuffer) setBatch(batch *logs.Batch, runs []streamRun, needLine bool) {
	b.records = nil
	b.batch, b.runs, b.needLine, b.rows = batch, runs, needLine, batch.Rows
}

// setRecords fills the buffer with a row read and drops any columnar read.
func (b *scanBuffer) setRecords(records []LogRecord) {
	b.batch = nil
	clear(b.runs[:cap(b.runs)])
	b.runs = b.runs[:0]
	b.records, b.rows = records, len(records)
}

// poison overwrites the message and metadata bytes of the batch.
func (b *scanBuffer) poison() {
	if b.batch == nil {
		return
	}
	if b.batch.Message != nil {
		fill(b.batch.Message.Data(), 0xFF)
	}
	for _, column := range b.batch.Metadata {
		fill(column.Values.Data(), 0xFF)
	}
}

func fill(data []byte, value byte) {
	for i := range data {
		data[i] = value
	}
}

// clear drops what the buffer points to, so a view that outlives its use fails loudly.
func (b *scanBuffer) clear() {
	b.rows = 0
	b.batch = nil
	b.records = nil

	// The runs hold label sets. An idle buffer must not pin them, including the ones that a failed
	// read appended past the length.
	clear(b.runs[:cap(b.runs)])
	b.runs = b.runs[:0]
}

// reclaim makes the memory of the last read available for the next one.
//
// It first drops the regions that the last read left unused, but only when the allocator holds
// more than twice the most memory that one read ever used. The allocator is first-fit, so without
// the trim it keeps one region for every new maximum size it served.
func (b *scanBuffer) reclaim() {
	allocated := b.alloc.AllocatedBytes()
	b.highWater = max(b.highWater, allocated-b.alloc.FreeBytes())
	if allocated > 2*b.highWater {
		b.alloc.Trim()
	}
	b.alloc.Reclaim()
}

// bufferPool is the fixed set of scanBuffers of one LogReader.
//
// A columnar scan takes a buffer before each read. A row scan takes one when it sends the records
// it built. The consumer gives the buffer back when it moves past the batch. The pool therefore
// bounds the number of batches in flight, and it reuses the allocator of each buffer for the next
// read.
//
// A scan holds no buffer while it waits for one, and the consumer gives its buffer back before it
// waits for the next batch. The pool cannot deadlock for any size of one or more.
type bufferPool struct {
	free chan *scanBuffer

	// all lists every buffer, free or not, so release can free their memory.
	all []*scanBuffer

	releaseOnce sync.Once
}

// newBufferPool returns a pool of size buffers. A buffer owns no memory until a scan uses it.
func newBufferPool(size int) *bufferPool {
	size = max(size, 1)
	pool := &bufferPool{
		free: make(chan *scanBuffer, size),
		all:  make([]*scanBuffer, size),
	}
	for i := range pool.all {
		pool.all[i] = new(scanBuffer)
		pool.free <- pool.all[i]
	}
	return pool
}

// get returns a free buffer, or the error of ctx if ctx ends first. The allocator of the buffer is
// ready for a new read.
func (p *bufferPool) get(ctx context.Context) (*scanBuffer, error) {
	select {
	case buf := <-p.free:
		buf.reclaim()
		buf.owned = true
		return buf, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// put clears buf and makes it free. The pool is as large as the number of buffers, so put never
// blocks.
//
// put panics when buf is not in use. Two holders of one buffer would reclaim each other's batch
// without any other signal.
func (p *bufferPool) put(buf *scanBuffer) {
	if !buf.owned {
		panic("dataobjread: buffer returned to the pool while it was free")
	}
	buf.owned = false

	if poisonOnPut.Load() {
		buf.poison()
	}
	buf.clear()
	p.free <- buf
}

// release frees the memory of every buffer. Only the first call has an effect.
//
// Call it only after every scan has stopped, because an allocator panics on concurrent use.
//
// The allocators have no parent, so Free hands their regions to the garbage collector and never
// reuses them. A batch that the consumer still holds stays valid.
func (p *bufferPool) release() {
	p.releaseOnce.Do(func() {
		for _, buf := range p.all {
			buf.alloc.Free()
		}
	})
}
