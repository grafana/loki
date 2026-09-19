package kfake

import (
	"bytes"
	"cmp"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type (
	groups struct {
		c  *Cluster
		gs map[string]*group
	}

	group struct {
		c    *Cluster
		gs   *groups
		name string
		typ  string

		state   groupState
		emptyAt time.Time // when classic group entered Empty state (for offset expiration)

		leader        string
		members       map[string]*groupMember
		pending       map[string]*groupMember
		staticMembers map[string]string // instanceID -> memberID; shared by classic and 848

		commits tps[offsetCommit]

		generation            int32
		groupEpoch            int32 // 848: bumped on subscription/membership change
		targetAssignmentEpoch int32 // 848: set to groupEpoch after target computation

		protocolType string
		protocols    map[string]int
		protocol     string

		reqCh     chan *clientReq
		controlCh chan func()

		nJoining int

		tRebalance     *time.Timer
		pendingSyncIDs map[string]struct{} // members that got JoinGroup but haven't sent SyncGroup yet
		tPendingSync   *time.Timer         // fires when pending sync members should be removed

		// KIP-848 consumer group fields
		assignorName    string
		consumerMembers map[string]*consumerMember
		partitionEpochs map[uuid]map[int32]int32 // (topicID, partition) -> owning member's epoch; -1 or absent means free
		lastTopicMeta   topicMetaSnap            // last snapshot received, for recomputation on member removal

		quit   sync.Once
		quitCh chan struct{}
	}

	groupMember struct {
		memberID   string
		instanceID *string
		clientID   string
		clientHost string

		join *kmsg.JoinGroupRequest // the latest join request

		// waitingReply is non-nil if a client is waiting for a reply
		// from us for a JoinGroupRequest or a SyncGroupRequest.
		waitingReply *clientReq

		assignment []byte

		t    *time.Timer
		last time.Time
	}

	consumerMember struct {
		memberID   string
		clientID   string
		clientHost string
		instanceID *string
		rackID     *string

		memberEpoch         int32 // confirmed epoch
		previousMemberEpoch int32 // epoch before last advance
		state               consumerMemberState

		// KIP-1251: per-partition assignment epoch. Each partition
		// tracks the member epoch at which it was first assigned.
		// OffsetCommit accepts reqEpoch >= partition assignment epoch.
		partAssignmentEpochs map[uuid]map[int32]int32

		rebalanceTimeoutMs int32
		serverAssignor     string

		subscribedTopics      []string
		subscribedTopicRegex  *regexp.Regexp
		subscribedRegexSource string // source string for change detection

		targetAssignment            map[uuid][]int32 // what server wants member to own
		partitionsPendingRevocation map[uuid][]int32 // told to revoke, awaiting client confirmation; contributes to epoch map
		lastReconciledSent          map[uuid][]int32 // exact reconciled assignment last included in a response

		t    *time.Timer // session timeout: fences member if no heartbeats
		last time.Time

		tRebal *time.Timer // rebalance timeout: fences member if slow to revoke
	}

	offsetCommit struct {
		offset      int64
		leaderEpoch int32
		metadata    *string
		lastCommit  time.Time
	}

	// topicMetaSnap is a snapshot of topic metadata taken in Cluster.run
	// and passed to group.manage for server-side assignment.
	topicMetaSnap = map[string]topicSnapInfo

	topicSnapInfo struct {
		id         uuid
		partitions int32
	}

	groupState int8
)

const (
	groupEmpty groupState = iota
	groupStable
	groupPreparingRebalance
	groupCompletingRebalance
	groupDead
	groupReconciling
)

type consumerMemberState int8

const (
	cmStable               consumerMemberState = iota
	cmUnrevokedPartitions                      // waiting for client to release revoked partitions
	cmUnreleasedPartitions                     // waiting for other members to release partitions
)

func (gs groupState) String() string {
	switch gs {
	case groupEmpty:
		return "Empty"
	case groupStable:
		return "Stable"
	case groupPreparingRebalance:
		return "PreparingRebalance"
	case groupCompletingRebalance:
		return "CompletingRebalance"
	case groupDead:
		return "Dead"
	case groupReconciling:
		return "Reconciling"
	default:
		return "Unknown"
	}
}

// emptyConsumerAssignment is a pre-serialized empty ConsumerMemberAssignment.
// Used to ensure followers in a "consumer" protocol group always receive a
// syntactically valid assignment blob even when the leader assigns them nothing.
// Technically, the broker should pass through whatever bytes the leader sends
// (including empty), but some clients fail to decode an empty assignment.
// We only apply this workaround for the "consumer" protocol type since other
// protocol types may use entirely different assignment formats.
var emptyConsumerAssignment = func() []byte {
	var assignment kmsg.ConsumerMemberAssignment
	return assignment.AppendTo(nil)
}()

func (c *Cluster) coordinator(id string) *broker {
	gen := c.coordinatorGen.Load()
	n := hashString(fmt.Sprintf("%d", gen)+"\x00\x00"+id) % uint64(len(c.bs))
	return c.bs[n]
}

func (c *Cluster) snapshotTopicMeta() topicMetaSnap {
	snap := make(topicMetaSnap, len(c.data.tps))
	for topic, ps := range c.data.tps {
		snap[topic] = topicSnapInfo{id: c.data.t2id[topic], partitions: int32(len(ps))}
	}
	return snap
}

// notifyTopicChange recomputes target assignments for all consumer and
// share groups after a topic is created, deleted, or has partitions
// added. We capture a fresh metadata snapshot here (in the cluster run
// loop where c.data is safe to read) and pass it to the manage
// goroutine so that the recomputation always sees the latest topics.
// This avoids a race where the manage goroutine could recompute using
// a stale snapshot from a heartbeat that was enqueued before the topic
// change.
//
// The generation bump matches Kafka's behavior where topic changes bump
// the group epoch. This ensures heartbeat responses keep re-sending the
// assignment until the member epoch catches up, which only happens when
// the client confirms the assignment via the heartbeat Topics field.
// Without this, a client that receives an assignment with an unknown
// topic ID (metadata not yet refreshed) would miss the assignment
// permanently because the server recorded it as delivered.
//
// This blocks the cluster run loop until each group's manage goroutine
// processes the notification. This is safe: the manage goroutine never
// calls c.admin() and replies go to cc.respCh (drained by the
// connection write goroutine, not the run loop).
func (c *Cluster) notifyTopicChange() {
	snap := c.snapshotTopicMeta()
	for _, g := range c.groups.gs {
		select {
		case g.controlCh <- func() {
			if len(g.consumerMembers) > 0 {
				g.groupEpoch++
				g.lastTopicMeta = snap
				g.computeTargetAssignment(snap)
				g.updateConsumerStateField()
				g.persistMeta848()
			}
		}:
		case <-g.quitCh:
		case <-g.c.die:
		}
	}
	for _, sg := range c.shareGroups.gs {
		select {
		case sg.controlCh <- func() {
			if len(sg.members) > 0 {
				sg.groupEpoch++
				sg.lastTopicMeta = snap
				sg.recomputeAssignments()
			}
		}:
		case <-sg.quitCh:
		case <-sg.c.die:
		}
	}
}

func (c *Cluster) validateGroup(creq *clientReq, group string) *kerr.Error {
	switch key := kmsg.Key(creq.kreq.Key()); key {
	case kmsg.OffsetCommit, kmsg.OffsetFetch, kmsg.DescribeGroups, kmsg.DeleteGroups, kmsg.ConsumerGroupDescribe:
	default:
		if group == "" {
			return kerr.InvalidGroupID
		}
	}
	coordinator := c.coordinator(group).node
	if coordinator != creq.cc.b.node {
		return kerr.NotCoordinator
	}
	return nil
}

func generateMemberID(clientID string, instanceID *string) string {
	if instanceID == nil {
		return clientID + "-" + randStrUUID()
	}
	return *instanceID + "-" + randStrUUID()
}

////////////
// GROUPS //
////////////

func (g *group) logName() string { return g.name[:min(16, len(g.name))] }

func (gs *groups) newGroup(name string) *group {
	return &group{
		c:             gs.c,
		gs:            gs,
		name:          name,
		typ:           "classic", // group-coordinator/src/main/java/org/apache/kafka/coordinator/group/Group.java
		members:       make(map[string]*groupMember),
		pending:       make(map[string]*groupMember),
		staticMembers: make(map[string]string),
		protocols:     make(map[string]int),
		reqCh:         make(chan *clientReq, 16),
		controlCh:     make(chan func(), 1), // buffer 1: holds a pending notifyTopicChange
		quitCh:        make(chan struct{}),
	}
}

// handleJoin completely hijacks the incoming request.
func (gs *groups) handleJoin(creq *clientReq) {
	if gs.gs == nil {
		gs.gs = make(map[string]*group)
	}
	req := creq.kreq.(*kmsg.JoinGroupRequest)

	// Group type exclusivity: if this group ID is already a share
	// group, reject the classic consumer join.
	if _, isShare := gs.c.shareGroups.gs[req.Group]; isShare {
		resp := req.ResponseKind().(*kmsg.JoinGroupResponse)
		resp.ErrorCode = kerr.GroupIDNotFound.Code
		resp.MemberID = ""
		resp.Generation = -1
		creq.cc.respCh <- clientResp{kresp: resp, corr: creq.corr, seq: creq.seq}
		return
	}

start:
	g := gs.gs[req.Group]
	if g == nil {
		g = gs.newGroup(req.Group)
		waitJoin := make(chan struct{})
		gs.gs[req.Group] = g
		go g.manage(func() { close(waitJoin) })
		defer func() { <-waitJoin }()
	}
	select {
	case g.reqCh <- creq:
	case <-g.quitCh:
		delete(gs.gs, req.Group)
		goto start
	case <-g.c.die:
	}
}

// Returns true if the request is hijacked and handled, otherwise false if the
// group does not exist.
func (gs *groups) handleHijack(group string, creq *clientReq) bool {
	if gs.gs == nil {
		return false
	}
	g := gs.gs[group]
	if g == nil {
		return false
	}
	select {
	case g.reqCh <- creq:
		return true
	case <-g.quitCh:
		return false
	case <-g.c.die:
		return false
	}
}

func (gs *groups) handleSync(creq *clientReq) bool {
	return gs.handleHijack(creq.kreq.(*kmsg.SyncGroupRequest).Group, creq)
}

func (gs *groups) handleHeartbeat(creq *clientReq) bool {
	return gs.handleHijack(creq.kreq.(*kmsg.HeartbeatRequest).Group, creq)
}

func (gs *groups) handleLeave(creq *clientReq) bool {
	return gs.handleHijack(creq.kreq.(*kmsg.LeaveGroupRequest).Group, creq)
}

func (gs *groups) handleOffsetCommit(creq *clientReq) {
	if gs.gs == nil {
		gs.gs = make(map[string]*group)
	}
	req := creq.kreq.(*kmsg.OffsetCommitRequest)
start:
	g := gs.gs[req.Group]
	if g == nil {
		g = gs.newGroup(req.Group)
		waitCommit := make(chan struct{})
		gs.gs[req.Group] = g
		go g.manage(func() { close(waitCommit) })
		defer func() { <-waitCommit }()
	}
	select {
	case g.reqCh <- creq:
	case <-g.quitCh:
		delete(gs.gs, req.Group)
		goto start
	case <-g.c.die:
	}
}

func (gs *groups) handleOffsetDelete(creq *clientReq) bool {
	return gs.handleHijack(creq.kreq.(*kmsg.OffsetDeleteRequest).Group, creq)
}

func (gs *groups) handleList(creq *clientReq) *kmsg.ListGroupsResponse {
	req := creq.kreq.(*kmsg.ListGroupsRequest)
	resp := req.ResponseKind().(*kmsg.ListGroupsResponse)

	for _, g := range gs.gs {
		if g.c.coordinator(g.name).node != creq.cc.b.node {
			continue
		}
		// ACL check: DESCRIBE on Group - filter out groups without permission
		if !g.c.allowedACL(creq, g.name, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDescribe) {
			continue
		}
		g.waitControl(func() {
			if len(req.StatesFilter) > 0 && !slices.Contains(req.StatesFilter, g.state.String()) {
				return
			}
			if len(req.TypesFilter) > 0 && !slices.Contains(req.TypesFilter, g.typ) {
				return
			}
			sg := kmsg.NewListGroupsResponseGroup()
			sg.Group = g.name
			sg.ProtocolType = g.protocolType
			sg.GroupState = g.state.String()
			sg.GroupType = g.typ
			resp.Groups = append(resp.Groups, sg)
		})
	}
	return resp
}

func (gs *groups) handleDescribe(creq *clientReq) *kmsg.DescribeGroupsResponse {
	req := creq.kreq.(*kmsg.DescribeGroupsRequest)
	resp := req.ResponseKind().(*kmsg.DescribeGroupsResponse)

	doneg := func(name string) *kmsg.DescribeGroupsResponseGroup {
		sg := kmsg.NewDescribeGroupsResponseGroup()
		sg.Group = name
		resp.Groups = append(resp.Groups, sg)
		return &resp.Groups[len(resp.Groups)-1]
	}

	for _, rg := range req.Groups {
		sg := doneg(rg)
		// ACL check: DESCRIBE on Group
		if !gs.c.allowedACL(creq, rg, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDescribe) {
			sg.ErrorCode = kerr.GroupAuthorizationFailed.Code
			continue
		}
		if kerr := gs.c.validateGroup(creq, rg); kerr != nil {
			sg.ErrorCode = kerr.Code
			continue
		}
		g, ok := gs.gs[rg]
		if !ok {
			sg.State = groupDead.String()
			if req.Version >= 6 {
				sg.ErrorCode = kerr.GroupIDNotFound.Code
			}
			if req.IncludeAuthorizedOperations {
				sg.AuthorizedOperations = gs.c.groupAuthorizedOps(creq, rg)
			}
			continue
		}
		if !g.waitControl(func() {
			sg.State = g.state.String()
			sg.ProtocolType = g.protocolType
			if g.state == groupStable {
				sg.Protocol = g.protocol
			}
			for _, m := range g.members {
				sm := kmsg.NewDescribeGroupsResponseGroupMember()
				sm.MemberID = m.memberID
				sm.InstanceID = m.instanceID
				sm.ClientID = m.clientID
				sm.ClientHost = m.clientHost
				if g.state == groupStable {
					for _, p := range m.join.Protocols {
						if p.Name == g.protocol {
							sm.ProtocolMetadata = p.Metadata
							break
						}
					}
					sm.MemberAssignment = m.assignment
				}
				sg.Members = append(sg.Members, sm)
			}
			if req.IncludeAuthorizedOperations {
				sg.AuthorizedOperations = gs.c.groupAuthorizedOps(creq, rg)
			}
		}) {
			sg.State = groupDead.String()
		}
	}
	return resp
}

func (gs *groups) handleDelete(creq *clientReq) *kmsg.DeleteGroupsResponse {
	req := creq.kreq.(*kmsg.DeleteGroupsRequest)
	resp := req.ResponseKind().(*kmsg.DeleteGroupsResponse)

	doneg := func(name string) *kmsg.DeleteGroupsResponseGroup {
		sg := kmsg.NewDeleteGroupsResponseGroup()
		sg.Group = name
		resp.Groups = append(resp.Groups, sg)
		return &resp.Groups[len(resp.Groups)-1]
	}

	for _, rg := range req.Groups {
		sg := doneg(rg)
		// ACL check: DELETE on Group
		if !gs.c.allowedACL(creq, rg, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDelete) {
			sg.ErrorCode = kerr.GroupAuthorizationFailed.Code
			continue
		}
		if kerr := gs.c.validateGroup(creq, rg); kerr != nil {
			sg.ErrorCode = kerr.Code
			continue
		}
		g, ok := gs.gs[rg]
		if !ok {
			sg.ErrorCode = kerr.GroupIDNotFound.Code
			continue
		}
		if !g.waitControl(func() {
			if g.typ == "consumer" {
				if g.activeConsumerCount() == 0 {
					g.quitOnce()
				} else {
					sg.ErrorCode = kerr.NonEmptyGroup.Code
				}
			} else {
				switch g.state {
				case groupDead:
					sg.ErrorCode = kerr.GroupIDNotFound.Code
				case groupEmpty:
					g.quitOnce()
				case groupPreparingRebalance, groupCompletingRebalance, groupStable, groupReconciling:
					sg.ErrorCode = kerr.NonEmptyGroup.Code
				}
			}
		}) {
			sg.ErrorCode = kerr.GroupIDNotFound.Code
		}
		// Delete from gs.gs in the Cluster.run() goroutine, not
		// inside the waitControl callback. The callback runs in the
		// manage goroutine; if it calls quitOnce() (closing quitCh),
		// waitControl can return before the callback finishes, and a
		// delete(gs.gs) in the callback would race with any
		// concurrent gs.gs iteration in Cluster.run().
		select {
		case <-g.quitCh:
			delete(gs.gs, rg)
		default:
		}
	}
	return resp
}

func (gs *groups) handleOffsetFetch(creq *clientReq) *kmsg.OffsetFetchResponse {
	req := creq.kreq.(*kmsg.OffsetFetchRequest)
	resp := req.ResponseKind().(*kmsg.OffsetFetchResponse)

	if req.Version <= 7 {
		rg := kmsg.NewOffsetFetchRequestGroup()
		rg.Group = req.Group
		if req.Topics != nil {
			rg.Topics = make([]kmsg.OffsetFetchRequestGroupTopic, 0, len(req.Topics))
		}
		for _, t := range req.Topics {
			rt := kmsg.NewOffsetFetchRequestGroupTopic()
			rt.Topic = t.Topic
			rt.Partitions = t.Partitions
			rg.Topics = append(rg.Topics, rt)
		}
		req.Groups = append(req.Groups, rg)

		defer func() {
			g0 := resp.Groups[0]
			resp.ErrorCode = g0.ErrorCode
			for _, t := range g0.Topics {
				st := kmsg.NewOffsetFetchResponseTopic()
				st.Topic = t.Topic
				for _, p := range t.Partitions {
					sp := kmsg.NewOffsetFetchResponseTopicPartition()
					sp.Partition = p.Partition
					sp.Offset = p.Offset
					sp.LeaderEpoch = p.LeaderEpoch
					sp.Metadata = p.Metadata
					sp.ErrorCode = p.ErrorCode
					st.Partitions = append(st.Partitions, sp)
				}
				resp.Topics = append(resp.Topics, st)
			}
		}()
	}

	doneg := func(name string) *kmsg.OffsetFetchResponseGroup {
		sg := kmsg.NewOffsetFetchResponseGroup()
		sg.Group = name
		resp.Groups = append(resp.Groups, sg)
		return &resp.Groups[len(resp.Groups)-1]
	}

	for _, rg := range req.Groups {
		sg := doneg(rg.Group)
		// ACL check: DESCRIBE on Group
		if !gs.c.allowedACL(creq, rg.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDescribe) {
			sg.ErrorCode = kerr.GroupAuthorizationFailed.Code
			continue
		}
		if kerr := gs.c.validateGroup(creq, rg.Group); kerr != nil {
			sg.ErrorCode = kerr.Code
			continue
		}

		// v10: resolve TopicIDs to topic names before processing.
		if req.Version >= 10 && rg.Topics != nil {
			for i := range rg.Topics {
				t := &rg.Topics[i]
				if name, ok := gs.c.data.id2t[t.TopicID]; ok {
					t.Topic = name
				}
			}
		}

		// KIP-447: check for pending transactional offsets before
		// entering waitControl (pids must be accessed from run()).
		// Real Kafka returns UNSTABLE_OFFSET_COMMIT per-partition,
		// not per-group.
		unstable := req.RequireStable && gs.c.pids.hasUnstableOffsets(rg.Group)
		g, ok := gs.gs[rg.Group]
		if !ok {
			sg.ErrorCode = kerr.GroupIDNotFound.Code
			continue
		}
		if !g.waitControl(func() {
			// KIP-848: validate MemberID/MemberEpoch for consumer
			// groups. Admin fetches (MemberID absent with
			// MemberEpoch < 0) skip validation. MemberID="" is a
			// real-but-invalid memberId per Java, not an admin
			// signal - it falls through to the member lookup and
			// returns UnknownMemberID.
			adminFetch := rg.MemberID == nil && rg.MemberEpoch < 0
			if g.typ == "consumer" && !adminFetch {
				if rg.MemberID == nil || *rg.MemberID == "" {
					sg.ErrorCode = kerr.UnknownMemberID.Code
					return
				}
				m, ok := g.consumerMembers[*rg.MemberID]
				if !ok {
					sg.ErrorCode = kerr.UnknownMemberID.Code
					return
				}
				if rg.MemberEpoch != m.memberEpoch {
					sg.ErrorCode = kerr.StaleMemberEpoch.Code
					return
				}
			}
			if rg.Topics == nil {
				for t, ps := range g.commits {
					st := kmsg.NewOffsetFetchResponseGroupTopic()
					st.Topic = t
					st.TopicID = gs.c.data.t2id[t]
					for p, c := range ps {
						sp := kmsg.NewOffsetFetchResponseGroupTopicPartition()
						sp.Partition = p
						if unstable {
							sp.ErrorCode = kerr.UnstableOffsetCommit.Code
							sp.Offset = -1
							sp.LeaderEpoch = -1
						} else {
							sp.Offset = c.offset
							sp.LeaderEpoch = c.leaderEpoch
							sp.Metadata = c.metadata
						}
						st.Partitions = append(st.Partitions, sp)
					}
					sg.Topics = append(sg.Topics, st)
				}
			} else {
				for _, t := range rg.Topics {
					st := kmsg.NewOffsetFetchResponseGroupTopic()
					st.Topic = t.Topic
					st.TopicID = t.TopicID
					for _, p := range t.Partitions {
						sp := kmsg.NewOffsetFetchResponseGroupTopicPartition()
						sp.Partition = p
						if unstable {
							sp.ErrorCode = kerr.UnstableOffsetCommit.Code
							sp.Offset = -1
							sp.LeaderEpoch = -1
						} else {
							c, ok := g.commits.getp(t.Topic, p)
							if !ok {
								sp.Offset = -1
								sp.LeaderEpoch = -1
							} else {
								sp.Offset = c.offset
								sp.LeaderEpoch = c.leaderEpoch
								sp.Metadata = c.metadata
							}
						}
						st.Partitions = append(st.Partitions, sp)
					}
					sg.Topics = append(sg.Topics, st)
				}
			}
		}) {
			sg.ErrorCode = kerr.GroupIDNotFound.Code
		}
	}
	return resp
}

func (g *group) handleOffsetDelete(creq *clientReq) *kmsg.OffsetDeleteResponse {
	req := creq.kreq.(*kmsg.OffsetDeleteRequest)
	resp := req.ResponseKind().(*kmsg.OffsetDeleteResponse)

	// ACL check: DELETE on Group
	if !g.c.allowedACL(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDelete) {
		resp.ErrorCode = kerr.GroupAuthorizationFailed.Code
		return resp
	}

	if kerr := g.c.validateGroup(creq, req.Group); kerr != nil {
		resp.ErrorCode = kerr.Code
		return resp
	}

	tidx := make(map[string]int)
	donet := func(t string) *kmsg.OffsetDeleteResponseTopic {
		if i, ok := tidx[t]; ok {
			return &resp.Topics[i]
		}
		tidx[t] = len(resp.Topics)
		st := kmsg.NewOffsetDeleteResponseTopic()
		st.Topic = t
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donep := func(t string, p int32, errCode int16) {
		sp := kmsg.NewOffsetDeleteResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		st := donet(t)
		st.Partitions = append(st.Partitions, sp)
	}

	// empty: delete everything in request
	// preparingRebalance, completingRebalance, stable:
	//   * if consumer, delete everything not subscribed to
	//   * if not consumer, delete nothing, error with non_empty_group
	var subTopics map[string]struct{}
	switch g.state {
	default:
		resp.ErrorCode = kerr.GroupIDNotFound.Code
		return resp
	case groupEmpty:
	case groupPreparingRebalance, groupCompletingRebalance, groupStable:
		if g.protocolType != "consumer" {
			resp.ErrorCode = kerr.NonEmptyGroup.Code
			return resp
		}
		subTopics = g.classicSubscribedTopics()
	}

	for _, t := range req.Topics {
		for _, p := range t.Partitions {
			if _, ok := subTopics[t.Topic]; ok {
				donep(t.Topic, p.Partition, kerr.GroupSubscribedToTopic.Code)
				continue
			}
			g.deleteCommitAndPersist(t.Topic, p.Partition)
			donep(t.Topic, p.Partition, 0)
		}
	}

	return resp
}

////////////////////
// GROUP HANDLING //
////////////////////

func (g *group) manage(detachNew func()) {
	// On the first join only, we want to ensure that if the join is
	// invalid, we clean the group up before we detach from the cluster
	// serialization loop that is initializing us. Groups loaded from
	// disk pass nil to skip this cleanup - they are legitimate groups
	// that should not self-destruct if the first post-restart request
	// happens to fail validation.
	var firstJoin func(bool)
	if detachNew == nil {
		firstJoin = func(bool) {}
	} else {
		firstJoin = func(ok bool) {
			firstJoin = func(bool) {}
			if !ok {
				delete(g.gs.gs, g.name)
				g.quitOnce()
			}
			detachNew()
		}
	}

	defer func() {
		for _, m := range g.members {
			if m.t != nil {
				m.t.Stop()
			}
		}
		for _, m := range g.pending {
			if m.t != nil {
				m.t.Stop()
			}
		}
		for _, m := range g.consumerMembers {
			if m.t != nil {
				m.t.Stop()
			}
		}
	}()

	for {
		select {
		case <-g.quitCh:
			return
		case <-g.c.die:
			return
		case creq := <-g.reqCh:
			kresp, ok := g.dispatchReq(creq)
			firstJoin(ok)
			if kresp != nil {
				g.reply(creq, kresp, nil)
			}

		case fn := <-g.controlCh:
			fn()
		}
	}
}

// dispatchReq handles a single request from reqCh. Returns the response
// and whether the request was valid (for firstJoin tracking). Used by
// manage() and by drainReqCh during shutdown.
func (g *group) dispatchReq(creq *clientReq) (kmsg.Response, bool) {
	switch creq.kreq.(type) {
	case *kmsg.JoinGroupRequest:
		return g.handleJoin(creq)
	case *kmsg.SyncGroupRequest:
		return g.handleSync(creq), true
	case *kmsg.HeartbeatRequest:
		return g.handleHeartbeat(creq), true
	case *kmsg.LeaveGroupRequest:
		return g.handleLeave(creq), true
	case *kmsg.OffsetCommitRequest:
		var resp *kmsg.OffsetCommitResponse
		var ok bool
		if g.typ == "consumer" {
			resp, ok = g.handleConsumerOffsetCommit(creq), true
		} else {
			resp, ok = g.handleOffsetCommit(creq)
		}
		if resp != nil && len(creq.offsetCommitErrTopics) > 0 {
			resp.Topics = append(resp.Topics, creq.offsetCommitErrTopics...)
		}
		return resp, ok
	case *kmsg.OffsetDeleteRequest:
		return g.handleOffsetDelete(creq), true
	case *kmsg.ConsumerGroupHeartbeatRequest:
		g.lastTopicMeta = creq.topicMeta
		return g.handleConsumerHeartbeat(creq), true
	}
	return nil, true
}

// drainReqCh processes any pending requests in reqCh. Called from a
// waitControl closure during shutdown to ensure committed offsets from
// in-flight OffsetCommit requests are captured before snapshotting.
// Must run in the manage goroutine (via waitControl).
func (g *group) drainReqCh() {
	for {
		select {
		case creq := <-g.reqCh:
			kresp, _ := g.dispatchReq(creq)
			if kresp != nil {
				g.reply(creq, kresp, nil)
			}
		default:
			return
		}
	}
}

// The group manage loop does not block: it sends to respCh which eventually
// writes; but that write is fast. There is no long-blocking code in the manage
// loop.
func (g *group) waitControl(fn func()) bool {
	return waitManageControl(g.controlCh, g.quitCh, g.c, fn)
}

// waitManageControl sends fn to a manage goroutine's controlCh and blocks
// until it completes. Used by group.waitControl and shareGroup.waitControl.
//
// This is a free function (not a method) because group and shareGroup are
// separate types that both need this logic. They share controlCh/quitCh/c
// fields but don't share a common embedded struct, so we pass the channels
// explicitly to avoid duplicating the deadlock-avoidance logic.
//
// Drains adminCh while waiting to avoid deadlock: the pids manage loop may
// call c.admin() (e.g. transaction timeout abort) while we're blocked sending
// to controlCh or waiting for the function to complete.
func waitManageControl(controlCh chan func(), quitCh chan struct{}, c *Cluster, fn func()) bool {
	wait := make(chan struct{})
	wfn := func() { fn(); close(wait) }
	for {
		select {
		case <-quitCh:
			return false
		case <-c.die:
			return false
		case controlCh <- wfn:
			goto sent
		case admin := <-c.adminCh:
			admin()
		}
	}
sent:
	// Once sent, the manage goroutine will run fn synchronously.
	// We must not select on quitCh here: fn itself may call
	// quitOnce (e.g. deleting an empty group), closing quitCh
	// before close(wait) executes. If the scheduler preempts
	// between the two closes, a quitCh select case would see
	// quitCh ready but wait not yet closed and incorrectly
	// return false.
	for {
		select {
		case <-wait:
			return true
		case <-c.die:
			return false
		case admin := <-c.adminCh:
			admin()
		}
	}
}

// Called in the manage loop.
func (g *group) quitOnce() {
	g.quit.Do(func() {
		g.state = groupDead
		close(g.quitCh)
	})
}

// Handles a join. We do not do the delayed join aspects in Kafka, we just punt
// to the client to immediately rejoin if a new client enters the group.
//
// If this returns nil, the request will be replied to later.
func (g *group) handleJoin(creq *clientReq) (kmsg.Response, bool) {
	req := creq.kreq.(*kmsg.JoinGroupRequest)
	resp := req.ResponseKind().(*kmsg.JoinGroupResponse)

	if kerr := g.c.validateGroup(creq, req.Group); kerr != nil {
		resp.ErrorCode = kerr.Code
		return resp, false
	}
	if !g.c.allowedACL(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		resp.ErrorCode = kerr.GroupAuthorizationFailed.Code
		return resp, false
	}
	if st := req.SessionTimeoutMillis; st < g.c.groupMinSessionTimeoutMs() || st > g.c.groupMaxSessionTimeoutMs() {
		resp.ErrorCode = kerr.InvalidSessionTimeout.Code
		return resp, false
	}
	if !g.protocolsMatch(req.ProtocolType, req.Protocols) {
		resp.ErrorCode = kerr.InconsistentGroupProtocol.Code
		return resp, false
	}

	// Clients first join with no member ID. For join v4+, we generate
	// the member ID and add the member to pending. For v3 and below,
	// we immediately enter rebalance. Static members (instanceID set)
	// may rejoin with an empty memberID - check the static mapping.
	if req.MemberID == "" {
		if req.InstanceID != nil {
			if oldMemberID, ok := g.staticMembers[*req.InstanceID]; ok {
				return g.replaceStaticMember(oldMemberID, creq, req, resp)
			}
		}
		if int32(len(g.members)+len(g.pending)) >= g.c.groupMaxSize() {
			resp.ErrorCode = kerr.GroupMaxSizeReached.Code
			return resp, true
		}
		memberID := generateMemberID(creq.cid, req.InstanceID)
		resp.MemberID = memberID
		m := &groupMember{
			memberID:   memberID,
			instanceID: req.InstanceID,
			clientID:   creq.cid,
			clientHost: creq.cc.conn.RemoteAddr().String(),
			join:       req,
		}
		if req.InstanceID != nil {
			g.staticMembers[*req.InstanceID] = memberID
			g.persistStaticMember(*req.InstanceID, memberID)
		}
		// Java's requiresKnownMemberId gate only triggers for DYNAMIC
		// members; static members (groupInstanceId != null) are
		// accepted with empty MemberID on v4+ JoinGroup and join
		// immediately.
		if req.Version >= 4 && req.InstanceID == nil {
			g.addPendingRebalance(m)
			resp.ErrorCode = kerr.MemberIDRequired.Code
			return resp, true
		}
		g.addMemberAndRebalance(m, creq, req)
		return nil, true
	}

	// Validate instanceID consistency for known members.
	if req.InstanceID != nil {
		if err := g.validateInstanceID(req.InstanceID, req.MemberID); err != nil {
			resp.ErrorCode = err.Code
			return resp, false
		}
	}

	// Pending members rejoining immediately enters rebalance.
	if m, ok := g.pending[req.MemberID]; ok {
		g.addMemberAndRebalance(m, creq, req)
		return nil, true
	}
	m, ok := g.members[req.MemberID]
	if !ok {
		resp.ErrorCode = kerr.UnknownMemberID.Code
		return resp, false
	}

	switch g.state {
	default:
		resp.ErrorCode = kerr.UnknownMemberID.Code
		return resp, false
	case groupPreparingRebalance:
		g.updateMemberAndRebalance(m, creq, req)
	case groupCompletingRebalance:
		if m.sameJoin(req) {
			g.fillJoinResp(m.memberID, resp)
			return resp, true
		}
		g.updateMemberAndRebalance(m, creq, req)
	case groupStable:
		if g.leader != req.MemberID && m.sameJoin(req) {
			// Non-leader with same metadata - no change needed
			g.fillJoinResp(m.memberID, resp)
			return resp, true
		}
		// Leader rejoining OR any member with changed metadata: trigger rebalance
		g.updateMemberAndRebalance(m, creq, req)
	}
	return nil, true
}

// replaceStaticMember handles a static member rejoining with the same
// instanceID. If the protocol is unchanged, the member is not the leader,
// and the group is stable, we skip the rebalance (KIP-345).
func (g *group) replaceStaticMember(oldMemberID string, creq *clientReq, req *kmsg.JoinGroupRequest, resp *kmsg.JoinGroupResponse) (kmsg.Response, bool) {
	old, ok := g.members[oldMemberID]
	if !ok {
		// Old member was pending or already removed - treat as new join.
		delete(g.staticMembers, *req.InstanceID)
		memberID := generateMemberID(creq.cid, req.InstanceID)
		resp.MemberID = memberID
		m := &groupMember{
			memberID:   memberID,
			instanceID: req.InstanceID,
			clientID:   creq.cid,
			clientHost: creq.cc.conn.RemoteAddr().String(),
			join:       req,
		}
		g.staticMembers[*req.InstanceID] = memberID
		g.persistStaticMember(*req.InstanceID, memberID)
		if p, ok := g.pending[oldMemberID]; ok {
			g.stopPending(p)
		}
		if req.Version >= 4 {
			g.addPendingRebalance(m)
			resp.ErrorCode = kerr.MemberIDRequired.Code
			return resp, true
		}
		g.addMemberAndRebalance(m, creq, req)
		return nil, true
	}

	// Generate a new memberID for the replacement.
	memberID := generateMemberID(creq.cid, req.InstanceID)

	// Create the new member, preserving the old assignment.
	m := &groupMember{
		memberID:   memberID,
		instanceID: req.InstanceID,
		clientID:   creq.cid,
		clientHost: creq.cc.conn.RemoteAddr().String(),
		join:       req,
		assignment: old.assignment,
	}

	// Fence the old member: if it's waiting for a reply, send FENCED_INSTANCE_ID.
	if !old.waitingReply.empty() {
		oldResp := old.waitingReply.kreq.ResponseKind()
		switch r := oldResp.(type) {
		case *kmsg.JoinGroupResponse:
			r.ErrorCode = kerr.FencedInstanceID.Code
		case *kmsg.SyncGroupResponse:
			r.ErrorCode = kerr.FencedInstanceID.Code
		}
		g.reply(old.waitingReply, oldResp, nil)
	}

	// Remove old member state without triggering rebalance.
	g.removeMember(old)

	// Register new member in static mapping.
	g.staticMembers[*req.InstanceID] = memberID
	g.persistStaticMember(*req.InstanceID, memberID)
	g.members[memberID] = m
	for _, p := range m.join.Protocols {
		g.protocols[p.Name]++
	}

	// If protocol unchanged and group is stable: skip rebalance (KIP-345).
	// For the leader, set SkipAssignment so it knows to re-send the
	// existing assignment without running the balancer (KIP-814).
	if g.state == groupStable && old.sameJoin(req) {
		if g.leader == oldMemberID {
			g.leader = memberID
		}
		g.updateHeartbeat(m)
		g.fillJoinResp(memberID, resp)
		if resp.LeaderID == memberID {
			resp.SkipAssignment = true
		}
		return resp, true
	}

	// Otherwise trigger a rebalance.
	g.nJoining++
	m.waitingReply = creq
	g.rebalance()
	return nil, true
}

// Handles a sync, which can transition us to stable.
func (g *group) handleSync(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.SyncGroupRequest)
	resp := req.ResponseKind().(*kmsg.SyncGroupResponse)

	if kerr := g.c.validateGroup(creq, req.Group); kerr != nil {
		resp.ErrorCode = kerr.Code
		return resp
	}
	if !g.c.allowedACL(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		resp.ErrorCode = kerr.GroupAuthorizationFailed.Code
		return resp
	}
	if err := g.validateInstanceID(req.InstanceID, req.MemberID); err != nil {
		resp.ErrorCode = err.Code
		return resp
	}
	m, ok := g.members[req.MemberID]
	if !ok {
		resp.ErrorCode = kerr.UnknownMemberID.Code
		return resp
	}
	if req.Generation != g.generation {
		resp.ErrorCode = kerr.IllegalGeneration.Code
		return resp
	}
	if req.ProtocolType != nil && *req.ProtocolType != g.protocolType {
		resp.ErrorCode = kerr.InconsistentGroupProtocol.Code
		return resp
	}
	if req.Protocol != nil && *req.Protocol != g.protocol {
		resp.ErrorCode = kerr.InconsistentGroupProtocol.Code
		return resp
	}

	switch g.state {
	default:
		resp.ErrorCode = kerr.UnknownMemberID.Code
	case groupPreparingRebalance:
		resp.ErrorCode = kerr.RebalanceInProgress.Code
	case groupCompletingRebalance:
		delete(g.pendingSyncIDs, req.MemberID)
		m.waitingReply = creq
		if req.MemberID == g.leader {
			g.completeLeaderSync(req)
		}
		return nil
	case groupStable: // member saw join and is now finally calling sync
		resp.ProtocolType = kmsg.StringPtr(g.protocolType)
		resp.Protocol = kmsg.StringPtr(g.protocol)
		resp.MemberAssignment = m.assignment
	}
	return resp
}

// Handles a heartbeat, a relatively simple request that just delays our
// session timeout timer.
func (g *group) handleHeartbeat(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.HeartbeatRequest)
	resp := req.ResponseKind().(*kmsg.HeartbeatResponse)

	if kerr := g.c.validateGroup(creq, req.Group); kerr != nil {
		resp.ErrorCode = kerr.Code
		return resp
	}
	if !g.c.allowedACL(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		resp.ErrorCode = kerr.GroupAuthorizationFailed.Code
		return resp
	}
	if err := g.validateInstanceID(req.InstanceID, req.MemberID); err != nil {
		resp.ErrorCode = err.Code
		return resp
	}
	m, ok := g.members[req.MemberID]
	if !ok {
		resp.ErrorCode = kerr.UnknownMemberID.Code
		return resp
	}
	if req.Generation != g.generation {
		resp.ErrorCode = kerr.IllegalGeneration.Code
		return resp
	}

	switch g.state {
	default:
		resp.ErrorCode = kerr.UnknownMemberID.Code
	case groupPreparingRebalance:
		resp.ErrorCode = kerr.RebalanceInProgress.Code
		g.updateHeartbeat(m)
	case groupCompletingRebalance, groupStable:
		g.updateHeartbeat(m)
	}
	return resp
}

