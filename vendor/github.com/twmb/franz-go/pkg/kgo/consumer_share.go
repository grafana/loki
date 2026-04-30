package kgo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo/internal/xsync"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type (
	shareConsumer struct {
		c   *consumer
		cl  *Client
		cfg *cfg

		fm fetchManager

		// Our subscribed topic set and their partition metadata,
		// updated atomically after metadata responses. The keys are
		// the topic names we send as SubscribedTopicNames in the
		// heartbeat; the values hold per-partition state including
		// the shareCursor and its current source. What we ACTUALLY
		// consume is whatever the broker assigns us out of this set,
		// tracked in nowAssigned.
		tps *topicsPartitions

		reSeen map[string]bool // topics we evaluated against regex, and whether we want them

		memberGen   groupMemberGen
		nowAssigned amtps

		// topic IDs from heartbeat responses that we cannot yet map to
		// names. Owned by the manage goroutine.
		unresolvedAssigns map[topicID][]int32

		lastSentSubscribedTopics []string
		lastSentRack             bool

		// ackMu/ackC/pendingAcks: used by FlushAcks to wait for
		// all in-flight acks to drain. pendingAcks is an
		// atomic counter (free reads/writes on the Record.Ack
		// hot path); ackMu+ackC are only taken when waking
		// FlushAcks waiters (broadcast happens under mu only
		// when pendingAcks reaches 0, to keep the cond
		// broadcast and the wait-loop's Load consistent).
		ackMu       xsync.Mutex
		ackC        *sync.Cond
		pendingAcks atomic.Int64

		// callbackRing serializes shareAckCallback invocations
		// via the same ring + spawn-on-empty pattern that
		// producer.go uses for batchPromises. Each entry carries
		// a pendingAcks count that is subtracted AFTER the user
		// callback returns, so FlushAcks blocks until callbacks
		// have completed.
		callbackRing ring[shareCallbackEntry]

		// Records returned by the last poll. On the next poll, any
		// record still pending (not acked) or renewed is auto-accepted.
		// Access serialized via sc.c.mu (same lock the classic consumer
		// uses for its poll path; also held by purgeTopics and metadata).
		lastPolled []*Record

		//////////////
		// mu block //
		//////////////

		mu       xsync.Mutex
		cond     *sync.Cond
		dying    bool // single-shot leave guard + incWorker gate
		workers  int  // active goroutines (manage + source loops)
		left     chan struct{}
		leaveErr error

		manageRunning atomic.Bool // CAS prevents double-spawn
	}

	shareCursor struct {
		topic     string
		topicID   [16]byte
		partition int32

		// source is atomic to support cursors moving between
		// sources concurrent with user acking.
		source atomic.Pointer[source]

		cursorsIdx int

		// assigned is true when the cursor's partition is currently
		// part of our share-group assignment (via assignPartitions),
		// false otherwise (default state or via revoke).
		//
		// Unlike classic source's cursor.useState, assigned is NOT
		// flipped during request build. The fetch loop is single-
		// threaded per source and the sem mechanism prevents
		// concurrent fetches, so the request-time flip-and-restore
		// dance from classic is unnecessary here and orchestrating
		// a similar flow actually makes the code worse.
		assigned atomic.Bool

		ackMu       xsync.Mutex
		pendingAcks []shareAckEntry // user acks (r.Ack, finalizePreviousPoll, batchAckRecords)
		pendingGaps []shareAckRange // internal acks (gap acks, release-undeliverable)
		closed      bool            // set to reject user-side acks arriving after shutdown
	}

	// AckStatus defines how the broker should handle an acquired share group
	// record: accept, release for redelivery, reject, or renew the lock.
	// The zero value is not valid; MarkAcks and Record.Ack silently
	// ignore it.
	AckStatus int8

	// ShareAckResult is a per-partition result from a share group acknowledge;
	// Err is nil on success.
	ShareAckResult struct {
		Topic     string
		Partition int32
		Err       error
	}

	// ShareAckResults are the per-partition outcomes from one share-group
	// acknowledge, as reported by the broker. Use [ShareAckResults.Ok] for
	// an all-or-nothing check, or iterate the slice directly for
	// per-partition handling. A nil [ShareAckResult.Err] means the broker
	// successfully processed that partition's acks; a non-nil Err means the
	// broker rejected them, the client could not deliver them, or the
	// broker omitted the partition from its response.
	ShareAckResults []ShareAckResult

	// shareMove records a partition that should be moved to a new leader,
	// collected from CurrentLeader hints in ShareFetch responses.
	shareMove struct {
		topicID   [16]byte
		partition int32
		leaderID  int32
	}

	// shareAckEntry is a per-record user ack.
	//
	// Every successful r.Ack CAS appends one entry and increments
	// sc.pendingAcks. Multiple entries for the same offset can occur
	// (e.g. AckRenew then a terminal CAS-overrides; both calls
	// append). buildAckRanges' dedupe loop collapses these into a
	// single wire range so the broker doesn't see duplicate
	// AcknowledgementBatches at the same offset (which would be
	// rejected with INVALID_RECORD_STATE). The pendingAcks counter
	// still tracks one increment per appended entry; subtraction at
	// callback time uses the entry count (post-stale-filter), not
	// the wire-range count, so the math stays correct.
	shareAckEntry struct {
		offset       int64
		source       *source
		status       *atomic.Int32 // live ack status; read at request-build time
		sessionEpoch int32
	}

	// shareAckRange is a contiguous offset range with a fixed ack
	// type. Used for:
	//   - wire format: merged ranges sent in ShareFetch/ShareAcknowledge
	//   - internal acks: gap acks and release-undeliverable (cursor's
	//     pendingGaps queue); these have no status pointer because
	//     the user never saw the records
	//
	// source and sessionEpoch are used by the staleness filter to
	// drop acks the broker would reject (session reset or cursor
	// migration).
	shareAckRange struct {
		firstOffset  int64
		lastOffset   int64
		source       *source
		sessionEpoch int32
		ackType      int8 // uniform type for the entire range
	}

	// shareAckState is per-record ack state (8 bytes).
	shareAckState struct {
		status        atomic.Int32
		deliveryCount int32 // broker's delivery count for this record (>= 1)
	}

	// shareAckSlab holds slab-allocated shareAckState objects for all
	// records from one batch within a ShareFetch partition decode. One
	// slab per batch is stored in the records' context via
	// batchContext. Records within a batch are contiguous in memory
	// (from a single []Record allocation), so pointer arithmetic from
	// records0 gives the correct slab index.
	//
	// Per-slab fields (uniform across all records in the batch):
	//   ackSource    - source that decoded the records (filters acks on source migration)
	//   cursor       - shareCursor for this partition (routes to the correct source)
	//   sessionEpoch - ackSource's session epoch at decode (filters acks on session reset)
	//   acqLockDeadlineNanos - broker's acquisition lock deadline (wall clock)
	shareAckSlab struct {
		states               []shareAckState
		records0             *Record // first record in the batch's backing array
		ackSource            *source
		cursor               *shareCursor
		acqLockDeadlineNanos int64
		sessionEpoch         int32
	}

	// shareCallbackEntry is pushed onto the callbackRing. The drainer
	// invokes the user callback, then subtracts nAcks from
	// sc.pendingAcks. This ensures FlushAcks blocks until all
	// callbacks have completed.
	shareCallbackEntry struct {
		results ShareAckResults
		nAcks   int64
	}

	// shareFetchResult is returned by handleShareReqResp for
	// shareFetch to act on at the request level.
	shareFetchResult struct {
		fetch           Fetch
		moves           []shareMove
		moveBrokers     []BrokerMetadata // NodeEndpoints from response; used by applyMoves to add unknown brokers
		ackResults      ShareAckResults
		ackRequeued     int64
		discardErr      error // non-nil: response thrown away, error all pending acks
		allErrsStripped bool  // every partition had errors, nothing buffered
	}

	// cursorAckDrain is a cursor + the entries/gaps drained from it.
	cursorAckDrain struct {
		cursor  *shareCursor
		entries []shareAckEntry // user acks
		gaps    []shareAckRange // internal acks (gap/release)
	}

	// cursorAcks accumulates per-record user ack entries for a cursor.
	cursorAcks struct {
		entries []shareAckEntry
		n       int64
	}

	cursorsAcks map[*shareCursor]*cursorAcks // groups cursorAcks by many cursors.
)

// isShareAckRetryable returns true only for errors where retrying the ack
// on the same broker could succeed. Narrower than kerr.IsRetriable:
// leader-change errors (NotLeaderForPartition, FencedLeaderEpoch) are
// excluded because the acquisition lock was released at leader change;
// unknown-topic errors (UnknownTopicOrPartition, UnknownTopicID) are
// excluded because the records will be re-delivered via broker-side
// acquisition-lock timeout.
func isShareAckRetryable(err error) bool {
	if !kerr.IsRetriable(err) {
		return false
	}
	switch {
	case errors.Is(err, kerr.NotLeaderForPartition),
		errors.Is(err, kerr.FencedLeaderEpoch),
		errors.Is(err, kerr.UnknownTopicOrPartition),
		errors.Is(err, kerr.UnknownTopicID):
		return false
	}
	return true
}

func (m *cursorsAcks) add(cursor *shareCursor, entry shareAckEntry, lastCursor *shareCursor, lastCa *cursorAcks) (*shareCursor, *cursorAcks) {
	ca := lastCa
	if cursor != lastCursor || ca == nil {
		if *m == nil {
			*m = make(cursorsAcks)
		}
		ca = (*m)[cursor]
		if ca == nil {
			ca = &cursorAcks{}
			(*m)[cursor] = ca
		}
	}
	ca.entries = append(ca.entries, entry)
	ca.n++
	return cursor, ca
}

const (
	// AckAccept marks a record as successfully processed. The broker
	// advances the share group's cursor past this record.
	AckAccept AckStatus = 1

	// AckRelease releases a record back to the broker for redelivery
	// to another consumer. The delivery count is incremented.
	AckRelease AckStatus = 2

	// AckReject marks a record as permanently unprocessable. The broker
	// archives the record and does not redeliver it.
	AckReject AckStatus = 3

	// AckRenew extends the acquisition lock on a record without completing
	// it (KIP-1222, requires Kafka 4.2+). Renews do not persist across
	// polls: any renewed record that is not explicitly acked with a
	// terminal status before the next [Client.PollRecords] is
	// auto-accepted. On client close, any records still in AckRenew
	// state are automatically converted to AckRelease so the broker
	// can redeliver them immediately.
	AckRenew AckStatus = 4
)

func (s AckStatus) String() string {
	switch s {
	case AckAccept:
		return "accept"
	case AckRelease:
		return "release"
	case AckReject:
		return "reject"
	case AckRenew:
		return "renew"
	default:
		return fmt.Sprintf("AckStatus(%d)", s)
	}
}

// Error returns the first non-nil error in the results, or nil.
func (rs ShareAckResults) Error() error {
	for _, r := range rs {
		if r.Err != nil {
			return r.Err
		}
	}
	return nil
}

// Ok returns true if all results succeeded.
func (rs ShareAckResults) Ok() bool {
	return rs.Error() == nil
}

/////////////////////////
// INIT / LEAVE / POLL //
/////////////////////////

func (c *consumer) initShare() {
	ctx, cancel := context.WithCancel(c.cl.ctx)
	sc := &shareConsumer{
		c:   c,
		cl:  c.cl,
		cfg: &c.cl.cfg,

		fm: newFetchManager(ctx, cancel, &c.pollActive, c.pollWake, c.cl.cfg.maxConcurrentFetches),

		reSeen: make(map[string]bool),
		tps:    newTopicsPartitions(),

		left: make(chan struct{}),
	}
	sc.ackC = sync.NewCond(&sc.ackMu)
	sc.cond = sync.NewCond(&sc.mu)
	c.s = sc

	if len(sc.cfg.topics) > 0 && !sc.cfg.regex {
		topics := make([]string, 0, len(sc.cfg.topics))
		for topic := range sc.cfg.topics {
			topics = append(topics, topic)
		}
		sc.tps.storeTopics(topics)
	}
}

