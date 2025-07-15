package kgo

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo/internal/sticky"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// GroupBalancer balances topics and partitions among group members.
//
// A GroupBalancer is roughly equivalent to Kafka's PartitionAssignor.
type GroupBalancer interface {
	// ProtocolName returns the name of the protocol, e.g. roundrobin,
	// range, sticky.
	ProtocolName() string

	// JoinGroupMetadata returns the metadata to use in JoinGroup, given
	// the topic interests and the current assignment and group generation.
	//
	// It is safe to modify the input topics and currentAssignment. The
	// input topics are guaranteed to be sorted, as are the partitions for
	// each topic in currentAssignment. It is recommended for your output
	// to be ordered by topic and partitions. Since Kafka uses the output
	// from this function to determine whether a rebalance is needed, a
	// deterministic output will avoid accidental rebalances.
	JoinGroupMetadata(
		topicInterests []string,
		currentAssignment map[string][]int32,
		generation int32,
	) []byte

	// ParseSyncAssignment returns assigned topics and partitions from an
	// encoded SyncGroupResponse's MemberAssignment.
	ParseSyncAssignment(assignment []byte) (map[string][]int32, error)

	// MemberBalancer returns a GroupMemberBalancer for the given group
	// members, as well as the topics that all the members are interested
	// in. If the client does not have some topics in the returned topics,
	// the client issues a metadata request to load the number of
	// partitions in those topics before calling the GroupMemberBalancer's
	// Balance function.
	//
	// The input group members are guaranteed to be sorted first by
	// instance ID, if non-nil, and then by member ID.
	//
	// It is up to the user to decide how to decode each member's
	// ProtocolMetadata field. The default client group protocol of
	// "consumer" by default uses join group metadata's of type
	// kmsg.ConsumerMemberMetadata. If this is the case for you, it may be
	// useful to use the ConsumerBalancer type to help parse the metadata
	// and balance.
	//
	// If the member metadata cannot be deserialized correctly, this should
	// return a relevant error.
	MemberBalancer(members []kmsg.JoinGroupResponseMember) (b GroupMemberBalancer, topics map[string]struct{}, err error)

	// IsCooperative returns if this is a cooperative balance strategy.
	IsCooperative() bool
}

// GroupMemberBalancer balances topics amongst group members. If your balancing
// can fail, you can implement GroupMemberBalancerOrError.
type GroupMemberBalancer interface {
	// Balance balances topics and partitions among group members, where
	// the int32 in the topics map corresponds to the number of partitions
	// known to be in each topic.
	Balance(topics map[string]int32) IntoSyncAssignment
}

// GroupMemberBalancerOrError is an optional extension interface for
// GroupMemberBalancer. This can be implemented if your balance function can
// fail.
//
// For interface purposes, it is required to implement GroupMemberBalancer, but
// Balance will never be called.
type GroupMemberBalancerOrError interface {
	GroupMemberBalancer
	BalanceOrError(topics map[string]int32) (IntoSyncAssignment, error)
}

// IntoSyncAssignment takes a balance plan and returns a list of assignments to
// use in a kmsg.SyncGroupRequest.
//
// It is recommended to ensure the output is deterministic and ordered by
// member / topic / partitions.
type IntoSyncAssignment interface {
	IntoSyncAssignment() []kmsg.SyncGroupRequestGroupAssignment
}

// ConsumerBalancer is a helper type for writing balance plans that use the
// "consumer" protocol, such that each member uses a kmsg.ConsumerMemberMetadata
// in its join group request.
type ConsumerBalancer struct {
	b         ConsumerBalancerBalance
	members   []kmsg.JoinGroupResponseMember
	metadatas []kmsg.ConsumerMemberMetadata
	topics    map[string]struct{}

	err error
}

// Balance satisfies the GroupMemberBalancer interface, but is never called
// because GroupMemberBalancerOrError exists.
func (*ConsumerBalancer) Balance(map[string]int32) IntoSyncAssignment {
	panic("unreachable")
}

// BalanceOrError satisfies the GroupMemberBalancerOrError interface.
func (b *ConsumerBalancer) BalanceOrError(topics map[string]int32) (IntoSyncAssignment, error) {
	return b.b.Balance(b, topics), b.err
}

// Members returns the list of input members for this group balancer.
func (b *ConsumerBalancer) Members() []kmsg.JoinGroupResponseMember {
	return b.members
}

// EachMember calls fn for each member and its corresponding metadata in the
// consumer group being balanced.
func (b *ConsumerBalancer) EachMember(fn func(member *kmsg.JoinGroupResponseMember, meta *kmsg.ConsumerMemberMetadata)) {
	for i := range b.members {
		fn(&b.members[i], &b.metadatas[i])
	}
}