// Handles a leave. We trigger a rebalance for every member leaving in a batch
// request, but that's fine because of our manage serialization.
func (g *group) handleLeave(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.LeaveGroupRequest)
	resp := req.ResponseKind().(*kmsg.LeaveGroupResponse)

	if kerr := g.c.validateGroup(creq, req.Group); kerr != nil {
		resp.ErrorCode = kerr.Code
		return resp
	}
	if !g.c.allowedACL(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		resp.ErrorCode = kerr.GroupAuthorizationFailed.Code
		return resp
	}
	if req.Version < 3 {
		req.Members = append(req.Members, kmsg.LeaveGroupRequestMember{
			MemberID: req.MemberID,
		})
		defer func() { resp.ErrorCode = resp.Members[0].ErrorCode }()
	}

	for _, rm := range req.Members {
		mresp := kmsg.NewLeaveGroupResponseMember()
		mresp.MemberID = rm.MemberID
		mresp.InstanceID = rm.InstanceID
		resp.Members = append(resp.Members, mresp)

		r := &resp.Members[len(resp.Members)-1]

		// Resolve memberID from instanceID for static members.
		if rm.InstanceID != nil {
			memberID, ok := g.staticMembers[*rm.InstanceID]
			if !ok {
				r.ErrorCode = kerr.UnknownMemberID.Code
				continue
			}
			rm.MemberID = memberID
			r.MemberID = memberID
		}

		if m, ok := g.members[rm.MemberID]; !ok {
			if p, ok := g.pending[rm.MemberID]; !ok {
				r.ErrorCode = kerr.UnknownMemberID.Code
			} else {
				g.stopPending(p)
			}
		} else {
			g.updateMemberAndRebalance(m, nil, nil)
		}
	}

	return resp
}