// poll is the share consumer's implementation of PollRecords, with the added
// behavior of auto-accepting records from the prior poll that were not
// explicitly ack'd or that were only renewed (AckRenew does not persist
// across polls).
func (sc *shareConsumer) poll(ctx context.Context, maxPollRecords int) Fetches {
	// Guard race conditions in case people use the API incorrectly
	// (concurrent leave with poll).
	//
	// Our lock pattern is slightly different than Client.PollRecords
	// because of, well, other races (finalizePreviousPoll, etc).
	sc.c.mu.Lock()
	sc.finalizePreviousPoll()

	var fetches Fetches
	fill := func() {
		paused := sc.c.loadPaused()

		sc.c.sourcesReadyMu.Lock()
		if maxPollRecords < 0 {
			for _, s := range sc.c.sourcesReadyForDraining {
				fetches = append(fetches, s.share.takeBuffered(paused))
			}
			sc.c.sourcesReadyForDraining = nil
		} else {
			remaining := maxPollRecords
			for len(sc.c.sourcesReadyForDraining) > 0 && remaining > 0 {
				s := sc.c.sourcesReadyForDraining[0]
				f, taken, drained := s.share.takeNBuffered(paused, remaining)
				if drained {
					sc.c.sourcesReadyForDraining = sc.c.sourcesReadyForDraining[1:]
				}
				remaining -= taken
				fetches = append(fetches, f)
			}
		}
		fetches = append(fetches, sc.c.fakeReadyForDraining...)
		sc.c.fakeReadyForDraining = nil
		sc.c.sourcesReadyMu.Unlock()

		if len(fetches) > 0 {
			sc.trackLastPolled(fetches)
		}
	}

	fill()
	sc.c.mu.Unlock()
	if len(fetches) > 0 || ctx == nil {
		return fetches
	}

	done := make(chan struct{})
	quit := false
	go func() {
		sc.c.sourcesReadyMu.Lock()
		defer sc.c.sourcesReadyMu.Unlock()
		defer close(done)
		for !quit && len(sc.c.sourcesReadyForDraining) == 0 && len(sc.c.fakeReadyForDraining) == 0 {
			sc.c.sourcesReadyCond.Wait()
		}
	}()

	exit := func() {
		sc.c.sourcesReadyMu.Lock()
		quit = true
		sc.c.sourcesReadyMu.Unlock()
		sc.c.sourcesReadyCond.Broadcast()
	}

	select {
	case <-sc.fm.ctx.Done():
		exit()
		return NewErrFetch(ErrClientClosed)
	case <-ctx.Done():
		exit()
		return NewErrFetch(ctx.Err())
	case <-done:
		sc.c.mu.Lock()
		fill()
		sc.c.mu.Unlock()
	}

	return fetches
}

// finalizePreviousPoll routes any outstanding ack state from records returned
// by the previous poll. Acks are collected per-cursor and merged into
// each cursor's pendingAcks, handling leader migration transparently.
//
// After this runs, every record from the previous poll is terminal and
// lastPolled is cleared. Note: Record.Ack(AckRenew) only extends the lock,
// renews do not persist across polls.
func (sc *shareConsumer) finalizePreviousPoll() {
	if len(sc.lastPolled) == 0 {
		return
	}

	var (
		byCursor   cursorsAcks
		lastCursor *shareCursor
		lastCa     *cursorAcks
	)

	for _, r := range sc.lastPolled {
		slab, st := shareAckFromCtx(r)
		if st == nil {
			continue
		}
		ok := false
		for { // unacked or renew get auto accepted
			cur := st.status.Load()
			if cur != 0 && cur != int32(AckRenew) {
				break
			}
			if st.status.CompareAndSwap(cur, int32(AckAccept)) {
				ok = true
				break
			}
		}
		if !ok {
			continue
		}
		lastCursor, lastCa = byCursor.add(slab.cursor, shareAckEntry{
			offset:       r.Offset,
			source:       slab.ackSource,
			status:       &st.status,
			sessionEpoch: slab.sessionEpoch,
		}, lastCursor, lastCa)
	}
	clear(sc.lastPolled)
	sc.lastPolled = sc.lastPolled[:0]

	sc.enqueueAllAcks(byCursor)
}

func (sc *shareConsumer) trackLastPolled(fetches Fetches) {
	total := fetches.NumRecords()
	sc.lastPolled = slices.Grow(sc.lastPolled[:0], total)[:0]
	for i := range fetches {
		f := &fetches[i]
		for j := range f.Topics {
			t := &f.Topics[j]
			for k := range t.Partitions {
				p := &t.Partitions[k]
				sc.lastPolled = append(sc.lastPolled, p.Records...)
			}
		}
	}
}

// MarkAcks sets the acknowledgement status for share group records.
// If no records are provided, all records from the last poll that have
// not already been marked are marked with the given status.
// If specific records are provided, the status is applied to each
// using the same per-record CAS rules as [Record.Ack]: terminal
// statuses override an existing [AckRenew] but not another terminal,
// and a renew is rejected if the record is already in any non-zero
// state.
func (cl *Client) MarkAcks(status AckStatus, rs ...*Record) {
	if status < AckAccept || status > AckRenew {
		return // unknown AckStatus value; silently ignore
	}
	sc := cl.consumer.s
	if sc == nil {
		return
	}
	if len(rs) > 0 {
		batchAckRecords(sc, rs, status, nil)
		return
	}
	// Fill in the gaps: only touch records that are still PENDING.
	// This preserves any explicit ack the caller already set.
	//
	// c.mu serializes lastPolled access against PollRecords and
	// sc.leave's reject pass; without it, a -race run with a user
	// who wrongly calls MarkAcks concurrently with PollRecords
	// would flag a data race on the slice header.
	sc.c.mu.Lock()
	defer sc.c.mu.Unlock()
	batchAckRecords(sc, sc.lastPolled, status, func(st *shareAckState) bool {
		return st.status.Load() == 0
	})
}

// FlushAcks synchronously flushes all pending share group acks. It
// blocks until the pending-ack counter reaches zero (every queued ack
// has been sent to its broker and the corresponding [ShareAckCallback]
// has been invoked), or the ctx is canceled.
//
// Do not call FlushAcks from inside a [ShareAckCallback].
func (cl *Client) FlushAcks(ctx context.Context) error {
	sc := cl.consumer.s
	if sc == nil {
		return nil
	}

	// Signal all sources to flush immediately.
	cl.allSources(func(s *source) {
		s.signalShareAckFlush()
	})

	// Wait for all pending acks to drain, or context cancellation.
	quit := false
	done := make(chan struct{})
	go func() {
		sc.ackMu.Lock()
		defer sc.ackMu.Unlock()
		defer close(done)
		for !quit && sc.pendingAcks.Load() > 0 {
			sc.ackC.Wait()
		}
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		sc.ackMu.Lock()
		quit = true
		sc.ackMu.Unlock()
		sc.ackC.Broadcast()
		return ctx.Err()
	}
}

// leave performs the graceful share-group shutdown:
//
//  1. Set dying (single-shot guard + blocks new goroutines).
//  2. Cancel fm.ctx so manage + source loops exit.
//  3. Wait for all workers (manage + source loops) to reach 0.
//  4. Release lastPolled and buffered records, drain fetchManager slots.
//  5. Per-source FINAL_EPOCH ShareFetch with remaining acks.
//  6. Leave heartbeat (MemberEpoch=-1).
//
// Called from a fresh goroutine by LeaveGroupContext. The caller
// selects on sc.left or ctx.Done.
func (sc *shareConsumer) leave(ctx context.Context) {
	sc.mu.Lock()
	if sc.dying {
		sc.mu.Unlock()
		return
	}
	sc.dying = true
	sc.mu.Unlock()
	defer close(sc.left)

	// Cancel fm.ctx: manage and source loops all select on
	// fm.ctx.Done and exit promptly.
	sc.fm.cancel()

	// Wait for every worker (manage + source loops) to exit.
	// After this we have exclusive access to all share state.
	sc.mu.Lock()
	for sc.workers > 0 {
		sc.cond.Wait()
	}
	sc.mu.Unlock()

	// A user PollRecords could still be in flight: even after
	// fm.cancel, the non-deterministic select may wake on the
	// sourcesReady channel instead of fm.ctx.Done. c.mu blocks
	// poll entirely. Under it we clear the source queue (so any
	// racing poll sees nothing -- the records are still on
	// s.share.buffered; closeShareSession finds and releases them)
	// and release unacked lastPolled records (AckRelease so another
	// consumer gets them, not auto-accept).
	sc.c.mu.Lock()
	sc.c.sourcesReadyMu.Lock()
	sc.c.sourcesReadyForDraining = nil
	sc.c.sourcesReadyMu.Unlock()
	if len(sc.lastPolled) > 0 {
		batchAckRecords(sc, sc.lastPolled, AckRelease, func(st *shareAckState) bool {
			return st.status.Load() == 0
		})
		sc.lastPolled = nil
	}
	sc.c.mu.Unlock()

	// Per-source: release buffered records, drain fetchManager
	// slots, then close the share session (FINAL_EPOCH ShareAcknowledge
	// with all remaining acks piggybacked).
	var wg sync.WaitGroup
	sc.cl.allSources(func(s *source) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.closeShareSession(ctx)
		}()
	})
	wg.Wait()

	// Leave heartbeat.
	memberID, _ := sc.memberGen.load()
	if memberID == "" {
		return
	}
	sc.cfg.logger.Log(LogLevelInfo, "leaving share group",
		"group", sc.cfg.shareGroup,
		"member_id", memberID,
	)
	req := kmsg.NewPtrShareGroupHeartbeatRequest()
	req.GroupID = sc.cfg.shareGroup
	req.MemberID = memberID
	req.MemberEpoch = -1
	resp, err := req.RequestWith(ctx, sc.cl)
	if err != nil {
		sc.leaveErr = err
		return
	}
	sc.leaveErr = errCodeMessage(resp.ErrorCode, resp.ErrorMessage)
}

