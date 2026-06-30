package kgo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func (g *groupConsumer) should848() bool {
	if wantBeta := g.cl.ctx.Value("opt_in_kafka_next_gen_balancer_beta"); wantBeta == nil { // !!! TODO REMOVE ONCE BROKER IMPROVES
		return false
	}
	if g.cl.cfg.disableNextGenBalancer {
		return false
	}
	// We pin to v1, introduced in Kafka 4, which fully stabilizes KIP-848.
	if !g.cl.supportsKIP848v1() {
		return false
	}
	switch g.cfg.balancers[0].(type) {
	case *stickyBalancer:
	case *rangeBalancer:
	default:
		return false
	}
	return true
}

// manage848 drives the KIP-848 heartbeat session: it restarts the session on
// transient errors and re-fetches outstanding partitions via g.fetching.
//
// Constraints any change to coordinator/leader-churn recovery (here, in
// heartbeat, and in fetchOffsets) must preserve — established by the
// rebalance-churn audit:
//
//  1. Heartbeat-originated transport and coordinator errors retry in place
//     (the stale-connection cycle 90bcc2bb fixed is real). Fetch errors do
//     NOT take that arm — they propagate here so the session restarts and
//     re-fetches; do not collapse the two error sources back together.
//  2. Session restart is the designed heal for fetch failures: the g.fetching
//     carryover exists precisely so a torn-down session re-fetches. Route
//     fixes through it rather than inventing a second retry inside fetchOffsets.
//  3. Member-identity resets are the minimum the error implies: a fresh UUID
//     for UnknownMemberID, the SAME UUID for epoch problems (FENCED and STALE
//     both keep it). Anything stronger strands server-side state for a full
//     session timeout.
//  4. Leaves (MemberEpoch -1/-2) are idempotent and stateless — safe to retry
//     anywhere. The CGHB no-retry rule applies only to reconciliation-carrying
//     heartbeats, never to leaves.
func (g *groupConsumer) manage848() {
	var serverAssignor string
	switch g.cfg.balancers[0].(type) {
	case *stickyBalancer:
		serverAssignor = "uniform"
	case *rangeBalancer:
		serverAssignor = "range"
	}

	// fallbackToClassic is set when manage848 hands off to the classic
	// manage() goroutine, which takes ownership of closing manageDone.
	var fallbackToClassic bool
	defer func() {
		if !fallbackToClassic {
			close(g.manageDone)
		}
	}()
	var known848Support bool
	optInKnown := func() {
		if known848Support {
			return
		}
		known848Support = true
		g.cfg.logger.Log(LogLevelInfo, "beginning to manage the next-gen group lifecycle", "group", g.cfg.group)
		g.cooperative.Store(true) // next gen is always cooperative
		if !g.cfg.autocommitDisable && g.cfg.autocommitInterval > 0 {
			g.cfg.logger.Log(LogLevelInfo, "beginning autocommit loop", "group", g.cfg.group)
			go g.loopCommit()
		}
	}

	g.mu.Lock()
	g848 := &g848{g: g, serverAssignor: serverAssignor}
	g.g848 = g848
	g.mu.Unlock()

	// v1+ requires the client to generate their own memberID.
	// On v0, the server provides the ID and we join twice.
	// We pin to v1+.
	g.memberGen.store(newStringUUID(), 0) // 0 joins the group

	// consecutiveErrors tracks failures in the outer loop: failed
	// initialJoin attempts and manageFailWait invocations. Used for
	// backoff scaling. Reset when the inner heartbeat loop is entered
	// (meaning initialJoin succeeded).
	var consecutiveErrors int
	var unreleasedInstanceRetries int // capped at 3; see retryable-error branch below
outer:
	for {
		initialHb, err := g848.initialJoin()

		// Even if Kafka replies that the API is available, if we use it
		// and the broker is not configured to support it, we receive
		// UnsupportedVersion. Until a join attempt settles the question
		// (any other kerr, or success, means the API is really served),
		// an UnsupportedVersion error falls back to classic group
		// management.
		if !known848Support {
			if err != nil {
				var ke *kerr.Error
				if errors.As(err, &ke) {
					if ke.Code == kerr.UnsupportedVersion.Code {
						// It's okay to update is848 here. This is used while leaving
						// and while heartbeating. We have not yet entered heartbeating.
						g.mu.Lock()
						g.is848 = false
						g.mu.Unlock()
						// We pre-stored a self-generated member id (v1 semantics)
						// that the server never admitted - the join just failed.
						// Clear it so a concurrent leave returns early (there is
						// no member to remove; both the 848 and classic leave
						// paths check for an empty member id) and so the classic
						// join below starts with an empty member id rather than
						// burning an UNKNOWN_MEMBER_ID round trip on our UUID.
						g.memberGen.store("", -1)
						g.cfg.logger.Log(LogLevelInfo, "falling back to standard consumer group management due to lack of broker support", "group", g.cfg.group)
						fallbackToClassic = true
						go g.manage()
						return
					}
					optInKnown() // A kerr that is NOT UnsupportVersion means this is supported.
				}
				// For non-kerr errors, we fall into normal logic below and retry.
			} else {
				optInKnown()
			}
		}

		// Retryable errors from initialJoin should not be surfaced
		// to the user: coordinator errors and broker-level errors
		// (connection closed, EOF) are transient, so we backoff and
		// retry without going through manageFailWait.
		//
		// UnreleasedInstanceID is retryable-with-a-cap: the broker
		// briefly keeps old static-instance state after
		// FencedMemberEpoch, so an immediate rejoin can race for
		// 1-2 cycles. Beyond 3 attempts it indicates a real
		// cross-process InstanceID conflict and we fall through to
		// manageFailWait so the user sees it (matches Java's
		// handleFatalFailure semantics, with a small race budget).
		retryable := err != nil && (g.cl.maybeDeleteStaleCoordinator(g.cfg.group, coordinatorTypeGroup, err) ||
			isRetryableBrokerErr(err) || isAnyDialErr(err) ||
			errors.Is(err, kerr.UnreleasedInstanceID) || errors.Is(err, kerr.StaleMemberEpoch))

		giveUp := false
		if retryable && errors.Is(err, kerr.UnreleasedInstanceID) {
			unreleasedInstanceRetries++
			if unreleasedInstanceRetries > 3 {
				g.cfg.logger.Log(LogLevelError,
					"UnreleasedInstanceID after 3 retries - static instance is held by another member; check for duplicate InstanceID config or an unclean peer shutdown",
					"group", g.cfg.group,
					"instance", g.cfg.instanceID,
				)
				giveUp = true
			}
		}

		if retryable && !giveUp {
			// StaleMemberEpoch on initialJoin means the server
			// remembers our memberID at a later epoch than the 0
			// we sent. Reset to a fresh member id so the retry
			// joins as a new member.
			if errors.Is(err, kerr.StaleMemberEpoch) {
				g.memberGen.store(newStringUUID(), 0)
			}
			consecutiveErrors++
			g.cfg.logger.Log(LogLevelInfo, "consumer group initial heartbeat hit retryable error, backing off and retrying",
				"group", g.cfg.group,
				"err", err,
				"consecutive_errors", consecutiveErrors,
				"unreleased_instance_retries", unreleasedInstanceRetries,
			)
			backoff := g.cfg.retryBackoff(consecutiveErrors)
			g.cl.waitmeta(g.ctx, backoff, "waitmeta during 848 retryable error backoff")
			after := time.NewTimer(backoff)
			select {
			case <-g.ctx.Done():
				after.Stop()
				return
			case <-after.C:
			}
			continue
		}

		// consecutiveTransientRestarts tracks how many times the
		// inner heartbeat session has silently restarted due to
		// transient errors (EOF, connection refused, etc.) without
		// any successful heartbeat in between. Every cfg.retries
		// consecutive restarts, we inject a fake fetch error so the
		// user knows the broker is unreachable. Reset on success.
		var consecutiveTransientRestarts int
		for err == nil {
			consecutiveErrors = 0
			unreleasedInstanceRetries = 0
			var nowAssigned map[string][]int32

			// In heartbeating, if we lose or gain partitions, we need to
			// exit the old heartbeat and re-enter setupAssignedAndHeartbeat.
			// Otherwise, we heartbeat exactly the same as the old.
			//
			// This results in a few more heartbeats than necessary
			// when things are changing, but keeps all the old
			// logic that handles all edge conditions.
			//
			// setupAssignedAndHeartbeat starts prerevoke (for
			// lost partitions) and heartbeating concurrently.
			// While prerevoking, the heartbeat sends keepalive
			// (Topics=nil) so the server does not see partitions
			// released before the user commits their offsets in
			// any OnPartitionsRevoked callback.
			// The prerevoke goroutine clears prerevoking when
			// revocation and offset commits are complete, and
			// subsequent heartbeats resume sending full requests.
			_, err = g.setupAssignedAndHeartbeat(initialHb, func() (time.Duration, error) {
				req := g848.mkreq()
				prerevoking := g848.prerevoking.Load()
				// When the client has no partitions (Topics is empty),
				// always send a full request rather than a keepalive.
				// This ensures the server sees our actual empty
				// assignment state. Without this, a lost response
				// containing our assignment can leave the server
				// thinking we acknowledged partitions we never
				// received: the server marks the assignment as
				// delivered, but we never got it. Keepalive
				// (Topics=nil) means "no change", which doesn't
				// correct the stale server state. Sending Topics=[]
				// tells the server "I have nothing", forcing it to
				// re-deliver.
				topicsMatch := len(req.Topics) > 0 && reflect.DeepEqual(g848.lastSubscribedTopics, req.SubscribedTopicNames) && reflect.DeepEqual(g848.lastTopics, req.Topics)
				if prerevoking || topicsMatch {
					req.InstanceID = nil
					req.RackID = nil
					req.RebalanceTimeoutMillis = -1
					req.ServerAssignor = nil
					req.SubscribedTopicRegex = nil
					req.SubscribedTopicNames = nil
					req.Topics = nil
				}
				resp, err := req.RequestWith(g.ctx, g.cl)
				sleep := g.cfg.heartbeatInterval
				if err == nil {
					err = errCodeMessage(resp.ErrorCode, resp.ErrorMessage)
					// A zero or negative server interval (buggy or
					// hostile broker) would hot-loop heartbeats at
					// round-trip pace; keep the configured cadence.
					if hb := time.Duration(resp.HeartbeatIntervalMillis) * time.Millisecond; hb > 0 {
						sleep = hb
					}
				}
				if err != nil {
					// Reset last-sent state so the next attempt
					// is a full request. If the server processed
					// our request but we lost the response, the
					// full retry corrects the server's view of
					// our current partitions.
					g848.lastTopics = nil
					g848.lastSubscribedTopics = nil
					return sleep, err
				}
				hbAssigned := g848.handleResp(req, resp)
				if hbAssigned != nil {
					err = errReassigned848
					nowAssigned = hbAssigned
				}
				return sleep, err
			})

			switch {
			case errors.Is(err, kerr.RebalanceInProgress):
				err = nil

			// Retryable broker errors (connection closed, EOF),
			// dial errors (connection refused - the broker may be
			// restarting), and coordinator errors
			// (NOT_COORDINATOR, etc.) are retried in the
			// heartbeat loop up to cfg.retries. If the cap is
			// hit, the error propagates here. We silently restart
			// the session and keep retrying. Every cfg.retries
			// consecutive restarts, we inject a fake fetch error
			// so the user knows the broker is unreachable.
			case isRetryableBrokerErr(err),
				isAnyDialErr(err),
				g.cl.maybeDeleteStaleCoordinator(g.cfg.group, coordinatorTypeGroup, err):
				consecutiveTransientRestarts++
				if shouldNotify848Restart(int64(consecutiveTransientRestarts), g.cfg.retries) {
					g.c.addFakeReadyForDraining("", 0, &ErrGroupSession{
						Err: fmt.Errorf("consumer group %s heartbeat has been failing for %d consecutive attempts, still retrying: %w", g.cfg.group, consecutiveTransientRestarts, err),
					}, "consumer group heartbeat persistently failing")
				}
				err = nil
				// Continue directly: we nil err only to keep the
				// session loop going, not because anything
				// succeeded. Falling into the err == nil reset
				// below would zero the counter we just
				// incremented, capping it at 1 forever and making
				// the every-cfg.retries notification above
				// unreachable. The reset is for the other arms,
				// whose nil means a processed response.
				continue

			case errors.Is(err, kerr.UnknownMemberID):
				// The server forgot us (session expired during an
				// outage, or we were administratively removed).
				// Abandon the assignment and re-initialJoin with a
				// fresh member id.
				member, gen := g.memberGen.load()
				g.cfg.logger.Log(LogLevelInfo, "consumer group heartbeat error, abandoning assignment and rejoining with new member id",
					"group", g.cfg.group,
					"member_id", member,
					"generation", gen,
					"err", err,
				)
				g.abandonAssignment(fmt.Sprintf("abandoning assignment after %v", err))
				g.memberGen.store(newStringUUID(), 0)
				continue outer

			// StaleMemberEpoch means our epoch drifted from the
			// server's; it reaches us via OffsetFetch (the heartbeat
			// itself fences with FencedMemberEpoch), so the server
			// still has this member. We must KEEP our member id:
			// rejoining at epoch 0 with the same id is the protocol's
			// lost-response recovery - the server re-admits the member
			// in place and re-delivers its assignment. Rejoining with
			// a fresh id would strand the old member server-side,
			// parking its partitions until the session timeout.
			case errors.Is(err, kerr.FencedMemberEpoch),
				errors.Is(err, kerr.StaleMemberEpoch),
				errors.Is(err, kerr.GroupMaxSizeReached),
				errors.Is(err, kerr.UnsupportedAssignor):
				lvl := LogLevelInfo
				if errors.Is(err, kerr.GroupMaxSizeReached) {
					lvl = LogLevelWarn
				} else if errors.Is(err, kerr.UnsupportedAssignor) {
					lvl = LogLevelError
				}
				member, gen := g.memberGen.load()
				g.cfg.logger.Log(lvl, "consumer group heartbeat error, abandoning assignment and rejoining",
					"group", g.cfg.group,
					"member_id", member,
					"generation", gen,
					"err", err,
				)
				g.abandonAssignment(fmt.Sprintf("abandoning assignment after %v", err))
				continue outer
			}

			if err == nil {
				consecutiveTransientRestarts = 0
			}
			if nowAssigned != nil {
				member, gen := g.memberGen.load()
				g.cfg.logger.Log(LogLevelInfo, "consumer group heartbeat detected an updated assignment; exited heartbeat loop to assign & reentering",
					"group", g.cfg.group,
					"member_id", member,
					"generation", gen,
					"now_assigned", nowAssigned,
				)
				// handleResp already stored nowAssigned (before memberGen)
				// so concurrent readers see a consistent (gen, assignment)
				// pair; no additional store needed here.
			}
		}

		// The errors we have to handle are:
		// * UnknownMemberID: abandon partitions, rejoin w/ new member id
		// * FencedMemberEpoch / StaleMemberEpoch: abandon partitions,
		//   rejoin with the SAME member id
		// * UnreleasedInstanceID: fatal error, do not rejoin
		// * General error: fatal error, do not rejoin
		//
		// In the latter two cases, we fall into rejoining anyway
		// because it is both non-problematic (we will keep failing
		// with the same error) and because it will cause the user
		// to repeatedly get error logs.
		//
		// Note that manageFailWait calls nowAssigned.store(nil),
		// meaning our initialJoin should *always* have an empty
		// Topics in the request.
		consecutiveErrors++
		ctxCanceled := g.manageFailWait(consecutiveErrors, err)
		if ctxCanceled {
			return
		}
	}
}

