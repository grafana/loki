package builder

import (
	"container/heap"
	"encoding/binary"
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"time"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
)

// mergedFile identifies one .lidx produced by the merge and the (date, shard)
// it covers, so the builder can wrap it in a fileInfo for upload. terms/docs
// carry the file's cardinality for the output metrics.
type mergedFile struct {
	path  string
	date  string
	shard int
	day   uint64
	terms int
	docs  int
}

// dateMapper derives dates and per-document time ranges from epoch ticks. All
// math is in absolute buckets (baseBucket + tick): a document's time range is
// absolute bucket × interval, and the merge assigns dense ranks in ascending
// tick order. One per shard merge (its dateCache is not safe for concurrent
// use).
type dateMapper struct {
	intervalNanos int64
	ticksPerDay   uint64
	baseBucket    uint64
	dateCache     map[uint64]string
}

func (b *postingsBuffer) newDateMapper() *dateMapper {
	return &dateMapper{
		intervalNanos: b.intervalNanos,
		ticksPerDay:   b.ticksPerDay,
		baseBucket:    b.baseBucket,
		dateCache:     map[uint64]string{},
	}
}

func (dm *dateMapper) dayIndex(tick uint32) uint64 {
	return (dm.baseBucket + uint64(tick)) / dm.ticksPerDay
}

func (dm *dateMapper) dateOfDay(day uint64) string {
	if s, ok := dm.dateCache[day]; ok {
		return s
	}
	absBucket := day * dm.ticksPerDay
	s := time.Unix(0, int64(absBucket)*dm.intervalNanos).UTC().Format("2006-01-02")
	dm.dateCache[day] = s
	return s
}

// docMeta returns {ID, MinTimeUnix, MaxTimeUnix} for an epoch-tick docID: the
// document covers [absolute bucket × interval, (absolute bucket + 1) × interval).
func (dm *dateMapper) docMeta(docID uint32, id uint32) format.DocumentMetadata {
	absBucket := dm.baseBucket + uint64(docID)
	startNanos := int64(absBucket) * dm.intervalNanos
	return format.DocumentMetadata{
		ID:          id,
		MinTimeUnix: time.Unix(0, startNanos).UnixMilli(),
		MaxTimeUnix: time.Unix(0, startNanos+dm.intervalNanos).UnixMilli(),
	}
}

// mergeRuns k-way merges the given runs into per-(date,shard) .lidx files, one
// shard at a time. Runs are shard-contiguous and each shard is an independently
// seekable s2 stream, so each iteration seeks straight to its shard's byte
// range in every run and merges only that shard — a single pass over the data,
// no re-reads — producing a disjoint set of .lidx files. mergeShard is
// self-contained (own readers, writers, dateMapper; refTicks is read-only), so
// merging shards in parallel would be a small, local change if flush wall time
// ever warrants it.
//
// runPaths is a parameter (not b.runPaths) because in extract-pipeline mode the
// merge host receives the UNION of every worker buffer's runs; its refTicks
// must already hold the matching (shard, day) union (see unionRefTicksInto).
func (b *postingsBuffer) mergeRuns(outDir, version string, cfg format.WriterConfig, runPaths []string) ([]mergedFile, error) {
	// The merge streams runs from disk; release the large ingest sort buffers
	// before opening writers so they don't stay pinned through the merge.
	b.releaseSortBuffers()

	nShards := b.shardCount
	if nShards < 1 {
		nShards = 1
	}
	var files []mergedFile
	for s := 0; s < nShards; s++ {
		mf, err := b.mergeShard(outDir, version, cfg, s, runPaths)
		if err != nil {
			// Shards completed before the failure already produced .lidx files
			// that the caller never learns about (only the returned slice is
			// registered for discardIndexes); remove them here so a failed
			// merge leaves nothing on disk that retry accounting can't see.
			for _, f := range files {
				os.Remove(f.path)
			}
			return nil, err
		}
		files = append(files, mf...)
	}
	return files, nil
}

// mergeShard merges a single shard's records from every run into that shard's
// per-date .lidx files.
func (b *postingsBuffer) mergeShard(outDir, version string, cfg format.WriterConfig, shardVal int, runPaths []string) (_ []mergedFile, err error) {
	sm := &shardMerger{
		shard:      shardVal,
		outDir:     outDir,
		version:    version,
		cfg:        cfg,
		dm:         b.newDateMapper(),
		refTicks:   b.refTicks,
		perDay:     b.ticksPerDay,
		baseBkt:    b.baseBucket,
		writers:    map[uint64]logline.Writer{},
		ranks:      map[uint64]map[uint32]uint32{},
		termCounts: map[uint64]int{},
	}

	h := &runHeap{}
	heap.Init(h)
	readers := make([]*runReader, 0, len(runPaths))
	defer func() {
		for _, rr := range readers {
			rr.close()
		}
		// On error, close any writers left open so fds aren't leaked across
		// flush retries, then remove the .lidx files this shard created: the
		// caller never sees sm.files on the error path, so anything left behind
		// would be invisible to discardIndexes and to disk accounting for the
		// whole retry window.
		if err != nil {
			sm.closeAll()
			for _, f := range sm.files {
				os.Remove(f.path)
			}
		}
	}()

	for _, p := range runPaths {
		rr, oerr := openRunShardReader(p, shardVal)
		if oerr != nil {
			return nil, oerr
		}
		readers = append(readers, rr)
		if k, d, ok, nerr := rr.next(); nerr != nil {
			return nil, nerr
		} else if ok {
			heap.Push(h, &runHead{key: k, doc: d, rr: rr})
		}
	}

	var docIDs []uint32
	for h.Len() > 0 {
		term := (*h)[0].key
		docIDs = docIDs[:0]
		var last uint32
		haveLast := false
		for h.Len() > 0 && (*h)[0].key == term {
			it := (*h)[0]
			if !haveLast || it.doc != last {
				docIDs = append(docIDs, it.doc)
				last = it.doc
				haveLast = true
			}
			k, d, ok, nerr := it.rr.next()
			if nerr != nil {
				return nil, nerr
			}
			if ok {
				it.key, it.doc = k, d
				heap.Fix(h, 0)
			} else {
				heap.Pop(h)
			}
		}
		if werr := sm.emitTerm(term, docIDs); werr != nil {
			return nil, werr
		}
	}

	if cerr := sm.closeAll(); cerr != nil {
		return nil, cerr
	}
	// Stamp per-file cardinalities now that every term has been emitted.
	for i := range sm.files {
		sm.files[i].terms = sm.termCounts[sm.files[i].day]
	}
	return sm.files, nil
}