// closeShareSession releases any buffered records on this source,
// drains fetchManager slots, then sends a FINAL_EPOCH ShareAcknowledge
// (-1) with all remaining cursor.pendingAcks piggybacked. Called
// from sc.leave after all workers have exited.
func (s *source) closeShareSession(ctx context.Context) {
	sc := s.share.sc
	// Release buffered-but-never-polled records. Their acks land
	// on cursor.pendingAcks and get piggybacked on the FINAL_EPOCH
	// request below.
	b := s.share.buffered
	s.share.buffered = shareBufferedFetch{}
	if b.doneFetch != nil { // source had a buffered fetch
		b.doneFetch <- false
		for ti := range b.fetch.Topics {
			t := &b.fetch.Topics[ti]
			for pi := range t.Partitions {
				batchAckRecords(sc, t.Partitions[pi].Records, AckRelease, func(st *shareAckState) bool {
					return st.status.Load() == 0
				})
			}
		}
	}

	// Drain all cursor acks. drainAndClose sets the closed flag
	// atomically with the drain so post-leave Record.Ack gets a
	// callback instead of being silently lost.
	//
	// Renew entries are converted to release: we are shutting down,
	// so extending the lock is pointless. Explicitly releasing
	// ensures the broker can redeliver immediately rather than
	// waiting for the acquisition lock to expire.
	s.share.mu.Lock()
	epoch := s.share.sessionEpoch
	var drains []cursorAckDrain
	var nAcks int64
	for _, c := range s.share.cursors {
		entries, gaps := c.drainAndClose()
		if len(entries) > 0 || len(gaps) > 0 {
			for i := range entries {
				entries[i].status.CompareAndSwap(int32(AckRenew), int32(AckRelease))
			}
			nAcks += int64(len(entries))
			drains = append(drains, cursorAckDrain{cursor: c, entries: entries, gaps: gaps})
		}
	}
	s.share.mu.Unlock()

	// Filter stale-epoch batches for consistency with shareAck.
	// The broker would reject them per-partition
	// anyway (their original session is gone), and we notify the
	// user via the shareAckCallback for consistency.
	nLive, nStaleAcks, staleResults := filterStaleEntries(s, epoch, drains)
	sc.enqueueCallback(staleResults, nStaleAcks)
	if nStaleAcks > 0 {
		sc.cfg.logger.Log(LogLevelInfo, "share session close: dropped stale-epoch acks",
			"broker", logID(s.nodeID),
			"stale_ack_records", nStaleAcks,
		)
	}

	// If we never established a session and there are no live
	// acks, there is nothing to tell the broker. Skip.
	if epoch == 0 && nLive == 0 {
		return
	}

	memberID, _ := sc.memberGen.load()
	req := kmsg.NewPtrShareAcknowledgeRequest()
	req.GroupID = &sc.cfg.shareGroup
	req.MemberID = &memberID
	req.ShareSessionEpoch = -1 // KIP-932: close the session

	// drainIdx tracks the (topicID, partition) keys that actually
	// appeared on the wire so the broker-omitted scan in the
	// response handler doesn't flag partitions we never sent (e.g.
	// a drain whose entries all have status=0 produces an empty
	// range list from buildAckRanges and is skipped below).
	var drainIdx map[tidp]int
	if nLive > 0 {
		drainIdx = make(map[tidp]int, len(drains))
		topicIdx := make(map[[16]byte]int)
		for i, d := range drains {
			if len(d.entries) == 0 && len(d.gaps) == 0 {
				continue
			}
			ranges, _ := buildAckRanges(d.entries, d.gaps)
			if len(ranges) == 0 {
				continue
			}
			drainIdx[tidp{d.cursor.topicID, d.cursor.partition}] = i
			tidx, ok := topicIdx[d.cursor.topicID]
			if !ok {
				tidx = len(req.Topics)
				topicIdx[d.cursor.topicID] = tidx
				req.Topics = append(req.Topics, kmsg.ShareAcknowledgeRequestTopic{
					TopicID: d.cursor.topicID,
				})
			}
			rt := &req.Topics[tidx]
			rp := kmsg.ShareAcknowledgeRequestTopicPartition{
				Partition: d.cursor.partition,
			}
			for _, r := range ranges {
				rp.AcknowledgementBatches = append(rp.AcknowledgementBatches,
					kmsg.ShareAcknowledgeRequestTopicPartitionAcknowledgementBatch{
						FirstOffset:      r.firstOffset,
						LastOffset:       r.lastOffset,
						AcknowledgeTypes: ackTypes(r.ackType),
					})
			}
			rt.Partitions = append(rt.Partitions, rp)
		}
	}

	sc.cfg.logger.Log(LogLevelDebug, "closing share session on broker",
		"broker", logID(s.nodeID),
		"group", sc.cfg.shareGroup,
		"piggybacked_ack_records", nLive,
	)

	// nil ctx on brokerOrErr is intentional: this matches the
	// classic killSessionOnClose idiom (source.go's
	// killSessionOnClose). We want to find the broker without
	// getting stuck waiting on a broker-lookup ctx; the actual
	// request below uses the caller-supplied ctx for its timeout.
	br, err := s.cl.brokerOrErr(nil, s.nodeID, errUnknownBroker)
	if err != nil {
		sc.cfg.logger.Log(LogLevelDebug, "share session close: unable to find broker",
			"broker", logID(s.nodeID),
			"err", err,
		)
		sc.enqueueAckErrors(drains, err, nAcks-nStaleAcks) // piggybacked acks lost, decrement pendingAcks
		return
	}
	done := make(chan struct{})
	var respErr error
	var resp *kmsg.ShareAcknowledgeResponse
	br.do(ctx, req, func(k kmsg.Response, e error) {
		respErr = e
		if k != nil {
			resp, _ = k.(*kmsg.ShareAcknowledgeResponse)
		}
		close(done)
	})
	select {
	case <-done:
	case <-ctx.Done():
		sc.enqueueAckErrors(drains, ctx.Err(), nAcks-nStaleAcks) // ctx expired before response; acks lost, decrement pendingAcks
		s.resetShareSession()
		return
	}

	// Per-partition callback dispatch.
	if respErr != nil {
		sc.cfg.logger.Log(LogLevelDebug, "share session close request failed",
			"broker", logID(s.nodeID),
			"err", respErr,
		)
		// Network failure: report all live drains as failed.
		// enqueueAckErrors skips empty entries/gaps internally,
		// so stale-only drains (already reported via staleResults)
		// are not double-fired.
		sc.enqueueAckErrors(drains, respErr, nAcks-nStaleAcks)
	} else if resp != nil {
		if topErr := kerr.ErrorForCode(resp.ErrorCode); topErr != nil {
			sc.cfg.logger.Log(LogLevelInfo, "share session close: top-level error from broker",
				"broker", logID(s.nodeID),
				"err", topErr,
			)
			sc.enqueueAckErrors(drains, topErr, nAcks-nStaleAcks)
			s.resetShareSession()
			return
		}
		seen := make(map[tidp]struct{}, len(drainIdx))
		var results ShareAckResults
		var respErrCount int
		for _, rt := range resp.Topics {
			for _, rp := range rt.Partitions {
				key := tidp{rt.TopicID, rp.Partition}
				if _, dup := seen[key]; dup {
					sc.cfg.logger.Log(LogLevelWarn, "broker returned duplicate partition in share session close response, ignoring duplicate",
						"broker", logID(s.nodeID),
						"partition", rp.Partition,
					)
					continue
				}
				seen[key] = struct{}{}
				i, ok := drainIdx[key]
				if !ok {
					continue // partition we did not ack
				}
				drained := drains[i]
				var err error
				if rp.ErrorCode != 0 {
					err = kerr.ErrorForCode(rp.ErrorCode)
					respErrCount++
				}
				results = append(results, ShareAckResult{
					Topic:     drained.cursor.topic,
					Partition: rp.Partition,
					Err:       err,
				})
			}
		}
		if respErrCount > 0 {
			sc.cfg.logger.Log(LogLevelInfo, "share session close: per-partition errors from broker",
				"broker", logID(s.nodeID),
				"partition_errors", respErrCount,
			)
		}
		for tp, i := range drainIdx {
			if _, processed := seen[tp]; !processed {
				drained := drains[i]
				sc.cfg.logger.Log(LogLevelWarn, "broker omitted partition from share session close response",
					"broker", logID(s.nodeID),
					"topic", drained.cursor.topic,
					"partition", tp.p,
				)
				results = append(results, ShareAckResult{
					Topic:     drained.cursor.topic,
					Partition: tp.p,
					Err:       errBrokerOmittedAckPartition,
				})
			}
		}
		sc.enqueueCallback(results, nAcks-nStaleAcks)
	}

	// Reset our local view of the session so a hypothetical reuse
	// (this client cannot reuse, but be defensive) starts fresh.
	s.resetShareSession()
}

// purgeTopics removes topics from the share consumer's subscription
// and revokes any cursors for them. Called from consumer.purgeTopics
// inside blockingMetadataFn, serializing with metadata updates and
// applyMoves.
//
// The share consumer does not own its assignment: the server does.
// So the purge is two-phase:
//
//  1. Local (here): revoke cursors, drain-and-close them (so
//     post-purge Record.Ack returns errShareConsumerLeft), remove
//     from tps and reSeen.
//  2. Server (async): the next heartbeat sends the updated
//     SubscribedTopicNames; the coordinator revokes; the next
//     heartbeat response drives assignPartitions to clean up
//     nowAssigned.
func (sc *shareConsumer) purgeTopics(topics []string) {
	sc.cfg.logger.Log(LogLevelDebug, "purging share group topics",
		"group", sc.cfg.shareGroup,
		"topics", topics,
	)
	tps := sc.tps.load()
	for _, topic := range topics {
		tp, ok := tps[topic]
		if !ok {
			continue
		}
		td := tp.load()
		for i := range td.partitions {
			cursor := td.partitions[i].shareCursor
			cursor.assigned.Store(false)
			cursor.source.Load().removeShareCursor(cursor)
			entries, _ := cursor.drainAndClose()
			if n := int64(len(entries)); n > 0 {
				sc.enqueueCallback(ShareAckResults{{cursor.topic, cursor.partition, errShareConsumerLeft}}, n)
			}
		}
	}
	for _, topic := range topics {
		delete(sc.reSeen, topic)
	}
	sc.tps.purgeTopics(topics)
}

////////////
// MANAGE //
////////////

// maybeStartManage is the share equivalent of findNewPartitions;
// we don't need to do find logic here (heartbeat manages that),
// so this just starts the manage goro if it is not yet started
// and if topic data has been loaded.
func (sc *shareConsumer) maybeStartManage() {
	if !sc.manageRunning.CompareAndSwap(false, true) {
		return
	}
	for _, topicPartitions := range sc.tps.load() {
		if len(topicPartitions.load().partitions) > 0 {
			go sc.manage()
			return
		}
	}
	sc.manageRunning.Store(false)
}