// MemberAt returns the nth member and its corresponding metadata.
func (b *ConsumerBalancer) MemberAt(n int) (*kmsg.JoinGroupResponseMember, *kmsg.ConsumerMemberMetadata) {
	return &b.members[n], &b.metadatas[n]
}

// SetError allows you to set any error that occurred while balancing. This
// allows you to fail balancing and return nil from Balance.
func (b *ConsumerBalancer) SetError(err error) {
	b.err = err
}

// MemberTopics returns the unique set of topics that all members are
// interested in.
//
// This can safely be called if the balancer is nil; if so, this will return
// nil.
func (b *ConsumerBalancer) MemberTopics() map[string]struct{} {
	if b == nil {
		return nil
	}
	return b.topics
}

// NewPlan returns a type that can be used to build a balance plan. The return
// satisfies the IntoSyncAssignment interface.
func (b *ConsumerBalancer) NewPlan() *BalancePlan {
	plan := make(map[string]map[string][]int32, len(b.members))
	for i := range b.members {
		plan[b.members[i].MemberID] = make(map[string][]int32)
	}
	return &BalancePlan{plan}
}

// ConsumerBalancerBalance is what the ConsumerBalancer invokes to balance a
// group.
//
// This is a complicated interface, but in short, this interface has one
// function that implements the actual balancing logic: using the input
// balancer, balance the input topics and partitions. If your balancing can
// fail, you can use ConsumerBalancer.SetError(...) to return an error from
// balancing, and then you can simply return nil from Balance.
type ConsumerBalancerBalance interface {
	Balance(*ConsumerBalancer, map[string]int32) IntoSyncAssignment
}

// ParseConsumerSyncAssignment returns an assignment as specified a
// kmsg.ConsumerMemberAssignment, that is, the type encoded in metadata for the
// consumer protocol.
func ParseConsumerSyncAssignment(assignment []byte) (map[string][]int32, error) {
	var kassignment kmsg.ConsumerMemberAssignment
	if err := kassignment.ReadFrom(assignment); err != nil {
		return nil, fmt.Errorf("sync assignment parse failed: %v", err)
	}

	m := make(map[string][]int32, len(kassignment.Topics))
	for _, topic := range kassignment.Topics {
		m[topic.Topic] = topic.Partitions
	}
	return m, nil
}

// NewConsumerBalancer parses the each member's metadata as a
// kmsg.ConsumerMemberMetadata and returns a ConsumerBalancer to use in balancing.
//
// If any metadata parsing fails, this returns an error.
func NewConsumerBalancer(balance ConsumerBalancerBalance, members []kmsg.JoinGroupResponseMember) (*ConsumerBalancer, error) {
	b := &ConsumerBalancer{
		b:         balance,
		members:   members,
		metadatas: make([]kmsg.ConsumerMemberMetadata, len(members)),
		topics:    make(map[string]struct{}),
	}

	for i, member := range members {
		meta := &b.metadatas[i]
		meta.Default()
		memberMeta := member.ProtocolMetadata
		if err := meta.ReadFrom(memberMeta); err != nil {
			// Some buggy clients claimed support for v1 but then
			// did not add OwnedPartitions, resulting in a short
			// metadata. If we fail at reading and the version is
			// v1, we retry again as v0. We do not support other
			// versions because hopefully other clients stop
			// claiming higher and higher version support and not
			// actually supporting them. Sarama has a similarish
			// workaround. See #493.
			if bytes.HasPrefix(memberMeta, []byte{0, 1}) {
				memberMeta[0] = 0
				memberMeta[1] = 0
				if err = meta.ReadFrom(memberMeta); err != nil {
					return nil, fmt.Errorf("unable to read member metadata: %v", err)
				}
			}
		}
		for _, topic := range meta.Topics {
			b.topics[topic] = struct{}{}
		}
		sort.Strings(meta.Topics)
	}

	return b, nil
}

// BalancePlan is a helper type to build the result of balancing topics
// and partitions among group members.
type BalancePlan struct {
	plan map[string]map[string][]int32 // member => topic => partitions
}

// AsMemberIDMap returns the plan as a map of member IDs to their topic &
// partition assignments.
//
// Internally, a BalancePlan is currently represented as this map. Any
// modification to the map modifies the plan. The internal representation of a
// plan may change in the future to include more metadata. If this happens, the
// map returned from this function may not represent all aspects of a plan.
// The client will attempt to mirror modifications to the map directly back
// into the underlying plan as best as possible.
func (p *BalancePlan) AsMemberIDMap() map[string]map[string][]int32 {
	return p.plan
}

