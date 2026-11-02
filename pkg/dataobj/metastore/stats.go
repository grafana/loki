package metastore

import (
	"sync"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// RegionSections names the xcap region of [ObjectMetastore.Sections] and
// [ObjectMetastore.SectionsStream]. The region lasts until the sequence ends, so it also covers
// the time the caller spends on each batch.
const RegionSections = "metastore.Sections"

// Metastore statistics.
var (
	// StatMetastoreSectionsDuration is the longest time one sections resolution took, in nanoseconds. It
	// records only when the resolution ends without an error and without being canceled. The time does not
	// include the time the caller spends iterating on the result when using the streaming API.
	StatMetastoreSectionsDuration = xcap.NewStatisticInt64("metastore.sections.duration", xcap.AggregationTypeMax)

	// StatMetastoreSectionsFirstIndexDuration is the longest time one sections resolution took to yield the
	// sections of its first index object, in nanoseconds. A later error or break keeps the stat.
	StatMetastoreSectionsFirstIndexDuration = xcap.NewStatisticInt64("metastore.sections.first_index.duration", xcap.AggregationTypeMax)

	// StatMetastoreTocTables is the number of TOC tables read.
	StatMetastoreTocTables = xcap.NewStatisticInt64("metastore.toc.tables", xcap.AggregationTypeSum)

	// StatMetastoreIndexObjects is the number of index objects resolved from the TOC lookup.
	StatMetastoreIndexObjects = xcap.NewStatisticInt64("metastore.index.objects", xcap.AggregationTypeSum)

	// StatMetastoreSectionsResolved is the number of logs section resolved by the metastore query.
	StatMetastoreSectionsResolved = xcap.NewStatisticInt64("metastore.logs.sections.resolved", xcap.AggregationTypeSum)

	// StatPostingsLabelColumnNameTotalPages – the total number of "column_name" column pages for label-based postings,
	// sum across all the sections.
	StatPostingsLabelColumnNameTotalPages = xcap.NewStatisticInt64(
		"dataobj.postings.label.column_name.pages.total",
		xcap.AggregationTypeSum,
	)

	// StatPostingsLabelColumnNameRelevantPages – the total number of "column_name" column relevant pages for
	// label-based postings, sum across all the sections.
	StatPostingsLabelColumnNameRelevantPages = xcap.NewStatisticInt64(
		"dataobj.postings.label.column_name.pages.relevant",
		xcap.AggregationTypeSum,
	)

	// StatPostingsBloomColumnNameTotalPages – the total number of "column_name" column pages for bloom-based postings,
	// sum across all the sections.
	StatPostingsBloomColumnNameTotalPages = xcap.NewStatisticInt64(
		"dataobj.postings.bloom.column_name.pages.total",
		xcap.AggregationTypeSum,
	)

	// StatPostingsBloomColumnNameRelevantPages – the total number of "column_name" column relevant pages for
	// bloom-based postings, sum across all the sections.
	StatPostingsBloomColumnNameRelevantPages = xcap.NewStatisticInt64(
		"dataobj.postings.bloom.column_name.pages.relevant",
		xcap.AggregationTypeSum,
	)
)

type readerStats struct {
	// Initialized is false when the reader never performed a read, in which case
	// ReadRows is meaningless and must not be observed.
	Initialized bool
	ReadRows    uint64
}

type statsProvider interface {
	stats() readerStats
}

type instrumentedReader struct {
	ArrowRecordBatchReader
	metrics *ObjectMetastoreMetrics
	once    sync.Once
}

func (r *instrumentedReader) stats() readerStats {
	return r.ArrowRecordBatchReader.(statsProvider).stats()
}

func (r *instrumentedReader) Close() {
	r.once.Do(func() {
		s := r.stats()
		if s.Initialized {
			r.metrics.indexReadRowsPerObject.Observe(float64(s.ReadRows))
		}
	})
	r.ArrowRecordBatchReader.Close()
}
