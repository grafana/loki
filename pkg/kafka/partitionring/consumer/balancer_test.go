package consumer

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Helper types for testing
type memberUpdate struct {
	memberID string
	topics   map[string][]int32
}

type mockPartitionRing struct {
	partitionIDs []int32
}

// Mock implementation of PartitionRing interface
func (m *mockPartitionRing) PartitionIDs() []int32 {
	return m.partitionIDs
}

type mockPartitionRingReader struct {
	ring *mockPartitionRing
}

func (m *mockPartitionRingReader) PartitionRing() *ring.PartitionRing {
	desc := ring.PartitionRingDesc{
		Partitions: make(map[int32]ring.PartitionDesc),
	}
	for _, id := range m.ring.partitionIDs {
		desc.Partitions[id] = ring.PartitionDesc{
			Id:     id,
			State:  ring.PartitionActive,
			Tokens: []uint32{uint32(id)}, // Use partition ID as token for simplicity
		}
	}
	return ring.NewPartitionRing(desc)
}

func TestCooperativeActiveStickyBalancer(t *testing.T) {
	type memberState struct {
		id                string
		currentPartitions []int32 // nil means new member
	}

	type memberResult struct {
		id         string
		partitions []int32
	}

	type testCase struct {
		name             string
		activePartitions []int32
		totalPartitions  int32
		members          []memberState
		expected         []memberResult // expected partition assignments per member
	}

	tests := []testCase{
		{
			name:             "initial assignment with two members",
			activePartitions: []int32{0, 1, 2},
			totalPartitions:  6,
			members: []memberState{
				{id: "member-1"},
				{id: "member-2"},
			},
			expected: []memberResult{
				{id: "member-1", partitions: []int32{0, 3, 5}}, // 1 active (0), 2 inactive (3,5)
				{id: "member-2", partitions: []int32{1, 2, 4}}, // 2 active (1,2), 1 inactive (4)
			},
		},
		{
			name:             "rebalance when adding third member",
			activePartitions: []int32{0, 1, 2},
			totalPartitions:  6,
			members: []memberState{
				{id: "member-1", currentPartitions: []int32{0, 3, 5}},
				{id: "member-2", currentPartitions: []int32{1, 2, 4}},
				{id: "member-3"},
			},
			expected: []memberResult{
				{id: "member-1", partitions: []int32{0, 3}}, // keeps active 0, keeps inactive 3
				{id: "member-2", partitions: []int32{1, 4}}, // keeps active 1, keeps inactive 4
				{id: "member-3", partitions: []int32{2, 5}}, // gets active 2, gets inactive 5
			},
		},
		{
			name:             "complex rebalance with more partitions",
			activePartitions: []int32{0, 1, 2, 3, 4},
			totalPartitions:  10,
			members: []memberState{
				{id: "member-1", currentPartitions: []int32{0, 1, 5, 6}},
				{id: "member-2", currentPartitions: []int32{2, 3, 7, 8}},
				{id: "member-3", currentPartitions: []int32{4, 9}},
				{id: "member-4"},
			},
			expected: []memberResult{
				{id: "member-1", partitions: []int32{0, 5, 9}}, // keeps active 0, keeps inactive 5, gets inactive 9
				{id: "member-2", partitions: []int32{2, 3, 6}}, // keeps active 2,3, gets inactive 6
				{id: "member-3", partitions: []int32{4, 7}},    // keeps active 4, gets inactive 7
				{id: "member-4", partitions: []int32{1, 8}},    // gets active 1,gets inactive 8
			},
		},
		{
			name:             "member leaves with many partitions",
			activePartitions: []int32{0, 1, 2, 3, 4, 5},
			totalPartitions:  12,
			members: []memberState{
				{id: "member-1", currentPartitions: []int32{0, 1, 6, 7}},
				{id: "member-2", currentPartitions: []int32{2, 3, 8, 9}},
				// member-3 left, had partitions: [4, 5, 10, 11]
			},
			expected: []memberResult{
				{id: "member-1", partitions: []int32{0, 1, 4, 6, 8, 10}}, // keeps active 0,1, gets active 4, keeps inactive 6, gets inactive 8,10
				{id: "member-2", partitions: []int32{2, 3, 5, 7, 9, 11}}, // keeps active 2,3, gets active 5, gets inactive 7, keeps inactive 9, gets inactive 11
			},
		},
		{
			name:             "all members leave except one",
			activePartitions: []int32{0, 1, 2, 3},
			totalPartitions:  8,
			members: []memberState{
				{id: "member-1", currentPartitions: []int32{0, 4}},
				// member-2 left, had [1, 5]
				// member-3 left, had [2, 6]
				// member-4 left, had [3, 7]
			},
			expected: []memberResult{
				{id: "member-1", partitions: []int32{0, 1, 2, 3, 4, 5, 6, 7}}, // gets all partitions
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock ring
			mockRing := &mockPartitionRing{partitionIDs: tc.activePartitions}
			mockReader := &mockPartitionRingReader{ring: mockRing}
			balancer := NewCooperativeActiveStickyBalancer(mockReader)

			// First rebalance: members announce what they want to give up
			members := make([]kmsg.JoinGroupResponseMember, len(tc.members))
			for i, m := range tc.members {
				var currentAssignment map[string][]int32
				if m.currentPartitions != nil {
					currentAssignment = map[string][]int32{"topic-1": m.currentPartitions}
				}
				members[i] = kmsg.JoinGroupResponseMember{
					MemberID:         m.id,
					ProtocolMetadata: createMemberMetadata(t, []string{"topic-1"}, currentAssignment),
				}
			}

			memberBalancer, _, err := balancer.MemberBalancer(members)
			require.NoError(t, err)
			assignment, err := memberBalancer.(kgo.GroupMemberBalancerOrError).BalanceOrError(map[string]int32{"topic-1": tc.totalPartitions})
			require.NoError(t, err)
			plan := assignment.IntoSyncAssignment()

			// Get current assignments after first rebalance
			currentAssignments := make(map[string][]int32)
			for _, m := range plan {
				partitions := extractPartitions(t, plan, m.MemberID, "topic-1")
				if len(partitions) > 0 {
					currentAssignments[m.MemberID] = partitions
				}
			}

			// Second rebalance: members can take new partitions
			members = make([]kmsg.JoinGroupResponseMember, len(tc.members))
			for i, m := range tc.members {
				currentPartitions := currentAssignments[m.id]
				if currentPartitions == nil && m.currentPartitions != nil {
					currentPartitions = m.currentPartitions
				}
				members[i] = kmsg.JoinGroupResponseMember{
					MemberID:         m.id,
					ProtocolMetadata: createMemberMetadata(t, []string{"topic-1"}, map[string][]int32{"topic-1": currentPartitions}),
				}
			}

			memberBalancer, _, err = balancer.MemberBalancer(members)
			require.NoError(t, err)
			assignment, err = memberBalancer.(kgo.GroupMemberBalancerOrError).BalanceOrError(map[string]int32{"topic-1": tc.totalPartitions})
			require.NoError(t, err)
			plan = assignment.IntoSyncAssignment()

			// Verify final assignments for each member
			for _, expected := range tc.expected {
				actual := extractPartitions(t, plan, expected.id, "topic-1")
				sort.Slice(actual, func(i, j int) bool { return actual[i] < actual[j] })
				sort.Slice(expected.partitions, func(i, j int) bool { return expected.partitions[i] < expected.partitions[j] })

				require.Equal(t, expected.partitions, actual,
					"Member %s got wrong partition assignment.\nExpected: %v (active: %v)\nGot: %v (active: %v)",
					expected.id,
					expected.partitions, countActivePartitions(expected.partitions, tc.activePartitions),
					actual, countActivePartitions(actual, tc.activePartitions))
			}
		})
	}
}