func fillOffsetCommit(req *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, code int16) {
	for _, t := range req.Topics {
		st := kmsg.NewOffsetCommitResponseTopic()
		st.Topic = t.Topic
		st.TopicID = t.TopicID
		for _, p := range t.Partitions {
			sp := kmsg.NewOffsetCommitResponseTopicPartition()
			sp.Partition = p.Partition
			sp.ErrorCode = code
			st.Partitions = append(st.Partitions, sp)
		}
		resp.Topics = append(resp.Topics, st)
	}
}

func setOffsetCommitPartitionErr(resp *kmsg.OffsetCommitResponse, topic string, partition int32, code int16) {
	for i := range resp.Topics {
		if resp.Topics[i].Topic == topic {
			for j := range resp.Topics[i].Partitions {
				if resp.Topics[i].Partitions[j].Partition == partition {
					resp.Topics[i].Partitions[j].ErrorCode = code
					return
				}
			}
		}
	}
}

// fillOffsetCommitWithACL fills the response with per-topic ACL checks.
// Returns topics that passed ACL check.
func (g *group) fillOffsetCommitWithACL(creq *clientReq, req *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse) []kmsg.OffsetCommitRequestTopic {
	var allowed []kmsg.OffsetCommitRequestTopic
	for _, t := range req.Topics {
		st := kmsg.NewOffsetCommitResponseTopic()
		st.Topic = t.Topic
		st.TopicID = t.TopicID
		if !g.c.allowedACL(creq, t.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead) {
			for _, p := range t.Partitions {
				sp := kmsg.NewOffsetCommitResponseTopicPartition()
				sp.Partition = p.Partition
				sp.ErrorCode = kerr.TopicAuthorizationFailed.Code
				st.Partitions = append(st.Partitions, sp)
			}
		} else {
			allowed = append(allowed, t)
			for _, p := range t.Partitions {
				sp := kmsg.NewOffsetCommitResponseTopicPartition()
				sp.Partition = p.Partition
				sp.ErrorCode = 0
				st.Partitions = append(st.Partitions, sp)
			}
		}
		resp.Topics = append(resp.Topics, st)
	}
	return allowed
}

// Handles a commit.
func (g *group) handleOffsetCommit(creq *clientReq) (*kmsg.OffsetCommitResponse, bool) {
	req := creq.kreq.(*kmsg.OffsetCommitRequest)
	resp := req.ResponseKind().(*kmsg.OffsetCommitResponse)

	if kerr := g.c.validateGroup(creq, req.Group); kerr != nil {
		fillOffsetCommit(req, resp, kerr.Code)
		return resp, false
	}

	// ACL check: READ on GROUP (if denied, fail all topics)
	if !g.c.allowedACL(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		fillOffsetCommit(req, resp, kerr.GroupAuthorizationFailed.Code)
		return resp, false
	}

	if err := g.validateInstanceID(req.InstanceID, req.MemberID); err != nil {
		fillOffsetCommit(req, resp, err.Code)
		return resp, false
	}

	var m *groupMember
	if len(g.members) > 0 {
		var ok bool
		m, ok = g.members[req.MemberID]
		if !ok {
			fillOffsetCommit(req, resp, kerr.UnknownMemberID.Code)
			return resp, false
		}
		if req.Generation != g.generation {
			fillOffsetCommit(req, resp, kerr.IllegalGeneration.Code)
			return resp, false
		}
	} else if req.Generation >= 0 {
		// Empty group: only accept simple commits (generation < 0).
		fillOffsetCommit(req, resp, kerr.IllegalGeneration.Code)
		return resp, false
	}

	switch g.state {
	default:
		fillOffsetCommit(req, resp, kerr.GroupIDNotFound.Code)
		return resp, true
	case groupEmpty, groupPreparingRebalance, groupStable:
		allowed := g.fillOffsetCommitWithACL(creq, req, resp)
		for _, t := range allowed {
			for _, p := range t.Partitions {
				g.commitAndPersist(t.Topic, p.Partition, offsetCommit{
					offset:      p.Offset,
					leaderEpoch: p.LeaderEpoch,
					metadata:    p.Metadata,
				})
			}
		}
		if m != nil {
			g.updateHeartbeat(m)
		}
	case groupCompletingRebalance:
		fillOffsetCommit(req, resp, kerr.RebalanceInProgress.Code)
		g.updateHeartbeat(m)
	}
	return resp, true
}

// Transitions the group to the preparing rebalance state. We first need to
// clear any member that is currently sitting in sync. If enough members have
// entered join, we immediately proceed to completeRebalance, otherwise we
// begin a wait timer.
func (g *group) rebalance() {
	if g.state == groupCompletingRebalance {
		for _, m := range g.members {
			m.assignment = nil
			if m.waitingReply.empty() {
				continue
			}
			sync, ok := m.waitingReply.kreq.(*kmsg.SyncGroupRequest)
			if !ok {
				continue
			}
			resp := sync.ResponseKind().(*kmsg.SyncGroupResponse)
			resp.ErrorCode = kerr.RebalanceInProgress.Code
			g.reply(m.waitingReply, resp, m)
		}
	}

	g.state = groupPreparingRebalance
	g.clearPendingSync()

	if g.nJoining >= len(g.members) {
		g.completeRebalance()
		return
	}

	// Always reset the timer with the current max rebalance timeout.
	// A subsequent rebalance() call may have a different timeout.
	if g.tRebalance != nil {
		g.tRebalance.Stop()
	}
	g.tRebalance = g.timerControlFn(time.Duration(g.maxRebalanceTimeoutMs())*time.Millisecond, g.completeRebalance)
}