// shouldNotify848Restart reports whether a transient-restart count warrants
// surfacing the "heartbeat persistently failing" notification - the only
// user-visible signal that an 848 group is unreachable: once we have
// restarted at least `retries` times, on every `retries`-th restart.
//
// retries <= 0 means the user disabled retries (RequestRetries(0)). That also
// disables in-session heartbeat retries - heartbeat() propagates the first
// transient error immediately rather than retrying in place - so every
// transient error is its own restart and each one warrants the notification.
// We must therefore notify on every restart, NOT divide restarts by zero
// (an integer divide-by-zero panic that would crash the manage goroutine on
// the first transient heartbeat error).
func shouldNotify848Restart(restarts, retries int64) bool {
	if retries < 1 {
		return true
	}
	return restarts >= retries && restarts%retries == 0
}

func (g *groupConsumer) leave848(ctx context.Context) {
	memberID := g.memberGen.memberID()
	g.cfg.logger.Log(LogLevelInfo, "leaving next-gen group",
		"group", g.cfg.group,
		"member_id", memberID,
		"instance_id", g.cfg.instanceID,
	)
	// If we error when leaving, there is not much
	// we can do. We may as well just return.
	req := kmsg.NewPtrConsumerGroupHeartbeatRequest()
	req.Group = g.cfg.group
	req.MemberID = memberID
	req.MemberEpoch = -1
	if g.cfg.instanceID != nil {
		req.MemberEpoch = -2
	}

	resp, err := req.RequestWith(ctx, g.cl)
	if err != nil {
		g.leaveErr = err
		return
	}
	err = errCodeMessage(resp.ErrorCode, resp.ErrorMessage)
	// The leave rides the coordinator retry wrapper: if a prior attempt
	// succeeded but its response was lost (the connection died), the
	// retry finds the member already gone. Same if the session expired
	// before we could leave. Either way the member is out of the group,
	// which is the goal state of leaving, not an error.
	if errors.Is(err, kerr.UnknownMemberID) {
		err = nil
	}
	g.leaveErr = err
}

