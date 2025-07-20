// Package sticky provides sticky partitioning strategy for Kafka, with a
// complete overhaul to be faster, more understandable, and optimal.
//
// For some points on how Java's strategy is flawed, see
// https://github.com/IBM/sarama/pull/1416/files/b29086bdaae0da7ce71eae3f854d50685fd6b631#r315005878
package sticky

import (
	"math"

	"github.com/twmb/franz-go/pkg/kbin"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Sticky partitioning has two versions, the latter from KIP-341 preventing a
// bug. The second version introduced generations with the default generation
// from the first generation's consumers defaulting to -1.

// We can support up to 65533 members; two slots are reserved.
// We can support up to 2,147,483,647 partitions.
// I expect a server to fall over before reaching either of these numbers.

// GroupMember is a Kafka group member.
type GroupMember struct {
	ID          string
	Topics      []string
	UserData    []byte
	Owned       []kmsg.ConsumerMemberMetadataOwnedPartition
	Generation  int32
	Cooperative bool
}

// Plan is the plan this package came up with (member => topic => partitions).
type Plan map[string]map[string][]int32

type balancer struct {
	// members are the members in play for this balance.
	// This is built in newBalancer mapping member IDs to the GroupMember.
	members []GroupMember

	memberNums map[string]uint16 // member id => index into members

	topicNums  map[string]uint32 // topic name => index into topicInfos
	topicInfos []topicInfo
	partOwners []uint32 // partition => owning topicNum

	// Stales tracks partNums that are doubly subscribed in this join
	// where one of the subscribers is on an old generation.
	//
	// The newer generation goes into plan directly, the older gets
	// stuffed here.
	stales map[int32]uint16 // partNum => stale memberNum

	plan membersPartitions // what we are building and balancing

	// planByNumPartitions orders plan members into partition count levels.
	//
	// The nodes in the tree reference values in plan, meaning updates in
	// this field are visible in plan.
	planByNumPartitions treePlan

	// if the subscriptions are complex (all members do _not_ consume the
	// same partitions), then we build a graph and use that for assigning.
	isComplex bool

	// stealGraph is a graphical representation of members and partitions
	// they want to steal.
	stealGraph graph
}

type topicInfo struct {
	partNum    int32 // base part num
	partitions int32 // number of partitions in the topic
	topic      string
}

func newBalancer(members []GroupMember, topics map[string]int32) *balancer {
	var (
		nparts     int
		topicNums  = make(map[string]uint32, len(topics))
		topicInfos = make([]topicInfo, len(topics))
	)
	for topic, partitions := range topics {
		topicNum := uint32(len(topicNums))
		topicNums[topic] = topicNum
		topicInfos[topicNum] = topicInfo{
			partNum:    int32(nparts),
			partitions: partitions,
			topic:      topic,
		}
		nparts += int(partitions)
	}
	partOwners := make([]uint32, 0, nparts)
	for topicNum, info := range topicInfos {
		for i := int32(0); i < info.partitions; i++ {
			partOwners = append(partOwners, uint32(topicNum))
		}
	}
	memberNums := make(map[string]uint16, len(members))
	for num, member := range members {
		memberNums[member.ID] = uint16(num)
	}

	b := &balancer{
		members:    members,
		memberNums: memberNums,
		topicNums:  topicNums,
		topicInfos: topicInfos,

		partOwners: partOwners,
		stales:     make(map[int32]uint16),
		plan:       make(membersPartitions, len(members)),
	}

	evenDivvy := nparts/len(members) + 1
	planBuf := make(memberPartitions, evenDivvy*len(members))
	for num := range members {
		b.plan[num] = planBuf[:0:evenDivvy]
		planBuf = planBuf[evenDivvy:]
	}
	return b
}

func (b *balancer) into() Plan {
	plan := make(Plan, len(b.plan))
	ntopics := 5 * len(b.topicNums) / 4

	for memberNum, partNums := range b.plan {
		member := b.members[memberNum].ID
		if len(partNums) == 0 {
			plan[member] = make(map[string][]int32, 0)
			continue
		}
		topics := make(map[string][]int32, ntopics)
		plan[member] = topics

		// partOwners is created by topic, and partNums refers to
		// indices in partOwners. If we sort by partNum, we have sorted
		// topics and partitions.
		sortPartNums(partNums)

		// We can reuse partNums for our topic partitions.
		topicParts := partNums[:0]

		lastTopicNum := b.partOwners[partNums[0]]
		lastTopicInfo := b.topicInfos[lastTopicNum]
		for _, partNum := range partNums {
			topicNum := b.partOwners[partNum]

			if topicNum != lastTopicNum {
				topics[lastTopicInfo.topic] = topicParts[:len(topicParts):len(topicParts)]
				topicParts = topicParts[len(topicParts):]

				lastTopicNum = topicNum
				lastTopicInfo = b.topicInfos[topicNum]
			}

			partition := partNum - lastTopicInfo.partNum
			topicParts = append(topicParts, partition)
		}
		topics[lastTopicInfo.topic] = topicParts[:len(topicParts):len(topicParts)]
	}
	return plan
}

func (b *balancer) partNumByTopic(topic string, partition int32) (int32, bool) {
	topicNum, exists := b.topicNums[topic]
	if !exists {
		return 0, false
	}
	topicInfo := b.topicInfos[topicNum]
	if partition >= topicInfo.partitions {
		return 0, false
	}
	return topicInfo.partNum + partition, true
}

// memberPartitions contains partitions for a member.
type memberPartitions []int32

func (m *memberPartitions) remove(needle int32) {
	s := *m
	var d int
	for i, check := range s {
		if check == needle {
			d = i
			break
		}
	}
	s[d] = s[len(s)-1]
	*m = s[:len(s)-1]
}

func (m *memberPartitions) takeEnd() int32 {
	s := *m
	r := s[len(s)-1]
	*m = s[:len(s)-1]
	return r
}

func (m *memberPartitions) add(partNum int32) {
	*m = append(*m, partNum)
}

// membersPartitions maps members to their partitions.
type membersPartitions []memberPartitions

type partitionLevel struct {
	level   int
	members []uint16
}

// partitionLevel's members field used to be a map, but removing it gains a
// slight perf boost at the cost of removing members being O(M).
// Even with the worse complexity, scanning a short list can be faster
// than managing a map, and we expect groups to not be _too_ large.
func (l *partitionLevel) removeMember(memberNum uint16) {
	for i, v := range l.members {
		if v == memberNum {
			l.members[i] = l.members[len(l.members)-1]
			l.members = l.members[:len(l.members)-1]
			return
		}
	}
}

func (b *balancer) findLevel(level int) *partitionLevel {
	return b.planByNumPartitions.findWithOrInsertWith(
		func(n *partitionLevel) int { return level - n.level },
		func() *partitionLevel { return newPartitionLevel(level) },
	).item
}

func (b *balancer) fixMemberLevel(
	src *treePlanNode,
	memberNum uint16,
	partNums memberPartitions,
) {
	b.removeLevelingMember(src, memberNum)
	newLevel := len(partNums)
	partLevel := b.findLevel(newLevel)
	partLevel.members = append(partLevel.members, memberNum)
}

func (b *balancer) removeLevelingMember(
	src *treePlanNode,
	memberNum uint16,
) {
	src.item.removeMember(memberNum)
	if len(src.item.members) == 0 {
		b.planByNumPartitions.delete(src)
	}
}

func (l *partitionLevel) less(r *partitionLevel) bool {
	return l.level < r.level
}

func newPartitionLevel(level int) *partitionLevel {
	return &partitionLevel{level: level}
}

func (b *balancer) initPlanByNumPartitions() {
	for memberNum, partNums := range b.plan {
		partLevel := b.findLevel(len(partNums))
		partLevel.members = append(partLevel.members, uint16(memberNum))
	}
}

// Balance performs sticky partitioning for the given group members and topics,
// returning the determined plan.
func Balance(members []GroupMember, topics map[string]int32) Plan {
	if len(members) == 0 {
		return make(Plan)
	}
	b := newBalancer(members, topics)
	if cap(b.partOwners) == 0 {
		return b.into()
	}
	b.parseMemberMetadata()
	b.assignUnassignedAndInitGraph()
	b.initPlanByNumPartitions()
	b.balance()
	return b.into()
}

// parseMemberMetadata parses all member userdata to initialize the prior plan.
func (b *balancer) parseMemberMetadata() {
	// all partitions => members that are consuming those partitions
	// Each partition should only have one consumer, but a flaky member
	// could rejoin with an old generation (stale user data) and say it
	// is consuming something a different member is. See KIP-341.
	partitionConsumersByGeneration := make([]memberGeneration, cap(b.partOwners))

	const highBit uint32 = 1 << 31
	var memberPlan []topicPartition
	var gen uint32

	for _, member := range b.members {
		// KAFKA-13715 / KIP-792: cooperative-sticky now includes a
		// generation directly with the currently-owned partitions, and
		// we can avoid deserializing UserData. This guards against
		// some zombie issues (see KIP).
		//
		// The eager (sticky) balancer revokes all partitions before
		// rejoining, so we cannot use Owned.
		if member.Cooperative && member.Generation >= 0 {
			memberPlan = memberPlan[:0]
			for _, t := range member.Owned {
				for _, p := range t.Partitions {
					memberPlan = append(memberPlan, topicPartition{t.Topic, p})
				}
			}
			gen = uint32(member.Generation)
		} else {
			memberPlan, gen = deserializeUserData(member.UserData, memberPlan[:0])
		}
		gen |= highBit
		memberNum := b.memberNums[member.ID]
		for _, topicPartition := range memberPlan {
			partNum, exists := b.partNumByTopic(topicPartition.topic, topicPartition.partition)
			if !exists {
				continue
			}

			// We keep the highest generation, and at most two generations.
			// If something is doubly consumed, we skip it.
			pcs := &partitionConsumersByGeneration[partNum]
			switch {
			case gen > pcs.genNew: // one consumer already, but new member has higher generation
				pcs.memberOld, pcs.genOld = pcs.memberNew, pcs.genNew
				pcs.memberNew, pcs.genNew = memberNum, gen

			case gen > pcs.genOld: // one consumer already, we could be second, or if there is a second, we have a high generation
				pcs.memberOld, pcs.genOld = memberNum, gen
			}
		}
	}

	for partNum, pcs := range partitionConsumersByGeneration {
		if pcs.genNew&highBit != 0 {
			b.plan[pcs.memberNew].add(int32(partNum))
			if pcs.genOld&highBit != 0 {
				b.stales[int32(partNum)] = pcs.memberOld
			}
		}
	}
}

type memberGeneration struct {
	memberNew uint16
	memberOld uint16
	genNew    uint32
	genOld    uint32
}

type topicPartition struct {
	topic     string
	partition int32
}

// deserializeUserData returns the topic partitions a member was consuming and
// the join generation it was consuming from.
//
// If anything fails or we do not understand the userdata parsing generation,
// we return empty defaults. The member will just be assumed to have no
// history.
func deserializeUserData(userdata []byte, base []topicPartition) (memberPlan []topicPartition, generation uint32) {
	memberPlan = base[:0]
	b := kbin.Reader{Src: userdata}
	for numAssignments := b.ArrayLen(); numAssignments > 0; numAssignments-- {
		topic := b.UnsafeString()
		for numPartitions := b.ArrayLen(); numPartitions > 0; numPartitions-- {
			memberPlan = append(memberPlan, topicPartition{
				topic,
				b.Int32(),
			})
		}
	}
	if len(b.Src) > 0 {
		// A generation of -1 is just as good of a generation as 0, so we use 0
		// and then use the high bit to signify this generation has been set.
		if generationI32 := b.Int32(); generationI32 > 0 {
			generation = uint32(generationI32)
		}
	}
	if b.Complete() != nil {
		memberPlan = memberPlan[:0]
	}
	return
}

// assignUnassignedAndInitGraph is a long function that assigns unassigned
// partitions to the least loaded members and initializes our steal graph.
//
// Doing so requires a bunch of metadata, and in the process we want to remove
// partitions from the plan that no longer exist in the client.
func (b *balancer) assignUnassignedAndInitGraph() {
	// First, over all members in this assignment, map each partition to
	// the members that can consume it. We will use this for assigning.
	//
	// To do this mapping efficiently, we first map each topic to the
	// memberNums that can consume those topics, and then use the results
	// below in the partition mapping. Doing this two step process allows
	// for a 10x speed boost rather than ranging over all partitions many
	// times.
	topicPotentialsBuf := make([]uint16, len(b.topicNums)*len(b.members))
	topicPotentials := make([][]uint16, len(b.topicNums))
	for memberNum, member := range b.members {
		for _, topic := range member.Topics {
			topicNum, exists := b.topicNums[topic]
			if !exists {
				continue
			}
			memberNums := topicPotentials[topicNum]
			if cap(memberNums) == 0 {
				memberNums = topicPotentialsBuf[:0:len(b.members)]
				topicPotentialsBuf = topicPotentialsBuf[len(b.members):]
			}
			topicPotentials[topicNum] = append(memberNums, uint16(memberNum))
		}
	}

	for _, topicMembers := range topicPotentials {
		// If the number of members interested in this topic is not the
		// same as the number of members in this group, then **other**
		// members are interested in other topics and not this one, and
		// we must go to complex balancing.
		//
		// We could accidentally fall into isComplex if any member is
		// not interested in anything, but realistically we do not
		// expect members to join with no interests.
		if len(topicMembers) != len(b.members) {
			b.isComplex = true
		}
	}

	// Next, over the prior plan, un-map deleted topics or topics that
	// members no longer want. This is where we determine what is now
	// unassigned.
	partitionConsumers := make([]partitionConsumer, cap(b.partOwners)) // partNum => consuming member
	for i := range partitionConsumers {
		partitionConsumers[i] = partitionConsumer{unassignedPart, unassignedPart}
	}
	for memberNum := range b.plan {
		partNums := &b.plan[memberNum]
		for _, partNum := range *partNums {
			topicNum := b.partOwners[partNum]
			if len(topicPotentials[topicNum]) == 0 { // all prior subscriptions stopped wanting this partition
				partNums.remove(partNum)
				continue
			}
			memberTopics := b.members[memberNum].Topics
			var memberStillWantsTopic bool
			for _, memberTopic := range memberTopics {
				if memberTopic == b.topicInfos[topicNum].topic {
					memberStillWantsTopic = true
					break
				}
			}
			if !memberStillWantsTopic {
				partNums.remove(partNum)
				continue
			}
			partitionConsumers[partNum] = partitionConsumer{uint16(memberNum), uint16(memberNum)}
		}
	}

	b.tryRestickyStales(topicPotentials, partitionConsumers)

	// For each member, we now sort their current partitions by partition,
	// then topic. Sorting the lowest numbers first means that once we
	// steal from the end (when adding a member), we steal equally across
	// all topics. This benefits the standard case the most, where all
	// members consume equally.
	for memberNum := range b.plan {
		b.sortMemberByLiteralPartNum(memberNum)
	}

	if !b.isComplex && len(topicPotentials) > 0 {
		potentials := topicPotentials[0]
		(&membersByPartitions{potentials, b.plan}).init()
		for partNum, owner := range partitionConsumers {
			if owner.memberNum != unassignedPart {
				continue
			}
			assigned := potentials[0]
			b.plan[assigned].add(int32(partNum))
			(&membersByPartitions{potentials, b.plan}).fix0()
			partitionConsumers[partNum].memberNum = assigned
		}
	} else {
		for partNum, owner := range partitionConsumers {
			if owner.memberNum != unassignedPart {
				continue
			}
			potentials := topicPotentials[b.partOwners[partNum]]
			if len(potentials) == 0 {
				continue
			}
			leastConsumingPotential := potentials[0]
			leastConsuming := len(b.plan[leastConsumingPotential])
			for _, potential := range potentials[1:] {
				potentialConsuming := len(b.plan[potential])
				if potentialConsuming < leastConsuming {
					leastConsumingPotential = potential
					leastConsuming = potentialConsuming
				}
			}
			b.plan[leastConsumingPotential].add(int32(partNum))
			partitionConsumers[partNum].memberNum = leastConsumingPotential
		}
	}

	// Lastly, with everything assigned, we build our steal graph for
	// balancing if needed.
	if b.isComplex {
		b.stealGraph = b.newGraph(
			partitionConsumers,
			topicPotentials,
		)
	}
}

// unassignedPart is a fake member number that we use to track if a partition
// is deleted or unassigned.
const unassignedPart = math.MaxUint16 - 1

// tryRestickyStales is a pre-assigning step where, for all stale members,
// we give partitions back to them if the partition is currently on an
// over loaded member or unassigned.
//
// This effectively re-stickies members before we balance further.
func (b *balancer) tryRestickyStales(
	topicPotentials [][]uint16,
	partitionConsumers []partitionConsumer,
) {
	for staleNum, lastOwnerNum := range b.stales {
		potentials := topicPotentials[b.partOwners[staleNum]] // there must be a potential consumer if we are here
		var canTake bool
		for _, potentialNum := range potentials {
			if potentialNum == lastOwnerNum {
				canTake = true
			}
		}
		if !canTake {
			return
		}

		// The part cannot be unassigned here; a stale member
		// would just have it. The part also cannot be deleted;
		// if it is, there are no potential consumers and the
		// logic above continues before getting here. The part
		// must be on a different owner (cannot be lastOwner),
		// otherwise it would not be a lastOwner in the stales
		// map; it would just be the current owner.
		currentOwner := partitionConsumers[staleNum].memberNum
		lastOwnerPartitions := &b.plan[lastOwnerNum]
		currentOwnerPartitions := &b.plan[currentOwner]
		if len(*lastOwnerPartitions)+1 < len(*currentOwnerPartitions) {
			currentOwnerPartitions.remove(staleNum)
			lastOwnerPartitions.add(staleNum)
		}
	}
}

type partitionConsumer struct {
	memberNum   uint16
	originalNum uint16
}

// While assigning, we keep members per topic heap sorted by the number of
// partitions they are currently consuming. This allows us to have quick
// assignment vs. always scanning to see the min loaded member.
//
// Our process is to init the heap and then always fix the 0th index after
// making it larger, so we only ever need to sift down.
type membersByPartitions struct {
	members []uint16
	plan    membersPartitions
}

func (m *membersByPartitions) init() {
	n := len(m.members)
	for i := n/2 - 1; i >= 0; i-- {
		m.down(i, n)
	}
}

func (m *membersByPartitions) fix0() {
	m.down(0, len(m.members))
}

func (m *membersByPartitions) down(i0, n int) {
	node := i0
	for {
		left := 2*node + 1
		if left >= n || left < 0 { // left < 0 after int overflow
			break
		}
		swap := left // left child
		swapLen := len(m.plan[m.members[left]])
		if right := left + 1; right < n {
			if rightLen := len(m.plan[m.members[right]]); rightLen < swapLen {
				swapLen = rightLen
				swap = right
			}
		}
		nodeLen := len(m.plan[m.members[node]])
		if nodeLen <= swapLen {
			break
		}
		m.members[node], m.members[swap] = m.members[swap], m.members[node]
		node = swap
	}
}

// balance loops trying to move partitions until the plan is as balanced
// as it can be.
func (b *balancer) balance() {
	if b.isComplex {
		b.balanceComplex()
		return
	}

	// If all partitions are consumed equally, we have a very easy
	// algorithm to balance: while the min and max levels are separated
	// by over two, take from the top and give to the bottom.
	min := b.planByNumPartitions.min().item
	max := b.planByNumPartitions.max().item
	for {
		if max.level <= min.level+1 {
			return
		}

		minMems := min.members
		maxMems := max.members
		for len(minMems) > 0 && len(maxMems) > 0 {
			dst := minMems[0]
			src := maxMems[0]

			minMems = minMems[1:]
			maxMems = maxMems[1:]

			srcPartitions := &b.plan[src]
			dstPartitions := &b.plan[dst]

			dstPartitions.add(srcPartitions.takeEnd())
		}

		nextUp := b.findLevel(min.level + 1)
		nextDown := b.findLevel(max.level - 1)

		endOfUps := len(min.members) - len(minMems)
		endOfDowns := len(max.members) - len(maxMems)

		nextUp.members = append(nextUp.members, min.members[:endOfUps]...)
		nextDown.members = append(nextDown.members, max.members[:endOfDowns]...)

		min.members = min.members[endOfUps:]
		max.members = max.members[endOfDowns:]

		if len(min.members) == 0 {
			b.planByNumPartitions.delete(b.planByNumPartitions.min())
			min = b.planByNumPartitions.min().item
		}
		if len(max.members) == 0 {
			b.planByNumPartitions.delete(b.planByNumPartitions.max())
			max = b.planByNumPartitions.max().item
		}
	}
}

func (b *balancer) balanceComplex() {
	for min := b.planByNumPartitions.min(); b.planByNumPartitions.size > 1; min = b.planByNumPartitions.min() {
		level := min.item
		// If this max level is within one of this level, then nothing
		// can steal down so we return early.
		max := b.planByNumPartitions.max().item
		if max.level <= level.level+1 {
			return
		}
		// We continually loop over this level until every member is
		// static (deleted) or bumped up a level.
		for len(level.members) > 0 {
			memberNum := level.members[0]
			if stealPath, found := b.stealGraph.findSteal(memberNum); found {
				for _, segment := range stealPath {
					b.reassignPartition(segment.src, segment.dst, segment.part)
				}
				if len(max.members) == 0 {
					break
				}
				continue
			}

			// If we could not find a steal path, this
			// member is not static (will never grow).
			level.removeMember(memberNum)
			if len(level.members) == 0 {
				b.planByNumPartitions.delete(b.planByNumPartitions.min())
			}
		}
	}
}

func (b *balancer) reassignPartition(src, dst uint16, partNum int32) {
	srcPartitions := &b.plan[src]
	dstPartitions := &b.plan[dst]

	oldSrcLevel := len(*srcPartitions)
	oldDstLevel := len(*dstPartitions)

	srcPartitions.remove(partNum)
	dstPartitions.add(partNum)

	b.fixMemberLevel(
		b.planByNumPartitions.findWith(func(n *partitionLevel) int {
			return oldSrcLevel - n.level
		}),
		src,
		*srcPartitions,
	)
	b.fixMemberLevel(
		b.planByNumPartitions.findWith(func(n *partitionLevel) int {
			return oldDstLevel - n.level
		}),
		dst,
		*dstPartitions,
	)

	b.stealGraph.changeOwnership(partNum, dst)
}