// Test helpers for consumer group testing
type testConsumerGroup struct {
	t            *testing.T
	admin        *kadm.Client
	mockRing     *mockPartitionRing
	mockReader   *mockPartitionRingReader
	groupName    string
	clusterAddrs []string
}

func newTestConsumerGroup(t *testing.T, numPartitions int) *testConsumerGroup {
	// Create a fake cluster
	cluster := kfake.MustCluster(
		kfake.NumBrokers(2),
		kfake.SeedTopics(int32(numPartitions), "test-topic"),
	)
	t.Cleanup(func() { cluster.Close() })

	addrs := cluster.ListenAddrs()
	require.NotEmpty(t, addrs)

	// Create admin client
	admClient, err := kgo.NewClient(
		kgo.SeedBrokers(addrs...),
	)
	require.NoError(t, err)
	t.Cleanup(func() { admClient.Close() })

	admin := kadm.NewClient(admClient)
	t.Cleanup(func() { admin.Close() })

	// Create mock ring with first 3 partitions active
	mockRing := &mockPartitionRing{partitionIDs: []int32{0, 1, 2}}
	mockReader := &mockPartitionRingReader{ring: mockRing}

	return &testConsumerGroup{
		t:            t,
		admin:        admin,
		mockRing:     mockRing,
		mockReader:   mockReader,
		groupName:    "test-group",
		clusterAddrs: addrs,
	}
}

