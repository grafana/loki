package sarama

import (
	"slices"
	"sync/atomic"
)

// NewBalanceStrategyCooperativeSticky returns a cooperative sticky balance strategy
//
// Existing groups should first deploy it alongside their current eager
// strategy, then remove the eager strategy in a second rolling deployment:
//
//	[]BalanceStrategy{NewBalanceStrategyCooperativeSticky(), NewBalanceStrategyRange()}
//	[]BalanceStrategy{NewBalanceStrategyCooperativeSticky()}
//
// The returned strategy is stateful and must not be shared between consumer
// groups. It requires Version >= V2_4_0_0.
func NewBalanceStrategyCooperativeSticky() BalanceStrategy {
	s := &cooperativeStickyBalanceStrategy{
		stickyBalanceStrategy: stickyBalanceStrategy{cooperative: true},
	}
	s.generation.Store(defaultGeneration)
	return s
}

type cooperativeStickyBalanceStrategy struct {
	stickyBalanceStrategy

	// generation disambiguates current ownership from stale subscriptions
	generation atomic.Int32
}

// Name implements BalanceStrategy.
func (s *cooperativeStickyBalanceStrategy) Name() string {
	return CooperativeStickyBalanceStrategyName
}

// SupportedProtocols implements RebalanceProtocolBalanceStrategy
func (s *cooperativeStickyBalanceStrategy) SupportedProtocols() []RebalanceProtocol {
	return []RebalanceProtocol{RebalanceProtocolCooperative, RebalanceProtocolEager}
}

// SubscriptionUserData implements SubscriptionUserDataBalanceStrategy
func (s *cooperativeStickyBalanceStrategy) SubscriptionUserData(topics []string) ([]byte, error) {
	return encode(&cooperativeStickyAssignorUserData{Generation: s.generation.Load()}, nil)
}

// OnAssignment implements OnAssignmentBalanceStrategy.
func (s *cooperativeStickyBalanceStrategy) OnAssignment(assignment *ConsumerGroupMemberAssignment, generationID int32) {
	s.generation.Store(generationID)
}

// AssignmentData implements BalanceStrategy
func (s *cooperativeStickyBalanceStrategy) AssignmentData(memberID string, topics map[string][]int32, generationID int32) ([]byte, error) {
	return nil, nil
}

// adjustCooperativeAssignment withholds partitions until their old owner revokes them
func adjustCooperativeAssignment(plan BalanceStrategyPlan, members map[string]ConsumerGroupMemberMetadata) {
	addedOwners := make(map[topicPartitionAssignment]string)
	revoked := make(map[topicPartitionAssignment]none)

	for memberID, meta := range members {
		owned := make(map[topicPartitionAssignment]none, len(meta.OwnedPartitions))
		for _, partition := range ownedTopicPartitions(meta.OwnedPartitions) {
			owned[partition] = none{}
		}

		assigned := make(map[topicPartitionAssignment]none)
		for topic, partitions := range plan[memberID] {
			for _, partition := range partitions {
				tp := topicPartitionAssignment{Topic: topic, Partition: partition}
				assigned[tp] = none{}
				if _, held := owned[tp]; !held {
					addedOwners[tp] = memberID
				}
			}
		}

		for tp := range owned {
			if _, keeps := assigned[tp]; !keeps {
				revoked[tp] = none{}
			}
		}
	}

	for tp, newOwner := range addedOwners {
		if _, transferring := revoked[tp]; !transferring {
			continue
		}
		topics := plan[newOwner]
		topics[tp.Topic] = slices.DeleteFunc(topics[tp.Topic], func(p int32) bool { return p == tp.Partition })
		if len(topics[tp.Topic]) == 0 {
			delete(topics, tp.Topic)
		}
	}
}
