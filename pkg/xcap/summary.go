package xcap

import (
	"time"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

// observations holds aggregated observations that can be transformed and merged.
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
// excluded names are skipped when rolling up children.
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

	// Collect observations from logs reader as the summary stats mainly relate to log lines.
	// In practice, new engine would process more bytes while scanning metastore objects and stream sections.
	collector := newObservationCollector(c)
	observations := collector.fromRegions("logs.Reader.Read", false).filter(
		StatDatasetPrimaryRowsRead.Key(),
		StatDatasetSecondaryRowsRead.Key(),
		StatDatasetPrimaryRowBytes.Key(),
		StatDatasetSecondaryRowBytes.Key(),
	)

	// TODO: track and report TotalStructuredMetadataBytesProcessed
	result.Querier.Store.Dataobj.PrePredicateDecompressedBytes = readInt64(observations, StatDatasetPrimaryRowBytes.Key())
	result.Querier.Store.Dataobj.PostPredicateDecompressedBytes = readInt64(observations, StatDatasetSecondaryRowBytes.Key())
	result.Querier.Store.Dataobj.PrePredicateDecompressedRows = readInt64(observations, StatDatasetPrimaryRowsRead.Key())
	// TotalPostFilterLines: rows output after filtering
	// TODO: this will report the wrong value if the plan has a filter stage.
	// pick the min of row_out from filter and scan nodes.
	result.Querier.Store.Dataobj.PostFilterRows = readInt64(observations, StatDatasetSecondaryRowsRead.Key())

	// Collect task cache stats and scheduler transfer stats from worker threads (thread.runJob).
	// All tasks — both metastore and execution tasks run on worker threads,
	// so thread.runJob captures the full picture.
	//
	// NOTE: do NOT also collect from engine.metastoreResolver (rollUp=true), because
	// worker threads are linked as children of that region, which would double-count
	// the metastore worker stats.
	workerCache := collector.fromRegions("thread.runJob", true).filter(
		TaskCacheHits.Key(), TaskCacheMisses.Key(),
		TaskCacheBatches.Key(), TaskCacheBytes.Key(),
		DataObjScanCacheHits.Key(), DataObjScanCacheMisses.Key(),
		DataObjScanCacheBatches.Key(), DataObjScanCacheBytes.Key(),
		TaskWireBytes.Key(),
	)

	taskHits := readInt64(workerCache, TaskCacheHits.Key()) + readInt64(workerCache, DataObjScanCacheHits.Key())
	taskMisses := readInt64(workerCache, TaskCacheMisses.Key()) + readInt64(workerCache, DataObjScanCacheMisses.Key())
	taskBatches := readInt64(workerCache, TaskCacheBatches.Key()) + readInt64(workerCache, DataObjScanCacheBatches.Key())
	taskBytes := readInt64(workerCache, TaskCacheBytes.Key()) + readInt64(workerCache, DataObjScanCacheBytes.Key())

	result.Caches.TaskResult.EntriesFound = int32(taskHits)
	result.Caches.TaskResult.EntriesRequested = int32(taskHits + taskMisses)
	result.Caches.TaskResult.Requests = int32(taskBatches)
	result.Caches.TaskResult.BytesReceived = taskBytes

	result.Querier.Store.Dataobj.WireBytesTransferred = readInt64(workerCache, TaskWireBytes.Key())

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
