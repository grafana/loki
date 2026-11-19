package tsdb

import (
	"container/heap"
	"context"
	"errors"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
)

type chunkCursorSource interface {
	openChunkCursors(context.Context, string, model.Time, model.Time, index.FingerprintFilter, ...*labels.Matcher) ([]sharding.ChunkCursor, error)
}

func openChunkCursors(ctx context.Context, idx Index, tenant string, from, through model.Time, filter index.FingerprintFilter, matchers ...*labels.Matcher) ([]sharding.ChunkCursor, error) {
	source, ok := idx.(chunkCursorSource)
	if !ok {
		return nil, sharding.ErrChunkCursorsUnsupported
	}
	return source.openChunkCursors(ctx, tenant, from, through, filter, matchers...)
}

func (f *TSDBFile) openChunkCursors(ctx context.Context, tenant string, from, through model.Time, filter index.FingerprintFilter, matchers ...*labels.Matcher) ([]sharding.ChunkCursor, error) {
	tsdb, ok := f.Index.(*TSDBIndex)
	if !ok {
		return nil, sharding.ErrChunkCursorsUnsupported
	}
	switch tsdb.reader.(type) {
	case *index.ByteSliceReader, *index.StreamReader:
	default:
		return nil, sharding.ErrChunkCursorsUnsupported
	}

	f.leaseMu.Lock()
	if f.closed {
		f.leaseMu.Unlock()
		return nil, sharding.ErrChunkCursorsUnsupported
	}
	f.leases++
	f.leaseMu.Unlock()

	var scan *index.OrderedChunkScan
	if streaming, ok := tsdb.reader.(*index.StreamReader); ok {
		// Keep a file handle open until the cursor closes so it can keep
		// reading if the file is removed from the cache.
		streamScan, err := streaming.NewPinnedSeriesScan()
		if err != nil {
			return nil, errors.Join(err, f.releaseCursor())
		}
		scan = streamScan
	} else {
		scan = tsdb.reader.(*index.ByteSliceReader).NewOrderedChunkScan()
	}
	postings, err := tsdb.postings(ctx, filter, matchers)
	if err != nil {
		return nil, errors.Join(err, scan.Close(), f.releaseCursor())
	}
	var filterer chunk.Filterer
	if requestFilter := tsdb.getChunkFilter(); requestFilter != nil {
		filterer = requestFilter.ForRequest(ctx)
	}
	return []sharding.ChunkCursor{&fileChunkCursor{
		ctx: ctx, file: f, tenant: tenant, from: int64(from), through: int64(through),
		postings: postings, scan: scan, fpFilter: filter,
		filterer: filterer, chunks: ChunkMetasPool.Get()[:0],
	}}, nil
}

func (f *TSDBFile) releaseCursor() error {
	f.leaseMu.Lock()
	f.leases--
	closeReader := f.closed && f.leases == 0
	f.leaseMu.Unlock()
	if closeReader {
		return f.Index.Close()
	}
	return nil
}

type fileChunkCursor struct {
	ctx           context.Context
	file          *TSDBFile
	tenant        string
	from, through int64
	postings      index.Postings
	scan          *index.OrderedChunkScan
	fpFilter      index.FingerprintFilter
	filterer      chunk.Filterer
	labels        labels.Labels

	fp      uint64
	chunks  []index.ChunkMeta
	next    int
	lists   chunkListHeap
	current index.ChunkMeta

	lookaheadFP uint64
	hasSeries   bool
	exhausted   bool
	err         error
}

func (c *fileChunkCursor) Next() bool {
	if c.err != nil || c.file == nil {
		return false
	}
	for {
		if len(c.lists) > 0 {
			head := &c.lists[0]
			c.current = head.chunks[0]
			if len(head.chunks) == 1 {
				heap.Pop(&c.lists)
			} else {
				head.chunks = head.chunks[1:]
				heap.Fix(&c.lists, 0)
			}
			return true
		}
		if c.next < len(c.chunks) {
			c.current = c.chunks[c.next]
			c.next++
			return true
		}
		if c.err = c.ctx.Err(); c.err != nil {
			return false
		}
		if !c.hasSeries {
			if c.exhausted || !c.advanceSeries() {
				return false
			}
		}
		if c.err = c.readFingerprint(); c.err != nil {
			return false
		}
	}
}

