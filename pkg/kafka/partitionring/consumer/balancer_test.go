package consumer

import (
	"fmt"
	"slices"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// An empty partition ring must yield an empty plan. The balancer must
// not fall back to spreading every Kafka partition across members when
// it has no information about which partitions are active.
func TestCooperativeActiveStickyBalancer_EmptyRing(t *testing.T) {
	t.Parallel()
	const topic = "ingest"
	b := NewCooperativeActiveStickyBalancer(newFakePartitionRing(), log.NewNopLogger())
	members := []kmsg.JoinGroupResponseMember{
		{MemberID: "m00", ProtocolMetadata: newMemberMetadata(t, topic, nil)},
		{MemberID: "m01", ProtocolMetadata: newMemberMetadata(t, topic, nil)},
	}
	plan := balanceWith(t, b, members, topic, 4)
	require.Empty(t, collectAssignedPartitions(t, plan, topic),
		"empty ring must assign no partitions")
}

// At converged steady state:
//
//  1. Every Active ring partition is owned by exactly one distinct
//     group member.
//  2. Inactive ring partitions are NOT sticky-assigned — they flow
//     through the inactive round-robin pool alongside Kafka partitions
//     absent from the ring.
//
// Both properties must hold across any sequence of cluster mutations
// (members joining/leaving, partitions becoming Active or Inactive).
func TestCooperativeActiveStickyBalancer_ConvergesToOneActivePerMember(t *testing.T) {
	t.Parallel()

	const topic = "ingest"
	const totalPartitions int32 = 1000

	// Cluster state mutated by the steps below.
	ring := newFakePartitionRing()
	balancer := NewCooperativeActiveStickyBalancer(ring, log.NewNopLogger())
	memberCount := 0

	// Drive Balance() until the plan stops changing. Cooperative-sticky
	// usually takes 1-2 iterations per mutation (revoke phase then
	// assign phase).
	converge := func(prior map[string][]int32) map[string][]int32 {
		t.Helper()
		const maxIters = 5
		current := prior
		for range maxIters {
			members := makeMembers(t, memberCount, topic, current)
			plan := balanceWith(t, balancer, members, topic, totalPartitions)
			next := perMemberPartitions(t, plan, topic)
			if samePartitionMap(current, next) {
				return next
			}
			current = next
		}
		t.Fatalf("balancer did not converge after %d iterations", maxIters)
		return nil
	}

	// assertActivesPartitionAreSpread: each Active partition has exactly one owner, and
	// no member holds more than one Active.
	assertActivesPartitionAreSpread := func(active []int32, plan map[string][]int32) {
		t.Helper()
		owners := map[int32]string{}
		for member, parts := range plan {
			for _, p := range parts {
				for _, a := range active {
					if p == a {
						owners[a] = member
					}
				}
			}
		}
		require.Lenf(t, owners, len(active),
			"every active must have an owner; got %v", owners)
		counts := map[string]int{}
		for _, m := range owners {
			counts[m]++
		}
		for owner, n := range counts {
			require.Equalf(t, 1, n,
				"%s holds %d actives, expected 1; owners=%v", owner, n, owners)
		}
	}

	// assertInactivesPartitionsAreNotSticky: Inactive ring partitions don't get dedicated
	// members. They share holders with the actives via the round-robin
	// pool (both walk members from m00), so the union of holders of
	// (active ∪ inactive) is bounded by max(N_active, N_inactive). If
	// Inactive partitions were sticky-assigned, each would land on a
	// distinct member and the union would swell to N_active+N_inactive.
	assertInactivesPartitionsAreNotSticky := func(active, inactive []int32, plan map[string][]int32) {
		t.Helper()
		holders := map[string]bool{}
		for member, parts := range plan {
			for _, p := range parts {
				for _, a := range active {
					if p == a {
						holders[member] = true
					}
				}
				for _, i := range inactive {
					if p == i {
						holders[member] = true
					}
				}
			}
		}
		bound := max(len(inactive), len(active))
		require.LessOrEqualf(t, len(holders), bound,
			"ring partitions {actives=%v, inactives=%v} must be held by at most %d "+
				"distinct members (max(N_active, N_inactive)); got %d holders=%v. "+
				"More than %d means Inactive partitions are being sticky-assigned.",
			active, inactive, bound, len(holders), holders, bound)
	}

	steps := []struct {
		name   string
		mutate func()
		check  func(active []int32, prev, next map[string][]int32)
	}{
		{
			name: "bootstrap: 5 members join, ring has 3 active partitions",
			mutate: func() {
				memberCount = 5
				ring.markActive(0)
				ring.markActive(1)
				ring.markActive(2)
			},
			check: func(active []int32, prev, next map[string][]int32) {
				require.ElementsMatch(t, []int32{0, 1, 2}, active)
				assertActivesPartitionAreSpread(active, next)
			},
		},
		{
			name: "scale up to 20 members",
			mutate: func() {
				memberCount = 20
			},
			check: func(active []int32, prev, next map[string][]int32) {
				require.ElementsMatch(t, []int32{0, 1, 2}, active)
				assertActivesPartitionAreSpread(active, next)
			},
		},
		{
			name: "ring grows: partitions 3, 4, 5 become Active",
			mutate: func() {
				ring.markActive(3)
				ring.markActive(4)
				ring.markActive(5)
			},
			check: func(active []int32, prev, next map[string][]int32) {
				require.ElementsMatch(t, []int32{0, 1, 2, 3, 4, 5}, active)
				assertActivesPartitionAreSpread(active, next)
			},
		},
		{
			name: "scale down to 8 members",
			mutate: func() {
				memberCount = 8
			},
			check: func(active []int32, prev, next map[string][]int32) {
				require.ElementsMatch(t, []int32{0, 1, 2, 3, 4, 5}, active)
				assertActivesPartitionAreSpread(active, next)
			},
		},
		{
			name: "partitions 2, 3, 4, 5 transition to Inactive",
			mutate: func() {
				ring.markInactive(2)
				ring.markInactive(3)
				ring.markInactive(4)
				ring.markInactive(5)
			},
			check: func(active []int32, prev, next map[string][]int32) {
				require.ElementsMatch(t, []int32{0, 1}, active)
				assertActivesPartitionAreSpread(active, next)
				assertInactivesPartitionsAreNotSticky(active, []int32{2, 3, 4, 5}, next)
			},
		},
	}

	var prev map[string][]int32
	for _, s := range steps {
		s.mutate()
		next := converge(prev)
		active := ring.PartitionRing().ActivePartitionIDs()
		s.check(active, prev, next)
		prev = next
	}
}

// balanceWith drives one round of rebalance against the given balancer
// and member set. Callers keep the balancer (and its underlying ring
// reader) across rounds so prior state and ring mutations are preserved.
func balanceWith(t *testing.T, b kgo.GroupBalancer, members []kmsg.JoinGroupResponseMember, topic string, total int32) []kmsg.SyncGroupRequestGroupAssignment {
	t.Helper()
	type memberBalancerProvider interface {
		MemberBalancer([]kmsg.JoinGroupResponseMember) (kgo.GroupMemberBalancer, map[string]struct{}, error)
	}
	mb, _, err := b.(memberBalancerProvider).MemberBalancer(members)
	require.NoError(t, err)
	assignment, err := mb.(kgo.GroupMemberBalancerOrError).BalanceOrError(map[string]int32{topic: total})
	require.NoError(t, err)
	return assignment.IntoSyncAssignment()
}

// makeMembers builds n members named "m00".."m{n-1}" (zero-padded so
// lexicographic sort matches numeric order). owned maps memberID to
// the partitions to claim as previously-owned.
func makeMembers(t *testing.T, n int, topic string, owned map[string][]int32) []kmsg.JoinGroupResponseMember {
	t.Helper()
	members := make([]kmsg.JoinGroupResponseMember, n)
	for i := range n {
		id := fmt.Sprintf("m%02d", i)
		members[i] = kmsg.JoinGroupResponseMember{
			MemberID:         id,
			ProtocolMetadata: newMemberMetadata(t, topic, owned[id]),
		}
	}
	return members
}

// newMemberMetadata builds the kmsg.ConsumerMemberMetadata bytes that
// would be sent by a member during JoinGroup. owned may be nil for a
// fresh member with no prior assignment.
func newMemberMetadata(t *testing.T, topic string, owned []int32) []byte {
	t.Helper()
	meta := kmsg.NewConsumerMemberMetadata()
	meta.Version = 3
	meta.Topics = []string{topic}
	meta.Generation = 1
	if len(owned) > 0 {
		op := kmsg.NewConsumerMemberMetadataOwnedPartition()
		op.Topic = topic
		op.Partitions = owned
		meta.OwnedPartitions = append(meta.OwnedPartitions, op)
	}
	return meta.AppendTo(nil)
}

// collectAssignedPartitions returns every partition assigned across all
// members for the given topic in plan.
func collectAssignedPartitions(t *testing.T, plan []kmsg.SyncGroupRequestGroupAssignment, topic string) []int32 {
	t.Helper()
	var out []int32
	for _, assn := range plan {
		var meta kmsg.ConsumerMemberAssignment
		require.NoError(t, meta.ReadFrom(assn.MemberAssignment))
		for _, ta := range meta.Topics {
			if ta.Topic == topic {
				out = append(out, ta.Partitions...)
			}
		}
	}
	return out
}

// perMemberPartitions returns memberID -> sorted partition list for
// the given topic.
func perMemberPartitions(t *testing.T, plan []kmsg.SyncGroupRequestGroupAssignment, topic string) map[string][]int32 {
	t.Helper()
	out := make(map[string][]int32, len(plan))
	for _, assn := range plan {
		var meta kmsg.ConsumerMemberAssignment
		require.NoError(t, meta.ReadFrom(assn.MemberAssignment))
		for _, ta := range meta.Topics {
			if ta.Topic == topic {
				parts := append([]int32(nil), ta.Partitions...)
				slices.Sort(parts)
				out[assn.MemberID] = parts
			}
		}
		if _, ok := out[assn.MemberID]; !ok {
			out[assn.MemberID] = nil
		}
	}
	return out
}

// samePartitionMap reports whether two per-member assignment maps are
// equal. Used to detect convergence between Balance() iterations.
func samePartitionMap(a, b map[string][]int32) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		if len(av) != len(bv) {
			return false
		}
		for i := range av {
			if av[i] != bv[i] {
				return false
			}
		}
	}
	return true
}