func (p *BalancePlan) String() string {
	var sb strings.Builder

	var membersWritten int
	for member, topics := range p.plan {
		membersWritten++
		sb.WriteString(member)
		sb.WriteString("{")

		var topicsWritten int
		for topic, partitions := range topics {
			fmt.Fprintf(&sb, "%s%v", topic, partitions)
			topicsWritten++
			if topicsWritten < len(topics) {
				sb.WriteString(", ")
			}
		}

		sb.WriteString("}")
		if membersWritten < len(p.plan) {
			sb.WriteString(", ")
		}
	}

	return sb.String()
}

// AddPartition assigns a partition for the topic to a given member.
func (p *BalancePlan) AddPartition(member *kmsg.JoinGroupResponseMember, topic string, partition int32) {
	memberPlan := p.plan[member.MemberID]
	memberPlan[topic] = append(memberPlan[topic], partition)
}

// AddPartitions assigns many partitions for a topic to a given member.
func (p *BalancePlan) AddPartitions(member *kmsg.JoinGroupResponseMember, topic string, partitions []int32) {
	memberPlan := p.plan[member.MemberID]
	memberPlan[topic] = append(memberPlan[topic], partitions...)
}

// IntoSyncAssignment satisfies the IntoSyncAssignment interface.
func (p *BalancePlan) IntoSyncAssignment() []kmsg.SyncGroupRequestGroupAssignment {
	kassignments := make([]kmsg.SyncGroupRequestGroupAssignment, 0, len(p.plan))
	for member, assignment := range p.plan {
		var kassignment kmsg.ConsumerMemberAssignment
		for topic, partitions := range assignment {
			sort.Slice(partitions, func(i, j int) bool { return partitions[i] < partitions[j] })
			assnTopic := kmsg.NewConsumerMemberAssignmentTopic()
			assnTopic.Topic = topic
			assnTopic.Partitions = partitions
			kassignment.Topics = append(kassignment.Topics, assnTopic)
		}
		sort.Slice(kassignment.Topics, func(i, j int) bool { return kassignment.Topics[i].Topic < kassignment.Topics[j].Topic })
		syncAssn := kmsg.NewSyncGroupRequestGroupAssignment()
		syncAssn.MemberID = member
		syncAssn.MemberAssignment = kassignment.AppendTo(nil)
		kassignments = append(kassignments, syncAssn)
	}
	sort.Slice(kassignments, func(i, j int) bool { return kassignments[i].MemberID < kassignments[j].MemberID })
	return kassignments
}

func joinMemberLess(l, r *kmsg.JoinGroupResponseMember) bool {
	if l.InstanceID != nil {
		if r.InstanceID == nil {
			return true
		}
		return *l.InstanceID < *r.InstanceID
	}
	if r.InstanceID != nil {
		return false
	}
	return l.MemberID < r.MemberID
}

func sortJoinMembers(members []kmsg.JoinGroupResponseMember) {
	sort.Slice(members, func(i, j int) bool { return joinMemberLess(&members[i], &members[j]) })
}

func sortJoinMemberPtrs(members []*kmsg.JoinGroupResponseMember) {
	sort.Slice(members, func(i, j int) bool { return joinMemberLess(members[i], members[j]) })
}

func (g *groupConsumer) findBalancer(from, proto string) (GroupBalancer, error) {
	for _, b := range g.cfg.balancers {
		if b.ProtocolName() == proto {
			return b, nil
		}
	}
	var ours []string
	for _, b := range g.cfg.balancers {
		ours = append(ours, b.ProtocolName())
	}
	g.cl.cfg.logger.Log(LogLevelError, fmt.Sprintf("%s could not find broker-chosen balancer", from), "kafka_choice", proto, "our_set", strings.Join(ours, ", "))
	return nil, fmt.Errorf("unable to balance: none of our balancers have a name equal to the balancer chosen for balancing (%s)", proto)
}

