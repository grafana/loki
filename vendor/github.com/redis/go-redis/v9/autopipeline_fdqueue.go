package redis

import "sync"

// fdQueue is the full-duplex submit queue: an MPSC hand-off from many submitting
// goroutines to the single writer goroutine.
//
// It replaces a buffered `chan fdReq`. The channel cost, per command:
//
//	producer  lock + 104-byte copy + unlock, plus a goready EVERY time an
//	          arrival found the writer parked
//	writer    lock + 104-byte copy + unlock, ONCE PER COMMAND
//
// At ~450k ops/s that measured 3.41 s in the submit send, 2.65 s in the writer's
// receive and 3.25 s in the accumulate select (19.6% of all CPU between them).
//
// The slice costs one lock and one append per command on the producer side, and
// ONE lock plus ONE bulk copy for the whole wave on the writer side — the
// writer's per-command cost disappears. pushBatch amortises the producer lock
// across k commands as well.
//
// The structural win is not the copy, it is the parking. A channel can only
// express "wake me on the next arrival", so an accumulating writer was woken
// once per command and re-entered its select ~112 times per 250 us window. This
// queue lets the writer say "wake me when the queue holds N more, or not at
// all" (park), so an accumulating writer parks and wakes ONCE per flush. A
// submitter that finds the writer awake, or the depth still short of what the
// writer asked for, does no channel work at all.
//
// Ordering is FIFO, so the full-order (T1) guarantee is unchanged. The one
// semantic loss versus a channel is fairness between submitters blocked on a
// FULL queue: chansend queues blocked senders FIFO, whereas here they re-contend
// for the mutex. Per-goroutine program order is still exact (a goroutine's push
// happens-before its next push), which is what the ordering contract actually
// promises; cross-goroutine arrival order under saturation was never defined.
//
// Re-contention is fair enough between single commands, which all need one
// slot, but not between a single command and a BATCH (pushBatch), which needs
// its whole length free at one instant. The writer frees at most a wave per
// take while singles refill any free slot, so under sustained load a batch
// longer than a wave would never see enough room. A batch that is refused
// therefore RESERVES its slots (hold): reserved slots count as occupied for
// every other push, so the writer's takes accumulate room for the batch
// instead of handing it straight back to the singles. Holders form a FIFO
// line and only the HEAD can be admitted (pushHeld): a later, smaller batch
// would otherwise refill the head's room exactly as the singles did. Each
// holder has its own wake channel and only the head is ever signalled, so no
// wake is shared, passed along, or lost between waiters.
type fdQueue struct {
	mu sync.Mutex
	// buf[head:] is the live queue. takeInto advances head instead of shifting
	// the remainder down, so a take costs O(batch) rather than O(queue depth):
	// at a full default queue (65536) a shift moved ~6.8 MB under this lock per
	// 200-command wave. The dead prefix is reclaimed when the queue empties, or
	// by compaction once it is at least as long as the live part, so a
	// compaction never copies more than was taken since the last one. Until
	// then an append that outgrows buf grows it (doubling), so buf can reach
	// about twice the live depth.
	buf  []fdReq
	head int
	max  int // bound; mirrors the old channel capacity

	// holders are the batches that were refused for lack of room and are
	// waiting to be admitted, in arrival order (see hold/pushHeld). reserved
	// is the sum of their sizes; it counts as occupied for push and for a
	// non-holding pushBatch, so neither a single command nor a new batch can
	// refill the room the writer frees ahead of a waiting batch. Only
	// holders[0] can be admitted, and it needs just its own length free: the
	// sum of reservations may exceed the capacity, so a rule that made the
	// head wait for ALL reservations to fit could never be satisfied. Every
	// event that may let the head in (a take, a drain, the previous head
	// leaving) signals holders[0].wake; closeQueue signals every holder.
	holders  []fdHolder
	reserved int
	nextID   uint64

	// parked/wakeAt are the writer's standing request: it is asleep and wants a
	// signal once the queue holds at least wakeAt entries. Only a submitter that
	// takes parked from true to false sends the signal, so a wave of arrivals
	// produces exactly one wake.
	parked bool
	wakeAt int

	wake chan struct{} // cap 1: submitter -> parked writer
	room chan struct{} // cap 1: writer -> single submitters blocked on a full queue

	closed bool
}

