// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/scheduler/queue/user_queues.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package queue

import (
	"math/rand"
	"sort"
	"time"

	"github.com/grafana/loki/pkg/util"
)

// querier holds information about a querier registered in the queue.
type querier struct {
	// Number of active connections.
	connections int

	// True if the querier notified it's gracefully shutting down.
	shuttingDown bool

	// When the last connection has been unregistered.
	disconnectedAt time.Time
}

// This struct holds tenant queues for pending requests. It also keeps track of connected queriers,
// and mapping between tenants and queriers.
type tenantQueues struct {
	queues map[string]*tenantQueue

	// List of all tenants with queues, used for iteration when searching for next queue to handle.
	// Tenants removed from the middle are replaced with "". To avoid skipping tenants during iteration, we only shrink
	// this list when there are ""'s at the end of it.
	tenants []string

	maxUserQueueSize int

	// How long to wait before removing a querier which has got disconnected
	// but hasn't notified about a graceful shutdown.
	forgetDelay time.Duration

	// Tracks queriers registered to the queue.
	queriers map[string]*querier

	// Sorted list of querier names, used when creating per-user shard.
	sortedQueriers []string
}

type tenantQueue struct {
	ch RequestChannel

	// If not nil, only these queriers can handle user requests. If nil, all queriers can.
	// We set this to nil if number of available queriers <= maxQueriers.
	queriers    map[string]struct{}
	maxQueriers int

	// Seed for shuffle sharding of queriers. This seed is based on userID only and is therefore consistent
	// between different frontends.
	seed int64

	// Points back to 'users' field in queues. Enables quick cleanup.
	index int
}

func newTenantQueues(maxUserQueueSize int, forgetDelay time.Duration) *tenantQueues {
	return &tenantQueues{
		queues:           map[string]*tenantQueue{},
		tenants:          nil,
		maxUserQueueSize: maxUserQueueSize,
		forgetDelay:      forgetDelay,
		queriers:         map[string]*querier{},
		sortedQueriers:   nil,
	}
}

func (q *tenantQueues) len() int {
	return len(q.queues)
}

func (q *tenantQueues) deleteQueue(tenant string) {
	uq := q.queues[tenant]
	if uq == nil {
		return
	}

	delete(q.queues, tenant)
	q.tenants[uq.index] = ""

	// Shrink users list size if possible. This is safe, and no users will be skipped during iteration.
	for ix := len(q.tenants) - 1; ix >= 0 && q.tenants[ix] == ""; ix-- {
		q.tenants = q.tenants[:ix]
	}
}

// Returns existing or new queue for a tenant.
// MaxQueriers is used to compute which queriers should handle requests for this tenant.
// If maxQueriers is <= 0, all queriers can handle this tenant's requests.
// If maxQueriers has changed since the last call, queriers for this are recomputed.
func (q *tenantQueues) getOrAddQueue(tenant string, maxQueriers int) RequestChannel {
	// Empty tenant is not allowed, as that would break our tenants list ("" is used for free spot).
	if tenant == "" {
		return nil
	}

	if maxQueriers < 0 {
		maxQueriers = 0
	}

	uq := q.queues[tenant]

	if uq == nil {
		uq = &tenantQueue{
			ch:    make(RequestChannel, q.maxUserQueueSize),
			seed:  util.ShuffleShardSeed(tenant, ""),
			index: -1,
		}
		q.queues[tenant] = uq

		// Add user to the list of users... find first free spot, and put it there.
		for ix, u := range q.tenants {
			if u == "" {
				uq.index = ix
				q.tenants[ix] = tenant
				break
			}
		}

		// ... or add to the end.
		if uq.index < 0 {
			uq.index = len(q.tenants)
			q.tenants = append(q.tenants, tenant)
		}
	}

	if uq.maxQueriers != maxQueriers {
		uq.maxQueriers = maxQueriers
		uq.queriers = shuffleQueriersForTenants(uq.seed, maxQueriers, q.sortedQueriers, nil)
	}

	return uq.ch
}

// Finds next queue for the querier. To support fair scheduling between users, client is expected
// to pass last user index returned by this function as argument. Is there was no previous
// last user index, use -1.
func (q *tenantQueues) getNextQueueForQuerier(lastUserIndex TenantIndex, querierID string) (RequestChannel, string, TenantIndex) {
	uid := lastUserIndex

	// Ensure the querier is not shutting down. If the querier is shutting down, we shouldn't forward
	// any more queries to it.
	if info := q.queriers[querierID]; info == nil || info.shuttingDown {
		return nil, "", uid
	}

	for iters := 0; iters < len(q.tenants); iters++ {
		uid = uid + 1

		// Don't use "mod len(q.users)", as that could skip users at the beginning of the list
		// for example when q.users has shrunk since last call.
		if int(uid) >= len(q.tenants) {
			uid = 0
		}

		u := q.tenants[uid]
		if u == "" {
			continue
		}

		q := q.queues[u]

		if q.queriers != nil {
			if _, ok := q.queriers[querierID]; !ok {
				// This querier is not handling the user.
				continue
			}
		}

		return q.ch, u, uid
	}
	return nil, "", uid
}

