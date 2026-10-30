package metastore

import (
	"context"
	"iter"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v4/pkg/dataobj/sections/streams"
)

type Metastore interface {
	// Sections returns a list of SectionDescriptors, including metadata (stream IDs, start & end times, bytes), for the given matchers & predicates between [start,end].
	// A request with a StreamHashRange can leave out the streams outside that range.
	Sections(ctx context.Context, req SectionsRequest) (SectionsResponse, error)

	// SectionsStream yields the sections that Sections returns, in batches. It yields the sections
	// of an index object when its read ends, without waiting for the slowest one.
	//
	// The reads do not wait for the caller. The batches wait in memory until the caller receives
	// them.
	//
	// Each section appears in at most one batch. A batch holds the sections of one index object
	// that no earlier batch listed. A batch is never empty, so an index object that lists no new
	// section yields no batch. The sections of one data object can span several batches. The order
	// of the batches, and of the sections in a batch, is unspecified.
	//
	// The sequence yields at most one error. The error ends the sequence and comes with a nil
	// batch. The batches before the error hold only some of the sections. A caller that ranges
	// over the sequence to its end without an error has received every section.
	//
	// The sequence does no work until the caller ranges over it. Each range runs a new resolution.
	// Breaking out of the loop cancels the reads and waits for them to end.
	//
	// The caller owns each batch slice and may modify it. The caller must not modify a yielded
	// descriptor, including its StreamIDs and AmbiguousPredicates, until the sequence ends. The
	// sequence still reads the descriptors. Reading them from other goroutines is safe.
	SectionsStream(ctx context.Context, req SectionsRequest) iter.Seq2[DataobjSectionDescriptors, error]

	GetIndexes(ctx context.Context, req GetIndexesRequest) (GetIndexesResponse, error)
	IndexSectionsReader(ctx context.Context, req IndexSectionsReaderRequest) (IndexSectionsReaderResponse, error)
	CollectSections(ctx context.Context, req CollectSectionsRequest) (CollectSectionsResponse, error)
}

type SectionsRequest struct {
	Start time.Time
	End   time.Time

	// Matchers select the streams. A request needs at least one, or the resolution fails.
	Matchers []*labels.Matcher

	// Predicates are matchers on a stream label name or a structured metadata key. The metastore
	// may prune a section that holds no row with the value of an equality predicate. A predicate
	// of any other type prunes no section.
	Predicates []*labels.Matcher

	// StreamHashRange, when set, asks the metastore to omit streams whose hash lies outside the
	// range. From must not exceed To.
	//
	// The range is on stream hashes, not shard buckets, so it applies to data objects of any shard
	// layout.
	//
	// The metastore can still list streams outside the range. The caller must drop them.
	//
	// If the metastore cannot apply the range to an index object, the resolution returns an error.
	// It does not fall back to the unpruned streams of that object.
	StreamHashRange *streams.HashRange
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
	// Reader must emit the batches of IndexSectionsReader.
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