func (g *testConsumerGroup) createConsumer(id string) *kgo.Client {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(g.clusterAddrs...),
		kgo.ConsumerGroup(g.groupName),
		kgo.ConsumeTopics("test-topic"),
		kgo.Balancers(NewCooperativeActiveStickyBalancer(g.mockReader)),
		kgo.ClientID(id),
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, m map[string][]int32) {
			g.t.Logf("Assigned partitions 1: %v", m)
		}),
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, m map[string][]int32) {
			g.t.Logf("Assigned partitions 2: %v", m)
		}),
	)
	require.NoError(g.t, err)
	return client
}

func (g *testConsumerGroup) getAssignments() map[string][]int32 {
	g.t.Helper()
	ctx := context.Background()
	groups, err := g.admin.DescribeGroups(ctx, g.groupName)
	require.NoError(g.t, err)

	require.Len(g.t, groups, 1)
	group := groups[g.groupName]

	assignments := make(map[string][]int32)
	for _, member := range group.Members {
		// Extract base member ID (without the suffix)
		baseMemberID := member.ClientID

		c, ok := member.Assigned.AsConsumer()
		require.True(g.t, ok)
		for _, topic := range c.Topics {
			if topic.Topic == "test-topic" {
				assignments[baseMemberID] = topic.Partitions
			}
		}
	}
	return assignments
}

func (g *testConsumerGroup) waitForStableAssignments(expectedMembers int, timeout time.Duration) map[string][]int32 {
	g.t.Helper()
	deadline := time.Now().Add(timeout)
	var lastAssignments map[string][]int32

	for time.Now().Before(deadline) {
		assignments := g.getAssignments()
		if len(assignments) == expectedMembers {
			// Check if assignments are stable
			if lastAssignments != nil {
				stable := true
				for id, parts := range assignments {
					lastParts, ok := lastAssignments[id]
					if !ok || !equalPartitions(parts, lastParts) {
						stable = false
						break
					}
				}
				if stable {
					return assignments
				}
			}
			lastAssignments = assignments
		}
		time.Sleep(100 * time.Millisecond)
	}
	g.t.Fatalf("Timeout waiting for stable assignments with %d members", expectedMembers)
	return nil
}