// Transitions the group to either dead or stable, depending on if any members
// remain by the time we clear those that are not waiting in join.
func (g *group) completeRebalance() {
	if g.tRebalance != nil {
		g.tRebalance.Stop()
		g.tRebalance = nil
	}
	g.nJoining = 0

	var foundLeader bool
	for _, m := range g.members {
		if m.waitingReply.empty() {
			g.removeMember(m)
			continue
		}
		if m.memberID == g.leader {
			foundLeader = true
		}
	}

	g.generation++
	if g.generation < 0 {
		g.generation = 1
	}
	if len(g.members) == 0 {
		g.state = groupEmpty
		g.emptyAt = time.Now()
		g.persistClassicMeta()
		return
	}
	g.state = groupCompletingRebalance

	// Kafka-style protocol voting: find candidate protocols
	// (supported by all members), then each member votes for
	// their most-preferred candidate. The protocol with the
	// most votes wins.
	candidates := make(map[string]struct{})
	for proto, nsupport := range g.protocols {
		if nsupport == len(g.members) {
			candidates[proto] = struct{}{}
		}
	}
	if len(candidates) == 0 {
		panic(fmt.Sprint("unable to find commonly supported protocol!", g.protocols, len(g.members)))
	}
	votes := make(map[string]int, len(candidates))
	for _, m := range g.members {
		for _, p := range m.join.Protocols {
			if _, ok := candidates[p.Name]; ok {
				votes[p.Name]++
				break
			}
		}
	}
	bestProto, bestVotes := "", 0
	for proto, v := range votes {
		if v > bestVotes {
			bestProto = proto
			bestVotes = v
		}
	}
	g.protocol = bestProto
	g.persistClassicMeta()

	// Track which members need to send SyncGroup.
	g.pendingSyncIDs = make(map[string]struct{}, len(g.members))
	for _, m := range g.members {
		if !foundLeader {
			g.leader = m.memberID
		}
		g.pendingSyncIDs[m.memberID] = struct{}{}
		req := m.join
		resp := req.ResponseKind().(*kmsg.JoinGroupResponse)
		g.fillJoinResp(m.memberID, resp)
		g.reply(m.waitingReply, resp, m)
	}

	// Start pending sync timeout: remove members that don't send
	// SyncGroup within the rebalance timeout.
	if g.tPendingSync != nil {
		g.tPendingSync.Stop()
	}
	g.tPendingSync = g.timerControlFn(time.Duration(g.maxRebalanceTimeoutMs())*time.Millisecond, func() {
		if len(g.pendingSyncIDs) == 0 {
			return
		}
		// Remove all pending members, then trigger one rebalance.
		for id := range g.pendingSyncIDs {
			if m := g.members[id]; m != nil {
				g.removeMember(m)
			}
		}
		g.pendingSyncIDs = nil
		g.rebalance()
	})
}

// Transitions the group to stable, the final step of a rebalance.
func (g *group) completeLeaderSync(req *kmsg.SyncGroupRequest) {
	for _, m := range g.members {
		m.assignment = nil
	}
	for _, a := range req.GroupAssignment {
		m, ok := g.members[a.MemberID]
		if !ok {
			continue
		}
		m.assignment = g.assignmentOrEmpty(a.MemberAssignment)
	}
	for _, m := range g.members {
		if m.waitingReply.empty() {
			continue
		}
		resp := m.waitingReply.kreq.ResponseKind().(*kmsg.SyncGroupResponse)
		resp.ProtocolType = kmsg.StringPtr(g.protocolType)
		resp.Protocol = kmsg.StringPtr(g.protocol)
		resp.MemberAssignment = m.assignment
		g.reply(m.waitingReply, resp, m)
	}
	g.state = groupStable
	g.clearPendingSync()
}

func (g *group) clearPendingSync() {
	g.pendingSyncIDs = nil
	if g.tPendingSync != nil {
		g.tPendingSync.Stop()
		g.tPendingSync = nil
	}
}

// assignmentOrEmpty returns the assignment bytes, or a pre-serialized empty
// ConsumerMemberAssignment if the assignment is empty and this is a "consumer"
// protocol group. This ensures followers always receive a decodable assignment.
func (g *group) assignmentOrEmpty(assignment []byte) []byte {
	if len(assignment) == 0 && g.protocolType == "consumer" {
		return append([]byte(nil), emptyConsumerAssignment...)
	}
	return assignment
}

func (g *group) updateHeartbeat(m *groupMember) {
	g.atSessionTimeout(m, func() {
		g.updateMemberAndRebalance(m, nil, nil)
	})
}

func (g *group) addPendingRebalance(m *groupMember) {
	g.pending[m.memberID] = m
	g.atSessionTimeout(m, func() {
		delete(g.pending, m.memberID)
	})
}

func (g *group) stopPending(m *groupMember) {
	delete(g.pending, m.memberID)
	if m.t != nil {
		m.t.Stop()
	}
}

func (g *group) maxRebalanceTimeoutMs() int32 {
	var max int32
	for _, m := range g.members {
		if m.join.RebalanceTimeoutMillis > max {
			max = m.join.RebalanceTimeoutMillis
		}
	}
	return max
}

// timerControlFn starts a timer that, on expiry, sends fn to the
// group's control channel. If the group is shutting down, the send
// is abandoned.
func (g *group) timerControlFn(d time.Duration, fn func()) *time.Timer {
	return time.AfterFunc(d, func() {
		select {
		case <-g.quitCh:
		case <-g.c.die:
		case g.controlCh <- fn:
		}
	})
}

func (g *group) atSessionTimeout(m *groupMember, fn func()) {
	g.atSessionTimeoutIn(m, time.Millisecond*time.Duration(m.join.SessionTimeoutMillis), fn)
}

// removeMember removes a member from the group, cleaning up its protocol
// counts, session timer, join counter, and static membership. Does not
// trigger a rebalance.
func (g *group) removeMember(m *groupMember) {
	for _, p := range m.join.Protocols {
		g.protocols[p.Name]--
	}
	delete(g.members, m.memberID)
	if m.instanceID != nil {
		delete(g.staticMembers, *m.instanceID)
		g.persistStaticMember(*m.instanceID, "")
	}
	if m.t != nil {
		m.t.Stop()
	}
	if !m.waitingReply.empty() {
		g.nJoining--
	}
}

// validateInstanceID checks that the given instanceID and memberID are
// consistent with the group's static membership state. Returns nil on
// success or a kerr error.
func (g *group) validateInstanceID(instanceID *string, memberID string) *kerr.Error {
	if instanceID == nil {
		return nil
	}
	knownMID, ok := g.staticMembers[*instanceID]
	if !ok {
		return kerr.UnknownMemberID
	}
	if knownMID != memberID {
		return kerr.FencedInstanceID
	}
	return nil
}

// This is used to update a member from a new join request, or to clear a
// member from failed heartbeats.
func (g *group) updateMemberAndRebalance(m *groupMember, waitingReply *clientReq, newJoin *kmsg.JoinGroupRequest) {
	if newJoin != nil {
		for _, p := range m.join.Protocols {
			g.protocols[p.Name]--
		}
		m.join = newJoin
		for _, p := range m.join.Protocols {
			g.protocols[p.Name]++
		}
		if m.waitingReply.empty() && !waitingReply.empty() {
			g.nJoining++
		}
		m.waitingReply = waitingReply
	} else {
		g.removeMember(m)
	}
	g.rebalance()
}

// Adds a new member to the group and rebalances.
func (g *group) addMemberAndRebalance(m *groupMember, waitingReply *clientReq, join *kmsg.JoinGroupRequest) {
	g.stopPending(m)
	m.join = join
	for _, p := range m.join.Protocols {
		g.protocols[p.Name]++
	}
	g.members[m.memberID] = m
	g.nJoining++
	m.waitingReply = waitingReply
	g.rebalance()
}

// Returns if a new join can even join the group based on the join's supported
// protocols.
func (g *group) protocolsMatch(protocolType string, protocols []kmsg.JoinGroupRequestProtocol) bool {
	if g.protocolType == "" {
		if protocolType == "" || len(protocols) == 0 {
			return false
		}
		g.protocolType = protocolType
		return true
	}
	if protocolType != g.protocolType {
		return false
	}
	if len(g.protocols) == 0 {
		return true
	}
	// The joining member must support at least one protocol that
	// is universally supported by all existing members.
	for _, p := range protocols {
		if g.protocols[p.Name] == len(g.members) {
			return true
		}
	}
	return false
}

// Returns if a new join request is the same as an old request; if so, for
// non-leaders, we just return the old join response.
func (m *groupMember) sameJoin(req *kmsg.JoinGroupRequest) bool {
	if len(m.join.Protocols) != len(req.Protocols) {
		return false
	}
	for i := range m.join.Protocols {
		if m.join.Protocols[i].Name != req.Protocols[i].Name {
			return false
		}
		if !bytes.Equal(m.join.Protocols[i].Metadata, req.Protocols[i].Metadata) {
			return false
		}
	}
	return true
}

func (g *group) fillJoinResp(memberID string, resp *kmsg.JoinGroupResponse) {
	resp.Generation = g.generation
	resp.ProtocolType = kmsg.StringPtr(g.protocolType)
	resp.Protocol = kmsg.StringPtr(g.protocol)
	resp.LeaderID = g.leader
	resp.MemberID = memberID
	if g.leader == memberID {
		resp.Members = g.joinResponseMetadata()
	}
}

func (g *group) joinResponseMetadata() []kmsg.JoinGroupResponseMember {
	metadata := make([]kmsg.JoinGroupResponseMember, 0, len(g.members))
members:
	for _, m := range g.members {
		for _, p := range m.join.Protocols {
			if p.Name == g.protocol {
				metadata = append(metadata, kmsg.JoinGroupResponseMember{
					MemberID:         m.memberID,
					InstanceID:       m.instanceID,
					ProtocolMetadata: p.Metadata,
				})
				continue members
			}
		}
		panic("inconsistent group protocol within saved members")
	}
	return metadata
}

// reply sends kresp back to the caller of creq. If m is non-nil,
// m.waitingReply is cleared before the send attempt so a dead
// connection does not leave a stale reply behind.
func (g *group) reply(creq *clientReq, kresp kmsg.Response, m *groupMember) {
	if m != nil {
		m.waitingReply = nil
	}
	select {
	case creq.cc.respCh <- clientResp{kresp: kresp, corr: creq.corr, seq: creq.seq}:
	case <-creq.cc.done:
		return
	case <-g.c.die:
		return
	}
	if m != nil {
		g.updateHeartbeat(m)
	}
}

///////////////////////////
// KIP-848 CONSUMER GROUPS
///////////////////////////

// Hijacks the consumer group heartbeat request into the group's manage
// goroutine. We snapshot topic metadata here (running in Cluster.run,
// safe access to c.data) so that computeAssignment does not need to
// call c.admin(), which would deadlock.
func (gs *groups) handleConsumerGroupHeartbeat(creq *clientReq) {
	if gs.gs == nil {
		gs.gs = make(map[string]*group)
	}
	req := creq.kreq.(*kmsg.ConsumerGroupHeartbeatRequest)

	// Group type exclusivity: if this group ID is already a share
	// group, reject the consumer group heartbeat.
	if _, isShare := gs.c.shareGroups.gs[req.Group]; isShare {
		resp := req.ResponseKind().(*kmsg.ConsumerGroupHeartbeatResponse)
		resp.ErrorCode = kerr.GroupIDNotFound.Code
		creq.cc.respCh <- clientResp{kresp: resp, corr: creq.corr, seq: creq.seq}
		return
	}

start:
	g := gs.gs[req.Group]
	if g == nil {
		g = gs.newGroup(req.Group)
		gs.gs[req.Group] = g
		go g.manage(func() {})
	}
	creq.topicMeta = gs.c.snapshotTopicMeta()
	select {
	case g.reqCh <- creq:
	case <-g.quitCh:
		// Group quit while we were dispatching. Replace the dead
		// group with a fresh one and retry so the heartbeat is
		// handled normally (the new group will return
		// UNKNOWN_MEMBER_ID for the stale member).
		delete(gs.gs, req.Group)
		goto start
	case <-g.c.die:
	}
}

func (gs *groups) handleConsumerGroupDescribe(creq *clientReq) *kmsg.ConsumerGroupDescribeResponse {
	req := creq.kreq.(*kmsg.ConsumerGroupDescribeRequest)
	resp := req.ResponseKind().(*kmsg.ConsumerGroupDescribeResponse)

	doneg := func(name string) *kmsg.ConsumerGroupDescribeResponseGroup {
		sg := kmsg.NewConsumerGroupDescribeResponseGroup()
		sg.Group = name
		sg.AuthorizedOperations = math.MinInt32
		resp.Groups = append(resp.Groups, sg)
		return &resp.Groups[len(resp.Groups)-1]
	}

	for _, rg := range req.Groups {
		sg := doneg(rg)
		if !gs.c.allowedACL(creq, rg, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDescribe) {
			sg.ErrorCode = kerr.GroupAuthorizationFailed.Code
			continue
		}
		if kerr := gs.c.validateGroup(creq, rg); kerr != nil {
			sg.ErrorCode = kerr.Code
			continue
		}
		g, ok := gs.gs[rg]
		if !ok {
			sg.ErrorCode = kerr.GroupIDNotFound.Code
			if req.IncludeAuthorizedOperations {
				sg.AuthorizedOperations = gs.c.groupAuthorizedOps(creq, rg)
			}
			continue
		}
		if !g.waitControl(func() {
			if g.typ != "consumer" {
				sg.ErrorCode = kerr.GroupIDNotFound.Code
				if req.IncludeAuthorizedOperations {
					sg.AuthorizedOperations = gs.c.groupAuthorizedOps(creq, rg)
				}
				return
			}
			sg.State = g.state.String()
			sg.Epoch = g.groupEpoch
			sg.AssignmentEpoch = g.targetAssignmentEpoch
			sg.AssignorName = g.assignorName
			for _, m := range g.consumerMembers {
				sm := kmsg.NewConsumerGroupDescribeResponseGroupMember()
				sm.MemberID = m.memberID
				sm.InstanceID = m.instanceID
				sm.RackID = m.rackID
				sm.MemberEpoch = m.memberEpoch
				sm.ClientID = m.clientID
				sm.ClientHost = m.clientHost
				sm.SubscribedTopics = m.subscribedTopics
				sm.MemberType = 1 // consumer
				sm.Assignment = uuidAssignmentToKmsg(m.lastReconciledSent)
				sm.TargetAssignment = uuidAssignmentToKmsg(m.targetAssignment)
				sg.Members = append(sg.Members, sm)
			}
			if req.IncludeAuthorizedOperations {
				sg.AuthorizedOperations = gs.c.groupAuthorizedOps(creq, rg)
			}
		}) {
			sg.ErrorCode = kerr.GroupIDNotFound.Code
		}
	}
	return resp
}

func uuidAssignmentToKmsg(a map[uuid][]int32) kmsg.Assignment {
	var ka kmsg.Assignment
	for id, parts := range a {
		tp := kmsg.NewAssignmentTopicPartition()
		tp.TopicID = id
		tp.Partitions = parts
		ka.TopicPartitions = append(ka.TopicPartitions, tp)
	}
	return ka
}

// A "full request" carries subscription and assignment info. Keepalive
// heartbeats set RebalanceTimeoutMillis to -1 and leave all optional
// fields nil; the server should skip subscription processing and omit
// assignment from the response unless the member is still reconciling.
// Kafka requires ALL three conditions: timeout present, subscription
// present, and owned partitions present.
func isFullRequest(req *kmsg.ConsumerGroupHeartbeatRequest) bool {
	return req.RebalanceTimeoutMillis != -1 &&
		(req.SubscribedTopicNames != nil || req.SubscribedTopicRegex != nil) &&
		req.Topics != nil
}

