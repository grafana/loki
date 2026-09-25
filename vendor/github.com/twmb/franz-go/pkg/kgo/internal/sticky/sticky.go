// Package sticky provides sticky partitioning strategy for Kafka, with a
// complete overhaul to be faster, more understandable, and optimal.
//
// For some points on how Java's strategy is flawed, see
// https://github.com/IBM/sarama/pull/1416/files/b29086bdaae0da7ce71eae3f854d50685fd6b631#r315005878
package sticky

import (
	"math"
	"slices"

	"github.com/twmb/franz-go/pkg/kbin"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Sticky partitioning has two versions, the latter from KIP-341 preventing a
// bug. The second version introduced generations with the default generation
// from the first generation's consumers defaulting to -1.

// We can support up to 65533 members; the unassignedPart sentinel and one
// spare slot are reserved.
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
	Rack        string // KIP-881: empty if not set
}

// Plan is the plan this package came up with (member => topic => partitions).
type Plan map[string]map[string][]int32

type balancer struct {
	// members are the members in play for this balance.
	// This is built in newBalancer mapping member IDs to the GroupMember.
	members []GroupMember

	memberNums map[string]uint16 // member id => index into members

	topicNums   map[string]uint32 // topic name => index into topicInfos
	topicInfos  []topicInfo
	topicNames  []string   // topicNum => topic name
	partOwners  []uint32   // partition => owning topicNum
	subscribers [][]uint16 // topicNum => members subscribed to it

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

	// KIP-881: rack-aware assignment. partRacks is indexed by flat
	// partNum (same as partOwners), memberRacks by memberNum. Rack
	// indices are 1-based so that zero-initialized slices naturally
	// mean "no rack" (noRack == 0). When no rack info is available,
	// both slices are nil. The nRacks field is the count of distinct
	// racks.
	memberRacks []uint16
	partRacks   []uint16
	nRacks      int

	// origOwner is who held each partition before this balance, or
	// unassignedPart for one nobody held. The repair uses this to keep
	// partitions where they were.
	origOwner []uint16
}

// topicInfo holds no topic name: it is indexed in the hottest loops here,
// and without a pointer the garbage collector never scans it. Names live
// in the parallel topicNames.
type topicInfo struct {
	partNum    int32 // base part num
	partitions int32 // number of partitions in the topic
}

func newBalancer(members []GroupMember, topics map[string]int32, partitionRacks map[string][]string) *balancer {
	var (
		nparts     int
		topicNums  = make(map[string]uint32, len(topics))
		topicInfos = make([]topicInfo, len(topics))
		topicNames = make([]string, len(topics))
	)
	for topic, partitions := range topics {
		topicNum := uint32(len(topicNums))
		topicNums[topic] = topicNum
		topicInfos[topicNum] = topicInfo{
			partNum:    int32(nparts),
			partitions: partitions,
		}
		topicNames[topicNum] = topic
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
		topicNames: topicNames,

		partOwners: partOwners,
		plan:       make(membersPartitions, len(members)),
	}

	evenDivvy := nparts/len(members) + 1
	planBuf := make(memberPartitions, evenDivvy*len(members))
	for num := range members {
		b.plan[num] = planBuf[:0:evenDivvy]
		planBuf = planBuf[evenDivvy:]
	}

	b.initRacks(partitionRacks)
	return b
}

