package builder

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"go.uber.org/atomic"

	"github.com/klauspost/compress/s2"

	"github.com/grafana/loki/v3/pkg/logline/shard"
)

// postingsBuffer buffers (ngram, docID) pairs in one flat SoA buffer, and on
// fill radix-sorts + dedupes them and spills a sorted run to scratch disk.
//
// Time units used throughout this type and the merge:
//   - Absolute bucket: wall-clock document bucket counted from Unix epoch
//     (1970). Used for day/date math.
//   - Tick (docID): absolute bucket minus baseBucket, packed into uint32 for
//     storage. baseBucket is the fixed docIDEpoch (2026-01-01); see
//     docid_window.go.
//
// A pair's date and shard are both recoverable at merge time without any
// per-bucket in-memory state — this is what lets the builder defer all
// (date, shard) bucketing to the k-way merge.
//
// Sizing comes from config: bufferPairs is Config.PostingsBufferPairs
// (resident memory ≈ pairs × 24 B — 12 B/pair live keys+docs plus 12 B/pair
// radix scratch, all allocated up front; see residentBytes) and spillWatermark
// is Config.PostingsSpillWatermark, the deduped-head fraction that triggers a
// run spill.
//
// Fill strategy (incremental fill): rather than spilling after every radix
// pass, the sorted+deduped result is kept as a "head" and the buffer keeps
// filling; each new "tail" is radix-sorted alone and linearly merged into the
// head (deduping across), and a run is spilled only once the deduped head
// reaches spillWatermark×bufferPairs. This packs each run ~1/spillWatermark×
// denser — far fewer, larger runs feed the merge — while still radix-sorting
// every appended pair exactly once. Measured on captured data: ~7× fewer runs
// and ~15-25% lower ingest+merge wall time than spill-per-fill, and it
// decouples run count from buffer size (a small buffer yields few runs).
type postingsBuffer struct {
	bufferPairs    int
	spillWatermark float64
	intervalNanos  int64
	ticksPerDay    uint64
	// baseBucket is the absolute bucket of docIDEpoch. Stored ticks are
	// relative: tick = absBucket - baseBucket (fits uint32). Day/date math
	// converts back with abs = baseBucket + tick.
	baseBucket uint64
	shardCount int
	shardFn    shard.Func
	scratchDir string
	// runPrefix names this buffer's run files ("run_" for the serial builder,
	// "run_w<i>_" per extract-pipeline worker) so multiple buffers can share
	// one runDir without filename collisions.
	runPrefix string

	// SoA buffer + tandem radix scratch (all cap bufferPairs).
	keys         [][8]byte
	docs         []uint32
	keyBuf       [][8]byte
	docBuf       []uint32
	hist         [3][1 << 16]int32 // one plane per data-bearing radix digit
	posArr       [1 << 16]int32
	shardScratch []uint8
	sortedHead   int // len of the sorted+deduped ngram-ordered prefix of keys/docs

	// Referenced tick-of-day bitset per (shard, dayIndex), populated at spill.
	// Each (date, shard) .lidx has a documents table: the set of time buckets
	// that appear in that file. recordDocumentTick records those buckets as we
	// spill so merge can build the table (and dense-rank postings into it).
	refTicks map[refKey][]uint64
	// refTicksBytes is the running total of bitset bytes allocated in refTicks,
	// maintained on the allocation path of recordDocumentTick (once per
	// (shard, day), never per pair) so residentBytes doesn't have to walk the
	// map. Atomic because in pipeline mode the poll goroutine's shouldFlush
	// sums residentBytes across worker buffers while the owning worker is
	// still spilling.
	refTicksBytes atomic.Uint64

	// sortBufBytes is the immutable capacity term of residentBytes, computed
	// once at construction. residentBytes must not derive it from cap(keys)
	// etc.: in pipeline mode the slice headers are rewritten by the owning
	// worker's appends while shouldFlush reads the estimate concurrently.
	sortBufBytes uint64
	// released records that releaseSortBuffers dropped the sort buffers, so
	// residentBytes stops counting sortBufBytes. Only ever set on a retired
	// builder (merge phase), after the drain barrier — never racing readers.
	released bool

	// shardScratchBytes is the resident size of the lazily-allocated
	// shard-reorder scratch (1 B/pair once the first sharded spill creates
	// it; excluded from sortBufferFloorBytes because it is not an up-front
	// allocation). Atomic for the same shouldFlush-vs-worker reason as
	// refTicksBytes: the owning worker (re)allocates the scratch on its spill
	// path while the poll goroutine reads the estimate.
	shardScratchBytes atomic.Uint64

	// spilled runs. runBytes is atomic for the same shouldFlush-vs-worker
	// reason as refTicksBytes (runDiskBytes sums it across worker buffers);
	// the remaining fields are touched only by the owning goroutine during
	// ingest and by the flush goroutine after the drain barrier.
	runPaths   []string
	runBytes   atomic.Int64
	runSeq     int
	dirCreated bool
	sw         *s2.Writer
}