// balanceGroup returns a balancePlan from a join group response.
//
// If the group has topics this leader does not want to consume, this also
// returns all topics and partitions; the leader will then periodically do its
// own metadata update to see if partition counts have changed for these random
// topics.
func (g *groupConsumer) balanceGroup(proto string, members []kmsg.JoinGroupResponseMember, skipBalance bool) ([]kmsg.SyncGroupRequestGroupAssignment, error) {
	g.cl.cfg.logger.Log(LogLevelInfo, "balancing group as leader")

	b, err := g.findBalancer("balance group", proto)
	if err != nil {
		return nil, err
	}

	sortJoinMembers(members)

	memberBalancer, topics, err := b.MemberBalancer(members)
	if err != nil {
		return nil, fmt.Errorf("unable to create group member balancer: %v", err)
	}

	myTopics := g.tps.load()
	var needMeta bool
	topicPartitionCount := make(map[string]int32, len(topics))
	for topic := range topics {
		data, exists := myTopics[topic]
		if !exists {
			needMeta = true
			continue
		}
		topicPartitionCount[topic] = int32(len(data.load().partitions))
	}

	// If our consumer metadata does not contain all topics, the group is
	// expressing interests in topics we are not consuming. Perhaps we have
	// those topics saved in our external topics map.
	if needMeta {
		g.loadExternal().fn(func(m map[string]int32) {
			needMeta = false
			for topic := range topics {
				partitions, exists := m[topic]
				if !exists {
					needMeta = true
					continue
				}
				topicPartitionCount[topic] = partitions
			}
		})
	}

	if needMeta {
		g.cl.cfg.logger.Log(LogLevelInfo, "group members indicated interest in topics the leader is not assigned, fetching metadata for all group topics")
		var metaTopics []string
		for topic := range topics {
			metaTopics = append(metaTopics, topic)
		}

		_, resp, err := g.cl.fetchMetadataForTopics(g.ctx, false, metaTopics)
		if err != nil {
			return nil, fmt.Errorf("unable to fetch metadata for group topics: %v", err)
		}
		for i := range resp.Topics {
			t := &resp.Topics[i]
			if t.Topic == nil {
				g.cl.cfg.logger.Log(LogLevelWarn, "metadata resp in balance for topic has nil topic, skipping...", "err", kerr.ErrorForCode(t.ErrorCode))
				continue
			}
			if t.ErrorCode != 0 {
				g.cl.cfg.logger.Log(LogLevelWarn, "metadata resp in balance for topic has error, skipping...", "topic", t.Topic, "err", kerr.ErrorForCode(t.ErrorCode))
				continue
			}
			topicPartitionCount[*t.Topic] = int32(len(t.Partitions))
		}

		g.initExternal(topicPartitionCount)
	}

	// If the returned balancer is a ConsumerBalancer (which it likely
	// always will be), then we can print some useful debugging information
	// about what member interests are.
	if b, ok := memberBalancer.(*ConsumerBalancer); ok {
		interests := new(bytes.Buffer)
		b.EachMember(func(member *kmsg.JoinGroupResponseMember, meta *kmsg.ConsumerMemberMetadata) {
			interests.Reset()
			fmt.Fprintf(interests, "interested topics: %v, previously owned: ", meta.Topics)
			for _, owned := range meta.OwnedPartitions {
				sort.Slice(owned.Partitions, func(i, j int) bool { return owned.Partitions[i] < owned.Partitions[j] })
				fmt.Fprintf(interests, "%s%v, ", owned.Topic, owned.Partitions)
			}
			strInterests := interests.String()
			strInterests = strings.TrimSuffix(strInterests, ", ")

			if member.InstanceID == nil {
				g.cl.cfg.logger.Log(LogLevelInfo, "balance group member", "id", member.MemberID, "interests", strInterests)
			} else {
				g.cl.cfg.logger.Log(LogLevelInfo, "balance group member", "id", member.MemberID, "instance_id", *member.InstanceID, "interests", strInterests)
			}
		})
	} else {
		g.cl.cfg.logger.Log(LogLevelInfo, "unable to log information about group member interests: the user has defined a custom balancer (not a *ConsumerBalancer)")
	}

	// KIP-814: we are leader and we know what the entire group is
	// consuming. Crucially, we parsed topics that we are potentially not
	// interested in and are now tracking them for metadata updates. We
	// have logged the current interests, we do not need to actually
	// balance.
	if skipBalance {
		switch proto := b.ProtocolName(); proto {
		case RangeBalancer().ProtocolName(),
			RoundRobinBalancer().ProtocolName(),
			StickyBalancer().ProtocolName(),
			CooperativeStickyBalancer().ProtocolName():
		default:
			return nil, nil
		}
	}

	// If the returned IntoSyncAssignment is a BalancePlan, which it likely
	// is if the balancer is a ConsumerBalancer, then we can again print
	// more useful debugging information.
	var into IntoSyncAssignment
	if memberBalancerOrErr, ok := memberBalancer.(GroupMemberBalancerOrError); ok {
		if into, err = memberBalancerOrErr.BalanceOrError(topicPartitionCount); err != nil {
			g.cl.cfg.logger.Log(LogLevelError, "balance failed", "err", err)
			return nil, err
		}
	} else {
		into = memberBalancer.Balance(topicPartitionCount)
	}

	if p, ok := into.(*BalancePlan); ok {
		g.cl.cfg.logger.Log(LogLevelInfo, "balanced", "plan", p.String())
	} else {
		g.cl.cfg.logger.Log(LogLevelInfo, "unable to log balance plan: the user has returned a custom IntoSyncAssignment (not a *BalancePlan)")
	}

	return into.IntoSyncAssignment(), nil
}

