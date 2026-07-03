package metastore

import (
	"sync"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// Metastore statistics.
var (
	// StatMetastoreTocTables is the number of TOC tables read.
	StatMetastoreTocTables = xcap.NewStatisticInt64("metastore.toc.tables", xcap.AggregationTypeSum)

	// StatMetastoreIndexObjects is the number of index objects resolved from the TOC lookup.
	StatMetastoreIndexObjects = xcap.NewStatisticInt64("metastore.index.objects", xcap.AggregationTypeSum)

	// StatMetastoreSectionsResolved is the number of logs section resolved by the metastore query.
	StatMetastoreSectionsResolved = xcap.NewStatisticInt64("metastore.logs.sections.resolved", xcap.AggregationTypeSum)

	// StatMetastorePointerSectionsOpened is the number of pointer section opened.
	StatMetastorePointerSectionsOpened = xcap.NewStatisticInt64("metastore.sections.opened", xcap.AggregationTypeSum)

	// StatMetastorePointerSectionsProductive counts the number of pointer sections that yielded
	// atleast one pointer.
	StatMetastorePointerSectionsProductive = xcap.NewStatisticInt64("metastore.sections.productive", xcap.AggregationTypeSum)

	StatMetastoreStreamsRead             = xcap.NewStatisticInt64("metastore.sections.streams.read", xcap.AggregationTypeSum)
	StatMetastoreStreamsReadTime         = xcap.NewStatisticFloat64("metastore.sections.streams.read.duration", xcap.AggregationTypeSum)
	StatMetastoreSectionPointersRead     = xcap.NewStatisticInt64("metastore.sections.pointers.read", xcap.AggregationTypeSum)
	StatMetastoreSectionPointersReadTime = xcap.NewStatisticFloat64("metastore.sections.pointers.read.duration", xcap.AggregationTypeSum)
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
