package kgo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type groupConsumer struct {
	c   *consumer // used to change consumer state; generally c.mu is grabbed on access
	cl  *Client   // used for running requests / adding to topics map
	cfg *cfg

	ctx        context.Context
	cancel     func()
	manageDone chan struct{} // closed once when the manage goroutine quits

	cooperative atomicBool // true if the group balancer chosen during Join is cooperative

	// The data for topics that the user assigned. Metadata updates the
	// atomic.Value in each pointer atomically.
	tps *topicsPartitions

	reSeen map[string]bool // topics we evaluated against regex, and whether we want them or not

	// Full lock grabbed in CommitOffsetsSync, read lock grabbed in
	// CommitOffsets, this lock ensures that only one sync commit can
	// happen at once, and if it is happening, no other commit can be
	// happening.
	syncCommitMu sync.RWMutex

	rejoinCh chan string // cap 1; sent to if subscription changes (regex)

	// For EOS, before we commit, we force a heartbeat. If the client and
	// group member are both configured properly, then the transactional
	// timeout will be less than the session timeout. By forcing a
	// heartbeat before the commit, if the heartbeat was successful, then
	// we ensure that we will complete the transaction within the group
	// session, meaning we will not commit after the group has rebalanced.
	heartbeatForceCh chan func(error)

	// The following two are only updated in the manager / join&sync loop
	// The nowAssigned map is read when commits fail: if the commit fails
	// with ILLEGAL_GENERATION and it contains only partitions that are in
	// nowAssigned, we re-issue.
	lastAssigned map[string][]int32
	nowAssigned  amtps

	// Fetching ensures we continue fetching offsets across cooperative
	// rebalance if an offset fetch returns early due to an immediate
	// rebalance. See the large comment on adjustCooperativeFetchOffsets
	// for more details.
	//
	// This is modified only in that function, or in the manage loop on a
	// hard error once the heartbeat/fetch has returned.
	fetching map[string]map[int32]struct{}

	// onFetchedMu ensures we do not call onFetched nor adjustOffsets
	// concurrent with onRevoked.
	//
	// The group session itself ensures that OnPartitions functions are
	// serial, but offset fetching is concurrent with heartbeating and can
	// finish before or after heartbeating has already detected a revoke.
	// To make user lives easier, we guarantee that offset fetch callbacks
	// cannot be concurrent with onRevoked with this mu. If fetch callbacks
	// are present, we hook this mu into onRevoked, and we grab it in the
	// locations fetch callbacks are called. We only have to worry about
	// onRevoked because fetching offsets occurs after onAssigned, and
	// onLost happens after fetching offsets is done.
	onFetchedMu sync.Mutex

	// leader is whether we are the leader right now. This is set to false
	//
	//  - set to false at the beginning of a join group session
	//  - set to true if join group response indicates we are leader
	//  - read on metadata updates in findNewAssignments
	leader atomicBool

	// Set to true when ending a transaction committing transaction
	// offsets, and then set to false immediately after before calling
	// EndTransaction.
	offsetsAddedToTxn bool

	// If we are leader, then other members may express interest to consume
	// topics that we are not interested in consuming. We track the entire
	// group's topics in external, and our fetchMetadata loop uses this.
	// We store this as a pointer for address comparisons.
	external atomic.Value // *groupExternal

	// See the big comment on `commit`. If we allow committing between
	// join&sync, we occasionally see RebalanceInProgress or
	// IllegalGeneration errors while cooperative consuming.
	noCommitDuringJoinAndSync sync.RWMutex

	//////////////
	// mu block //
	//////////////
	mu sync.Mutex

	// using is updated when finding new assignments, we always add to this
	// if we want to consume a topic (or see there are more potential
	// partitions). Only the leader can trigger a new group session if there
	// are simply more partitions for existing topics.
	//
	// This is read when joining a group or leaving a group.
	using map[string]int // topics *we* are currently using => # partitions known in that topic

	// uncommitted is read and updated all over:
	// - updated before PollFetches returns
	// - updated when directly setting offsets (to rewind, for transactions)
	// - emptied when leaving a group
	// - updated when revoking
	// - updated after fetching offsets once we receive our group assignment
	// - updated after we commit
	// - read when getting uncommitted or committed
	uncommitted uncommitted

	// memberID and generation are written to in the join and sync loop,
	// and mostly read within that loop. This can be read during commits,
	// which can happy any time. It is **recommended** to be done within
	// the context of a group session, but (a) users may have some unique
	// use cases, and (b) the onRevoke hook may take longer than a user
	// expects, which would rotate a session.
	memberGen groupMemberGen

	// commitCancel and commitDone are set under mu before firing off an
	// async commit request. If another commit happens, it cancels the
	// prior commit, waits for the prior to be done, and then starts its
	// own.
	commitCancel func()
	commitDone   chan struct{}

	// blockAuto is set and cleared in CommitOffsets{,Sync} to block
	// autocommitting if autocommitting is active. This ensures that an
	// autocommit does not cancel the user's manual commit.
	blockAuto bool

	// We set this once to manage the group lifecycle once.
	managing bool

	dying    bool // set when closing, read in findNewAssignments
	left     chan struct{}
	leaveErr error // set before left is closed
}

type groupMemberGen struct {
	v atomic.Value // *groupMemberGenT
}

type groupMemberGenT struct {
	memberID   string
	generation int32
}

func (g *groupMemberGen) memberID() string {
	memberID, _ := g.load()
	return memberID
}

func (g *groupMemberGen) generation() int32 {
	_, generation := g.load()
	return generation
}

func (g *groupMemberGen) load() (memberID string, generation int32) {
	v := g.v.Load()
	if v == nil {
		return "", -1
	}
	t := v.(*groupMemberGenT)
	return t.memberID, t.generation
}

func (g *groupMemberGen) store(memberID string, generation int32) {
	g.v.Store(&groupMemberGenT{memberID, generation})
}

func (g *groupMemberGen) storeMember(memberID string) {
	g.store(memberID, g.generation())
}

// LeaveGroup leaves a group. Close automatically leaves the group, so this is
// only necessary to call if you plan to leave the group but continue to use
// the client. If a rebalance is in progress, this function waits for the
// rebalance to complete before the group can be left. This is necessary to
// allow you to safely issue one final offset commit in OnPartitionsRevoked. If
// you have overridden the default revoke, you must manually commit offsets
// before leaving the group.
//
// If you have configured the group with an InstanceID, this does not leave the
// group. With instance IDs, it is expected that clients will restart and
// re-use the same instance ID. To leave a group using an instance ID, you must
// manually issue a kmsg.LeaveGroupRequest or use an external tool (kafka
// scripts or kcl).
//
// It is recommended to use LeaveGroupContext to see if the leave was
// successful.
func (cl *Client) LeaveGroup() {
	cl.LeaveGroupContext(cl.ctx)
}

// LeaveGroup leaves a group. Close automatically leaves the group, so this is
// only necessary to call if you plan to leave the group but continue to use
// the client. If a rebalance is in progress, this function waits for the
// rebalance to complete before the group can be left. This is necessary to
// allow you to safely issue one final offset commit in OnPartitionsRevoked. If
// you have overridden the default revoke, you must manually commit offsets
// before leaving the group.
//
// The context can be used to avoid waiting for the client to leave the group.
// Not waiting may result in your client being stuck in the group and the
// partitions this client was consuming being stuck until the session timeout.
// This function returns any leave group error or context cancel error. If the
// context is nil, this immediately leaves the group and does not wait and does
// not return an error.
//
// If you have configured the group with an InstanceID, this does not leave the
// group. With instance IDs, it is expected that clients will restart and
// re-use the same instance ID. To leave a group using an instance ID, you must
// manually issue a kmsg.LeaveGroupRequest or use an external tool (kafka
// scripts or kcl).
func (cl *Client) LeaveGroupContext(ctx context.Context) error {
	c := &cl.consumer
	if c.g == nil {
		return nil
	}
	var immediate bool
	if ctx == nil {
		var cancel func()
		ctx, cancel = context.WithCancel(context.Background())
		cancel()
		immediate = true
	}

	go func() {
		c.waitAndAddRebalance()
		c.mu.Lock() // lock for assign
		c.assignPartitions(nil, assignInvalidateAll, nil, "invalidating all assignments in LeaveGroup")
		c.g.leave(ctx)
		c.mu.Unlock()
		c.unaddRebalance()
	}()

	select {
	case <-ctx.Done():
		if immediate {
			return nil
		}
		return ctx.Err()
	case <-c.g.left:
		return c.g.leaveErr
	}
}

// GroupMetadata returns the current group member ID and generation, or an
// empty string and -1 if not in the group.
func (cl *Client) GroupMetadata() (string, int32) {
	g := cl.consumer.g
	if g == nil {
		return "", -1
	}
	return g.memberGen.load()
}

func (c *consumer) initGroup() {
	ctx, cancel := context.WithCancel(c.cl.ctx)
	g := &groupConsumer{
		c:   c,
		cl:  c.cl,
		cfg: &c.cl.cfg,

		ctx:    ctx,
		cancel: cancel,

		reSeen: make(map[string]bool),

		manageDone:       make(chan struct{}),
		tps:              newTopicsPartitions(),
		rejoinCh:         make(chan string, 1),
		heartbeatForceCh: make(chan func(error)),
		using:            make(map[string]int),

		left: make(chan struct{}),
	}
	c.g = g
	if !g.cfg.setCommitCallback {
		g.cfg.commitCallback = g.defaultCommitCallback
	}

	if g.cfg.txnID == nil {
		// We only override revoked / lost if they were not explicitly
		// set by options.
		if !g.cfg.setRevoked {
			g.cfg.onRevoked = g.defaultRevoke
		}
		// For onLost, we do not want to commit in onLost, so we
		// explicitly set onLost to an empty function to avoid the
		// fallback to onRevoked.
		if !g.cfg.setLost {
			g.cfg.onLost = func(context.Context, *Client, map[string][]int32) {}
		}
	} else {
		g.cfg.autocommitDisable = true
	}

	for _, logOn := range []struct {
		name string
		set  *func(context.Context, *Client, map[string][]int32)
	}{
		{"OnPartitionsAssigned", &g.cfg.onAssigned},
		{"OnPartitionsRevoked", &g.cfg.onRevoked},
		{"OnPartitionsLost", &g.cfg.onLost},
	} {
		user := *logOn.set
		name := logOn.name
		*logOn.set = func(ctx context.Context, cl *Client, m map[string][]int32) {
			var ctxExpired bool
			select {
			case <-ctx.Done():
				ctxExpired = true
			default:
			}
			if ctxExpired {
				cl.cfg.logger.Log(LogLevelDebug, "entering "+name, "with", m, "context_expired", ctxExpired)
			} else {
				cl.cfg.logger.Log(LogLevelDebug, "entering "+name, "with", m)
			}
			if user != nil {
				dup := make(map[string][]int32)
				for k, vs := range m {
					dup[k] = append([]int32(nil), vs...)
				}
				user(ctx, cl, dup)
			}
		}
	}

	if g.cfg.onFetched != nil || g.cfg.adjustOffsetsBeforeAssign != nil {
		revoked := g.cfg.onRevoked
		g.cfg.onRevoked = func(ctx context.Context, cl *Client, m map[string][]int32) {
			g.onFetchedMu.Lock()
			defer g.onFetchedMu.Unlock()
			revoked(ctx, cl, m)
		}
	}

	// For non-regex topics, we explicitly ensure they exist for loading
	// metadata. This is of no impact if we are *also* consuming via regex,
	// but that is no problem.
	if len(g.cfg.topics) > 0 && !g.cfg.regex {
		topics := make([]string, 0, len(g.cfg.topics))
		for topic := range g.cfg.topics {
			topics = append(topics, topic)
		}
		g.tps.storeTopics(topics)
	}
}