// initRacks sets up rack-aware fields if any member and any partition
// have rack info. Both sides must have racks for rack-aware assignment
// to be useful.
func (b *balancer) initRacks(partitionRacks map[string][]string) {
	if len(partitionRacks) == 0 {
		return
	}

	// Map rack strings to small 1-based indices (0 = noRack).
	rackIndex := make(map[string]uint16)
	rackOf := func(s string) uint16 {
		if s == "" {
			return noRack
		}
		idx, ok := rackIndex[s]
		if !ok {
			idx = uint16(len(rackIndex) + 1)
			rackIndex[s] = idx
		}
		return idx
	}

	memberRacks := make([]uint16, len(b.members))
	var anyMemberRack bool
	for i := range b.members {
		memberRacks[i] = rackOf(b.members[i].Rack)
		if memberRacks[i] != noRack {
			anyMemberRack = true
		}
	}
	if !anyMemberRack {
		return
	}

	// Build flat partRacks indexed by partNum. Zero-init is noRack.
	partRacks := make([]uint16, cap(b.partOwners))
	var anyPartRack bool
	for topic, racks := range partitionRacks {
		topicNum, ok := b.topicNums[topic]
		if !ok {
			continue
		}
		info := b.topicInfos[topicNum]
		for i, rack := range racks {
			if int32(i) >= info.partitions {
				break
			}
			if rack != "" {
				idx := rackOf(rack)
				partRacks[info.partNum+int32(i)] = idx
				anyPartRack = true
			}
		}
	}
	if !anyPartRack {
		return
	}

	b.partRacks = partRacks
	b.memberRacks = memberRacks
	b.nRacks = len(rackIndex)
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
		// A member cannot have more topics than partitions; with many
		// topics and few partitions per member, ntopics overallocates.
		topics := make(map[string][]int32, min(ntopics, len(partNums)))
		plan[member] = topics

		// partOwners is created by topic, and partNums refers to
		// indices in partOwners. If we sort by partNum, we have sorted
		// topics and partitions.
		slices.Sort(partNums)

		// We can reuse partNums for our topic partitions.
		topicParts := partNums[:0]

		lastTopicNum := b.partOwners[partNums[0]]
		lastTopicInfo := b.topicInfos[lastTopicNum]
		for _, partNum := range partNums {
			topicNum := b.partOwners[partNum]

			if topicNum != lastTopicNum {
				topics[b.topicNames[lastTopicNum]] = topicParts[:len(topicParts):len(topicParts)]
				topicParts = topicParts[len(topicParts):]

				lastTopicNum = topicNum
				lastTopicInfo = b.topicInfos[topicNum]
			}

			partition := partNum - lastTopicInfo.partNum
			topicParts = append(topicParts, partition)
		}
		topics[b.topicNames[lastTopicNum]] = topicParts[:len(topicParts):len(topicParts)]
	}
	return plan
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
	return BalanceWithRacks(members, topics, nil)
}