// fdQueueReady is an always-ready receive. The writer selects on it instead of
// the wake channel when park() reported that work is already queued, so the
// "already had work" and "waited for work" paths share one select and one copy
// of the batch-building code.
var fdQueueReady = func() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}()

// fdHolder is a batch waiting for room with a reservation of n slots. The id
// identifies it to pushHeld and unhold. wake (cap 1) is signalled when the
// holder is at the head of the line and may fit, and on close. A holder behind
// the head is never woken by room events: nothing it could do with the wake.
type fdHolder struct {
	id   uint64
	n    int
	wake chan struct{}
}

// fdPushResult is the outcome of an enqueue attempt.
type fdPushResult int

const (
	fdPushOK     fdPushResult = iota
	fdPushFull                // bound reached; caller waits on roomCh and retries
	fdPushClosed              // engine shut down; caller fails the command
)

func newFDQueue(max int) *fdQueue {
	if max < 1 {
		max = 1
	}
	return &fdQueue{
		buf:  make([]fdReq, 0, 64), // grows to the live depth, not to max up front
		max:  max,
		wake: make(chan struct{}, 1),
		room: make(chan struct{}, 1),
	}
}

// NIL-QUEUE CONTRACT. Every method below tolerates a nil receiver, and together
// they reproduce the semantics of the nil `chan fdReq` this type replaced: a nil
// channel is never ready, so a receive on it blocks forever and a send on it
// blocks forever. That is not a defensive flourish — fdEngine is constructed as a
// bare struct literal in several tests that exercise session()/attempt() without
// a queue, and those tests depend on the submit arm simply never firing. So:
//
//	wakeCh/roomCh  nil channel  -> that select arm is never ready
//	park           true         -> "parked", and the nil wakeCh never wakes us
//	takeInto       no-op        -> no work ever arrives
//	push           fdPushFull   -> submit waits on the nil roomCh, i.e. forever,
//	                               leaving only its ctx.Done() arms, exactly as a
//	                               blocked send on a nil channel did
func (q *fdQueue) wakeCh() <-chan struct{} {
	if q == nil {
		return nil
	}
	return q.wake
}

func (q *fdQueue) roomCh() <-chan struct{} {
	if q == nil {
		return nil
	}
	return q.room
}

// capacity is the queue bound, 0 for a nil queue.
func (q *fdQueue) capacity() int {
	if q == nil {
		return 0
	}
	return q.max
}

// live is the number of queued commands. Caller holds mu.
func (q *fdQueue) live() int { return len(q.buf) - q.head }

// compact moves the live queue to the front of buf and clears the vacated tail,
// so no stale fdReq pins a Cmder. Caller holds mu.
func (q *fdQueue) compact() {
	if q.head == 0 {
		return
	}
	n := copy(q.buf, q.buf[q.head:])
	clear(q.buf[n:])
	q.buf = q.buf[:n]
	q.head = 0
}

// fdQueueRetainCap bounds the backing array an EMPTY queue keeps. A burst (a
// stalled peer backing the queue up to the window, 65536 by default) grows buf
// to several MiB; keeping that for the life of an idle engine would pin the
// burst's high-water memory. A queue that routinely runs deeper than this is
// re-grown by append, which doubles, so the release is not paid per wave.
const fdQueueRetainCap = 4096

// resetEmpty starts an empty queue over from slot 0, releasing a backing
// array a burst grew past fdQueueRetainCap. Caller holds mu and has already
// cleared the slots.
func (q *fdQueue) resetEmpty() {
	if cap(q.buf) > fdQueueRetainCap {
		q.buf = make([]fdReq, 0, 64)
	} else {
		q.buf = q.buf[:0]
	}
	q.head = 0
}

// reserve compacts before an append that would outgrow buf, when the dead
// prefix is long enough to pay for it; otherwise the append grows buf.
// Caller holds mu.
func (q *fdQueue) reserve(k int) {
	if len(q.buf)+k > cap(q.buf) && q.head >= q.live() {
		q.compact()
	}
}