// Manages the group consumer's join / sync / heartbeat / fetch offset flow.
//
// Once a group is assigned, we fire a metadata request for all topics the
// assignment specified interest in. Only after we finally have some topic
// metadata do we join the group, and once joined, this management runs in a
// dedicated goroutine until the group is left.
func (g *groupConsumer) manage() {
	defer close(g.manageDone)
	g.cfg.logger.Log(LogLevelInfo, "beginning to manage the group lifecycle", "group", g.cfg.group)
	if !g.cfg.autocommitDisable && g.cfg.autocommitInterval > 0 {
		g.cfg.logger.Log(LogLevelInfo, "beginning autocommit loop", "group", g.cfg.group)
		go g.loopCommit()
	}

	var consecutiveErrors int
	joinWhy := "beginning to manage the group lifecycle"
	for {
		if joinWhy == "" {
			joinWhy = "rejoining from normal rebalance"
		}
		err := g.joinAndSync(joinWhy)
		if err == nil {
			if joinWhy, err = g.setupAssignedAndHeartbeat(); err != nil {
				if errors.Is(err, kerr.RebalanceInProgress) {
					err = nil
				}
			}
		}
		if err == nil {
			consecutiveErrors = 0
			continue
		}
		joinWhy = "rejoining after we previously errored and backed off"

		// If the user has BlockPollOnRebalance enabled, we have to
		// block around the onLost and assigning.
		g.c.waitAndAddRebalance()

		if errors.Is(err, context.Canceled) && g.cfg.onRevoked != nil {
			// The cooperative consumer does not revoke everything
			// while rebalancing, meaning if our context is
			// canceled, we may have uncommitted data. Rather than
			// diving into onLost, we should go into onRevoked,
			// because for the most part, a context cancelation
			// means we are leaving the group. Going into onRevoked
			// gives us an opportunity to commit outstanding
			// offsets. For the eager consumer, since we always
			// revoke before exiting the heartbeat loop, we do not
			// really care so much about *needing* to call
			// onRevoked, but since we are handling this case for
			// the cooperative consumer we may as well just also
			// include the eager consumer.
			g.cfg.onRevoked(g.cl.ctx, g.cl, g.nowAssigned.read())
		} else {
			// Any other error is perceived as a fatal error,
			// and we go into onLost as appropriate.
			if g.cfg.onLost != nil {
				g.cfg.onLost(g.cl.ctx, g.cl, g.nowAssigned.read())
			}
			g.cfg.hooks.each(func(h Hook) {
				if h, ok := h.(HookGroupManageError); ok {
					h.OnGroupManageError(err)
				}
			})
			g.c.addFakeReadyForDraining("", 0, &ErrGroupSession{err}, "notification of group management loop error")
		}

		// If we are eager, we should have invalidated everything
		// before getting here, but we do so doubly just in case.
		//
		// If we are cooperative, the join and sync could have failed
		// during the cooperative rebalance where we were still
		// consuming. We need to invalidate everything. Waiting to
		// resume from poll is necessary, but the user will likely be
		// unable to commit.
		{
			g.c.mu.Lock()
			g.c.assignPartitions(nil, assignInvalidateAll, nil, "clearing assignment at end of group management session")
			g.mu.Lock()     // before allowing poll to touch uncommitted, lock the group
			g.c.mu.Unlock() // now part of poll can continue
			g.uncommitted = nil
			g.mu.Unlock()

			g.nowAssigned.store(nil)
			g.lastAssigned = nil
			g.fetching = nil

			g.leader.Store(false)
			g.resetExternal()
		}

		// Unblock bolling now that we have called onLost and
		// re-assigned.
		g.c.unaddRebalance()

		if errors.Is(err, context.Canceled) { // context was canceled, quit now
			return
		}

		// Waiting for the backoff is a good time to update our
		// metadata; maybe the error is from stale metadata.
		consecutiveErrors++
		backoff := g.cfg.retryBackoff(consecutiveErrors)
		g.cfg.logger.Log(LogLevelError, "join and sync loop errored",
			"group", g.cfg.group,
			"err", err,
			"consecutive_errors", consecutiveErrors,
			"backoff", backoff,
		)
		deadline := time.Now().Add(backoff)
		g.cl.waitmeta(g.ctx, backoff, "waitmeta during join & sync error backoff")
		after := time.NewTimer(time.Until(deadline))
		select {
		case <-g.ctx.Done():
			after.Stop()
			return
		case <-after.C:
		}
	}
}

func (g *groupConsumer) leave(ctx context.Context) {
	// If g.using is nonzero before this check, then a manage goroutine has
	// started. If not, it will never start because we set dying.
	g.mu.Lock()
	wasDead := g.dying
	g.dying = true
	wasManaging := g.managing
	g.cancel()
	g.mu.Unlock()

	go func() {
		if wasManaging {
			// We want to wait for the manage goroutine to be done
			// so that we call the user's on{Assign,RevokeLost}.
			<-g.manageDone
		}
		if wasDead {
			// If we already called leave(), then we just wait for
			// the prior leave to finish and we avoid re-issuing a
			// LeaveGroup request.
			return
		}

		defer close(g.left)

		if g.cfg.instanceID != nil {
			return
		}

		memberID := g.memberGen.memberID()
		g.cfg.logger.Log(LogLevelInfo, "leaving group",
			"group", g.cfg.group,
			"member_id", memberID,
		)
		// If we error when leaving, there is not much
		// we can do. We may as well just return.
		req := kmsg.NewPtrLeaveGroupRequest()
		req.Group = g.cfg.group
		req.MemberID = memberID
		member := kmsg.NewLeaveGroupRequestMember()
		member.MemberID = memberID
		member.Reason = kmsg.StringPtr("client leaving group per normal operation")
		req.Members = append(req.Members, member)

		resp, err := req.RequestWith(ctx, g.cl)
		if err != nil {
			g.leaveErr = err
			return
		}
		g.leaveErr = kerr.ErrorForCode(resp.ErrorCode)
	}()
}

// returns the difference of g.nowAssigned and g.lastAssigned.
func (g *groupConsumer) diffAssigned() (added, lost map[string][]int32) {
	nowAssigned := g.nowAssigned.clone()
	if !g.cooperative.Load() {
		return nowAssigned, nil
	}

	added = make(map[string][]int32, len(nowAssigned))
	lost = make(map[string][]int32, len(nowAssigned))

	// First, we diff lasts: any topic in last but not now is lost,
	// otherwise, (1) new partitions are added, (2) common partitions are
	// ignored, and (3) partitions no longer in now are lost.
	lasts := make(map[int32]struct{}, 100)
	for topic, lastPartitions := range g.lastAssigned {
		nowPartitions, exists := nowAssigned[topic]
		if !exists {
			lost[topic] = lastPartitions
			continue
		}

		for _, lastPartition := range lastPartitions {
			lasts[lastPartition] = struct{}{}
		}

		// Anything now that does not exist in last is new,
		// otherwise it is in common and we ignore it.
		for _, nowPartition := range nowPartitions {
			if _, exists := lasts[nowPartition]; !exists {
				added[topic] = append(added[topic], nowPartition)
			} else {
				delete(lasts, nowPartition)
			}
		}

		// Anything remanining in last does not exist now
		// and is thus lost.
		for last := range lasts {
			lost[topic] = append(lost[topic], last)
			delete(lasts, last) // reuse lasts
		}
	}

	// Finally, any new topics in now assigned are strictly added.
	for topic, nowPartitions := range nowAssigned {
		if _, exists := g.lastAssigned[topic]; !exists {
			added[topic] = nowPartitions
		}
	}

	return added, lost
}

type revokeStage int8

const (
	revokeLastSession = iota
	revokeThisSession
)

// revoke calls onRevoked for partitions that this group member is losing and
// updates the uncommitted map after the revoke.
//
// For eager consumers, this simply revokes g.assigned. This will only be
// called at the end of a group session.
//
// For cooperative consumers, this either
//
//	(1) if revoking lost partitions from a prior session (i.e., after sync),
//	    this revokes the passed in lost
//	(2) if revoking at the end of a session, this revokes topics that the
//	    consumer is no longer interested in consuming
//
// Lastly, for cooperative consumers, this must selectively delete what was
// lost from the uncommitted map.
func (g *groupConsumer) revoke(stage revokeStage, lost map[string][]int32, leaving bool) {
	g.c.waitAndAddRebalance()
	defer g.c.unaddRebalance()

	if !g.cooperative.Load() || leaving { // stage == revokeThisSession if not cooperative
		// If we are an eager consumer, we stop fetching all of our
		// current partitions as we will be revoking them.
		g.c.mu.Lock()
		if leaving {
			g.c.assignPartitions(nil, assignInvalidateAll, nil, "revoking all assignments because we are leaving the group")
		} else {
			g.c.assignPartitions(nil, assignInvalidateAll, nil, "revoking all assignments because we are not cooperative")
		}
		g.c.mu.Unlock()

		if !g.cooperative.Load() {
			g.cfg.logger.Log(LogLevelInfo, "eager consumer revoking prior assigned partitions", "group", g.cfg.group, "revoking", g.nowAssigned.read())
		} else {
			g.cfg.logger.Log(LogLevelInfo, "cooperative consumer revoking prior assigned partitions because leaving group", "group", g.cfg.group, "revoking", g.nowAssigned.read())
		}
		if g.cfg.onRevoked != nil {
			g.cfg.onRevoked(g.cl.ctx, g.cl, g.nowAssigned.read())
		}
		g.nowAssigned.store(nil)
		g.lastAssigned = nil

		// After nilling uncommitted here, nothing should recreate
		// uncommitted until a future fetch after the group is
		// rejoined. This _can_ be broken with a manual SetOffsets or
		// with CommitOffsets{,Sync} but we explicitly document not
		// to do that outside the context of a live group session.
		g.mu.Lock()
		g.uncommitted = nil
		g.mu.Unlock()
		return
	}

	switch stage {
	case revokeLastSession:
		// we use lost in this case

	case revokeThisSession:
		// lost is nil for cooperative assigning. Instead, we determine
		// lost by finding subscriptions we are no longer interested
		// in. This would be from a user's PurgeConsumeTopics call.
		//
		// We just paused metadata, but purging triggers a rebalance
		// which causes a new metadata request -- in short, this could
		// be concurrent with a metadata findNewAssignments, so we
		// lock.
		g.nowAssigned.write(func(nowAssigned map[string][]int32) {
			g.mu.Lock()
			for topic, partitions := range nowAssigned {
				if _, exists := g.using[topic]; !exists {
					if lost == nil {
						lost = make(map[string][]int32)
					}
					lost[topic] = partitions
					delete(nowAssigned, topic)
				}
			}
			g.mu.Unlock()
		})
	}

	if len(lost) > 0 {
		// We must now stop fetching anything we lost and invalidate
		// any buffered fetches before falling into onRevoked.
		//
		// We want to invalidate buffered fetches since they may
		// contain partitions that we lost, and we do not want a future
		// poll to return those fetches.
		lostOffsets := make(map[string]map[int32]Offset, len(lost))

		for lostTopic, lostPartitions := range lost {
			lostPartitionOffsets := make(map[int32]Offset, len(lostPartitions))
			for _, lostPartition := range lostPartitions {
				lostPartitionOffsets[lostPartition] = Offset{}
			}
			lostOffsets[lostTopic] = lostPartitionOffsets
		}

		// We must invalidate before revoking and before updating
		// uncommitted, because we want any commits in onRevoke to be
		// for the final polled offsets. We do not want to allow the
		// logical race of allowing fetches for revoked partitions
		// after a revoke but before an invalidation.
		g.c.mu.Lock()
		g.c.assignPartitions(lostOffsets, assignInvalidateMatching, g.tps, "revoking assignments from cooperative consuming")
		g.c.mu.Unlock()
	}

	if len(lost) > 0 || stage == revokeThisSession {
		if len(lost) == 0 {
			g.cfg.logger.Log(LogLevelInfo, "cooperative consumer calling onRevoke at the end of a session even though no partitions were lost", "group", g.cfg.group)
		} else {
			g.cfg.logger.Log(LogLevelInfo, "cooperative consumer calling onRevoke", "group", g.cfg.group, "lost", lost, "stage", stage)
		}
		if g.cfg.onRevoked != nil {
			g.cfg.onRevoked(g.cl.ctx, g.cl, lost)
		}
	}

	if len(lost) == 0 { // if we lost nothing, do nothing
		return
	}

	if stage != revokeThisSession { // cooperative consumers rejoin after they revoking what they lost
		defer g.rejoin("cooperative rejoin after revoking what we lost from a rebalance")
	}

	// The block below deletes everything lost from our uncommitted map.
	// All commits should be **completed** by the time this runs. An async
	// commit can undo what we do below. The default revoke runs a sync
	// commit.
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.uncommitted == nil {
		return
	}
	for lostTopic, lostPartitions := range lost {
		uncommittedPartitions := g.uncommitted[lostTopic]
		if uncommittedPartitions == nil {
			continue
		}
		for _, lostPartition := range lostPartitions {
			delete(uncommittedPartitions, lostPartition)
		}
		if len(uncommittedPartitions) == 0 {
			delete(g.uncommitted, lostTopic)
		}
	}
	if len(g.uncommitted) == 0 {
		g.uncommitted = nil
	}
}

