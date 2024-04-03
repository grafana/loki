// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/scheduler/queue/user_queues_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package queue

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/scheduler/limits"
)

var noQueueLimits = limits.NewQueueLimits(nil)

func TestQueues(t *testing.T) {
	uq := newTenantQueues(0, 0, noQueueLimits)
	assert.NotNil(t, uq)
	assert.NoError(t, isConsistent(uq))

	uq.addConsumerToConnection("consumer-1")
	uq.addConsumerToConnection("consumer-2")

	q, u, lastUserIndex := uq.getNextQueueForConsumer(-1, "consumer-1")
	assert.Nil(t, q)
	assert.Equal(t, "", u)

	// Add queues: [one]
	qOne := getOrAdd(t, uq, "one")
	lastUserIndex = confirmOrderForConsumer(t, uq, "consumer-1", lastUserIndex, qOne, qOne)

	// [one two]
	qTwo := getOrAdd(t, uq, "two")
	assert.NotEqual(t, qOne, qTwo)

	lastUserIndex = confirmOrderForConsumer(t, uq, "consumer-1", lastUserIndex, qTwo, qOne, qTwo, qOne)
	confirmOrderForConsumer(t, uq, "consumer-2", -1, qOne, qTwo, qOne)

	// [one two three]
	// confirm fifo by adding a third queue and iterating to it
	qThree := getOrAdd(t, uq, "three")

	lastUserIndex = confirmOrderForConsumer(t, uq, "consumer-1", lastUserIndex, qTwo, qThree, qOne)

	// Remove one: ["" two three]
	uq.deleteQueue("one")
	assert.NoError(t, isConsistent(uq))

	lastUserIndex = confirmOrderForConsumer(t, uq, "consumer-1", lastUserIndex, qTwo, qThree, qTwo)

	// "four" is added at the beginning of the list: [four two three]
	qFour := getOrAdd(t, uq, "four")

	lastUserIndex = confirmOrderForConsumer(t, uq, "consumer-1", lastUserIndex, qThree, qFour, qTwo, qThree)

	// Remove two: [four "" three]
	uq.deleteQueue("two")
	assert.NoError(t, isConsistent(uq))

	lastUserIndex = confirmOrderForConsumer(t, uq, "consumer-1", lastUserIndex, qFour, qThree, qFour)

	// Remove three: [four]
	uq.deleteQueue("three")
	assert.NoError(t, isConsistent(uq))

	// Remove four: []
	uq.deleteQueue("four")
	assert.NoError(t, isConsistent(uq))

	q, _, _ = uq.getNextQueueForConsumer(lastUserIndex, "consumer-1")
	assert.Nil(t, q)
}

func TestQueuesOnTerminatingConsumer(t *testing.T) {
	uq := newTenantQueues(0, 0, noQueueLimits)
	assert.NotNil(t, uq)
	assert.NoError(t, isConsistent(uq))

	uq.addConsumerToConnection("consumer-1")
	uq.addConsumerToConnection("consumer-2")

	// Add queues: [one, two]
	qOne := getOrAdd(t, uq, "one")
	qTwo := getOrAdd(t, uq, "two")
	confirmOrderForConsumer(t, uq, "consumer-1", -1, qOne, qTwo, qOne, qTwo)
	confirmOrderForConsumer(t, uq, "consumer-2", -1, qOne, qTwo, qOne, qTwo)

	// After notify shutdown for consumer-2, it's expected to own no queue.
	uq.notifyQuerierShutdown("consumer-2")
	q, u, _ := uq.getNextQueueForConsumer(-1, "consumer-2")
	assert.Nil(t, q)
	assert.Equal(t, "", u)

	// However, consumer-1 still get queues because it's still running.
	confirmOrderForConsumer(t, uq, "consumer-1", -1, qOne, qTwo, qOne, qTwo)

	// After disconnecting consumer-2, it's expected to own no queue.
	uq.removeConsumer("consumer-2")
	q, u, _ = uq.getNextQueueForConsumer(-1, "consumer-2")
	assert.Nil(t, q)
	assert.Equal(t, "", u)
}