// signalRoom re-arms the cap-1 room signal. Called by a submitter that just
// woke from a full queue, to chain the wake to the next one waiting, and by a
// holder whose reservation stops counting against the singles.
func (q *fdQueue) signalRoom() {
	if q == nil {
		return
	}
	select {
	case q.room <- struct{}{}:
	default:
	}
}

// signalHolder wakes one holder. Non-blocking on its cap-1 channel, so a
// pending wake is not doubled; nil (no holder) is a no-op. Called OUTSIDE mu.
func signalHolder(ch chan struct{}) {
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// headWakeLocked returns the wake channel of the head of the holder line, or
// nil when the line is empty. Caller holds mu; the send happens after unlock.
func (q *fdQueue) headWakeLocked() chan struct{} {
	if len(q.holders) == 0 {
		return nil
	}
	return q.holders[0].wake
}

// signalRoomAndHead releases the waiters a take or drain may have made room
// for: one single on room, and the head of the holder line on its own wake.
// Called OUTSIDE mu with head read under it.
func (q *fdQueue) signalRoomAndHead(head chan struct{}) {
	q.signalRoom()
	signalHolder(head)
}

// hold reserves k slots for a batch that was refused for lack of room and
// puts it at the back of the holder line. Until the holder is admitted
// (pushHeld) or gives up (unhold), the slots count as occupied for every other
// push, so the room the writer frees accumulates for the holders. k is bounded
// by the caller to the queue capacity. Returns the holder's id and the channel
// it must wait on; on a nil queue both are zero and the nil channel never
// fires (see the nil-queue contract).
func (q *fdQueue) hold(k int) (id uint64, wake <-chan struct{}) {
	if q == nil || k <= 0 {
		return 0, nil
	}
	ch := make(chan struct{}, 1)
	q.mu.Lock()
	q.nextID++
	id = q.nextID
	q.holders = append(q.holders, fdHolder{id: id, n: k, wake: ch})
	q.reserved += k
	q.mu.Unlock()
	return id, ch
}

// unhold releases the reservation of a holder that gave up (ctx done,
// shutdown). It wakes a single the reservation may have been holding back,
// and, if the holder was at the head of the line, the new head, which may fit.
// A holder behind the head leaves without a trace: it was never signalled, so
// no wake is lost with it.
func (q *fdQueue) unhold(id uint64) {
	if q == nil {
		return
	}
	q.mu.Lock()
	wasHead, found := q.dropHolderLocked(id)
	wakeSingles := found && q.live()+q.reserved < q.max
	var head chan struct{}
	if wasHead {
		head = q.headWakeLocked()
	}
	q.mu.Unlock()
	if wakeSingles {
		q.signalRoom()
	}
	signalHolder(head)
}

// dropHolderLocked removes the holder with id from the line and releases
// its slots. Caller holds mu.
func (q *fdQueue) dropHolderLocked(id uint64) (wasHead, found bool) {
	for i, h := range q.holders {
		if h.id != id {
			continue
		}
		q.reserved -= h.n
		q.holders = append(q.holders[:i], q.holders[i+1:]...)
		if len(q.holders) == 0 {
			q.holders = nil // let a burst's line be collected
		}
		return i == 0, true
	}
	return false, false
}

// roomFor reports whether n more single commands fit, net of reservations.
func (q *fdQueue) roomFor(n int) bool {
	if q == nil {
		return false
	}
	q.mu.Lock()
	ok := q.max-q.live()-q.reserved >= n
	q.mu.Unlock()
	return ok
}

// push enqueues one command. The wake signal is sent OUTSIDE the mutex: it is a
// non-blocking send on a cap-1 channel, but it can call goready, and holding the
// submit mutex across a scheduler operation would convoy every other submitter
// behind it.
func (q *fdQueue) push(req fdReq) fdPushResult {
	if q == nil {
		return fdPushFull // see the nil-queue contract
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return fdPushClosed
	}
	if q.live()+q.reserved >= q.max {
		q.mu.Unlock()
		return fdPushFull
	}
	q.reserve(1)
	q.buf = append(q.buf, req)
	signal := q.parked && q.live() >= q.wakeAt
	if signal {
		q.parked = false // claim the wake: later arrivals in this wave stay silent
	}
	q.mu.Unlock()
	if signal {
		q.signalWake()
	}
	return fdPushOK
}

// pushBatch enqueues reqs under a SINGLE lock, for callers that already hold a
// run of commands (a pipeline, or a carry tail being returned to the queue).
// All-or-nothing: a partial enqueue would split a pipeline across two flushes
// and, worse, leave the caller unsure which half it owns. Slots reserved by
// waiting holders count as occupied; a refused caller that wants to wait
// should hold its own slots and retry through pushHeld.
func (q *fdQueue) pushBatch(reqs []fdReq) fdPushResult {
	if len(reqs) == 0 {
		return fdPushOK
	}
	if q == nil {
		return fdPushFull // see the nil-queue contract
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return fdPushClosed
	}
	if q.live()+q.reserved+len(reqs) > q.max {
		q.mu.Unlock()
		return fdPushFull
	}
	signal := q.appendLocked(reqs)
	q.mu.Unlock()
	if signal {
		q.signalWake()
	}
	return fdPushOK
}

// pushHeld is pushBatch for the holder with id, whose reservation is
// len(reqs) slots. Only the head of the holder line is admitted; anyone else
// is refused (fdPushFull) and waits on its own wake, which fires once it has
// become the head and may fit. The head needs only its own length free: the
// slots behind it are reserved, not occupied, and counting them would let the
// line deadlock once the reservations outgrow the capacity. On admission the
// reservation is consumed, a single the reservation was holding back is woken
// if room remains, and the next holder is woken to try in turn.
func (q *fdQueue) pushHeld(reqs []fdReq, id uint64) fdPushResult {
	if q == nil {
		return fdPushFull // see the nil-queue contract
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return fdPushClosed
	}
	if len(q.holders) == 0 || q.holders[0].id != id || q.live()+len(reqs) > q.max {
		q.mu.Unlock()
		return fdPushFull
	}
	signal := q.appendLocked(reqs)
	q.dropHolderLocked(id)
	wakeSingles := q.live()+q.reserved < q.max
	head := q.headWakeLocked()
	q.mu.Unlock()
	if signal {
		q.signalWake()
	}
	if wakeSingles {
		q.signalRoom()
	}
	signalHolder(head)
	return fdPushOK
}

// appendLocked appends an admitted run and reports whether the parked writer
// must be woken. Caller holds mu and has checked the bound.
func (q *fdQueue) appendLocked(reqs []fdReq) (signal bool) {
	q.reserve(len(reqs))
	q.buf = append(q.buf, reqs...)
	signal = q.parked && q.live() >= q.wakeAt
	if signal {
		q.parked = false
	}
	return signal
}

// signalWake sends the writer wake claimed under mu. Outside the lock: see push.
func (q *fdQueue) signalWake() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// pushFront returns commands the WRITER already took but could not write this
// round (the MaxBatchBytes cap trips mid-wave) to the head of the queue, so they
// go out first and FIFO order is exact. Writer-only: there is one writer, it
// holds no other lock here, and the commands were already admitted, so this is
// not a new enqueue and is deliberately not bounded by max — total outstanding
// work is unchanged.
func (q *fdQueue) pushFront(reqs []fdReq) {
	// Unreachable on a nil queue: takeInto yields nothing there, so the writer
	// never holds a tail to give back.
	if q == nil || len(reqs) == 0 {
		return
	}
	q.mu.Lock()
	if q.head >= len(reqs) {
		// The freed slots before head take them in place.
		q.head -= len(reqs)
		copy(q.buf[q.head:], reqs)
	} else {
		q.buf = append(append(make([]fdReq, 0, len(reqs)+q.live()), reqs...), q.buf[q.head:]...)
		q.head = 0
	}
	q.mu.Unlock()
}

// takeInto moves up to max queued commands onto dst and returns the extended
// slice: one lock and one bulk copy for the whole wave, proportional to the
// wave, not to what stays queued.
//
// The taken slots are cleared, because the backing array outlives them and a
// stale fdReq pins a Cmder, a context and an apBatch. That is 104 bytes per slot
// of memclr, against the per-command lock and copy the channel charged.
// headRun counts the leading queued requests for which match is true. With
// takeInto it lets the engine goroutine take the queued tail of a pipeline:
// only that goroutine removes from the head, so the run cannot change in
// between.
func (q *fdQueue) headRun(match func(fdReq) bool) int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	n := 0
	for q.head+n < len(q.buf) && match(q.buf[q.head+n]) {
		n++
	}
	q.mu.Unlock()
	return n
}