// helper func; range and roundrobin use v0
func simpleMemberMetadata(interests []string, generation int32) []byte {
	meta := kmsg.NewConsumerMemberMetadata()
	meta.Version = 3        // BUMP ME WHEN NEW FIELDS ARE ADDED, AND BUMP BELOW
	meta.Topics = interests // input interests are already sorted
	// meta.OwnedPartitions is nil, since simple protocols are not cooperative
	meta.Generation = generation
	return meta.AppendTo(nil)
}

///////////////////
// Balance Plans //
///////////////////

// RoundRobinBalancer returns a group balancer that evenly maps topics and
// partitions to group members.
//
// Suppose there are two members M0 and M1, two topics t0 and t1, and each
// topic has three partitions p0, p1, and p2. The partition balancing will be
//
//	M0: [t0p0, t0p2, t1p1]
//	M1: [t0p1, t1p0, t1p2]
//
// If all members subscribe to all topics equally, the roundrobin balancer
// will give a perfect balance. However, if topic subscriptions are quite
// unequal, the roundrobin balancer may lead to a bad balance. See KIP-49
// for one example (note that the fair strategy mentioned in KIP-49 does
// not exist).
//
// This is equivalent to the Java roundrobin balancer.
func RoundRobinBalancer() GroupBalancer {
	return new(roundRobinBalancer)
}

type roundRobinBalancer struct{}

func (*roundRobinBalancer) ProtocolName() string { return "roundrobin" }
func (*roundRobinBalancer) IsCooperative() bool  { return false }
func (*roundRobinBalancer) JoinGroupMetadata(interests []string, _ map[string][]int32, generation int32) []byte {
	return simpleMemberMetadata(interests, generation)
}

func (*roundRobinBalancer) ParseSyncAssignment(assignment []byte) (map[string][]int32, error) {
	return ParseConsumerSyncAssignment(assignment)
}

func (r *roundRobinBalancer) MemberBalancer(members []kmsg.JoinGroupResponseMember) (GroupMemberBalancer, map[string]struct{}, error) {
	b, err := NewConsumerBalancer(r, members)
	return b, b.MemberTopics(), err
}

func (*roundRobinBalancer) Balance(b *ConsumerBalancer, topics map[string]int32) IntoSyncAssignment {
	type topicPartition struct {
		topic     string
		partition int32
	}
	var nparts int
	for _, partitions := range topics {
		nparts += int(partitions)
	}
	// Order all partitions available to balance, filtering out those that
	// no members are subscribed to.
	allParts := make([]topicPartition, 0, nparts)
	for topic := range b.MemberTopics() {
		for partition := int32(0); partition < topics[topic]; partition++ {
			allParts = append(allParts, topicPartition{
				topic,
				partition,
			})
		}
	}
	sort.Slice(allParts, func(i, j int) bool {
		l, r := allParts[i], allParts[j]
		return l.topic < r.topic || l.topic == r.topic && l.partition < r.partition
	})

	plan := b.NewPlan()
	// While parts are unassigned, assign them.
	var memberIdx int
	for len(allParts) > 0 {
		next := allParts[0]
		allParts = allParts[1:]

		// The Java roundrobin strategy walks members circularly until
		// a member can take this partition, and then starts the next
		// partition where the circular iterator left off.
	assigned:
		for {
			member, meta := b.MemberAt(memberIdx)
			memberIdx = (memberIdx + 1) % len(b.Members())
			for _, topic := range meta.Topics {
				if topic == next.topic {
					plan.AddPartition(member, next.topic, next.partition)
					break assigned
				}
			}
		}
	}

	return plan
}

// RangeBalancer returns a group balancer that, per topic, maps partitions to
// group members. Since this works on a topic level, uneven partitions per
// topic to the number of members can lead to slight partition consumption
// disparities.
//
// Suppose there are two members M0 and M1, two topics t0 and t1, and each
// topic has three partitions p0, p1, and p2. The partition balancing will be
//
//	M0: [t0p0, t0p1, t1p0, t1p1]
//	M1: [t0p2, t1p2]
//
// This is equivalent to the Java range balancer.
func RangeBalancer() GroupBalancer {
	return new(rangeBalancer)
}