type refKey struct {
	shard int
	day   uint64
}

// postingsBufferConfig holds the resolved, global parameters for one
// accumulation cycle.
type postingsBufferConfig struct {
	bufferPairs    int
	spillWatermark float64
	intervalNanos  int64
	ticksPerDay    uint64
	baseBucket     uint64
	shardCount     int
	shardFn        shard.Func
	scratchDir     string
	runPrefix      string
}

func newPostingsBuffer(cfg postingsBufferConfig) *postingsBuffer {
	prefix := cfg.runPrefix
	if prefix == "" {
		prefix = "run_"
	}
	return &postingsBuffer{
		bufferPairs:    cfg.bufferPairs,
		spillWatermark: cfg.spillWatermark,
		intervalNanos:  cfg.intervalNanos,
		ticksPerDay:    cfg.ticksPerDay,
		baseBucket:     cfg.baseBucket,
		shardCount:     cfg.shardCount,
		shardFn:        cfg.shardFn,
		scratchDir:     cfg.scratchDir,
		runPrefix:      prefix,
		keys:           make([][8]byte, 0, cfg.bufferPairs),
		docs:           make([]uint32, 0, cfg.bufferPairs),
		keyBuf:         make([][8]byte, cfg.bufferPairs),
		docBuf:         make([]uint32, cfg.bufferPairs),
		sortBufBytes:   sortBufferFloorBytes(cfg.bufferPairs),
		refTicks:       make(map[refKey][]uint64),
	}
}

// tick converts an absolute document bucket (from 1970) into a packed tick
// relative to baseBucket, reporting whether it fits in the uint32 window.
// Out-of-window entries (before the fixed docIDEpoch, or beyond 2^32 ticks
// past it) must never be silently wrapped into a wrong — possibly recent —
// date's index; the caller panics on them (panicOutOfWindow).
func (b *postingsBuffer) tick(absBucket uint64) (uint32, bool) {
	if absBucket < b.baseBucket {
		return 0, false
	}
	rel := absBucket - b.baseBucket
	if rel > math.MaxUint32 {
		return 0, false
	}
	return uint32(rel), true
}

// appendPair is the hot-path append: a bare two-slice append, small enough to
// inline into processStream's ngram loop. Spilling/integration is the caller's
// job (bufferFull + onFull) so this never grows the slices past capacity.
func (b *postingsBuffer) appendPair(ngram [8]byte, docID uint32) {
	b.keys = append(b.keys, ngram)
	b.docs = append(b.docs, docID)
}

func (b *postingsBuffer) bufferFull() bool { return len(b.keys) >= b.bufferPairs }

// onFull integrates the freshly appended tail into the sorted head and spills a
// run once the deduped head crosses the high-water mark.
func (b *postingsBuffer) onFull() error {
	b.integrateTail()
	if b.sortedHead >= int(b.spillWatermark*float64(b.bufferPairs)) {
		return b.spillSorted()
	}
	return nil
}

