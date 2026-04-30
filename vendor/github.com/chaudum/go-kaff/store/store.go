// Package store holds the in-memory data model for go-kaff.
package store

import (
	"fmt"
	"sync"
)

// Store is the root mutable object.  All access to the topics and groups maps
// is protected by a RWMutex; individual Partitions and ConsumerGroups carry
// their own locks.
type Store struct {
	mu     sync.RWMutex
	topics map[string]*Topic
	groups map[string]*ConsumerGroup
}

// New returns an empty Store ready for use.
func New() *Store {
	return &Store{
		topics: make(map[string]*Topic),
		groups: make(map[string]*ConsumerGroup),
	}
}

// CreateTopic adds a new topic with the given number of partitions.
// maxBytesPerPartition is the per-partition soft retention cap (0 = unlimited).
// Returns an error if the topic already exists.
func (s *Store) CreateTopic(name string, numPartitions int32, maxBytesPerPartition int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.topics[name]; exists {
		return fmt.Errorf("store: topic %q already exists", name)
	}
	s.topics[name] = newTopic(name, numPartitions, maxBytesPerPartition)
	return nil
}

// GetOrCreateTopic returns an existing topic or atomically creates a new one.
// The second return value is true when the topic was just created.
func (s *Store) GetOrCreateTopic(name string, numPartitions int32, maxBytesPerPartition int64) (*Topic, bool) {
	// Fast path under read lock.
	s.mu.RLock()
	t, ok := s.topics[name]
	s.mu.RUnlock()
	if ok {
		return t, false
	}

	// Slow path: create under write lock, but re-check for the race window.
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok = s.topics[name]; ok {
		return t, false
	}
	t = newTopic(name, numPartitions, maxBytesPerPartition)
	s.topics[name] = t
	return t, true
}

// DeleteTopic removes a topic from the store.  It wakes any goroutines blocked
// on long-poll Fetch so they return UNKNOWN_TOPIC_OR_PARTITION.
// Returns an error if the topic does not exist.
func (s *Store) DeleteTopic(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.topics[name]
	if !ok {
		return fmt.Errorf("store: topic %q does not exist", name)
	}
	t.Close() // wake any blocked Fetch goroutines
	delete(s.topics, name)
	return nil
}

// GetTopic returns the named Topic or (nil, false) if it does not exist.
func (s *Store) GetTopic(name string) (*Topic, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.topics[name]
	return t, ok
}

// ── Consumer group accessors ─────────────────────────────────────────────────

// GetOrCreateGroup returns an existing ConsumerGroup or atomically creates one.
func (s *Store) GetOrCreateGroup(id string) *ConsumerGroup {
	// Fast path.
	s.mu.RLock()
	g, ok := s.groups[id]
	s.mu.RUnlock()
	if ok {
		return g
	}
	// Slow path.
	s.mu.Lock()
	defer s.mu.Unlock()
	if g, ok = s.groups[id]; ok {
		return g
	}
	g = newConsumerGroup(id)
	s.groups[id] = g
	return g
}

// GetGroup returns the named ConsumerGroup or (nil, false) if it does not exist.
func (s *Store) GetGroup(id string) (*ConsumerGroup, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.groups[id]
	return g, ok
}

// ── Topic accessors ───────────────────────────────────────────────────────────

// ListTopics returns all topics as a snapshot slice.
// The slice may be modified freely; the Topics themselves are live objects.
func (s *Store) ListTopics() []*Topic {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Topic, 0, len(s.topics))
	for _, t := range s.topics {
		out = append(out, t)
	}
	return out
}

// ListGroups returns a snapshot of all known consumer groups.
func (s *Store) ListGroups() []*ConsumerGroup {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ConsumerGroup, 0, len(s.groups))
	for _, g := range s.groups {
		out = append(out, g)
	}
	return out
}
