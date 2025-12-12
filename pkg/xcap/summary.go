package xcap

import (
	"sort"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
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

// filter returns a new observations containing only entries with matching stat keys.
func (o *observations) filter(keys ...StatisticKey) *observations {
	if len(keys) == 0 || o == nil {
		return o
	}

	keySet := make(map[StatisticKey]struct{}, len(keys))
	for _, k := range keys {
		keySet[k] = struct{}{}
	}

	result := newObservations()
	for k, obs := range o.data {
		if _, ok := keySet[k]; ok {
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

		// Format duration values (keys ending with "duration")
		if strings.HasSuffix(p.name, "duration") {
			switch val := value.(type) {
			case float64:
				value = time.Duration(val * float64(time.Second)).String()
			case int64:
				value = time.Duration(val * int64(time.Second)).String()
			case uint64:
				value = time.Duration(val * uint64(time.Second)).String()
			}
		}

		result = append(result, p.name, value)
	}
	return result
}

// observationCollector provides methods to collect observations from a Capture.
type observationCollector struct {
	capture       *Capture
	childrenMap   map[identifier][]*Region
	nameToRegions map[string][]*Region
}

// newObservationCollector creates a new collector for gathering observations from the given capture.
func newObservationCollector(capture *Capture) *observationCollector {
	if capture == nil {
		return nil
	}

	// Build
	// - parent -> children
	// - name -> matching regions
	childrenMap := make(map[identifier][]*Region)
	nameToRegions := make(map[string][]*Region)
	for _, r := range capture.regions {
		childrenMap[r.parentID] = append(childrenMap[r.parentID], r)
		nameToRegions[r.name] = append(nameToRegions[r.name], r)
	}

	return &observationCollector{
		capture:       capture,
		childrenMap:   childrenMap,
		nameToRegions: nameToRegions,
	}
}

// fromRegions collects observations from regions with the given name.
// If rollUp is true, each region's stats include all its descendant stats
// aggregated according to each stat's aggregation type.
func (c *observationCollector) fromRegions(name string, rollUp bool, excluded ...string) *observations {
	result := newObservations()

	if c == nil {
		return result
	}

	regions := c.nameToRegions[name]
	if len(regions) == 0 {
		return result
	}

	excludedSet := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		excludedSet[name] = struct{}{}
	}

	for _, region := range regions {
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

// Region name for data object scan operations.
const regionNameDataObjScan = "DataObjScan"

// ToStatsSummary computes a stats.Result from observations in the capture.
func (c *Capture) ToStatsSummary(execTime, queueTime time.Duration, totalEntriesReturned int) stats.Result {
	result := stats.Result{
		Querier: stats.Querier{
			Store: stats.Store{
				QueryUsedV2Engine: true,
			},
		},
	}

	if c == nil {
		result.ComputeSummary(execTime, queueTime, totalEntriesReturned)
		return result
	}

	// Collect observations from DataObjScan as the summary stats mainly relate to log lines.
	// In practice, new engine would process more bytes while scanning metastore objects and stream sections.
	collector := newObservationCollector(c)
	observations := collector.fromRegions(regionNameDataObjScan, true).filter(
		StatPipelineRowsOut.Key(),
		StatDatasetPrimaryRowsRead.Key(),
		StatDatasetPrimaryColumnUncompressedBytes.Key(),
		StatDatasetSecondaryColumnUncompressedBytes.Key(),
	)

	// TODO: track and report TotalStructuredMetadataBytesProcessed
	result.Querier.Store.Dataobj.PrePredicateDecompressedBytes = readInt64(observations, StatDatasetPrimaryColumnUncompressedBytes.Key())
	result.Querier.Store.Dataobj.PostPredicateDecompressedBytes = readInt64(observations, StatDatasetSecondaryColumnUncompressedBytes.Key())
	result.Querier.Store.Dataobj.PrePredicateDecompressedRows = readInt64(observations, StatDatasetPrimaryRowsRead.Key())
	// TotalPostFilterLines: rows output after filtering
	// TODO: this will report the wrong value if the plan has a filter stage.
	// pick the min of row_out from filter and scan nodes.
	result.Querier.Store.Dataobj.PostFilterRows = readInt64(observations, StatPipelineRowsOut.Key())

	result.ComputeSummary(execTime, queueTime, totalEntriesReturned)
	return result
}

// readInt64 reads an int64 observation for the given stat key.
func readInt64(o *observations, key StatisticKey) int64 {
	if o == nil {
		return 0
	}

	if agg, ok := o.data[key]; ok {
		if v, ok := agg.Int64(); ok {
			return v
		}
	}
	return 0
}