// integrateTail sorts+dedupes the freshly appended tail [sortedHead:len] in
// ngram order and 2-way-merges it (deduping across) with the already-sorted
// head, leaving keys/docs[0:sortedHead'] sorted+deduped in ngram order. Each
// appended pair is radix-sorted exactly once (in its one tail); the merge is
// linear. Shard reordering is deferred to spill, so this carries no shardFn
// cost.
func (b *postingsBuffer) integrateTail() {
	n := len(b.keys)
	h := b.sortedHead
	if n <= h {
		return
	}

	tk, td := radixSortByNgram(b.keys[h:n], b.docs[h:n], b.keyBuf[h:n], b.docBuf[h:n], &b.hist, &b.posArr)
	m := sortAndDedupeGroups(tk, td)
	// Ensure the sorted tail lives in keys-side [h:h+m] so keyBuf/docBuf are
	// free to receive the merge output.
	if m > 0 && &tk[0] != &b.keys[h] {
		copy(b.keys[h:h+m], tk[:m])
		copy(b.docs[h:h+m], td[:m])
	}

	if h == 0 {
		b.keys = b.keys[:m]
		b.docs = b.docs[:m]
		b.sortedHead = m
		return
	}

	merged := mergeSortedPairs(b.keys[:h], b.docs[:h], b.keys[h:h+m], b.docs[h:h+m], b.keyBuf, b.docBuf)
	b.keys = b.keys[:merged]
	b.docs = b.docs[:merged]
	copy(b.keys, b.keyBuf[:merged])
	copy(b.docs, b.docBuf[:merged])
	b.sortedHead = merged
}

// finish integrates the final tail and spills the remaining head as a run. It
// is called once at end-of-cycle (prepareIndexes), before the merge, and is
// idempotent: with the buffer already drained it is a no-op, so a retried
// prepareIndexes re-merges the surviving runs without inventing new ones.
func (b *postingsBuffer) finish() error {
	b.integrateTail()
	return b.spillSorted()
}

// spillSorted writes the sorted+deduped head [0:sortedHead] as a run, applying
// the shard reorder and recording referenced ticks (once, over the deduped
// head) along the way.
func (b *postingsBuffer) spillSorted() error {
	n := b.sortedHead
	if n == 0 {
		return nil
	}
	sk, sd, counts := b.shardSortAndTrack(n)

	if !b.dirCreated {
		if err := os.MkdirAll(b.scratchDir, 0o755); err != nil {
			return err
		}
		b.dirCreated = true
	}
	path := filepath.Join(b.scratchDir, fmt.Sprintf("%s%d.frun", b.runPrefix, b.runSeq))
	b.runSeq++
	size, err := writeRun(path, sk, sd, counts, &b.sw)
	if err != nil {
		return err
	}
	b.runPaths = append(b.runPaths, path)
	b.runBytes.Add(size)

	b.keys = b.keys[:0]
	b.docs = b.docs[:0]
	b.sortedHead = 0
	return nil
}

// shardSortAndTrack reorders the ngram-sorted head [0:n] into shard-contiguous
// order (stable within a shard, so ngram order is preserved) and records every
// pair's document tick for that shard's documents table. Scatters into
// keyBuf/docBuf (free at spill time) and returns them plus the per-shard
// record counts (used to write the run's shard directory). When sharding is
// disabled the reorder is skipped and ticks are recorded against shard 0.
func (b *postingsBuffer) shardSortAndTrack(n int) ([][8]byte, []uint32, []int32) {
	src, srcD := b.keys[:n], b.docs[:n]
	if b.shardCount <= 1 {
		for i := 0; i < n; i++ {
			b.recordDocumentTick(0, srcD[i])
		}
		return src, srcD, []int32{int32(n)}
	}

	if cap(b.shardScratch) < n {
		b.shardScratch = make([]uint8, n)
		b.shardScratchBytes.Store(uint64(n))
	}
	shards := b.shardScratch[:n]
	cnt := make([]int32, b.shardCount)
	for i := 0; i < n; i++ {
		s := uint8(b.shardFn(src[i], b.shardCount))
		shards[i] = s
		cnt[s]++
		b.recordDocumentTick(int(s), srcD[i])
	}
	pos := make([]int32, b.shardCount)
	for i := 1; i < b.shardCount; i++ {
		pos[i] = pos[i-1] + cnt[i-1]
	}
	dstK, dstD := b.keyBuf[:n], b.docBuf[:n]
	for i := 0; i < n; i++ {
		p := pos[shards[i]]
		dstK[p] = src[i]
		dstD[p] = srcD[i]
		pos[shards[i]] = p + 1
	}
	return dstK, dstD, cnt
}

