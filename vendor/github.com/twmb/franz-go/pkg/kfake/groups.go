package kfake

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// TODO instance IDs
// TODO persisting groups so commits can happen to client-managed groups
//      we need lastCommit, and need to better prune empty groups

type (
	groups struct {
		c  *Cluster
		gs map[string]*group
	}

	group struct {
		c    *Cluster
		gs   *groups
		name string

		state groupState

		leader  string
		members map[string]*groupMember
		pending map[string]*groupMember

		commits tps[offsetCommit]

		generation   int32
		protocolType string
		protocols    map[string]int
		protocol     string

		reqCh     chan *clientReq
		controlCh chan func()

		nJoining int

		tRebalance *time.Timer

		quit   sync.Once
		quitCh chan struct{}
	}

	groupMember struct {
		memberID   string
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

	offsetCommit struct {
		offset      int64
		leaderEpoch int32
		metadata    *string
	}

	groupState int8
)

const (
	groupEmpty groupState = iota
	groupStable
	groupPreparingRebalance
	groupCompletingRebalance
	groupDead
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
	default:
		return "Unknown"
	}
}

func (c *Cluster) coordinator(id string) *broker {
	gen := c.coordinatorGen.Load()
	n := hashString(fmt.Sprintf("%d", gen)+"\x00\x00"+id) % uint64(len(c.bs))
	return c.bs[n]
}

