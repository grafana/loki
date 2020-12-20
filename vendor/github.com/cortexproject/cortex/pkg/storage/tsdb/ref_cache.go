package tsdb

import (
	"sync"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"go.uber.org/atomic"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
)

const (
	// DefaultRefCacheTTL is the default RefCache purge TTL. We use a reasonable
	// value that should cover most use cases. The cache would be ineffective if
	// the scrape interval of a series is greater than this TTL.
	DefaultRefCacheTTL = 10 * time.Minute

	numRefCacheStripes = 512
)

// RefCache is a single-tenant cache mapping a labels set with the reference
// ID in TSDB, in order to be able to append samples to the TSDB head without having
// to copy write request series labels each time (because the memory buffers used to
// unmarshal the write request is reused).
type RefCache struct {
	// The cache is split into stripes, each one with a dedicated lock, in
	// order to reduce lock contention.
	stripes [numRefCacheStripes]refCacheStripe
}

// refCacheStripe holds a subset of the series references for a single tenant.
type refCacheStripe struct {
	refsMu sync.RWMutex
	refs   map[model.Fingerprint][]refCacheEntry
}

// refCacheEntry holds a single series reference.
type refCacheEntry struct {
	lbs       labels.Labels
	ref       uint64
	touchedAt atomic.Int64 // Unix nano time.
}

// NewRefCache makes a new RefCache.
func NewRefCache() *RefCache {
	c := &RefCache{}

	// Stripes are pre-allocated so that we only read on them and no lock is required.
	for i := 0; i < numRefCacheStripes; i++ {
		c.stripes[i].refs = map[model.Fingerprint][]refCacheEntry{}
	}

	return c
}

// Ref returns the cached series reference, and guarantees the input labels set
// is NOT retained.
func (c *RefCache) Ref(now time.Time, series labels.Labels) (uint64, bool) {
	fp := client.Fingerprint(series)
	stripeID := util.HashFP(fp) % numRefCacheStripes

	return c.stripes[stripeID].ref(now, series, fp)
}

// SetRef sets/updates the cached series reference. The input labels set IS retained.
func (c *RefCache) SetRef(now time.Time, series labels.Labels, ref uint64) {
	fp := client.Fingerprint(series)
	stripeID := util.HashFP(fp) % numRefCacheStripes

	c.stripes[stripeID].setRef(now, series, fp, ref)
}

// Purge removes expired entries from the cache. This function should be called
// periodically to avoid memory leaks.
func (c *RefCache) Purge(keepUntil time.Time) {
	for s := 0; s < numRefCacheStripes; s++ {
		c.stripes[s].purge(keepUntil)
	}
}

func (s *refCacheStripe) ref(now time.Time, series labels.Labels, fp model.Fingerprint) (uint64, bool) {
	s.refsMu.RLock()
	defer s.refsMu.RUnlock()

	entries, ok := s.refs[fp]
	if !ok {
		return 0, false
	}

	for ix := range entries {
		if labels.Equal(entries[ix].lbs, series) {
			// Since we use read-only lock, we need to use atomic update.
			entries[ix].touchedAt.Store(now.UnixNano())
			return entries[ix].ref, true
		}
	}

	return 0, false
}

func (s *refCacheStripe) setRef(now time.Time, series labels.Labels, fp model.Fingerprint, ref uint64) {
	s.refsMu.Lock()
	defer s.refsMu.Unlock()

	// Check if already exists within the entries.
	for ix, entry := range s.refs[fp] {
		if !labels.Equal(entry.lbs, series) {
			continue
		}

		entry.ref = ref
		entry.touchedAt.Store(now.UnixNano())
		s.refs[fp][ix] = entry
		return
	}

	// The entry doesn't exist, so we have to add a new one.
	refCacheEntry := refCacheEntry{lbs: series, ref: ref}
	refCacheEntry.touchedAt.Store(now.UnixNano())
	s.refs[fp] = append(s.refs[fp], refCacheEntry)
}

func (s *refCacheStripe) purge(keepUntil time.Time) {
	s.refsMu.Lock()
	defer s.refsMu.Unlock()

	keepUntilNanos := keepUntil.UnixNano()

	for fp, entries := range s.refs {
		// Since we do expect very few fingerprint collisions, we
		// have an optimized implementation for the common case.
		if len(entries) == 1 {
			if entries[0].touchedAt.Load() < keepUntilNanos {
				delete(s.refs, fp)
			}

			continue
		}

		// We have more entries, which means there's a collision,
		// so we have to iterate over the entries.
		for i := 0; i < len(entries); {
			if entries[i].touchedAt.Load() < keepUntilNanos {
				entries = append(entries[:i], entries[i+1:]...)
			} else {
				i++
			}
		}

		// Either update or delete the entries in the map
		if len(entries) == 0 {
			delete(s.refs, fp)
		} else {
			s.refs[fp] = entries
		}
	}
}