type rangeBalancer struct{}

func (*rangeBalancer) ProtocolName() string { return "range" }
func (*rangeBalancer) IsCooperative() bool  { return false }
func (*rangeBalancer) JoinGroupMetadata(interests []string, _ map[string][]int32, generation int32) []byte {
	return simpleMemberMetadata(interests, generation)
}

func (*rangeBalancer) ParseSyncAssignment(assignment []byte) (map[string][]int32, error) {
	return ParseConsumerSyncAssignment(assignment)
}

func (r *rangeBalancer) MemberBalancer(members []kmsg.JoinGroupResponseMember) (GroupMemberBalancer, map[string]struct{}, error) {
	b, err := NewConsumerBalancer(r, members)
	return b, b.MemberTopics(), err
}

func (*rangeBalancer) Balance(b *ConsumerBalancer, topics map[string]int32) IntoSyncAssignment {
	topics2PotentialConsumers := make(map[string][]*kmsg.JoinGroupResponseMember)
	b.EachMember(func(member *kmsg.JoinGroupResponseMember, meta *kmsg.ConsumerMemberMetadata) {
		for _, topic := range meta.Topics {
			topics2PotentialConsumers[topic] = append(topics2PotentialConsumers[topic], member)
		}
	})

	plan := b.NewPlan()
	for topic, potentialConsumers := range topics2PotentialConsumers {
		sortJoinMemberPtrs(potentialConsumers)

		numPartitions := topics[topic]
		partitions := make([]int32, numPartitions)
		for i := range partitions {
			partitions[i] = int32(i)
		}
		numParts := len(partitions)
		div, rem := numParts/len(potentialConsumers), numParts%len(potentialConsumers)

		var consumerIdx int
		for len(partitions) > 0 {
			num := div
			if rem > 0 {
				num++
				rem--
			}

			member := potentialConsumers[consumerIdx]
			plan.AddPartitions(member, topic, partitions[:num])

			consumerIdx++
			partitions = partitions[num:]
		}
	}

	return plan
}

// StickyBalancer returns a group balancer that ensures minimal partition
// movement on group changes while also ensuring optimal balancing.
//
// Suppose there are three members M0, M1, and M2, and two topics t0 and t1
// each with three partitions p0, p1, and p2. If the initial balance plan looks
// like
//
//	M0: [t0p0, t0p1, t0p2]
//	M1: [t1p0, t1p1, t1p2]
//	M2: [t2p0, t2p2, t2p2]
//
// If M2 disappears, both roundrobin and range would have mostly destructive
// reassignments.
//
// Range would result in
//
//	M0: [t0p0, t0p1, t1p0, t1p1, t2p0, t2p1]
//	M1: [t0p2, t1p2, t2p2]
//
// which is imbalanced and has 3 partitions move from members that did not need
// to move (t0p2, t1p0, t1p1).
//
// RoundRobin would result in
//
//	M0: [t0p0, t0p2, t1p1, t2p0, t2p2]
//	M1: [t0p1, t1p0, t1p2, t2p1]
//
// which is balanced, but has 2 partitions move when they do not need to
// (t0p1, t1p1).
//
// Sticky balancing results in
//
//	M0: [t0p0, t0p1, t0p2, t2p0, t2p2]
//	M1: [t1p0, t1p1, t1p2, t2p1]
//
// which is balanced and does not cause any unnecessary partition movement.
// The actual t2 partitions may not be in that exact combination, but they
// will be balanced.
//
// An advantage of the sticky consumer is that it allows API users to
// potentially avoid some cleanup until after the consumer knows which
// partitions it is losing when it gets its new assignment. Users can
// then only cleanup state for partitions that changed, which will be
// minimal (see KIP-54; this client also includes the KIP-351 bugfix).
//
// Note that this API implements the sticky partitioning quite differently from
// the Java implementation. The Java implementation is difficult to reason
// about and has many edge cases that result in non-optimal balancing (albeit,
// you likely have to be trying to hit those edge cases). This API uses a
// different algorithm to ensure optimal balancing while being an order of
// magnitude faster.
//
// Since the new strategy is a strict improvement over the Java strategy, it is
// entirely compatible. Any Go client sharing a group with a Java client will
// not have its decisions undone on leadership change from a Go consumer to a
// Java one. Java balancers do not apply the strategy it comes up with if it
// deems the balance score equal to or worse than the original score (the score
// being effectively equal to the standard deviation of the mean number of
// assigned partitions). This Go sticky balancer is optimal and extra sticky.
// Thus, the Java balancer will never back out of a strategy from this
// balancer.
func StickyBalancer() GroupBalancer {
	return &stickyBalancer{cooperative: false}
}

