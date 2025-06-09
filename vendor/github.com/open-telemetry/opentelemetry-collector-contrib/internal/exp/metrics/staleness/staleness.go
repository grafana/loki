// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package staleness // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/staleness"

import (
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/streams"
)

// We override how Now() is returned, so we can have deterministic tests
var NowFunc = time.Now

var (
	_ streams.Map[any] = (*Staleness[any])(nil)
	_ streams.Evictor  = (*Staleness[any])(nil)
)

// Staleness a a wrapper over a map that adds an additional "staleness" value to each entry. Users can
// call ExpireOldEntries() to automatically remove all entries from the map whole staleness value is
// older than the `max`
//
// NOTE: Staleness methods are *not* thread-safe. If the user needs to use Staleness in a multi-threaded
// environment, then it is the user's responsibility to properly serialize calls to Staleness methods
type Staleness[T any] struct {
	Max time.Duration

	items streams.Map[T]
	pq    PriorityQueue
}

func NewStaleness[T any](max time.Duration, items streams.Map[T]) *Staleness[T] {
	return &Staleness[T]{
		Max: max,

		items: items,
		pq:    NewPriorityQueue(),
	}
}

// Load the value at key. If it does not exist, the boolean will be false and the value returned will be the zero value
func (s *Staleness[T]) Load(id identity.Stream) (T, bool) {
	return s.items.Load(id)
}

// Store the given key value pair in the map, and update the pair's staleness value to "now"
func (s *Staleness[T]) Store(id identity.Stream, v T) error {
	s.pq.Update(id, NowFunc())
	return s.items.Store(id, v)
}

func (s *Staleness[T]) Delete(id identity.Stream) {
	s.items.Delete(id)
}

// Items returns an iterator function that in future go version can be used with range
// See: https://go.dev/wiki/RangefuncExperiment
func (s *Staleness[T]) Items() func(yield func(identity.Stream, T) bool) bool {
	return s.items.Items()
}

// ExpireOldEntries will remove all entries whose staleness value is older than `now() - max`
// For example, if an entry has a staleness value of two hours ago, and max == 1 hour, then the entry would
// be removed. But if an entry had a stalness value of 30 minutes, then it *wouldn't* be removed.
func (s *Staleness[T]) ExpireOldEntries() {
	now := NowFunc()
	for {
		if s.Len() == 0 {
			return
		}
		_, ts := s.pq.Peek()
		if now.Sub(ts) < s.Max {
			break
		}
		id, _ := s.pq.Pop()
		s.items.Delete(id)
	}
}

func (s *Staleness[T]) Len() int {
	return s.items.Len()
}

func (s *Staleness[T]) Next() time.Time {
	_, ts := s.pq.Peek()
	return ts
}

func (s *Staleness[T]) Evict() (identity.Stream, bool) {
	_, ts := s.pq.Peek()
	if NowFunc().Sub(ts) < s.Max {
		return identity.Stream{}, false
	}

	id, _ := s.pq.Pop()
	s.items.Delete(id)
	return id, true
}

func (s *Staleness[T]) Clear() {
	s.items.Clear()
}

type Tracker struct {
	pq PriorityQueue
}

func NewTracker() Tracker {
	return Tracker{pq: NewPriorityQueue()}
}

func (stale Tracker) Refresh(ts time.Time, ids ...identity.Stream) {
	for _, id := range ids {
		stale.pq.Update(id, ts)
	}
}

func (stale Tracker) Collect(max time.Duration) []identity.Stream {
	now := NowFunc()

	var ids []identity.Stream
	for stale.pq.Len() > 0 {
		_, ts := stale.pq.Peek()
		if now.Sub(ts) < max {
			break
		}
		id, _ := stale.pq.Pop()
		ids = append(ids, id)
	}

	return ids
}
