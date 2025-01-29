package consumer

import (
	"sort"

	"github.com/grafana/dskit/ring"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type cooperativeActiveStickyBalancer struct {
	kgo.GroupBalancer
	partitionRing ring.PartitionRingReader
}

// NewCooperativeActiveStickyBalancer creates a balancer that combines Kafka's cooperative sticky balancing
// with partition ring awareness. It works by:
//
// 1. Using the partition ring to determine which partitions are "active" (i.e. should be processed)
// 2. Filtering out inactive partitions from member assignments during rebalancing, but still assigning them
// 3. Applying cooperative sticky balancing only to the active partitions
//
// This ensures that:
// - Active partitions are balanced evenly across consumers using sticky assignment for optimal processing
// - Inactive partitions are still assigned and consumed in a round-robin fashion, but without sticky assignment
// - All partitions are monitored even if inactive, allowing quick activation when needed
// - Partition handoff happens cooperatively to avoid stop-the-world rebalances
//
// This balancer should be used with [NewGroupClient] which monitors the partition ring and triggers
// rebalancing when the set of active partitions changes. This ensures optimal partition distribution
// as the active partition set evolves.
func NewCooperativeActiveStickyBalancer(partitionRing ring.PartitionRingReader) kgo.GroupBalancer {
	return &cooperativeActiveStickyBalancer{
		GroupBalancer: kgo.CooperativeStickyBalancer(),
		partitionRing: partitionRing,
	}
}

func (*cooperativeActiveStickyBalancer) ProtocolName() string {
	return "cooperative-active-sticky"
}

func (b *cooperativeActiveStickyBalancer) MemberBalancer(members []kmsg.JoinGroupResponseMember) (kgo.GroupMemberBalancer, map[string]struct{}, error) {
	// Get active partitions from ring
	activePartitions := make(map[int32]struct{})
	for _, id := range b.partitionRing.PartitionRing().PartitionIDs() {
		activePartitions[id] = struct{}{}
	}

	// Filter member metadata to only include active partitions
	filteredMembers := make([]kmsg.JoinGroupResponseMember, len(members))
	for i, member := range members {
		var meta kmsg.ConsumerMemberMetadata
		err := meta.ReadFrom(member.ProtocolMetadata)
		if err != nil {
			continue
		}

		// Filter owned partitions to only include active ones
		filteredOwned := make([]kmsg.ConsumerMemberMetadataOwnedPartition, 0, len(meta.OwnedPartitions))
		for _, owned := range meta.OwnedPartitions {
			filtered := kmsg.ConsumerMemberMetadataOwnedPartition{
				Topic:      owned.Topic,
				Partitions: make([]int32, 0, len(owned.Partitions)),
			}
			for _, p := range owned.Partitions {
				if _, isActive := activePartitions[p]; isActive {
					filtered.Partitions = append(filtered.Partitions, p)
				}
			}
			if len(filtered.Partitions) > 0 {
				filteredOwned = append(filteredOwned, filtered)
			}
		}
		meta.OwnedPartitions = filteredOwned

		// Create filtered member
		filteredMembers[i] = kmsg.JoinGroupResponseMember{
			MemberID:         member.MemberID,
			ProtocolMetadata: meta.AppendTo(nil),
		}
	}

	balancer, err := kgo.NewConsumerBalancer(b, filteredMembers)
	return balancer, balancer.MemberTopics(), err
}

// syncAssignments implements kgo.IntoSyncAssignment
type syncAssignments []kmsg.SyncGroupRequestGroupAssignment

func (s syncAssignments) IntoSyncAssignment() []kmsg.SyncGroupRequestGroupAssignment {
	return s
}

func (b *cooperativeActiveStickyBalancer) Balance(balancer *kgo.ConsumerBalancer, topics map[string]int32) kgo.IntoSyncAssignment {
	// Get active partition count
	actives := b.partitionRing.PartitionRing().PartitionsCount()

	// First, let the sticky balancer handle active partitions
	activeTopics := make(map[string]int32)
	inactiveTopics := make(map[string]int32)
	for topic, total := range topics {
		activeTopics[topic] = int32(actives)
		if total > int32(actives) {
			inactiveTopics[topic] = total - int32(actives)
		}
	}

	// Get active partition assignment
	assignment := b.GroupBalancer.(kgo.ConsumerBalancerBalance).Balance(balancer, activeTopics)

	plan := assignment.IntoSyncAssignment()

	// Get sorted list of members for deterministic round-robin
	members := make([]string, 0, len(plan))
	for _, m := range plan {
		members = append(members, m.MemberID)
	}
	sort.Strings(members)

	// Distribute inactive partitions round-robin
	memberIdx := 0
	for topic, numInactive := range inactiveTopics {
		for p := int32(actives); p < int32(actives)+numInactive; p++ {
			// Find the member's assignment
			for i, m := range plan {
				if m.MemberID == members[memberIdx] {
					var meta kmsg.ConsumerMemberAssignment
					err := meta.ReadFrom(m.MemberAssignment)
					if err != nil {
						continue
					}

					// Find or create topic assignment
					found := false
					for j, t := range meta.Topics {
						if t.Topic == topic {
							meta.Topics[j].Partitions = append(t.Partitions, p)
							found = true
							break
						}
					}
					if !found {
						meta.Topics = append(meta.Topics, kmsg.ConsumerMemberAssignmentTopic{
							Topic:      topic,
							Partitions: []int32{p},
						})
					}

					plan[i].MemberAssignment = meta.AppendTo(nil)
					break
				}
			}
			memberIdx = (memberIdx + 1) % len(members)
		}
	}

	return syncAssignments(plan)
}