func TestCooperativeActiveStickyBalancerE2E(t *testing.T) {
	group := newTestConsumerGroup(t, 6)

	// Create first consumer
	consumer1 := group.createConsumer("member-1")
	defer consumer1.Close()

	// Wait for initial assignment
	assignments := group.waitForStableAssignments(1, 5*time.Second)
	t.Log("Initial state:")
	t.Logf("Assignments: %v", assignments)
	require.NotEmpty(t, assignments["member-1"])

	// Create second consumer
	consumer2 := group.createConsumer("member-2")
	defer consumer2.Close()

	// Wait for rebalance
	assignments = group.waitForStableAssignments(2, 5*time.Second)
	t.Log("After consumer2 joins:")
	t.Logf("Assignments: %v", assignments)
	require.NotEmpty(t, assignments["member-1"])
	require.NotEmpty(t, assignments["member-2"])

	// Create third consumer
	consumer3 := group.createConsumer("member-3")
	defer consumer3.Close()

	// Wait for rebalance
	assignments = group.waitForStableAssignments(3, 5*time.Second)
	t.Log("After consumer3 joins:")
	t.Logf("Assignments: %v", assignments)
	require.NotEmpty(t, assignments["member-1"])
	require.NotEmpty(t, assignments["member-2"])
	require.NotEmpty(t, assignments["member-3"])

	// Close consumer2 to simulate it leaving
	consumer2.Close()

	// Wait for rebalance
	assignments = group.waitForStableAssignments(2, 5*time.Second)
	t.Log("After consumer2 leaves:")
	t.Logf("Assignments: %v", assignments)
	require.NotEmpty(t, assignments["member-1"])
	require.NotEmpty(t, assignments["member-3"])

	// Verify active partitions are evenly distributed
	verifyActivePartitions := func(assignments map[string][]int32) int {
		activeCount := 0
		for _, partitions := range assignments {
			for _, p := range partitions {
				if p <= 2 { // partitions 0,1,2 are active
					activeCount++
				}
			}
		}
		return activeCount
	}

	active1 := verifyActivePartitions(map[string][]int32{"test-topic": assignments["member-1"]})
	active3 := verifyActivePartitions(map[string][]int32{"test-topic": assignments["member-3"]})
	t.Logf("Active partitions - Consumer1: %d, Consumer3: %d", active1, active3)
	require.True(t, abs(active1-active3) <= 1, "Active partitions should be evenly distributed")
}

// Helper to check if two partition slices are equal
func equalPartitions(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Helper function to count active partitions in an assignment
func countActivePartitions(partitions []int32, activePartitions []int32) int {
	count := 0
	for _, p := range partitions {
		if contains(activePartitions, p) {
			count++
		}
	}
	return count
}

// Helper function to get absolute difference
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Helper function to check if a slice contains a value
func contains(slice []int32, val int32) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// Helper functions that create metadata for a member
func createMemberMetadata(t *testing.T, topics []string, currentAssignment map[string][]int32) []byte {
	t.Helper()
	meta := kmsg.NewConsumerMemberMetadata()
	meta.Version = 3
	meta.Topics = topics
	meta.Generation = 1

	if currentAssignment != nil {
		for topic, partitions := range currentAssignment {
			owned := kmsg.NewConsumerMemberMetadataOwnedPartition()
			owned.Topic = topic
			owned.Partitions = partitions
			meta.OwnedPartitions = append(meta.OwnedPartitions, owned)
		}
		sort.Slice(meta.OwnedPartitions, func(i, j int) bool {
			return meta.OwnedPartitions[i].Topic < meta.OwnedPartitions[j].Topic
		})
	}

	return meta.AppendTo(nil)
}

// Helper function to extract partitions from a plan
func extractPartitions(t *testing.T, plan []kmsg.SyncGroupRequestGroupAssignment, memberID, topic string) []int32 {
	t.Helper()
	for _, assignment := range plan {
		if assignment.MemberID == memberID {
			var meta kmsg.ConsumerMemberAssignment
			err := meta.ReadFrom(assignment.MemberAssignment)
			require.NoError(t, err)
			for _, topicAssignment := range meta.Topics {
				if topicAssignment.Topic == topic {
					return topicAssignment.Partitions
				}
			}
		}
	}
	return nil
}