type g848 struct {
	g *groupConsumer

	serverAssignor string

	lastSubscribedTopics []string
	lastTopics           []kmsg.ConsumerGroupHeartbeatRequestTopic

	// unresolvedAssigned holds topic IDs from a heartbeat
	// response that could not be mapped to a name via id2t.
	// These are included in subsequent heartbeat Topics so the
	// server sees them acknowledged. When metadata resolves the
	// ID, the topic is moved into newAssigned.
	unresolvedAssigned map[topicID][]int32

	// prerevoking is true while prerevoke is running: the
	// assignment has changed and lost partitions are being
	// revoked and their offsets committed. While true, the
	// heartbeat closure sends keepalive (Topics=nil) so the
	// server does not see partitions as released before offsets
	// are committed. Cleared by the prerevoke goroutine.
	prerevoking atomic.Bool
}

// sanitizePartitions returns the broker-provided partitions sorted, with
// duplicates and negatives dropped. A duplicated partition is not just
// redundant: it survives into nowAssigned, makes the assignment compare as
// changed, and diffAssigned then re-"adds" the partition we already own -
// re-fetching its committed offset and rewinding the live cursor into
// duplicate consumption. Negative numbers can only come from a buggy or
// hostile broker and would otherwise flow into the offset-load machinery.
func sanitizePartitions(ps []int32) []int32 {
	ps = slices.Clone(ps)
	slices.Sort(ps)
	ps = slices.Compact(ps)
	for len(ps) > 0 && ps[0] < 0 {
		ps = ps[1:]
	}
	return ps
}