// assignRevokeSession aids in sequencing prerevoke/assign/revoke.
type assignRevokeSession struct {
	prerevokeDone chan struct{}
	assignDone    chan struct{}
	revokeDone    chan struct{}
}

func newAssignRevokeSession() *assignRevokeSession {
	return &assignRevokeSession{
		prerevokeDone: make(chan struct{}),
		assignDone:    make(chan struct{}),
		revokeDone:    make(chan struct{}),
	}
}

// For cooperative consumers, the first thing a cooperative consumer does is to
// diff its last assignment and its new assignment and revoke anything lost.
// We call this a "prerevoke".
func (s *assignRevokeSession) prerevoke(g *groupConsumer, lost map[string][]int32) <-chan struct{} {
	go func() {
		defer close(s.prerevokeDone)
		if g.cooperative.Load() && len(lost) > 0 {
			g.revoke(revokeLastSession, lost, false)
		}
	}()
	return s.prerevokeDone
}

func (s *assignRevokeSession) assign(g *groupConsumer, newAssigned map[string][]int32) <-chan struct{} {
	go func() {
		defer close(s.assignDone)
		<-s.prerevokeDone
		if g.cfg.onAssigned != nil {
			// We always call on assigned, even if nothing new is
			// assigned. This allows consumers to know that
			// assignment is done and do setup logic.
			//
			// If configured, we have to block polling.
			g.c.waitAndAddRebalance()
			defer g.c.unaddRebalance()
			g.cfg.onAssigned(g.cl.ctx, g.cl, newAssigned)
		}
	}()
	return s.assignDone
}

// At the end of a group session, before we leave the heartbeat loop, we call
// revoke. For non-cooperative consumers, this revokes everything in the
// current session, and before revoking, we invalidate all partitions.  For the
// cooperative consumer, this does nothing but does notify the client that a
// revoke has begun / the group session is ending.
//
// This may not run before returning from the heartbeat loop: if we encounter a
// fatal error, we return before revoking so that we can instead call onLost in
// the manage loop.
func (s *assignRevokeSession) revoke(g *groupConsumer, leaving bool) <-chan struct{} {
	go func() {
		defer close(s.revokeDone)
		<-s.assignDone
		g.revoke(revokeThisSession, nil, leaving)
	}()
	return s.revokeDone
}

// This chunk of code "pre" revokes lost partitions for the cooperative
// consumer and then begins heartbeating while fetching offsets. This returns
// when heartbeating errors (or if fetch offsets errors).
//
// Before returning, this function ensures that
//   - onAssigned is complete
//   - which ensures that pre revoking is complete
//   - fetching is complete
//   - heartbeating is complete
func (g *groupConsumer) setupAssignedAndHeartbeat() (string, error) {
	type hbquit struct {
		rejoinWhy string
		err       error
	}
	hbErrCh := make(chan hbquit, 1)
	fetchErrCh := make(chan error, 1)

	s := newAssignRevokeSession()
	added, lost := g.diffAssigned()
	g.lastAssigned = g.nowAssigned.clone() // now that we are done with our last assignment, update it per the new assignment

	g.cfg.logger.Log(LogLevelInfo, "new group session begun", "group", g.cfg.group, "added", mtps(added), "lost", mtps(lost))
	s.prerevoke(g, lost) // for cooperative consumers

	// Since we have joined the group, we immediately begin heartbeating.
	// This will continue until the heartbeat errors, the group is killed,
	// or the fetch offsets below errors.
	ctx, cancel := context.WithCancel(g.ctx)
	go func() {
		defer cancel() // potentially kill offset fetching
		g.cfg.logger.Log(LogLevelInfo, "beginning heartbeat loop", "group", g.cfg.group)
		rejoinWhy, err := g.heartbeat(fetchErrCh, s)
		hbErrCh <- hbquit{rejoinWhy, err}
	}()

	// We immediately begin fetching offsets. We want to wait until the
	// fetch function returns, since it assumes within it that another
	// assign cannot happen (it assigns partitions itself). Returning
	// before the fetch completes would be not good.
	//
	// The difference between fetchDone and fetchErrCh is that fetchErrCh
	// can kill heartbeating, or signal it to continue, while fetchDone
	// is specifically used for this function's return.
	fetchDone := make(chan struct{})
	defer func() { <-fetchDone }()

	// Before we fetch offsets, we wait for the user's onAssign callback to
	// be done. This ensures a few things:
	//
	// * that we wait for for prerevoking to be done, which updates the
	// uncommitted field. Waiting for that ensures that a rejoin and poll
	// does not have weird concurrent interaction.
	//
	// * that our onLost will not be concurrent with onAssign
	//
	// * that the user can start up any per-partition processors necessary
	// before we begin consuming that partition.
	//
	// We especially need to wait here because heartbeating may not
	// necessarily run onRevoke before returning (because of a fatal
	// error).
	s.assign(g, added)

	// If cooperative consuming, we may have to resume fetches. See the
	// comment on adjustCooperativeFetchOffsets.
	//
	// We do this AFTER the user's callback. If we add more partitions
	// to `added` that are from a previously canceled fetch, we do NOT
	// want to pass those fetch-resumed partitions to the user callback
	// again. See #705.
	if g.cooperative.Load() {
		added = g.adjustCooperativeFetchOffsets(added, lost)
	}

	<-s.assignDone

	if len(added) > 0 {
		go func() {
			defer close(fetchDone)
			defer close(fetchErrCh)
			fetchErrCh <- g.fetchOffsets(ctx, added)
		}()
	} else {
		close(fetchDone)
		close(fetchErrCh)
	}

	// Finally, we simply return whatever the heartbeat error is. This will
	// be the fetch offset error if that function is what killed this.

	done := <-hbErrCh
	return done.rejoinWhy, done.err
}

// heartbeat issues heartbeat requests to Kafka for the duration of a group
// session.
//
// This function begins before fetching offsets to allow the consumer's
// onAssigned to be called before fetching. If the eventual offset fetch
// errors, we continue heartbeating until onRevoked finishes and our metadata
// is updated. If the error is not RebalanceInProgress, we return immediately.
//
// If the offset fetch is successful, then we basically sit in this function
// until a heartbeat errors or we, being the leader, decide to re-join.
func (g *groupConsumer) heartbeat(fetchErrCh <-chan error, s *assignRevokeSession) (string, error) {
	ticker := time.NewTicker(g.cfg.heartbeatInterval)
	defer ticker.Stop()

	// We issue one heartbeat quickly if we are cooperative because
	// cooperative consumers rejoin the group immediately, and we want to
	// detect that in 500ms rather than 3s.
	var cooperativeFastCheck <-chan time.Time
	if g.cooperative.Load() {
		cooperativeFastCheck = time.After(500 * time.Millisecond)
	}

	var metadone, revoked <-chan struct{}
	var heartbeat, didMetadone, didRevoke bool
	var rejoinWhy string
	var lastErr error

	ctxCh := g.ctx.Done()

	for {
		var err error
		var force func(error)
		heartbeat = false
		select {
		case <-cooperativeFastCheck:
			heartbeat = true
		case <-ticker.C:
			heartbeat = true
		case force = <-g.heartbeatForceCh:
			heartbeat = true
		case rejoinWhy = <-g.rejoinCh:
			// If a metadata update changes our subscription,
			// we just pretend we are rebalancing.
			g.cfg.logger.Log(LogLevelInfo, "forced rejoin quitting heartbeat loop", "why", rejoinWhy)
			err = kerr.RebalanceInProgress
		case err = <-fetchErrCh:
			fetchErrCh = nil
		case <-metadone:
			metadone = nil
			didMetadone = true
		case <-revoked:
			revoked = nil
			didRevoke = true
		case <-ctxCh:
			// Even if the group is left, we need to wait for our
			// revoke to finish before returning, otherwise the
			// manage goroutine will race with us setting
			// nowAssigned.
			ctxCh = nil
			err = context.Canceled
		}

		if heartbeat {
			g.cfg.logger.Log(LogLevelDebug, "heartbeating", "group", g.cfg.group)
			req := kmsg.NewPtrHeartbeatRequest()
			req.Group = g.cfg.group
			memberID, generation := g.memberGen.load()
			req.Generation = generation
			req.MemberID = memberID
			req.InstanceID = g.cfg.instanceID
			var resp *kmsg.HeartbeatResponse
			if resp, err = req.RequestWith(g.ctx, g.cl); err == nil {
				err = kerr.ErrorForCode(resp.ErrorCode)
			}
			g.cfg.logger.Log(LogLevelDebug, "heartbeat complete", "group", g.cfg.group, "err", err)
			if force != nil {
				force(err)
			}
		}

		// The first error either triggers a clean revoke and metadata
		// update or it returns immediately. If we triggered the
		// revoke, we wait for it to complete regardless of any future
		// error.
		if didMetadone && didRevoke {
			return rejoinWhy, lastErr
		}

		if err == nil {
			continue
		}

		if lastErr == nil {
			g.cfg.logger.Log(LogLevelInfo, "heartbeat errored", "group", g.cfg.group, "err", err)
		} else {
			g.cfg.logger.Log(LogLevelInfo, "heartbeat errored again while waiting for user revoke to finish", "group", g.cfg.group, "err", err)
		}

		// Since we errored, we must revoke.
		if !didRevoke && revoked == nil {
			// If our error is not from rebalancing, then we
			// encountered IllegalGeneration or UnknownMemberID or
			// our context closed all of which are unexpected and
			// unrecoverable.
			//
			// We return early rather than revoking and updating
			// metadata; the groupConsumer's manage function will
			// call onLost with all partitions.
			//
			// setupAssignedAndHeartbeat still waits for onAssigned
			// to be done so that we avoid calling onLost
			// concurrently.
			if !errors.Is(err, kerr.RebalanceInProgress) && revoked == nil {
				return "", err
			}

			// Now we call the user provided revoke callback, even
			// if cooperative: if cooperative, this only revokes
			// partitions we no longer want to consume.
			//
			// If the err is context.Canceled, the group is being
			// left and we revoke everything.
			revoked = s.revoke(g, errors.Is(err, context.Canceled))
		}
		// Since we errored, while waiting for the revoke to finish, we
		// update our metadata. A leader may have re-joined with new
		// metadata, and we want the update.
		if !didMetadone && metadone == nil {
			waited := make(chan struct{})
			metadone = waited
			go func() {
				g.cl.waitmeta(g.ctx, g.cfg.sessionTimeout, "waitmeta after heartbeat error")
				close(waited)
			}()
		}

		// We always save the latest error; generally this should be
		// REBALANCE_IN_PROGRESS, but if the revoke takes too long,
		// Kafka may boot us and we will get a different error.
		lastErr = err
	}
}

// ForceRebalance quits a group member's heartbeat loop so that the member
// rejoins with a JoinGroupRequest.
//
// This function is only useful if you either (a) know that the group member is
// a leader, and want to force a rebalance for any particular reason, or (b)
// are using a custom group balancer, and have changed the metadata that will
// be returned from its JoinGroupMetadata method. This function has no other
// use; see KIP-568 for more details around this function's motivation.
//
// If neither of the cases above are true (this member is not a leader, and the
// join group metadata has not changed), then Kafka will not actually trigger a
// rebalance and will instead reply to the member with its current assignment.
func (cl *Client) ForceRebalance() {
	if g := cl.consumer.g; g != nil {
		g.rejoin("rejoin from ForceRebalance")
	}
}

// rejoin is called after a cooperative member revokes what it lost at the
// beginning of a session, or if we are leader and detect new partitions to
// consume.
func (g *groupConsumer) rejoin(why string) {
	select {
	case g.rejoinCh <- why:
	default:
	}
}

