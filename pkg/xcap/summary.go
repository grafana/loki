package xcap

import (
	"sort"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
)

// observations holds aggregated observations that can be transformed and merged.
//
// All transformation methods (filter, prefix, normalizeKeys) return new instances,
// leaving the original unchanged.
type observations struct {
	data map[StatisticKey]*AggregatedObservation
}

// newObservations creates an empty observations.
func newObservations() *observations {
	return &observations{data: make(map[StatisticKey]*AggregatedObservation)}
}

// filter returns a new observations containing only entries with matching stat names.
func (o *observations) filter(names ...string) *observations {
	if len(names) == 0 || o == nil {
		return o
	}

	nameSet := make(map[string]struct{}, len(names))
	for _, n := range names {
		nameSet[n] = struct{}{}
	}

	result := newObservations()
	for k, obs := range o.data {
		if _, ok := nameSet[k.Name]; ok {
			result.data[k] = obs
		}
	}
	return result
}

// prefix returns a new observations with all stat names prefixed.
func (o *observations) prefix(p string) *observations {
	if p == "" || o == nil {
		return o
	}

	result := newObservations()
	for k, obs := range o.data {
		newKey := StatisticKey{
			Name:        p + k.Name,
			DataType:    k.DataType,
			Aggregation: k.Aggregation,
		}
		result.data[newKey] = obs
	}
	return result
}

// normalizeKeys returns a new observations with dots replaced by underscores in stat names.
func (o *observations) normalizeKeys() *observations {
	if o == nil {
		return o
	}

	result := newObservations()
	for k, obs := range o.data {
		newKey := StatisticKey{
			Name:        strings.ReplaceAll(k.Name, ".", "_"),
			DataType:    k.DataType,
			Aggregation: k.Aggregation,
		}
		result.data[newKey] = obs
	}
	return result
}

// merge merges another observations into this one.
func (o *observations) merge(other *observations) {
	if other == nil {
		return
	}
	for k, obs := range other.data {
		if existing, ok := o.data[k]; ok {
			existing.Merge(obs)
		} else {
			o.data[k] = &AggregatedObservation{
				Statistic: obs.Statistic,
				Value:     obs.Value,
				Count:     obs.Count,
			}
		}
	}
}

// ToLogValues converts observations to a slice suitable for go-kit/log.
// Keys are sorted for deterministic output.
func (o *observations) toLogValues() []any {
	if o == nil {
		return nil
	}

	// Collect key-value pairs for sorting by name.
	type kv struct {
		name  string
		value any
	}
	pairs := make([]kv, 0, len(o.data))
	for k, obs := range o.data {
		pairs = append(pairs, kv{name: k.Name, value: obs.Value})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return strings.Compare(pairs[i].name, pairs[j].name) < 0
	})

	result := make([]any, 0, len(pairs)*2)
	for _, p := range pairs {
		value := p.value

		// Format bytes values (keys ending with "_bytes")
		if strings.HasSuffix(p.name, "_bytes") {
			switch val := value.(type) {
			case uint64:
				value = humanize.Bytes(val)
			case int64:
				value = humanize.Bytes(uint64(val))
			}
		}

		// Format duration values (keys ending with "duration_ns")
		if strings.HasSuffix(p.name, "duration_ns") {
			switch val := value.(type) {
			case int64:
				value = time.Duration(val).String()
			case uint64:
				value = time.Duration(val).String()
			}
		}

		result = append(result, p.name, value)
	}
	return result
}

// observationCollector provides composable methods to collect observations from a Capture.
// It pre-computes the region tree for efficient rollup operations.
type observationCollector struct {
	capture     *Capture
	childrenMap map[identifier][]*Region
}

// newObservationCollector creates a new collector for gathering observations from the given capture.
func newObservationCollector(capture *Capture) *observationCollector {
	if capture == nil {
		return nil
	}

	// Build parent -> children map
	childrenMap := make(map[identifier][]*Region)
	for _, r := range capture.Regions() {
		childrenMap[r.parentID] = append(childrenMap[r.parentID], r)
	}

	return &observationCollector{
		capture:     capture,
		childrenMap: childrenMap,
	}
}

// fromRegions collects observations from regions with the given name.
// If rollUp is true, each region's stats include all its descendant stats
// aggregated according to each stat's aggregation type.
func (c *observationCollector) fromRegions(name string, rollUp bool, excluded ...string) *observations {
	if c == nil {
		return newObservations()
	}

	excludedSet := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		excludedSet[name] = struct{}{}
	}

	result := newObservations()
	for _, region := range c.capture.Regions() {
		if region.name != name {
			continue
		}

		var obs *observations
		if rollUp {
			obs = c.rollUpObservations(region, excludedSet)
		} else {
			obs = c.getRegionObservations(region)
		}

		result.merge(obs)
	}

	return result
}

// getRegionObservations returns a copy of a region's observations.
func (c *observationCollector) getRegionObservations(region *Region) *observations {
	result := newObservations()
	for k, obs := range region.observations {
		result.data[k] = &AggregatedObservation{
			Statistic: obs.Statistic,
			Value:     obs.Value,
			Count:     obs.Count,
		}
	}
	return result
}

// rollUpObservations computes observations for a region including all its descendants.
// Stats are aggregated according to their aggregation type.
func (c *observationCollector) rollUpObservations(region *Region, excludedSet map[string]struct{}) *observations {
	result := c.getRegionObservations(region)

	// Recursively aggregate from children.
	for _, child := range c.childrenMap[region.id] {
		// Skip children with excluded names.
		if _, excluded := excludedSet[child.name]; excluded {
			continue
		}
		result.merge(c.rollUpObservations(child, excludedSet))
	}

	return result
}