func TestQueuesWithConsumers(t *testing.T) {
	maxConsumers := 5
	uq := newTenantQueues(0, 0, &mockQueueLimits{maxConsumers: maxConsumers})
	assert.NotNil(t, uq)
	assert.NoError(t, isConsistent(uq))

	consumers := 30
	users := 1000

	// Add some consumers.
	for ix := 0; ix < consumers; ix++ {
		qid := fmt.Sprintf("consumer-%d", ix)
		uq.addConsumerToConnection(qid)

		// No consumer has any queues yet.
		q, u, _ := uq.getNextQueueForConsumer(-1, qid)
		assert.Nil(t, q)
		assert.Equal(t, "", u)
	}

	assert.NoError(t, isConsistent(uq))

	// Add user queues.
	for u := 0; u < users; u++ {
		uid := fmt.Sprintf("user-%d", u)
		getOrAdd(t, uq, uid)

		// Verify it has maxConsumers consumers assigned now.
		qs := uq.mapping.GetByKey(uid).consumers
		assert.Equal(t, maxConsumers, len(qs))
	}

	// After adding all users, verify results. For each consumer, find out how many different users it handles,
	// and compute mean and stdDev.
	consumerMap := make(map[string]int)

	for q := 0; q < consumers; q++ {
		qid := fmt.Sprintf("consumer-%d", q)

		lastUserIndex := StartIndex
		for {
			_, _, newIx := uq.getNextQueueForConsumer(lastUserIndex, qid)
			if newIx < lastUserIndex {
				break
			}
			lastUserIndex = newIx
			consumerMap[qid]++
		}
	}

	mean := float64(0)
	for _, c := range consumerMap {
		mean += float64(c)
	}
	mean = mean / float64(len(consumerMap))

	stdDev := float64(0)
	for _, c := range consumerMap {
		d := float64(c) - mean
		stdDev += (d * d)
	}
	stdDev = math.Sqrt(stdDev / float64(len(consumerMap)))
	t.Log("mean:", mean, "stddev:", stdDev)

	assert.InDelta(t, users*maxConsumers/consumers, mean, 1)
	assert.InDelta(t, stdDev, 0, mean*0.2)
}

func TestQueuesConsistency(t *testing.T) {
	tests := map[string]struct {
		forgetDelay time.Duration
	}{
		"without forget delay": {},
		"with forget delay":    {forgetDelay: time.Minute},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			uq := newTenantQueues(0, testData.forgetDelay, &mockQueueLimits{maxConsumers: 3})
			assert.NotNil(t, uq)
			assert.NoError(t, isConsistent(uq))

			r := rand.New(rand.NewSource(time.Now().Unix()))

			lastUserIndexes := map[string]QueueIndex{}

			conns := map[string]int{}

			for i := 0; i < 10000; i++ {
				switch r.Int() % 6 {
				case 0:
					q, err := uq.getOrAddQueue(generateTenant(r), generateActor(r))
					assert.NoError(t, err)
					assert.NotNil(t, q)
				case 1:
					qid := generateConsumer(r)
					_, _, luid := uq.getNextQueueForConsumer(lastUserIndexes[qid], qid)
					lastUserIndexes[qid] = luid
				case 2:
					uq.deleteQueue(generateTenant(r))
				case 3:
					q := generateConsumer(r)
					uq.addConsumerToConnection(q)
					conns[q]++
				case 4:
					q := generateConsumer(r)
					if conns[q] > 0 {
						uq.removeConsumerConnection(q, time.Now())
						conns[q]--
					}
				case 5:
					q := generateConsumer(r)
					uq.notifyQuerierShutdown(q)
				}

				assert.NoErrorf(t, isConsistent(uq), "last action %d", i)
			}
		})
	}
}

