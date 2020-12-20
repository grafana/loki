package frontend

import (
	"math/rand"
	"sort"

	"github.com/cortexproject/cortex/pkg/util"
)

// This struct holds user queues for pending requests. It also keeps track of connected queriers,
// and mapping between users and queriers.
type queues struct {
	userQueues map[string]*userQueue

	// List of all users with queues, used for iteration when searching for next queue to handle.
	// Users removed from the middle are replaced with "". To avoid skipping users during iteration, we only shrink
	// this list when there are ""'s at the end of it.
	users []string

	maxUserQueueSize int

	// Number of connections per querier.
	querierConnections map[string]int
	// Sorted list of querier names, used when creating per-user shard.
	sortedQueriers []string
}

type userQueue struct {
	ch chan *request

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

func newUserQueues(maxUserQueueSize int) *queues {
	return &queues{
		userQueues:         map[string]*userQueue{},
		users:              nil,
		maxUserQueueSize:   maxUserQueueSize,
		querierConnections: map[string]int{},
		sortedQueriers:     nil,
	}
}

func (q *queues) len() int {
	return len(q.userQueues)
}

func (q *queues) deleteQueue(userID string) {
	uq := q.userQueues[userID]
	if uq == nil {
		return
	}

	delete(q.userQueues, userID)
	q.users[uq.index] = ""

	// Shrink users list size if possible. This is safe, and no users will be skipped during iteration.
	for ix := len(q.users) - 1; ix >= 0 && q.users[ix] == ""; ix-- {
		q.users = q.users[:ix]
	}
}

// Returns existing or new queue for user.
// MaxQueriers is used to compute which queriers should handle requests for this user.
// If maxQueriers is <= 0, all queriers can handle this user's requests.
// If maxQueriers has changed since the last call, queriers for this are recomputed.
func (q *queues) getOrAddQueue(userID string, maxQueriers int) chan *request {
	// Empty user is not allowed, as that would break our users list ("" is used for free spot).
	if userID == "" {
		return nil
	}

	if maxQueriers < 0 {
		maxQueriers = 0
	}

	uq := q.userQueues[userID]

	if uq == nil {
		uq = &userQueue{
			ch:    make(chan *request, q.maxUserQueueSize),
			seed:  util.ShuffleShardSeed(userID, ""),
			index: -1,
		}
		q.userQueues[userID] = uq

		// Add user to the list of users... find first free spot, and put it there.
		for ix, u := range q.users {
			if u == "" {
				uq.index = ix
				q.users[ix] = userID
				break
			}
		}

		// ... or add to the end.
		if uq.index < 0 {
			uq.index = len(q.users)
			q.users = append(q.users, userID)
		}
	}

	if uq.maxQueriers != maxQueriers {
		uq.maxQueriers = maxQueriers
		uq.queriers = shuffleQueriersForUser(uq.seed, maxQueriers, q.sortedQueriers, nil)
	}

	return uq.ch
}

// Finds next queue for the querier. To support fair scheduling between users, client is expected
// to pass last user index returned by this function as argument. Is there was no previous
// last user index, use -1.
func (q *queues) getNextQueueForQuerier(lastUserIndex int, querier string) (chan *request, string, int) {
	uid := lastUserIndex

	for iters := 0; iters < len(q.users); iters++ {
		uid = uid + 1

		// Don't use "mod len(q.users)", as that could skip users at the beginning of the list
		// for example when q.users has shrunk since last call.
		if uid >= len(q.users) {
			uid = 0
		}

		u := q.users[uid]
		if u == "" {
			continue
		}

		q := q.userQueues[u]

		if q.queriers != nil {
			if _, ok := q.queriers[querier]; !ok {
				// This querier is not handling the user.
				continue
			}
		}

		return q.ch, u, uid
	}
	return nil, "", uid
}

func (q *queues) addQuerierConnection(querier string) {
	conns := q.querierConnections[querier]

	q.querierConnections[querier] = conns + 1

	// First connection from this querier.
	if conns == 0 {
		q.sortedQueriers = append(q.sortedQueriers, querier)
		sort.Strings(q.sortedQueriers)

		q.recomputeUserQueriers()
	}
}

func (q *queues) removeQuerierConnection(querier string) {
	conns := q.querierConnections[querier]
	if conns <= 0 {
		panic("unexpected number of connections for querier")
	}

	conns--
	if conns > 0 {
		q.querierConnections[querier] = conns
	} else {
		delete(q.querierConnections, querier)

		ix := sort.SearchStrings(q.sortedQueriers, querier)
		if ix >= len(q.sortedQueriers) || q.sortedQueriers[ix] != querier {
			panic("incorrect state of sorted queriers")
		}

		q.sortedQueriers = append(q.sortedQueriers[:ix], q.sortedQueriers[ix+1:]...)

		q.recomputeUserQueriers()
	}
}

func (q *queues) recomputeUserQueriers() {
	scratchpad := make([]string, 0, len(q.sortedQueriers))

	for _, uq := range q.userQueues {
		uq.queriers = shuffleQueriersForUser(uq.seed, uq.maxQueriers, q.sortedQueriers, scratchpad)
	}
}

// Scratchpad is used for shuffling, to avoid new allocations. If nil, new slice is allocated.
// shuffleQueriersForUser returns nil if queriersToSelect is 0 or there are not enough queriers to select from.
// In that case *all* queriers should be used.
func shuffleQueriersForUser(userSeed int64, queriersToSelect int, allSortedQueriers []string, scratchpad []string) map[string]struct{} {
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