func (sc *shareConsumer) manage() {
	defer sc.manageRunning.Store(false)
	if !sc.incWorker() {
		return // dying, bail
	}
	defer sc.decWorker()

	sc.cfg.logger.Log(LogLevelInfo, "beginning to manage the share group lifecycle",
		"group", sc.cfg.shareGroup,
	)

	sc.memberGen.store(newStringUUID(), 0) // 0 joins the group

	var consecutiveErrors int
	for {
		sleep, err := sc.heartbeat()
		if err == nil {
			consecutiveErrors = 0

			timer := time.NewTimer(sleep)
			select {
			case <-sc.fm.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}

		switch {
		case errors.Is(err, context.Canceled):
			return

		case errors.Is(err, kerr.UnknownMemberID),
			errors.Is(err, kerr.FencedMemberEpoch):
			// Keep the same UUID (matches the Java client) and reset
			// to epoch 0 so the next heartbeat re-joins.
			member, gen := sc.memberGen.load()
			sc.memberGen.storeGeneration(0)
			sc.cfg.logger.Log(LogLevelInfo, "share group heartbeat lost membership, resetting epoch",
				"group", sc.cfg.shareGroup,
				"member_id", member,
				"generation", gen,
				"err", err,
			)
			sc.lastSentSubscribedTopics = nil
			sc.lastSentRack = false
			sc.assignPartitions(nil)
			sc.unresolvedAssigns = nil
			sc.cl.allSources(func(s *source) {
				s.resetShareSession()
			})
			consecutiveErrors = 0
			continue

		case isRetryableBrokerErr(err),
			isAnyDialErr(err),
			sc.cl.maybeDeleteStaleCoordinator(sc.cfg.shareGroup, coordinatorTypeShare, err):
			// Retryable -- fall through to shared backoff below.

		default:
			// Fatal or unknown -- inject error into poll path and
			// fire hooks so the user knows something is wrong.
			// We do NOT assignPartitions(nil) here: we don't know
			// if the broker kept or dropped our assignment. If it
			// dropped us, the next heartbeat returns
			// UnknownMemberID/FencedMemberEpoch which already
			// clears. Clearing eagerly when the broker still has
			// our assignment would cause unnecessary churn
			// (revoke + re-assign on the next heartbeat).
			sc.c.addFakeReadyForDraining("", 0, &ErrGroupSession{err}, "notification of share group management error")
			sc.cfg.hooks.each(func(h Hook) {
				if h, ok := h.(HookGroupManageError); ok {
					h.OnGroupManageError(err)
				}
			})
		}

		// Reset sent-field tracking on any error so we re-send
		// SubscribedTopicNames and RackID on the next heartbeat.
		// If the coordinator changed, the new coordinator may not
		// have our state yet.
		sc.lastSentSubscribedTopics = nil
		sc.lastSentRack = false

		consecutiveErrors++
		backoff := sc.cfg.retryBackoff(consecutiveErrors)
		sc.cfg.logger.Log(LogLevelInfo, "share group manage loop hit a retryable error, retrying",
			"group", sc.cfg.shareGroup,
			"err", err,
			"consecutive_errors", consecutiveErrors,
			"backoff", backoff,
		)
		deadline := time.Now().Add(backoff)
		sc.cl.waitmeta(sc.fm.ctx, backoff, "waitmeta during share group manage backoff")
		after := time.NewTimer(time.Until(deadline))
		select {
		case <-sc.fm.ctx.Done():
			after.Stop()
			return
		case <-after.C:
		}
	}
}

// Issues a heartbeat request, handles the response, and returns
// how long to wait until the next heartbeat.
func (sc *shareConsumer) heartbeat() (time.Duration, error) {
	req := kmsg.NewPtrShareGroupHeartbeatRequest()
	req.GroupID = sc.cfg.shareGroup
	req.MemberID, req.MemberEpoch = sc.memberGen.load()

	// Only send RackID once; reset on fence/unknown forces re-send.
	if sc.cfg.rack != "" && !sc.lastSentRack {
		req.RackID = &sc.cfg.rack
	}

	tps := sc.tps.load()
	subscribedTopics := slices.Sorted(maps.Keys(tps))
	if sc.lastSentSubscribedTopics == nil || !slices.Equal(subscribedTopics, sc.lastSentSubscribedTopics) {
		req.SubscribedTopicNames = subscribedTopics
	}

	sc.cfg.logger.Log(LogLevelDebug, "share group heartbeating",
		"group", sc.cfg.shareGroup,
		"member_id", req.MemberID,
		"member_epoch", req.MemberEpoch,
	)
	resp, err := req.RequestWith(sc.fm.ctx, sc.cl)
	sleep := sc.cfg.heartbeatInterval
	if err == nil {
		err = errCodeMessage(resp.ErrorCode, resp.ErrorMessage)
		sleep = time.Duration(resp.HeartbeatIntervalMillis) * time.Millisecond
		if sleep < time.Second {
			sleep = time.Second // sanity
		}
	}
	sc.cfg.logger.Log(LogLevelDebug, "share group heartbeat complete",
		"group", sc.cfg.shareGroup,
		"new_member_epoch", func() int32 {
			if resp != nil {
				return resp.MemberEpoch
			}
			return 0
		}(),
		"err", err,
	)
	if err != nil {
		return sleep, err
	}

	sc.lastSentSubscribedTopics = subscribedTopics
	sc.lastSentRack = req.RackID != nil

	newAssigned := sc.handleHeartbeatResp(resp)
	if newAssigned != nil {
		sc.assignPartitions(newAssigned)
	}
	return sleep, nil
}

// Returns a new assignment IF the server gives us a new assignment
// OR if we were previously assigned topic IDs we could not map to topics,
// and now we can.
func (sc *shareConsumer) handleHeartbeatResp(resp *kmsg.ShareGroupHeartbeatResponse) map[string][]int32 {
	// The member ID is client-generated and immutable; only
	// update the epoch from the response.
	sc.memberGen.storeGeneration(resp.MemberEpoch)

	if resp.Assignment == nil {
		if len(sc.unresolvedAssigns) == 0 {
			return nil
		}
		// No assignment: try to resolve prior un-resolvable
		// topics. If we can, that updates our current assignment
		// and we return the new update.
		resolved := sc.resolveUnresolvedTopicIDs()
		if len(resolved) == 0 {
			return nil
		}
		current := sc.nowAssigned.read()
		merged := make(map[string][]int32, len(current)+len(resolved))
		maps.Copy(merged, current)
		maps.Copy(merged, resolved)
		return merged
	}

	// Fresh assignment: discard prior unresolved state since it does not
	// matter; parse assignment, and perhaps again add to unresolved.
	id2t := sc.cl.id2tMap()
	newAssigned := make(map[string][]int32)
	sc.unresolvedAssigns = nil
	for _, t := range resp.Assignment.TopicPartitions {
		name := id2t[t.TopicID]
		if name == "" {
			if sc.unresolvedAssigns == nil {
				sc.unresolvedAssigns = make(map[topicID][]int32)
			}
			sc.unresolvedAssigns[topicID(t.TopicID)] = t.Partitions
			continue
		}
		newAssigned[name] = t.Partitions
	}

	if len(sc.unresolvedAssigns) > 0 {
		sc.cl.triggerUpdateMetadataNow("share group heartbeat has unresolved topic IDs in assignment")
	}

	return newAssigned
}

// resolveUnresolvedTopicIDs maps unknown topic IDs to topic names.
// Anything that cannot be mapped stays unresolved.
// Returns newly resolved topics in a new map.
func (sc *shareConsumer) resolveUnresolvedTopicIDs() map[string][]int32 {
	if len(sc.unresolvedAssigns) == 0 {
		return nil
	}
	id2t := sc.cl.id2tMap()
	var dst map[string][]int32
	for id, ps := range sc.unresolvedAssigns {
		name := id2t[[16]byte(id)]
		if name == "" {
			continue
		}
		if dst == nil {
			dst = make(map[string][]int32)
		}
		dst[name] = append(dst[name], ps...) // shouldn't exist, but append anyway
		delete(sc.unresolvedAssigns, id)
	}
	return dst
}

func (sc *shareConsumer) assignPartitions(assignments map[string][]int32) {
	sc.cfg.logger.Log(LogLevelInfo, "assigning share partitions",
		"group", sc.cfg.shareGroup,
		"assignments", assignments,
	)

	old := sc.nowAssigned.read() // manage loop is the sole writer; can be concurrently read by source goros
	tps := sc.tps.load()

	sourcesToWake := make(map[*source]struct{})
	defer func() {
		for src := range sourcesToWake {
			src.maybeShareConsume()
		}
	}()

	// Revoke what was lost.
	for t, oldPs := range old {
		newPs := assignments[t]
		tp, ok := tps[t]
		if !ok {
			continue // if it's not in tps (or similar checks below), we weren't using it
		}
		td := tp.load()
		for _, p := range oldPs {
			if slices.Contains(newPs, p) { // linear, *usually* fast...
				continue
			}
			if int(p) >= len(td.partitions) {
				continue
			}
			cursor := td.partitions[p].shareCursor
			cursor.assigned.Store(false)
			sourcesToWake[cursor.source.Load()] = struct{}{}
		}
	}

	// Add what is new.
	var needsMetaUpdate bool
	for t, newPs := range assignments {
		oldPs := old[t]
		tp, ok := tps[t]
		if !ok {
			needsMetaUpdate = true
			continue // if we don't know the tps data, we can't assign it; force a meta refresh
		}
		td := tp.load()
		for _, p := range newPs {
			if slices.Contains(oldPs, p) {
				continue // already was assigned, no-op
			}
			if int(p) >= len(td.partitions) {
				needsMetaUpdate = true
				continue
			}
			cursor := td.partitions[p].shareCursor
			cursor.assigned.Store(true)
			sourcesToWake[cursor.source.Load()] = struct{}{}
		}
	}
	if needsMetaUpdate {
		sc.cl.triggerUpdateMetadataNow("share assignment has unknown topic or partition, or leader has no source")
	}

	sc.nowAssigned.store(assignments)
}

// applyMoves moves share cursors to new leaders based on CurrentLeader
// hints from a ShareFetch response. When the response's NodeEndpoints
// list includes brokers we do not yet know about, register them first
// (via the kip951move pattern) so we can move the cursor without
// waiting for a metadata refresh. The move runs asynchronously inside
// blockingMetadataFn so it serializes with metadata updates and purge,
// matching the kip951move pattern for producers.
func (sc *shareConsumer) applyMoves(moves []shareMove, endpoints []BrokerMetadata) {
	if len(moves) == 0 {
		return
	}
	go sc.cl.blockingMetadataFn(func() {
		// Seed any brokers from the response's NodeEndpoints that we
		// do not yet know about. Same merge-without-remove invariant
		// as kip951move.ensureBrokers.
		if len(endpoints) > 0 {
			k := kip951move{brokers: endpoints}
			k.ensureBrokers(sc.cl)
		}
		// Ensure per-broker sink+source exist for every move target,
		// and snapshot the sink+source pointer per leader to avoid
		// repeated locks.
		leaderSrcs := make(map[int32]*source, len(moves))
		sc.cl.sinksAndSourcesMu.Lock()
		for _, m := range moves {
			if _, exists := leaderSrcs[m.leaderID]; exists {
				continue
			}
			sns, ok := sc.cl.sinksAndSources[m.leaderID]
			if !ok {
				sns = sinkAndSource{
					sink:   sc.cl.newSink(m.leaderID),
					source: sc.cl.newSource(m.leaderID),
				}
				sc.cl.sinksAndSources[m.leaderID] = sns
			}
			leaderSrcs[m.leaderID] = sns.source
		}
		sc.cl.sinksAndSourcesMu.Unlock()

		// Safety: Any leader whose endpoint we didn't see needs
		// metadata to populate cl.brokers.
		seenEndpoint := make(map[int32]struct{}, len(endpoints))
		for _, ep := range endpoints {
			seenEndpoint[ep.NodeID] = struct{}{}
		}
		needsMeta := false
		for _, m := range moves {
			if _, ok := seenEndpoint[m.leaderID]; !ok {
				needsMeta = true
				break
			}
		}
		if needsMeta {
			sc.cl.triggerUpdateMetadataNow("share cursor CurrentLeader hint points to broker not in NodeEndpoints")
		}

		id2t := sc.cl.id2tMap()
		tps := sc.tps.load()

		var moved int
		for _, m := range moves {
			topicName := id2t[m.topicID]
			if topicName == "" {
				continue
			}
			tp, ok := tps[topicName]
			if !ok {
				continue
			}
			td := tp.load()
			if int(m.partition) >= len(td.partitions) {
				continue
			}
			cursor := td.partitions[m.partition].shareCursor
			oldSource := cursor.source.Load()
			if oldSource.nodeID == m.leaderID {
				continue
			}
			newSource := leaderSrcs[m.leaderID]
			oldSource.removeShareCursor(cursor)
			cursor.source.Store(newSource)
			newSource.addShareCursor(cursor)
			moved++
		}
		if moved > 0 {
			sc.cfg.logger.Log(LogLevelInfo, "migrated share cursors via CurrentLeader hints",
				"total_hints", len(moves),
				"applied", moved,
			)
		}
	})
}

////////////
// SOURCE //
////////////

func (s *source) maybeShareConsume() {
	if s.fetchState.maybeBegin() {
		go s.loopShareFetch()
	}
}

// incWorker is called at the very top of loopShareFetch. If the
// share consumer is dying, it returns false and the loop must
// hardFinish.
//
// This mirrors the consumerSession worker-count pattern in
// source.go's loopFetch but uses sc.mu (shared with sc.cond) instead
// of a per-session mutex, since shareConsumer has no per-session
// lifecycle.
func (sc *shareConsumer) incWorker() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.dying {
		return false
	}
	sc.workers++
	return true
}

func (sc *shareConsumer) decWorker() {
	sc.mu.Lock()
	sc.workers--
	if sc.workers == 0 {
		sc.cond.Broadcast()
	}
	sc.mu.Unlock()
}

func (s *source) loopShareFetch() {
	sc := s.share.sc

	if !sc.incWorker() {
		s.fetchState.hardFinish()
		return
	}
	defer sc.decWorker()

	var (
		canFetch  = make(chan chan bool, 1)
		ackTimer  *time.Timer
		ackTimerC <-chan time.Time // nil until acks are pending

		resetAckTimer = func() {
			if ackTimer == nil {
				ackTimer = time.NewTimer(time.Second)
			} else {
				ackTimer.Reset(time.Second)
			}
			ackTimerC = ackTimer.C
		}
		stopAckTimer = func() {
			if ackTimer != nil {
				ackTimer.Stop()
			}
			ackTimerC = nil
		}
		flushAcks = func() {
			stopAckTimer()
			s.shareAck(nil)
		}
	)

	// If this function is signaled by acks and we have nothing to consume,
	// we will EVENTUALLY exit once we loop through seeing there is nothing
	// to fetch and the workState allows us to exit.
	//
	// On fm.ctx cancellation we do NOT flush acks: shareAck would fail on
	// the cancelled ctx and enqueueAckErrors would drop them. Instead, we
	// leave them on the cursor for closeShareSession to drain and
	// piggyback on the FINAL_EPOCH request (which uses the caller ctx).
	for {
	unbuffered:
		for {
			select {
			case <-sc.fm.ctx.Done():
				return
			case <-s.share.ackCh:
				resetAckTimer()
			case <-s.share.ackFlushCh:
				flushAcks()
			case <-ackTimerC:
				flushAcks()
				// No more acks: We try finishing by doing a
				// quick check of cursors, the check that is
				// done when actually building a ShareFetch.
				//
				// If we DO loop back to the start (maybe two
				// acks bumped workState to continueWorking),
				// we will go through the full request flow and
				// exit after creating an empty request, same
				// as the normal consumer.
				if again := s.fetchState.maybeFinish(false); !again {
					return
				}
			case <-s.sem:
				break unbuffered
			}
		}

	desire:
		for {
			select {
			case <-sc.fm.ctx.Done():
				return
			case <-s.share.ackCh:
				resetAckTimer()
			case <-s.share.ackFlushCh:
				flushAcks()
			case <-ackTimerC:
				flushAcks()
			case sc.fm.desireFetch() <- canFetch:
				break desire
			}
		}

	allowed:
		for {
			select {
			case <-sc.fm.ctx.Done():
				sc.fm.cancelFetchCh <- canFetch
				return
			case <-s.share.ackCh:
				resetAckTimer()
			case <-s.share.ackFlushCh:
				flushAcks()
			case <-ackTimerC:
				flushAcks()
			case doneFetch := <-canFetch:
				if sc.fm.ctx.Err() != nil {
					doneFetch <- false
					return
				}
				fetched := s.shareFetch(doneFetch)
				// If we fetched, any pending acks from this source's
				// cursors were piggybacked on the request. Stop the
				// ack timer; if more acks arrive between here and
				// the next loop iteration, a signal is waiting in
				// share.ackCh that will restart the timer.
				if fetched {
					stopAckTimer()
				}
				if !s.fetchState.maybeFinish(fetched || ackTimerC != nil) {
					return
				}
				break allowed
			}
		}
	}
}

func (s *sourceShare) takeBuffered(paused pausedTopics) Fetch {
	sc := s.sc
	b := s.buffered
	s.buffered = shareBufferedFetch{}
	b.doneFetch <- true
	close(s.s.sem)

	f := b.fetch
	s.s.hook(&f, false, true) // unbuffered, polled

	// Strip paused partitions from the returned fetch and release
	// their records back to the broker for redelivery.
	keep := f.Topics[:0]
	for _, t := range f.Topics {
		pps, topicPaused := paused.t(t.Topic)
		if topicPaused && pps.all {
			for i := range t.Partitions {
				batchAckRecords(sc, t.Partitions[i].Records, AckRelease, nil)
			}
			continue
		}
		if topicPaused {
			keepp := t.Partitions[:0]
			for _, p := range t.Partitions {
				if _, ok := pps.m[p.Partition]; ok {
					batchAckRecords(sc, p.Records, AckRelease, nil)
					continue
				}
				keepp = append(keepp, p)
			}
			t.Partitions = keepp
		}
		if len(t.Partitions) > 0 {
			keep = append(keep, t)
		}
	}
	f.Topics = keep

	return f
}

func (s *sourceShare) takeNBuffered(paused pausedTopics, n int) (Fetch, int, bool) {
	sc := s.sc
	var (
		r      Fetch
		rstrip Fetch
		taken  int
	)

	b := &s.buffered
	bf := &b.fetch
	for len(bf.Topics) > 0 && n > 0 {
		t := &bf.Topics[0]

		if paused.has(t.Topic, -1) {
			rstrip.Topics = append(rstrip.Topics, *t)
			bf.Topics = bf.Topics[1:]
			for i := range t.Partitions {
				batchAckRecords(sc, t.Partitions[i].Records, AckRelease, nil)
			}
			continue
		}

		var rt *FetchTopic
		ensureTopicAdded := func() {
			if rt != nil {
				return
			}
			r.Topics = append(r.Topics, *t)
			rt = &r.Topics[len(r.Topics)-1]
			rt.Partitions = nil
		}
		var rtstrip *FetchTopic
		ensureTopicStripped := func() {
			if rtstrip != nil {
				return
			}
			rstrip.Topics = append(rstrip.Topics, *t)
			rtstrip = &rstrip.Topics[len(rstrip.Topics)-1]
			rtstrip.Partitions = nil
		}

		for len(t.Partitions) > 0 && n > 0 {
			p := &t.Partitions[0]

			if paused.has(t.Topic, p.Partition) {
				ensureTopicStripped()
				rtstrip.Partitions = append(rtstrip.Partitions, *p)
				batchAckRecords(sc, p.Records, AckRelease, nil)
				t.Partitions = t.Partitions[1:]
				continue
			}

			ensureTopicAdded()
			rt.Partitions = append(rt.Partitions, *p)
			rp := &rt.Partitions[len(rt.Partitions)-1]

			take := min(n, len(p.Records))
			rp.Records = p.Records[:take:take]
			p.Records = p.Records[take:]
			n -= take
			taken += take

			if len(p.Records) == 0 {
				t.Partitions = t.Partitions[1:]
			}
		}

		if len(t.Partitions) == 0 {
			bf.Topics = bf.Topics[1:]
		}
	}

	if len(rstrip.Topics) > 0 {
		s.s.hook(&rstrip, false, true)
	}
	s.s.hook(&r, false, true) // unbuffered, polled

	drained := len(bf.Topics) == 0
	if drained {
		s.takeBuffered(nil)
	}
	return r, taken, drained
}

