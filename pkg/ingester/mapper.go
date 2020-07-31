package ingester

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/ingester/client"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"
)

const maxMappedFP = 1 << 20 // About 1M fingerprints reserved for mapping.

var separatorString = string([]byte{model.SeparatorByte})

// fpMapper is used to map fingerprints in order to work around fingerprint
// collisions.
type fpMapper struct {
	// highestMappedFP has to be aligned for atomic operations.
	highestMappedFP atomic.Uint64

	mtx sync.RWMutex // Protects mappings.
	// maps original fingerprints to a map of string representations of
	// metrics to the truly unique fingerprint.
	mappings map[model.Fingerprint]map[string]model.Fingerprint

	// Returns existing labels for given fingerprint, if any.
	// Equality check relies on labels.Labels being sorted.
	fpToLabels func(fingerprint model.Fingerprint) labels.Labels
}

// newFPMapper returns an fpMapper ready to use.
func newFPMapper(fpToLabels func(fingerprint model.Fingerprint) labels.Labels) *fpMapper {
	if fpToLabels == nil {
		panic("nil fpToLabels")
	}

	return &fpMapper{
		fpToLabels: fpToLabels,
		mappings:   map[model.Fingerprint]map[string]model.Fingerprint{},
	}
}

// mapFP takes a raw fingerprint (as returned by Metrics.FastFingerprint) and
// returns a truly unique fingerprint. The caller must have locked the raw
// fingerprint.
func (m *fpMapper) mapFP(fp model.Fingerprint, metric []client.LabelAdapter) model.Fingerprint {
	// First check if we are in the reserved FP space, in which case this is
	// automatically a collision that has to be mapped.
	if fp <= maxMappedFP {
		return m.maybeAddMapping(fp, metric)
	}

	// Then check the most likely case: This fp belongs to a series that is
	// already in memory.
	s := m.fpToLabels(fp)
	if s != nil {
		// FP exists in memory, but is it for the same metric?
		if equalLabels(metric, s) {
			// Yupp. We are done.
			return fp
		}
		// Collision detected!
		return m.maybeAddMapping(fp, metric)
	}
	// Metric is not in memory. Before doing the expensive archive lookup,
	// check if we have a mapping for this metric in place already.
	m.mtx.RLock()
	mappedFPs, fpAlreadyMapped := m.mappings[fp]
	m.mtx.RUnlock()
	if fpAlreadyMapped {
		// We indeed have mapped fp historically.
		ms := metricToUniqueString(metric)
		// fp is locked by the caller, so no further locking of
		// 'collisions' required (it is specific to fp).
		mappedFP, ok := mappedFPs[ms]
		if ok {
			// Historical mapping found, return the mapped FP.
			return mappedFP
		}
	}
	return fp
}

func valueForName(s labels.Labels, name string) (string, bool) {
	pos := sort.Search(len(s), func(i int) bool { return s[i].Name >= name })
	if pos == len(s) || s[pos].Name != name {
		return "", false
	}
	return s[pos].Value, true
}

// Check if a and b contain the same name/value pairs
func equalLabels(a []client.LabelAdapter, b labels.Labels) bool {
	if len(a) != len(b) {
		return false
	}
	// Check as many as we can where the two sets are in the same order
	i := 0
	for ; i < len(a); i++ {
		if b[i].Name != a[i].Name {
			break
		}
		if b[i].Value != a[i].Value {
			return false
		}
	}
	// Now check remaining values using binary search
	for ; i < len(a); i++ {
		v, found := valueForName(b, a[i].Name)
		if !found || v != a[i].Value {
			return false
		}
	}
	return true
}

// maybeAddMapping is only used internally. It takes a detected collision and
// adds it to the collisions map if not yet there. In any case, it returns the
// truly unique fingerprint for the colliding metric.
func (m *fpMapper) maybeAddMapping(fp model.Fingerprint, collidingMetric []client.LabelAdapter) model.Fingerprint {
	ms := metricToUniqueString(collidingMetric)
	m.mtx.RLock()
	mappedFPs, ok := m.mappings[fp]
	m.mtx.RUnlock()
	if ok {
		// fp is locked by the caller, so no further locking required.
		mappedFP, ok := mappedFPs[ms]
		if ok {
			return mappedFP // Existing mapping.
		}
		// A new mapping has to be created.
		mappedFP = m.nextMappedFP()
		mappedFPs[ms] = mappedFP
		level.Info(util.Logger).Log(
			"msg", "fingerprint collision detected, mapping to new fingerprint",
			"old_fp", fp,
			"new_fp", mappedFP,
			"metric", ms,
		)
		return mappedFP
	}
	// This is the first collision for fp.
	mappedFP := m.nextMappedFP()
	mappedFPs = map[string]model.Fingerprint{ms: mappedFP}
	m.mtx.Lock()
	m.mappings[fp] = mappedFPs
	m.mtx.Unlock()
	level.Info(util.Logger).Log(
		"msg", "fingerprint collision detected, mapping to new fingerprint",
		"old_fp", fp,
		"new_fp", mappedFP,
		"metric", collidingMetric,
	)
	return mappedFP
}

func (m *fpMapper) nextMappedFP() model.Fingerprint {
	mappedFP := model.Fingerprint(m.highestMappedFP.Inc())
	if mappedFP > maxMappedFP {
		panic(fmt.Errorf("more than %v fingerprints mapped in collision detection", maxMappedFP))
	}
	return mappedFP
}

// metricToUniqueString turns a metric into a string in a reproducible and
// unique way, i.e. the same metric will always create the same string, and
// different metrics will always create different strings. In a way, it is the
// "ideal" fingerprint function, only that it is more expensive than the
// FastFingerprint function, and its result is not suitable as a key for maps
// and indexes as it might become really large, causing a lot of hashing effort
// in maps and a lot of storage overhead in indexes.
func metricToUniqueString(m []client.LabelAdapter) string {
	parts := make([]string, 0, len(m))
	for _, pair := range m {
		parts = append(parts, pair.Name+separatorString+pair.Value)
	}
	sort.Strings(parts)
	return strings.Join(parts, separatorString)
}
