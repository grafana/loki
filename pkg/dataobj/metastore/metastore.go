package metastore

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

type Metastore interface {
	// Sections returns a list of SectionDescriptors, including metadata (stream IDs, start & end times, bytes), for the given matchers & predicates between [start,end].
	// A request with a ShardBucketRange can leave out the streams outside that range.
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

	// ShardBucketRange, when set, asks the metastore to leave out streams outside the range.
	//
	// The response holds every matching stream inside the range. It can also hold streams outside
	// it, because the metastore prunes whole index rows by their bucket ranges, not single
	// streams. The caller must drop the streams outside the range.
	//
	// Sections can fail when From exceeds To, or when an index object lacks the bucket
	// information to prune by. It can also fail when two index objects list different streams
	// for one section, as when only one of them can prune by the range.
	ShardBucketRange *postings.ShardBucketRange
}

type SectionsResponse struct {
	Sections DataobjSectionDescriptors
}

type GetIndexesRequest struct {
	Start time.Time
	End   time.Time
}

// IndexEntry is a reference to an index object together with the time range it covers.
type IndexEntry struct {
	Path  string
	Start time.Time
	End   time.Time
}

type GetIndexesResponse struct {
	TableOfContentsPaths []string
	Indexes              []IndexEntry
}

type IndexSectionsReaderRequest struct {
	IndexPath       string
	SectionsRequest SectionsRequest
	PrefetchBytes   int64
	BatchSize       int
}

type IndexSectionsReaderResponse struct {
	Reader ArrowRecordBatchReader
}

type CollectSectionsRequest struct {
	Reader ArrowRecordBatchReader
}

type CollectSectionsResponse struct {
	SectionsResponse SectionsResponse
}

type ArrowRecordBatchReader interface {
	Open(ctx context.Context) error
	Read(ctx context.Context) (arrow.RecordBatch, error)
	Close()
}