// Handles a KIP-848 consumer group heartbeat, routing to join, leave, or
// regular heartbeat based on MemberEpoch.
func (g *group) handleConsumerHeartbeat(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.ConsumerGroupHeartbeatRequest)
	resp := req.ResponseKind().(*kmsg.ConsumerGroupHeartbeatResponse)
	resp.HeartbeatIntervalMillis = g.c.consumerHeartbeatIntervalMs()

	if kerr := g.c.validateGroup(creq, req.Group); kerr != nil {
		resp.ErrorCode = kerr.Code
		return resp
	}
	if !g.c.allowedACL(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		resp.ErrorCode = kerr.GroupAuthorizationFailed.Code
		return resp
	}

	// Groups start as "classic" (the default in newGroup). The first
	// ConsumerGroupHeartbeat upgrades the group to "consumer",
	// unless classic members are already active.
	if g.typ == "classic" {
		if len(g.members) > 0 {
			resp.ErrorCode = kerr.GroupIDNotFound.Code
			return resp
		}
		g.typ = "consumer"
		g.consumerMembers = make(map[string]*consumerMember)
		g.partitionEpochs = make(map[uuid]map[int32]int32)
	}

	switch req.MemberEpoch {
	case 0:
		return g.consumerJoin(creq, req, resp)
	case -1:
		return g.consumerLeave(req, resp)
	case -2:
		return g.consumerStaticLeave(req, resp)
	default:
		return g.consumerRegularHeartbeat(req, resp)
	}
}

func (g *group) consumerJoin(creq *clientReq, req *kmsg.ConsumerGroupHeartbeatRequest, resp *kmsg.ConsumerGroupHeartbeatResponse) *kmsg.ConsumerGroupHeartbeatResponse {
	memberID := req.MemberID
	if memberID == "" {
		memberID = generateMemberID(creq.cid, req.InstanceID)
	}

	// Check group max size for truly new members (not rejoins).
	isRejoin := g.consumerMembers[memberID] != nil
	if req.InstanceID != nil {
		if oldMID, ok := g.staticMembers[*req.InstanceID]; ok {
			isRejoin = isRejoin || oldMID == memberID || g.consumerMembers[oldMID] != nil
		}
	}
	if !isRejoin && int32(len(g.consumerMembers)) >= g.c.groupMaxSize() {
		resp.ErrorCode = kerr.GroupMaxSizeReached.Code
		return resp
	}

	// Static member: if this instanceID maps to a different member,
	// the old member must have sent a static leave (epoch -2) before
	// replacement is allowed. If the old member is still active, we
	// return UNRELEASED_INSTANCE_ID per Kafka's
	// throwIfInstanceIdIsUnreleased (GroupMetadataManager.java:3163).
	var inheritSent, inheritTarget map[uuid][]int32
	var inheritAssignEpochs map[uuid]map[int32]int32
	var oldTopics []string
	if req.InstanceID != nil {
		if oldMemberID, ok := g.staticMembers[*req.InstanceID]; ok && oldMemberID != memberID {
			if old := g.consumerMembers[oldMemberID]; old != nil {
				if old.memberEpoch != -2 {
					resp.ErrorCode = kerr.UnreleasedInstanceID.Code
					return resp
				}
				oldTopics = old.subscribedTopics
				inheritSent = copyAssignment(old.lastReconciledSent)
				inheritTarget = copyAssignment(old.targetAssignment)
				inheritAssignEpochs = old.partAssignmentEpochs
				g.fenceConsumerMember(old)
				delete(g.consumerMembers, oldMemberID)
			}
		}
		g.staticMembers[*req.InstanceID] = memberID
	}

	// Same-memberID: handle static rejoin from epoch -2 or
	// non-static rejoin (update in place).
	if prev := g.consumerMembers[memberID]; prev != nil {
		if prev.memberEpoch != -2 {
			// Active non-static rejoin: update subscription
			// in place, preserving partition state.
			return g.consumerRejoin(creq, req, resp, prev)
		}
		if inheritSent == nil {
			oldTopics = prev.subscribedTopics
			inheritSent = copyAssignment(prev.lastReconciledSent)
			inheritTarget = copyAssignment(prev.targetAssignment)
			inheritAssignEpochs = prev.partAssignmentEpochs
		}
		g.fenceConsumerMember(prev)
		delete(g.consumerMembers, memberID)
	}

	m := &consumerMember{
		memberID:                    memberID,
		clientID:                    creq.cid,
		clientHost:                  creq.cc.conn.RemoteAddr().String(),
		instanceID:                  req.InstanceID,
		rackID:                      req.RackID,
		rebalanceTimeoutMs:          req.RebalanceTimeoutMillis,
		targetAssignment:            make(map[uuid][]int32),
		partitionsPendingRevocation: make(map[uuid][]int32),
	}
	if req.ServerAssignor != nil {
		if !validServerAssignor(*req.ServerAssignor) {
			resp.ErrorCode = kerr.UnsupportedAssignor.Code
			return resp
		}
		m.serverAssignor = *req.ServerAssignor
	}
	if req.SubscribedTopicNames != nil {
		m.subscribedTopics = slices.Clone(req.SubscribedTopicNames)
		slices.Sort(m.subscribedTopics)
	}
	if req.SubscribedTopicRegex != nil {
		re, err := regexp.Compile(*req.SubscribedTopicRegex)
		if err != nil {
			resp.ErrorCode = kerr.InvalidRequest.Code
			return resp
		}
		m.subscribedTopicRegex = re
		m.subscribedRegexSource = *req.SubscribedTopicRegex
	}
	if m.rebalanceTimeoutMs <= 0 {
		m.rebalanceTimeoutMs = 45000
	}

	// Inherit state from epoch -2 static member so the sticky
	// assignor preserves the partition distribution and the
	// reconciliation sees the correct owned partitions.
	if inheritSent != nil {
		m.lastReconciledSent = inheritSent
		m.targetAssignment = inheritTarget
		m.partAssignmentEpochs = inheritAssignEpochs
	}

	g.consumerMembers[memberID] = m
	g.assignorName = m.serverAssignor

	// Bump groupEpoch when: group is new (epoch 0), truly new
	// member (no prior static member), or subscription changed.
	if g.groupEpoch == 0 || oldTopics == nil || !slices.Equal(oldTopics, m.subscribedTopics) {
		g.groupEpoch++
		g.computeTargetAssignment(g.lastTopicMeta)
		g.persistMeta848()
	}
	if m.instanceID != nil {
		g.persistStaticMember(*m.instanceID, memberID)
	}

	// Compute initial assignment for this new member.
	newAssigned, newPending, _ := g.computeNextAssignment(m, nil)
	m.lastReconciledSent = newAssigned
	m.partitionsPendingRevocation = newPending

	// Add to epoch map so other members can't claim these
	// partitions until this member releases them. Old state
	// is empty (new member or fenced rejoin).
	g.updateMemberEpochs(m, nil, nil, 0)

	g.updateConsumerStateField()
	g.atConsumerSessionTimeout(m)

	resp.MemberID = &memberID
	resp.MemberEpoch = m.memberEpoch
	g.c.cfg.logger.Logf(LogLevelDebug, "RECONCILE SEND: member=%s epoch=%d partitions=%d", m.memberID, m.memberEpoch, countAssignment(m.lastReconciledSent))
	resp.Assignment = makeAssignment(m.lastReconciledSent)
	g.c.cfg.logger.Logf(LogLevelInfo, "consumerJoin: group=%s member=%s epoch=%d sent=%d epochMap=%d members=%d",
		g.logName(), m.memberID, m.memberEpoch, countAssignment(m.lastReconciledSent), g.countPartitionEpochs(), len(g.consumerMembers))
	m.updatePartAssignmentEpochs(m.lastReconciledSent)
	if len(m.partitionsPendingRevocation) > 0 {
		g.scheduleConsumerRebalanceTimeout(m)
	}
	return resp
}

// updateMemberSubscriptions processes subscription and assignor changes
// from a heartbeat request. Returns whether the subscription changed
// and an error code (0 on success).
func (g *group) updateMemberSubscriptions(m *consumerMember, req *kmsg.ConsumerGroupHeartbeatRequest) (changed bool, errCode int16) {
	if req.SubscribedTopicNames != nil {
		sorted := slices.Clone(req.SubscribedTopicNames)
		slices.Sort(sorted)
		if !slices.Equal(m.subscribedTopics, sorted) {
			m.subscribedTopics = sorted
			changed = true
		}
	}
	if req.SubscribedTopicRegex != nil && *req.SubscribedTopicRegex != m.subscribedRegexSource {
		re, err := regexp.Compile(*req.SubscribedTopicRegex)
		if err != nil {
			return false, kerr.InvalidRequest.Code
		}
		m.subscribedTopicRegex = re
		m.subscribedRegexSource = *req.SubscribedTopicRegex
		changed = true
	}
	if req.ServerAssignor != nil && *req.ServerAssignor != m.serverAssignor {
		if !validServerAssignor(*req.ServerAssignor) {
			return false, kerr.UnsupportedAssignor.Code
		}
		m.serverAssignor = *req.ServerAssignor
		g.assignorName = m.serverAssignor
		changed = true
	}
	return changed, 0
}

// consumerRejoin handles a non-static rejoin: an active member sends
// epoch 0 to update its subscription without losing partition state.
// Updates the member in place and runs reconciliation.
func (g *group) consumerRejoin(creq *clientReq, req *kmsg.ConsumerGroupHeartbeatRequest, resp *kmsg.ConsumerGroupHeartbeatResponse, m *consumerMember) *kmsg.ConsumerGroupHeartbeatResponse {
	m.clientID = creq.cid
	m.clientHost = creq.cc.conn.RemoteAddr().String()
	m.rackID = req.RackID
	if req.RebalanceTimeoutMillis > 0 {
		m.rebalanceTimeoutMs = req.RebalanceTimeoutMillis
	}

	hasSubscriptionChanged, errCode := g.updateMemberSubscriptions(m, req)
	if errCode != 0 {
		resp.ErrorCode = errCode
		return resp
	}
	if hasSubscriptionChanged {
		g.groupEpoch++
		g.computeTargetAssignment(g.lastTopicMeta)
		g.persistMeta848()
	}

	g.atConsumerSessionTimeout(m)
	resp.MemberID = &m.memberID

	g.maybeReconcile(m, hasSubscriptionChanged, req.Topics)

	g.c.cfg.logger.Logf(LogLevelDebug, "RECONCILE SEND: member=%s epoch=%d partitions=%d (rejoin)", m.memberID, m.memberEpoch, countAssignment(m.lastReconciledSent))
	resp.Assignment = makeAssignment(m.lastReconciledSent)
	resp.MemberEpoch = m.memberEpoch
	g.updateConsumerStateField()
	if len(m.partitionsPendingRevocation) > 0 {
		g.scheduleConsumerRebalanceTimeout(m)
	} else {
		g.cancelConsumerRebalanceTimeout(m)
	}
	g.c.cfg.logger.Logf(LogLevelInfo, "consumerJoin: group=%s member=%s epoch=%d sent=%d epochMap=%d members=%d (rejoin)",
		g.logName(), m.memberID, m.memberEpoch, countAssignment(m.lastReconciledSent), g.countPartitionEpochs(), len(g.consumerMembers))
	return resp
}

func (g *group) consumerLeave(req *kmsg.ConsumerGroupHeartbeatRequest, resp *kmsg.ConsumerGroupHeartbeatResponse) *kmsg.ConsumerGroupHeartbeatResponse {
	m, ok := g.consumerMembers[req.MemberID]
	if !ok {
		resp.ErrorCode = kerr.UnknownMemberID.Code
		return resp
	}
	g.c.cfg.logger.Logf(LogLevelInfo, "consumerLeave: group=%s member=%s epoch=%d sent=%d pending=%d remaining=%d",
		g.logName(), m.memberID, m.memberEpoch, countAssignment(m.lastReconciledSent), countAssignment(m.partitionsPendingRevocation), len(g.consumerMembers)-1)
	g.fenceConsumerMember(m)
	delete(g.consumerMembers, req.MemberID)
	// Full leave (-1): remove from static membership too.
	if m.instanceID != nil {
		delete(g.staticMembers, *m.instanceID)
		g.persistStaticMember(*m.instanceID, "")
	}

	g.groupEpoch++
	g.computeTargetAssignment(g.lastTopicMeta)
	g.updateConsumerStateField()
	g.persistMeta848()

	resp.MemberID = &req.MemberID
	resp.MemberEpoch = -1
	return resp
}

// consumerStaticLeave handles epoch -2: static member leave. Matches
// Kafka's consumerGroupStaticMemberGroupLeave - the member is NOT
// removed from consumerMembers. Instead it stays with epoch -2 so its
// partition epoch entries remain alive (preventing other members from
// claiming those partitions). On rejoin, the old member is fenced and
// the new member inherits the assignment.
func (g *group) consumerStaticLeave(req *kmsg.ConsumerGroupHeartbeatRequest, resp *kmsg.ConsumerGroupHeartbeatResponse) *kmsg.ConsumerGroupHeartbeatResponse {
	m, ok := g.consumerMembers[req.MemberID]
	if !ok {
		resp.ErrorCode = kerr.UnknownMemberID.Code
		return resp
	}
	if m.instanceID == nil {
		// Dynamic member sent MemberEpoch=-2. Java's
		// GroupMetadataManager silently fences and removes the
		// member rather than returning UnreleasedInstanceID. Fall
		// through to the same path consumerLeave uses for full
		// leave (MemberEpoch=-1).
		g.c.cfg.logger.Logf(LogLevelInfo, "consumerStaticLeave: dynamic member sent epoch=-2; treating as full leave; group=%s member=%s",
			g.logName(), m.memberID)
		g.fenceConsumerMember(m)
		delete(g.consumerMembers, req.MemberID)
		g.groupEpoch++
		g.computeTargetAssignment(g.lastTopicMeta)
		g.updateConsumerStateField()
		g.persistMeta848()
		return resp
	}
	g.c.cfg.logger.Logf(LogLevelInfo, "consumerStaticLeave: group=%s member=%s instance=%s epoch=%d sent=%d remaining=%d",
		g.logName(), m.memberID, *m.instanceID, m.memberEpoch, countAssignment(m.lastReconciledSent), g.activeConsumerCount()-1)

	// Mark as temporarily departed - do NOT fence, do NOT
	// delete from consumerMembers, do NOT bump generation.
	//
	// Re-stamp epoch map: remove entries at the current epoch,
	// free pending revocation partitions, and add sent entries
	// at -2. This keeps lastReconciledSent partitions reserved
	// while allowing fenceConsumerMember (which uses
	// m.memberEpoch for epoch-matching) to clean up on rejoin.
	oldEpoch := m.memberEpoch
	g.removePartitionEpochs(m.lastReconciledSent, oldEpoch)
	g.removePartitionEpochs(m.partitionsPendingRevocation, oldEpoch)
	m.partitionsPendingRevocation = make(map[uuid][]int32)
	m.memberEpoch = -2
	g.addPartitionEpochs(m.lastReconciledSent, -2)
	if m.t != nil {
		m.t.Stop()
		m.t = nil
	}
	g.cancelConsumerRebalanceTimeout(m)
	// Keep g.staticMembers[*m.instanceID] intact.

	g.updateConsumerStateField()

	resp.MemberID = &req.MemberID
	resp.MemberEpoch = -2
	return resp
}