/////////
// ACK //
/////////

// shareAck sends a ShareAcknowledge request. If predrained is non-nil,
// those acks are used directly; otherwise acks are drained from cursors.
// On success, bumps the share session epoch. On failure, either
// re-queues to cursors (retryable) or drops (session destroyed /
// transport error) and notifies the callback.
func (s *source) shareAck(predrained []cursorAckDrain) {
	sc := s.share.sc

	var drains []cursorAckDrain
	s.share.mu.Lock() // guard concurrent cursor movement
	epoch := s.share.sessionEpoch
	if predrained != nil {
		drains = predrained
	} else {
		for _, c := range s.share.cursors {
			entries, gaps := c.drainAcks()
			if len(entries) > 0 || len(gaps) > 0 {
				drains = append(drains, cursorAckDrain{cursor: c, entries: entries, gaps: gaps})
			}
		}
	}
	s.share.mu.Unlock()

	if len(drains) == 0 {
		return
	}

	nAcks, nStaleAcks, staleResults := filterStaleEntries(s, epoch, drains)

	sc.enqueueCallback(staleResults, nStaleAcks)
	if nAcks == 0 {
		return // everything was stale
	}

	if epoch == 0 {
		sc.cfg.logger.Log(LogLevelInfo, "dropping stale acks, share session epoch is 0", "broker", s.nodeID)
		sc.enqueueAckErrors(drains, kerr.InvalidShareSessionEpoch, nAcks)
		return
	}

	memberID, _ := sc.memberGen.load()
	req := kmsg.NewPtrShareAcknowledgeRequest()
	req.GroupID = &sc.cfg.shareGroup
	req.MemberID = &memberID
	req.ShareSessionEpoch = epoch

	topicIdx := make(map[[16]byte]int)
	drainIdx := make(map[tidp]int) // maps topic+partition to drains index
	for i, d := range drains {
		if len(d.entries) == 0 && len(d.gaps) == 0 {
			continue
		}
		ranges, hasRenew := buildAckRanges(d.entries, d.gaps)
		if len(ranges) == 0 {
			continue
		}
		drainIdx[tidp{d.cursor.topicID, d.cursor.partition}] = i
		if hasRenew {
			req.IsRenewAck = true
		}
		tidx, ok := topicIdx[d.cursor.topicID]
		if !ok {
			tidx = len(req.Topics)
			topicIdx[d.cursor.topicID] = tidx
			req.Topics = append(req.Topics, kmsg.ShareAcknowledgeRequestTopic{TopicID: d.cursor.topicID})
		}
		rp := kmsg.ShareAcknowledgeRequestTopicPartition{Partition: d.cursor.partition}
		for _, r := range ranges {
			rp.AcknowledgementBatches = append(rp.AcknowledgementBatches,
				kmsg.ShareAcknowledgeRequestTopicPartitionAcknowledgementBatch{
					FirstOffset:      r.firstOffset,
					LastOffset:       r.lastOffset,
					AcknowledgeTypes: ackTypes(r.ackType),
				})
		}
		req.Topics[tidx].Partitions = append(req.Topics[tidx].Partitions, rp)
	}
	sc.cfg.logger.Log(LogLevelDebug, "sending share acknowledge",
		"broker", logID(s.nodeID),
		"group", sc.cfg.shareGroup,
		"session_epoch", req.ShareSessionEpoch,
		"n_topics", len(req.Topics),
		"n_ack_records", nAcks,
		"is_renew_ack", req.IsRenewAck,
	)
	// A retry is likely useless if the request actually left the client,
	// the broker rejects duplicate acks with INVALID_RECORD_STATE.
	// But, if the request did not leave the client, we benefit, and
	// failing early if the req didn't is worse.
	kresp, err := s.cl.retryableBrokerFn(func() (*broker, error) {
		return s.cl.brokerOrErr(sc.cl.ctx, s.nodeID, errUnknownBroker)
	}).Request(sc.cl.ctx, req)
	if err != nil {
		sc.cfg.logger.Log(LogLevelDebug, "share ack request failed",
			"broker", s.nodeID,
			"err", err,
		)
		s.resetShareSession()
		sc.enqueueAckErrors(drains, err, nAcks)
		return
	}

	resp := kresp.(*kmsg.ShareAcknowledgeResponse)
	if topErr := kerr.ErrorForCode(resp.ErrorCode); topErr != nil {
		s.resetShareSession()
		sc.cfg.logger.Log(LogLevelInfo, "share ack top-level error, acks not delivered",
			"broker", s.nodeID,
			"err", topErr,
		)
		sc.enqueueAckErrors(drains, topErr, nAcks)
		return
	}
	s.bumpShareSessionEpochIfCurrent(epoch) // concurrent session reset / disconnect could have reset us

	var (
		seen     = make(map[tidp]struct{}, len(drains))
		results  ShareAckResults
		moves    []shareMove
		requeued int64
	)
	for _, rt := range resp.Topics {
		for _, rp := range rt.Partitions {
			tpKey := tidp{rt.TopicID, rp.Partition}
			if _, dup := seen[tpKey]; dup {
				sc.cfg.logger.Log(LogLevelWarn, "broker returned duplicate partition in share ack response, ignoring",
					"broker", logID(s.nodeID),
					"partition", rp.Partition,
				)
				continue
			}
			seen[tpKey] = struct{}{}
			i, ok := drainIdx[tpKey]
			if !ok {
				continue // broker returned a partition we didn't ack
			}
			drained := drains[i]

			var partErr error
			if rp.ErrorCode != 0 {
				partErr = kerr.ErrorForCode(rp.ErrorCode)
				if rp.CurrentLeader.LeaderID >= 0 && rp.CurrentLeader.LeaderEpoch >= 0 {
					moves = append(moves, shareMove{
						topicID:   rt.TopicID,
						partition: rp.Partition,
						leaderID:  rp.CurrentLeader.LeaderID,
					})
				}
				if isShareAckRetryable(partErr) {
					drained.cursor.requeueEntries(sc, drained.entries)
					requeued += int64(len(drained.entries))
					drained.cursor.requeueGaps(drained.gaps)
					continue
				}
			}

			// On success (or non-retryable error): reset renew
			// statuses so the user can renew the same record again.
			// Only walk entries when the request actually carried
			// renew acks -- otherwise the CAS is guaranteed to
			// no-op on every entry.
			if req.IsRenewAck {
				for _, e := range drained.entries {
					e.status.CompareAndSwap(int32(AckRenew), 0)
				}
			}

			results = append(results, ShareAckResult{
				Topic:     drained.cursor.topic,
				Partition: rp.Partition,
				Err:       partErr,
			})
		}
	}

	for tp, i := range drainIdx {
		if _, processed := seen[tp]; !processed {
			drained := drains[i]
			sc.cfg.logger.Log(LogLevelWarn, "broker omitted partition from share ack response",
				"broker", logID(s.nodeID),
				"topic", drained.cursor.topic,
				"partition", tp.p,
			)
			results = append(results, ShareAckResult{
				Topic:     drained.cursor.topic,
				Partition: tp.p,
				Err:       errBrokerOmittedAckPartition,
			})
		}
	}
	if len(moves) > 0 {
		var moveBrokers []BrokerMetadata
		if len(resp.NodeEndpoints) > 0 {
			moveBrokers = make([]BrokerMetadata, 0, len(resp.NodeEndpoints))
			for _, ep := range resp.NodeEndpoints {
				moveBrokers = append(moveBrokers, BrokerMetadata{
					NodeID: ep.NodeID,
					Host:   ep.Host,
					Port:   ep.Port,
					Rack:   ep.Rack,
				})
			}
		}
		sc.applyMoves(moves, moveBrokers)
	}
	sc.enqueueCallback(results, nAcks-requeued)
}

// releaseUndeliverable releases records that were "acquired" but for which the
// broker gave us no record data (i.e. protocol violation).
func (s *source) releaseUndeliverable(cursor *shareCursor, acquired []kmsg.ShareFetchResponseTopicPartitionAcquiredRecord, epoch int32) {
	if len(acquired) == 0 {
		return
	}
	releases := make([]shareAckRange, 0, len(acquired))
	for _, ar := range acquired {
		if ar.LastOffset < ar.FirstOffset {
			continue // inverted range, skip
		}
		releases = append(releases, shareAckRange{
			firstOffset:  ar.FirstOffset,
			lastOffset:   ar.LastOffset,
			source:       s,
			sessionEpoch: epoch,
			ackType:      int8(AckRelease),
		})
	}
	cursor.enqueueGaps(releases)
}

func (sc *shareConsumer) enqueueAllAcks(byCursor cursorsAcks) {
	if len(byCursor) == 0 {
		return
	}
	sourcesToWake := make(map[*source]struct{})
	defer func() {
		for src := range sourcesToWake {
			src.signalShareAcks()
		}
	}()
	var closedRes ShareAckResults
	for cursor, ca := range byCursor {
		cursor.ackMu.Lock()
		if cursor.closed {
			cursor.ackMu.Unlock()
			closedRes = append(closedRes, ShareAckResult{cursor.topic, cursor.partition, errShareConsumerLeft})
			continue
		}
		cursor.pendingAcks = append(cursor.pendingAcks, ca.entries...)
		sc.pendingAcks.Add(ca.n)
		cursor.ackMu.Unlock()
		sourcesToWake[cursor.source.Load()] = struct{}{}
	}
	if len(closedRes) > 0 {
		sc.enqueueCallback(closedRes, 0)
	}
}

func (sc *shareConsumer) subtractPendingAcks(n int64) {
	if n <= 0 {
		return
	}
	if sc.pendingAcks.Add(-n) == 0 {
		sc.ackMu.Lock()
		sc.ackMu.Unlock() //nolint:staticcheck,gocritic // intentional Lock/Unlock: serialization point before ackC.Broadcast
		sc.ackC.Broadcast()
	}
}

// enqueueAckErrors builds per-partition error results from drains
// and enqueues them. Empty-batch drains are skipped: they were
// fully stale-filtered and already reported via the stale callback.
func (sc *shareConsumer) enqueueAckErrors(drains []cursorAckDrain, err error, nAcks int64) {
	if len(drains) == 0 {
		return
	}
	var results ShareAckResults
	for _, d := range drains {
		if len(d.entries) == 0 && len(d.gaps) == 0 {
			continue
		}
		results = append(results, ShareAckResult{d.cursor.topic, d.cursor.partition, err})
	}
	sc.enqueueCallback(results, nAcks)
}

// enqueueCallback pushes a callback + pending-count entry onto the ring.
func (sc *shareConsumer) enqueueCallback(results ShareAckResults, nAcks int64) {
	entry := shareCallbackEntry{results: results, nAcks: nAcks}
	if first, _ := sc.callbackRing.push(entry); first {
		go sc.drainCallbacks(entry)
	}
}

func (sc *shareConsumer) drainCallbacks(entry shareCallbackEntry) {
	cb := sc.cfg.shareAckCallback
	for {
		if cb != nil && len(entry.results) > 0 {
			cb(sc.cl, entry.results)
		}
		sc.subtractPendingAcks(entry.nAcks)
		var more bool
		entry, more, _ = sc.callbackRing.dropPeek()
		if !more {
			return
		}
	}
}

// requeueEntries puts user ack entries back onto the cursor without
// touching sc.pendingAcks. Used for retryable requeues.
// PurgeTopics forces us to do the share callback here.
func (c *shareCursor) requeueEntries(sc *shareConsumer, entries []shareAckEntry) {
	c.ackMu.Lock()
	if c.closed {
		c.ackMu.Unlock()
		sc.enqueueCallback(ShareAckResults{{c.topic, c.partition, errShareConsumerLeft}}, int64(len(entries)))
		return
	}
	c.pendingAcks = append(c.pendingAcks, entries...)
	c.ackMu.Unlock()
}

// requeueGaps puts gap/release ranges back onto the cursor without
// touching sc.pendingAcks. Used for retryable requeues.
func (c *shareCursor) requeueGaps(gaps []shareAckRange) {
	c.ackMu.Lock()
	if c.closed { // this has the same PurgeTopics race, but gaps have no callback
		c.ackMu.Unlock()
		return
	}
	c.pendingGaps = append(c.pendingGaps, gaps...)
	c.ackMu.Unlock()
}