func TestQueues_ForgetDelay(t *testing.T) {
	const (
		forgetDelay  = time.Minute
		maxConsumers = 1
		numUsers     = 100
	)

	now := time.Now()
	uq := newTenantQueues(0, forgetDelay, &mockQueueLimits{maxConsumers: maxConsumers})
	assert.NotNil(t, uq)
	assert.NoError(t, isConsistent(uq))

	// 3 consumers open 2 connections each.
	for i := 1; i <= 3; i++ {
		uq.addConsumerToConnection(fmt.Sprintf("consumer-%d", i))
		uq.addConsumerToConnection(fmt.Sprintf("consumer-%d", i))
	}

	// Add user queues.
	for i := 0; i < numUsers; i++ {
		userID := fmt.Sprintf("user-%d", i)
		getOrAdd(t, uq, userID)
	}

	// We expect consumer-1 to have some users.
	consumer1Users := getUsersByConsumer(uq, "consumer-1")
	require.NotEmpty(t, consumer1Users)

	// Gracefully shutdown consumer-1.
	uq.removeConsumerConnection("consumer-1", now.Add(20*time.Second))
	uq.removeConsumerConnection("consumer-1", now.Add(21*time.Second))
	uq.notifyQuerierShutdown("consumer-1")

	// We expect consumer-1 has been removed.
	assert.NotContains(t, uq.consumers, "consumer-1")
	assert.NoError(t, isConsistent(uq))

	// We expect consumer-1 users have been shuffled to other consumers.
	for _, userID := range consumer1Users {
		assert.Contains(t, append(getUsersByConsumer(uq, "consumer-2"), getUsersByConsumer(uq, "consumer-3")...), userID)
	}

	// Consumer-1 reconnects.
	uq.addConsumerToConnection("consumer-1")
	uq.addConsumerToConnection("consumer-1")

	// We expect the initial consumer-1 users have got back to consumer-1.
	for _, userID := range consumer1Users {
		assert.Contains(t, getUsersByConsumer(uq, "consumer-1"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-2"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-3"), userID)
	}

	// Consumer-1 abruptly terminates (no shutdown notification received).
	uq.removeConsumerConnection("consumer-1", now.Add(40*time.Second))
	uq.removeConsumerConnection("consumer-1", now.Add(41*time.Second))

	// We expect consumer-1 has NOT been removed.
	assert.Contains(t, uq.consumers, "consumer-1")
	assert.NoError(t, isConsistent(uq))

	// We expect the consumer-1 users have not been shuffled to other consumers.
	for _, userID := range consumer1Users {
		assert.Contains(t, getUsersByConsumer(uq, "consumer-1"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-2"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-3"), userID)
	}

	// Try to forget disconnected consumers, but consumer-1 forget delay hasn't passed yet.
	uq.forgetDisconnectedConsumers(now.Add(90 * time.Second))

	assert.Contains(t, uq.consumers, "consumer-1")
	assert.NoError(t, isConsistent(uq))

	for _, userID := range consumer1Users {
		assert.Contains(t, getUsersByConsumer(uq, "consumer-1"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-2"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-3"), userID)
	}

	// Try to forget disconnected consumers. This time consumer-1 forget delay has passed.
	uq.forgetDisconnectedConsumers(now.Add(105 * time.Second))

	assert.NotContains(t, uq.consumers, "consumer-1")
	assert.NoError(t, isConsistent(uq))

	// We expect consumer-1 users have been shuffled to other consumers.
	for _, userID := range consumer1Users {
		assert.Contains(t, append(getUsersByConsumer(uq, "consumer-2"), getUsersByConsumer(uq, "consumer-3")...), userID)
	}
}

