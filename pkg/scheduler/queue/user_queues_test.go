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
)

func TestQueues(t *testing.T) {
	uq := newUserQueues(0, 0)
	assert.NotNil(t, uq)
	assert.NoError(t, isConsistent(uq))

	uq.addQuerierConnection("querier-1")
	uq.addQuerierConnection("querier-2")

	q, u, lastUserIndex := uq.getNextQueueForQuerier(-1, "querier-1")
	assert.Nil(t, q)
	assert.Equal(t, "", u)

	// Add queues: [one]
	qOne := getOrAdd(t, uq, "one", 0)
	lastUserIndex = confirmOrderForQuerier(t, uq, "querier-1", lastUserIndex, qOne, qOne)

	// [one two]
	qTwo := getOrAdd(t, uq, "two", 0)
	assert.NotEqual(t, qOne, qTwo)

	lastUserIndex = confirmOrderForQuerier(t, uq, "querier-1", lastUserIndex, qTwo, qOne, qTwo, qOne)
	confirmOrderForQuerier(t, uq, "querier-2", -1, qOne, qTwo, qOne)

	// [one two three]
	// confirm fifo by adding a third queue and iterating to it
	qThree := getOrAdd(t, uq, "three", 0)

	lastUserIndex = confirmOrderForQuerier(t, uq, "querier-1", lastUserIndex, qTwo, qThree, qOne)

	// Remove one: ["" two three]
	uq.deleteQueue("one")
	assert.NoError(t, isConsistent(uq))

	lastUserIndex = confirmOrderForQuerier(t, uq, "querier-1", lastUserIndex, qTwo, qThree, qTwo)

	// "four" is added at the beginning of the list: [four two three]
	qFour := getOrAdd(t, uq, "four", 0)

	lastUserIndex = confirmOrderForQuerier(t, uq, "querier-1", lastUserIndex, qThree, qFour, qTwo, qThree)

	// Remove two: [four "" three]
	uq.deleteQueue("two")
	assert.NoError(t, isConsistent(uq))

	lastUserIndex = confirmOrderForQuerier(t, uq, "querier-1", lastUserIndex, qFour, qThree, qFour)

	// Remove three: [four]
	uq.deleteQueue("three")
	assert.NoError(t, isConsistent(uq))

	// Remove four: []
	uq.deleteQueue("four")
	assert.NoError(t, isConsistent(uq))

	q, _, _ = uq.getNextQueueForQuerier(lastUserIndex, "querier-1")
	assert.Nil(t, q)
}

func TestQueuesOnTerminatingQuerier(t *testing.T) {
	uq := newUserQueues(0, 0)
	assert.NotNil(t, uq)
	assert.NoError(t, isConsistent(uq))

	uq.addQuerierConnection("querier-1")
	uq.addQuerierConnection("querier-2")

	// Add queues: [one, two]
	qOne := getOrAdd(t, uq, "one", 0)
	qTwo := getOrAdd(t, uq, "two", 0)
	confirmOrderForQuerier(t, uq, "querier-1", -1, qOne, qTwo, qOne, qTwo)
	confirmOrderForQuerier(t, uq, "querier-2", -1, qOne, qTwo, qOne, qTwo)

	// After notify shutdown for querier-2, it's expected to own no queue.
	uq.notifyQuerierShutdown("querier-2")
	q, u, _ := uq.getNextQueueForQuerier(-1, "querier-2")
	assert.Nil(t, q)
	assert.Equal(t, "", u)

	// However, querier-1 still get queues because it's still running.
	confirmOrderForQuerier(t, uq, "querier-1", -1, qOne, qTwo, qOne, qTwo)

	// After disconnecting querier-2, it's expected to own no queue.
	uq.removeQuerier("querier-2")
	q, u, _ = uq.getNextQueueForQuerier(-1, "querier-2")
	assert.Nil(t, q)
	assert.Equal(t, "", u)
}