// enqueueGaps appends gap/release ranges to the cursor's pendingGaps.
// Does NOT increment sc.pendingAcks - internal acks are invisible to
// FlushAcks. Returns false if the cursor is closed.
func (c *shareCursor) enqueueGaps(gaps []shareAckRange) bool {
	c.ackMu.Lock()
	if c.closed {
		c.ackMu.Unlock()
		return false
	}
	c.pendingGaps = append(c.pendingGaps, gaps...)
	c.ackMu.Unlock()
	return true
}

// drainAndClose drains all pending user acks and gaps AND sets
// c.closed=true under a single c.ackMu acquisition. After this
// call, any user-side appendAck/enqueueGaps on the cursor is rejected.
func (c *shareCursor) drainAndClose() ([]shareAckEntry, []shareAckRange) {
	c.ackMu.Lock()
	entries := c.pendingAcks
	gaps := c.pendingGaps
	c.pendingAcks = nil
	c.pendingGaps = nil
	c.closed = true
	c.ackMu.Unlock()
	return entries, gaps
}

// drainAcks atomically removes and returns all pending user acks
// and gaps from the cursor.
func (c *shareCursor) drainAcks() ([]shareAckEntry, []shareAckRange) {
	c.ackMu.Lock()
	entries := c.pendingAcks
	gaps := c.pendingGaps
	c.pendingAcks = nil
	c.pendingGaps = nil
	c.ackMu.Unlock()
	return entries, gaps
}

var shareAckKey = strp("share-ack")

func shareAckFromCtx(r *Record) (*shareAckSlab, *shareAckState) {
	if r.Context == nil {
		return nil, nil
	}
	v := r.Context.Value(shareAckKey)
	if v == nil {
		return nil, nil
	}
	slab := v.(*shareAckSlab)
	idx := int((uintptr(unsafe.Pointer(r)) - uintptr(unsafe.Pointer(slab.records0))) / recSize) //nolint:gosec // pointer arithmetic to index the slab; idx is bounds-checked below
	if idx < 0 || idx >= len(slab.states) {
		return nil, nil
	}
	return slab, &slab.states[idx]
}

// appendAck appends a per-record user ack entry and increments
// sc.pendingAcks. The status pointer is read at request-build time
// to get the current ack type.
func (c *shareCursor) appendAck(sc *shareConsumer, ackSource *source, sessionEpoch int32, offset int64, status *atomic.Int32) {
	c.ackMu.Lock()
	if c.closed {
		c.ackMu.Unlock()
		sc.enqueueCallback(ShareAckResults{{c.topic, c.partition, errShareConsumerLeft}}, 0)
		return
	}
	wake := len(c.pendingAcks) == 0 // only empty->non-empty needs a wake signal
	c.pendingAcks = append(c.pendingAcks, shareAckEntry{
		offset:       offset,
		source:       ackSource,
		status:       status,
		sessionEpoch: sessionEpoch,
	})
	sc.pendingAcks.Add(1) // must happen inside mu, otherwise drainAndClose could race before we inc and FlushAcks could end early
	c.ackMu.Unlock()

	if wake {
		c.source.Load().signalShareAcks()
	}
}