// Joins and then syncs, issuing the two slow requests in goroutines to allow
// for group cancelation to return early.
func (g *groupConsumer) joinAndSync(joinWhy string) error {
	g.noCommitDuringJoinAndSync.Lock()
	g.cfg.logger.Log(LogLevelDebug, "blocking commits from join&sync")
	defer g.noCommitDuringJoinAndSync.Unlock()
	defer g.cfg.logger.Log(LogLevelDebug, "unblocking commits from join&sync")

	g.cfg.logger.Log(LogLevelInfo, "joining group", "group", g.cfg.group)
	g.leader.Store(false)
	g.getAndResetExternalRejoin()
	defer func() {
		// If we are not leader, we clear any tracking of external
		// topics from when we were previously leader, since tracking
		// these is just a waste.
		if !g.leader.Load() {
			g.resetExternal()
		}
	}()

start:
	select {
	case <-g.rejoinCh: // drain to avoid unnecessary rejoins
	default:
	}

	joinReq := kmsg.NewPtrJoinGroupRequest()
	joinReq.Group = g.cfg.group
	joinReq.SessionTimeoutMillis = int32(g.cfg.sessionTimeout.Milliseconds())
	joinReq.RebalanceTimeoutMillis = int32(g.cfg.rebalanceTimeout.Milliseconds())
	joinReq.ProtocolType = g.cfg.protocol
	joinReq.MemberID = g.memberGen.memberID()
	joinReq.InstanceID = g.cfg.instanceID
	joinReq.Protocols = g.joinGroupProtocols()
	if joinWhy != "" {
		joinReq.Reason = kmsg.StringPtr(joinWhy)
	}
	var (
		joinResp *kmsg.JoinGroupResponse
		err      error
		joined   = make(chan struct{})
	)

	// NOTE: For this function, we have to use the client context, not the
	// group context. We want to allow people to issue one final commit in
	// OnPartitionsRevoked before leaving a group, so we need to block
	// commits during join&sync. If we used the group context, we would be
	// cancled immediately when leaving while a join or sync is inflight,
	// and then our final commit will receive either REBALANCE_IN_PROGRESS
	// or ILLEGAL_GENERATION.

	go func() {
		defer close(joined)
		joinResp, err = joinReq.RequestWith(g.cl.ctx, g.cl)
	}()

	select {
	case <-joined:
	case <-g.cl.ctx.Done():
		return g.cl.ctx.Err() // client closed
	}
	if err != nil {
		return err
	}

	restart, protocol, plan, err := g.handleJoinResp(joinResp)
	if restart {
		goto start
	}
	if err != nil {
		g.cfg.logger.Log(LogLevelWarn, "join group failed", "group", g.cfg.group, "err", err)
		return err
	}

	syncReq := kmsg.NewPtrSyncGroupRequest()
	syncReq.Group = g.cfg.group
	memberID, generation := g.memberGen.load()
	syncReq.Generation = generation
	syncReq.MemberID = memberID
	syncReq.InstanceID = g.cfg.instanceID
	syncReq.ProtocolType = &g.cfg.protocol
	syncReq.Protocol = &protocol
	if !joinResp.SkipAssignment {
		syncReq.GroupAssignment = plan // nil unless we are the leader
	}
	var (
		syncResp *kmsg.SyncGroupResponse
		synced   = make(chan struct{})
	)

	g.cfg.logger.Log(LogLevelInfo, "syncing", "group", g.cfg.group, "protocol_type", g.cfg.protocol, "protocol", protocol)
	go func() {
		defer close(synced)
		syncResp, err = syncReq.RequestWith(g.cl.ctx, g.cl)
	}()

	select {
	case <-synced:
	case <-g.cl.ctx.Done():
		return g.cl.ctx.Err()
	}
	if err != nil {
		return err
	}

	if err = g.handleSyncResp(protocol, syncResp); err != nil {
		if errors.Is(err, kerr.RebalanceInProgress) {
			g.cfg.logger.Log(LogLevelInfo, "sync failed with RebalanceInProgress, rejoining", "group", g.cfg.group)
			goto start
		}
		g.cfg.logger.Log(LogLevelWarn, "sync group failed", "group", g.cfg.group, "err", err)
		return err
	}

	// KIP-814 fixes one limitation with KIP-345, but has another
	// fundamental limitation. When an instance ID leader restarts, its
	// first join always gets its old assignment *even if* the member's
	// topic interests have changed. The broker tells us to skip doing
	// assignment ourselves, but we ignore that for our well known
	// balancers. Instead, we balance (but avoid sending it while syncing,
	// as we are supposed to), and if our sync assignment differs from our
	// own calculated assignment, We know we have a stale broker assignment
	// and must trigger a rebalance.
	if plan != nil && joinResp.SkipAssignment {
		for _, assign := range plan {
			if assign.MemberID == memberID {
				if !bytes.Equal(assign.MemberAssignment, syncResp.MemberAssignment) {
					g.rejoin("instance group leader restarted and was reassigned old plan, our topic interests changed and we must rejoin to force a rebalance")
				}
				break
			}
		}
	}

	return nil
}

func (g *groupConsumer) handleJoinResp(resp *kmsg.JoinGroupResponse) (restart bool, protocol string, plan []kmsg.SyncGroupRequestGroupAssignment, err error) {
	if err = kerr.ErrorForCode(resp.ErrorCode); err != nil {
		switch err {
		case kerr.MemberIDRequired:
			g.memberGen.storeMember(resp.MemberID) // KIP-394
			g.cfg.logger.Log(LogLevelInfo, "join returned MemberIDRequired, rejoining with response's MemberID", "group", g.cfg.group, "member_id", resp.MemberID)
			return true, "", nil, nil
		case kerr.UnknownMemberID:
			g.memberGen.storeMember("")
			g.cfg.logger.Log(LogLevelInfo, "join returned UnknownMemberID, rejoining without a member id", "group", g.cfg.group)
			return true, "", nil, nil
		}
		return // Request retries as necessary, so this must be a failure
	}
	g.memberGen.store(resp.MemberID, resp.Generation)

	if resp.Protocol != nil {
		protocol = *resp.Protocol
	}

	for _, balancer := range g.cfg.balancers {
		if protocol == balancer.ProtocolName() {
			cooperative := balancer.IsCooperative()
			if !cooperative && g.cooperative.Load() {
				g.cfg.logger.Log(LogLevelWarn, "downgrading from cooperative group to eager group, this is not supported per KIP-429!")
			}
			g.cooperative.Store(cooperative)
			break
		}
	}

	// KIP-345 has a fundamental limitation that KIP-814 also does not
	// solve.
	//
	// When using instance IDs, if a leader restarts, its first join
	// receives its old assignment no matter what. KIP-345 resulted in
	// leaderless consumer groups, KIP-814 fixes this by notifying the
	// restarted leader that it is still leader but that it should not
	// balance.
	//
	// If the join response is <= v8, we hackily work around the leaderless
	// situation by checking if the LeaderID is prefixed with our
	// InstanceID. This is how Kafka and Redpanda are both implemented.  At
	// worst, if we mis-predict the leader, then we may accidentally try to
	// cause a rebalance later and it will do nothing. That's fine. At
	// least we can cause rebalances now, rather than having a leaderless,
	// not-ever-rebalancing client.
	//
	// KIP-814 does not solve our problem fully: if we restart and rejoin,
	// we always get our old assignment even if we changed what topics we
	// were interested in. Because we have our old assignment, we think
	// that the plan is fine *even with* our new interests, and we wait for
	// some external rebalance trigger. We work around this limitation
	// above (see "KIP-814") only for well known balancers; we cannot work
	// around this limitation for not well known balancers because they may
	// do so weird things we cannot control nor reason about.
	leader := resp.LeaderID == resp.MemberID
	leaderNoPlan := !leader && resp.Version <= 8 && g.cfg.instanceID != nil && strings.HasPrefix(resp.LeaderID, *g.cfg.instanceID+"-")
	if leader {
		g.leader.Store(true)
		g.cfg.logger.Log(LogLevelInfo, "joined, balancing group",
			"group", g.cfg.group,
			"member_id", resp.MemberID,
			"instance_id", strptr{g.cfg.instanceID},
			"generation", resp.Generation,
			"balance_protocol", protocol,
			"leader", true,
		)
		plan, err = g.balanceGroup(protocol, resp.Members, resp.SkipAssignment)
	} else if leaderNoPlan {
		g.leader.Store(true)
		g.cfg.logger.Log(LogLevelInfo, "joined as leader but unable to balance group due to KIP-345 limitations",
			"group", g.cfg.group,
			"member_id", resp.MemberID,
			"instance_id", strptr{g.cfg.instanceID},
			"generation", resp.Generation,
			"balance_protocol", protocol,
			"leader", true,
		)
	} else {
		g.cfg.logger.Log(LogLevelInfo, "joined",
			"group", g.cfg.group,
			"member_id", resp.MemberID,
			"instance_id", strptr{g.cfg.instanceID},
			"generation", resp.Generation,
			"leader", false,
		)
	}
	return
}

type strptr struct {
	s *string
}

func (s strptr) String() string {
	if s.s == nil {
		return "<nil>"
	}
	return *s.s
}

// If other group members consume topics we are not interested in, we track the
// entire group's topics in this groupExternal type. On metadata update, we see
// if any partitions for any of these topics have changed, and if so, we as
// leader rejoin the group.
//
// Our external topics are cleared whenever we join and are not leader. We keep
// our previous external topics if we are leader: on the first balance as
// leader, we request metadata for all topics, then on followup balances, we
// already have that metadata and do not need to reload it when balancing.
//
// Whenever metadata updates, we detect if a rejoin is needed and always reset
// the rejoin status.
type groupExternal struct {
	tps    atomic.Value // map[string]int32
	rejoin atomicBool
}

func (g *groupConsumer) loadExternal() *groupExternal {
	e := g.external.Load()
	if e != nil {
		return e.(*groupExternal)
	}
	return nil
}

// We reset our external topics whenever join&sync loop errors, or when we join
// and are not leader.
func (g *groupConsumer) resetExternal() {
	g.external.Store((*groupExternal)(nil))
}

// If this is our first join as leader, or if a new member joined with new
// topics we were not tracking, we re-initialize external with the all-topics
// metadata refresh.
func (g *groupConsumer) initExternal(current map[string]int32) {
	var e groupExternal
	e.tps.Store(dupmsi32(current))
	g.external.Store(&e)
}

// Reset whenever we join, & potentially used to rejoin when finding new
// assignments (i.e., end of metadata).
func (g *groupConsumer) getAndResetExternalRejoin() bool {
	e := g.loadExternal()
	if e == nil {
		return false
	}
	defer e.rejoin.Store(false)
	return e.rejoin.Load()
}

// Runs fn over a load, not copy, of our map.
func (g *groupExternal) fn(fn func(map[string]int32)) {
	if g == nil {
		return
	}
	v := g.tps.Load()
	if v == nil {
		return
	}
	tps := v.(map[string]int32)
	fn(tps)
}

// Runs fn over a clone of our external map and updates the map.
func (g *groupExternal) cloned(fn func(map[string]int32)) {
	g.fn(func(tps map[string]int32) {
		dup := dupmsi32(tps)
		fn(dup)
		g.tps.Store(dup)
	})
}

func (g *groupExternal) eachTopic(fn func(string)) {
	g.fn(func(tps map[string]int32) {
		for t := range tps {
			fn(t)
		}
	})
}

func (g *groupExternal) updateLatest(meta map[string]*metadataTopic) {
	g.cloned(func(tps map[string]int32) {
		var rejoin bool
		for t, ps := range tps {
			latest, exists := meta[t]
			if !exists || latest.loadErr != nil {
				continue
			}
			if psLatest := int32(len(latest.partitions)); psLatest != ps {
				rejoin = true
				tps[t] = psLatest
			}
		}
		if rejoin {
			g.rejoin.Store(true)
		}
	})
}

func (g *groupConsumer) handleSyncResp(protocol string, resp *kmsg.SyncGroupResponse) error {
	if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
		return err
	}

	b, err := g.findBalancer("sync assignment", protocol)
	if err != nil {
		return err
	}

	assigned, err := b.ParseSyncAssignment(resp.MemberAssignment)
	if err != nil {
		g.cfg.logger.Log(LogLevelError, "sync assignment parse failed", "group", g.cfg.group, "err", err)
		return err
	}

	g.cfg.logger.Log(LogLevelInfo, "synced", "group", g.cfg.group, "assigned", mtps(assigned))

	// Past this point, we will fall into the setupAssigned prerevoke code,
	// meaning for cooperative, we will revoke what we need to.
	g.nowAssigned.store(assigned)
	return nil
}

