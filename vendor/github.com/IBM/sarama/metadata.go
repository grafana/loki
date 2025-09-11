package sarama

import (
	"sync"
)

type metadataRefresh func(topics []string) error

// currentRefresh makes sure sarama does not issue metadata requests
// in parallel. If we need to refresh the metadata for a list of topics,
// this struct will check if a refresh is already ongoing, and if so, it will
// accumulate the list of topics to refresh in the next refresh.
// When the current refresh is over, it will queue a new metadata refresh call
// with the accumulated list of topics.
type currentRefresh struct {
	// This is the function that gets called when to refresh the metadata.
	// It is called with the list of all topics that need to be refreshed
	// or with nil if all topics need to be refreshed.
	refresh func(topics []string) error

	mu        sync.Mutex
	ongoing   bool
	topicsMap map[string]struct{}
	topics    []string
	allTopics bool
	chans     []chan error
}

// addTopicsFrom adds topics from the next refresh to the current refresh.
// You need to hold the lock to call this method.
func (r *currentRefresh) addTopicsFrom(next *nextRefresh) {
	if next.allTopics {
		r.allTopics = true
		return
	}
	if len(next.topics) > 0 {
		r.addTopics(next.topics)
	}
}

// nextRefresh holds the list of topics we will need
// to refresh in the next refresh.
// When a refresh is ongoing, calls to RefreshMetadata() are
// accumulated in this struct, so that we can immediately issue another
// refresh when the current refresh is over.
type nextRefresh struct {
	mu        sync.Mutex
	topics    []string
	allTopics bool
}

// addTopics adds topics to the refresh.
// You need to hold the lock to call this method.
func (r *currentRefresh) addTopics(topics []string) {
	if len(topics) == 0 {
		r.allTopics = true
		return
	}
	for _, topic := range topics {
		if _, ok := r.topicsMap[topic]; ok {
			continue
		}
		r.topicsMap[topic] = struct{}{}
		r.topics = append(r.topics, topic)
	}
}

func (r *nextRefresh) addTopics(topics []string) {
	if len(topics) == 0 {
		r.allTopics = true
		// All topics are requested, so we can clear the topics
		// that were previously accumulated.
		r.topics = r.topics[:0]
		return
	}
	r.topics = append(r.topics, topics...)
}

func (r *nextRefresh) clear() {
	r.topics = r.topics[:0]
	r.allTopics = false
}

func (r *currentRefresh) hasTopics(topics []string) bool {
	if len(topics) == 0 {
		// This means that the caller wants to know if the refresh is for all topics.
		// In this case, we return true if the refresh is for all topics, or false if it is not.
		return r.allTopics
	}
	if r.allTopics {
		return true
	}
	for _, topic := range topics {
		if _, ok := r.topicsMap[topic]; !ok {
			return false
		}
	}
	return true
}

// start starts a new refresh.
// The refresh is started in a new goroutine, and this function
// returns a channel on which the caller can wait for the refresh
// to complete.
// You need to hold the lock to call this method.
func (r *currentRefresh) start() chan error {
	r.ongoing = true
	ch := r.wait()
	topics := r.topics
	if r.allTopics {
		topics = nil
	}
	go func() {
		err := r.refresh(topics)
		r.mu.Lock()
		defer r.mu.Unlock()

		r.ongoing = false
		for _, ch := range r.chans {
			ch <- err
			close(ch)
		}
		r.clear()
	}()
	return ch
}

// clear clears the refresh state.
// You need to hold the lock to call this method.
func (r *currentRefresh) clear() {
	r.topics = r.topics[:0]
	for key := range r.topicsMap {
		delete(r.topicsMap, key)
	}
	r.allTopics = false
	r.chans = r.chans[:0]
}

// wait returns the channel on which you can wait for the refresh
// to complete.
// You need to hold the lock to call this method.
func (r *currentRefresh) wait() chan error {
	if !r.ongoing {
		panic("waiting for a refresh that is not ongoing")
	}
	ch := make(chan error, 1)
	r.chans = append(r.chans, ch)
	return ch
}

// singleFlightMetadataRefresher helps managing metadata refreshes.
// It makes sure a sarama client never issues more than one metadata refresh
// in parallel.
type singleFlightMetadataRefresher struct {
	current *currentRefresh
	next    *nextRefresh
}

func newSingleFlightRefresher(f func(topics []string) error) metadataRefresh {
	return newMetadataRefresh(f).Refresh
}

func newMetadataRefresh(f func(topics []string) error) *singleFlightMetadataRefresher {
	return &singleFlightMetadataRefresher{
		current: &currentRefresh{
			topicsMap: make(map[string]struct{}),
			refresh:   f,
		},
		next: &nextRefresh{},
	}
}

// Refresh is the function that clients call when they want to refresh
// the metadata. This function blocks until a refresh is issued, and its
// result is received, for the list of topics the caller provided.
// If a refresh was already ongoing for this list of topics, the function
// waits on that refresh to complete, and returns its result.
// If a refresh was already ongoing for a different list of topics, the function
// accumulates the list of topics to refresh in the next refresh, and queues that refresh.
// If no refresh is ongoing, it will start a new refresh, and return its result.
func (m *singleFlightMetadataRefresher) Refresh(topics []string) error {
	for {
		ch, queued := m.refreshOrQueue(topics)
		if !queued {
			return <-ch
		}
		<-ch
	}
}

// refreshOrQueue returns a channel the refresh needs to wait on, and a boolean
// that indicates whether waiting on the channel will return the result of that refresh
// or whether the refresh was "queued" and the caller needs to wait for the channel to
// return, and then call refreshOrQueue again.
// When calling refreshOrQueue, three things can happen:
//  1. either no refresh is ongoing.
//     In this case, a new refresh is started, and the channel that's returned will
//     contain the result of that refresh, so it returns "false" as the second return value.
//  2. a refresh is ongoing, and it contains the topics we need.
//     In this case, the channel that's returned will contain the result of that refresh,
//     so it returns "false" as the second return value.
//     In this case, the channel that's returned will contain the result of that refresh,
//     so it returns "false" as the second return value.
//  3. a refresh is already ongoing, but doesn't contain the topics we need. In this case,
//     the caller needs to wait for the refresh to finish, and then call refreshOrQueue again.
//     The channel that's returned is for the current refresh (not the one the caller is
//     interested in), so it returns "true" as the second return value. The caller needs to
//     wait on the channel, disregard the value, and call refreshOrQueue again.
func (m *singleFlightMetadataRefresher) refreshOrQueue(topics []string) (chan error, bool) {
	m.current.mu.Lock()
	defer m.current.mu.Unlock()
	if !m.current.ongoing {
		// If no refresh is ongoing, we can start a new one, in which
		// we add the topics that have been accumulated in the next refresh
		// and the topics that have been provided by the caller.
		m.next.mu.Lock()
		m.current.addTopicsFrom(m.next)
		m.next.clear()
		m.next.mu.Unlock()
		m.current.addTopics(topics)
		ch := m.current.start()
		return ch, false
	}
	if m.current.hasTopics(topics) {
		// A refresh is ongoing, and we were lucky: it is refreshing the topics we need already:
		// we just have to wait for it to finish and return its results.
		ch := m.current.wait()
		return ch, false
	}
	// There is a refresh ongoing, but it is not refreshing the topics we need.
	// We need to wait for it to finish, and then start a new refresh.
	ch := m.current.wait()
	m.next.mu.Lock()
	m.next.addTopics(topics)
	m.next.mu.Unlock()
	// This is where we wait for that refresh to finish, and the loop will take care
	// of starting the new one.
	return ch, true
}
