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
)
