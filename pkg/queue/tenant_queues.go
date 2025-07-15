// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/scheduler/queue/user_queues.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package queue

import (
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

type intPointerMap map[string]*int

func (tqs intPointerMap) Inc(key string) int {
	ptr, ok := tqs[key]
	if !ok {
		size := 1
		tqs[key] = &size
		return size
	}
	(*ptr)++
	return *ptr
}

func (tqs intPointerMap) Dec(key string) int {
	ptr, ok := tqs[key]
	if !ok {
		return 0
	}
	(*ptr)--
	if *ptr == 0 {
		delete(tqs, key)
	}
	return *ptr
}

// consumer holds information about a consumer registered in the queue.
type consumer struct {
	// Number of active connections.
	connections int

	// True if the consumer notified it's gracefully shutting down.
	shuttingDown bool

	// When the last connection has been unregistered.
	disconnectedAt time.Time
}

// This struct holds tenant queues for pending requests. It also keeps track of connected consumers,
// and mapping between tenants and consumers.
type tenantQueues struct {
	mapping *Mapping[*tenantQueue]

	maxUserQueueSize int
	perUserQueueLen  intPointerMap

	// How long to wait before removing a consumer which has got disconnected
	// but hasn't notified about a graceful shutdown.
	forgetDelay time.Duration

	// Tracks consumers registered to the queue.
	consumers map[string]*consumer

	// sortedConsumer list of consumer IDs, used when creating per-user shard.
	sortedConsumers []string

	limits Limits
}

type Queue interface {
	Chan() RequestChannel
	Dequeue() Request
	Name() string
	Len() int
}

type Mapable interface {
	*tenantQueue | *TreeQueue
	// https://github.com/golang/go/issues/48522#issuecomment-924348755
	Pos() QueueIndex
	SetPos(index QueueIndex)
}

type tenantQueue struct {
	*TreeQueue

	// If not nil, only these consumers can handle user requests. If nil, all consumers can.
	// We set this to nil if number of available consumers <= MaxConsumers.
	consumers map[string]struct{}

	// Seed for shuffle sharding of consumers. This seed is based on userID only and is therefore consistent
	// between different frontends.
	seed int64
}

func newTenantQueues(maxUserQueueSize int, forgetDelay time.Duration, limits Limits) *tenantQueues {
	mm := &Mapping[*tenantQueue]{}
	mm.Init(64)
	return &tenantQueues{
		mapping:          mm,
		maxUserQueueSize: maxUserQueueSize,
		perUserQueueLen:  make(intPointerMap),
		forgetDelay:      forgetDelay,
		consumers:        map[string]*consumer{},
		sortedConsumers:  nil,
		limits:           limits,
	}
}

func (q *tenantQueues) hasNoTenantQueues() bool {
	return q.mapping.Len() == 0
}

func (q *tenantQueues) deleteQueue(tenant string) {
	q.mapping.Remove(tenant)
}

// Returns existing or new queue for a tenant.
func (q *tenantQueues) getOrAddQueue(tenantID string, path []string) (Queue, error) {
	// Empty tenant is not allowed, as that would break our tenants list ("" is used for free spot).
	if tenantID == "" {
		return nil, fmt.Errorf("empty tenant is not allowed")
	}

	// extract tenantIDs to compute limits for multi-tenant queries
	tenantIDs, err := tenant.TenantIDsFromOrgID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("extract tenant ids: %w", err)
	}

	uq := q.mapping.GetByKey(tenantID)
	if uq == nil {
		uq = &tenantQueue{
			seed: util.ShuffleShardSeed(tenantID, ""),
		}
		uq.TreeQueue = newTreeQueue(q.maxUserQueueSize, tenantID)
		q.mapping.Put(tenantID, uq)
	}

	consumersToSelect := validation.SmallestPositiveNonZeroIntPerTenant(
		tenantIDs,
		func(tenantID string) int {
			return q.limits.MaxConsumers(tenantID, len(q.sortedConsumers))
		},
	)

	if len(uq.consumers) != consumersToSelect {
		uq.consumers = shuffleConsumersForTenants(uq.seed, consumersToSelect, q.sortedConsumers, nil)
	}

	if len(path) == 0 {
		return uq, nil
	}
	return uq.add(path), nil
}

// Finds next queue for the consumer. To support fair scheduling between users, client is expected
// to pass last user index returned by this function as argument. Is there was no previous
// last user index, use -1.
func (q *tenantQueues) getNextQueueForConsumer(lastUserIndex QueueIndex, consumerID string) (Queue, string, QueueIndex) {
	uid := lastUserIndex

	// at the RequestQueue level we don't have local queues, so start index is -1
	if uid == StartIndexWithLocalQueue {
		uid = StartIndex
	}

	// Ensure the consumer is not shutting down. If the consumer is shutting down, we shouldn't forward
	// any more queries to it.
	if info := q.consumers[consumerID]; info == nil || info.shuttingDown {
		return nil, "", uid
	}

	maxIters := len(q.mapping.keys) + 1
	for iters := 0; iters < maxIters; iters++ {
		tq, err := q.mapping.GetNext(uid)
		if err == ErrOutOfBounds {
			uid = StartIndex
			continue
		}
		if tq == nil {
			break
		}
		uid = tq.pos

		if tq.consumers != nil {
			if _, ok := tq.consumers[consumerID]; !ok {
				// This consumer is not handling the user.
				continue
			}
		}
		return tq, tq.name, uid
	}

	return nil, "", uid
}

