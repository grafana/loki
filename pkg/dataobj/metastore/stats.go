package metastore

import "github.com/grafana/loki/v3/pkg/xcap"

// Metastore statistics.
var (
	// StatMetastoreTocTables is the number of TOC tables read.
	StatMetastoreTocTables = xcap.NewStatisticInt64("metastore.toc.tables", xcap.AggregationTypeSum)

	// StatMetastorePointerSectionsOpened is the number of pointer section opened.
	StatMetastorePointerSectionsOpened = xcap.NewStatisticInt64("metastore.sections.opened", xcap.AggregationTypeSum)

	// StatMetastorePointerSectionsProductive counts the number of pointer sections that yielded
	// atleast one pointer.
	StatMetastorePointerSectionsProductive = xcap.NewStatisticInt64("metastore.sections.productive", xcap.AggregationTypeSum)
)
