package store

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ── Error codes (same numeric values as the Kafka protocol) ───────────────────

const (
	GroupErrNone                int16 = 0
	GroupErrIllegalGeneration   int16 = 22
	GroupErrUnknownMemberID     int16 = 23
	GroupErrRebalanceInProgress int16 = 25
)

// ── Value types ───────────────────────────────────────────────────────────────

// TopicPartition identifies a specific partition within a topic.
type TopicPartition struct {
	Topic     string
	Partition int32
}

// GroupState represents the lifecycle state of a consumer group.
type GroupState int8

const (
	GroupStateEmpty               GroupState = 0
	GroupStatePreparingRebalance  GroupState = 1
	GroupStateCompletingRebalance GroupState = 2
	GroupStateStable              GroupState = 3
)

// MemberProtocol is one entry from a JoinGroup protocols array.
type MemberProtocol struct {
	Name     string
	Metadata []byte
}

// GroupMember holds the in-memory state for one group member.
type GroupMember struct {
	ID                 string
	ClientID           string // from the JoinGroup request header
	SessionTimeoutMs   int32
	RebalanceTimeoutMs int32
	Protocols          []MemberProtocol
}

// GroupMemberInfo is a read-only snapshot of a member for DescribeGroups.
type GroupMemberInfo struct {
	MemberID   string
	ClientID   string
	ClientHost string // empty (connection info not available in handler)
	Metadata   []byte // protocol metadata for the chosen protocol
	Assignment []byte // from syncAssignments
}

// GroupInfo is a read-only snapshot of a ConsumerGroup for DescribeGroups.
type GroupInfo struct {
	ID           string
	State        GroupState
	Protocol     string
	ProtocolType string
	LeaderID     string
	Members      []GroupMemberInfo
}

// GroupStateString returns the human-readable Kafka state name.
func (s GroupState) String() string {
	switch s {
	case GroupStateEmpty:
		return "Empty"
	case GroupStatePreparingRebalance:
		return "PreparingRebalance"
	case GroupStateCompletingRebalance:
		return "CompletingRebalance"
	case GroupStateStable:
		return "Stable"
	default:
		return "Unknown"
	}
}

// JoinGroupResult is delivered to the JoinGroup handler after the barrier fires.
type JoinGroupResult struct {
	ErrorCode    int16
	GenerationID int32
	Protocol     string
	LeaderID     string
	MemberID     string
	// Members is populated only for the group leader; nil for regular members.
	Members []GroupMember
}

// SyncGroupResult is delivered to the SyncGroup handler after the leader syncs.
type SyncGroupResult struct {
	ErrorCode  int16
	Assignment []byte
}

// ── ConsumerGroup ─────────────────────────────────────────────────────────────

// ConsumerGroup manages the state for one Kafka consumer group.
// All exported methods are safe for concurrent use.
type ConsumerGroup struct {
	mu sync.Mutex

	id            string
	state         GroupState
	generation    int32
	protocol      string // chosen assignment protocol name
	protocolType  string // e.g. "consumer"
	leaderID      string
	nextMemberSeq int

	// members holds the set of members that successfully completed the last
	// SyncGroup phase (i.e. the currently stable members).
	members map[string]*GroupMember

	// ── PreparingRebalance ────────────────────────────────────────────────────
	// joiners collects members currently blocked in JoinGroup.
	joiners        map[string]*GroupMember
	joinRespChs    map[string]chan JoinGroupResult
	rebalanceTimer *time.Timer

	// ── CompletingRebalance / Stable ──────────────────────────────────────────
	// syncWaiters holds members currently blocked in SyncGroup.
	syncWaiters     map[string]chan SyncGroupResult
	syncAssignments map[string][]byte // set by the leader; read by all members

	// ── Session timeouts ─────────────────────────────────────────────────────
	sessionTimers map[string]*time.Timer

	// ── Committed offsets ─────────────────────────────────────────────────────
	offsets map[TopicPartition]int64
}

func newConsumerGroup(id string) *ConsumerGroup {
	return &ConsumerGroup{
		id:            id,
		state:         GroupStateEmpty,
		members:       make(map[string]*GroupMember),
		joiners:       make(map[string]*GroupMember),
		joinRespChs:   make(map[string]chan JoinGroupResult),
		syncWaiters:   make(map[string]chan SyncGroupResult),
		sessionTimers: make(map[string]*time.Timer),
		offsets:       make(map[TopicPartition]int64),
	}
}

