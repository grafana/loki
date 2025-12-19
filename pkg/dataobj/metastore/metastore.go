package metastore

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/xcap"
)

type Metastore interface {
	// Sections returns a list of SectionDescriptors, including metadata (stream IDs, start & end times, bytes), for the given matchers & predicates between [start,end]
	Sections(ctx context.Context, req SectionsRequest) (SectionsResponse, error)

	GetIndexes(ctx context.Context, req GetIndexesRequest) (GetIndexesResponse, error)
	IndexSectionsReader(ctx context.Context, req IndexSectionsReaderRequest) (IndexSectionsReaderResponse, error)
	CollectSections(ctx context.Context, req CollectSectionsRequest) (CollectSectionsResponse, error)

	// Labels returns all possible labels from matching streams between [start,end]
	Labels(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) // Used to get possible labels for a given stream

	// Values returns all possible values for the given label matchers between [start,end]
	Values(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) // Used to get all values for a given set of label matchers
}

type SectionsRequest struct {
	Start      time.Time
	End        time.Time
	Matchers   []*labels.Matcher
	Predicates []*labels.Matcher
}

type SectionsResponse struct {
	Sections []*DataobjSectionDescriptor
}

type GetIndexesRequest struct {
	Start time.Time
	End   time.Time
}

type GetIndexesResponse struct {
	TableOfContentsPaths []string
	IndexesPaths         []string
}

type IndexSectionsReaderRequest struct {
	IndexPath       string
	Region          *xcap.Region
	SectionsRequest SectionsRequest
}

type IndexSectionsReaderResponse struct {
	Reader           ArrowRecordBatchReader
	LabelsByStreamID map[int64][]string
}

type CollectSectionsRequest struct {
	Reader           ArrowRecordBatchReader
	LabelsByStreamID map[int64][]string
}

type CollectSectionsResponse struct {
	SectionsResponse SectionsResponse
}

type ArrowRecordBatchReader interface {
	Read(ctx context.Context) (arrow.RecordBatch, error)
	Close()
}