// v1+ requires the end user to generate their own MemberID, with the
// recommendation being v4 uuid base64 encoded so it can be put in URLs. We
// roughly do that (no version nor variant bits). crypto/rand does not fail
// and, in future Go versions, will panic on internal errors.
func newStringUUID() string {
	var uuid [16]byte
	io.ReadFull(rand.Reader, uuid[:]) // even more random than adding version & variant bits is having full randomness
	return base64.URLEncoding.EncodeToString(uuid[:])
}

func (g *g848) initialJoin() (time.Duration, error) {
	// handleResp publishes nowAssigned BEFORE memberGen so that any
	// concurrent commit retry observing the new gen also sees the new
	// assignment (see comment in handleResp). Verify the invariant up
	// front: a clean initialJoin must enter with no assignment so that
	// the publish order remains the source of truth.
	if g.g.nowAssigned.read() != nil {
		panic("nowAssigned is not nil in our initial join, invalid invariant!")
	}
	g.g.memberGen.storeGeneration(0)
	g.lastSubscribedTopics = nil
	g.lastTopics = nil
	// A (re)join must carry an EMPTY owned-partitions list: the broker
	// rejects any epoch-0 heartbeat whose Topics is non-empty (or null)
	// with INVALID_REQUEST, "TopicPartitions must be empty when
	// (re-)joining." unresolvedAssigned holds the OLD member's
	// server-side assignment, and mkreq folds it into Topics - so
	// carrying it across a member reset would poison every join, and
	// permanently: the only other thing that clears unresolvedAssigned
	// is a successful assignment-carrying response, which a rejected
	// join never produces. Dropping it loses nothing - the join
	// response always re-delivers the member's full assignment. The
	// Java client likewise clears its unresolved-IDs cache on every
	// transition to joining.
	g.unresolvedAssigned = nil
	g.prerevoking.Store(false)
	// Drain any stale rejoin signal, mirroring joinAndSync. Nothing
	// else on the 848 path consumes the channel across a member reset:
	// if a signal was queued (e.g. metadata found new matching topics)
	// and the heartbeat loop then exited on a fatal error without
	// reading it, the signal would survive into the session we are
	// about to build and immediately bounce it. The join below already
	// carries our current subscription (mkreq reads live state), which
	// is everything a queued signal could ask for.
	select {
	case <-g.g.rejoinCh:
	default:
	}
	req := g.mkreq()
	resp, err := req.RequestWith(g.g.ctx, g.g.cl)
	if err == nil {
		err = errCodeMessage(resp.ErrorCode, resp.ErrorMessage)
	}
	if err != nil {
		return 0, err
	}
	nowAssigned := g.handleResp(req, resp)
	member, gen := g.g.memberGen.load()
	g.g.cfg.logger.Log(LogLevelInfo, "consumer group initial heartbeat received assignment",
		"group", g.g.cfg.group,
		"member_id", member,
		"generation", gen,
		"now_assigned", nowAssigned,
	)

	// As in the heartbeat closure: never adopt a zero/negative server
	// interval, it would hot-loop the heartbeat timer.
	if hb := time.Duration(resp.HeartbeatIntervalMillis) * time.Millisecond; hb > 0 {
		return hb, nil
	}
	return g.g.cfg.heartbeatInterval, nil
}