func (q *fdQueue) takeInto(dst []fdReq, max int) []fdReq {
	if q == nil || max <= 0 {
		return dst
	}
	q.mu.Lock()
	n := q.live()
	if n == 0 {
		q.mu.Unlock()
		return dst
	}
	if n > max {
		n = max
	}
	dst = append(dst, q.buf[q.head:q.head+n]...)
	clear(q.buf[q.head : q.head+n])
	q.head += n
	switch {
	case q.head == len(q.buf):
		q.resetEmpty()
	case q.head >= q.live():
		q.compact()
	}
	q.parked = false // we are awake; stop submitters from signalling
	head := q.headWakeLocked()
	q.mu.Unlock()
	// Release anyone blocked on a full queue. Non-blocking on cap-1 channels, so
	// this is a cheap no-op once a signal is already pending.
	q.signalRoomAndHead(head)
	return dst
}

// park registers the writer's intent to sleep until the queue holds at least
// minDepth commands. It returns false when the queue ALREADY has that many, in
// which case the writer must take instead of waiting — checking under the same
// lock that a submitter needs to signal is what makes the wake lossless.
//
// minDepth > 1 is only safe when the caller ALSO waits on a timer: submitters
// stay silent below the threshold, so a wave that never reaches minDepth must be
// released by something other than a push.
func (q *fdQueue) park(minDepth int) bool {
	if q == nil {
		return true // "parked" on a nil wakeCh: that arm never fires
	}
	if minDepth < 1 {
		minDepth = 1
	}
	q.mu.Lock()
	if q.closed || q.live() >= minDepth {
		q.parked = false
		q.mu.Unlock()
		return false
	}
	q.parked = true
	q.wakeAt = minDepth
	q.mu.Unlock()
	return true
}

