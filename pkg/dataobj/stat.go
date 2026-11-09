package dataobj

import "github.com/grafana/loki/v3/pkg/xcap"

// These stats are defined alongside the dataobj package so that
// [pkg/dataobj/internal/dataset] can record them without forcing
// [pkg/xcap] to learn about dataobj-internal counters.

var (
	// StatDatasetPagesTotal is the total number of pages contained in the
	// dataset sections opened by a task's [dataset.RowReader], summed across
	// every read column. This is the pre-pruning page count: it is recorded
	// before metadata predicates have a chance to eliminate pages from the read
	// set.
	StatDatasetPagesTotal = xcap.NewStatisticInt64("dataobj.dataset.pages.total", xcap.AggregationTypeSum)

	// StatDatasetPagesPruned counts pages that a task's [dataset.RowReader]
	// eliminated using page-level metadata statistics (min/max) before any page
	// data was downloaded.
	//
	// The counter is incremented per (page, leaf-predicate) rejection, so a
	// page that is rejected by multiple predicates contributes more than once.
	// This matches "work avoided by metadata pruning" rather than "distinct
	// pages skipped".
	StatDatasetPagesPruned = xcap.NewStatisticInt64("dataobj.dataset.pages.pruned", xcap.AggregationTypeSum)

	// StatObjectBytesFetched is the total number of bytes a task fetched through
	// the bucket. Every request made through a bucket from
	// [NewInstrumentedBucket], or a reader from [NewInstrumentedBucketReader],
	// counts: metadata reads, page reads and ad-hoc reads.
	//
	// A body records its bytes when the caller closes it. Bytes served from the
	// metadata prefetch buffer are not counted again. The querier puts the
	// instrumented bucket under the bucket cache. The statistic therefore counts
	// only the bytes that reach object storage. It includes the gaps between the
	// ranges that the coalescing bucket merged. A cache hit counts nothing.
	StatObjectBytesFetched = xcap.NewStatisticInt64("dataobj.object.bytes.fetched", xcap.AggregationTypeSum)

	// StatObjectsOpened* count the objects that [FromBucket] opened. The compaction state of the key
	// picks the statistic. See [IsCompactedObjectPath].
	//
	// An open counts once it has read the object metadata, even when a cache served the read. A failed
	// open counts nothing. Opening one key twice counts twice.
	StatObjectsOpenedCompacted   = xcap.NewStatisticInt64("dataobj.objects.opened.compacted", xcap.AggregationTypeSum)
	StatObjectsOpenedUncompacted = xcap.NewStatisticInt64("dataobj.objects.opened.uncompacted", xcap.AggregationTypeSum)

	// StatObjectRequests* count every request a task issued through a bucket
	// from [NewInstrumentedBucket], or a reader from
	// [NewInstrumentedBucketReader], successful or not.
	//
	// Reads served from memory, such as the prefetch buffer, issue no request and
	// are not counted. The querier puts the instrumented bucket under the bucket
	// cache, so a read that the cache serves issues no request and is not counted.
	StatObjectRequestsAttributes = xcap.NewStatisticInt64("dataobj.object.requests.attributes", xcap.AggregationTypeSum)
	StatObjectRequestsGet        = xcap.NewStatisticInt64("dataobj.object.requests.get", xcap.AggregationTypeSum)
	StatObjectRequestsGetRange   = xcap.NewStatisticInt64("dataobj.object.requests.get_range", xcap.AggregationTypeSum)

	// StatObjectRequestFailures* count the requests of the matching
	// StatObjectRequests* stat that failed, in the call or while reading the
	// body it returned. A not-found error or a canceled request is not a
	// failure. A request counts as failed once, even if several reads of its
	// body fail.
	StatObjectRequestFailuresAttributes = xcap.NewStatisticInt64("dataobj.object.request_failures.attributes", xcap.AggregationTypeSum)
	StatObjectRequestFailuresGet        = xcap.NewStatisticInt64("dataobj.object.request_failures.get", xcap.AggregationTypeSum)
	StatObjectRequestFailuresGetRange   = xcap.NewStatisticInt64("dataobj.object.request_failures.get_range", xcap.AggregationTypeSum)

	// Dataset column statistics.
	StatDatasetPrimaryColumns       = xcap.NewStatisticInt64("primary.columns", xcap.AggregationTypeSum, xcap.Local())
	StatDatasetSecondaryColumns     = xcap.NewStatisticInt64("secondary.columns", xcap.AggregationTypeSum, xcap.Local())
	StatDatasetPrimaryColumnPages   = xcap.NewStatisticInt64("primary.column.pages", xcap.AggregationTypeSum, xcap.Local())
	StatDatasetSecondaryColumnPages = xcap.NewStatisticInt64("secondary.column.pages", xcap.AggregationTypeSum, xcap.Local())

	// Dataset row statistics.
	StatDatasetMaxRows           = xcap.NewStatisticInt64("rows.max", xcap.AggregationTypeSum)
	StatDatasetRowsAfterPruning  = xcap.NewStatisticInt64("rows.after.pruning", xcap.AggregationTypeSum, xcap.Local())
	StatDatasetPrimaryRowsRead   = xcap.NewStatisticInt64("primary.rows.read", xcap.AggregationTypeSum)
	StatDatasetSecondaryRowsRead = xcap.NewStatisticInt64("secondary.rows.read", xcap.AggregationTypeSum)
	StatDatasetPrimaryRowBytes   = xcap.NewStatisticInt64("primary.row.read.bytes", xcap.AggregationTypeSum)
	StatDatasetSecondaryRowBytes = xcap.NewStatisticInt64("secondary.row.read.bytes", xcap.AggregationTypeSum)

	// Dataset page scan statistics.
	StatDatasetPageDownloadTime = xcap.NewStatisticFloat64("pages.download.duration", xcap.AggregationTypeSum, xcap.Local())

	// Dataset page download byte statistics.
	StatDatasetPrimaryPagesDownloaded           = xcap.NewStatisticInt64("primary.pages.downloaded", xcap.AggregationTypeSum)
	StatDatasetSecondaryPagesDownloaded         = xcap.NewStatisticInt64("secondary.pages.downloaded", xcap.AggregationTypeSum)
	StatDatasetPrimaryColumnBytes               = xcap.NewStatisticInt64("primary.pages.compressed.bytes", xcap.AggregationTypeSum)
	StatDatasetSecondaryColumnBytes             = xcap.NewStatisticInt64("secondary.pages.compressed.bytes", xcap.AggregationTypeSum)
	StatDatasetPrimaryColumnUncompressedBytes   = xcap.NewStatisticInt64("primary.column.uncompressed.bytes", xcap.AggregationTypeSum, xcap.Local())
	StatDatasetSecondaryColumnUncompressedBytes = xcap.NewStatisticInt64("secondary.column.uncompressed.bytes", xcap.AggregationTypeSum, xcap.Local())

	// Dataset read operation statistics.
	StatDatasetReadCalls = xcap.NewStatisticInt64("dataset.read.calls", xcap.AggregationTypeSum, xcap.Local())

	// -- Following stats are added to understand how data locality affects a query --
	//
	// A row is considered relevant to the query if it matches the stream selector.
	// A page is considered relevant to the query if it contains a matching stream.
	//
	// Good locality would result in most sections being highly relevant.

	// StatStreamPagesTotal is the total number of pages in the stream-ID column.
	StatStreamPagesTotal = xcap.NewStatisticInt64("dataobj.stream.pages.total", xcap.AggregationTypeSum)

	// StatStreamRelevantPages is the number of stream-ID column pages whose
	// min/max range overlaps the queried stream IDs.
	StatStreamRelevantPages = xcap.NewStatisticInt64("dataobj.stream.pages.relevant", xcap.AggregationTypeSum)

	// StatStreamRelevantRows is the number of rows that passed the stream-ID
	// predicate before other query predicates narrowed the result further.
	StatStreamRelevantRows = xcap.NewStatisticInt64("dataobj.stream.rows.relevant", xcap.AggregationTypeSum)

	// StatStreamPageRuns is the number of contiguous runs of relevant
	// stream-ID pages. A single run means all relevant pages are back-to-back;
	// a count equal to relevant pages means every relevant page is isolated.
	StatStreamPageRuns = xcap.NewStatisticInt64("dataobj.stream.pages.runs", xcap.AggregationTypeSum)
)
