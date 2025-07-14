package iter

import (
	"context"
	"io"
	"math"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

type streamIterator struct {
	i      int
	stream logproto.Stream
}

// NewStreamIterator iterates over entries in a stream.
func NewStreamIterator(stream logproto.Stream) EntryIterator {
	return &streamIterator{
		i:      -1,
		stream: stream,
	}
}

func (i *streamIterator) Next() bool {
	i.i++
	return i.i < len(i.stream.Entries)
}

func (i *streamIterator) Err() error {
	return nil
}

func (i *streamIterator) Labels() string {
	return i.stream.Labels
}

func (i *streamIterator) StreamHash() uint64 {
	return i.stream.Hash
}

func (i *streamIterator) At() logproto.Entry {
	return i.stream.Entries[i.i]
}

func (i *streamIterator) Close() error {
	return nil
}

// MergeEntryIterator exposes additional fields that are used by the Tailer only.
// Not safe for concurrent use!
type MergeEntryIterator interface {
	EntryIterator

	Peek() time.Time
	IsEmpty() bool
	Push(EntryIterator)
}

// mergeEntryIterator implements the MergeEntryIterator interface functions.
type mergeEntryIterator struct {
	tree  *loser.Tree[sortFields, EntryIterator]
	stats *stats.Context

	// buffer of entries to be returned by Next()
	// We buffer entries with the same timestamp to correctly dedupe them.
	buffer    []entryWithLabels
	currEntry entryWithLabels
	errs      []error
}

// NewMergeEntryIterator returns a new iterator which uses a looser tree to merge together entries for multiple iterators and deduplicate entries if any.
// The iterator only order and merge entries across given `is` iterators, it does not merge entries within individual iterator.
// This means using this iterator with a single iterator will result in the same result as the input iterator.
// If you don't need to deduplicate entries, use `NewSortEntryIterator` instead.
func NewMergeEntryIterator(ctx context.Context, is []EntryIterator, direction logproto.Direction) MergeEntryIterator {
	maxVal, less := treeLess(direction)
	result := &mergeEntryIterator{stats: stats.FromContext(ctx)}
	result.tree = loser.New(is, maxVal, sortFieldsAt, less, result.closeEntry)
	result.buffer = make([]entryWithLabels, 0, len(is))
	return result
}

func (i *mergeEntryIterator) closeEntry(e EntryIterator) {
	if err := e.Err(); err != nil {
		i.errs = append(i.errs, err)
	}
	util.LogError("closing iterator", e.Close)
}

func (i *mergeEntryIterator) Push(ei EntryIterator) {
	i.tree.Push(ei)
}

// Next fetches entries from the tree until it finds an entry with a different timestamp or stream hash.
// Generally i.buffer has one or more entries with the same timestamp and stream hash,
// followed by one more item where the timestamp or stream hash was different.
func (i *mergeEntryIterator) Next() bool {
	if len(i.buffer) < 2 {
		i.fillBuffer()
	}
	if len(i.buffer) == 0 {
		return false
	}
	i.nextFromBuffer()

	return true
}

func (i *mergeEntryIterator) fillBuffer() {
	if !i.tree.Next() {
		return
	}
	// At this point we have zero or one items in i.buffer, and the next item is available from i.tree.

	// We support multiple entries with the same timestamp, and we want to
	// preserve their original order.
	// Entries with identical timestamp and line are removed as duplicates.
	for {
		next := i.tree.Winner()
		entry := next.At()
		i.buffer = append(i.buffer, entryWithLabels{
			Entry:      entry,
			labels:     next.Labels(),
			streamHash: next.StreamHash(),
		})
		if len(i.buffer) > 1 &&
			(i.buffer[0].streamHash != next.StreamHash() ||
				!i.buffer[0].Entry.Timestamp.Equal(entry.Timestamp)) {
			break
		}
		previous := i.buffer[:len(i.buffer)-1]

		var dupe bool
		for _, t := range previous {
			if t.Entry.Line == entry.Line {
				i.stats.AddDuplicates(1)
				dupe = true
				break
			}
		}
		if dupe {
			i.buffer = previous
		}
		if !i.tree.Next() {
			break
		}
	}
}

func (i *mergeEntryIterator) nextFromBuffer() {
	i.currEntry.Entry = i.buffer[0].Entry
	i.currEntry.labels = i.buffer[0].labels
	i.currEntry.streamHash = i.buffer[0].streamHash
	if len(i.buffer) == 2 {
		i.buffer[0] = i.buffer[1]
		i.buffer = i.buffer[:1]
		return
	}
	if len(i.buffer) == 1 {
		i.buffer = i.buffer[:0]
		return
	}
	i.buffer = i.buffer[1:]
}

func (i *mergeEntryIterator) At() logproto.Entry {
	return i.currEntry.Entry
}

func (i *mergeEntryIterator) Labels() string {
	return i.currEntry.labels
}

func (i *mergeEntryIterator) StreamHash() uint64 { return i.currEntry.streamHash }

func (i *mergeEntryIterator) Err() error {
	switch len(i.errs) {
	case 0:
		return nil
	case 1:
		return i.errs[0]
	default:
		return util.MultiError(i.errs)
	}
}

func (i *mergeEntryIterator) Close() error {
	i.tree.Close()
	i.buffer = nil
	return i.Err()
}

func (i *mergeEntryIterator) Peek() time.Time {
	if len(i.buffer) == 0 {
		i.fillBuffer()
	}
	if len(i.buffer) == 0 {
		return time.Time{}
	}
	return i.buffer[0].Timestamp
}

// IsEmpty returns true if there are no more entries to pull.
func (i *mergeEntryIterator) IsEmpty() bool {
	if len(i.buffer) == 0 {
		i.fillBuffer()
	}
	return len(i.buffer) == 0
}

type entrySortIterator struct {
	tree      *loser.Tree[sortFields, EntryIterator]
	currEntry entryWithLabels
	errs      []error
}

// NewSortEntryIterator returns a new EntryIterator that sorts entries by timestamp (depending on the direction) the input iterators.
// The iterator only order entries across given `is` iterators, it does not sort entries within individual iterator.
// This means using this iterator with a single iterator will result in the same result as the input iterator.
// When timestamp is equal, the iterator sorts samples by their label alphabetically.
func NewSortEntryIterator(is []EntryIterator, direction logproto.Direction) EntryIterator {
	if len(is) == 0 {
		return NoopEntryIterator
	}
	if len(is) == 1 {
		return is[0]
	}
	maxVal, less := treeLess(direction)
	result := &entrySortIterator{}
	result.tree = loser.New(is, maxVal, sortFieldsAt, less, result.closeEntry)
	return result
}

func treeLess(direction logproto.Direction) (maxVal sortFields, less func(a, b sortFields) bool) {
	switch direction {
	case logproto.BACKWARD:
		maxVal = sortFields{timeNanos: math.MinInt64}
		less = lessDescending
	case logproto.FORWARD:
		maxVal = sortFields{timeNanos: math.MaxInt64}
		less = lessAscending
	default:
		panic("bad direction")
	}
	return
}

type sortFields struct {
	labels     string
	timeNanos  int64
	streamHash uint64
}

func sortFieldsAt(i EntryIterator) sortFields {
	return sortFields{
		timeNanos:  i.At().Timestamp.UnixNano(),
		labels:     i.Labels(),
		streamHash: i.StreamHash(),
	}
}

func lessAscending(e1, e2 sortFields) bool {
	if e1.timeNanos == e2.timeNanos {
		// The underlying stream hash may not be available, such as when merging LokiResponses in the
		// frontend which were sharded. Prefer to use the underlying stream hash when available,
		// which is needed in deduping code, but defer to label sorting when it's not present.
		if e1.streamHash == 0 {
			return e1.labels < e2.labels
		}
		return e1.streamHash < e2.streamHash
	}
	return e1.timeNanos < e2.timeNanos
}

func lessDescending(e1, e2 sortFields) bool {
	if e1.timeNanos == e2.timeNanos {
		if e1.streamHash == 0 {
			return e1.labels < e2.labels
		}
		return e1.streamHash < e2.streamHash
	}
	return e1.timeNanos > e2.timeNanos
}

func (i *entrySortIterator) closeEntry(e EntryIterator) {
	if err := e.Err(); err != nil {
		i.errs = append(i.errs, err)
	}
	util.LogError("closing iterator", e.Close)
}

func (i *entrySortIterator) Next() bool {
	ret := i.tree.Next()
	if !ret {
		return false
	}
	next := i.tree.Winner()
	i.currEntry.Entry = next.At()
	i.currEntry.labels = next.Labels()
	i.currEntry.streamHash = next.StreamHash()
	return true
}

func (i *entrySortIterator) At() logproto.Entry {
	return i.currEntry.Entry
}

func (i *entrySortIterator) Labels() string {
	return i.currEntry.labels
}

func (i *entrySortIterator) StreamHash() uint64 {
	return i.currEntry.streamHash
}

func (i *entrySortIterator) Err() error {
	switch len(i.errs) {
	case 0:
		return nil
	case 1:
		return i.errs[0]
	default:
		return util.MultiError(i.errs)
	}
}

func (i *entrySortIterator) Close() error {
	i.tree.Close()
	return i.Err()
}

// NewStreamsIterator returns an iterator over logproto.Stream
func NewStreamsIterator(streams []logproto.Stream, direction logproto.Direction) EntryIterator {
	is := make([]EntryIterator, 0, len(streams))
	for i := range streams {
		is = append(is, NewStreamIterator(streams[i]))
	}
	return NewSortEntryIterator(is, direction)
}

// NewQueryResponseIterator returns an iterator over a QueryResponse.
func NewQueryResponseIterator(resp *logproto.QueryResponse, direction logproto.Direction) EntryIterator {
	return NewStreamsIterator(resp.Streams, direction)
}

type queryClientIterator struct {
	client    logproto.Querier_QueryClient
	direction logproto.Direction
	err       error
	curr      EntryIterator
}

// NewQueryClientIterator returns an iterator over a QueryClient.
func NewQueryClientIterator(client logproto.Querier_QueryClient, direction logproto.Direction) EntryIterator {
	return &queryClientIterator{
		client:    client,
		direction: direction,
	}
}

func (i *queryClientIterator) Next() bool {
	ctx := i.client.Context()
	for i.curr == nil || !i.curr.Next() {
		batch, err := i.client.Recv()
		if err == io.EOF {
			return false
		} else if err != nil {
			i.err = err
			return false
		}
		stats.JoinIngesters(ctx, batch.Stats)
		_ = metadata.AddWarnings(ctx, batch.Warnings...)
		i.curr = NewQueryResponseIterator(batch, i.direction)
	}

	return true
}

func (i *queryClientIterator) At() logproto.Entry {
	return i.curr.At()
}

func (i *queryClientIterator) Labels() string {
	return i.curr.Labels()
}

func (i *queryClientIterator) StreamHash() uint64 { return i.curr.StreamHash() }

func (i *queryClientIterator) Err() error {
	return i.err
}

func (i *queryClientIterator) Close() error {
	return i.client.CloseSend()
}

type nonOverlappingIterator struct {
	iterators []EntryIterator
	curr      EntryIterator
}

// NewNonOverlappingIterator gives a chained iterator over a list of iterators.
func NewNonOverlappingIterator(iterators []EntryIterator) EntryIterator {
	return &nonOverlappingIterator{
		iterators: iterators,
	}
}

func (i *nonOverlappingIterator) Next() bool {
	for i.curr == nil || !i.curr.Next() {
		if len(i.iterators) == 0 {
			if i.curr != nil {
				i.curr.Close()
			}
			return false
		}
		if i.curr != nil {
			i.curr.Close()
		}
		i.curr, i.iterators = i.iterators[0], i.iterators[1:]
	}

	return true
}

func (i *nonOverlappingIterator) At() logproto.Entry {
	return i.curr.At()
}

func (i *nonOverlappingIterator) Labels() string {
	if i.curr == nil {
		return ""
	}
	return i.curr.Labels()
}

func (i *nonOverlappingIterator) StreamHash() uint64 {
	if i.curr == nil {
		return 0
	}
	return i.curr.StreamHash()
}

func (i *nonOverlappingIterator) Err() error {
	if i.curr == nil {
		return nil
	}
	return i.curr.Err()
}

func (i *nonOverlappingIterator) Close() error {
	if i.curr != nil {
		i.curr.Close()
	}
	for _, iter := range i.iterators {
		iter.Close()
	}
	i.iterators = nil
	return nil
}

type timeRangedIterator struct {
	EntryIterator
	mint, maxt time.Time
}

// NewTimeRangedIterator returns an iterator which filters entries by time range.
// Note: Only works with iterators that go forwards.
func NewTimeRangedIterator(it EntryIterator, mint, maxt time.Time) EntryIterator {
	return &timeRangedIterator{
		EntryIterator: it,
		mint:          mint,
		maxt:          maxt,
	}
}

func (i *timeRangedIterator) Next() bool {
	ok := i.EntryIterator.Next()
	if !ok {
		i.EntryIterator.Close()
		return ok
	}
	ts := i.EntryIterator.At().Timestamp
	for ok && i.mint.After(ts) {
		ok = i.EntryIterator.Next()
		if !ok {
			continue
		}
		ts = i.EntryIterator.At().Timestamp
	}
	if ok {
		if ts.Equal(i.mint) { // The mint is inclusive
			return true
		}
		if i.maxt.Before(ts) || i.maxt.Equal(ts) { // The maxt is exclusive.
			ok = false
		}
	}
	if !ok {
		i.EntryIterator.Close()
	}
	return ok
}

type entryWithLabels struct {
	logproto.Entry
	labels     string
	streamHash uint64
}

type reverseIterator struct {
	iter              EntryIterator
	cur               entryWithLabels
	entriesWithLabels []entryWithLabels

	loaded bool
	limit  uint32
}

// NewReversedIter returns an iterator which loads all or up to N entries
// of an existing iterator, and then iterates over them backward.
// Preload entries when they are being queried with a timeout.
func NewReversedIter(it EntryIterator, limit uint32, preload bool) (EntryIterator, error) {
	iter, err := &reverseIterator{
		iter:              it,
		entriesWithLabels: make([]entryWithLabels, 0, 1024),
		limit:             limit,
	}, it.Err()
	if err != nil {
		return nil, err
	}

	if preload {
		iter.load()
	}

	return iter, nil
}

func (i *reverseIterator) load() {
	if !i.loaded {
		i.loaded = true
		for count := uint32(0); (i.limit == 0 || count < i.limit) && i.iter.Next(); count++ {
			i.entriesWithLabels = append(i.entriesWithLabels, entryWithLabels{i.iter.At(), i.iter.Labels(), i.iter.StreamHash()})
		}
		i.iter.Close()
	}
}

func (i *reverseIterator) Next() bool {
	i.load()
	if len(i.entriesWithLabels) == 0 {
		i.entriesWithLabels = nil
		return false
	}
	i.cur, i.entriesWithLabels = i.entriesWithLabels[len(i.entriesWithLabels)-1], i.entriesWithLabels[:len(i.entriesWithLabels)-1]
	return true
}

func (i *reverseIterator) At() logproto.Entry {
	return i.cur.Entry
}

func (i *reverseIterator) Labels() string {
	return i.cur.labels
}

func (i *reverseIterator) StreamHash() uint64 {
	return i.cur.streamHash
}

func (i *reverseIterator) Err() error { return nil }

func (i *reverseIterator) Close() error {
	if !i.loaded {
		return i.iter.Close()
	}
	return nil
}

var entryBufferPool = sync.Pool{
	New: func() interface{} {
		return &entryBuffer{
			entries: make([]entryWithLabels, 0, 1024),
		}
	},
}

type entryBuffer struct {
	entries []entryWithLabels
}

type reverseEntryIterator struct {
	iter EntryIterator
	cur  entryWithLabels
	buf  *entryBuffer

	loaded bool
}

// NewEntryReversedIter returns an iterator which loads all entries and iterates backward.
// The labels of entries is always empty.
func NewEntryReversedIter(it EntryIterator) (EntryIterator, error) {
	iter, err := &reverseEntryIterator{
		iter: it,
		buf:  entryBufferPool.Get().(*entryBuffer),
	}, it.Err()
	if err != nil {
		return nil, err
	}

	return iter, nil
}

func (i *reverseEntryIterator) load() {
	if !i.loaded {
		i.loaded = true
		for i.iter.Next() {
			i.buf.entries = append(i.buf.entries, entryWithLabels{i.iter.At(), i.iter.Labels(), i.iter.StreamHash()})
		}
		i.iter.Close()
	}
}

func (i *reverseEntryIterator) Next() bool {
	i.load()
	if i.buf == nil || len(i.buf.entries) == 0 {
		i.release()
		return false
	}
	i.cur, i.buf.entries = i.buf.entries[len(i.buf.entries)-1], i.buf.entries[:len(i.buf.entries)-1]
	return true
}

func (i *reverseEntryIterator) At() logproto.Entry {
	return i.cur.Entry
}

func (i *reverseEntryIterator) Labels() string {
	return i.cur.labels
}

func (i *reverseEntryIterator) StreamHash() uint64 {
	return i.cur.streamHash
}

func (i *reverseEntryIterator) Err() error { return nil }

func (i *reverseEntryIterator) release() {
	if i.buf == nil {
		return
	}

	if i.buf.entries != nil {
		// preserve the underlying slice before releasing to pool
		i.buf.entries = i.buf.entries[:0]
	}
	entryBufferPool.Put(i.buf)
	i.buf = nil
}

func (i *reverseEntryIterator) Close() error {
	i.release()
	if !i.loaded {
		return i.iter.Close()
	}
	return nil
}

// ReadBatch reads a set of entries off an iterator.
func ReadBatch(i EntryIterator, size uint32) (*logproto.QueryResponse, uint32, error) {
	var (
		streams      = map[uint64]map[string]*logproto.Stream{}
		respSize     uint32
		streamsCount int
	)
	for ; respSize < size && i.Next(); respSize++ {
		labels, hash, entry := i.Labels(), i.StreamHash(), i.At()
		mutatedStreams, ok := streams[hash]
		if !ok {
			mutatedStreams = map[string]*logproto.Stream{}
			streams[hash] = mutatedStreams
		}
		mutatedStream, ok := mutatedStreams[labels]
		if !ok {
			streamsCount++
			mutatedStream = &logproto.Stream{
				Labels: labels,
				Hash:   hash,
			}
			mutatedStreams[labels] = mutatedStream
		}
		mutatedStream.Entries = append(mutatedStream.Entries, entry)
	}

	result := logproto.QueryResponse{
		Streams: make([]logproto.Stream, 0, streamsCount),
	}
	for _, mutatedStreams := range streams {
		for _, s := range mutatedStreams {
			result.Streams = append(result.Streams, *s)
		}
	}
	return &result, respSize, i.Err()
}

type peekingEntryIterator struct {
	iter EntryIterator

	cache *entryWithLabels
	next  *entryWithLabels
}

// PeekingEntryIterator is an entry iterator that can look ahead an entry
// using `Peek` without advancing its cursor.
type PeekingEntryIterator interface {
	EntryIterator
	Peek() (string, logproto.Entry, bool)
}

// NewPeekingIterator creates a new peeking iterator.
func NewPeekingIterator(iter EntryIterator) PeekingEntryIterator {
	// initialize the next entry so we can peek right from the start.
	var cache *entryWithLabels
	next := &entryWithLabels{}
	if iter.Next() {
		cache = &entryWithLabels{
			Entry:      iter.At(),
			labels:     iter.Labels(),
			streamHash: iter.StreamHash(),
		}
		next.Entry = cache.Entry
		next.labels = cache.labels
	}
	return &peekingEntryIterator{
		iter:  iter,
		cache: cache,
		next:  next,
	}
}

// Next implements `EntryIterator`
func (it *peekingEntryIterator) Next() bool {
	if it.cache != nil {
		it.next.Entry = it.cache.Entry
		it.next.labels = it.cache.labels
		it.next.streamHash = it.cache.streamHash
		it.cacheNext()
		return true
	}
	return false
}

// cacheNext caches the next element if it exists.
func (it *peekingEntryIterator) cacheNext() {
	if it.iter.Next() {
		it.cache.Entry = it.iter.At()
		it.cache.labels = it.iter.Labels()
		it.cache.streamHash = it.iter.StreamHash()
		return
	}
	// nothing left removes the cached entry
	it.cache = nil
}

// Peek implements `PeekingEntryIterator`
func (it *peekingEntryIterator) Peek() (string, logproto.Entry, bool) {
	if it.cache != nil {
		return it.cache.labels, it.cache.Entry, true
	}
	return "", logproto.Entry{}, false
}

// Labels implements `EntryIterator`
func (it *peekingEntryIterator) Labels() string {
	if it.next != nil {
		return it.next.labels
	}
	return ""
}

func (it *peekingEntryIterator) StreamHash() uint64 {
	if it.next != nil {
		return it.next.streamHash
	}
	return 0
}

// Entry implements `EntryIterator`
func (it *peekingEntryIterator) At() logproto.Entry {
	if it.next != nil {
		return it.next.Entry
	}
	return logproto.Entry{}
}

// Error implements `EntryIterator`
func (it *peekingEntryIterator) Err() error {
	return it.iter.Err()
}

// Close implements `EntryIterator`
func (it *peekingEntryIterator) Close() error {
	return it.iter.Close()
}

type withCloseEntryIterator struct {
	closeOnce sync.Once
	closeFn   func() error
	errs      []error
	EntryIterator
}

func (w *withCloseEntryIterator) Close() error {
	w.closeOnce.Do(func() {
		if err := w.EntryIterator.Close(); err != nil {
			w.errs = append(w.errs, err)
		}
		if err := w.closeFn(); err != nil {
			w.errs = append(w.errs, err)
		}
	})
	if len(w.errs) == 0 {
		return nil
	}
	return util.MultiError(w.errs)
}

func EntryIteratorWithClose(it EntryIterator, closeFn func() error) EntryIterator {
	return &withCloseEntryIterator{
		closeOnce:     sync.Once{},
		closeFn:       closeFn,
		EntryIterator: it,
	}
}