func TestQueuesWithQueriers(t *testing.T) {
	uq := newUserQueues(0, 0)
	assert.NotNil(t, uq)
	assert.NoError(t, isConsistent(uq))

	queriers := 30
	users := 1000
	maxQueriersPerUser := 5

	// Add some queriers.
	for ix := 0; ix < queriers; ix++ {
		qid := fmt.Sprintf("querier-%d", ix)
		uq.addQuerierConnection(qid)

		// No querier has any queues yet.
		q, u, _ := uq.getNextQueueForQuerier(-1, qid)
		assert.Nil(t, q)
		assert.Equal(t, "", u)
	}

	assert.NoError(t, isConsistent(uq))

	// Add user queues.
	for u := 0; u < users; u++ {
		uid := fmt.Sprintf("user-%d", u)
		getOrAdd(t, uq, uid, maxQueriersPerUser)

		// Verify it has maxQueriersPerUser queriers assigned now.
		qs := uq.userQueues[uid].queriers
		assert.Equal(t, maxQueriersPerUser, len(qs))
	}

	// After adding all users, verify results. For each querier, find out how many different users it handles,
	// and compute mean and stdDev.
	queriersMap := make(map[string]int)

	for q := 0; q < queriers; q++ {
		qid := fmt.Sprintf("querier-%d", q)

		lastUserIndex := FirstUser
		for {
			_, _, newIx := uq.getNextQueueForQuerier(lastUserIndex, qid)
			if newIx < lastUserIndex {
				break
			}
			lastUserIndex = newIx
			queriersMap[qid]++
		}
	}

	mean := float64(0)
	for _, c := range queriersMap {
		mean += float64(c)
	}
	mean = mean / float64(len(queriersMap))

	stdDev := float64(0)
	for _, c := range queriersMap {
		d := float64(c) - mean
		stdDev += (d * d)
	}
	stdDev = math.Sqrt(stdDev / float64(len(queriersMap)))
	t.Log("mean:", mean, "stddev:", stdDev)

	assert.InDelta(t, users*maxQueriersPerUser/queriers, mean, 1)
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
			uq := newUserQueues(0, testData.forgetDelay)
			assert.NotNil(t, uq)
			assert.NoError(t, isConsistent(uq))

			r := rand.New(rand.NewSource(time.Now().Unix()))

			lastUserIndexes := map[string]UserIndex{}

			conns := map[string]int{}

			for i := 0; i < 10000; i++ {
				switch r.Int() % 6 {
				case 0:
					assert.NotNil(t, uq.getOrAddQueue(generateTenant(r), 3))
				case 1:
					qid := generateQuerier(r)
					_, _, luid := uq.getNextQueueForQuerier(lastUserIndexes[qid], qid)
					lastUserIndexes[qid] = luid
				case 2:
					uq.deleteQueue(generateTenant(r))
				case 3:
					q := generateQuerier(r)
					uq.addQuerierConnection(q)
					conns[q]++
				case 4:
					q := generateQuerier(r)
					if conns[q] > 0 {
						uq.removeQuerierConnection(q, time.Now())
						conns[q]--
					}
				case 5:
					q := generateQuerier(r)
					uq.notifyQuerierShutdown(q)
				}

				assert.NoErrorf(t, isConsistent(uq), "last action %d", i)
			}
		})
	}
}

func TestQueues_ForgetDelay(t *testing.T) {
	const (
		forgetDelay        = time.Minute
		maxQueriersPerUser = 1
		numUsers           = 100
	)

	now := time.Now()
	uq := newUserQueues(0, forgetDelay)
	assert.NotNil(t, uq)
	assert.NoError(t, isConsistent(uq))

	// 3 queriers open 2 connections each.
	for i := 1; i <= 3; i++ {
		uq.addQuerierConnection(fmt.Sprintf("querier-%d", i))
		uq.addQuerierConnection(fmt.Sprintf("querier-%d", i))
	}

	// Add user queues.
	for i := 0; i < numUsers; i++ {
		userID := fmt.Sprintf("user-%d", i)
		getOrAdd(t, uq, userID, maxQueriersPerUser)
	}

	// We expect querier-1 to have some users.
	querier1Users := getUsersByQuerier(uq, "querier-1")
	require.NotEmpty(t, querier1Users)

	// Gracefully shutdown querier-1.
	uq.removeQuerierConnection("querier-1", now.Add(20*time.Second))
	uq.removeQuerierConnection("querier-1", now.Add(21*time.Second))
	uq.notifyQuerierShutdown("querier-1")

	// We expect querier-1 has been removed.
	assert.NotContains(t, uq.queriers, "querier-1")
	assert.NoError(t, isConsistent(uq))

	// We expect querier-1 users have been shuffled to other queriers.
	for _, userID := range querier1Users {
		assert.Contains(t, append(getUsersByQuerier(uq, "querier-2"), getUsersByQuerier(uq, "querier-3")...), userID)
	}

	// Querier-1 reconnects.
	uq.addQuerierConnection("querier-1")
	uq.addQuerierConnection("querier-1")

	// We expect the initial querier-1 users have got back to querier-1.
	for _, userID := range querier1Users {
		assert.Contains(t, getUsersByQuerier(uq, "querier-1"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-2"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-3"), userID)
	}

	// Querier-1 abruptly terminates (no shutdown notification received).
	uq.removeQuerierConnection("querier-1", now.Add(40*time.Second))
	uq.removeQuerierConnection("querier-1", now.Add(41*time.Second))

	// We expect querier-1 has NOT been removed.
	assert.Contains(t, uq.queriers, "querier-1")
	assert.NoError(t, isConsistent(uq))

	// We expect the querier-1 users have not been shuffled to other queriers.
	for _, userID := range querier1Users {
		assert.Contains(t, getUsersByQuerier(uq, "querier-1"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-2"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-3"), userID)
	}

	// Try to forget disconnected queriers, but querier-1 forget delay hasn't passed yet.
	uq.forgetDisconnectedQueriers(now.Add(90 * time.Second))

	assert.Contains(t, uq.queriers, "querier-1")
	assert.NoError(t, isConsistent(uq))

	for _, userID := range querier1Users {
		assert.Contains(t, getUsersByQuerier(uq, "querier-1"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-2"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-3"), userID)
	}

	// Try to forget disconnected queriers. This time querier-1 forget delay has passed.
	uq.forgetDisconnectedQueriers(now.Add(105 * time.Second))

	assert.NotContains(t, uq.queriers, "querier-1")
	assert.NoError(t, isConsistent(uq))

	// We expect querier-1 users have been shuffled to other queriers.
	for _, userID := range querier1Users {
		assert.Contains(t, append(getUsersByQuerier(uq, "querier-2"), getUsersByQuerier(uq, "querier-3")...), userID)
	}
}