// shardMerger owns one shard's per-date writers during a merge.
type shardMerger struct {
	shard    int
	outDir   string
	version  string
	cfg      format.WriterConfig
	dm       *dateMapper
	refTicks map[refKey][]uint64
	perDay   uint64
	baseBkt  uint64

	writers    map[uint64]logline.Writer    // day -> writer
	ranks      map[uint64]map[uint32]uint32 // day -> (epoch-tick docID -> dense rank)
	termCounts map[uint64]int               // day -> terms written to that day's file
	files      []mergedFile
}

// emitTerm writes one term's globally-sorted, deduped docIDs into this shard's
// writer(s), splitting the run at day boundaries and remapping epoch-tick docIDs to
// dense per-(date,shard) ranks.
func (sm *shardMerger) emitTerm(term [8]byte, docIDs []uint32) error {
	if len(docIDs) == 0 {
		return nil
	}
	start := 0
	curDay := sm.dm.dayIndex(docIDs[0])
	flush := func(day uint64, seg []uint32) error {
		w, ranks, err := sm.writerForDay(day)
		if err != nil {
			return err
		}
		out := make([]uint32, len(seg))
		for i, d := range seg {
			r, ok := ranks[d]
			if !ok {
				return fmt.Errorf("merge: docID %d missing from ranks for shard %d day %d", d, sm.shard, day)
			}
			out[i] = r
		}
		sm.termCounts[day]++
		return w.WriteTermDocIDs(term, out, len(out))
	}
	for i := 1; i < len(docIDs); i++ {
		d := sm.dm.dayIndex(docIDs[i])
		if d != curDay {
			if err := flush(curDay, docIDs[start:i]); err != nil {
				return err
			}
			start = i
			curDay = d
		}
	}
	return flush(curDay, docIDs[start:])
}

// writerForDay lazily creates the (date, shard) writer for a day, seeded with
// the exact documents this shard references in that day (from refTicks), and
// the epoch-tick-docID -> dense-rank map for remapping postings.
func (sm *shardMerger) writerForDay(day uint64) (logline.Writer, map[uint32]uint32, error) {
	if w, ok := sm.writers[day]; ok {
		return w, sm.ranks[day], nil
	}
	bs := sm.refTicks[refKey{shard: sm.shard, day: day}]
	ranks := make(map[uint32]uint32)
	var docs []format.DocumentMetadata
	var rank uint32
	dayStartAbs := day * sm.perDay
	for word := range bs {
		bitsWord := bs[word]
		for bitsWord != 0 {
			tod := uint(word)*64 + uint(bits.TrailingZeros64(bitsWord))
			bitsWord &= bitsWord - 1
			docID := uint32(dayStartAbs + uint64(tod) - sm.baseBkt)
			ranks[docID] = rank
			docs = append(docs, sm.dm.docMeta(docID, rank))
			rank++
		}
	}
	date := sm.dm.dateOfDay(day)
	path := filepath.Join(sm.outDir, fmt.Sprintf("%s_s%d.lidx", date, sm.shard))
	w, err := logline.NewWriter(sm.version, path, docs, &sm.cfg)
	if err != nil {
		return nil, nil, err
	}
	sm.writers[day] = w
	sm.ranks[day] = ranks
	sm.files = append(sm.files, mergedFile{path: path, date: date, shard: sm.shard, day: day, docs: len(docs)})
	return w, ranks, nil
}

func (sm *shardMerger) closeAll() error {
	var firstErr error
	for day, w := range sm.writers {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(sm.writers, day)
	}
	return firstErr
}

// runHead is a min-heap element keyed by (ngram, docID) for a single shard's
// k-way merge.
type runHead struct {
	key [8]byte
	doc uint32
	rr  *runReader
}

type runHeap []*runHead

func (h runHeap) Len() int { return len(h) }
func (h runHeap) Less(i, j int) bool {
	if h[i].key != h[j].key {
		return binary.BigEndian.Uint64(h[i].key[:]) < binary.BigEndian.Uint64(h[j].key[:])
	}
	return h[i].doc < h[j].doc
}
func (h runHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *runHeap) Push(x any)   { *h = append(*h, x.(*runHead)) }
func (h *runHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]
	return it
}