// BalanceWithRacks performs sticky partitioning with rack-aware assignment
// (KIP-881). partitionRacks maps topic => partition index => rack of the
// partition leader. When non-nil and members also have racks, the plan
// keeps every member's partitions in its own rack wherever balance allows,
// ahead of keeping partitions where they were.
func BalanceWithRacks(members []GroupMember, topics map[string]int32, partitionRacks map[string][]string) Plan {
	if len(members) == 0 {
		return make(Plan)
	}
	b := newBalancer(members, topics, partitionRacks)
	if cap(b.partOwners) == 0 {
		return b.into()
	}
	b.parseMemberMetadata()
	b.assignUnassignedAndInitGraph()
	b.initPlanByNumPartitions()
	b.balance()
	b.repairAssignment()
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
		// Owned partitions arrive grouped by topic, so remembering the
		// last topic saves hashing the name once per partition.
		var (
			lastTopic string
			lastInfo  topicInfo
			lastOK    bool
		)
		for _, topicPartition := range memberPlan {
			if topicPartition.topic != lastTopic || !lastOK {
				lastTopic = topicPartition.topic
				topicNum, exists := b.topicNums[lastTopic]
				lastOK = exists
				if exists {
					lastInfo = b.topicInfos[topicNum]
				}
			}
			// Claimed partitions are arbitrary input from other group
			// members; a negative partition would index our flat
			// partition state at a negative offset (or alias into the
			// preceding topic's range).
			if !lastOK || topicPartition.partition < 0 || topicPartition.partition >= lastInfo.partitions {
				continue
			}
			partNum := lastInfo.partNum + topicPartition.partition

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
				if b.stales == nil { // rare; only doubly claimed partitions land here
					b.stales = make(map[int32]uint16)
				}
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
	return memberPlan, generation
}

func (b *balancer) sortMemberByLiteralPartNum(memberNum int) {
	partNums := b.plan[memberNum]
	slices.SortFunc(partNums, func(lpNum, rpNum int32) int {
		ltNum, rtNum := b.partOwners[lpNum], b.partOwners[rpNum]
		li, ri := b.topicInfos[ltNum], b.topicInfos[rtNum]
		lt, rt := b.topicNames[ltNum], b.topicNames[rtNum]
		lp, rp := lpNum-li.partNum, rpNum-ri.partNum
		if lp < rp {
			return -1
		} else if lp > rp {
			return 1
		} else if lt < rt {
			return -1
		}
		return 1
	})
}

// assignUnassignedAndInitGraph assigns unassigned partitions to the least
// loaded members and initializes our steal graph.
//
// Doing so requires a bunch of metadata, and in the process we want to remove
// partitions from the plan that no longer exist in the client.
func (b *balancer) assignUnassignedAndInitGraph() {
	topicPotentials, memberSubs := b.topicPotentials()
	b.subscribers = topicPotentials

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

	partitionConsumers := b.dropUnwantedPartitions(memberSubs)

	b.tryRestickyStales(topicPotentials, partitionConsumers)

	// After restickying, not before: giving a partition back to an older
	// generation's claimant changes who counts as having come in with it.
	b.origOwner = make([]uint16, len(partitionConsumers))
	for i := range partitionConsumers {
		b.origOwner[i] = partitionConsumers[i].originalNum
	}

	if !b.isComplex && len(topicPotentials) > 0 {
		if b.partRacks != nil {
			b.assignRackAware(partitionConsumers, topicPotentials)
		}
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
		b.assignUnassignedComplex(partitionConsumers, topicPotentials)
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

// topicPotentials maps each topic to the members that can consume it, and
// also returns a per member bitset of the topics that member subscribes to.
func (b *balancer) topicPotentials() ([][]uint16, []uint64) {
	// We reserve the average subscribers per topic and let the few above
	// average grow by append. Reserving len(members) per topic is exact
	// only when every member subscribes to everything, and for regex
	// consumers over a large cluster is orders of magnitude too much.
	var nsubs int
	for i := range b.members {
		nsubs += len(b.members[i].Topics)
	}
	perTopic := nsubs/len(b.topicNums) + 1
	topicPotentialsBuf := make([]uint16, perTopic*len(b.topicNums))
	topicPotentials := make([][]uint16, len(b.topicNums))

	nsubWords := (len(b.topicNums) + 63) / 64
	memberSubs := make([]uint64, len(b.members)*nsubWords)
	for memberNum, member := range b.members {
		for _, topic := range member.Topics {
			topicNum, exists := b.topicNums[topic]
			if !exists {
				continue
			}
			// Subscriptions arrive in other members' join metadata and
			// can repeat a topic. Counting the repeat would make the
			// topic look like it has more subscribers than the group
			// has members, which is the complex balancing test.
			word, bit := memberNum*nsubWords+int(topicNum)/64, uint64(1)<<(topicNum%64)
			if memberSubs[word]&bit != 0 {
				continue
			}
			memberSubs[word] |= bit
			memberNums := topicPotentials[topicNum]
			if cap(memberNums) == 0 {
				memberNums = topicPotentialsBuf[:0:perTopic]
				topicPotentialsBuf = topicPotentialsBuf[perTopic:]
			}
			topicPotentials[topicNum] = append(memberNums, uint16(memberNum))
		}
	}
	return topicPotentials, memberSubs
}

// dropUnwantedPartitions removes from the prior plan any partition whose
// topic its member no longer subscribes to, which includes deleted topics
// and topics nobody wants anymore, and returns who consumes what is left.
func (b *balancer) dropUnwantedPartitions(memberSubs []uint64) []partitionConsumer {
	partitionConsumers := make([]partitionConsumer, cap(b.partOwners)) // partNum => consuming member
	for i := range partitionConsumers {
		partitionConsumers[i] = partitionConsumer{unassignedPart, unassignedPart}
	}
	nsubWords := (len(b.topicNums) + 63) / 64
	for memberNum := range b.plan {
		partNums := &b.plan[memberNum]
		subs := memberSubs[memberNum*nsubWords : (memberNum+1)*nsubWords]
		// We compact rather than swap-remove while ranging: remove is a
		// linear scan, so a member dropping a large subscription would
		// be quadratic in its partitions.
		keep := (*partNums)[:0]
		for _, partNum := range *partNums {
			topicNum := b.partOwners[partNum]
			if subs[topicNum/64]&(1<<(topicNum%64)) == 0 {
				continue
			}
			keep = append(keep, partNum)
			partitionConsumers[partNum] = partitionConsumer{uint16(memberNum), uint16(memberNum)}
		}
		*partNums = keep
	}
	return partitionConsumers
}

// assignUnassignedComplex assigns each unassigned partition to the least
// loaded member that subscribes to its topic, preferring one in the
// partition's rack among the least loaded.
func (b *balancer) assignUnassignedComplex(partitionConsumers []partitionConsumer, topicPotentials [][]uint16) {
	// partOwners groups partitions by topic, so partNum ascends through
	// one topic at a time and we build the member heaps once per topic
	// rather than scanning every eligible member once per partition. With
	// racks there is a heap per rack, so that the least loaded member in
	// the partition's rack is one lookup away.
	var (
		heapTopic = ^uint32(0)
		heapBuf   []uint16
		heaps     []membersByPartitions
		rackEnd   []int
	)
	for partNum, owner := range partitionConsumers {
		if owner.memberNum != unassignedPart {
			continue
		}
		topicNum := b.partOwners[partNum]
		potentials := topicPotentials[topicNum]
		if len(potentials) == 0 {
			continue
		}
		if topicNum != heapTopic {
			heapTopic = topicNum
			heapBuf = append(heapBuf[:0], potentials...)
			heaps = heaps[:0]
			if b.partRacks == nil {
				heaps = append(heaps, membersByPartitions{heapBuf, b.plan})
			} else {
				// A counting sort by rack, after which rack r's
				// members are heapBuf[rackEnd[r-1]:rackEnd[r]].
				rackEnd = slices.Grow(rackEnd[:0], b.nRacks+2)[:b.nRacks+2]
				clear(rackEnd)
				for _, m := range potentials {
					rackEnd[b.memberRacks[m]+1]++
				}
				for r := 1; r < len(rackEnd); r++ {
					rackEnd[r] += rackEnd[r-1]
				}
				for _, m := range potentials {
					heapBuf[rackEnd[b.memberRacks[m]]] = m
					rackEnd[b.memberRacks[m]]++
				}
				start := 0
				for r := 0; r <= b.nRacks; r++ {
					heaps = append(heaps, membersByPartitions{heapBuf[start:rackEnd[r]:rackEnd[r]], b.plan})
					start = rackEnd[r]
				}
			}
			for i := range heaps {
				heaps[i].init()
			}
		}

		best, bestLoad := -1, math.MaxInt
		for i := range heaps {
			if len(heaps[i].members) == 0 {
				continue
			}
			if load := len(b.plan[heaps[i].members[0]]); load < bestLoad {
				best, bestLoad = i, load
			}
		}
		if b.partRacks != nil {
			if r := int(b.partRacks[partNum]); r != noRack && len(heaps[r].members) > 0 && len(b.plan[heaps[r].members[0]]) == bestLoad {
				best = r
			}
		}
		assigned := heaps[best].members[0]
		b.plan[assigned].add(int32(partNum))
		heaps[best].fix0()
		partitionConsumers[partNum].memberNum = assigned
	}
}

// unassignedPart is a fake member number that we use to track if a partition
// is deleted or unassigned.
const unassignedPart = math.MaxUint16 - 1

// noRack is the sentinel for "no rack info" in memberRacks / partRacks.
// Rack indices are 1-based so that zero-initialized slices default to noRack.
const noRack = 0

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
			continue
		}

		// The part cannot be deleted; if it is, there are no
		// potential consumers and canTake is false above. The part
		// CAN be unassigned: the member that won the claim (the
		// higher generation) may have dropped its subscription to
		// the topic, in which case our caller un-mapped the
		// partition from the winner's plan and left it unassigned.
		// We give the partition straight back to the stale member.
		currentOwner := partitionConsumers[staleNum].memberNum
		if currentOwner == unassignedPart {
			b.plan[lastOwnerNum].add(staleNum)
			partitionConsumers[staleNum] = partitionConsumer{lastOwnerNum, lastOwnerNum}
			continue
		}
		lastOwnerPartitions := &b.plan[lastOwnerNum]
		currentOwnerPartitions := &b.plan[currentOwner]
		if len(*lastOwnerPartitions)+1 < len(*currentOwnerPartitions) {
			currentOwnerPartitions.remove(staleNum)
			lastOwnerPartitions.add(staleNum)
			// partitionConsumers seeds the steal graph's edge
			// ownership (cxns) on the complex path. If we move the
			// partition in the plan but not here, a later steal of
			// this partition resolves to the old owner and remove()
			// runs against a plan that does not contain the
			// partition -- which swap-removes an unrelated
			// partition: the final plan then has this partition on
			// two members and the wrongly removed one on none.
			partitionConsumers[staleNum] = partitionConsumer{lastOwnerNum, lastOwnerNum}
		}
	}
}