// ID returns the group identifier.
func (g *ConsumerGroup) ID() string { return g.id }

// ── JoinGroup ─────────────────────────────────────────────────────────────────

// JoinGroup adds a member to the current rebalance barrier and returns the
// assigned member ID plus a buffered channel that will receive exactly one
// JoinGroupResult when the barrier fires or an error occurs immediately.
//
// The caller must select on the returned channel and the broker context.
func (g *ConsumerGroup) JoinGroup(
	memberID, clientID, protocolType string,
	sessionTimeoutMs, rebalanceTimeoutMs int32,
	protocols []MemberProtocol,
) (string, <-chan JoinGroupResult) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Assign a member ID to new members.
	if memberID == "" {
		memberID = g.newMemberIDLocked()
	}

	// Reject while sync is in progress; the client will retry.
	if g.state == GroupStateCompletingRebalance {
		ch := make(chan JoinGroupResult, 1)
		ch <- JoinGroupResult{ErrorCode: GroupErrRebalanceInProgress, MemberID: memberID}
		return memberID, ch
	}

	// Trigger a new rebalance when the group is Empty or Stable.
	if g.state == GroupStateEmpty || g.state == GroupStateStable {
		g.startRebalanceLocked()
	}

	// Cancel any existing session timer for this member.
	if t, ok := g.sessionTimers[memberID]; ok {
		t.Stop()
		delete(g.sessionTimers, memberID)
	}

	// Record the protocol type on first join.
	if g.protocolType == "" && protocolType != "" {
		g.protocolType = protocolType
	}

	member := &GroupMember{
		ID:                 memberID,
		ClientID:           clientID,
		SessionTimeoutMs:   sessionTimeoutMs,
		RebalanceTimeoutMs: rebalanceTimeoutMs,
		Protocols:          protocols,
	}
	g.joiners[memberID] = member

	ch := make(chan JoinGroupResult, 1)
	g.joinRespChs[memberID] = ch

	// Complete the barrier if all previously-stable members have rejoined.
	if g.allMembersJoinedLocked() {
		g.completeJoinGroupLocked()
	}

	return memberID, ch
}

// startRebalanceLocked transitions the group into PreparingRebalance.
// Must be called with g.mu held.
func (g *ConsumerGroup) startRebalanceLocked() {
	// Cancel in-flight sync waiters.
	for _, ch := range g.syncWaiters {
		ch <- SyncGroupResult{ErrorCode: GroupErrRebalanceInProgress}
	}
	g.syncWaiters = make(map[string]chan SyncGroupResult)
	g.syncAssignments = nil

	// Cancel existing session timers; they restart after the next SyncGroup.
	for _, t := range g.sessionTimers {
		t.Stop()
	}
	g.sessionTimers = make(map[string]*time.Timer)

	g.state = GroupStatePreparingRebalance
	g.generation++

	// Fresh joiners pool.
	g.joiners = make(map[string]*GroupMember)
	g.joinRespChs = make(map[string]chan JoinGroupResult)

	// Start the rebalance timeout.
	if g.rebalanceTimer != nil {
		g.rebalanceTimer.Stop()
	}
	maxTimeout := g.maxRebalanceTimeoutMsLocked()
	if maxTimeout <= 0 {
		maxTimeout = 30_000
	}
	g.rebalanceTimer = time.AfterFunc(
		time.Duration(maxTimeout)*time.Millisecond,
		g.rebalanceTimerFired,
	)
}

// allMembersJoinedLocked returns true when every previously-stable member has
// re-sent JoinGroup and at least one joiner is present.
// Must be called with g.mu held.
func (g *ConsumerGroup) allMembersJoinedLocked() bool {
	if len(g.joiners) == 0 {
		return false
	}
	for memberID := range g.members {
		if _, ok := g.joiners[memberID]; !ok {
			return false
		}
	}
	return true
}

// rebalanceTimerFired is called by time.AfterFunc; it flushes the join barrier
// with however many members have joined so far.
func (g *ConsumerGroup) rebalanceTimerFired() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state == GroupStatePreparingRebalance && len(g.joinRespChs) > 0 {
		g.completeJoinGroupLocked()
	}
}