// unpark withdraws a standing park request. The writer calls it after any wait
// that did NOT end in a push signal (timer, idle, max-hold, close), so a later
// arrival does not send a wake nobody is listening for.
func (q *fdQueue) unpark() {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.parked = false
	q.mu.Unlock()
}

func (q *fdQueue) depth() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	n := q.live()
	q.mu.Unlock()
	return n
}

// drainAll removes and returns everything queued. Used by the shutdown and
// fail-backlog paths, which replace the channel's `for { select { case r := <-ch:
// default: return } }` drains.
func (q *fdQueue) drainAll(dst []fdReq) []fdReq {
	if q == nil {
		return dst
	}
	q.mu.Lock()
	dst = append(dst, q.buf[q.head:]...)
	clear(q.buf)
	q.resetEmpty()
	q.parked = false
	head := q.headWakeLocked()
	q.mu.Unlock()
	q.signalRoomAndHead(head)
	return dst
}

// closeQueue rejects further pushes. The engine's submitMu still orders this
// against in-flight submits exactly as it ordered the old channel drain.
func (q *fdQueue) closeQueue() {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.closed = true
	// Every holder, not just the head: a holder behind the head is otherwise
	// woken only by its ctx, and Close must release it.
	wakes := make([]chan struct{}, len(q.holders))
	for i, h := range q.holders {
		wakes[i] = h.wake
	}
	q.mu.Unlock()
	// Release submitters blocked on a full queue, singles and holders alike, so
	// they observe the closed state.
	q.signalRoom()
	for _, ch := range wakes {
		signalHolder(ch)
	}
}