// batchAckRecords accumulates acks for the given records per-cursor and
// flushes them in one pass per cursor (one lock acquisition, one signal
// per source).
//
// keep is called under st.mu and decides whether each record should be
// included in the batch. Passing nil acks every record unconditionally.
func batchAckRecords(sc *shareConsumer, rs []*Record, status AckStatus, keep func(st *shareAckState) bool) {
	var (
		byCursor   cursorsAcks
		lastCursor *shareCursor
		lastCa     *cursorAcks
	)

	renew := status == AckRenew
	for _, r := range rs {
		slab, st := shareAckFromCtx(r)
		if st == nil {
			continue
		}
		if keep != nil && !keep(st) {
			continue
		}
		if renew {
			if !st.status.CompareAndSwap(0, int32(AckRenew)) {
				continue
			}
		} else {
			// Terminal: override 0 or AckRenew. Skip if already terminal.
			ok := false
			for {
				cur := st.status.Load()
				if cur != 0 && cur != int32(AckRenew) {
					break
				}
				if st.status.CompareAndSwap(cur, int32(status)) {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}

		lastCursor, lastCa = byCursor.add(slab.cursor, shareAckEntry{
			offset:       r.Offset,
			source:       slab.ackSource,
			status:       &st.status,
			sessionEpoch: slab.sessionEpoch,
		}, lastCursor, lastCa)
	}

	sc.enqueueAllAcks(byCursor)
}

// filterStaleEntries mutates drains in-place to drop ack
// batches that the broker cannot possibly honor, reporting them via
// the user callback as pre-filtered drops. Two categories:
//
//  1. b.source == s && b.sessionEpoch > epoch: same source, batch
//     stamped with an epoch higher than the current session epoch:
//     a session reset happened and the broker lost record state.
//
//  2. b.source != s: the cursor migrated from the original source to
//     s after the acks were queued. Acq state is not transferred to
//     the new broker.
//
// Edge cases: enough epoch bumps happen, or the leader transfer from
// A to B then back; it's fine, we'll just have a wasted round trip
// and the broker rejects the acks.
//
// Returns:
//   - nAcks: count of deliverable (non-stale, non-migrated) records
//   - nStaleAcks: count of pre-filtered records (stale + migrated)
//   - staleResults: per-cursor pre-filter results for the user callback
func filterStaleEntries(s *source, epoch int32, drains []cursorAckDrain) (nUserAcks, nStaleUserAcks int64, staleResults ShareAckResults) {
	for i := range drains {
		d := &drains[i]

		// Filter user ack entries.
		filteredEntries := d.entries[:0]
		var dropErr error
		for _, e := range d.entries {
			switch {
			case e.source == s && e.sessionEpoch > epoch:
				nStaleUserAcks++
				if dropErr == nil {
					dropErr = kerr.InvalidShareSessionEpoch
				}
			case e.source != s:
				nStaleUserAcks++
				if dropErr == nil {
					dropErr = kerr.InvalidRecordState
				}
			default:
				nUserAcks++
				filteredEntries = append(filteredEntries, e)
			}
		}
		d.entries = filteredEntries

		// Filter gap/release ranges (same source/epoch check).
		filteredGaps := d.gaps[:0]
		for _, g := range d.gaps {
			switch {
			case g.source == s && g.sessionEpoch > epoch:
				// stale gap; drop silently (not counted in pendingAcks)
			case g.source != s:
				// migrated gap; drop silently
			default:
				filteredGaps = append(filteredGaps, g)
			}
		}
		d.gaps = filteredGaps

		if dropErr != nil {
			staleResults = append(staleResults, ShareAckResult{d.cursor.topic, d.cursor.partition, dropErr})
		}
	}
	return
}

var ackTypeSlices = [...][]int8{
	0:                {0}, // gap (compaction skip)
	int8(AckAccept):  {int8(AckAccept)},
	int8(AckRelease): {int8(AckRelease)},
	int8(AckReject):  {int8(AckReject)},
	int8(AckRenew):   {int8(AckRenew)},
} // eliminates many tiny allocs

func ackTypes(t int8) []int8 {
	if t >= 0 && int(t) < len(ackTypeSlices) {
		return ackTypeSlices[t]
	}
	return []int8{t}
}

// buildAckRanges converts user ack entries + gap ranges into merged
// wire-format ranges. User entries are read via their live status
// pointer; entries with status 0 (reset or unhandled) are skipped.
// hasRenew is true if any entry has AckRenew status.
//
// Entries and gaps are sorted by offset before coalescing so that
// contiguous same-type ranges merge regardless of insertion order.
// The two are built separately (gaps are acked immediately so they
// rarely coalesce with user entries).
func buildAckRanges(entries []shareAckEntry, gaps []shareAckRange) (ranges []shareAckRange, hasRenew bool) {
	slices.SortFunc(entries, func(a, b shareAckEntry) int {
		return cmp.Compare(a.offset, b.offset)
	})
	slices.SortFunc(gaps, func(a, b shareAckRange) int {
		return cmp.Compare(a.firstOffset, b.firstOffset)
	})
	// Dedupe: a single record can have multiple entries for the same offset
	// (e.g. Ack(AckRenew) then Ack(AckAccept) both append; the terminal
	// CAS overwrites the renew but the renew entry remains in the slice).
	// Both entries read the same final status, so emit only one. Without
	// this, the request carries two adjacent [X,X,T] batches and the
	// broker rejects with INVALID_RECORD_STATE.
	var lastOffset int64 = -1
	for _, e := range entries {
		t := int8(e.status.Load())
		if t == 0 {
			continue // status was reset or not yet decided
		}
		if e.offset == lastOffset {
			continue // duplicate from a renew-then-terminal sequence
		}
		lastOffset = e.offset
		if t == int8(AckRenew) {
			hasRenew = true
		}
		ranges = coalesceAppendRange(ranges, shareAckRange{
			firstOffset:  e.offset,
			lastOffset:   e.offset,
			source:       e.source,
			sessionEpoch: e.sessionEpoch,
			ackType:      t,
		})
	}
	for _, g := range gaps {
		ranges = coalesceAppendRange(ranges, g)
	}
	return
}

// coalesceAppendRange appends r to the slice, merging with the last
// element if they are contiguous, same type, same source, and same epoch.
func coalesceAppendRange(out []shareAckRange, r shareAckRange) []shareAckRange {
	if n := len(out); n > 0 {
		last := &out[n-1]
		if last.ackType == r.ackType && last.source == r.source &&
			last.sessionEpoch == r.sessionEpoch && last.lastOffset+1 == r.firstOffset {
			last.lastOffset = r.lastOffset
			return out
		}
	}
	return append(out, r)
}

///////////
// FETCH // -- methods on source rather than shareSource b/c most need source fields
///////////

// shareFetch orchestrates a share fetch: send the request, handle
// errors and backoff, apply leader moves, dispatch ack callbacks,
// and buffer the result. Per-partition handling lives in
// handleShareReqResp.
func (s *source) shareFetch(doneFetch chan<- bool) (fetched bool) {
	sc := s.share.sc
	req, usable, piggybackAcks, sentPiggyback, nAcks, staleResults, nStaleAcks, hasRenew := s.createShareReq(false)

	// Renew acks (type 4) cannot be piggybacked on a ShareFetch
	// (the broker requires IsRenewAck + zero fetch params). If any
	// are present, send ALL drained acks via standalone
	// ShareAcknowledge first, then rebuild the request without
	// re-draining acks (they were already sent, new ones COULD
	// have happened but we need forward progress...).
	if hasRenew {
		sc.enqueueCallback(staleResults, nStaleAcks)
		s.shareAck(piggybackAcks)
		req, usable, piggybackAcks, sentPiggyback, nAcks, staleResults, nStaleAcks, _ = s.createShareReq(true) // clears piggyBack, nacks, stale
	}

	var (
		buffered               bool
		alreadySentToDoneFetch bool
	)
	defer func() {
		if !buffered && !alreadySentToDoneFetch {
			doneFetch <- false
		}
	}()

	// Handle stale-filtered acks from createShareReq.
	sc.enqueueCallback(staleResults, nStaleAcks)

	// Drop piggybacked acks on a fresh session (epoch 0): the
	// broker rejects acks on an initial fetch.
	if req != nil && req.ShareSessionEpoch == 0 && len(piggybackAcks) > 0 {
		sc.cfg.logger.Log(LogLevelInfo, "dropping stale piggybacked acks, share session epoch is 0",
			"broker", s.nodeID,
		)
		sc.enqueueAckErrors(piggybackAcks, kerr.InvalidShareSessionEpoch, nAcks)
		piggybackAcks = nil
		nAcks = 0
	}

	if req == nil { // nothing to fetch or forget; fallback to a shareAck
		s.shareAck(nil)
		return false
	}

	sc.cfg.logger.Log(LogLevelDebug, "sending share fetch",
		"broker", logID(s.nodeID),
		"group", sc.cfg.shareGroup,
		"session_epoch", req.ShareSessionEpoch,
		"n_topics", len(req.Topics),
		"n_forgotten_topics", len(req.ForgottenTopicsData),
		"max_wait_ms", req.MaxWaitMillis,
		"max_records", req.MaxRecords,
		"batch_size", req.BatchSize,
		"n_piggyback_acks", nAcks,
	)

	// Bound the broker round-trip to MaxWait + 5s. The broker caps
	// its own wait at MaxWaitMillis, so a healthy response arrives
	// within MaxWait + RTT; 5s of grace tolerates slow links while
	// still giving LeaveGroup a predictable shutdown ceiling instead
	// of cancelling mid-fetch (which strands piggybacked acks that
	// the broker already processed, causing double-ack or stale-
	// epoch errors on the FINAL_EPOCH resend path).
	timeout := time.Duration(req.MaxWaitMillis)*time.Millisecond + 5*time.Second
	var (
		kresp       kmsg.Response
		err         error
		requested   = make(chan struct{})
		ctx, cancel = context.WithTimeout(sc.cl.ctx, timeout)
	)
	defer cancel()

	br, brerr := s.cl.brokerOrErr(ctx, s.nodeID, errUnknownBroker)
	if brerr != nil {
		close(requested)
		err = brerr
	} else {
		br.do(ctx, req, func(k kmsg.Response, e error) {
			kresp, err = k, e
			close(requested)
		})
	}

	// Wait for the response. ctx carries a MaxWait+5s deadline rather
	// than fm.ctx cancellation: cancelling mid-fetch is lossy (the
	// broker may have already consumed piggybacked acks and bumped
	// the session epoch), so we let in-flight fetches finish within
	// their natural window. LeaveGroup's worker barrier tolerates
	// this because the deadline caps total shutdown delay. Only the
	// hard-stuck case (broker unresponsive past the deadline) falls
	// into the requeue path below, where closeShareSession re-sends
	// the acks on the userCtx-bounded FINAL_EPOCH ShareAcknowledge.
	select {
	case <-requested:
		if isContextErr(err) && ctx.Err() != nil {
			// Deadline hit or cl.ctx cancelled. Requeue piggybacked
			// acks so closeShareSession or a later fetch can retry.
			for _, pa := range piggybackAcks {
				pa.cursor.requeueEntries(sc, pa.entries)
				pa.cursor.requeueGaps(pa.gaps)
			}
			return false
		}
		fetched = true
	case <-ctx.Done():
		// cl.ctx cancelled or deadline. Requeue for closeShareSession.
		for _, pa := range piggybackAcks {
			pa.cursor.requeueEntries(sc, pa.entries)
			pa.cursor.requeueGaps(pa.gaps)
		}
		return false
	}

	var didBackoff bool
	backoff := func() {
		// Release fetch slot before sleeping so other sources
		// can fetch during our backoff.
		doneFetch <- false
		alreadySentToDoneFetch = true
		didBackoff = true
		s.consecutiveFailures++
		after := time.NewTimer(sc.cfg.retryBackoff(s.consecutiveFailures))
		defer after.Stop()
		select {
		case <-after.C:
		case <-ctx.Done():
		}
	}
	defer func() {
		if !didBackoff {
			s.consecutiveFailures = 0
		}
	}()

	if err != nil {
		s.resetShareSession()
		backoff()
		sc.enqueueAckErrors(piggybackAcks, err, nAcks)
		return fetched
	}

	resp := kresp.(*kmsg.ShareFetchResponse)

	res := s.handleShareReqResp(req, resp, usable, piggybackAcks, sentPiggyback)

	if res.discardErr != nil {
		sc.enqueueAckErrors(piggybackAcks, res.discardErr, nAcks)
		return fetched
	}

	if len(res.moves) > 0 {
		sc.applyMoves(res.moves, res.moveBrokers)
	}

	sc.enqueueCallback(res.ackResults, nAcks-res.ackRequeued)

	if res.fetch.hasErrorsOrRecords() {
		buffered = true
		s.share.buffered = shareBufferedFetch{
			fetch:     res.fetch,
			doneFetch: doneFetch,
		}
		s.sem = make(chan struct{})
		s.hook(&res.fetch, true, false)
		sc.c.addSourceReadyForDraining(s)
	} else if res.allErrsStripped {
		backoff()
	}
	return fetched
}

// handleShareReqResp decodes partitions, bumps the session epoch,
// and handles gap acks, undeliverable releases, and ack requeues.
// Returns a shareFetchResult for shareFetch to act on.
func (s *source) handleShareReqResp(req *kmsg.ShareFetchRequest, resp *kmsg.ShareFetchResponse, usable []*shareCursor, piggybackAcks []cursorAckDrain, piggybackIdx map[tidp]int) shareFetchResult {
	sc := s.share.sc

	if topErr := kerr.ErrorForCode(resp.ErrorCode); topErr != nil {
		s.resetShareSession()
		sc.cfg.logger.Log(LogLevelInfo, "share fetch top-level error",
			"broker", s.nodeID,
			"err", topErr,
		)
		return shareFetchResult{discardErr: topErr}
	}

	// Mid-flight reset guard: if manage reset the session while
	// our request was in flight, the records are unackable (their
	// slab epoch won't match the new session) but piggybacked ack
	// results are still valid -- the broker processed them. We
	// extract ack results and discard records.
	epoch := req.ShareSessionEpoch
	s.share.mu.Lock()
	sessionStale := s.share.sessionEpoch != epoch
	if !sessionStale {
		s.share.sessionEpoch++
		for _, rt := range req.Topics {
			for _, rp := range rt.Partitions {
				s.share.sessionParts[tidp{rt.TopicID, rp.Partition}] = struct{}{}
			}
		}
		for _, ft := range req.ForgottenTopicsData {
			for _, p := range ft.Partitions {
				delete(s.share.sessionParts, tidp{ft.TopicID, p})
			}
		}
	}
	newEpoch := s.share.sessionEpoch
	s.share.mu.Unlock()
	if sessionStale {
		sc.cfg.logger.Log(LogLevelInfo, "share fetch session was reset mid-flight, extracting ack results only",
			"broker", s.nodeID,
			"sent_epoch", epoch,
			"current_epoch", newEpoch,
		)
	}

	acqLockMillis := resp.AcquisitionLockTimeoutMillis
	if acqLockMillis < 1000 {
		if acqLockMillis <= 0 {
			s.cl.cfg.logger.Log(LogLevelWarn, "broker share fetch response has non-positive AcquisitionLockTimeoutMillis; clamping to 1s",
				"broker", logID(s.nodeID),
				"value", acqLockMillis,
			)
		}
		acqLockMillis = 1000
	}
	acqLockDeadlineNanos := time.Now().Add(time.Duration(acqLockMillis) * time.Millisecond).UnixNano()

	// cursorMap includes both usable and piggyback-only cursors so
	// ack-only partitions in the response are not treated as unknown.
	cursorMap := make(map[tidp]*shareCursor, len(usable)+len(piggybackAcks))
	for _, c := range usable {
		cursorMap[tidp{c.topicID, c.partition}] = c
	}
	for _, d := range piggybackAcks {
		cursorMap[tidp{d.cursor.topicID, d.cursor.partition}] = d.cursor
	}

	var (
		fetch              Fetch
		ackResults         ShareAckResults
		moves              []shareMove
		ackRequeued        int64
		partitionsWithErrs int
		seen               = make(map[tidp]struct{}, len(usable)+len(piggybackAcks))
	)

	for i := range resp.Topics {
		rt := &resp.Topics[i]

		var topicName string
		var partitions []FetchPartition

		for j := range rt.Partitions {
			rp := &rt.Partitions[j]
			tpKey := tidp{rt.TopicID, rp.Partition}
			if _, dup := seen[tpKey]; dup {
				s.cl.cfg.logger.Log(LogLevelWarn, "broker returned duplicate partition in share fetch response, ignoring duplicate",
					"broker", logID(s.nodeID),
					"topic_id", rt.TopicID,
					"partition", rp.Partition,
				)
				continue
			}
			seen[tpKey] = struct{}{}
			cursor := cursorMap[tpKey]
			if cursor == nil {
				s.cl.cfg.logger.Log(LogLevelWarn, "broker returned partition from share fetch that we did not ask for",
					"broker", logID(s.nodeID),
					"topic_id", rt.TopicID,
					"partition", rp.Partition,
				)
				continue
			}
			topicName = cursor.topic

			// Handle acks first,
			pi, hadAcks := piggybackIdx[tpKey]
			if rp.AcknowledgeErrorCode != 0 {
				ackErr := kerr.ErrorForCode(rp.AcknowledgeErrorCode)
				if !hadAcks {
					s.cl.cfg.logger.Log(LogLevelWarn, "broker returned ack error for partition we did not ack",
						"broker", logID(s.nodeID),
						"topic", topicName,
						"partition", rp.Partition,
						"err", ackErr,
					)
				} else if isShareAckRetryable(ackErr) {
					pa := piggybackAcks[pi]
					pa.cursor.requeueEntries(sc, pa.entries)
					ackRequeued += int64(len(pa.entries))
					pa.cursor.requeueGaps(pa.gaps)
				} else {
					ackResults = append(ackResults, ShareAckResult{topicName, rp.Partition, ackErr})
				}
			} else if hadAcks {
				ackResults = append(ackResults, ShareAckResult{topicName, rp.Partition, nil})
			}

			if sessionStale {
				continue // records are unackable, extract ack results only
			}

			// then records.
			if rp.ErrorCode != 0 {
				partitionsWithErrs++
				if rp.CurrentLeader.LeaderID >= 0 && rp.CurrentLeader.LeaderEpoch >= 0 {
					moves = append(moves, shareMove{
						topicID:   rt.TopicID,
						partition: rp.Partition,
						leaderID:  rp.CurrentLeader.LeaderID,
					})
					continue
				}
				partErr := kerr.ErrorForCode(rp.ErrorCode)
				partitions = append(partitions, FetchPartition{
					Partition: rp.Partition,
					Err:       partErr,
				})
				continue
			}

			if len(rp.Records) == 0 && len(rp.AcquiredRecords) == 0 {
				continue
			}

			if len(rp.Records) == 0 && len(rp.AcquiredRecords) > 0 { // buggy broker guard
				s.cl.cfg.logger.Log(LogLevelWarn, "broker share fetch response has acquired records but no record data; releasing for redelivery",
					"broker", logID(s.nodeID),
					"topic", topicName,
					"partition", rp.Partition,
					"acquired_ranges", len(rp.AcquiredRecords),
				)
				s.releaseUndeliverable(cursor, rp.AcquiredRecords, newEpoch)
				continue
			}

			// We could be delivered acks that are no longer
			// assigned (concurrent heartbeat update); that's
			// fine, we deliver anyway and user will ack.
			// The records are acquired for us on the broker,
			// not auto-released.

			fp, gapAcks := s.processSharePartition(topicName, cursor, newEpoch, rp, acqLockDeadlineNanos)
			if len(gapAcks) > 0 {
				cursor.enqueueGaps(gapAcks)
			}

			if len(fp.Records) == 0 && fp.Err == nil {
				continue
			}
			partitions = append(partitions, fp)
		}
		if len(partitions) > 0 {
			fetch.Topics = append(fetch.Topics, FetchTopic{
				Topic:      topicName,
				Partitions: partitions,
			})
		}
	}

	for tp, pi := range piggybackIdx {
		if _, processed := seen[tp]; !processed {
			topic := piggybackAcks[pi].cursor.topic
			s.cl.cfg.logger.Log(LogLevelWarn, "broker omitted partition from share fetch response that we sent acks for",
				"broker", logID(s.nodeID),
				"topic", topic,
				"partition", tp.p,
			)
			ackResults = append(ackResults, ShareAckResult{topic, tp.p, errBrokerOmittedAckPartition})
		}
	}

	var moveBrokers []BrokerMetadata
	if len(moves) > 0 && len(resp.NodeEndpoints) > 0 {
		moveBrokers = make([]BrokerMetadata, 0, len(resp.NodeEndpoints))
		for _, ep := range resp.NodeEndpoints {
			moveBrokers = append(moveBrokers, BrokerMetadata{
				NodeID: ep.NodeID,
				Host:   ep.Host,
				Port:   ep.Port,
				Rack:   ep.Rack,
			})
		}
	}

	return shareFetchResult{
		fetch:           fetch,
		moves:           moves,
		moveBrokers:     moveBrokers,
		ackResults:      ackResults,
		ackRequeued:     ackRequeued,
		allErrsStripped: partitionsWithErrs > 0 && !fetch.hasErrorsOrRecords(),
	}
}

// processSharePartition decodes a single partition from a ShareFetch
// response, filters to acquired records, and generates gap acks.
//
// Gap acks cover offsets in AcquiredRecords ranges that have no
// corresponding record data (compaction holes or decode errors).
// The broker tracks acks in blocks - regardless of whether there are
// actually underlying records. We ack the gaps immediately to free
// up the acquired count on the broker.
func (s *source) processSharePartition(topicName string, cursor *shareCursor, sessionEpoch int32, rp *kmsg.ShareFetchResponseTopicPartition, acqLockDeadlineNanos int64) (FetchPartition, []shareAckRange) {
	sc := s.share.sc
	// Build a synthetic FetchResponseTopicPartition because ShareFetch
	// uses the same wire format for records.
	fakePart := kmsg.NewFetchResponseTopicPartition()
	fakePart.Partition = rp.Partition
	fakePart.RecordBatches = rp.Records
	fakePart.HighWatermark = -1
	fakePart.LastStableOffset = -1
	fakePart.LogStartOffset = -1

	fp, _ := ProcessFetchPartition(ProcessFetchPartitionOpts{
		KeepControlRecords:   sc.cfg.keepControl,
		DisableCRCValidation: sc.cfg.disableFetchCRCValidation,
		Topic:                topicName,
		Partition:            rp.Partition,
		Pools:                sc.cfg.pools,
		shareAckSlab: func(numRecords int, firstRecord *Record) *shareAckSlab {
			return &shareAckSlab{
				states:               make([]shareAckState, numRecords),
				records0:             firstRecord,
				ackSource:            s,
				cursor:               cursor,
				acqLockDeadlineNanos: acqLockDeadlineNanos,
				sessionEpoch:         sessionEpoch,
			}
		},
	}, &fakePart, sc.cfg.decompressor, nil)

	// Defense: validate AcquiredRecords. The two-pointer scan
	// below assumes the ranges are sorted by FirstOffset, non-
	// overlapping, and have FirstOffset <= LastOffset. Buggy
	// brokers may violate any of these.
	//
	// Salvage strategy:
	//   - Sort by FirstOffset (handles unsorted).
	//   - Drop ranges where FirstOffset > LastOffset (degenerate;
	//     unrecoverable per-range, but the rest of the partition
	//     is still usable).
	//   - Merge overlapping ranges into their union (overlapping
	//     acquisition windows shouldn't exist, but if they do,
	//     treat as one wider acquisition).
	if len(rp.AcquiredRecords) > 0 {
		acquired := rp.AcquiredRecords

		// Drop degenerate ranges in-place and detect
		// unsorted/overlapping in the same pass.
		n := 0
		needsFix := false
		for _, ar := range acquired {
			if ar.FirstOffset < 0 || ar.FirstOffset > ar.LastOffset {
				continue
			}
			if n > 0 && ar.FirstOffset <= acquired[n-1].LastOffset {
				needsFix = true
			}
			acquired[n] = ar
			n++
		}
		if n < len(acquired) {
			s.cl.cfg.logger.Log(LogLevelWarn, "broker share fetch response had degenerate AcquiredRecords ranges; skipping",
				"broker", logID(s.nodeID),
				"topic", topicName,
				"partition", rp.Partition,
				"dropped", len(acquired)-n,
			)
			acquired = acquired[:n]
			rp.AcquiredRecords = acquired
		}
		if needsFix {
			s.cl.cfg.logger.Log(LogLevelWarn, "broker share fetch response had unsorted or overlapping AcquiredRecords; salvaging via sort+merge",
				"broker", logID(s.nodeID),
				"topic", topicName,
				"partition", rp.Partition,
			)
			fixed := slices.Clone(acquired)
			slices.SortFunc(fixed, func(a, b kmsg.ShareFetchResponseTopicPartitionAcquiredRecord) int {
				return cmp.Compare(a.FirstOffset, b.FirstOffset)
			})
			out := fixed[:0]
			for _, ar := range fixed {
				if len(out) > 0 && ar.FirstOffset <= out[len(out)-1].LastOffset+1 {
					if ar.LastOffset > out[len(out)-1].LastOffset {
						out[len(out)-1].LastOffset = ar.LastOffset
					}
					continue
				}
				out = append(out, ar)
			}
			rp.AcquiredRecords = out
		}
	}

	// Filter to acquired & gaps using a two-pointer scan.
	gapType := int8(0) // 0 = gap
	if fp.Err != nil { // error codes are handled before entering; an error here is a decode error
		// fp.Err here is a whole-batch decode failure: the batch
		// header, CRC, or compressed payload could not be parsed. kgo
		// does not have per-record deserializers, so there is no
		// per-record decode error to surface -- any future
		// per-record error (e.g. key/value decompression of an
		// individual record inside a decoded batch) is not reported
		// via fp.Err. Current enumeration of causes:
		//   - batch CRC mismatch
		//   - whole-batch decompression failure
		//   - malformed batch header / length
		// Because the entire batch failed to decode, we can't
		// distinguish which acquired offsets belonged to the bad
		// batch vs a successfully-decoded one. We RELEASE the
		// unfilled offsets so the broker re-delivers them after
		// acquisition-lock expiry; Reject would permanently archive
		// records that could be fine on a redelivery from another
		// consumer (e.g. transient corruption on the wire).
		//
		// If the underlying error indicates irrecoverable corruption
		// (not a network/transport issue), reject would be more
		// correct -- but kgo does not currently distinguish those
		// classes, so RELEASE is the safe default.
		gapType = int8(AckRelease)
		sc.cl.cfg.logger.Log(LogLevelWarn, "share fetch decode error on batch; releasing affected offsets for broker redelivery",
			"topic", topicName,
			"partition", rp.Partition,
			"err", fp.Err,
			"acquired_ranges", len(rp.AcquiredRecords),
			"records_decoded", len(fp.Records),
		)
	}
	var (
		gapAcks []shareAckRange
		ri, n   int
	)

	for _, ar := range rp.AcquiredRecords {
		nextExpected := ar.FirstOffset
		for ri < len(fp.Records) && fp.Records[ri].Offset < ar.FirstOffset {
			ri++
		}
		for ri < len(fp.Records) && fp.Records[ri].Offset <= ar.LastOffset {
			r := fp.Records[ri]
			if r.Offset > nextExpected {
				gapAcks = append(gapAcks, shareAckRange{
					firstOffset:  nextExpected,
					lastOffset:   r.Offset - 1,
					source:       s,
					sessionEpoch: sessionEpoch,
					ackType:      gapType,
				})
			}
			slab := r.Context.Value(shareAckKey).(*shareAckSlab)
			idx := int((uintptr(unsafe.Pointer(r)) - uintptr(unsafe.Pointer(slab.records0))) / recSize) //nolint:gosec // pointer arithmetic to index the slab (same as pools.go)
			deliveryCount := int32(ar.DeliveryCount)
			if deliveryCount < 1 {
				deliveryCount = 1 // handle buggy broker; we guarantee 1 min in DeliveryCount docs
			}
			slab.states[idx] = shareAckState{
				deliveryCount: deliveryCount,
			}
			fp.Records[n] = r
			n++
			nextExpected = r.Offset + 1
			ri++
		}
		if nextExpected <= ar.LastOffset {
			gapAcks = append(gapAcks, shareAckRange{
				firstOffset:  nextExpected,
				lastOffset:   ar.LastOffset,
				source:       s,
				sessionEpoch: sessionEpoch,
				ackType:      gapType,
			})
		}
	}

	clear(fp.Records[n:])
	fp.Records = fp.Records[:n]
	return fp, gapAcks
}

// createShareReq builds a ShareFetch request under share.mu.
// Returns nil req if there is nothing to fetch or forget.
// Longer than source.go's createReq because share also drains
// piggybacked acks, filters stale batches, and computes the
// forget set (session diff) inline under the same lock.
func (s *source) createShareReq(skipAckDrain bool) (
	req *kmsg.ShareFetchRequest,
	usable []*shareCursor,
	piggybackAcks []cursorAckDrain,
	sentPiggyback map[tidp]int,
	nAcks int64,
	staleResults ShareAckResults,
	nStaleAcks int64,
	hasRenew bool,
) {
	sc := s.share.sc
	paused := s.cl.consumer.loadPaused()

	s.share.mu.Lock()
	defer s.share.mu.Unlock()

	// Build the WANT set: cursors with assigned=true and not paused.
	// usable (the returned slice) preserves round-robin fairness for
	// req.Topics ordering. wantSet is the set form for the diff
	// against sessionParts below.
	wantSet := make(map[tidp]struct{}, len(s.share.cursors))
	usable = make([]*shareCursor, 0, len(s.share.cursors))
	nShareCursors := len(s.share.cursors)
	ci := s.share.cursorsStart
	for range s.share.cursors {
		c := s.share.cursors[ci]
		ci = (ci + 1) % nShareCursors
		if !c.assigned.Load() || paused.has(c.topic, c.partition) {
			continue
		}
		wantSet[tidp{c.topicID, c.partition}] = struct{}{}
		usable = append(usable, c)
	}
	if nShareCursors > 0 {
		s.share.cursorsStart = (s.share.cursorsStart + 1) % nShareCursors
	}

	// Compute toForget = sessionParts - wantSet. Anything currently
	// in the broker session that we no longer want gets a
	// ForgottenTopicsData entry. Only meaningful at epoch > 0
	// (sessionParts is empty at epoch 0).
	var toForget []tidp
	epoch := s.share.sessionEpoch
	if epoch > 0 {
		for sp := range s.share.sessionParts {
			if _, want := wantSet[sp]; !want {
				toForget = append(toForget, sp)
			}
		}
	}

	// Nothing to fetch AND nothing to forget: hand off to shareAck.
	if len(usable) == 0 && len(toForget) == 0 {
		return
	}

	// Drain acks from ALL cursors on this source (not just usable
	// ones) to piggyback on the ShareFetch request.
	if !skipAckDrain {
		for _, c := range s.share.cursors {
			entries, gaps := c.drainAcks()
			if len(entries) > 0 || len(gaps) > 0 {
				piggybackAcks = append(piggybackAcks, cursorAckDrain{cursor: c, entries: entries, gaps: gaps})
			}
		}
		nAcks, nStaleAcks, staleResults = filterStaleEntries(s, epoch, piggybackAcks)
		// Compute hasRenew once here so callers don't have to re-walk
		// every entry's status atomic in a separate hasRenewAck pass.
	scanRenew:
		for i := range piggybackAcks {
			for _, e := range piggybackAcks[i].entries {
				if int8(e.status.Load()) == int8(AckRenew) {
					hasRenew = true
					break scanRenew
				}
			}
		}
	}

	memberID, _ := sc.memberGen.load()
	req = kmsg.NewPtrShareFetchRequest()
	req.GroupID = &sc.cfg.shareGroup
	req.MemberID = &memberID
	req.ShareSessionEpoch = epoch
	req.MaxWaitMillis = sc.cfg.maxWait
	if nAcks > 0 && req.MaxWaitMillis > 500 {
		req.MaxWaitMillis = 500 // the broker holds the entire response (ack results included) until MaxWait expires or records appear; cap so FlushAcks isn't blocked
	}
	req.MinBytes = sc.cfg.minBytes
	req.MaxBytes = sc.cfg.maxBytes.load()
	maxRecs := sc.cfg.shareMaxRecords
	if maxRecs <= 0 {
		maxRecs = 500 // KIP-1206 default
	}
	req.MaxRecords = maxRecs
	req.BatchSize = maxRecs
	if sc.cfg.shareMaxRecordsStrict {
		req.ShareAcquireMode = 1 // KIP-1206 record-limit mode
	}

	topicIdx := make(map[[16]byte]int, len(usable))
	partIdx := make(map[tidp]int, len(usable))

	// Add usable cursors, skipping those already in the broker
	// session (incremental fetch).
	for _, c := range usable {
		if epoch > 0 {
			if _, inSession := s.share.sessionParts[tidp{c.topicID, c.partition}]; inSession {
				continue
			}
		}
		tidx, ok := topicIdx[c.topicID]
		if !ok {
			tidx = len(req.Topics)
			topicIdx[c.topicID] = tidx
			req.Topics = append(req.Topics, kmsg.ShareFetchRequestTopic{
				TopicID: c.topicID,
			})
		}
		partIdx[tidp{c.topicID, c.partition}] = len(req.Topics[tidx].Partitions)
		req.Topics[tidx].Partitions = append(req.Topics[tidx].Partitions, kmsg.ShareFetchRequestTopicPartition{
			Partition:         c.partition,
			PartitionMaxBytes: sc.cfg.maxPartBytes.load(),
		})
	}

	// Piggyback acks (epoch 0 is a fresh session; the caller
	// handles the drop + callback). If any renew entries are
	// present, the pre-flight in shareFetch sends all acks via
	// standalone ShareAcknowledge and rebuilds without acks.
	if epoch > 0 && len(piggybackAcks) > 0 {
		sentPiggyback = make(map[tidp]int, len(piggybackAcks))
		for i := range piggybackAcks {
			d := &piggybackAcks[i]
			ranges, _ := buildAckRanges(d.entries, d.gaps) // renew unnecessary per comment just above
			if len(ranges) == 0 {
				continue
			}
			tid := d.cursor.topicID
			partition := d.cursor.partition
			sentPiggyback[tidp{tid, partition}] = i
			tidx, ok := topicIdx[tid]
			if !ok {
				tidx = len(req.Topics)
				topicIdx[tid] = tidx
				req.Topics = append(req.Topics, kmsg.ShareFetchRequestTopic{TopicID: tid})
			}
			rt := &req.Topics[tidx]
			key := tidp{tid, partition}
			pi, ok := partIdx[key]
			if !ok {
				pi = len(rt.Partitions)
				partIdx[key] = pi
				rt.Partitions = append(rt.Partitions, kmsg.ShareFetchRequestTopicPartition{
					Partition: partition,
				})
			}
			rp := &rt.Partitions[pi]
			for _, r := range ranges {
				rp.AcknowledgementBatches = append(rp.AcknowledgementBatches,
					kmsg.ShareFetchRequestTopicPartitionAcknowledgementBatch{
						FirstOffset:      r.firstOffset,
						LastOffset:       r.lastOffset,
						AcknowledgeTypes: ackTypes(r.ackType),
					})
			}
		}
	}

	// Forget partitions no longer wanted (sessionParts - wantSet).
	for _, key := range toForget {
		var found bool
		for i := range req.ForgottenTopicsData {
			if req.ForgottenTopicsData[i].TopicID == key.id {
				req.ForgottenTopicsData[i].Partitions = append(req.ForgottenTopicsData[i].Partitions, key.p)
				found = true
				break
			}
		}
		if !found {
			req.ForgottenTopicsData = append(req.ForgottenTopicsData,
				kmsg.ShareFetchRequestForgottenTopicsData{
					TopicID:    key.id,
					Partitions: []int32{key.p},
				})
		}
	}

	return
}
