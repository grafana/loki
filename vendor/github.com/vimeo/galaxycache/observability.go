/*
Copyright 2018 Google LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package galaxycache

import (
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	// Copied from https://github.com/census-instrumentation/opencensus-go/blob/ff7de98412e5c010eb978f11056f90c00561637f/plugin/ocgrpc/stats_common.go#L54
	defaultBytesDistribution = view.Distribution(0, 1024, 2048, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824, 4294967296)
	// Copied from https://github.com/census-instrumentation/opencensus-go/blob/ff7de98412e5c010eb978f11056f90c00561637f/plugin/ocgrpc/stats_common.go#L55
	defaultMillisecondsDistribution = view.Distribution(0, 0.01, 0.05, 0.1, 0.3, 0.6, 0.8, 1, 2, 3, 4, 5, 6, 8, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80, 100, 130, 160, 200, 250, 300, 400, 500, 650, 800, 1000, 2000, 5000, 10000, 20000, 50000, 100000)
)

// Opencensus stats
var (
	MGets              = stats.Int64("galaxycache/gets", "The number of Get requests", stats.UnitDimensionless)
	MLoads             = stats.Int64("galaxycache/loads", "The number of gets/cacheHits", stats.UnitDimensionless)
	MLoadErrors        = stats.Int64("galaxycache/loads_errors", "The number of errors encountered during Get", stats.UnitDimensionless)
	MCacheHits         = stats.Int64("galaxycache/cache_hits", "The number of times that the cache was hit", stats.UnitDimensionless)
	MPeerLoads         = stats.Int64("galaxycache/peer_loads", "The number of remote loads or remote cache hits", stats.UnitDimensionless)
	MPeerLoadErrors    = stats.Int64("galaxycache/peer_errors", "The number of remote errors", stats.UnitDimensionless)
	MBackendLoads      = stats.Int64("galaxycache/backend_loads", "The number of successful loads from the backend getter", stats.UnitDimensionless)
	MBackendLoadErrors = stats.Int64("galaxycache/local_load_errors", "The number of failed backend loads", stats.UnitDimensionless)

	MCoalescedLoads        = stats.Int64("galaxycache/coalesced_loads", "The number of loads coalesced by singleflight", stats.UnitDimensionless)
	MCoalescedCacheHits    = stats.Int64("galaxycache/coalesced_cache_hits", "The number of coalesced times that the cache was hit", stats.UnitDimensionless)
	MCoalescedPeerLoads    = stats.Int64("galaxycache/coalesced_peer_loads", "The number of coalesced remote loads or remote cache hits", stats.UnitDimensionless)
	MCoalescedBackendLoads = stats.Int64("galaxycache/coalesced_backend_loads", "The number of coalesced successful loads from the backend getter", stats.UnitDimensionless)

	MServerRequests = stats.Int64("galaxycache/server_requests", "The number of Gets that came over the network from peers", stats.UnitDimensionless)
	MKeyLength      = stats.Int64("galaxycache/key_length", "The length of keys", stats.UnitBytes)
	MValueLength    = stats.Int64("galaxycache/value_length", "The length of values", stats.UnitBytes)

	MRoundtripLatencyMilliseconds = stats.Float64("galaxycache/roundtrip_latency", "Roundtrip latency in milliseconds", stats.UnitMilliseconds)

	MCacheSize    = stats.Int64("galaxycache/cache_bytes", "The number of bytes used for storing Keys and Values in the cache", stats.UnitBytes)
	MCacheEntries = stats.Int64("galaxycache/cache_entries", "The number of entries in the cache", stats.UnitDimensionless)
)

var (
	// GalaxyKey tags the name of the galaxy
	GalaxyKey = tag.MustNewKey("galaxy")

	// CacheLevelKey tags the level at which data was found on Get
	CacheLevelKey = tag.MustNewKey("cache-hit-level")

	// CacheTypeKey tags the galaxy sub-cache the metric applies to
	CacheTypeKey = tag.MustNewKey("cache-type")
)

// AllViews is a slice of default views for people to use
var AllViews = []*view.View{
	{Measure: MGets, TagKeys: []tag.Key{GalaxyKey}, Aggregation: view.Count()},
	{Measure: MLoads, TagKeys: []tag.Key{GalaxyKey}, Aggregation: view.Count()},
	{Measure: MCacheHits, TagKeys: []tag.Key{GalaxyKey, CacheLevelKey}, Aggregation: view.Count()},
	{Measure: MPeerLoads, TagKeys: []tag.Key{GalaxyKey}, Aggregation: view.Count()},
	{Measure: MPeerLoadErrors, TagKeys: []tag.Key{GalaxyKey}, Aggregation: view.Count()},
	{Measure: MBackendLoads, TagKeys: []tag.Key{GalaxyKey}, Aggregation: view.Count()},
	{Measure: MBackendLoadErrors, TagKeys: []tag.Key{GalaxyKey}, Aggregation: view.Count()},

	{Measure: MCoalescedLoads, TagKeys: []tag.Key{GalaxyKey}, Aggregation: view.Count()},
	{Measure: MCoalescedCacheHits, TagKeys: []tag.Key{GalaxyKey, CacheLevelKey}, Aggregation: view.Count()},
	{Measure: MCoalescedPeerLoads, TagKeys: []tag.Key{GalaxyKey}, Aggregation: view.Count()},
	{Measure: MCoalescedBackendLoads, TagKeys: []tag.Key{GalaxyKey}, Aggregation: view.Count()},

	{Measure: MServerRequests, TagKeys: []tag.Key{GalaxyKey}, Aggregation: view.Count()},
	{Measure: MKeyLength, TagKeys: []tag.Key{GalaxyKey}, Aggregation: defaultBytesDistribution},
	{Measure: MValueLength, TagKeys: []tag.Key{GalaxyKey}, Aggregation: defaultBytesDistribution},

	{Measure: MRoundtripLatencyMilliseconds, TagKeys: []tag.Key{GalaxyKey}, Aggregation: defaultMillisecondsDistribution},
	{Measure: MCacheSize, TagKeys: []tag.Key{GalaxyKey, CacheTypeKey}, Aggregation: view.LastValue()},
	{Measure: MCacheEntries, TagKeys: []tag.Key{GalaxyKey, CacheTypeKey}, Aggregation: view.LastValue()},
}

func sinceInMilliseconds(start time.Time) float64 {
	d := time.Since(start)
	return float64(d.Nanoseconds()) / 1e6
}