type stickyBalancer struct {
	cooperative bool
}

func (s *stickyBalancer) ProtocolName() string {
	if s.cooperative {
		return "cooperative-sticky"
	}
	return "sticky"
}
func (s *stickyBalancer) IsCooperative() bool { return s.cooperative }
func (s *stickyBalancer) JoinGroupMetadata(interests []string, currentAssignment map[string][]int32, generation int32) []byte {
	meta := kmsg.NewConsumerMemberMetadata()
	meta.Version = 3 // BUMP ME WHEN NEW FIELDS ARE ADDED, AND BUMP ABOVE
	meta.Topics = interests
	meta.Generation = generation
	stickyMeta := kmsg.NewStickyMemberMetadata()
	stickyMeta.Generation = generation
	for topic, partitions := range currentAssignment {
		if s.cooperative {
			metaPart := kmsg.NewConsumerMemberMetadataOwnedPartition()
			metaPart.Topic = topic
			metaPart.Partitions = partitions
			meta.OwnedPartitions = append(meta.OwnedPartitions, metaPart)
		}
		stickyAssn := kmsg.NewStickyMemberMetadataCurrentAssignment()
		stickyAssn.Topic = topic
		stickyAssn.Partitions = partitions
		stickyMeta.CurrentAssignment = append(stickyMeta.CurrentAssignment, stickyAssn)
	}

	// KAFKA-12898: ensure our topics are sorted
	metaOwned := meta.OwnedPartitions
	stickyCurrent := stickyMeta.CurrentAssignment
	sort.Slice(metaOwned, func(i, j int) bool { return metaOwned[i].Topic < metaOwned[j].Topic })
	sort.Slice(stickyCurrent, func(i, j int) bool { return stickyCurrent[i].Topic < stickyCurrent[j].Topic })

	meta.UserData = stickyMeta.AppendTo(nil)
	return meta.AppendTo(nil)
}

func (*stickyBalancer) ParseSyncAssignment(assignment []byte) (map[string][]int32, error) {
	return ParseConsumerSyncAssignment(assignment)
}

func (s *stickyBalancer) MemberBalancer(members []kmsg.JoinGroupResponseMember) (GroupMemberBalancer, map[string]struct{}, error) {
	b, err := NewConsumerBalancer(s, members)
	return b, b.MemberTopics(), err
}

func (s *stickyBalancer) Balance(b *ConsumerBalancer, topics map[string]int32) IntoSyncAssignment {
	// Since our input into balancing is already sorted by instance ID,
	// the sticky strategy does not need to worry about instance IDs at all.
	// See my (slightly rambling) comment on KAFKA-8432.
	stickyMembers := make([]sticky.GroupMember, 0, len(b.Members()))
	b.EachMember(func(member *kmsg.JoinGroupResponseMember, meta *kmsg.ConsumerMemberMetadata) {
		stickyMembers = append(stickyMembers, sticky.GroupMember{
			ID:          member.MemberID,
			Topics:      meta.Topics,
			UserData:    meta.UserData,
			Owned:       meta.OwnedPartitions,
			Generation:  meta.Generation,
			Cooperative: s.cooperative,
		})
	})

	p := &BalancePlan{sticky.Balance(stickyMembers, topics)}
	if s.cooperative {
		p.AdjustCooperative(b)
	}
	return p
}

// CooperativeStickyBalancer performs the sticky balancing strategy, but
// additionally opts the consumer group into "cooperative" rebalancing.
//
// Cooperative rebalancing differs from "eager" (the original) rebalancing in
// that group members do not stop processing partitions during the rebalance.
// Instead, once they receive their new assignment, each member determines
// which partitions it needs to revoke. If any, they send a new join request
// (before syncing), and the process starts over. This should ultimately end up
// in only two join rounds, with the major benefit being that processing never
// needs to stop.
//
// NOTE once a group is collectively using cooperative balancing, it is unsafe
// to have a member join the group that does not support cooperative balancing.
// If the only-eager member is elected leader, it will not know of the new
// multiple join strategy and things will go awry. Thus, once a group is
// entirely on cooperative rebalancing, it cannot go back.
//
// Migrating an eager group to cooperative balancing requires two rolling
// bounce deploys. The first deploy should add the cooperative-sticky strategy
// as an option (that is, each member goes from using one balance strategy to
// two). During this deploy, Kafka will tell leaders to continue using the old
// eager strategy, since the old eager strategy is the only one in common among
// all members. The second rolling deploy removes the old eager strategy. At
// this point, Kafka will tell the leader to use cooperative-sticky balancing.
// During this roll, all members in the group that still have both strategies
// continue to be eager and give up all of their partitions every rebalance.
// However, once a member only has cooperative-sticky, it can begin using this
// new strategy and things will work correctly. See KIP-429 for more details.
func CooperativeStickyBalancer() GroupBalancer {
	return &stickyBalancer{cooperative: true}
}