func (g *group) consumerRegularHeartbeat(req *kmsg.ConsumerGroupHeartbeatRequest, resp *kmsg.ConsumerGroupHeartbeatResponse) *kmsg.ConsumerGroupHeartbeatResponse {
	m, ok := g.consumerMembers[req.MemberID]
	if !ok {
		// If the request carries an instanceID owned by a different
		// member, return FENCED_INSTANCE_ID per Kafka's
		// throwIfInstanceIdIsFenced (GroupMetadataManager.java:1647).
		if req.InstanceID != nil {
			if ownerID, ok := g.staticMembers[*req.InstanceID]; ok && ownerID != req.MemberID {
				resp.ErrorCode = kerr.FencedInstanceID.Code
				return resp
			}
		}
		resp.ErrorCode = kerr.UnknownMemberID.Code
		return resp
	}

	full := isFullRequest(req)

	// Previous-epoch recovery: accept the member's previous epoch if
	// the reported partitions are a subset of the server's
	// authoritative assigned set (lastReconciledSent).
	if req.MemberEpoch != m.memberEpoch {
		matchesPrev := req.MemberEpoch == m.previousMemberEpoch
		topicsNil := req.Topics == nil
		isSubset := false
		if !topicsNil {
			isSubset = isSubsetAssignment(req.Topics, m.lastReconciledSent)
		}
		if !matchesPrev || topicsNil || !isSubset {
			resp.ErrorCode = kerr.FencedMemberEpoch.Code
			return resp
		}
	}

	if gap := time.Since(m.last); gap > 2*time.Duration(g.c.consumerHeartbeatIntervalMs())*time.Millisecond {
		g.c.cfg.logger.Logf(LogLevelWarn, "consumerHeartbeatLate: group=%s member=%s gap=%dms interval=%dms",
			g.logName(), m.memberID, gap.Milliseconds(), g.c.consumerHeartbeatIntervalMs())
	}
	g.atConsumerSessionTimeout(m)

	// Process subscription changes.
	var hasSubscriptionChanged bool
	if full {
		var errCode int16
		hasSubscriptionChanged, errCode = g.updateMemberSubscriptions(m, req)
		if errCode != 0 {
			resp.ErrorCode = errCode
			return resp
		}
	}

	if hasSubscriptionChanged {
		g.groupEpoch++
		g.computeTargetAssignment(g.lastTopicMeta)
		g.persistMeta848()
	}
	resp.MemberID = &req.MemberID

	changed := g.maybeReconcile(m, hasSubscriptionChanged, req.Topics)

	// Send the assignment in the response on full requests
	// or when the reconciled set changed.
	if full || changed {
		g.c.cfg.logger.Logf(LogLevelDebug, "RECONCILE SEND: member=%s epoch=%d partitions=%d",
			m.memberID, m.memberEpoch, countAssignment(m.lastReconciledSent))
		resp.Assignment = makeAssignment(m.lastReconciledSent)
	}

	resp.MemberEpoch = m.memberEpoch

	g.updateConsumerStateField()

	// Schedule or cancel the rebalance timeout: active only when
	// the member has partitions pending revocation. Always
	// reschedule (not just on first entry) so the timeout
	// tracks the latest revocation state.
	if len(m.partitionsPendingRevocation) > 0 {
		g.scheduleConsumerRebalanceTimeout(m)
	} else {
		g.cancelConsumerRebalanceTimeout(m)
	}

	return resp
}

// validServerAssignor returns whether the given assignor name is
// supported for consumer groups. Only "uniform" and "range" are valid
// ("simple" is for share groups only - KIP-932).
func validServerAssignor(name string) bool {
	return name == "uniform" || name == "range"
}

type assignorTP struct {
	topic string
	id    uuid
	part  int32
}

// computeTargetAssignment resolves subscriptions against the topic
// metadata snapshot and dispatches to the appropriate assignor based on
// g.assignorName. Updates targetAssignment on each consumerMember.
func (g *group) computeTargetAssignment(snap topicMetaSnap) {
	memberSubs := make(map[string]map[string]struct{}, len(g.consumerMembers))
	var allTPs []assignorTP

	subscribedSet := make(map[string]struct{})
	for mid, m := range g.consumerMembers {
		// Skip members with epoch -2 (static leave) - they are
		// "away" and should not receive new partitions, but their
		// existing assignment stays reserved via partition epochs.
		if m.memberEpoch == -2 {
			continue
		}
		subs := make(map[string]struct{}, len(m.subscribedTopics))
		for _, t := range m.subscribedTopics {
			subs[t] = struct{}{}
		}
		if m.subscribedTopicRegex != nil {
			for topic := range snap {
				if m.subscribedTopicRegex.MatchString(topic) {
					subs[topic] = struct{}{}
				}
			}
		}
		memberSubs[mid] = subs
		for t := range subs {
			subscribedSet[t] = struct{}{}
		}
	}
	for topic := range subscribedSet {
		info, ok := snap[topic]
		if !ok {
			continue
		}
		for p := int32(0); p < info.partitions; p++ {
			allTPs = append(allTPs, assignorTP{topic: topic, id: info.id, part: p})
		}
	}

	// Sort deterministically: by topic name, then partition.
	slices.SortFunc(allTPs, func(a, b assignorTP) int {
		if c := cmp.Compare(a.topic, b.topic); c != 0 {
			return c
		}
		return cmp.Compare(a.part, b.part)
	})

	// Sort members deterministically. For the range assignor, static
	// members (sorted by instanceID) come before dynamic members
	// (sorted by memberID), matching Kafka's behavior. Epoch -2
	// members are excluded (skipped in memberSubs above).
	memberIDs := slices.Collect(maps.Keys(memberSubs))
	if g.assignorName == "range" {
		slices.SortFunc(memberIDs, func(a, b string) int {
			ma, mb := g.consumerMembers[a], g.consumerMembers[b]
			aStatic := ma.instanceID != nil
			bStatic := mb.instanceID != nil
			if aStatic != bStatic {
				if aStatic {
					return -1
				}
				return 1
			}
			if aStatic {
				return cmp.Compare(*ma.instanceID, *mb.instanceID)
			}
			return cmp.Compare(a, b)
		})
	} else {
		slices.Sort(memberIDs)
	}

	if len(memberIDs) == 0 {
		return
	}

	switch g.assignorName {
	case "range":
		// Range is position-based, not sticky - clear and recompute.
		for _, mid := range memberIDs {
			g.consumerMembers[mid].targetAssignment = make(map[uuid][]int32)
		}
		g.assignRange(allTPs, memberIDs, memberSubs)
	default: // "uniform" or "" (pre-assignor groups) - validated at heartbeat time
		// assignUniform is sticky: it preserves existing targets
		// and only reassigns partitions that need to move.
		g.assignUniform(allTPs, memberIDs, memberSubs)
	}

	// Sort partition lists for determinism.
	for _, mid := range memberIDs {
		m := g.consumerMembers[mid]
		for id := range m.targetAssignment {
			slices.Sort(m.targetAssignment[id])
		}
	}

	// Epoch map is NOT rebuilt here; it is managed
	// incrementally per-member:
	// - consumerJoin: updateMemberEpochs for new member
	// - maybeReconcile: updateMemberEpochs on assignment change
	// - epoch bump: updateMemberEpochs re-stamps at new epoch
	// - fenceConsumerMember: removePartitionEpochs

	g.targetAssignmentEpoch = g.groupEpoch
}

// assignUniform distributes partitions across all eligible members using
// a sticky algorithm: existing assignments are preserved when valid, and
// only unassigned partitions are redistributed. This minimizes partition
// movement during cooperative rebalances (KIP-848), where each moved
// partition requires multiple heartbeat rounds to revoke and reassign.
func (g *group) assignUniform(allTPs []assignorTP, memberIDs []string, memberSubs map[string]map[string]struct{}) {
	type tpKey struct {
		id   uuid
		part int32
	}
	allTPSet := make(map[tpKey]string, len(allTPs))
	for _, tp := range allTPs {
		allTPSet[tpKey{tp.id, tp.part}] = tp.topic
	}

	// Step 1: keep existing assignments that are still valid (member
	// exists, subscribes to the topic, partition still exists).
	assigned := make(map[tpKey]bool, len(allTPs))
	counts := make(map[string]int, len(memberIDs))
	for _, mid := range memberIDs {
		m := g.consumerMembers[mid]
		for id, parts := range m.targetAssignment {
			var kept []int32
			for _, p := range parts {
				k := tpKey{id, p}
				topic, exists := allTPSet[k]
				if !exists {
					continue
				}
				if _, ok := memberSubs[mid][topic]; !ok {
					continue
				}
				kept = append(kept, p)
				assigned[k] = true
			}
			if len(kept) > 0 {
				m.targetAssignment[id] = kept
			} else {
				delete(m.targetAssignment, id)
			}
		}
		for _, parts := range m.targetAssignment {
			counts[mid] += len(parts)
		}
	}

	// Step 2: balance - members with too many partitions shed
	// excess. With N total and M members, each gets floor(N/M)
	// or floor(N/M)+1.
	minCount := len(allTPs) / len(memberIDs)
	extra := len(allTPs) % len(memberIDs)

	// Determine the allowed max per member. Sort members by
	// current count descending - the top `extra` members are
	// allowed minCount+1, the rest get minCount. This minimizes
	// movement by letting the most-loaded members keep an extra
	// partition when possible.
	type memberCount struct {
		mid   string
		count int
	}
	sorted := make([]memberCount, len(memberIDs))
	for i, mid := range memberIDs {
		sorted[i] = memberCount{mid, counts[mid]}
	}
	slices.SortFunc(sorted, func(a, b memberCount) int {
		if c := cmp.Compare(b.count, a.count); c != 0 {
			return c // descending
		}
		return cmp.Compare(a.mid, b.mid) // tie-break by member ID
	})
	allowed := make(map[string]int, len(memberIDs))
	for i, mc := range sorted {
		if i < extra {
			allowed[mc.mid] = minCount + 1
		} else {
			allowed[mc.mid] = minCount
		}
	}

	// Shed excess from overloaded members. Only update
	// m.targetAssignment and remove from assigned; step 3
	// collects all unassigned partitions in one pass.
	for _, mid := range memberIDs {
		m := g.consumerMembers[mid]
		excess := counts[mid] - allowed[mid]
		if excess <= 0 {
			continue
		}
		// Remove excess partitions. Iterate topic IDs in sorted
		// order so the choice is deterministic.
		ids := slices.Collect(maps.Keys(m.targetAssignment))
		slices.SortFunc(ids, func(a, b uuid) int { return bytes.Compare(a[:], b[:]) })
		for _, id := range ids {
			if excess <= 0 {
				break
			}
			parts := m.targetAssignment[id]
			remove := min(excess, len(parts))
			for _, p := range parts[len(parts)-remove:] {
				delete(assigned, tpKey{id, p})
			}
			m.targetAssignment[id] = parts[:len(parts)-remove]
			if len(m.targetAssignment[id]) == 0 {
				delete(m.targetAssignment, id)
			}
			counts[mid] -= remove
			excess -= remove
		}
	}

	// Step 3: collect all unassigned partitions - from members
	// that left, partitions never assigned, and excess shed above.
	var unassigned []assignorTP
	for _, tp := range allTPs {
		if !assigned[tpKey{tp.id, tp.part}] {
			unassigned = append(unassigned, tp)
		}
	}
	if len(unassigned) == 0 {
		return
	}

	// Step 4: distribute unassigned partitions to members with
	// the fewest partitions. unassigned preserves allTPs order
	// (already sorted by topic, partition).
	for _, tp := range unassigned {
		bestMid := ""
		bestCount := math.MaxInt
		for _, mid := range memberIDs {
			if _, ok := memberSubs[mid][tp.topic]; !ok {
				continue
			}
			if counts[mid] < bestCount {
				bestMid = mid
				bestCount = counts[mid]
			}
		}
		if bestMid == "" {
			continue
		}
		m := g.consumerMembers[bestMid]
		m.targetAssignment[tp.id] = append(m.targetAssignment[tp.id], tp.part)
		counts[bestMid]++
	}
}

// assignRange distributes contiguous partition ranges per topic. For
// each topic, members subscribed to that topic (in sorted order) get
// a contiguous block. If partitions don't divide evenly, the first
// members get one extra partition.
func (g *group) assignRange(allTPs []assignorTP, memberIDs []string, memberSubs map[string]map[string]struct{}) {
	// Group TPs by topic. allTPs is sorted by (topic, partition),
	// so partitions for each topic are contiguous.
	type topicSlice struct {
		topic      string
		partitions []assignorTP
	}
	var topics []topicSlice
	for i, tp := range allTPs {
		if i == 0 || tp.topic != allTPs[i-1].topic {
			topics = append(topics, topicSlice{topic: tp.topic})
		}
		topics[len(topics)-1].partitions = append(topics[len(topics)-1].partitions, tp)
	}

	for _, ts := range topics {
		topic := ts.topic
		partitions := ts.partitions

		// Filter to members subscribed to this topic, preserving
		// the sorted order from memberIDs.
		var subs []string
		for _, mid := range memberIDs {
			if _, ok := memberSubs[mid][topic]; ok {
				subs = append(subs, mid)
			}
		}
		if len(subs) == 0 {
			continue
		}

		numP := len(partitions)
		numM := len(subs)
		minQuota := numP / numM
		extra := numP % numM
		nextRange := 0

		for _, mid := range subs {
			quota := minQuota
			if extra > 0 {
				quota++
				extra--
			}
			m := g.consumerMembers[mid]
			for _, tp := range partitions[nextRange : nextRange+quota] {
				m.targetAssignment[tp.id] = append(m.targetAssignment[tp.id], tp.part)
			}
			nextRange += quota
		}
	}
}

// computeNextAssignment computes the next assignment and pending
// revocations for a member in a single pass (matching Kafka's
// CurrentAssignmentBuilder). Processes lastReconciledSent against
// targetAssignment, computing keep/revoke/assign sets:
//
//	a) Has revocations and client owns them - UNREVOKED_PARTITIONS
//	b) Has new partitions to assign - advance epoch, assign them
//	c) Has unreleased partitions only - advance epoch, wait
//	d) Stable - advance epoch, no change needed
//
// Sets m.state, m.memberEpoch, and m.previousMemberEpoch. Returns
// the new assigned and pending maps plus whether the assignment
// changed.
func (g *group) computeNextAssignment(m *consumerMember,
	ownedTopicPartitions []kmsg.ConsumerGroupHeartbeatRequestTopic,
) (
	newAssigned, newPending map[uuid][]int32, changed bool,
) {
	// Collect all topic IDs from both maps.
	allTopics := make(map[uuid]struct{})
	for id := range m.lastReconciledSent {
		allTopics[id] = struct{}{}
	}
	for id := range m.targetAssignment {
		allTopics[id] = struct{}{}
	}

	keep := make(map[uuid][]int32)
	pendingRevocation := make(map[uuid][]int32)
	pendingAssignment := make(map[uuid][]int32)
	hasUnreleasedPartitions := false
	var blockedCount int

	for id := range allTopics {
		sent := m.lastReconciledSent[id]
		target := m.targetAssignment[id]

		// keep = sent intersect target; revoke = sent - target.
		for _, p := range sent {
			if slices.Contains(target, p) {
				keep[id] = append(keep[id], p)
			} else {
				pendingRevocation[id] = append(pendingRevocation[id], p)
			}
		}

		// pendingAssignment = target - sent, filtered by epoch.
		for _, p := range target {
			if slices.Contains(sent, p) {
				continue // already in keep
			}
			epoch := g.currentPartitionEpoch(id, p)
			if epoch == -1 || slices.Contains(m.partitionsPendingRevocation[id], p) {
				pendingAssignment[id] = append(pendingAssignment[id], p)
			} else {
				hasUnreleasedPartitions = true
				blockedCount++
			}
		}
	}

	// Merge existing pending revocations (from previous rounds,
	// not yet confirmed by client) with newly computed ones.
	allPendingRevocation := copyAssignment(m.partitionsPendingRevocation)
	for id, parts := range pendingRevocation {
		for _, p := range parts {
			if !slices.Contains(allPendingRevocation[id], p) {
				allPendingRevocation[id] = append(allPendingRevocation[id], p)
			}
		}
	}

	hasAnyPendingRevocation := len(allPendingRevocation) > 0
	hasPendingAssignment := len(pendingAssignment) > 0

	switch {
	case hasAnyPendingRevocation && ownsRevokedPartitions(allPendingRevocation, ownedTopicPartitions):
		// Branch a: client still owns revoked partitions.
		m.state = cmUnrevokedPartitions
		// Epoch stays unchanged.
		newAssigned = keep
		newPending = allPendingRevocation
		g.c.cfg.logger.Logf(LogLevelDebug, "RECONCILE REVOKE-FIRST: member=%s reconciled=%d sent=%d target=%d pending=%d",
			m.memberID, countAssignment(keep), countAssignment(m.lastReconciledSent), countAssignment(m.targetAssignment), countAssignment(allPendingRevocation))

	case hasPendingAssignment:
		// Branch b: new partitions to assign.
		if hasUnreleasedPartitions {
			m.state = cmUnreleasedPartitions
		} else {
			m.state = cmStable
		}
		m.previousMemberEpoch = m.memberEpoch
		m.memberEpoch = g.targetAssignmentEpoch
		newAssigned = make(map[uuid][]int32, len(keep)+len(pendingAssignment))
		maps.Copy(newAssigned, keep)
		for id, parts := range pendingAssignment {
			newAssigned[id] = append(newAssigned[id], parts...)
		}
		newPending = make(map[uuid][]int32)
		g.c.cfg.logger.Logf(LogLevelDebug, "RECONCILE SUMMARY: member=%s reconciled=%d target=%d +%d blocked=%d",
			m.memberID, countAssignment(newAssigned), countAssignment(m.targetAssignment), countAssignment(pendingAssignment), blockedCount)

	default:
		// Branches c+d: no new partitions.
		if hasUnreleasedPartitions {
			m.state = cmUnreleasedPartitions
			g.c.cfg.logger.Logf(LogLevelDebug, "RECONCILE SUMMARY: member=%s reconciled=%d target=%d +%d blocked=%d",
				m.memberID, countAssignment(keep), countAssignment(m.targetAssignment), 0, blockedCount)
		} else {
			m.state = cmStable
		}
		m.previousMemberEpoch = m.memberEpoch
		m.memberEpoch = g.targetAssignmentEpoch
		newAssigned = keep
		newPending = make(map[uuid][]int32)
	}

	changed = !maps.EqualFunc(m.lastReconciledSent, newAssigned, slices.Equal) || !maps.EqualFunc(m.partitionsPendingRevocation, newPending, slices.Equal)

	return newAssigned, newPending, changed
}

