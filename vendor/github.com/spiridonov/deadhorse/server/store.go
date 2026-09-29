package server

import (
	"sync"
	"time"
)

// DefaultStripes and DefaultGCInterval are what newStore falls back to when
// given numStripes<=0 or gcInterval<=0 (see NewInMemoryThrottler, which
// exposes that same zero-means-default convention for direct embedders).
// They're exported so cmd/deadhorse can use them as its own -stripes and
// -gc-interval flag defaults, rather than duplicating these numbers as a
// second, driftable copy -- see cmd/deadhorse/main.go.
const (
	DefaultStripes    = 256
	DefaultGCInterval = 60 * time.Second
)

// bucketState is the entire per-key state: one timestamp. mu guards it
// against two concurrent requests for the same key racing on the same
// stripe (the stripe's own lock only protects the map, not a bucket already
// handed out of it).
type bucketState struct {
	mu  sync.Mutex
	tat int64
}

// stripe holds one slice of the keyspace as two map generations. Rotating
// hot into cold and starting a fresh hot map is how idle keys get dropped --
// see (*store).gcLoop.
type stripe struct {
	mu   sync.Mutex
	hot  map[string]*bucketState
	cold map[string]*bucketState
}

func newStripe() *stripe {
	return &stripe{hot: make(map[string]*bucketState)}
}

// getOrCreate returns key's bucket, promoting it into the hot generation
// first if it was only found in cold. Being in hot after the next rotation is
// exactly what "touched within the last GC interval" means -- there is no
// separate last-access timestamp to maintain.
func (s *stripe) getOrCreate(key string) *bucketState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if b, ok := s.hot[key]; ok {
		return b
	}
	if b, ok := s.cold[key]; ok {
		s.hot[key] = b
		delete(s.cold, key)
		return b
	}
	b := &bucketState{}
	s.hot[key] = b
	return b
}

func (s *stripe) rotate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cold = s.hot
	s.hot = make(map[string]*bucketState)
}

func (s *stripe) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hot) + len(s.cold)
}

// store is the striped, self-expiring key/bucket map behind InMemoryThrottler.
// Striping here is purely an in-process concurrency detail -- unrelated to,
// and much finer-grained than, sharding a fleet of DeadHorse processes by key
// (which a client does on its own, across separate server instances; a
// single store here only ever serves one such process).
type store struct {
	stripes   []*stripe
	done      chan struct{}
	closeOnce sync.Once
}

func newStore(numStripes int, gcInterval time.Duration) *store {
	if numStripes <= 0 {
		numStripes = DefaultStripes
	}
	if gcInterval <= 0 {
		gcInterval = DefaultGCInterval
	}

	st := &store{
		stripes: make([]*stripe, numStripes),
		done:    make(chan struct{}),
	}
	for i := range st.stripes {
		st.stripes[i] = newStripe()
	}

	go st.gcLoop(gcInterval)
	return st
}

func (st *store) gcLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for _, s := range st.stripes {
				s.rotate()
			}
		case <-st.done:
			return
		}
	}
}

// close stops the GC loop. It's safe to call more than once -- a double
// Close is an easy caller mistake (e.g. a deferred Close alongside an
// explicit early one), and unlike a bare close(st.done) it must not panic
// on the second call.
func (st *store) close() {
	st.closeOnce.Do(func() { close(st.done) })
}

func (st *store) stripeFor(key string) *stripe {
	return st.stripes[fnv1a(key)%uint64(len(st.stripes))]
}

func (st *store) keyCountEstimate() int {
	total := 0
	for _, s := range st.stripes {
		total += s.size()
	}
	return total
}

func fnv1a(s string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211

	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}