func (q *tenantQueues) addConsumerToConnection(consumerID string) {
	info := q.consumers[consumerID]
	if info != nil {
		info.connections++

		// Reset in case the consumer re-connected while it was in the forget waiting period.
		info.shuttingDown = false
		info.disconnectedAt = time.Time{}

		return
	}

	// First connection from this consumer.
	q.consumers[consumerID] = &consumer{connections: 1}
	q.sortedConsumers = append(q.sortedConsumers, consumerID)
	sort.Strings(q.sortedConsumers)

	q.recomputeUserConsumers()
}

func (q *tenantQueues) removeConsumerConnection(consumerID string, now time.Time) {
	info := q.consumers[consumerID]
	if info == nil || info.connections <= 0 {
		panic("unexpected number of connections for consumer")
	}

	// Decrease the number of active connections.
	info.connections--
	if info.connections > 0 {
		return
	}

	// There no more active connections. If the forget delay is configured then
	// we can remove it only if consumer has announced a graceful shutdown.
	if info.shuttingDown || q.forgetDelay == 0 {
		q.removeConsumer(consumerID)
		return
	}

	// No graceful shutdown has been notified yet, so we should track the current time
	// so that we'll remove the consumer as soon as we receive the graceful shutdown
	// notification (if any) or once the threshold expires.
	info.disconnectedAt = now
}

func (q *tenantQueues) removeConsumer(consumerID string) {
	delete(q.consumers, consumerID)

	ix := sort.SearchStrings(q.sortedConsumers, consumerID)
	if ix >= len(q.sortedConsumers) || q.sortedConsumers[ix] != consumerID {
		panic("incorrect state of sorted consumers")
	}

	q.sortedConsumers = append(q.sortedConsumers[:ix], q.sortedConsumers[ix+1:]...)

	q.recomputeUserConsumers()
}

// notifyQuerierShutdown records that a consumer has sent notification about a graceful shutdown.
func (q *tenantQueues) notifyQuerierShutdown(consumerID string) {
	info := q.consumers[consumerID]
	if info == nil {
		// The consumer may have already been removed, so we just ignore it.
		return
	}

	// If there are no more connections, we should remove the consumer.
	if info.connections == 0 {
		q.removeConsumer(consumerID)
		return
	}

	// Otherwise we should annotate we received a graceful shutdown notification
	// and the consumer will be removed once all connections are unregistered.
	info.shuttingDown = true
}

// forgetDisconnectedConsumers removes all disconnected consumer that have gone since at least
// the forget delay. Returns the number of forgotten consumers.
func (q *tenantQueues) forgetDisconnectedConsumers(now time.Time) int {
	// Nothing to do if the forget delay is disabled.
	if q.forgetDelay == 0 {
		return 0
	}

	// Remove all consumers with no connections that have gone since at least the forget delay.
	threshold := now.Add(-q.forgetDelay)
	forgotten := 0

	for id := range q.consumers {
		if info := q.consumers[id]; info.connections == 0 && info.disconnectedAt.Before(threshold) {
			q.removeConsumer(id)
			forgotten++
		}
	}

	return forgotten
}

func (q *tenantQueues) recomputeUserConsumers() {
	scratchpad := make([]string, 0, len(q.sortedConsumers))

	for _, tenantID := range q.mapping.Keys() {
		if uq := q.mapping.GetByKey(tenantID); uq != nil {
			tenantIDs, err := tenant.TenantIDsFromOrgID(tenantID)
			if err != nil {
				// this is unlikely to happen since we do tenantID validation when creating the queue.
				level.Error(util_log.Logger).Log("msg", "failed to shuffle consumers because of errors in tenantID extraction", "tenant", tenantID, "error", err)
				continue
			}

			consumersToSelect := validation.SmallestPositiveNonZeroIntPerTenant(
				tenantIDs,
				func(tenantID string) int {
					return q.limits.MaxConsumers(tenantID, len(q.sortedConsumers))
				},
			)
			uq.consumers = shuffleConsumersForTenants(uq.seed, consumersToSelect, q.sortedConsumers, scratchpad)
		}
	}
}

// shuffleConsumersForTenants returns nil if consumersToSelect is 0 or there are not enough consumers to select from.
// In that case *all* consumers should be used.
// Scratchpad is used for shuffling, to avoid new allocations. If nil, new slice is allocated.
func shuffleConsumersForTenants(userSeed int64, consumersToSelect int, allSortedConsumers []string, scratchpad []string) map[string]struct{} {
	if consumersToSelect == 0 || len(allSortedConsumers) <= consumersToSelect {
		return nil
	}

	result := make(map[string]struct{}, consumersToSelect)
	rnd := rand.New(rand.NewSource(userSeed)) //#nosec G404 -- Load spreading does not require CSPRNG

	scratchpad = scratchpad[:0]
	scratchpad = append(scratchpad, allSortedConsumers...)

	last := len(scratchpad) - 1
	for i := 0; i < consumersToSelect; i++ {
		r := rnd.Intn(last + 1)
		result[scratchpad[r]] = struct{}{}
		// move selected item to the end, it won't be selected anymore.
		scratchpad[r], scratchpad[last] = scratchpad[last], scratchpad[r]
		last--
	}

	return result
}
