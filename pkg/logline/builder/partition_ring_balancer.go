package builder

import (
	"sort"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/grafana/dskit/ring"
)

type cooperativeActiveStickyBalancer struct {
	kgo.GroupBalancer
	partitionRing ring.PartitionRingReader
	logger        log.Logger
}

// newCooperativeActiveStickyBalancer creates a balancer that combines Kafka's cooperative sticky balancing
// with partition ring awareness. It works by:
//
// 1. Using the partition ring to enumerate every partition that should be consumed
// 2. Applying cooperative sticky balancing to all of those partitions
//
// This ensures that:
//   - Every partition in the ring is balanced evenly across consumers using sticky
//     assignment, regardless of its state (active/inactive) or lock status
//   - Partitions stay pinned to a stable owner across rebalances.
//   - All partitions are monitored even if inactive, allowing quick activation when needed
//   - Partition handoff happens cooperatively to avoid stop-the-world rebalances
//
// The caller is responsible for ensuring the ring is populated before the
// first JoinGroup. If Balance is invoked with an empty ring it returns an
// empty plan and logs an error rather than silently falling back to plain
// cooperative-sticky — that fallback is exactly the load-skew bug this
// balancer exists to fix.
func newCooperativeActiveStickyBalancer(partitionRing ring.PartitionRingReader, logger log.Logger) kgo.GroupBalancer {
	return &cooperativeActiveStickyBalancer{
		GroupBalancer: kgo.CooperativeStickyBalancer(),
		partitionRing: partitionRing,
		logger:        logger,
	}
}

func (*cooperativeActiveStickyBalancer) ProtocolName() string {
	return "cooperative-active-sticky"
}

// stickyPartitions returns the partitions that get sticky assignment. Every
// partition in the ring is sticky regardless of its state (active/inactive).
func (b *cooperativeActiveStickyBalancer) stickyPartitions() map[int32]struct{} {
	sticky := make(map[int32]struct{})
	for _, p := range b.partitionRing.PartitionRing().Partitions() {
		sticky[p.GetId()] = struct{}{}
	}
	return sticky
}

func (b *cooperativeActiveStickyBalancer) MemberBalancer(members []kmsg.JoinGroupResponseMember) (kgo.GroupMemberBalancer, map[string]struct{}, error) {
	stickyPartitions := b.stickyPartitions()

	// Filter member metadata to only include sticky partitions
	filteredMembers := make([]kmsg.JoinGroupResponseMember, len(members))
	for i, member := range members {
		var meta kmsg.ConsumerMemberMetadata
		err := meta.ReadFrom(member.ProtocolMetadata)
		if err != nil {
			continue
		}

		// Filter owned partitions to only include sticky ones
		filteredOwned := make([]kmsg.ConsumerMemberMetadataOwnedPartition, 0, len(meta.OwnedPartitions))
		for _, owned := range meta.OwnedPartitions {
			filtered := kmsg.ConsumerMemberMetadataOwnedPartition{
				Topic:      owned.Topic,
				Partitions: make([]int32, 0, len(owned.Partitions)),
			}
			for _, p := range owned.Partitions {
				if _, ok := stickyPartitions[p]; ok {
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

func (b *cooperativeActiveStickyBalancer) Balance(balancer *kgo.ConsumerBalancer, partitionsPerTopic map[string]int32) kgo.IntoSyncAssignment {
	stickyCount := int32(len(b.stickyPartitions()))

	// Empty ring guard. The builder service refuses to start with an
	// empty ring (see PartitionRingServices.WaitForPartitions), so this
	// path should be unreachable in steady state. If we do hit it (the
	// ring was lost mid-flight, e.g. transient KV/memberlist failure),
	// emit an empty plan and log loudly — the consumer group will stay
	// alive but make zero progress, which is preferable to silently
	// reverting to plain cooperative-sticky over the full topic.
	if stickyCount == 0 {
		level.Error(b.logger).Log("msg",
			"partition ring is empty during rebalance, refusing to assign partitions; "+
				"consumer group will not consume until the ring is repopulated")
		return b.GroupBalancer.(kgo.ConsumerBalancerBalance).Balance(balancer, map[string]int32{})
	}

	// Let the sticky balancer handle the sticky set; round-robin the rest.
	sticky := make(map[string]int32)
	nonSticky := make(map[string]int32)
	for topic, total := range partitionsPerTopic {
		sticky[topic] = stickyCount
		if total > stickyCount {
			nonSticky[topic] = total - stickyCount
		}
	}

	// Get the sticky-set assignment
	assignment := b.GroupBalancer.(kgo.ConsumerBalancerBalance).Balance(balancer, sticky)

	plan := assignment.IntoSyncAssignment()

	// Get sorted list of members for deterministic round-robin
	members := make([]string, 0, len(plan))
	for _, m := range plan {
		members = append(members, m.MemberID)
	}
	sort.Strings(members)
	if len(members) == 0 {
		return syncAssignments(plan)
	}

	// Distribute non-sticky partitions round-robin
	memberIdx := 0
	for topic, nonStickyCount := range nonSticky {
		for p := stickyCount; p < stickyCount+nonStickyCount; p++ {
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
