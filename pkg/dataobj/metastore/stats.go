package metastore

import (
	"sync"

	"github.com/grafana/loki/v3/pkg/xcap"
	"github.com/grafana/loki/v3/pkg/xcap/statid"
)

// Metastore statistics.
var (
	// StatMetastoreTocTables is the number of TOC tables read.
	StatMetastoreTocTables = xcap.NewStatisticInt64(statid.MetastoreTocTables, "metastore.toc.tables", xcap.AggregationTypeSum)

	// StatMetastoreIndexObjects is the number of index objects resolved from the TOC lookup.
	StatMetastoreIndexObjects = xcap.NewStatisticInt64(statid.MetastoreIndexObjects, "metastore.index.objects", xcap.AggregationTypeSum)

	// StatMetastoreSectionsResolved is the number of logs section resolved by the metastore query.
	StatMetastoreSectionsResolved = xcap.NewStatisticInt64(statid.MetastoreSectionsResolved, "metastore.logs.sections.resolved", xcap.AggregationTypeSum)

	// StatMetastorePointerSectionsOpened is the number of pointer section opened.
	StatMetastorePointerSectionsOpened = xcap.NewStatisticInt64(statid.MetastorePointerSectionsOpened, "metastore.sections.opened", xcap.AggregationTypeSum)

	// StatMetastorePointerSectionsProductive counts the number of pointer sections that yielded
	// atleast one pointer.
	StatMetastorePointerSectionsProductive = xcap.NewStatisticInt64(statid.MetastorePointerSectionsProductive, "metastore.sections.productive", xcap.AggregationTypeSum)

	// StatPostingsLabelColumnNameTotalPages – the total number of "column_name" column pages for label-based postings,
	// sum across all the sections.
	StatPostingsLabelColumnNameTotalPages = xcap.NewStatisticInt64(statid.PostingsLabelColumnNameTotalPages,
		"dataobj.postings.label.column_name.pages.total",
		xcap.AggregationTypeSum,
	)

	// StatPostingsLabelColumnNameRelevantPages – the total number of "column_name" column relevant pages for
	// label-based postings, sum across all the sections.
	StatPostingsLabelColumnNameRelevantPages = xcap.NewStatisticInt64(statid.PostingsLabelColumnNameRelevantPages,
		"dataobj.postings.label.column_name.pages.relevant",
		xcap.AggregationTypeSum,
	)

	// StatPostingsBloomColumnNameTotalPages – the total number of "column_name" column pages for bloom-based postings,
	// sum across all the sections.
	StatPostingsBloomColumnNameTotalPages = xcap.NewStatisticInt64(statid.PostingsBloomColumnNameTotalPages,
		"dataobj.postings.bloom.column_name.pages.total",
		xcap.AggregationTypeSum,
	)

	// StatPostingsBloomColumnNameRelevantPages – the total number of "column_name" column relevant pages for
	// bloom-based postings, sum across all the sections.
	StatPostingsBloomColumnNameRelevantPages = xcap.NewStatisticInt64(statid.PostingsBloomColumnNameRelevantPages,
		"dataobj.postings.bloom.column_name.pages.relevant",
		xcap.AggregationTypeSum,
	)
)

const (
	flowPostings = "postings"
	flowStreams  = "streams"
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
	flow    string
	once    sync.Once
}

func (r *instrumentedReader) stats() readerStats {
	return r.ArrowRecordBatchReader.(statsProvider).stats()
}

func (r *instrumentedReader) Close() {
	r.once.Do(func() {
		s := r.stats()
		if s.Initialized {
			r.metrics.indexReadRowsPerObject.WithLabelValues(r.flow).Observe(float64(s.ReadRows))
		}
	})
	r.ArrowRecordBatchReader.Close()
}