func (g *groupConsumer) joinGroupProtocols() []kmsg.JoinGroupRequestProtocol {
	g.mu.Lock()

	topics := make([]string, 0, len(g.using))
	for topic := range g.using {
		topics = append(topics, topic)
	}
	lastDup := make(map[string][]int32, len(g.lastAssigned))
	for t, ps := range g.lastAssigned {
		lastDup[t] = append([]int32(nil), ps...) // deep copy to allow modifications
	}

	g.mu.Unlock()

	sort.Strings(topics) // we guarantee to JoinGroupMetadata that the input strings are sorted
	for _, partitions := range lastDup {
		sort.Slice(partitions, func(i, j int) bool { return partitions[i] < partitions[j] }) // same for partitions
	}

	gen := g.memberGen.generation()
	var protos []kmsg.JoinGroupRequestProtocol
	for _, balancer := range g.cfg.balancers {
		proto := kmsg.NewJoinGroupRequestProtocol()
		proto.Name = balancer.ProtocolName()
		proto.Metadata = balancer.JoinGroupMetadata(topics, lastDup, gen)
		protos = append(protos, proto)
	}
	return protos
}

// If we are cooperatively consuming, we have a potential problem: if fetch
// offsets is canceled due to an immediate rebalance, when we resume, we will
// not re-fetch offsets for partitions we were previously assigned and are
// still assigned. We will only fetch offsets for new assignments.
//
// To work around that issue, we track everything we are fetching in g.fetching
// and only clear g.fetching if fetchOffsets returns with no error.
//
// Now, if fetching returns early due to an error, when we rejoin and re-fetch,
// we will resume fetching what we were previously:
//
//   - first we remove what was lost
//   - then we add anything new
//   - then we translate our total set into the "added" list to be fetched on return
//
// Any time a group is completely lost, the manage loop clears fetching. When
// cooperative consuming, a hard error is basically losing the entire state and
// rejoining from scratch.
func (g *groupConsumer) adjustCooperativeFetchOffsets(added, lost map[string][]int32) map[string][]int32 {
	if g.fetching != nil {
		// We were fetching previously: remove anything lost.
		for topic, partitions := range lost {
			ft := g.fetching[topic]
			if ft == nil {
				continue // we were not fetching this topic
			}
			for _, partition := range partitions {
				delete(ft, partition)
			}
			if len(ft) == 0 {
				delete(g.fetching, topic)
			}
		}
	} else {
		// We were not fetching previously: start a new map for what we
		// are adding.
		g.fetching = make(map[string]map[int32]struct{})
	}

	// Merge everything we are newly fetching to our fetching map.
	for topic, partitions := range added {
		ft := g.fetching[topic]
		if ft == nil {
			ft = make(map[int32]struct{}, len(partitions))
			g.fetching[topic] = ft
		}
		for _, partition := range partitions {
			ft[partition] = struct{}{}
		}
	}

	// Now translate our full set (previously fetching ++ newly fetching --
	// lost) into a new "added" map to be fetched.
	added = make(map[string][]int32, len(g.fetching))
	for topic, partitions := range g.fetching {
		ps := make([]int32, 0, len(partitions))
		for partition := range partitions {
			ps = append(ps, partition)
		}
		added[topic] = ps
	}
	return added
}

// fetchOffsets is issued once we join a group to see what the prior commits
// were for the partitions we were assigned.
func (g *groupConsumer) fetchOffsets(ctx context.Context, added map[string][]int32) (rerr error) { // we must use "rerr"! see introducing commit
	// If we fetch successfully, we can clear the cross-group-cycle
	// fetching tracking.
	defer func() {
		if rerr == nil {
			g.fetching = nil
		}
	}()

	// Our client maps the v0 to v7 format to v8+ when sharding this
	// request, if we are only requesting one group, as well as maps the
	// response back, so we do not need to worry about v8+ here.
start:
	req := kmsg.NewPtrOffsetFetchRequest()
	req.Group = g.cfg.group
	req.RequireStable = g.cfg.requireStable
	for topic, partitions := range added {
		reqTopic := kmsg.NewOffsetFetchRequestTopic()
		reqTopic.Topic = topic
		reqTopic.Partitions = partitions
		req.Topics = append(req.Topics, reqTopic)
	}

	var resp *kmsg.OffsetFetchResponse
	var err error

	fetchDone := make(chan struct{})
	go func() {
		defer close(fetchDone)
		resp, err = req.RequestWith(ctx, g.cl)
	}()
	select {
	case <-fetchDone:
	case <-ctx.Done():
		g.cfg.logger.Log(LogLevelInfo, "fetch offsets failed due to context cancelation", "group", g.cfg.group)
		return ctx.Err()
	}
	if err != nil {
		g.cfg.logger.Log(LogLevelError, "fetch offsets failed with non-retryable error", "group", g.cfg.group, "err", err)
		return err
	}

	// Even if a leader epoch is returned, if brokers do not support
	// OffsetForLeaderEpoch for some reason (odd set of supported reqs), we
	// cannot use the returned leader epoch.
	kip320 := g.cl.supportsOffsetForLeaderEpoch()

	offsets := make(map[string]map[int32]Offset)
	for _, rTopic := range resp.Topics {
		topicOffsets := make(map[int32]Offset)
		offsets[rTopic.Topic] = topicOffsets
		for _, rPartition := range rTopic.Partitions {
			if err = kerr.ErrorForCode(rPartition.ErrorCode); err != nil {
				// KIP-447: Unstable offset commit means there is a
				// pending transaction that should be committing soon.
				// We sleep for 1s and retry fetching offsets.
				if errors.Is(err, kerr.UnstableOffsetCommit) {
					g.cfg.logger.Log(LogLevelInfo, "fetch offsets failed with UnstableOffsetCommit, waiting 1s and retrying",
						"group", g.cfg.group,
						"topic", rTopic.Topic,
						"partition", rPartition.Partition,
					)
					select {
					case <-ctx.Done():
					case <-time.After(time.Second):
						goto start
					}
				}
				g.cfg.logger.Log(LogLevelError, "fetch offsets failed",
					"group", g.cfg.group,
					"topic", rTopic.Topic,
					"partition", rPartition.Partition,
					"err", err,
				)
				return err
			}
			offset := Offset{
				at:    rPartition.Offset,
				epoch: -1,
			}
			if resp.Version >= 5 && kip320 { // KIP-320
				offset.epoch = rPartition.LeaderEpoch
			}
			if rPartition.Offset == -1 {
				offset = g.cfg.resetOffset
			}
			topicOffsets[rPartition.Partition] = offset
		}
	}

	groupTopics := g.tps.load()
	for fetchedTopic := range offsets {
		if !groupTopics.hasTopic(fetchedTopic) {
			delete(offsets, fetchedTopic)
			g.cfg.logger.Log(LogLevelWarn, "member was assigned topic that we did not ask for in ConsumeTopics! skipping assigning this topic!", "group", g.cfg.group, "topic", fetchedTopic)
		}
	}

	if g.cfg.onFetched != nil {
		g.onFetchedMu.Lock()
		err = g.cfg.onFetched(ctx, g.cl, resp)
		g.onFetchedMu.Unlock()
		if err != nil {
			return err
		}
	}
	if g.cfg.adjustOffsetsBeforeAssign != nil {
		g.onFetchedMu.Lock()
		offsets, err = g.cfg.adjustOffsetsBeforeAssign(ctx, offsets)
		g.onFetchedMu.Unlock()
		if err != nil {
			return err
		}
	}

	// Lock for assign and then updating uncommitted.
	g.c.mu.Lock()
	defer g.c.mu.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()

	// Eager: we already invalidated everything; nothing to re-invalidate.
	// Cooperative: assign without invalidating what we are consuming.
	g.c.assignPartitions(offsets, assignWithoutInvalidating, g.tps, fmt.Sprintf("newly fetched offsets for group %s", g.cfg.group))

	// We need to update the uncommitted map so that SetOffsets(Committed)
	// does not rewind before the committed offsets we just fetched.
	if g.uncommitted == nil {
		g.uncommitted = make(uncommitted, 10)
	}
	for topic, partitions := range offsets {
		topicUncommitted := g.uncommitted[topic]
		if topicUncommitted == nil {
			topicUncommitted = make(map[int32]uncommit, 20)
			g.uncommitted[topic] = topicUncommitted
		}
		for partition, offset := range partitions {
			if offset.at < 0 {
				continue // not yet committed
			}
			committed := EpochOffset{
				Epoch:  offset.epoch,
				Offset: offset.at,
			}
			topicUncommitted[partition] = uncommit{
				dirty:     committed,
				head:      committed,
				committed: committed,
			}
		}
	}
	return nil
}