// recordDocumentTick notes that shardVal's .lidx for tick's calendar day must
// include this document bucket. Converts the packed tick back to an absolute
// bucket (baseBucket + tick) so day / tick-of-day land on real calendar
// boundaries, then sets the bit in that (shard, day)'s bitset.
func (b *postingsBuffer) recordDocumentTick(shardVal int, tick uint32) {
	abs := b.baseBucket + uint64(tick)
	day := abs / b.ticksPerDay
	tod := abs % b.ticksPerDay
	k := refKey{shard: shardVal, day: day}
	bs := b.refTicks[k]
	if bs == nil {
		bs = make([]uint64, (b.ticksPerDay+63)/64)
		b.refTicks[k] = bs
		b.refTicksBytes.Add(uint64(len(bs)) * 8)
	}
	bs[tod>>6] |= 1 << (tod & 63)
}

// releaseSortBuffers frees the large ingest sort buffers once ingest is
// complete; the merge streams runs from disk and does not need them. refTicks
// is retained — the merge needs it for doc-metadata synthesis.
func (b *postingsBuffer) releaseSortBuffers() {
	b.keys = nil
	b.docs = nil
	b.keyBuf = nil
	b.docBuf = nil
	b.shardScratch = nil
	b.shardScratchBytes.Store(0)
	b.released = true
}

// residentBytes estimates the resident in-memory working set: the CAPACITY of
// all four sort buffers (keys/keyBuf are 8 B/slot, docs/docBuf 4 B/slot — the
// buffers are allocated in full up front, so length would understate what the
// process actually holds), plus the shard-reorder scratch (1 B/slot, allocated
// lazily by the first sharded spill and retained for the cycle), plus the
// refTicks bitsets. After releaseSortBuffers the buffer and scratch terms drop
// to zero, leaving only refTicks. The capacity term is the precomputed
// sortBufBytes rather than cap() reads, and the scratch term an atomic: in
// pipeline mode the owning worker rewrites the slice headers on every append
// (and allocates the scratch on its spill path) while shouldFlush reads the
// estimate. This feeds the GOMEMLIMIT-fraction full-flush trigger, so it must
// not understate: an optimistic estimate silently disarms that safety net.
func (b *postingsBuffer) residentBytes() uint64 {
	buf := b.sortBufBytes
	if b.released {
		buf = 0
	}
	return buf + b.shardScratchBytes.Load() + b.refTicksBytes.Load()
}

// sortBufferFloorBytes returns the fixed resident floor of the ingest sort
// buffers for a batch of n pairs: keys+keyBuf at 8 B/slot plus docs+docBuf at
// 4 B/slot, all allocated in full at construction (~480 MiB at the default
// postings_buffer_pairs of 20M). This is the constant term of residentBytes
// while ingest is active (cached once as sortBufBytes so residentBytes can be
// read race-free while a worker mutates the buffer). The shard-reorder scratch
// (1 B/pair, counted by residentBytes) is deliberately excluded: the floor is
// what is allocated UP FRONT, and the scratch appears only lazily on the first
// sharded spill.
func sortBufferFloorBytes(n int) uint64 {
	return uint64(n) * 24
}

// clear resets every piece of per-cycle state: buffered pairs, the sorted-head
// mark, referenced-tick bitsets, and the run bookkeeping (paths, bytes, seq,
// dir flag). It does NOT re-arm the sort buffers released by
// releaseSortBuffers — builders are single-cycle (swapBuilder always
// constructs a fresh one); clearing fully just ensures nothing stale survives.
func (b *postingsBuffer) clear() {
	if b.keys != nil {
		b.keys = b.keys[:0]
		b.docs = b.docs[:0]
	}
	b.sortedHead = 0
	b.refTicks = make(map[refKey][]uint64)
	b.refTicksBytes.Store(0)
	b.runPaths = nil
	b.runBytes.Store(0)
	b.runSeq = 0
	b.dirCreated = false
}