func (c *fileChunkCursor) advanceSeries() bool {
	c.hasSeries = false
	for {
		if c.err = c.ctx.Err(); c.err != nil {
			return false
		}
		if !c.postings.Next() {
			c.err = c.postings.Err()
			c.exhausted = true
			return false
		}
		fp, err := c.scan.SeriesFingerprint(c.postings.At())
		if err != nil {
			c.err = err
			return false
		}
		if c.fpFilter == nil || c.fpFilter.Match(model.Fingerprint(fp)) {
			c.lookaheadFP, c.hasSeries = fp, true
			return true
		}
	}
}

func (c *fileChunkCursor) readFingerprint() error {
	c.fp = c.lookaheadFP
	c.chunks = c.chunks[:0]
	c.next = 0
	var seriesLabels *labels.Labels
	if c.filterer != nil {
		seriesLabels = &c.labels
	}
	// Series sharing a fingerprint can have overlapping chunk lists. Keep
	// their lists until the fingerprint changes, then merge them in order.
	for {
		var extra []index.ChunkMeta
		dst := &c.chunks
		if len(c.chunks) > 0 {
			dst = &extra
		}
		if _, err := c.scan.Series(c.postings.At(), c.from, c.through, seriesLabels, dst); err != nil {
			return err
		}
		if c.filterer != nil && c.filterer.ShouldFilter(c.labels) {
			*dst = (*dst)[:0]
		}
		if len(extra) > 0 {
			if len(c.lists) == 0 {
				c.lists = append(c.lists, chunkList{chunks: c.chunks})
			}
			c.lists = append(c.lists, chunkList{chunks: extra, order: len(c.lists)})
		}
		if !c.advanceSeries() || c.lookaheadFP != c.fp {
			break
		}
	}
	if len(c.lists) > 0 {
		c.next = len(c.chunks)
		heap.Init(&c.lists)
	}
	return c.err
}

func (c *fileChunkCursor) At() logproto.ChunkRefWithSizingInfo {
	meta := c.current
	return logproto.ChunkRefWithSizingInfo{
		ChunkRef: logproto.ChunkRef{
			Fingerprint: c.fp,
			UserID:      c.tenant,
			From:        meta.From(),
			Through:     meta.Through(),
			Checksum:    meta.Checksum,
		},
		KB:      meta.KB,
		Entries: meta.Entries,
	}
}

func (c *fileChunkCursor) Err() error { return c.err }

func (c *fileChunkCursor) Close() error {
	if c.file == nil {
		return nil
	}
	err := errors.Join(c.scan.Close(), c.file.releaseCursor())
	ChunkMetasPool.Put(c.chunks)
	c.file, c.scan, c.postings, c.chunks = nil, nil, nil, nil
	c.lists = nil
	return err
}

type chunkList struct {
	chunks []index.ChunkMeta
	order  int
}

type chunkListHeap []chunkList

func (h chunkListHeap) Len() int      { return len(h) }
func (h chunkListHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h chunkListHeap) Less(i, j int) bool {
	a, b := h[i].chunks[0], h[j].chunks[0]
	if a.Less(b) {
		return true
	}
	// If two lists contain the same ref, prefer the sizing from the first series.
	return !b.Less(a) && h[i].order < h[j].order
}

func (h *chunkListHeap) Push(x any) { *h = append(*h, x.(chunkList)) }
func (h *chunkListHeap) Pop() any {
	last := len(*h) - 1
	list := (*h)[last]
	(*h)[last] = chunkList{}
	*h = (*h)[:last]
	return list
}