// findNewAssignments updates topics the group wants to use and other metadata.
// We only grab the group mu at the end if we need to.
//
// This joins the group if
//   - the group has never been joined
//   - new topics are found for consuming (changing this consumer's join metadata)
//
// Additionally, if the member is the leader, this rejoins the group if the
// leader notices new partitions in an existing topic.
//
// This does not rejoin if the leader notices a partition is lost, which is
// finicky.
func (g *groupConsumer) findNewAssignments() {
	topics := g.tps.load()

	type change struct {
		isNew bool
		delta int
	}

	var numNewTopics int
	toChange := make(map[string]change, len(topics))
	for topic, topicPartitions := range topics {
		parts := topicPartitions.load()
		numPartitions := len(parts.partitions)
		// If we are already using this topic, add that it changed if
		// there are more partitions than we were using prior.
		if used, exists := g.using[topic]; exists {
			if added := numPartitions - used; added > 0 {
				toChange[topic] = change{delta: added}
			}
			continue
		}

		// We are iterating over g.tps, which is initialized in the
		// group.init from the config's topics, but can also be added
		// to in AddConsumeTopics. By default, we use the topic. If
		// this is regex based, the config's topics are regular
		// expressions that we need to evaluate against (and we do not
		// support adding new regex).
		useTopic := true
		if g.cfg.regex {
			useTopic = g.reSeen[topic]
		}

		// We only track using the topic if there are partitions for
		// it; if there are none, then the topic was set by _us_ as "we
		// want to load the metadata", but the topic was not returned
		// in the metadata (or it was returned with an error).
		if useTopic && numPartitions > 0 {
			if g.cfg.regex && parts.isInternal {
				continue
			}
			toChange[topic] = change{isNew: true, delta: numPartitions}
			numNewTopics++
		}
	}

	externalRejoin := g.leader.Load() && g.getAndResetExternalRejoin()

	if len(toChange) == 0 && !externalRejoin {
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.dying {
		return
	}

	for topic, change := range toChange {
		g.using[topic] += change.delta
	}

	if !g.managing {
		g.managing = true
		go g.manage()
		return
	}

	if numNewTopics > 0 {
		g.rejoin("rejoining because there are more topics to consume, our interests have changed")
	} else if g.leader.Load() {
		if len(toChange) > 0 {
			g.rejoin("rejoining because we are the leader and noticed some topics have new partitions")
		} else if externalRejoin {
			g.rejoin("leader detected that partitions on topics another member is consuming have changed, rejoining to trigger rebalance")
		}
	}
}

// uncommit tracks the latest offset polled (+1) and the latest commit.
// The reason head is just past the latest offset is because we want
// to commit TO an offset, not BEFORE an offset.
type uncommit struct {
	dirty     EpochOffset // if autocommitting, what will move to head on next Poll
	head      EpochOffset // ready to commit
	committed EpochOffset // what is committed
}

// EpochOffset combines a record offset with the leader epoch the broker
// was at when the record was written.
type EpochOffset struct {
	// Epoch is the leader epoch of the record being committed. Truncation
	// detection relies on the epoch of the CURRENT record. For truncation
	// detection, the client asks "what is the the end of this epoch?",
	// which returns one after the end offset (see the next field, and
	// check the docs on kmsg.OffsetForLeaderEpochRequest).
	Epoch int32

	// Offset is the offset of a record. If committing, this should be one
	// AFTER a record's offset. Clients start consuming at the offset that
	// is committed.
	Offset int64
}

// Less returns whether the this EpochOffset is less than another. This is less
// than the other if this one's epoch is less, or the epoch's are equal and
// this one's offset is less.
func (e EpochOffset) Less(o EpochOffset) bool {
	return e.Epoch < o.Epoch || e.Epoch == o.Epoch && e.Offset < o.Offset
}

type uncommitted map[string]map[int32]uncommit

// updateUncommitted sets the latest uncommitted offset.
func (g *groupConsumer) updateUncommitted(fetches Fetches) {
	var b bytes.Buffer
	debug := g.cfg.logger.Level() >= LogLevelDebug

	// We set the head offset if autocommitting is disabled (because we
	// only use head / committed in that case), or if we are greedily
	// autocommitting (so that the latest head is available to autocommit).
	setHead := g.cfg.autocommitDisable || g.cfg.autocommitGreedy

	g.mu.Lock()
	defer g.mu.Unlock()

	for _, fetch := range fetches {
		for _, topic := range fetch.Topics {
			if debug {
				fmt.Fprintf(&b, "%s[", topic.Topic)
			}
			var topicOffsets map[int32]uncommit
			for _, partition := range topic.Partitions {
				if len(partition.Records) == 0 {
					continue
				}
				final := partition.Records[len(partition.Records)-1]

				if topicOffsets == nil {
					if g.uncommitted == nil {
						g.uncommitted = make(uncommitted, 10)
					}
					topicOffsets = g.uncommitted[topic.Topic]
					if topicOffsets == nil {
						topicOffsets = make(map[int32]uncommit, 20)
						g.uncommitted[topic.Topic] = topicOffsets
					}
				}

				// Our new head points just past the final consumed offset,
				// that is, if we rejoin, this is the offset to begin at.
				set := EpochOffset{
					final.LeaderEpoch, // -1 if old message / unknown
					final.Offset + 1,
				}
				prior := topicOffsets[partition.Partition]

				if debug {
					if setHead {
						fmt.Fprintf(&b, "%d{%d=>%d r%d}, ", partition.Partition, prior.head.Offset, set.Offset, len(partition.Records))
					} else {
						fmt.Fprintf(&b, "%d{%d=>%d=>%d r%d}, ", partition.Partition, prior.head.Offset, prior.dirty.Offset, set.Offset, len(partition.Records))
					}
				}

				prior.dirty = set
				if setHead {
					prior.head = set
				}
				topicOffsets[partition.Partition] = prior
			}

			if debug {
				if bytes.HasSuffix(b.Bytes(), []byte(", ")) {
					b.Truncate(b.Len() - 2)
				}
				b.WriteString("], ")
			}
		}
	}

	if debug {
		update := b.String()
		update = strings.TrimSuffix(update, ", ") // trim trailing comma and space after final topic
		g.cfg.logger.Log(LogLevelDebug, "updated uncommitted", "group", g.cfg.group, "to", update)
	}
}

// Called at the start of PollXyz only if autocommitting is enabled and we are
// not committing greedily, this ensures that when we enter poll, everything
// previously consumed is a candidate for autocommitting.
func (g *groupConsumer) undirtyUncommitted() {
	if g == nil {
		return
	}
	// Disabling autocommit means we do not use the dirty offset: we always
	// update head, and then manual commits use that.
	if g.cfg.autocommitDisable {
		return
	}
	// Greedy autocommitting does not use dirty offsets, because we always
	// just set head to the latest.
	if g.cfg.autocommitGreedy {
		return
	}
	// If we are autocommitting marked records only, then we do not
	// automatically un-dirty our offsets.
	if g.cfg.autocommitMarks {
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	for _, partitions := range g.uncommitted {
		for partition, uncommit := range partitions {
			if uncommit.dirty != uncommit.head {
				uncommit.head = uncommit.dirty
				partitions[partition] = uncommit
			}
		}
	}
}

// updateCommitted updates the group's uncommitted map. This function triply
// verifies that the resp matches the req as it should and that the req does
// not somehow contain more than what is in our uncommitted map.
func (g *groupConsumer) updateCommitted(
	req *kmsg.OffsetCommitRequest,
	resp *kmsg.OffsetCommitResponse,
) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if req.Generation != g.memberGen.generation() {
		return
	}
	if g.uncommitted == nil {
		g.cfg.logger.Log(LogLevelWarn, "received an OffsetCommitResponse after our group session has ended, unable to handle this (were we kicked from the group?)")
		return
	}
	if len(req.Topics) != len(resp.Topics) { // bad kafka
		g.cfg.logger.Log(LogLevelError, fmt.Sprintf("broker replied to our OffsetCommitRequest incorrectly! Num topics in request: %d, in reply: %d, we cannot handle this!", len(req.Topics), len(resp.Topics)), "group", g.cfg.group)
		return
	}

	sort.Slice(req.Topics, func(i, j int) bool {
		return req.Topics[i].Topic < req.Topics[j].Topic
	})
	sort.Slice(resp.Topics, func(i, j int) bool {
		return resp.Topics[i].Topic < resp.Topics[j].Topic
	})

	var b bytes.Buffer
	debug := g.cfg.logger.Level() >= LogLevelDebug

	for i := range resp.Topics {
		reqTopic := &req.Topics[i]
		respTopic := &resp.Topics[i]
		topic := g.uncommitted[respTopic.Topic]
		if topic == nil || // just in case
			reqTopic.Topic != respTopic.Topic || // bad kafka
			len(reqTopic.Partitions) != len(respTopic.Partitions) { // same
			g.cfg.logger.Log(LogLevelError, fmt.Sprintf("broker replied to our OffsetCommitRequest incorrectly! Topic at request index %d: %s, reply at index: %s; num partitions on request topic: %d, in reply: %d, we cannot handle this!", i, reqTopic.Topic, respTopic.Topic, len(reqTopic.Partitions), len(respTopic.Partitions)), "group", g.cfg.group)
			continue
		}

		sort.Slice(reqTopic.Partitions, func(i, j int) bool {
			return reqTopic.Partitions[i].Partition < reqTopic.Partitions[j].Partition
		})
		sort.Slice(respTopic.Partitions, func(i, j int) bool {
			return respTopic.Partitions[i].Partition < respTopic.Partitions[j].Partition
		})

		if debug {
			fmt.Fprintf(&b, "%s[", respTopic.Topic)
		}
		for i := range respTopic.Partitions {
			reqPart := &reqTopic.Partitions[i]
			respPart := &respTopic.Partitions[i]
			uncommit, exists := topic[respPart.Partition]
			if !exists { // just in case
				continue
			}
			if reqPart.Partition != respPart.Partition { // bad kafka
				g.cfg.logger.Log(LogLevelError, fmt.Sprintf("broker replied to our OffsetCommitRequest incorrectly! Topic %s partition %d != resp partition %d", reqTopic.Topic, reqPart.Partition, respPart.Partition), "group", g.cfg.group)
				continue
			}
			if respPart.ErrorCode != 0 {
				g.cfg.logger.Log(LogLevelWarn, "unable to commit offset for topic partition",
					"group", g.cfg.group,
					"topic", reqTopic.Topic,
					"partition", reqPart.Partition,
					"commit_from", uncommit.committed.Offset,
					"commit_to", reqPart.Offset,
					"commit_epoch", reqPart.LeaderEpoch,
					"error_code", respPart.ErrorCode,
				)
				continue
			}

			if debug {
				fmt.Fprintf(&b, "%d{%d=>%d}, ", reqPart.Partition, uncommit.committed.Offset, reqPart.Offset)
			}

			set := EpochOffset{
				reqPart.LeaderEpoch,
				reqPart.Offset,
			}
			uncommit.committed = set

			// head is set in four places:
			//  (1) if manually committing or greedily autocommitting,
			//      then head is bumped on poll
			//  (2) if autocommitting normally, then head is bumped
			//      to the prior poll on poll
			//  (3) if using marks, head is bumped on mark
			//  (4) here, and we can be here on autocommit or on
			//      manual commit (usually manual in an onRevoke)
			//
			// head is usually at or past the commit: usually, head
			// is used to build the commit itself. However, in case 4
			// when the user manually commits in onRevoke, the user
			// is likely committing with UncommittedOffsets, i.e.,
			// the dirty offsets that are past the current head.
			// We want to ensure we forward the head so that using
			// it later does not rewind the manual commit.
			//
			// This does not affect the first case, because dirty == head,
			// and manually committing dirty changes nothing.
			//
			// This does not affect the second case, because effectively,
			// this is just bumping head early (dirty == head, no change).
			//
			// This *could* affect the third case, because an
			// autocommit could begin, followed by a mark rewind,
			// followed by autocommit completion. We document that
			// using marks to rewind is not recommended.
			//
			// The user could also muck the offsets with SetOffsets.
			// We document that concurrent committing is not encouraged,
			// we do not attempt to guard past that.
			//
			// w.r.t. leader epoch's, we document that modifying
			// leader epoch's is not recommended.
			if uncommit.head.Less(set) {
				uncommit.head = set
			}

			topic[respPart.Partition] = uncommit
		}

		if debug {
			if bytes.HasSuffix(b.Bytes(), []byte(", ")) {
				b.Truncate(b.Len() - 2)
			}
			b.WriteString("], ")
		}
	}

	if debug {
		update := b.String()
		update = strings.TrimSuffix(update, ", ") // trim trailing comma and space after final topic
		g.cfg.logger.Log(LogLevelDebug, "updated committed", "group", g.cfg.group, "to", update)
	}
}

func (g *groupConsumer) defaultCommitCallback(_ *Client, _ *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			g.cfg.logger.Log(LogLevelError, "default commit failed", "group", g.cfg.group, "err", err)
		} else {
			g.cfg.logger.Log(LogLevelDebug, "default commit canceled", "group", g.cfg.group)
		}
		return
	}
	for _, topic := range resp.Topics {
		for _, partition := range topic.Partitions {
			if err := kerr.ErrorForCode(partition.ErrorCode); err != nil {
				g.cfg.logger.Log(LogLevelError, "in default commit: unable to commit offsets for topic partition",
					"group", g.cfg.group,
					"topic", topic.Topic,
					"partition", partition.Partition,
					"error", err)
			}
		}
	}
}

func (g *groupConsumer) loopCommit() {
	ticker := time.NewTicker(g.cfg.autocommitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
		case <-g.ctx.Done():
			return
		}

		// We use the group context for the default autocommit; revokes
		// use the client context so that we can be sure we commit even
		// after the group context is canceled (which is the first
		// thing that happens so as to quit the manage loop before
		// leaving a group).
		//
		// We always commit only the head. If we are autocommitting
		// dirty, then updateUncommitted updates the head to dirty
		// offsets.
		g.noCommitDuringJoinAndSync.RLock()
		g.mu.Lock()
		if !g.blockAuto {
			uncommitted := g.getUncommittedLocked(true, false)
			if len(uncommitted) == 0 {
				g.cfg.logger.Log(LogLevelDebug, "skipping autocommit due to no offsets to commit", "group", g.cfg.group)
				g.noCommitDuringJoinAndSync.RUnlock()
			} else {
				g.cfg.logger.Log(LogLevelDebug, "autocommitting", "group", g.cfg.group)
				g.commit(g.ctx, uncommitted, func(cl *Client, req *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
					g.noCommitDuringJoinAndSync.RUnlock()
					g.cfg.commitCallback(cl, req, resp, err)
				})
			}
		} else {
			g.noCommitDuringJoinAndSync.RUnlock()
		}
		g.mu.Unlock()
	}
}

// For SetOffsets, the gist of what follows:
//
// We need to set uncommitted.committed; that is the guarantee of this
// function. However, if, for everything we are setting, the head equals the
// commit, then we do not need to actually invalidate our current assignments.
// This is a great optimization for transactions that are resetting their state
// on abort.
func (g *groupConsumer) getSetAssigns(setOffsets map[string]map[int32]EpochOffset) (assigns map[string]map[int32]Offset) {
	g.mu.Lock()
	defer g.mu.Unlock()

	groupTopics := g.tps.load()

	if g.uncommitted == nil {
		g.uncommitted = make(uncommitted)
	}
	for topic, partitions := range setOffsets {
		if !groupTopics.hasTopic(topic) {
			continue // trying to set a topic that was not assigned...
		}
		topicUncommitted := g.uncommitted[topic]
		if topicUncommitted == nil {
			topicUncommitted = make(map[int32]uncommit)
			g.uncommitted[topic] = topicUncommitted
		}
		var topicAssigns map[int32]Offset
		for partition, epochOffset := range partitions {
			current, exists := topicUncommitted[partition]
			topicUncommitted[partition] = uncommit{
				dirty:     epochOffset,
				head:      epochOffset,
				committed: epochOffset,
			}
			if exists && current.dirty == epochOffset {
				continue
			} else if topicAssigns == nil {
				topicAssigns = make(map[int32]Offset, len(partitions))
			}
			topicAssigns[partition] = Offset{
				at:    epochOffset.Offset,
				epoch: epochOffset.Epoch,
			}
		}
		if len(topicAssigns) > 0 {
			if assigns == nil {
				assigns = make(map[string]map[int32]Offset, 10)
			}
			assigns[topic] = topicAssigns
		}
	}

	return assigns
}