func (g *g848) handleResp(req *kmsg.ConsumerGroupHeartbeatRequest, resp *kmsg.ConsumerGroupHeartbeatResponse) map[string][]int32 {
	// A success response can only legitimately carry a negative member
	// epoch as the echo of a leave (-1, or -2 static), which this loop
	// never sends; the broker rejects requests below -2 outright. If a
	// buggy or hostile broker hands us a negative epoch here and we
	// store it, our next heartbeat would BE a leave: the member silently
	// exits the group while fetches continue. Ignore the response
	// entirely, like the Java client does.
	if resp.MemberEpoch < 0 {
		g.g.cfg.logger.Log(LogLevelWarn, "ignoring consumer group heartbeat response with an invalid negative member epoch",
			"group", g.g.cfg.group,
			"epoch", resp.MemberEpoch,
		)
		return nil
	}

	id2t := g.g.cl.id2tMap()
	newAssigned := make(map[string][]int32)

	// Only update the last-sent fields when Topics was actually
	// included in the request. When the request was a keepalive
	// (Topics=nil), we preserve the previous values so the next
	// comparison still matches and produces another keepalive.
	// Without this guard, storing nil after a keepalive causes
	// DeepEqual(nil, []) to fail on the next heartbeat, re-sending
	// Topics=[] - creating an alternating full/keepalive pattern.
	// The server clears pendingRevocations when Topics does not
	// contain a pending partition, so the alternating stale full
	// heartbeats can cause premature revocation clearing and dual
	// assignment.
	if req.Topics != nil {
		g.lastSubscribedTopics = req.SubscribedTopicNames
		g.lastTopics = req.Topics
	}

	if resp.Assignment != nil {
		// Fresh assignment from server - replace unresolved state.
		g.unresolvedAssigned = nil
		for _, t := range resp.Assignment.Topics {
			ps := sanitizePartitions(t.Partitions)
			name := id2t[t.TopicID]
			if name == "" {
				if g.unresolvedAssigned == nil {
					g.unresolvedAssigned = make(map[topicID][]int32)
				}
				g.unresolvedAssigned[topicID(t.TopicID)] = ps
				continue
			}
			newAssigned[name] = ps
		}
	}

	// Try to resolve previously-unresolved topic IDs now that
	// metadata may have refreshed since the last heartbeat.
	// We don't proactively hook into the metadata update to
	// resolve immediately - waiting for the next heartbeat is
	// simpler and only costs one heartbeat interval (~5s).
	for id, ps := range g.unresolvedAssigned {
		if name := id2t[[16]byte(id)]; name != "" {
			newAssigned[name] = ps
			delete(g.unresolvedAssigned, id)
		}
	}
	if len(g.unresolvedAssigned) > 0 {
		g.g.cl.triggerUpdateMetadataNow("consumer group heartbeat has unresolved topic IDs in assignment")
	}

	// storeMember publishes the new memberGen. We defer it so it runs AFTER
	// any nowAssigned.store below. Atomic stores in Go are sequentially
	// consistent, so concurrent readers (e.g. the commit() STALE retry
	// filter at consumer_group.go) cannot observe new memberGen with
	// stale nowAssigned: seeing the new gen happens-after seeing the new
	// assignment. This closes the race that would otherwise leak revoked
	// partitions into a retried commit.
	storeMember := func() {
		if resp.MemberID != nil {
			g.g.memberGen.store(*resp.MemberID, resp.MemberEpoch)
			g.g.cl.cfg.logger.Log(LogLevelDebug, "storing member and epoch", "group", g.g.cfg.group, "member", *resp.MemberID, "epoch", resp.MemberEpoch)
		} else {
			g.g.memberGen.storeGeneration(resp.MemberEpoch)
			g.g.cl.cfg.logger.Log(LogLevelDebug, "storing epoch", "group", g.g.cfg.group, "epoch", resp.MemberEpoch)
		}
	}
	defer storeMember()

	// Only return nil (no change) when the response had no
	// assignment at all (keepalive) or when all topics in the
	// assignment are still unresolved. When the server explicitly
	// sends an empty assignment (resp.Assignment != nil with no
	// topics), fall through to the comparison so the client
	// detects it as "revoke everything".
	//
	// The unresolved check handles the case where the server
	// assigned topics whose IDs the client can't map to names
	// yet (e.g. newly created topic, metadata not refreshed).
	// Those went into unresolvedAssigned rather than
	// newAssigned. Without this guard, we'd fall through with
	// newAssigned={} and tell the client to revoke everything,
	// when really the server did assign partitions - we just
	// need to wait for metadata resolution.
	if len(newAssigned) == 0 && (resp.Assignment == nil || len(g.unresolvedAssigned) > 0) {
		return nil
	}

	// Merge with current assignment: newAssigned only has topics
	// from the response or freshly resolved; fill in any existing
	// topics the server didn't mention (resp.Assignment == nil).
	current := g.g.nowAssigned.read()
	if resp.Assignment == nil {
		for t, ps := range current {
			if _, ok := newAssigned[t]; !ok {
				newAssigned[t] = ps
			}
		}
	}

	if !mapi32sDeepEq(current, newAssigned) {
		// Store BEFORE the deferred storeMember runs, so an observer that
		// sees new memberGen via memberGen.load() is guaranteed to also
		// see the matching nowAssigned via nowAssigned.read().
		g.g.nowAssigned.store(newAssigned)
		return newAssigned
	}
	return nil
}

