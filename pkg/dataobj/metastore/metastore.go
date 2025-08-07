package metastore

import (
	"context"
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

type Metastore interface {
	// Streams returns all streams corresponding to the given matchers between [start,end]
	Streams(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]*labels.Labels, error)

	// DataObjects returns paths to all matching the given matchers between [start,end]
	// TODO(chaudum); The comment is not correct, because the implementation does not filter by matchers, only by [start, end].
	DataObjects(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error)

	// StreamsIDs returns object store paths and stream IDs for all matching objects for the given matchers between [start,end]
	StreamIDs(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, [][]int64, []int, error)

	// StreamsIDsWithSections returns object store paths, stream IDs and section indices for all matching objects for the given matchers between [start,end]
	StreamIDsWithSections(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, [][]int64, [][]int, error)

	// Sections returns a list of SectionDescriptors, including metadata (stream IDs, start & end times, bytes), for the given matchers & predicates between [start,end]
	Sections(ctx context.Context, start, end time.Time, matchers []*labels.Matcher, predicates []*labels.Matcher) ([]*DataobjSectionDescriptor, error)

	// Labels returns all possible labels from matching streams between [start,end]
	Labels(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) // Used to get possible labels for a given stream

	// Values returns all possible values for the given label matchers between [start,end]
	Values(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) // Used to get all values for a given set of label matchers
}