// UncommittedOffsets returns the latest uncommitted offsets. Uncommitted
// offsets are always updated on calls to PollFetches.
//
// If there are no uncommitted offsets, this returns nil.
func (cl *Client) UncommittedOffsets() map[string]map[int32]EpochOffset {
	if g := cl.consumer.g; g != nil {
		return g.getUncommitted(true)
	}
	return nil
}

// MarkedOffsets returns the latest marked offsets. When autocommitting, a
// marked offset is an offset that can be committed, in comparison to a dirty
// offset that cannot yet be committed. MarkedOffsets returns nil if you are
// not using AutoCommitMarks.
func (cl *Client) MarkedOffsets() map[string]map[int32]EpochOffset {
	g := cl.consumer.g
	if g == nil || !cl.cfg.autocommitMarks {
		return nil
	}
	return g.getUncommitted(false)
}

// CommittedOffsets returns the latest committed offsets. Committed offsets are
// updated from commits or from joining a group and fetching offsets.
//
// If there are no committed offsets, this returns nil.
func (cl *Client) CommittedOffsets() map[string]map[int32]EpochOffset {
	g := cl.consumer.g
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.getUncommittedLocked(false, false)
}

func (g *groupConsumer) getUncommitted(dirty bool) map[string]map[int32]EpochOffset {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.getUncommittedLocked(true, dirty)
}

func (g *groupConsumer) getUncommittedLocked(head, dirty bool) map[string]map[int32]EpochOffset {
	if g.uncommitted == nil {
		return nil
	}

	var uncommitted map[string]map[int32]EpochOffset
	for topic, partitions := range g.uncommitted {
		var topicUncommitted map[int32]EpochOffset
		for partition, uncommit := range partitions {
			if head && (dirty && uncommit.dirty == uncommit.committed || !dirty && uncommit.head == uncommit.committed) {
				continue
			}
			if topicUncommitted == nil {
				if uncommitted == nil {
					uncommitted = make(map[string]map[int32]EpochOffset, len(g.uncommitted))
				}
				topicUncommitted = uncommitted[topic]
				if topicUncommitted == nil {
					topicUncommitted = make(map[int32]EpochOffset, len(partitions))
					uncommitted[topic] = topicUncommitted
				}
			}
			if head {
				if dirty {
					topicUncommitted[partition] = uncommit.dirty
				} else {
					topicUncommitted[partition] = uncommit.head
				}
			} else {
				topicUncommitted[partition] = uncommit.committed
			}
		}
	}
	return uncommitted
}

type commitContextFnT struct{}

var commitContextFn commitContextFnT

// PreCommitFnContext attaches fn to the context through WithValue. Using the
// context while committing allows fn to be called just before the commit is
// issued. This can be used to modify the actual commit, such as by associating
// metadata with partitions. If fn returns an error, the commit is not
// attempted.
func PreCommitFnContext(ctx context.Context, fn func(*kmsg.OffsetCommitRequest) error) context.Context {
	return context.WithValue(ctx, commitContextFn, fn)
}

type txnCommitContextFnT struct{}

var txnCommitContextFn txnCommitContextFnT

// PreTxnCommitFnContext attaches fn to the context through WithValue. Using
// the context while committing a transaction allows fn to be called just
// before the commit is issued. This can be used to modify the actual commit,
// such as by associating metadata with partitions (for transactions, the
// default internal metadata is the client's current member ID). If fn returns
// an error, the commit is not attempted. This context can be used in either
// GroupTransactSession.End or in Client.EndTransaction.
func PreTxnCommitFnContext(ctx context.Context, fn func(*kmsg.TxnOffsetCommitRequest) error) context.Context {
	return context.WithValue(ctx, txnCommitContextFn, fn)
}

// CommitRecords issues a synchronous offset commit for the offsets contained
// within rs. Retryable errors are retried up to the configured retry limit,
// and any unretryable error is returned.
//
// This function is useful as a simple way to commit offsets if you have
// disabled autocommitting. As an alternative if you always want to commit
// everything, see CommitUncommittedOffsets.
//
// Simple usage of this function may lead to duplicate records if a consumer
// group rebalance occurs before or while this function is being executed. You
// can avoid this scenario by calling CommitRecords in a custom
// OnPartitionsRevoked, but for most workloads, a small bit of potential
// duplicate processing is fine.  See the documentation on DisableAutoCommit
// for more details. You can also avoid this problem by using
// BlockRebalanceOnPoll, but that option comes with its own tradeoffs (refer to
// its documentation).
//
// It is recommended to always commit records in order (per partition). If you
// call this function twice with record for partition 0 at offset 999
// initially, and then with record for partition 0 at offset 4, you will rewind
// your commit.
//
// A use case for this function may be to partially process a batch of records,
// commit, and then continue to process the rest of the records. It is not
// recommended to call this for every record processed in a high throughput
// scenario, because you do not want to unnecessarily increase load on Kafka.
//
// If you do not want to wait for this function to complete before continuing
// processing records, you can call this function in a goroutine.
func (cl *Client) CommitRecords(ctx context.Context, rs ...*Record) error {
	// First build the offset commit map. We favor the latest epoch, then
	// offset, if any records map to the same topic / partition.
	offsets := make(map[string]map[int32]EpochOffset)
	for _, r := range rs {
		toffsets := offsets[r.Topic]
		if toffsets == nil {
			toffsets = make(map[int32]EpochOffset)
			offsets[r.Topic] = toffsets
		}

		if at, exists := toffsets[r.Partition]; exists {
			if at.Epoch > r.LeaderEpoch || at.Epoch == r.LeaderEpoch && at.Offset > r.Offset {
				continue
			}
		}
		toffsets[r.Partition] = EpochOffset{
			r.LeaderEpoch,
			r.Offset + 1, // need to advice to next offset to move forward
		}
	}

	var rerr error // return error

	// Our client retries an OffsetCommitRequest as necessary if the first
	// response partition has a retryable group error (group coordinator
	// loading, etc), so any partition error is fatal.
	cl.CommitOffsetsSync(ctx, offsets, func(_ *Client, _ *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
		if err != nil {
			rerr = err
			return
		}

		for _, topic := range resp.Topics {
			for _, partition := range topic.Partitions {
				if err := kerr.ErrorForCode(partition.ErrorCode); err != nil {
					rerr = err
					return
				}
			}
		}
	})

	return rerr
}

// MarkCommitRecords marks records to be available for autocommitting. This
// function is only useful if you use the AutoCommitMarks config option, see
// the documentation on that option for more details. This function does not
// allow rewinds.
func (cl *Client) MarkCommitRecords(rs ...*Record) {
	g := cl.consumer.g
	if g == nil || !cl.cfg.autocommitMarks {
		return
	}

	sort.Slice(rs, func(i, j int) bool {
		return rs[i].Topic < rs[j].Topic ||
			rs[i].Topic == rs[j].Topic && rs[i].Partition < rs[j].Partition
	})

	// protect g.uncommitted map
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.uncommitted == nil {
		g.uncommitted = make(uncommitted)
	}
	var curTopic string
	var curPartitions map[int32]uncommit
	for _, r := range rs {
		if curPartitions == nil || r.Topic != curTopic {
			curPartitions = g.uncommitted[r.Topic]
			if curPartitions == nil {
				curPartitions = make(map[int32]uncommit)
				g.uncommitted[r.Topic] = curPartitions
			}
			curTopic = r.Topic
		}

		current := curPartitions[r.Partition]
		if newHead := (EpochOffset{
			r.LeaderEpoch,
			r.Offset + 1,
		}); current.head.Less(newHead) {
			curPartitions[r.Partition] = uncommit{
				dirty:     current.dirty,
				committed: current.committed,
				head:      newHead,
			}
		}
	}
}

// MarkCommitOffsets marks offsets to be available for autocommitting. This
// function is only useful if you use the AutoCommitMarks config option, see
// the documentation on that option for more details. This function does not
// allow rewinds.
func (cl *Client) MarkCommitOffsets(unmarked map[string]map[int32]EpochOffset) {
	g := cl.consumer.g
	if g == nil || !cl.cfg.autocommitMarks {
		return
	}

	// protect g.uncommitted map
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.uncommitted == nil {
		g.uncommitted = make(uncommitted)
	}

	for topic, partitions := range unmarked {
		curPartitions := g.uncommitted[topic]
		if curPartitions == nil {
			curPartitions = make(map[int32]uncommit)
			g.uncommitted[topic] = curPartitions
		}

		for partition, newHead := range partitions {
			current := curPartitions[partition]
			if current.head.Less(newHead) {
				curPartitions[partition] = uncommit{
					dirty:     current.dirty,
					committed: current.committed,
					head:      newHead,
				}
			}
		}
	}
}

// CommitUncommittedOffsets issues a synchronous offset commit for any
// partition that has been consumed from that has uncommitted offsets.
// Retryable errors are retried up to the configured retry limit, and any
// unretryable error is returned.
//
// The recommended pattern for using this function is to have a poll / process
// / commit loop. First PollFetches, then process every record, then call
// CommitUncommittedOffsets.
//
// As an alternative if you want to commit specific records, see CommitRecords.
func (cl *Client) CommitUncommittedOffsets(ctx context.Context) error {
	// This function is just the tail end of CommitRecords just above.
	return cl.commitOffsets(ctx, cl.UncommittedOffsets())
}

// CommitMarkedOffsets issues a synchronous offset commit for any partition
// that has been consumed from that has marked offsets.  Retryable errors are
// retried up to the configured retry limit, and any unretryable error is
// returned.
//
// This function is only useful if you have marked offsets with
// MarkCommitRecords when using AutoCommitMarks, otherwise this is a no-op.
//
// The recommended pattern for using this function is to have a poll / process
// / commit loop. First PollFetches, then process every record,
// call MarkCommitRecords for the records you wish the commit and then call
// CommitMarkedOffsets.
//
// As an alternative if you want to commit specific records, see CommitRecords.
func (cl *Client) CommitMarkedOffsets(ctx context.Context) error {
	// This function is just the tail end of CommitRecords just above.
	marked := cl.MarkedOffsets()
	if len(marked) == 0 {
		return nil
	}
	return cl.commitOffsets(ctx, marked)
}

func (cl *Client) commitOffsets(ctx context.Context, offsets map[string]map[int32]EpochOffset) error {
	var rerr error
	cl.CommitOffsetsSync(ctx, offsets, func(_ *Client, _ *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
		if err != nil {
			rerr = err
			return
		}

		for _, topic := range resp.Topics {
			for _, partition := range topic.Partitions {
				if err := kerr.ErrorForCode(partition.ErrorCode); err != nil {
					rerr = err
					return
				}
			}
		}
	})
	return rerr
}

// CommitOffsetsSync cancels any active CommitOffsets, begins a commit that
// cannot be canceled, and waits for that commit to complete. This function
// will not return until the commit is done and the onDone callback is
// complete.
//
// The purpose of this function is for use in OnPartitionsRevoked or committing
// before leaving a group, because you do not want to have a commit issued in
// OnPartitionsRevoked canceled.
//
// This is an advanced function, and for simpler, more easily understandable
// committing, see CommitRecords and CommitUncommittedOffsets.
//
// For more information about committing and committing asynchronously, see
// CommitOffsets.
func (cl *Client) CommitOffsetsSync(
	ctx context.Context,
	uncommitted map[string]map[int32]EpochOffset,
	onDone func(*Client, *kmsg.OffsetCommitRequest, *kmsg.OffsetCommitResponse, error),
) {
	if onDone == nil {
		onDone = func(*Client, *kmsg.OffsetCommitRequest, *kmsg.OffsetCommitResponse, error) {}
	}

	g := cl.consumer.g
	if g == nil {
		onDone(cl, kmsg.NewPtrOffsetCommitRequest(), kmsg.NewPtrOffsetCommitResponse(), errNotGroup)
		return
	}
	if len(uncommitted) == 0 {
		onDone(cl, kmsg.NewPtrOffsetCommitRequest(), kmsg.NewPtrOffsetCommitResponse(), nil)
		return
	}
	g.commitOffsetsSync(ctx, uncommitted, onDone)
}

