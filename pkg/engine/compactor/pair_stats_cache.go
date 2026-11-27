package compactor

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/thanos-io/objstore"
)

// Values of the result label of pairStatsCacheRequestsTotal.
const (
	statsCacheHit  = "hit"
	statsCacheMiss = "miss"
)

// logSectionsCache holds the stats reads of the index runs of one tenant, so
// a scan reads only the index runs that are new since the last scan.
//
// Index object paths are content hashes, and an index object never changes.
// So a read of an index run stays valid while the ToC of its window lists the
// index run. The cache keeps, for each window, only the index runs that the
// last ToC read of the window lists. A failed ToC read drops nothing. The
// cache does not cache errors, so a scan reads a failed or corrupt index run
// again.
//
// Callers must not modify a returned read, because later scans share it.
//
// logSectionsCache is safe for concurrent use.
type logSectionsCache struct {
	tenant  string
	metrics *coordinatorMetrics

	windowsMu sync.Mutex
	// windows holds the cached reads of each window, by index run key.
	windows map[time.Time]map[string]cachedLogSections
	// runs and keys are the number of cached reads, and of keys in them.
	runs, keys int
}

type cachedLogSections struct {
	read logSectionsRead
	keys int
}

func newLogSectionsCache(tenant string, metrics *coordinatorMetrics) *logSectionsCache {
	return &logSectionsCache{tenant: tenant, metrics: metrics, windows: make(map[time.Time]map[string]cachedLogSections)}
}

// dropUnlisted drops each cached read of window whose index run is not in
// indexRuns. indexRuns are the index runs of the latest ToC of window. A nil
// indexRuns, for a window without a ToC, drops every read of window.
func (c *logSectionsCache) dropUnlisted(window time.Time, indexRuns []indexRun) {
	listed := make(map[string]struct{}, len(indexRuns))
	for _, run := range indexRuns {
		listed[indexRunCacheKey(run)] = struct{}{}
	}
	c.windowsMu.Lock()
	defer c.windowsMu.Unlock()
	for key, entry := range c.windows[window] {
		if _, ok := listed[key]; !ok {
			c.dropLocked(window, key, entry)
		}
	}
	c.reportLocked()
}

// dropWindowsExcept drops the cached reads of every window not in windows,
// for example a window that left the planned range.
func (c *logSectionsCache) dropWindowsExcept(windows []time.Time) {
	c.windowsMu.Lock()
	defer c.windowsMu.Unlock()
	for window, entries := range c.windows {
		if slices.ContainsFunc(windows, window.Equal) {
			continue
		}
		for key, entry := range entries {
			c.dropLocked(window, key, entry)
		}
		delete(c.windows, window)
	}
	c.reportLocked()
}

// read returns the stats read of indexRun of window from the cache, or reads
// it with readLogSections and caches the result. It returns the errors of
// readLogSections, and caches nothing on an error.
//
// A cached read is only valid for the paths of indexRun, so read checks the
// run length of the ToC entries on a hit too. The ToC can list the same paths
// with another run length.
func (c *logSectionsCache) read(ctx context.Context, bucket objstore.Bucket, window time.Time, indexRun indexRun) (logSectionsRead, error) {
	if err := indexRun.checkLength(); err != nil {
		return logSectionsRead{}, err
	}
	key := indexRunCacheKey(indexRun)
	c.windowsMu.Lock()
	entry, ok := c.windows[window][key]
	c.windowsMu.Unlock()
	if ok {
		c.metrics.pairStatsCacheRequestsTotal.WithLabelValues(c.tenant, statsCacheHit).Inc()
		return entry.read, nil
	}
	c.metrics.pairStatsCacheRequestsTotal.WithLabelValues(c.tenant, statsCacheMiss).Inc()

	read, err := readLogSections(ctx, bucket, c.tenant, indexRun)
	if err != nil {
		return logSectionsRead{}, err
	}
	var keys int
	for _, section := range read.sections {
		keys += len(section.Keys)
	}
	c.windowsMu.Lock()
	defer c.windowsMu.Unlock()
	if c.windows[window] == nil {
		c.windows[window] = make(map[string]cachedLogSections)
	}
	if old, ok := c.windows[window][key]; ok {
		c.dropLocked(window, key, old)
	}
	c.windows[window][key] = cachedLogSections{read: read, keys: keys}
	c.runs++
	c.keys += keys
	c.reportLocked()
	return read, nil
}

// dropLocked removes the cached read entry of key in window. The caller must
// hold windowsMu.
func (c *logSectionsCache) dropLocked(window time.Time, key string, entry cachedLogSections) {
	delete(c.windows[window], key)
	c.runs--
	c.keys -= entry.keys
}

// reportLocked sets the size gauges of the cache. The caller must hold
// windowsMu.
func (c *logSectionsCache) reportLocked() {
	c.metrics.pairStatsCacheIndexRuns.WithLabelValues(c.tenant).Set(float64(c.runs))
	c.metrics.pairStatsCacheKeys.WithLabelValues(c.tenant).Set(float64(c.keys))
}

// indexRunCacheKey identifies indexRun by the paths of its index objects. The
// ToC does not promise an order of the entries of a run, so the key sorts the
// paths.
func indexRunCacheKey(run indexRun) string {
	paths := run.paths()
	slices.Sort(paths)
	return strings.Join(paths, "\x00")
}