func (q *tenantQueues) addQuerierConnection(querierID string) {
	info := q.queriers[querierID]
	if info != nil {
		info.connections++

		// Reset in case the querier re-connected while it was in the forget waiting period.
		info.shuttingDown = false
		info.disconnectedAt = time.Time{}

		return
	}

	// First connection from this querier.
	q.queriers[querierID] = &querier{connections: 1}
	q.sortedQueriers = append(q.sortedQueriers, querierID)
	sort.Strings(q.sortedQueriers)

	q.recomputeUserQueriers()
}

func (q *tenantQueues) removeQuerierConnection(querierID string, now time.Time) {
	info := q.queriers[querierID]
	if info == nil || info.connections <= 0 {
		panic("unexpected number of connections for querier")
	}

	// Decrease the number of active connections.
	info.connections--
	if info.connections > 0 {
		return
	}

	// There no more active connections. If the forget delay is configured then
	// we can remove it only if querier has announced a graceful shutdown.
	if info.shuttingDown || q.forgetDelay == 0 {
		q.removeQuerier(querierID)
		return
	}

	// No graceful shutdown has been notified yet, so we should track the current time
	// so that we'll remove the querier as soon as we receive the graceful shutdown
	// notification (if any) or once the threshold expires.
	info.disconnectedAt = now
}

func (q *tenantQueues) removeQuerier(querierID string) {
	delete(q.queriers, querierID)

	ix := sort.SearchStrings(q.sortedQueriers, querierID)
	if ix >= len(q.sortedQueriers) || q.sortedQueriers[ix] != querierID {
		panic("incorrect state of sorted queriers")
	}

	q.sortedQueriers = append(q.sortedQueriers[:ix], q.sortedQueriers[ix+1:]...)

	q.recomputeUserQueriers()
}

// notifyQuerierShutdown records that a querier has sent notification about a graceful shutdown.
func (q *tenantQueues) notifyQuerierShutdown(querierID string) {
	info := q.queriers[querierID]
	if info == nil {
		// The querier may have already been removed, so we just ignore it.
		return
	}

	// If there are no more connections, we should remove the querier.
	if info.connections == 0 {
		q.removeQuerier(querierID)
		return
	}

	// Otherwise we should annotate we received a graceful shutdown notification
	// and the querier will be removed once all connections are unregistered.
	info.shuttingDown = true
}

// forgetDisconnectedQueriers removes all disconnected queriers that have gone since at least
// the forget delay. Returns the number of forgotten queriers.
func (q *tenantQueues) forgetDisconnectedQueriers(now time.Time) int {
	// Nothing to do if the forget delay is disabled.
	if q.forgetDelay == 0 {
		return 0
	}

	// Remove all queriers with no connections that have gone since at least the forget delay.
	threshold := now.Add(-q.forgetDelay)
	forgotten := 0

	for querierID := range q.queriers {
		if info := q.queriers[querierID]; info.connections == 0 && info.disconnectedAt.Before(threshold) {
			q.removeQuerier(querierID)
			forgotten++
		}
	}

	return forgotten
}

func (q *tenantQueues) recomputeUserQueriers() {
	scratchpad := make([]string, 0, len(q.sortedQueriers))

	for _, uq := range q.queues {
		uq.queriers = shuffleQueriersForTenants(uq.seed, uq.maxQueriers, q.sortedQueriers, scratchpad)
	}
}

// shuffleQueriersForTenants returns nil if queriersToSelect is 0 or there are not enough queriers to select from.
// In that case *all* queriers should be used.
// Scratchpad is used for shuffling, to avoid new allocations. If nil, new slice is allocated.
func shuffleQueriersForTenants(userSeed int64, queriersToSelect int, allSortedQueriers []string, scratchpad []string) map[string]struct{} {
	if queriersToSelect == 0 || len(allSortedQueriers) <= queriersToSelect {
		return nil
	}

	result := make(map[string]struct{}, queriersToSelect)
	rnd := rand.New(rand.NewSource(userSeed))

	scratchpad = scratchpad[:0]
	scratchpad = append(scratchpad, allSortedQueriers...)

	last := len(scratchpad) - 1
	for i := 0; i < queriersToSelect; i++ {
		r := rnd.Intn(last + 1)
		result[scratchpad[r]] = struct{}{}
		// move selected item to the end, it won't be selected anymore.
		scratchpad[r], scratchpad[last] = scratchpad[last], scratchpad[r]
		last--
	}

	return result
}