func TestQueues_ForgetDelay_ShouldCorrectlyHandleQuerierReconnectingBeforeForgetDelayIsPassed(t *testing.T) {
	const (
		forgetDelay        = time.Minute
		maxQueriersPerUser = 1
		numUsers           = 100
	)

	now := time.Now()
	uq := newUserQueues(0, forgetDelay)
	assert.NotNil(t, uq)
	assert.NoError(t, isConsistent(uq))

	// 3 queriers open 2 connections each.
	for i := 1; i <= 3; i++ {
		uq.addQuerierConnection(fmt.Sprintf("querier-%d", i))
		uq.addQuerierConnection(fmt.Sprintf("querier-%d", i))
	}

	// Add user queues.
	for i := 0; i < numUsers; i++ {
		userID := fmt.Sprintf("user-%d", i)
		getOrAdd(t, uq, userID, maxQueriersPerUser)
	}

	// We expect querier-1 to have some users.
	querier1Users := getUsersByQuerier(uq, "querier-1")
	require.NotEmpty(t, querier1Users)

	// Querier-1 abruptly terminates (no shutdown notification received).
	uq.removeQuerierConnection("querier-1", now.Add(40*time.Second))
	uq.removeQuerierConnection("querier-1", now.Add(41*time.Second))

	// We expect querier-1 has NOT been removed.
	assert.Contains(t, uq.queriers, "querier-1")
	assert.NoError(t, isConsistent(uq))

	// We expect the querier-1 users have not been shuffled to other queriers.
	for _, userID := range querier1Users {
		assert.Contains(t, getUsersByQuerier(uq, "querier-1"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-2"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-3"), userID)
	}

	// Try to forget disconnected queriers, but querier-1 forget delay hasn't passed yet.
	uq.forgetDisconnectedQueriers(now.Add(90 * time.Second))

	// Querier-1 reconnects.
	uq.addQuerierConnection("querier-1")
	uq.addQuerierConnection("querier-1")

	assert.Contains(t, uq.queriers, "querier-1")
	assert.NoError(t, isConsistent(uq))

	// We expect the querier-1 users have not been shuffled to other queriers.
	for _, userID := range querier1Users {
		assert.Contains(t, getUsersByQuerier(uq, "querier-1"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-2"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-3"), userID)
	}

	// Try to forget disconnected queriers far in the future, but there's no disconnected querier.
	uq.forgetDisconnectedQueriers(now.Add(200 * time.Second))

	assert.Contains(t, uq.queriers, "querier-1")
	assert.NoError(t, isConsistent(uq))

	for _, userID := range querier1Users {
		assert.Contains(t, getUsersByQuerier(uq, "querier-1"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-2"), userID)
		assert.NotContains(t, getUsersByQuerier(uq, "querier-3"), userID)
	}
}

func generateTenant(r *rand.Rand) string {
	return fmt.Sprint("tenant-", r.Int()%5)
}

func generateQuerier(r *rand.Rand) string {
	return fmt.Sprint("querier-", r.Int()%5)
}

func getOrAdd(t *testing.T, uq *queues, tenant string, maxQueriers int) chan Request {
	q := uq.getOrAddQueue(tenant, maxQueriers)
	assert.NotNil(t, q)
	assert.NoError(t, isConsistent(uq))
	assert.Equal(t, q, uq.getOrAddQueue(tenant, maxQueriers))
	return q
}

func confirmOrderForQuerier(t *testing.T, uq *queues, querier string, lastUserIndex int, qs ...chan Request) int {
	var n chan Request
	for _, q := range qs {
		n, _, lastUserIndex = uq.getNextQueueForQuerier(lastUserIndex, querier)
		assert.Equal(t, q, n)
		assert.NoError(t, isConsistent(uq))
	}
	return lastUserIndex
}

func isConsistent(uq *queues) error {
	if len(uq.sortedQueriers) != len(uq.queriers) {
		return fmt.Errorf("inconsistent number of sorted queriers and querier connections")
	}

	uc := 0
	for ix, u := range uq.users {
		q := uq.userQueues[u]
		if u != "" && q == nil {
			return fmt.Errorf("user %s doesn't have queue", u)
		}
		if u == "" && q != nil {
			return fmt.Errorf("user %s shouldn't have queue", u)
		}
		if u == "" {
			continue
		}

		uc++

		if q.index != ix {
			return fmt.Errorf("invalid user's index, expected=%d, got=%d", ix, q.index)
		}

		if q.maxQueriers == 0 && q.queriers != nil {
			return fmt.Errorf("user %s has queriers, but maxQueriers=0", u)
		}

		if q.maxQueriers > 0 && len(uq.sortedQueriers) <= q.maxQueriers && q.queriers != nil {
			return fmt.Errorf("user %s has queriers set despite not enough queriers available", u)
		}

		if q.maxQueriers > 0 && len(uq.sortedQueriers) > q.maxQueriers && len(q.queriers) != q.maxQueriers {
			return fmt.Errorf("user %s has incorrect number of queriers, expected=%d, got=%d", u, len(q.queriers), q.maxQueriers)
		}
	}

	if uc != len(uq.userQueues) {
		return fmt.Errorf("inconsistent number of users list and user queues")
	}

	return nil
}

// getUsersByQuerier returns the list of users handled by the provided querierID.
func getUsersByQuerier(queues *queues, querierID string) []string {
	var userIDs []string
	for userID, q := range queues.userQueues {
		if q.queriers == nil {
			// If it's nil then all queriers can handle this user.
			userIDs = append(userIDs, userID)
			continue
		}
		if _, ok := q.queriers[querierID]; ok {
			userIDs = append(userIDs, userID)
		}
	}
	return userIDs
}

func TestShuffleQueriers(t *testing.T) {
	allQueriers := []string{"a", "b", "c", "d", "e"}

	require.Nil(t, shuffleQueriersForUser(12345, 10, allQueriers, nil))
	require.Nil(t, shuffleQueriersForUser(12345, len(allQueriers), allQueriers, nil))

	r1 := shuffleQueriersForUser(12345, 3, allQueriers, nil)
	require.Equal(t, 3, len(r1))

	// Same input produces same output.
	r2 := shuffleQueriersForUser(12345, 3, allQueriers, nil)
	require.Equal(t, 3, len(r2))
	require.Equal(t, r1, r2)
}

func TestShuffleQueriersCorrectness(t *testing.T) {
	const queriersCount = 100

	var allSortedQueriers []string
	for i := 0; i < queriersCount; i++ {
		allSortedQueriers = append(allSortedQueriers, fmt.Sprintf("%d", i))
	}
	sort.Strings(allSortedQueriers)

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	const tests = 1000
	for i := 0; i < tests; i++ {
		toSelect := r.Intn(queriersCount)
		if toSelect == 0 {
			toSelect = 3
		}

		selected := shuffleQueriersForUser(r.Int63(), toSelect, allSortedQueriers, nil)

		require.Equal(t, toSelect, len(selected))

		sort.Strings(allSortedQueriers)
		prevQuerier := ""
		for _, q := range allSortedQueriers {
			require.True(t, prevQuerier < q, "non-unique querier")
			prevQuerier = q

			ix := sort.SearchStrings(allSortedQueriers, q)
			require.True(t, ix < len(allSortedQueriers) && allSortedQueriers[ix] == q, "selected querier is not between all queriers")
		}
	}
}