// waitJoinSyncMu is a rather insane way to try to grab a lock, but also return
// early if we have to wait and the context is canceled.
func (g *groupConsumer) waitJoinSyncMu(ctx context.Context) error {
	if g.noCommitDuringJoinAndSync.TryRLock() {
		g.cfg.logger.Log(LogLevelDebug, "grabbed join/sync mu on first try")
		return nil
	}

	var (
		blockJoinSyncCh = make(chan struct{})
		mu              sync.Mutex
		returned        bool
		maybeRUnlock    = func() {
			mu.Lock()
			defer mu.Unlock()
			if returned {
				g.noCommitDuringJoinAndSync.RUnlock()
			}
			returned = true
		}
	)

	go func() {
		g.noCommitDuringJoinAndSync.RLock()
		close(blockJoinSyncCh)
		maybeRUnlock()
	}()

	select {
	case <-blockJoinSyncCh:
		g.cfg.logger.Log(LogLevelDebug, "grabbed join/sync mu after waiting")
		return nil
	case <-ctx.Done():
		g.cfg.logger.Log(LogLevelDebug, "not grabbing mu because context canceled")
		maybeRUnlock()
		return ctx.Err()
	}
}

func (g *groupConsumer) commitOffsetsSync(
	ctx context.Context,
	uncommitted map[string]map[int32]EpochOffset,
	onDone func(*Client, *kmsg.OffsetCommitRequest, *kmsg.OffsetCommitResponse, error),
) {
	g.cfg.logger.Log(LogLevelDebug, "in CommitOffsetsSync", "group", g.cfg.group, "with", uncommitted)
	defer g.cfg.logger.Log(LogLevelDebug, "left CommitOffsetsSync", "group", g.cfg.group)

	done := make(chan struct{})
	defer func() { <-done }()

	if onDone == nil {
		onDone = func(*Client, *kmsg.OffsetCommitRequest, *kmsg.OffsetCommitResponse, error) {}
	}

	if err := g.waitJoinSyncMu(ctx); err != nil {
		onDone(g.cl, kmsg.NewPtrOffsetCommitRequest(), kmsg.NewPtrOffsetCommitResponse(), err)
		close(done)
		return
	}

	g.syncCommitMu.Lock() // block all other concurrent commits until our OnDone is done.
	unblockCommits := func(cl *Client, req *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
		g.noCommitDuringJoinAndSync.RUnlock()
		defer close(done)
		defer g.syncCommitMu.Unlock()
		onDone(cl, req, resp, err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.blockAuto = true
	unblockAuto := func(cl *Client, req *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
		unblockCommits(cl, req, resp, err)
		g.mu.Lock()
		defer g.mu.Unlock()
		g.blockAuto = false
	}

	g.commit(ctx, uncommitted, unblockAuto)
}

// CommitOffsets commits the given offsets for a group, calling onDone with the
// commit request and either the response or an error if the response was not
// issued. If uncommitted is empty or the client is not consuming as a group,
// onDone is called with (nil, nil, nil) and this function returns immediately.
// It is OK if onDone is nil, but you will not know if your commit succeeded.
//
// This is an advanced function and is difficult to use correctly. For simpler,
// more easily understandable committing, see CommitRecords and
// CommitUncommittedOffsets.
//
// This function itself does not wait for the commit to finish. By default,
// this function is an asynchronous commit. You can use onDone to make it sync.
// If autocommitting is enabled, this function blocks autocommitting until this
// function is complete and the onDone has returned.
//
// It is invalid to use this function to commit offsets for a transaction.
//
// Note that this function ensures absolute ordering of commit requests by
// canceling prior requests and ensuring they are done before executing a new
// one. This means, for absolute control, you can use this function to
// periodically commit async and then issue a final sync commit before quitting
// (this is the behavior of autocommiting and using the default revoke). This
// differs from the Java async commit, which does not retry requests to avoid
// trampling on future commits.
//
// It is highly recommended to check the response's partition's error codes if
// the response is non-nil. While unlikely, individual partitions can error.
// This is most likely to happen if a commit occurs too late in a rebalance
// event.
//
// Do not use this async CommitOffsets in OnPartitionsRevoked, instead use
// CommitOffsetsSync. If you commit async, the rebalance will proceed before
// this function executes, and you will commit offsets for partitions that have
// moved to a different consumer.
func (cl *Client) CommitOffsets(
	ctx context.Context,
	uncommitted map[string]map[int32]EpochOffset,
	onDone func(*Client, *kmsg.OffsetCommitRequest, *kmsg.OffsetCommitResponse, error),
) {
	cl.cfg.logger.Log(LogLevelDebug, "in CommitOffsets", "with", uncommitted)
	defer cl.cfg.logger.Log(LogLevelDebug, "left CommitOffsets")
	if onDone == nil {
		onDone = func(*Client, *kmsg.OffsetCommitRequest, *kmsg.OffsetCommitResponse, error) {}
	}

	g := cl.consumer.g
	if g == nil {
		onDone(cl, kmsg.NewPtrOffsetCommitRequest(), kmsg.NewPtrOffsetCommitResponse(), errNotGroup)
		return
	}
	if len(uncommitted) == 0 {
		onDone(cl, kmsg.NewPtrOffsetCommitRequest(), kmsg.NewPtrOffsetCommitResponse(), nil)
		return
	}

	if err := g.waitJoinSyncMu(ctx); err != nil {
		onDone(g.cl, kmsg.NewPtrOffsetCommitRequest(), kmsg.NewPtrOffsetCommitResponse(), err)
		return
	}

	g.syncCommitMu.RLock() // block sync commit, but allow other concurrent Commit to cancel us
	unblockJoinSync := func(cl *Client, req *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
		g.noCommitDuringJoinAndSync.RUnlock()
		defer g.syncCommitMu.RUnlock()
		onDone(cl, req, resp, err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.blockAuto = true
	unblockAuto := func(cl *Client, req *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
		unblockJoinSync(cl, req, resp, err)
		g.mu.Lock()
		defer g.mu.Unlock()
		g.blockAuto = false
	}

	g.commit(ctx, uncommitted, unblockAuto)
}

// defaultRevoke commits the last fetched offsets and waits for the commit to
// finish. This is the default onRevoked function which, when combined with the
// default autocommit, ensures we never miss committing everything.
//
// Note that the heartbeat loop invalidates all buffered, unpolled fetches
// before revoking, meaning this truly will commit all polled fetches.
func (g *groupConsumer) defaultRevoke(context.Context, *Client, map[string][]int32) {
	if !g.cfg.autocommitDisable {
		// We use the client's context rather than the group context,
		// because this could come from the group being left. The group
		// context will already be canceled.
		g.commitOffsetsSync(g.cl.ctx, g.getUncommitted(false), g.cfg.commitCallback)
	}
}

// The actual logic to commit. This is called under two locks:
//   - g.noCommitDuringJoinAndSync.RLock()
//   - g.mu.Lock()
//
// By blocking the JoinGroup from being issued, or blocking the commit on join
// & sync finishing, we avoid RebalanceInProgress and IllegalGeneration.  The
// former error happens if a commit arrives to the broker between the two, the
// latter error happens when a commit arrives to the broker with the old
// generation (it was in flight before sync finished).
//
// Practically, what this means is that a user's commits will be blocked if
// they try to commit between join and sync.
//
// For eager consuming, the user should not have any partitions to commit
// anyway. For cooperative consuming, a rebalance can happen after at any
// moment. We block only revokation aspects of rebalances with
// BlockRebalanceOnPoll; we want to allow the cooperative part of rebalancing
// to occur.
func (g *groupConsumer) commit(
	ctx context.Context,
	uncommitted map[string]map[int32]EpochOffset,
	onDone func(*Client, *kmsg.OffsetCommitRequest, *kmsg.OffsetCommitResponse, error),
) {
	// The user could theoretically give us topics that have no partitions
	// to commit. We strip those: Kafka does not reply to them, and we
	// expect all partitions in our request to be replied to in
	// updateCommitted. If any topic is empty, we deeply clone and then
	// strip everything empty. See #186.
	var clone bool
	for _, ps := range uncommitted {
		if len(ps) == 0 {
			clone = true
			break
		}
	}
	if clone {
		dup := make(map[string]map[int32]EpochOffset, len(uncommitted))
		for t, ps := range uncommitted {
			if len(ps) == 0 {
				continue
			}
			dupPs := make(map[int32]EpochOffset, len(ps))
			dup[t] = dupPs
			for p, eo := range ps {
				dupPs[p] = eo
			}
		}
		uncommitted = dup
	}

	if len(uncommitted) == 0 { // only empty if called thru autocommit / default revoke
		// We have to do this concurrently because the expectation is
		// that commit itself does not block.
		go onDone(g.cl, kmsg.NewPtrOffsetCommitRequest(), kmsg.NewPtrOffsetCommitResponse(), nil)
		return
	}

	priorCancel := g.commitCancel
	priorDone := g.commitDone

	commitCtx, commitCancel := context.WithCancel(ctx) // enable ours to be canceled and waited for
	commitDone := make(chan struct{})

	g.commitCancel = commitCancel
	g.commitDone = commitDone

	req := kmsg.NewPtrOffsetCommitRequest()
	req.Group = g.cfg.group
	memberID, generation := g.memberGen.load()
	req.Generation = generation
	req.MemberID = memberID
	req.InstanceID = g.cfg.instanceID

	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				commitCancel()
			case <-commitCtx.Done():
			}
		}()
	}

	go func() {
		defer close(commitDone) // allow future commits to continue when we are done
		defer commitCancel()
		if priorDone != nil { // wait for any prior request to finish
			select {
			case <-priorDone:
			default:
				g.cfg.logger.Log(LogLevelDebug, "canceling prior commit to issue another", "group", g.cfg.group)
				priorCancel()
				<-priorDone
			}
		}
		g.cfg.logger.Log(LogLevelDebug, "issuing commit", "group", g.cfg.group, "uncommitted", uncommitted)

		for topic, partitions := range uncommitted {
			reqTopic := kmsg.NewOffsetCommitRequestTopic()
			reqTopic.Topic = topic
			for partition, eo := range partitions {
				reqPartition := kmsg.NewOffsetCommitRequestTopicPartition()
				reqPartition.Partition = partition
				reqPartition.Offset = eo.Offset
				reqPartition.LeaderEpoch = eo.Epoch // KIP-320
				reqPartition.Metadata = &req.MemberID
				reqTopic.Partitions = append(reqTopic.Partitions, reqPartition)
			}
			req.Topics = append(req.Topics, reqTopic)
		}

		if fn, ok := ctx.Value(commitContextFn).(func(*kmsg.OffsetCommitRequest) error); ok {
			if err := fn(req); err != nil {
				onDone(g.cl, req, nil, err)
				return
			}
		}

		resp, err := req.RequestWith(commitCtx, g.cl)
		if err != nil {
			onDone(g.cl, req, nil, err)
			return
		}
		g.updateCommitted(req, resp)
		onDone(g.cl, req, resp, nil)
	}()
}

type reNews struct {
	added   map[string][]string
	skipped []string
}

func (r *reNews) add(re, match string) {
	if r.added == nil {
		r.added = make(map[string][]string)
	}
	r.added[re] = append(r.added[re], match)
}

func (r *reNews) skip(topic string) {
	r.skipped = append(r.skipped, topic)
}

func (r *reNews) log(cfg *cfg) {
	if len(r.added) == 0 && len(r.skipped) == 0 {
		return
	}
	var addeds []string
	for re, matches := range r.added {
		sort.Strings(matches)
		addeds = append(addeds, fmt.Sprintf("%s[%s]", re, strings.Join(matches, " ")))
	}
	added := strings.Join(addeds, " ")
	sort.Strings(r.skipped)
	cfg.logger.Log(LogLevelInfo, "consumer regular expressions evaluated on new topics", "added", added, "evaluated_and_skipped", r.skipped)
}
