package metastore

import (
	"context"
	"iter"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

type Metastore interface {
	// Sections returns a list of SectionDescriptors, including metadata (stream IDs, start & end times, bytes), for the given matchers & predicates between [start,end].
	// A request with a ShardBucketRange can leave out the streams outside that range.
	Sections(ctx context.Context, req SectionsRequest) (SectionsResponse, error)

	// SectionsStream yields the sections that Sections returns, in batches. It yields a batch when
	// an index object finishes, not after the slowest index object.
	//
	// The resolution does not wait for the caller. It reads every index object, and the batches
	// wait in memory until the caller receives them.
	//
	// Each section appears in at most one batch. A batch is never empty. The order of the batches,
	// and of the sections in a batch, is unspecified.
	//
	// The sequence yields at most one error. The error ends the sequence and comes with a nil
	// batch. The batches before the error hold sections, but not all of them. A caller that ranges
	// over the sequence to its end without an error has received every section.
	//
	// The sequence does no work until the caller ranges over it. Each range runs a new resolution.
	// Breaking out of the loop cancels the reads and waits for them to end.
	//
	// The caller owns each batch slice. The caller must not modify a yielded descriptor, or the
	// slices it holds, until the sequence ends. The sequence still reads them. Reading them from
	// other goroutines is safe.
	SectionsStream(ctx context.Context, req SectionsRequest) iter.Seq2[DataobjSectionDescriptors, error]

	GetIndexes(ctx context.Context, req GetIndexesRequest) (GetIndexesResponse, error)
	IndexSectionsReader(ctx context.Context, req IndexSectionsReaderRequest) (IndexSectionsReaderResponse, error)
	CollectSections(ctx context.Context, req CollectSectionsRequest) (CollectSectionsResponse, error)
}

type SectionsRequest struct {
	Start time.Time
	End   time.Time

	// Matchers select the streams. A request needs at least one, or the resolution fails.
	Matchers   []*labels.Matcher
	Predicates []*labels.Matcher

	// ShardBucketRange, when set, asks the metastore to leave the streams outside the range out
	// of the section descriptors it returns.
	//
	// The index stores one bucket range for each label name and value in a section, not for
	// each stream. The metastore skips a label name and value only when its whole bucket range
	// lies outside the range. A kept one can still list streams outside the range, so the
	// caller must drop them.
	//
	// A resolution can fail when From exceeds To, or when an index object lacks the bucket
	// information to prune by. It can also fail when two index objects list different streams
	// for one section.
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