// updateCurrentAssignment handles subscription changes for a member
// that is waiting for revocations. It only removes partitions for
// unsubscribed topics from lastReconciledSent.
func (g *group) updateCurrentAssignment(m *consumerMember,
	ownedTopicPartitions []kmsg.ConsumerGroupHeartbeatRequestTopic,
) (
	newAssigned, newPending map[uuid][]int32, changed bool,
) {
	// Compute subscribed topic IDs from subscriptions + regex.
	subscribedIDs := make(map[uuid]struct{})
	snap := g.lastTopicMeta
	for _, topic := range m.subscribedTopics {
		if info, ok := snap[topic]; ok {
			subscribedIDs[info.id] = struct{}{}
		}
	}
	if m.subscribedTopicRegex != nil {
		for topic, info := range snap {
			if m.subscribedTopicRegex.MatchString(topic) {
				subscribedIDs[info.id] = struct{}{}
			}
		}
	}

	// Remove unsubscribed topics from lastReconciledSent.
	pendingRevocation := make(map[uuid][]int32)
	newAssigned = make(map[uuid][]int32, len(m.lastReconciledSent))
	for id, parts := range m.lastReconciledSent {
		if _, ok := subscribedIDs[id]; ok {
			newAssigned[id] = parts
		} else {
			pendingRevocation[id] = parts
		}
	}

	if len(pendingRevocation) == 0 {
		return nil, nil, false
	}

	// Merge with existing pending.
	newPending = copyAssignment(m.partitionsPendingRevocation)
	for id, parts := range pendingRevocation {
		newPending[id] = append(newPending[id], parts...)
	}

	if ownsRevokedPartitions(newPending, ownedTopicPartitions) {
		m.state = cmUnrevokedPartitions
	}
	// Epoch doesn't change for subscription-only updates.

	changed = true
	return newAssigned, newPending, changed
}

// clearConfirmedRevocations removes partitions from
// m.partitionsPendingRevocation that are no longer reported in
// ownedTopicPartitions (the client confirmed releasing them).
// Updates the partition epoch map accordingly.
func (g *group) clearConfirmedRevocations(m *consumerMember,
	ownedTopicPartitions []kmsg.ConsumerGroupHeartbeatRequestTopic,
) {
	reported := topicsToAssignment(ownedTopicPartitions)

	oldPending := m.partitionsPendingRevocation
	newPending := make(map[uuid][]int32)
	for id, parts := range oldPending {
		for _, p := range parts {
			if slices.Contains(reported[id], p) {
				newPending[id] = append(newPending[id], p)
			}
		}
	}
	m.partitionsPendingRevocation = newPending

	if cleared := countAssignment(oldPending) - countAssignment(newPending); cleared > 0 {
		g.c.cfg.logger.Logf(LogLevelDebug, "pendingRevoke cleared: member=%s epoch=%d cleared=%d kept=%d",
			m.memberID, m.memberEpoch, cleared, countAssignment(newPending))
	}

	// Update epoch map: remove old pending, add new.
	g.removePartitionEpochs(oldPending, m.memberEpoch)
	g.addPartitionEpochs(m.partitionsPendingRevocation, m.memberEpoch)
}

// maybeReconcile dispatches to the appropriate reconciliation path
// based on the member's current state.
func (g *group) maybeReconcile(m *consumerMember, hasSubscriptionChanged bool,
	ownedTopicPartitions []kmsg.ConsumerGroupHeartbeatRequestTopic,
) bool {
	// Clear confirmed revocations first.
	if ownedTopicPartitions != nil {
		g.clearConfirmedRevocations(m, ownedTopicPartitions)
	}

	// Early exit if nothing to do.
	if !hasSubscriptionChanged && m.state == cmStable && m.memberEpoch == g.targetAssignmentEpoch {
		return false
	}

	// Save state before computation for epoch map update.
	oldSent := m.lastReconciledSent
	oldPending := m.partitionsPendingRevocation
	oldEpoch := m.memberEpoch

	var newAssigned, newPending map[uuid][]int32
	var changed bool

	switch m.state {
	case cmStable:
		if m.memberEpoch != g.targetAssignmentEpoch {
			newAssigned, newPending, changed = g.computeNextAssignment(m, ownedTopicPartitions)
		} else if hasSubscriptionChanged {
			newAssigned, newPending, changed = g.updateCurrentAssignment(m, ownedTopicPartitions)
		}

	case cmUnrevokedPartitions:
		if ownsRevokedPartitions(m.partitionsPendingRevocation, ownedTopicPartitions) {
			if hasSubscriptionChanged {
				newAssigned, newPending, changed = g.updateCurrentAssignment(m, ownedTopicPartitions)
			}
		} else {
			newAssigned, newPending, changed = g.computeNextAssignment(m, ownedTopicPartitions)
		}

	case cmUnreleasedPartitions:
		newAssigned, newPending, changed = g.computeNextAssignment(m, ownedTopicPartitions)
	}

	if changed {
		m.lastReconciledSent = newAssigned
		m.partitionsPendingRevocation = newPending
		g.updateMemberEpochs(m, oldSent, oldPending, oldEpoch)
		m.updatePartAssignmentEpochs(newAssigned)
	} else if m.memberEpoch != oldEpoch {
		// Epoch advanced but assignment didn't change.
		// Re-stamp epoch map entries at the new epoch.
		g.updateMemberEpochs(m, oldSent, oldPending, oldEpoch)
	}

	return changed
}

// Builds the response assignment from a partition map.
func makeAssignment(assigned map[uuid][]int32) *kmsg.ConsumerGroupHeartbeatResponseAssignment {
	a := new(kmsg.ConsumerGroupHeartbeatResponseAssignment)
	for id, parts := range assigned {
		t := kmsg.NewConsumerGroupHeartbeatResponseAssignmentTopic()
		t.TopicID = id
		t.Partitions = parts
		a.Topics = append(a.Topics, t)
	}
	return a
}

// Updates the consumer group state based on member reconciliation status.
// Skips epoch -2 members (static leave) - they are temporarily departed.
func (g *group) updateConsumerStateField() {
	var active int
	for _, m := range g.consumerMembers {
		if m.memberEpoch == -2 {
			continue
		}
		active++
		if m.state != cmStable || m.memberEpoch != g.targetAssignmentEpoch {
			g.state = groupReconciling
			return
		}
	}
	if active == 0 {
		g.state = groupEmpty
		return
	}
	g.state = groupStable
}

func (g *group) meta848Entry() groupLogEntry {
	return groupLogEntry{
		Type:       "meta848",
		Group:      g.name,
		GroupType:  g.typ,
		Assignor:   g.assignorName,
		GroupEpoch: g.groupEpoch,
	}
}

func (g *group) persistMeta848() { g.c.persistGroupEntry(g.meta848Entry()) }

func (g *group) classicMetaEntry() groupLogEntry {
	return groupLogEntry{
		Type:       "meta",
		Group:      g.name,
		GroupType:  g.typ,
		ProtoType:  g.protocolType,
		Protocol:   g.protocol,
		Generation: g.generation,
	}
}

func (g *group) persistClassicMeta() { g.c.persistGroupEntry(g.classicMetaEntry()) }

func (g *group) commitEntry(topic string, part int32, oc offsetCommit) groupLogEntry {
	entry := groupLogEntry{
		Type:     "commit",
		Group:    g.name,
		Topic:    topic,
		Part:     part,
		Offset:   oc.offset,
		Epoch:    oc.leaderEpoch,
		Metadata: oc.metadata,
	}
	if !oc.lastCommit.IsZero() {
		ms := oc.lastCommit.UnixMilli()
		entry.LastCommit = &ms
	}
	return entry
}

func (g *group) commitAndPersist(topic string, part int32, oc offsetCommit) {
	oc.lastCommit = time.Now()
	g.commits.set(topic, part, oc)
	g.c.persistGroupEntry(g.commitEntry(topic, part, oc))
}

func (g *group) deleteCommitAndPersist(topic string, part int32) {
	g.commits.delp(topic, part)
	g.c.persistGroupEntry(groupLogEntry{
		Type:  "delete",
		Group: g.name,
		Topic: topic,
		Part:  part,
	})
}

// classicSubscribedTopics returns the set of topics subscribed by any
// member of a classic consumer group (from join protocol metadata).
func (g *group) classicSubscribedTopics() map[string]struct{} {
	sub := make(map[string]struct{})
	for _, ms := range []map[string]*groupMember{g.members, g.pending} {
		for _, m := range ms {
			if m.join == nil {
				continue
			}
			for _, proto := range m.join.Protocols {
				var meta kmsg.ConsumerMemberMetadata
				if err := meta.ReadFrom(proto.Metadata); err == nil {
					for _, topic := range meta.Topics {
						sub[topic] = struct{}{}
					}
				}
			}
		}
	}
	return sub
}

// expireOffsets removes committed offsets that have exceeded the retention
// period (KIP-211). Returns true if all offsets expired and the group can
// be deleted (no members, no pending txn offsets).
//
// Must be called from within the group's manage goroutine.
func (g *group) expireOffsets(retentionMs int64, hasUnstableOffsets bool) bool {
	subscribed := g.offsetExpirationFilter()
	if subscribed == nil {
		return false // no expiration applies in this state
	}

	now := time.Now()
	retention := time.Duration(retentionMs) * time.Millisecond

	// Collect expired offsets. We can't delete during iteration since
	// delp may remove the inner map.
	type tp struct {
		t string
		p int32
	}
	var expired []tp
	g.commits.each(func(topic string, part int32, oc *offsetCommit) {
		if _, ok := subscribed[topic]; ok {
			return // subscribed - skip
		}
		if oc.lastCommit.IsZero() {
			return // no timestamp (pre-KIP-211 data) - skip
		}
		base := oc.lastCommit
		// For empty classic groups with protocolType, use max(emptyAt, commitTimestamp)
		if g.protocolType != "" && g.state == groupEmpty && g.emptyAt.After(base) {
			base = g.emptyAt
		}
		if now.Sub(base) >= retention {
			expired = append(expired, tp{topic, part})
		}
	})

	for _, e := range expired {
		g.deleteCommitAndPersist(e.t, e.p)
	}

	if hasUnstableOffsets || len(g.commits) > 0 {
		return false
	}
	if g.typ == "consumer" {
		return g.activeConsumerCount() == 0
	}
	return g.state == groupEmpty
}

// expireAllTopics is a non-nil empty sentinel for offsetExpirationFilter:
// no topics are subscribed, so all are eligible for expiration.
var expireAllTopics = map[string]struct{}{}

// offsetExpirationFilter returns the set of subscribed topics whose
// offsets should NOT be expired, or nil if no expiration applies.
// A non-nil empty map means all topics are eligible for expiration.
//
// Expiration conditions per Kafka (KIP-211):
//   - Simple group (no protocolType): all topics, any state
//   - Classic with protocolType, Empty: all topics
//   - Classic consumer, Stable: unsubscribed topics only
//   - Classic with protocolType, other states: no expiration
//   - 848 consumer: unsubscribed topics, any state
func (g *group) offsetExpirationFilter() map[string]struct{} {
	switch g.typ {
	case "consumer":
		// 848: expire unsubscribed topics in any state
		var sub map[string]struct{}
		for _, m := range g.consumerMembers {
			if m.memberEpoch == -2 {
				continue
			}
			for _, t := range m.subscribedTopics {
				if sub == nil {
					sub = make(map[string]struct{})
				}
				sub[t] = struct{}{}
			}
		}
		if sub == nil {
			return expireAllTopics
		}
		return sub

	default:
		// Classic group
		if g.protocolType == "" {
			return expireAllTopics // simple group - expire all
		}
		switch g.state {
		case groupEmpty:
			return expireAllTopics
		case groupStable:
			if g.protocolType == "consumer" {
				return g.classicSubscribedTopics()
			}
			return nil
		case groupPreparingRebalance, groupCompletingRebalance, groupDead, groupReconciling:
			return nil
		}
		return nil
	}
}

// staticMemberEntry builds a static member log entry. Empty memberID
// signals deletion on replay.
func (g *group) staticMemberEntry(instanceID, memberID string) groupLogEntry {
	return groupLogEntry{
		Type:       "static",
		Group:      g.name,
		InstanceID: instanceID,
		MemberID:   memberID,
	}
}

func (g *group) persistStaticMember(instanceID, memberID string) {
	g.c.persistGroupEntry(g.staticMemberEntry(instanceID, memberID))
}

// evictConsumerMember fences and removes a consumer member, then
// recomputes the target assignment.
func (g *group) evictConsumerMember(m *consumerMember) {
	g.fenceConsumerMember(m)
	delete(g.consumerMembers, m.memberID)
	g.groupEpoch++
	g.computeTargetAssignment(g.lastTopicMeta)
	g.updateConsumerStateField()
	g.persistMeta848()
}

// atConsumerSessionTimeout sets up the session timeout for a consumer
// member. The session timeout uses the per-group or server-level config
// group.consumer.session.timeout.ms and fences the member entirely if
// no heartbeats are received within the timeout.
func (g *group) atConsumerSessionTimeout(m *consumerMember) {
	g.atConsumerSessionTimeoutIn(m, time.Duration(g.c.consumerSessionTimeoutMsForGroup(g.name))*time.Millisecond)
}

// scheduleConsumerRebalanceTimeout starts a per-member rebalance
// timeout that fences the member if it does not complete partition
// revocation within rebalanceTimeoutMs. Only active when the member
// has partitions to release. If the member's epoch has advanced by
// the time the timer fires, the timeout is ignored.
func (g *group) scheduleConsumerRebalanceTimeout(m *consumerMember) {
	g.cancelConsumerRebalanceTimeout(m)
	timeout := time.Duration(m.rebalanceTimeoutMs) * time.Millisecond
	epoch := m.memberEpoch
	memberID := m.memberID
	m.tRebal = g.timerControlFn(timeout, func() {
		// Check the member still exists and hasn't
		// progressed past the epoch we were watching.
		cur, ok := g.consumerMembers[memberID]
		if !ok || cur.memberEpoch != epoch {
			return
		}
		g.c.cfg.logger.Logf(LogLevelWarn, "consumerRebalanceTimeout: group=%s member=%s epoch=%d remaining=%d",
			g.logName(), memberID, epoch, len(g.consumerMembers)-1)
		g.evictConsumerMember(cur)
	})
}

func (*group) cancelConsumerRebalanceTimeout(m *consumerMember) {
	if m.tRebal != nil {
		m.tRebal.Stop()
		m.tRebal = nil
	}
}