func TestQueues_ForgetDelay_ShouldCorrectlyHandleConsumerReconnectingBeforeForgetDelayIsPassed(t *testing.T) {
	const (
		forgetDelay  = time.Minute
		maxConsumers = 1
		numUsers     = 100
	)

	now := time.Now()
	uq := newTenantQueues(0, forgetDelay, &mockQueueLimits{maxConsumers: maxConsumers})
	assert.NotNil(t, uq)
	assert.NoError(t, isConsistent(uq))

	// 3 consumers open 2 connections each.
	for i := 1; i <= 3; i++ {
		uq.addConsumerToConnection(fmt.Sprintf("consumer-%d", i))
		uq.addConsumerToConnection(fmt.Sprintf("consumer-%d", i))
	}

	// Add user queues.
	for i := 0; i < numUsers; i++ {
		userID := fmt.Sprintf("user-%d", i)
		getOrAdd(t, uq, userID)
	}

	// We expect consumer-1 to have some users.
	consumer1Users := getUsersByConsumer(uq, "consumer-1")
	require.NotEmpty(t, consumer1Users)

	// Consumer-1 abruptly terminates (no shutdown notification received).
	uq.removeConsumerConnection("consumer-1", now.Add(40*time.Second))
	uq.removeConsumerConnection("consumer-1", now.Add(41*time.Second))

	// We expect consumer-1 has NOT been removed.
	assert.Contains(t, uq.consumers, "consumer-1")
	assert.NoError(t, isConsistent(uq))

	// We expect the consumer-1 users have not been shuffled to other consumers.
	for _, userID := range consumer1Users {
		assert.Contains(t, getUsersByConsumer(uq, "consumer-1"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-2"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-3"), userID)
	}

	// Try to forget disconnected consumers, but consumer-1 forget delay hasn't passed yet.
	uq.forgetDisconnectedConsumers(now.Add(90 * time.Second))

	// Consumer-1 reconnects.
	uq.addConsumerToConnection("consumer-1")
	uq.addConsumerToConnection("consumer-1")

	assert.Contains(t, uq.consumers, "consumer-1")
	assert.NoError(t, isConsistent(uq))

	// We expect the consumer-1 users have not been shuffled to other consumers.
	for _, userID := range consumer1Users {
		assert.Contains(t, getUsersByConsumer(uq, "consumer-1"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-2"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-3"), userID)
	}

	// Try to forget disconnected consumers far in the future, but there's no disconnected consumer.
	uq.forgetDisconnectedConsumers(now.Add(200 * time.Second))

	assert.Contains(t, uq.consumers, "consumer-1")
	assert.NoError(t, isConsistent(uq))

	for _, userID := range consumer1Users {
		assert.Contains(t, getUsersByConsumer(uq, "consumer-1"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-2"), userID)
		assert.NotContains(t, getUsersByConsumer(uq, "consumer-3"), userID)
	}
}

func generateActor(r *rand.Rand) []string {
	return []string{fmt.Sprint("actor-", r.Int()%10)}
}

func generateTenant(r *rand.Rand) string {
	return fmt.Sprint("tenant-", r.Int()%5)
}

func generateConsumer(r *rand.Rand) string {
	return fmt.Sprint("consumer-", r.Int()%5)
}

func getOrAdd(t *testing.T, uq *tenantQueues, tenant string) Queue {
	actor := []string{}
	q, err := uq.getOrAddQueue(tenant, actor)
	assert.NoError(t, err)
	assert.NotNil(t, q)
	assert.NoError(t, isConsistent(uq))
	q2, err := uq.getOrAddQueue(tenant, actor)
	assert.NoError(t, err)
	assert.Equal(t, q, q2)
	return q
}

func confirmOrderForConsumer(t *testing.T, uq *tenantQueues, consumer string, lastUserIndex QueueIndex, qs ...Queue) QueueIndex {
	t.Helper()
	var n Queue
	for _, q := range qs {
		n, _, lastUserIndex = uq.getNextQueueForConsumer(lastUserIndex, consumer)
		assert.Equal(t, q, n)
		assert.NoError(t, isConsistent(uq))
	}
	return lastUserIndex
}

func isConsistent(uq *tenantQueues) error {
	if len(uq.sortedConsumers) != len(uq.consumers) {
		return fmt.Errorf("inconsistent number of sorted consumers and consumer connections")
	}

	uc := 0
	for _, u := range uq.mapping.Keys() {
		q := uq.mapping.GetByKey(u)
		if u != empty && q == nil {
			return fmt.Errorf("user %s doesn't have queue", u)
		}
		if u == empty && q != nil {
			return fmt.Errorf("user %s shouldn't have queue", u)
		}
		if u == empty {
			continue
		}

		uc++

		maxConsumers := uq.limits.MaxConsumers(u, len(uq.consumers))
		if maxConsumers == 0 && q.consumers != nil {
			return fmt.Errorf("consumers for user %s should be nil when no limits are set (when MaxConsumers is 0)", u)
		}

		if maxConsumers > 0 && len(uq.sortedConsumers) <= maxConsumers && q.consumers != nil {
			return fmt.Errorf("consumers for user %s should be nil when MaxConsumers allowed is higher than the available consumers", u)
		}

		if maxConsumers > 0 && len(uq.sortedConsumers) > maxConsumers && len(q.consumers) != maxConsumers {
			return fmt.Errorf("user %s has incorrect number of consumers, expected=%d, got=%d", u, maxConsumers, len(q.consumers))
		}
	}

	if uc != uq.mapping.Len() {
		return fmt.Errorf("inconsistent number of users list and user queues")
	}

	return nil
}

// getUsersByConsumer returns the list of users handled by the provided consumerID.
func getUsersByConsumer(queues *tenantQueues, consumerID string) []string {
	var userIDs []string
	for _, userID := range queues.mapping.Keys() {
		q := queues.mapping.GetByKey(userID)
		if q.consumers == nil {
			// If it's nil then all consumers can handle this user.
			userIDs = append(userIDs, userID)
			continue
		}
		if _, ok := q.consumers[consumerID]; ok {
			userIDs = append(userIDs, userID)
		}
	}
	return userIDs
}

func TestShuffleConsumers(t *testing.T) {
	allConsumers := []string{"a", "b", "c", "d", "e"}

	require.Nil(t, shuffleConsumersForTenants(12345, 10, allConsumers, nil))
	require.Nil(t, shuffleConsumersForTenants(12345, len(allConsumers), allConsumers, nil))

	r1 := shuffleConsumersForTenants(12345, 3, allConsumers, nil)
	require.Equal(t, 3, len(r1))

	// Same input produces same output.
	r2 := shuffleConsumersForTenants(12345, 3, allConsumers, nil)
	require.Equal(t, 3, len(r2))
	require.Equal(t, r1, r2)
}

func TestShuffleConsumersCorrectness(t *testing.T) {
	const consumersCount = 100

	var allSortedConsumers []string
	for i := 0; i < consumersCount; i++ {
		allSortedConsumers = append(allSortedConsumers, fmt.Sprintf("%d", i))
	}
	sort.Strings(allSortedConsumers)

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	const tests = 1000
	for i := 0; i < tests; i++ {
		toSelect := r.Intn(consumersCount)
		if toSelect == 0 {
			toSelect = 3
		}

		selected := shuffleConsumersForTenants(r.Int63(), toSelect, allSortedConsumers, nil)

		require.Equal(t, toSelect, len(selected))

		sort.Strings(allSortedConsumers)
		prevConsumer := ""
		for _, q := range allSortedConsumers {
			require.True(t, prevConsumer < q, "non-unique consumer")
			prevConsumer = q

			ix := sort.SearchStrings(allSortedConsumers, q)
			require.True(t, ix < len(allSortedConsumers) && allSortedConsumers[ix] == q, "selected consumer is not between all consumers")
		}
	}
}

type mockQueueLimits struct {
	maxConsumers int
}

func (l *mockQueueLimits) MaxConsumers(_ string, _ int) int {
	return l.maxConsumers
}