// AdjustCooperative performs the final adjustment to a plan for cooperative
// balancing.
//
// Over the plan, we remove all partitions that migrated from one member (where
// it was assigned) to a new member (where it is now planned).
//
// This allows members that had partitions removed to revoke and rejoin, which
// will then do another rebalance, and in that new rebalance, the planned
// partitions are now on the free list to be assigned.
func (p *BalancePlan) AdjustCooperative(b *ConsumerBalancer) {
	allAdded := make(map[string]map[int32]string, 100) // topic => partition => member
	allRevoked := make(map[string]map[int32]struct{}, 100)

	addT := func(t string) map[int32]string {
		addT := allAdded[t]
		if addT == nil {
			addT = make(map[int32]string, 20)
			allAdded[t] = addT
		}
		return addT
	}
	revokeT := func(t string) map[int32]struct{} {
		revokeT := allRevoked[t]
		if revokeT == nil {
			revokeT = make(map[int32]struct{}, 20)
			allRevoked[t] = revokeT
		}
		return revokeT
	}

	tmap := make(map[string]struct{}) // reusable topic existence map
	pmap := make(map[int32]struct{})  // reusable partitions existence map

	plan := p.plan

	// First, on all members, we find what was added and what was removed
	// to and from that member.
	b.EachMember(func(member *kmsg.JoinGroupResponseMember, meta *kmsg.ConsumerMemberMetadata) {
		planned := plan[member.MemberID]

		// added   := planned - current
		// revoked := current - planned

		for ptopic := range planned { // set existence for all planned topics
			tmap[ptopic] = struct{}{}
		}
		for _, otopic := range meta.OwnedPartitions { // over all prior owned topics,
			topic := otopic.Topic
			delete(tmap, topic)
			ppartitions, exists := planned[topic]
			if !exists { // any topic that is no longer planned was entirely revoked,
				allRevokedT := revokeT(topic)
				for _, opartition := range otopic.Partitions {
					allRevokedT[opartition] = struct{}{}
				}
				continue
			}
			// calculate what was added by creating a planned existence map,
			// then removing what was owned, and anything that remains is new,
			for _, ppartition := range ppartitions {
				pmap[ppartition] = struct{}{}
			}
			for _, opartition := range otopic.Partitions {
				delete(pmap, opartition)
			}
			if len(pmap) > 0 {
				allAddedT := addT(topic)
				for ppartition := range pmap {
					delete(pmap, ppartition)
					allAddedT[ppartition] = member.MemberID
				}
			}
			// then calculate removal by creating owned existence map,
			// then removing what was planned, anything remaining was revoked.
			for _, opartition := range otopic.Partitions {
				pmap[opartition] = struct{}{}
			}
			for _, ppartition := range ppartitions {
				delete(pmap, ppartition)
			}
			if len(pmap) > 0 {
				allRevokedT := revokeT(topic)
				for opartition := range pmap {
					delete(pmap, opartition)
					allRevokedT[opartition] = struct{}{}
				}
			}
		}
		for ptopic := range tmap { // finally, anything remaining in tmap is a new planned topic.
			delete(tmap, ptopic)
			allAddedT := addT(ptopic)
			for _, ppartition := range planned[ptopic] {
				allAddedT[ppartition] = member.MemberID
			}
		}
	})

	// Over all revoked, if the revoked partition was added to a different
	// member, we remove that partition from the new member.
	for topic, rpartitions := range allRevoked {
		atopic, exists := allAdded[topic]
		if !exists {
			continue
		}
		for rpartition := range rpartitions {
			amember, exists := atopic[rpartition]
			if !exists {
				continue
			}

			ptopics := plan[amember]
			ppartitions := ptopics[topic]
			for i, ppartition := range ppartitions {
				if ppartition == rpartition {
					ppartitions[i] = ppartitions[len(ppartitions)-1]
					ppartitions = ppartitions[:len(ppartitions)-1]
					break
				}
			}
			if len(ppartitions) > 0 {
				ptopics[topic] = ppartitions
			} else {
				delete(ptopics, topic)
			}
		}
	}
}