// activeConsumerCount returns the number of consumer members that are
// not temporarily departed (epoch -2).
func (g *group) activeConsumerCount() int {
	var n int
	for _, m := range g.consumerMembers {
		if m.memberEpoch != -2 {
			n++
		}
	}
	return n
}

// fenceConsumerMember stops all timers and clears partition epoch
// entries for the member. The caller must delete the member from
// consumerMembers separately.
func (g *group) fenceConsumerMember(m *consumerMember) {
	if m.t != nil {
		m.t.Stop()
	}
	g.cancelConsumerRebalanceTimeout(m)
	// Remove this member's epoch contribution. Epoch-matching
	// is safe here because updateMemberEpochs at the epoch
	// bump keeps epoch values in sync with memberEpoch.
	g.removePartitionEpochs(m.lastReconciledSent, m.memberEpoch)
	g.removePartitionEpochs(m.partitionsPendingRevocation, m.memberEpoch)
}

// Handles a commit for consumer groups with relaxed validation per KIP-1251.
func (g *group) handleConsumerOffsetCommit(creq *clientReq) *kmsg.OffsetCommitResponse {
	req := creq.kreq.(*kmsg.OffsetCommitRequest)
	resp := req.ResponseKind().(*kmsg.OffsetCommitResponse)

	if kerr := g.c.validateGroup(creq, req.Group); kerr != nil {
		fillOffsetCommit(req, resp, kerr.Code)
		return resp
	}
	if !g.c.allowedACL(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		fillOffsetCommit(req, resp, kerr.GroupAuthorizationFailed.Code)
		return resp
	}

	// Empty group with negative epoch: accept without validation
	// (admin / kadm commits). Use activeConsumerCount to skip
	// epoch -2 (static leave) members. This check must come BEFORE
	// the version gate so that admin commits on empty groups are
	// accepted at any OffsetCommit version.
	var cm *consumerMember
	if g.activeConsumerCount() == 0 && req.Generation < 0 { //nolint:revive // intentional empty block, falls through to commit
	} else {
		// Consumer groups require OffsetCommit v9+ (KIP-848). Older
		// versions use GenerationID which does not map to member
		// epochs.
		if req.Version < 9 {
			fillOffsetCommit(req, resp, kerr.UnsupportedVersion.Code)
			return resp
		}
		if req.MemberID == "" {
			fillOffsetCommit(req, resp, kerr.UnknownMemberID.Code)
			return resp
		}
		m, ok := g.consumerMembers[req.MemberID]
		if !ok {
			fillOffsetCommit(req, resp, kerr.UnknownMemberID.Code)
			return resp
		}
		if req.Generation > m.memberEpoch {
			g.c.cfg.logger.Logf(LogLevelWarn, "OffsetCommit STALE: group=%s member=%s reqEpoch=%d memberEpoch=%d",
				g.logName(), req.MemberID, req.Generation, m.memberEpoch)
			fillOffsetCommit(req, resp, kerr.StaleMemberEpoch.Code)
			return resp
		}
		g.atConsumerSessionTimeout(m)
		cm = m
	}

	allowed := g.fillOffsetCommitWithACL(creq, req, resp)
	for _, t := range allowed {
		for _, p := range t.Partitions {
			// KIP-1251: per-partition assignment epoch check.
			// Reject if the request epoch is older than when
			// this partition was assigned to the member.
			if cm != nil {
				if id, ok := g.lastTopicMeta[t.Topic]; ok {
					if epochs, ok := cm.partAssignmentEpochs[id.id]; ok {
						if assignEpoch, ok := epochs[p.Partition]; ok && req.Generation < assignEpoch {
							g.c.cfg.logger.Logf(LogLevelWarn, "OffsetCommit STALE: group=%s member=%s topic=%s p=%d reqEpoch=%d assignmentEpoch=%d",
								g.logName(), req.MemberID, t.Topic[:min(16, len(t.Topic))], p.Partition, req.Generation, assignEpoch)
							setOffsetCommitPartitionErr(resp, t.Topic, p.Partition, kerr.StaleMemberEpoch.Code)
							continue
						}
					}
				}
			}
			g.commitAndPersist(t.Topic, p.Partition, offsetCommit{
				offset:      p.Offset,
				leaderEpoch: p.LeaderEpoch,
				metadata:    p.Metadata,
			})
		}
	}
	return resp
}

func copyAssignment(a map[uuid][]int32) map[uuid][]int32 {
	c := make(map[uuid][]int32, len(a))
	for id, parts := range a {
		c[id] = slices.Clone(parts)
	}
	return c
}

func countAssignment(a map[uuid][]int32) int {
	var n int
	for _, parts := range a {
		n += len(parts)
	}
	return n
}

func (g *group) countPartitionEpochs() int {
	var n int
	for _, pm := range g.partitionEpochs {
		n += len(pm)
	}
	return n
}

func topicsToAssignment(topics []kmsg.ConsumerGroupHeartbeatRequestTopic) map[uuid][]int32 {
	m := make(map[uuid][]int32, len(topics))
	for _, t := range topics {
		m[t.TopicID] = t.Partitions
	}
	return m
}

// updatePartAssignmentEpochs updates per-partition assignment epochs
// after reconciliation. Retained partitions keep their existing epoch;
// newly assigned partitions get the member's current epoch.
func (m *consumerMember) updatePartAssignmentEpochs(reconciled map[uuid][]int32) {
	newEpochs := make(map[uuid]map[int32]int32, len(reconciled))
	for id, parts := range reconciled {
		newEpochs[id] = make(map[int32]int32, len(parts))
		for _, p := range parts {
			if existing, ok := m.partAssignmentEpochs[id]; ok {
				if epoch, ok := existing[p]; ok {
					newEpochs[id][p] = epoch
					continue
				}
			}
			newEpochs[id][p] = m.memberEpoch
		}
	}
	m.partAssignmentEpochs = newEpochs
}

// ownsRevokedPartitions matches Kafka's check: if topics is nil
// (keepalive), conservatively assume the client still owns the revoked
// partitions. Otherwise, return true if any pending partition is still
// reported by the client.
func ownsRevokedPartitions(pending map[uuid][]int32, topics []kmsg.ConsumerGroupHeartbeatRequestTopic) bool {
	if topics == nil {
		return len(pending) > 0
	}
	reported := topicsToAssignment(topics)
	for id, parts := range pending {
		for _, p := range parts {
			if slices.Contains(reported[id], p) {
				return true
			}
		}
	}
	return false
}

// Returns true if every partition in owned is present in target.
func isSubsetAssignment(owned []kmsg.ConsumerGroupHeartbeatRequestTopic, target map[uuid][]int32) bool {
	for _, t := range owned {
		tParts, ok := target[t.TopicID]
		if !ok {
			return false
		}
		for _, p := range t.Partitions {
			if !slices.Contains(tParts, p) {
				return false
			}
		}
	}
	return true
}

// currentPartitionEpoch returns the epoch of the member that currently
// owns the given partition, or -1 if no member owns it.
func (g *group) currentPartitionEpoch(topicID uuid, partition int32) int32 {
	if pm := g.partitionEpochs[topicID]; pm != nil {
		if epoch, ok := pm[partition]; ok {
			return epoch
		}
	}
	return -1
}

// updateMemberEpochs updates the partition epoch map when a member's
// state changes. Removes old contribution (epoch-matching to avoid
// clobbering another member's entry), then adds new contribution
// from the member's current fields. Caller must save old state
// before mutating the member.
func (g *group) updateMemberEpochs(m *consumerMember, oldSent, oldPending map[uuid][]int32, oldEpoch int32) {
	g.removePartitionEpochs(oldSent, oldEpoch)
	g.removePartitionEpochs(oldPending, oldEpoch)
	g.addPartitionEpochs(m.lastReconciledSent, m.memberEpoch)
	g.addPartitionEpochs(m.partitionsPendingRevocation, m.memberEpoch)
}

// addPartitionEpochs records that a member at the given epoch owns the
// given partitions. If a partition already has a higher or equal epoch,
// the caller has a logic bug (double assignment) - we log and skip the
// update to avoid masking the real owner.
func (g *group) addPartitionEpochs(a map[uuid][]int32, epoch int32) {
	for id, parts := range a {
		pm := g.partitionEpochs[id]
		if pm == nil {
			pm = make(map[int32]int32, len(parts))
			g.partitionEpochs[id] = pm
		}
		for _, p := range parts {
			if existing, ok := pm[p]; ok && existing >= epoch {
				g.c.cfg.logger.Logf(LogLevelError, "group %s: addPartitionEpochs: partition %d of topic %v already has epoch %d >= %d, skipping", g.name, p, id, existing, epoch)
				continue
			}
			pm[p] = epoch
		}
	}
}

// removePartitionEpochs clears epoch entries for the given partitions,
// but only if the stored epoch matches expectedEpoch. This prevents a
// stale removal from clearing a newer owner's entry.
func (g *group) removePartitionEpochs(a map[uuid][]int32, expectedEpoch int32) {
	for id, parts := range a {
		pm := g.partitionEpochs[id]
		if pm == nil {
			continue
		}
		for _, p := range parts {
			if pm[p] == expectedEpoch {
				delete(pm, p)
			}
		}
		if len(pm) == 0 {
			delete(g.partitionEpochs, id)
		}
	}
}

// validateMemberGeneration checks that the memberID and generation are
// valid for this group. Must be called from the manage loop (via
// waitControl). Returns 0 on success or an error code.
func (g *group) validateMemberGeneration(memberID string, generation int32) int16 {
	if g.typ == "consumer" {
		if memberID != "" {
			m, exists := g.consumerMembers[memberID]
			if !exists {
				return kerr.UnknownMemberID.Code
			}
			if generation != -1 && generation != m.memberEpoch {
				return kerr.IllegalGeneration.Code
			}
		} else if generation != -1 && generation != g.groupEpoch {
			return kerr.IllegalGeneration.Code
		}
	} else {
		if memberID != "" {
			if _, exists := g.members[memberID]; !exists {
				return kerr.UnknownMemberID.Code
			}
		}
		if generation != -1 && generation != g.generation {
			return kerr.IllegalGeneration.Code
		}
	}
	return 0
}

// restoreClassicMembers restores classic group members from persisted
// session state. Members whose session has expired (elapsed time since
// shutdown >= session timeout) are skipped.
func (g *group) restoreClassicMembers(shutdownAt time.Time, sg sessionClassicGroup) {
	// Provisionally set state to groupStable while restoring members;
	// if the persisted sg.State was non-Stable we re-trigger the
	// rebalance at the end of the function. See comment near the end.
	g.state = groupStable
	g.leader = sg.Leader
	for _, sm := range sg.Members {
		// Use per-member last heartbeat time if available,
		// falling back to shutdownAt for old session files.
		lastHB := sm.LastHeartbeat
		if lastHB.IsZero() {
			lastHB = shutdownAt
		}
		remaining := time.Duration(sm.SessionTimeoutMs)*time.Millisecond - time.Since(lastHB)
		if remaining <= 0 {
			continue
		}
		m := &groupMember{
			memberID:   sm.ID,
			instanceID: sm.InstanceID,
			clientID:   sm.ClientID,
			clientHost: sm.ClientHost,
			assignment: sm.Assignment,
			join: &kmsg.JoinGroupRequest{
				SessionTimeoutMillis:   sm.SessionTimeoutMs,
				RebalanceTimeoutMillis: sm.RebalanceTimeoutMs,
			},
		}
		for _, name := range sm.Protocols {
			m.join.Protocols = append(m.join.Protocols, kmsg.JoinGroupRequestProtocol{Name: name})
			g.protocols[name]++
		}
		g.members[m.memberID] = m
		if m.instanceID != nil {
			g.staticMembers[*m.instanceID] = m.memberID
		}
		g.atSessionTimeoutIn(m, remaining, func() {
			g.removeMember(m)
			g.rebalance()
		})
		// Preserve the real last heartbeat time so that across
		// repeated restarts, dead members accumulate elapsed time.
		m.last = lastHB
	}
	if len(g.members) == 0 {
		g.state = groupEmpty
		g.emptyAt = time.Now()
		g.leader = ""
		return
	}
	// If the pre-shutdown state was non-Stable, a rebalance was in
	// progress (e.g. a LeaveGroup had transitioned the group to
	// PreparingRebalance but the rebalance hadn't completed before
	// shutdown). Re-trigger rebalance so members rejoin and the
	// assignment reflects the current member set -- otherwise
	// partitions owned by departed members stay orphaned. If the
	// persisted state was Stable, the old assignment is still valid
	// for the current members; do not force a rebalance.
	if sg.State != groupStable && sg.State != groupEmpty {
		g.rebalance()
	}
}

// atSessionTimeoutIn starts a session timeout timer with a custom duration.
// atSessionTimeout is a convenience wrapper that reads the timeout from the
// member's join request.
func (g *group) atSessionTimeoutIn(m *groupMember, d time.Duration, fn func()) {
	if m.t != nil {
		m.t.Stop()
	}
	m.last = time.Now()
	m.t = g.timerControlFn(d, func() {
		if time.Since(m.last) >= d {
			fn()
		}
	})
}

// restoreConsumerMembers restores 848 consumer group members from persisted
// session state. If all sessions have expired, no members are restored.
func (g *group) restoreConsumerMembers(shutdownAt time.Time, sg sessionConsumerGroup) {
	sessionTimeout := time.Duration(g.c.consumerSessionTimeoutMsForGroup(g.name)) * time.Millisecond

	g.partitionEpochs = sg.PartitionEpochs
	g.targetAssignmentEpoch = sg.TargetAssignmentEpoch

	for _, sm := range sg.Members {
		// Use per-member last heartbeat time if available,
		// falling back to shutdownAt for old session files.
		lastHB := sm.LastHeartbeat
		if lastHB.IsZero() {
			lastHB = shutdownAt
		}
		remaining := sessionTimeout - time.Since(lastHB)
		if remaining <= 0 {
			g.c.cfg.logger.Logf(LogLevelDebug, "restoreConsumerMembers: group=%s member=%s expired (lastHB=%v ago)",
				g.logName(), sm.ID, time.Since(lastHB))
			continue
		}

		m := &consumerMember{
			memberID:                    sm.ID,
			instanceID:                  sm.InstanceID,
			clientID:                    sm.ClientID,
			clientHost:                  sm.ClientHost,
			memberEpoch:                 sm.Epoch,
			previousMemberEpoch:         sm.PrevEpoch,
			state:                       consumerMemberState(sm.CmState),
			subscribedTopics:            sm.Topics,
			lastReconciledSent:          sm.Reconciled,
			partitionsPendingRevocation: sm.PendingRevoke,
			targetAssignment:            sm.Target,
			partAssignmentEpochs:        sm.PartAssignmentEpochs,
			rackID:                      sm.Rack,
			serverAssignor:              sm.Assignor,
			rebalanceTimeoutMs:          sm.RebalanceTimeoutMs,
		}
		if m.instanceID != nil {
			g.staticMembers[*m.instanceID] = m.memberID
		}
		g.consumerMembers[m.memberID] = m
		g.atConsumerSessionTimeoutIn(m, remaining)
		// Preserve the real last heartbeat time so that across
		// repeated restarts, dead members accumulate elapsed time
		// instead of resetting to "just now" each restore.
		m.last = lastHB
	}
	if len(g.consumerMembers) == 0 {
		return
	}
	g.updateConsumerStateField()
}

// atConsumerSessionTimeoutIn starts a consumer session timeout timer with a
// custom initial duration. Used after loading session state.
func (g *group) atConsumerSessionTimeoutIn(m *consumerMember, d time.Duration) {
	if m.t != nil {
		m.t.Stop()
	}
	m.last = time.Now()
	m.t = g.timerControlFn(d, func() {
		if time.Since(m.last) >= d {
			g.c.cfg.logger.Logf(LogLevelWarn, "consumerSessionTimeout: group=%s member=%s epoch=%d remaining=%d",
				g.logName(), m.memberID, m.memberEpoch, len(g.consumerMembers)-1)
			g.evictConsumerMember(m)
		}
	})
}