// completeJoinGroupLocked finalises the join barrier: chooses a protocol,
// elects a leader, sends JoinGroupResult to all waiting members, and advances
// state to CompletingRebalance.
// Must be called with g.mu held.
func (g *ConsumerGroup) completeJoinGroupLocked() {
	if g.rebalanceTimer != nil {
		g.rebalanceTimer.Stop()
		g.rebalanceTimer = nil
	}

	g.state = GroupStateCompletingRebalance
	g.protocol = g.chooseProtocolLocked()

	// Keep the existing leader if they rejoined; otherwise elect the
	// lexicographically smallest member ID for determinism.
	if _, ok := g.joiners[g.leaderID]; !ok {
		ids := make([]string, 0, len(g.joiners))
		for id := range g.joiners {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		g.leaderID = ids[0]
	}

	// Build the full member list for the leader response.
	allMembers := make([]GroupMember, 0, len(g.joiners))
	for _, m := range g.joiners {
		cp := *m
		// Trim to just the chosen protocol's metadata.
		cp.Protocols = nil
		for _, p := range m.Protocols {
			if p.Name == g.protocol {
				cp.Protocols = []MemberProtocol{p}
				break
			}
		}
		allMembers = append(allMembers, cp)
	}
	sort.Slice(allMembers, func(i, j int) bool {
		return allMembers[i].ID < allMembers[j].ID
	})

	// Send responses.
	for memberID, ch := range g.joinRespChs {
		res := JoinGroupResult{
			ErrorCode:    GroupErrNone,
			GenerationID: g.generation,
			Protocol:     g.protocol,
			LeaderID:     g.leaderID,
			MemberID:     memberID,
		}
		if memberID == g.leaderID {
			res.Members = allMembers
		}
		ch <- res
	}

	// The new stable member set is the joiners set.
	g.members = make(map[string]*GroupMember, len(g.joiners))
	for k, v := range g.joiners {
		g.members[k] = v
	}

	// Reset joining state.
	g.joiners = make(map[string]*GroupMember)
	g.joinRespChs = make(map[string]chan JoinGroupResult)

	// Reset sync state for the new round.
	g.syncWaiters = make(map[string]chan SyncGroupResult)
	g.syncAssignments = nil
}

// maxRebalanceTimeoutMsLocked returns the maximum rebalanceTimeoutMs across
// all current + joining members.  Must be called with g.mu held.
func (g *ConsumerGroup) maxRebalanceTimeoutMsLocked() int32 {
	var max int32
	for _, m := range g.members {
		if m.RebalanceTimeoutMs > max {
			max = m.RebalanceTimeoutMs
		}
	}
	for _, m := range g.joiners {
		if m.RebalanceTimeoutMs > max {
			max = m.RebalanceTimeoutMs
		}
	}
	return max
}

// chooseProtocolLocked returns the first protocol name supported by all
// joiners.  Falls back to the first joiner's first protocol if no intersection
// is found (which can happen with heterogeneous clients).
// Must be called with g.mu held.
func (g *ConsumerGroup) chooseProtocolLocked() string {
	if len(g.joiners) == 0 {
		return g.protocol
	}
	// Candidates from the lexicographically first joiner for determinism.
	ids := make([]string, 0, len(g.joiners))
	for id := range g.joiners {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	first := g.joiners[ids[0]]

	for _, candidate := range first.Protocols {
		allSupport := true
		for _, m := range g.joiners {
			found := false
			for _, p := range m.Protocols {
				if p.Name == candidate.Name {
					found = true
					break
				}
			}
			if !found {
				allSupport = false
				break
			}
		}
		if allSupport {
			return candidate.Name
		}
	}
	// Fallback.
	if len(first.Protocols) > 0 {
		return first.Protocols[0].Name
	}
	return ""
}

// newMemberIDLocked generates a unique member ID.
// Must be called with g.mu held.
func (g *ConsumerGroup) newMemberIDLocked() string {
	g.nextMemberSeq++
	return fmt.Sprintf("%s-member-%d", g.id, g.nextMemberSeq)
}

// ── Info (snapshot for DescribeGroups) ───────────────────────────────────────

// Info returns a read-only snapshot of the group's current state.
func (g *ConsumerGroup) Info() GroupInfo {
	g.mu.Lock()
	defer g.mu.Unlock()

	info := GroupInfo{
		ID:           g.id,
		State:        g.state,
		Protocol:     g.protocol,
		ProtocolType: g.protocolType,
		LeaderID:     g.leaderID,
	}

	for _, m := range g.members {
		mi := GroupMemberInfo{
			MemberID: m.ID,
			ClientID: m.ClientID,
		}
		for _, p := range m.Protocols {
			if p.Name == g.protocol {
				mi.Metadata = p.Metadata
				break
			}
		}
		if g.syncAssignments != nil {
			mi.Assignment = g.syncAssignments[m.ID]
		}
		info.Members = append(info.Members, mi)
	}

	return info
}

// ── SyncGroup ─────────────────────────────────────────────────────────────────

// SyncGroup records a member's sync request and returns a buffered channel
// that will receive exactly one SyncGroupResult.
//
// The leader must pass a non-nil assignments map; non-leaders pass nil.
// The caller must select on the returned channel and the broker context.
func (g *ConsumerGroup) SyncGroup(
	memberID string,
	generationID int32,
	assignments map[string][]byte,
) <-chan SyncGroupResult {
	g.mu.Lock()
	defer g.mu.Unlock()

	errCh := func(code int16) <-chan SyncGroupResult {
		ch := make(chan SyncGroupResult, 1)
		ch <- SyncGroupResult{ErrorCode: code}
		return ch
	}

	if _, ok := g.members[memberID]; !ok {
		return errCh(GroupErrUnknownMemberID)
	}
	if generationID != g.generation {
		return errCh(GroupErrIllegalGeneration)
	}

	switch g.state {
	case GroupStatePreparingRebalance:
		return errCh(GroupErrRebalanceInProgress)
	case GroupStateStable:
		// Late arrival: assignments are already available.
		ch := make(chan SyncGroupResult, 1)
		ch <- SyncGroupResult{Assignment: g.syncAssignments[memberID]}
		return ch
	case GroupStateCompletingRebalance:
		// Normal path.
	default:
		return errCh(GroupErrRebalanceInProgress)
	}

	ch := make(chan SyncGroupResult, 1)
	g.syncWaiters[memberID] = ch

	// If this is the leader and they provided assignments, complete immediately.
	if memberID == g.leaderID && assignments != nil {
		g.completeSyncLocked(assignments)
	}

	return ch
}

// completeSyncLocked distributes assignments to all sync waiters and advances
// state to Stable.  Must be called with g.mu held.
func (g *ConsumerGroup) completeSyncLocked(assignments map[string][]byte) {
	g.syncAssignments = assignments
	g.state = GroupStateStable

	for memberID, ch := range g.syncWaiters {
		ch <- SyncGroupResult{Assignment: assignments[memberID]}
		g.startSessionTimerLocked(memberID)
	}
	g.syncWaiters = make(map[string]chan SyncGroupResult)
}

// startSessionTimerLocked (re-)arms the session timer for memberID.
// Must be called with g.mu held.
func (g *ConsumerGroup) startSessionTimerLocked(memberID string) {
	if t, ok := g.sessionTimers[memberID]; ok {
		t.Stop()
	}
	member, ok := g.members[memberID]
	if !ok {
		return
	}
	timeout := time.Duration(member.SessionTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	g.sessionTimers[memberID] = time.AfterFunc(timeout, func() {
		g.sessionTimerFired(memberID)
	})
}

func (g *ConsumerGroup) sessionTimerFired(memberID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Only evict from the Stable state; other states handle their own cleanup.
	if g.state != GroupStateStable {
		return
	}
	if _, ok := g.members[memberID]; !ok {
		return // already removed
	}
	delete(g.members, memberID)
	if t, ok := g.sessionTimers[memberID]; ok {
		t.Stop()
		delete(g.sessionTimers, memberID)
	}

	if len(g.members) == 0 {
		g.state = GroupStateEmpty
		g.leaderID = ""
	} else {
		g.startRebalanceLocked()
	}
}

// ── Heartbeat ─────────────────────────────────────────────────────────────────

// Heartbeat validates the member's session and resets its session timer.
// Returns a Kafka error code: 0 on success.
func (g *ConsumerGroup) Heartbeat(memberID string, generationID int32) int16 {
	g.mu.Lock()
	defer g.mu.Unlock()

	switch g.state {
	case GroupStatePreparingRebalance:
		// A member in the previous stable set is still recognised; tell them to rejoin.
		if _, ok := g.members[memberID]; ok {
			return GroupErrRebalanceInProgress
		}
		// Check joiners too.
		if _, ok := g.joiners[memberID]; ok {
			return GroupErrRebalanceInProgress
		}
		return GroupErrUnknownMemberID

	case GroupStateCompletingRebalance:
		if _, ok := g.members[memberID]; !ok {
			return GroupErrUnknownMemberID
		}
		return GroupErrRebalanceInProgress

	case GroupStateStable:
		if _, ok := g.members[memberID]; !ok {
			return GroupErrUnknownMemberID
		}
		if generationID != g.generation {
			return GroupErrIllegalGeneration
		}
		g.startSessionTimerLocked(memberID)
		return GroupErrNone

	default: // Empty
		return GroupErrUnknownMemberID
	}
}

// ── LeaveGroup ────────────────────────────────────────────────────────────────

// LeaveGroup removes a member from the group.
// Returns 0 on success, or a Kafka error code.
func (g *ConsumerGroup) LeaveGroup(memberID string) int16 {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Cancel session timer.
	if t, ok := g.sessionTimers[memberID]; ok {
		t.Stop()
		delete(g.sessionTimers, memberID)
	}

	// If the member is in the current joiners (PreparingRebalance), remove them.
	if _, inJoiners := g.joiners[memberID]; inJoiners {
		delete(g.joiners, memberID)
		if ch, ok := g.joinRespChs[memberID]; ok {
			ch <- JoinGroupResult{ErrorCode: GroupErrUnknownMemberID, MemberID: memberID}
			delete(g.joinRespChs, memberID)
		}
		// Do not trigger another rebalance here; the ongoing one will complete
		// without this member when the timer fires or allMembersJoined is met.
		return GroupErrNone
	}

	if _, inMembers := g.members[memberID]; !inMembers {
		return GroupErrUnknownMemberID
	}

	delete(g.members, memberID)

	if len(g.members) == 0 {
		// Group is now empty; reset to Empty state.
		g.state = GroupStateEmpty
		g.leaderID = ""
		g.generation = 0
		if g.rebalanceTimer != nil {
			g.rebalanceTimer.Stop()
			g.rebalanceTimer = nil
		}
		// Cancel sync waiters.
		for _, ch := range g.syncWaiters {
			ch <- SyncGroupResult{ErrorCode: GroupErrRebalanceInProgress}
		}
		g.syncWaiters = make(map[string]chan SyncGroupResult)
		g.syncAssignments = nil
	} else if g.state == GroupStateStable {
		g.startRebalanceLocked()
	}

	return GroupErrNone
}

// ── OffsetCommit / OffsetFetch ────────────────────────────────────────────────

// CommitOffsets stores committed offsets for the group.
// generationID == -1 skips member / generation validation (simple consumer).
func (g *ConsumerGroup) CommitOffsets(
	generationID int32,
	memberID string,
	offsets map[TopicPartition]int64,
) int16 {
	g.mu.Lock()
	defer g.mu.Unlock()

	if generationID >= 0 {
		if g.state != GroupStateStable {
			return GroupErrRebalanceInProgress
		}
		if _, ok := g.members[memberID]; !ok {
			return GroupErrUnknownMemberID
		}
		if generationID != g.generation {
			return GroupErrIllegalGeneration
		}
	}

	for tp, offset := range offsets {
		g.offsets[tp] = offset
	}
	return GroupErrNone
}

// FetchOffsets returns committed offsets.
// If partitions is nil, all committed offsets are returned.
// Uncommitted partitions are returned with offset -1.
func (g *ConsumerGroup) FetchOffsets(partitions []TopicPartition) map[TopicPartition]int64 {
	g.mu.Lock()
	defer g.mu.Unlock()

	if partitions == nil {
		result := make(map[TopicPartition]int64, len(g.offsets))
		for k, v := range g.offsets {
			result[k] = v
		}
		return result
	}

	result := make(map[TopicPartition]int64, len(partitions))
	for _, tp := range partitions {
		if offset, ok := g.offsets[tp]; ok {
			result[tp] = offset
		} else {
			result[tp] = -1 // not committed
		}
	}
	return result
}
