// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/scheduler/queue/user_queues.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package queue

import (
	"container/list"
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

type LinkedMap struct {
	l        *list.List // LinkedList<TenantQueue>
	elements map[string]*list.Element
}

func (m *LinkedMap) Init() *LinkedMap {
	m.l = list.New()
	m.elements = make(map[string]*list.Element)
	return m
}

func (m *LinkedMap) Len() int {
	return m.l.Len()
}

func (m *LinkedMap) Add(key string, value any) *list.Element {
	// prevent inserting empty string as key or nil value
	if key == "" || value == nil {
		return nil
	}
	e := m.l.PushBack(value)
	m.elements[key] = e
	return e
}

func (m *LinkedMap) Get(key string) any {
	if e, ok := m.elements[key]; ok {
		return e.Value
	}
	return nil
}

func (m *LinkedMap) Remove(key string) any {
	if e, ok := m.elements[key]; ok {
		return m.l.Remove(e)
	}
	return nil
}

func (m *LinkedMap) Next(key string) any {
	if e, ok := m.elements[key]; ok {
		return e.Next()
	}
	// key == ""
	return m.First()
}

func (m *LinkedMap) Keys() []string {
	keys := make([]string, 0, len(m.elements))
	for k := range m.elements {
		keys = append(keys, k)
	}
	return keys
}

func (m *LinkedMap) Values() []any {
	values := make([]interface{}, 0, len(m.elements))
	for k := range m.elements {
		values = append(values, m.elements[k].Value)
	}
	return values
}

func (m *LinkedMap) First() any {
	if first := m.l.Front(); first != nil {
		return first.Value
	}
	return nil
}

// This struct holds tenant queues for pending requests. It also keeps track of connected queriers,
// and mapping between tenants and queriers.
type tenantQueues struct {
	queues *LinkedMap

	maxUserQueueSize int

	// How long to wait before removing a querier which has got disconnected
	// but hasn't notified about a graceful shutdown.
	forgetDelay time.Duration

	// Tracks queriers registered to the queue.
	queriers map[string]*querier

	// Sorted list of querier names, used when creating per-user shard.
	sortedQueriers []string
}

type Queue interface {
	Chan() RequestChannel
}

type tenantQueue struct {
	ch RequestChannel

	// If not nil, only these queriers can handle user requests. If nil, all queriers can.
	// We set this to nil if number of available queriers <= maxQueriers.
	queriers    map[string]struct{}
	maxQueriers int

	// name of the queue
	name string

	// Seed for shuffle sharding of queriers. This seed is based on userID only and is therefore consistent
	// between different frontends.
	seed int64
}

func (q *tenantQueue) Chan() RequestChannel {
	return q.ch
}

func newTenantQueues(maxUserQueueSize int, forgetDelay time.Duration) *tenantQueues {
	queues := &LinkedMap{}
	return &tenantQueues{
		queues:           queues.Init(),
		maxUserQueueSize: maxUserQueueSize,
		forgetDelay:      forgetDelay,
		queriers:         map[string]*querier{},
		sortedQueriers:   nil,
	}
}

func (q *tenantQueues) len() int {
	return q.queues.Len()
}

func (q *tenantQueues) deleteQueue(tenant string) {
	q.queues.Remove(tenant)
}

// Returns existing or new queue for a tenant.
// MaxQueriers is used to compute which queriers should handle requests for this tenant.
// If maxQueriers is <= 0, all queriers can handle this tenant's requests.
// If maxQueriers has changed since the last call, queriers for this are recomputed.
func (q *tenantQueues) getOrAddQueue(tenant string, maxQueriers int) Queue {
	// Empty tenant is not allowed, as that would break our tenants list ("" is used for free spot).
	if tenant == "" {
		return nil
	}

	if maxQueriers < 0 {
		maxQueriers = 0
	}

	tq, _ := q.queues.Get(tenant).(*tenantQueue)
	if tq == nil {
		tq = &tenantQueue{
			ch:   make(RequestChannel, q.maxUserQueueSize),
			seed: util.ShuffleShardSeed(tenant, ""),
			name: tenant,
		}
		q.queues.Add(tenant, tq)
	}

	if tq.maxQueriers != maxQueriers {
		tq.maxQueriers = maxQueriers
		tq.queriers = shuffleQueriersForTenants(tq.seed, maxQueriers, q.sortedQueriers, nil)
	}

	return tq
}

// Finds next queue for the querier. To support fair scheduling between users, client is expected
// to pass last user index returned by this function as argument. Is there was no previous
// last user index, use -1.
func (q *tenantQueues) getNextQueueForQuerier(lastTenant string, querierID string) (Queue, string) {
	newTenant := lastTenant

	// Ensure the querier is not shutting down. If the querier is shutting down, we shouldn't forward
	// any more queries to it.
	if info := q.queriers[querierID]; info == nil || info.shuttingDown {
		return nil, newTenant
	}

	for tq, _ := q.queues.Next(newTenant).(*tenantQueue); tq != nil; {
		newTenant = tq.name
		if tq.queriers != nil {
			if _, ok := q.queriers[querierID]; !ok {
				// This querier is not handling the user.
				continue
			}
		}
		return tq, newTenant
	}
	return nil, ""
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

	for _, queue := range q.queues.Values() {
		uq := queue.(*tenantQueue)
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