func (c *Cluster) validateGroup(creq *clientReq, group string) *kerr.Error {
	switch key := kmsg.Key(creq.kreq.Key()); key {
	case kmsg.OffsetCommit, kmsg.OffsetFetch, kmsg.DescribeGroups, kmsg.DeleteGroups:
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

func (gs *groups) newGroup(name string) *group {
	return &group{
		c:         gs.c,
		gs:        gs,
		name:      name,
		members:   make(map[string]*groupMember),
		pending:   make(map[string]*groupMember),
		protocols: make(map[string]int),
		reqCh:     make(chan *clientReq),
		controlCh: make(chan func()),
		quitCh:    make(chan struct{}),
	}
}

// handleJoin completely hijacks the incoming request.
func (gs *groups) handleJoin(creq *clientReq) {
	if gs.gs == nil {
		gs.gs = make(map[string]*group)
	}
	req := creq.kreq.(*kmsg.JoinGroupRequest)
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
		goto start
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
		goto start
	}
}

func (gs *groups) handleOffsetDelete(creq *clientReq) bool {
	return gs.handleHijack(creq.kreq.(*kmsg.OffsetDeleteRequest).Group, creq)
}

func (gs *groups) handleList(creq *clientReq) *kmsg.ListGroupsResponse {
	req := creq.kreq.(*kmsg.ListGroupsRequest)
	resp := req.ResponseKind().(*kmsg.ListGroupsResponse)

	var states map[string]struct{}
	if len(req.StatesFilter) > 0 {
		states = make(map[string]struct{})
		for _, state := range req.StatesFilter {
			states[state] = struct{}{}
		}
	}

	for _, g := range gs.gs {
		if g.c.coordinator(g.name).node != creq.cc.b.node {
			continue
		}
		g.waitControl(func() {
			if states != nil {
				if _, ok := states[g.state.String()]; !ok {
					return
				}
			}
			sg := kmsg.NewListGroupsResponseGroup()
			sg.Group = g.name
			sg.ProtocolType = g.protocolType
			sg.GroupState = g.state.String()
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
		if kerr := gs.c.validateGroup(creq, rg); kerr != nil {
			sg.ErrorCode = kerr.Code
			continue
		}
		g, ok := gs.gs[rg]
		if !ok {
			sg.State = groupDead.String()
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
			switch g.state {
			case groupDead:
				sg.ErrorCode = kerr.GroupIDNotFound.Code
			case groupEmpty:
				g.quitOnce()
				delete(gs.gs, rg)
			case groupPreparingRebalance, groupCompletingRebalance, groupStable:
				sg.ErrorCode = kerr.NonEmptyGroup.Code
			}
		}) {
			sg.ErrorCode = kerr.GroupIDNotFound.Code
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
			rg.Topics = make([]kmsg.OffsetFetchRequestGroupTopic, len(req.Topics))
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
		if kerr := gs.c.validateGroup(creq, rg.Group); kerr != nil {
			sg.ErrorCode = kerr.Code
			continue
		}
		g, ok := gs.gs[rg.Group]
		if !ok {
			sg.ErrorCode = kerr.GroupIDNotFound.Code
			continue
		}
		if !g.waitControl(func() {
			if rg.Topics == nil {
				for t, ps := range g.commits {
					st := kmsg.NewOffsetFetchResponseGroupTopic()
					st.Topic = t
					for p, c := range ps {
						sp := kmsg.NewOffsetFetchResponseGroupTopicPartition()
						sp.Partition = p
						sp.Offset = c.offset
						sp.LeaderEpoch = c.leaderEpoch
						sp.Metadata = c.metadata
						st.Partitions = append(st.Partitions, sp)
					}
					sg.Topics = append(sg.Topics, st)
				}
			} else {
				for _, t := range rg.Topics {
					st := kmsg.NewOffsetFetchResponseGroupTopic()
					st.Topic = t.Topic
					for _, p := range t.Partitions {
						sp := kmsg.NewOffsetFetchResponseGroupTopicPartition()
						sp.Partition = p
						c, ok := g.commits.getp(t.Topic, p)
						if !ok {
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

	if kerr := g.c.validateGroup(creq, req.Group); kerr != nil {
		resp.ErrorCode = kerr.Code
		return resp
	}

	tidx := make(map[string]int)
	donet := func(t string, errCode int16) *kmsg.OffsetDeleteResponseTopic {
		if i, ok := tidx[t]; ok {
			return &resp.Topics[i]
		}
		tidx[t] = len(resp.Topics)
		st := kmsg.NewOffsetDeleteResponseTopic()
		st.Topic = t
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donep := func(t string, p int32, errCode int16) *kmsg.OffsetDeleteResponseTopicPartition {
		sp := kmsg.NewOffsetDeleteResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		st := donet(t, 0)
		st.Partitions = append(st.Partitions, sp)
		return &st.Partitions[len(st.Partitions)-1]
	}

	// empty: delete everything in request
	// preparingRebalance, completingRebalance, stable:
	//   * if consumer, delete everything not subscribed to
	//   * if not consumer, delete nothing, error with non_empty_group
	subTopics := make(map[string]struct{})
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
		for _, m := range []map[string]*groupMember{
			g.members,
			g.pending,
		} {
			for _, m := range m {
				if m.join == nil {
					continue
				}
				for _, proto := range m.join.Protocols {
					var m kmsg.ConsumerMemberMetadata
					if err := m.ReadFrom(proto.Metadata); err == nil {
						for _, topic := range m.Topics {
							subTopics[topic] = struct{}{}
						}
					}
				}
			}
		}
	}

	for _, t := range req.Topics {
		for _, p := range t.Partitions {
			if _, ok := subTopics[t.Topic]; ok {
				donep(t.Topic, p.Partition, kerr.GroupSubscribedToTopic.Code)
				continue
			}
			g.commits.delp(t.Topic, p.Partition)
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
	// serialization loop that is initializing us.
	var firstJoin func(bool)
	firstJoin = func(ok bool) {
		firstJoin = func(bool) {}
		if !ok {
			delete(g.gs.gs, g.name)
			g.quitOnce()
		}
		detachNew()
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
	}()

	for {
		select {
		case <-g.quitCh:
			return
		case creq := <-g.reqCh:
			var kresp kmsg.Response
			switch creq.kreq.(type) {
			case *kmsg.JoinGroupRequest:
				var ok bool
				kresp, ok = g.handleJoin(creq)
				firstJoin(ok)
			case *kmsg.SyncGroupRequest:
				kresp = g.handleSync(creq)
			case *kmsg.HeartbeatRequest:
				kresp = g.handleHeartbeat(creq)
			case *kmsg.LeaveGroupRequest:
				kresp = g.handleLeave(creq)
			case *kmsg.OffsetCommitRequest:
				var ok bool
				kresp, ok = g.handleOffsetCommit(creq)
				firstJoin(ok)
			case *kmsg.OffsetDeleteRequest:
				kresp = g.handleOffsetDelete(creq)
			}
			if kresp != nil {
				g.reply(creq, kresp, nil)
			}

		case fn := <-g.controlCh:
			fn()
		}
	}
}

func (g *group) waitControl(fn func()) bool {
	wait := make(chan struct{})
	wfn := func() { fn(); close(wait) }
	select {
	case <-g.quitCh:
		return false
	case g.controlCh <- wfn:
		<-wait
		return true
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
	if req.InstanceID != nil {
		resp.ErrorCode = kerr.InvalidGroupID.Code
		return resp, false
	}
	if st := int64(req.SessionTimeoutMillis); st < g.c.cfg.minSessionTimeout.Milliseconds() || st > g.c.cfg.maxSessionTimeout.Milliseconds() {
		resp.ErrorCode = kerr.InvalidSessionTimeout.Code
		return resp, false
	}
	if !g.protocolsMatch(req.ProtocolType, req.Protocols) {
		resp.ErrorCode = kerr.InconsistentGroupProtocol.Code
		return resp, false
	}

	// Clients first join with no member ID. For join v4+, we generate
	// the member ID and add the member to pending. For v3 and below,
	// we immediately enter rebalance.
	if req.MemberID == "" {
		memberID := generateMemberID(creq.cid, req.InstanceID)
		resp.MemberID = memberID
		m := &groupMember{
			memberID:   memberID,
			clientID:   creq.cid,
			clientHost: creq.cc.conn.RemoteAddr().String(),
			join:       req,
		}
		if req.Version >= 4 {
			g.addPendingRebalance(m)
			resp.ErrorCode = kerr.MemberIDRequired.Code
			return resp, true
		}
		g.addMemberAndRebalance(m, creq, req)
		return nil, true
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
			g.fillJoinResp(req, resp)
			return resp, true
		}
		g.updateMemberAndRebalance(m, creq, req)
	case groupStable:
		if g.leader != req.MemberID || m.sameJoin(req) {
			g.fillJoinResp(req, resp)
			return resp, true
		}
		g.updateMemberAndRebalance(m, creq, req)
	}
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
	if req.InstanceID != nil {
		resp.ErrorCode = kerr.InvalidGroupID.Code
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
	if req.InstanceID != nil {
		resp.ErrorCode = kerr.InvalidGroupID.Code
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
		if rm.InstanceID != nil {
			r.ErrorCode = kerr.UnknownMemberID.Code
			continue
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
		for _, p := range t.Partitions {
			sp := kmsg.NewOffsetCommitResponseTopicPartition()
			sp.Partition = p.Partition
			sp.ErrorCode = code
			st.Partitions = append(st.Partitions, sp)
		}
		resp.Topics = append(resp.Topics, st)
	}
}

// Handles a commit.
func (g *group) handleOffsetCommit(creq *clientReq) (*kmsg.OffsetCommitResponse, bool) {
	req := creq.kreq.(*kmsg.OffsetCommitRequest)
	resp := req.ResponseKind().(*kmsg.OffsetCommitResponse)

	if kerr := g.c.validateGroup(creq, req.Group); kerr != nil {
		fillOffsetCommit(req, resp, kerr.Code)
		return resp, false
	}
	if req.InstanceID != nil {
		fillOffsetCommit(req, resp, kerr.InvalidGroupID.Code)
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
	} else {
		if req.MemberID != "" {
			fillOffsetCommit(req, resp, kerr.UnknownMemberID.Code)
			return resp, false
		}
		if req.Generation != -1 {
			fillOffsetCommit(req, resp, kerr.IllegalGeneration.Code)
			return resp, false
		}
		if g.state != groupEmpty {
			panic("invalid state: no members, but group not empty")
		}
	}

	switch g.state {
	default:
		fillOffsetCommit(req, resp, kerr.GroupIDNotFound.Code)
		return resp, true
	case groupEmpty:
		for _, t := range req.Topics {
			for _, p := range t.Partitions {
				g.commits.set(t.Topic, p.Partition, offsetCommit{
					offset:      p.Offset,
					leaderEpoch: p.LeaderEpoch,
					metadata:    p.Metadata,
				})
			}
		}
		fillOffsetCommit(req, resp, 0)
	case groupPreparingRebalance, groupStable:
		for _, t := range req.Topics {
			for _, p := range t.Partitions {
				g.commits.set(t.Topic, p.Partition, offsetCommit{
					offset:      p.Offset,
					leaderEpoch: p.LeaderEpoch,
					metadata:    p.Metadata,
				})
			}
		}
		fillOffsetCommit(req, resp, 0)
		g.updateHeartbeat(m)
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

	if g.nJoining >= len(g.members) {
		g.completeRebalance()
		return
	}

	var rebalanceTimeoutMs int32
	for _, m := range g.members {
		if m.join.RebalanceTimeoutMillis > rebalanceTimeoutMs {
			rebalanceTimeoutMs = m.join.RebalanceTimeoutMillis
		}
	}
	if g.tRebalance == nil {
		g.tRebalance = time.AfterFunc(time.Duration(rebalanceTimeoutMs)*time.Millisecond, func() {
			select {
			case <-g.quitCh:
			case g.controlCh <- func() {
				g.completeRebalance()
			}:
			}
		})
	}
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
			for _, p := range m.join.Protocols {
				g.protocols[p.Name]--
			}
			delete(g.members, m.memberID)
			if m.t != nil {
				m.t.Stop()
			}
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
		return
	}
	g.state = groupCompletingRebalance

	var foundProto bool
	for proto, nsupport := range g.protocols {
		if nsupport == len(g.members) {
			g.protocol = proto
			foundProto = true
			break
		}
	}
	if !foundProto {
		panic(fmt.Sprint("unable to find commonly supported protocol!", g.protocols, len(g.members)))
	}

	for _, m := range g.members {
		if !foundLeader {
			g.leader = m.memberID
		}
		req := m.join
		resp := req.ResponseKind().(*kmsg.JoinGroupResponse)
		g.fillJoinResp(req, resp)
		g.reply(m.waitingReply, resp, m)
	}
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
		m.assignment = a.MemberAssignment
	}
	for _, m := range g.members {
		if m.waitingReply.empty() {
			continue // this member saw join but has not yet called sync
		}
		resp := m.waitingReply.kreq.ResponseKind().(*kmsg.SyncGroupResponse)
		resp.ProtocolType = kmsg.StringPtr(g.protocolType)
		resp.Protocol = kmsg.StringPtr(g.protocol)
		resp.MemberAssignment = m.assignment
		g.reply(m.waitingReply, resp, m)
	}
	g.state = groupStable
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

func (g *group) atSessionTimeout(m *groupMember, fn func()) {
	if m.t != nil {
		m.t.Stop()
	}
	timeout := time.Millisecond * time.Duration(m.join.SessionTimeoutMillis)
	m.last = time.Now()
	tfn := func() {
		select {
		case <-g.quitCh:
		case g.controlCh <- func() {
			if time.Since(m.last) >= timeout {
				fn()
			}
		}:
		}
	}
	m.t = time.AfterFunc(timeout, tfn)
}

// This is used to update a member from a new join request, or to clear a
// member from failed heartbeats.
func (g *group) updateMemberAndRebalance(m *groupMember, waitingReply *clientReq, newJoin *kmsg.JoinGroupRequest) {
	for _, p := range m.join.Protocols {
		g.protocols[p.Name]--
	}
	m.join = newJoin
	if m.join != nil {
		for _, p := range m.join.Protocols {
			g.protocols[p.Name]++
		}
		if m.waitingReply.empty() && !waitingReply.empty() {
			g.nJoining++
		}
		m.waitingReply = waitingReply
	} else {
		delete(g.members, m.memberID)
		if m.t != nil {
			m.t.Stop()
		}
		if !m.waitingReply.empty() {
			g.nJoining--
		}
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
	for _, p := range protocols {
		if _, ok := g.protocols[p.Name]; ok {
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

func (g *group) fillJoinResp(req *kmsg.JoinGroupRequest, resp *kmsg.JoinGroupResponse) {
	resp.Generation = g.generation
	resp.ProtocolType = kmsg.StringPtr(g.protocolType)
	resp.Protocol = kmsg.StringPtr(g.protocol)
	resp.LeaderID = g.leader
	resp.MemberID = req.MemberID
	if g.leader == req.MemberID {
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
					ProtocolMetadata: p.Metadata,
				})
				continue members
			}
		}
		panic("inconsistent group protocol within saved members")
	}
	return metadata
}

func (g *group) reply(creq *clientReq, kresp kmsg.Response, m *groupMember) {
	select {
	case creq.cc.respCh <- clientResp{kresp: kresp, corr: creq.corr, seq: creq.seq}:
	case <-g.c.die:
		return
	}
	if m != nil {
		m.waitingReply = nil
		g.updateHeartbeat(m)
	}
}
