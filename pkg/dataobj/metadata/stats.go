package metadata

import (
	"context"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// These stats count the metadata that a query reads from a cache or from object storage, in two phases. The
// prefetch phase is the work of the metadata prefetcher. The on demand phase is every other read: the
// reads of a section, the reads of an open that does not use the prefetcher, and the reads of a table
// of contents.
//
// The stats of object storage take their counts from the statistics of the instrumented bucket, which
// [pkg/dataobj/bucketstat] defines. A read that does not go through that bucket reports none. This
// package records the reads that it runs, and the loads of [Cache]. [pkg/dataobj] and the metastore
// record the other on demand reads.

var (
	// StatMetadataPrefetchRequestsToCache counts the lookups that the metadata prefetcher sent to its
	// cache. One lookup is one call of the cache Fetch method, whatever the number of keys. A cache
	// client can split one lookup into several network requests, for example in batches of keys. A
	// lookup counts when it fails.
	//
	// StatMetadataPrefetchBytesFromCache counts the bytes of metadata that those lookups returned. It is
	// the total length of the data of the groups and region items that decoded, so it counts the decoded
	// bytes and not the stored bytes of the cache items. It counts a lookup whose result the prefetcher
	// then does not use, for example a partial miss that sends the prefetcher to object storage.
	StatMetadataPrefetchRequestsToCache = xcap.NewStatisticInt64("dataobj.metadata.prefetch.requests.to_cache", xcap.AggregationTypeSum)
	StatMetadataPrefetchBytesFromCache  = xcap.NewStatisticInt64("dataobj.metadata.prefetch.bytes.from_cache", xcap.AggregationTypeSum)

	// StatMetadataPrefetchRequestsToStorage counts the object-store requests that the metadata
	// prefetcher sent to load metadata that its cache did not hold. A failed request counts too.
	//
	// StatMetadataPrefetchBytesFromStorage counts the bytes that those requests downloaded. A load can
	// download more than the response holds, for example the whole region of an object.
	//
	// The region loader records these two stats, for the read that it sends to object storage. When the
	// cache answers first, the loader cancels that read, and the request that it sent still counts.
	StatMetadataPrefetchRequestsToStorage = xcap.NewStatisticInt64("dataobj.metadata.prefetch.requests.to_storage", xcap.AggregationTypeSum)
	StatMetadataPrefetchBytesFromStorage  = xcap.NewStatisticInt64("dataobj.metadata.prefetch.bytes.from_storage", xcap.AggregationTypeSum)

	// StatMetadataRequestsToCache counts the lookups that [Cache] received, and StatMetadataBytesFromCache
	// counts the bytes that the cache served. They are the on demand counterparts of the prefetch stats.
	// The bytes are the length of the cached value, and not the stored bytes of the cache item. A
	// lookup counts when it fails.
	StatMetadataRequestsToCache = xcap.NewStatisticInt64("dataobj.metadata.requests.to_cache", xcap.AggregationTypeSum)
	StatMetadataBytesFromCache  = xcap.NewStatisticInt64("dataobj.metadata.bytes.from_cache", xcap.AggregationTypeSum)

	// StatMetadataRequestsToStorage counts the object-store requests of the on demand reads of metadata,
	// and StatMetadataBytesFromStorage counts the bytes that those requests downloaded. A read that a
	// prefetched range serves, or that a reader over an [io.ReaderAt] serves, sends no request and
	// downloads no byte. A failed request counts too.
	//
	// [Cache] records them for its loads, and [pkg/dataobj] and the metastore record them for the reads
	// outside the cache: the reads of a section, the direct reads of an open and the reads of a table of
	// contents. The bytes are transferred bytes. A reader can merge two close reads into one request that also covers
	// the bytes between them, and a large read splits into several requests.
	StatMetadataRequestsToStorage = xcap.NewStatisticInt64("dataobj.metadata.requests.to_storage", xcap.AggregationTypeSum)
	StatMetadataBytesFromStorage  = xcap.NewStatisticInt64("dataobj.metadata.bytes.from_storage", xcap.AggregationTypeSum)
)

// recordPrefetchCacheLookup records one cache lookup of the prefetcher in the region of ctx.
func recordPrefetchCacheLookup(ctx context.Context) {
	xcap.RegionFromContext(ctx).Record(StatMetadataPrefetchRequestsToCache.Observe(1))
}

// recordPrefetchCacheBytes records the bytes that the cache served to the prefetcher, in the region of ctx.
func recordPrefetchCacheBytes(ctx context.Context, bytes int64) {
	xcap.RegionFromContext(ctx).Record(StatMetadataPrefetchBytesFromCache.Observe(bytes))
}

// recordCacheLookup records one lookup of [Cache] in the region of ctx.
func recordCacheLookup(ctx context.Context) {
	xcap.RegionFromContext(ctx).Record(StatMetadataRequestsToCache.Observe(1))
}

// recordCacheBytes records the bytes that [Cache] served, in the region of ctx.
func recordCacheBytes(ctx context.Context, bytes int64) {
	xcap.RegionFromContext(ctx).Record(StatMetadataBytesFromCache.Observe(bytes))
}