// assignRackAware pre-assigns unassigned partitions to the least loaded
// member in the partition's rack, up to an even share each. Only the
// uniform path uses this; the repair after balancing settles the rest.
func (b *balancer) assignRackAware(
	partitionConsumers []partitionConsumer,
	topicPotentials [][]uint16,
) {
	// Capping at the floor of an even share, not the ceiling, means the
	// fill after this lifts everybody to within one level, so balancing
	// has nothing to move.
	maxQuota := cap(b.partOwners) / len(b.members)

	rackHeaps := make([]membersByPartitions, b.nRacks+1) // by rack; noRack stays empty
	for _, m := range topicPotentials[0] {
		if rack := b.memberRacks[m]; rack != noRack {
			rackHeaps[rack].members = append(rackHeaps[rack].members, m)
		}
	}
	for i := range rackHeaps {
		rackHeaps[i].plan = b.plan
		rackHeaps[i].init()
	}
	for partNum, owner := range partitionConsumers {
		if owner.memberNum != unassignedPart {
			continue
		}
		rh := &rackHeaps[b.partRacks[partNum]]
		if len(rh.members) == 0 || len(b.plan[rh.members[0]]) >= maxQuota {
			continue
		}
		candidate := rh.members[0]
		b.plan[candidate].add(int32(partNum))
		rh.fix0()
		partitionConsumers[partNum].memberNum = candidate
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
	if max.level <= min.level+1 {
		return
	}

	// We sort each member's partitions by partition, then topic. Sorting
	// the lowest numbers first means that once we steal from the end, we
	// steal equally across all topics. This benefits the standard case the
	// most, where all members consume equally.
	//
	// Only members above min.level+1 are ever stolen from: min only rises
	// and max only falls until they meet. Sorting is the most expensive
	// step of a balance, so we skip everybody else.
	for memberNum := range b.plan {
		if len(b.plan[memberNum]) > min.level+1 {
			b.sortMemberByLiteralPartNum(memberNum)
		}
	}

	for max.level > min.level+1 {
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
			//
			// We remove through level, not min: deleting from the tree
			// swaps items between nodes, so min.item may by now be a
			// different level. The delete re-finds the min for the
			// same reason.
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