func (g *g848) mkreq() *kmsg.ConsumerGroupHeartbeatRequest {
	req := kmsg.NewPtrConsumerGroupHeartbeatRequest()
	req.Group = g.g.cfg.group
	req.MemberID, req.MemberEpoch = g.g.memberGen.load()

	// Most fields in the request can be null if the field is equal to the
	// last time we sent the request. The first time we write, we include
	// all information. We always return all information here; the caller
	// of mkreq may strip fields as needed.
	//
	// Our initial set of subscribed topics is specified in our config.
	// For non-regex consuming, topics are directly created into g.tps.
	// As well, g.tps is added to or purged from in AddConsumeTopics or
	// PurgeConsumeTopics. We can always use g.tps for direct topics the
	// user wants to consume.
	//
	// For regex topics, they cannot add or remove after client creation.
	// We just use the initial config field.

	req.InstanceID = g.g.cfg.instanceID
	if g.g.cfg.rack != "" {
		req.RackID = &g.g.cfg.rack
	}
	req.RebalanceTimeoutMillis = int32(g.g.cfg.rebalanceTimeout.Milliseconds())
	req.ServerAssignor = &g.serverAssignor

	tps := g.g.tps.load()
	if g.g.cl.cfg.regex && len(g.g.cl.cfg.excludeTopics) > 0 {
		// KIP-848's SubscribedTopicRegex is include-only with no exclude
		// counterpart. When excludes are configured, fall back to sending
		// the already-resolved topic names from g.tps (which
		// filterMetadataAllTopics populates with excludes applied).
		// New topics are picked up on the next metadata refresh.
		//
		// Skip internal topics: classic regex consuming never uses them
		// (findNewAssignments skips isInternal), and the broker honors
		// explicit name subscriptions to internal topics, so emulating
		// the regex with names would otherwise consume e.g.
		// __consumer_offsets whenever the regex matches it.
		subscribedTopics := make([]string, 0, len(tps))
		for t, tp := range tps {
			if tp.load().isInternal {
				continue
			}
			subscribedTopics = append(subscribedTopics, t)
		}
		slices.Sort(subscribedTopics)
		req.SubscribedTopicNames = subscribedTopics
	} else if g.g.cl.cfg.regex {
		topics := g.g.cl.cfg.topics
		patterns := make([]string, 0, len(topics))
		for topic := range topics {
			patterns = append(patterns, "(?:"+topic+")")
		}
		slices.Sort(patterns)
		pattern := strings.Join(patterns, "|")
		req.SubscribedTopicRegex = &pattern
	} else {
		// SubscribedTopics must always exist when epoch == 0.
		// We specifically 'make' the slice to ensure it is non-nil.
		subscribedTopics := make([]string, 0, len(tps))
		for t := range tps {
			subscribedTopics = append(subscribedTopics, t)
		}
		slices.Sort(subscribedTopics)
		req.SubscribedTopicNames = subscribedTopics
	}
	// Build Topics from our current assignment. The heartbeat closure
	// uses prerevoking to strip this to nil during prerevoke,
	// preventing the server from seeing released partitions before
	// offsets are committed.
	//
	// A topic may be in nowAssigned but absent from tps if the user
	// just called PurgeFetchTopics: purge removes from tps, but
	// nowAssigned reflects the server's view and is only updated when
	// the server acknowledges the revoke in a future heartbeat
	// response. We skip such topics here; SubscribedTopicNames (built
	// from tps above) signals the unsubscribe and the server revokes
	// on the next response.
	nowAssigned := g.g.nowAssigned.read()
	req.Topics = []kmsg.ConsumerGroupHeartbeatRequestTopic{} // ALWAYS initialize: len 0 is significantly different than nil (nil means same as last time)
	for t, ps := range nowAssigned {
		tp, ok := tps[t]
		if !ok {
			continue
		}
		rt := kmsg.NewConsumerGroupHeartbeatRequestTopic()
		rt.Partitions = slices.Clone(ps)
		rt.TopicID = tp.load().id
		req.Topics = append(req.Topics, rt)
	}
	// Include unresolved topic IDs so the server sees them
	// acknowledged. This also makes topicsMatch false (since
	// lastTopics won't contain these), forcing a full request
	// whose isFullRequest triggers a re-send of the assignment.
	for id, ps := range g.unresolvedAssigned {
		rt := kmsg.NewConsumerGroupHeartbeatRequestTopic()
		rt.TopicID = [16]byte(id)
		rt.Partitions = slices.Clone(ps)
		req.Topics = append(req.Topics, rt)
	}

	// Canonicalize ordering by TopicID so the heartbeat closure's
	// DeepEqual against lastTopics is stable across map-iteration
	// orderings, avoiding unnecessary full heartbeats.
	slices.SortFunc(req.Topics, func(a, b kmsg.ConsumerGroupHeartbeatRequestTopic) int {
		return bytes.Compare(a.TopicID[:], b.TopicID[:])
	})

	return req
}
