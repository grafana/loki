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

	// StatObjectBytesDownloaded is the total number of bytes a task pulled from
	// the backing object store via the [rangeReader] interface across every
	// request (metadata reads, page downloads, ad-hoc reads, etc.).
	//
	// It is recorded at the lowest rangeReader layer that actually hits remote
	// storage. Bytes served from the metadata prefetch buffer are not counted
	// again, so this is a true "transferred from storage" figure.
	StatObjectBytesDownloaded = xcap.NewStatisticInt64("dataobj.object.bytes.downloaded", xcap.AggregationTypeSum)

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

	// StatPostingsColumnNamePagesTotal is the total number of pages in the
	// postings column-name column.
	StatPostingsColumnNamePagesTotal = xcap.NewStatisticInt64("postings.column_name.pages.total", xcap.AggregationTypeSum)

	// StatPostingsColumnNameRelevantPages is the number of postings column-name
	// pages whose min/max range overlaps queried label names.
	StatPostingsColumnNameRelevantPages = xcap.NewStatisticInt64("postings.column_name.pages.relevant", xcap.AggregationTypeSum)

	// StatPostingsColumnNamePageRuns is the number of contiguous runs of
	// relevant postings column-name pages.
	StatPostingsColumnNamePageRuns = xcap.NewStatisticInt64("postings.column_name.pages.runs", xcap.AggregationTypeSum)
)
