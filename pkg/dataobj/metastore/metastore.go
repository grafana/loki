package metastore

import (
	"context"
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

// ResolveStrategyType is the type of metastore resolver to use.
type ResolveStrategyType int

const (
	// ResolveStrategyTypeDirect is the default strategy for resolving data objects.
	// It uses the direct object store paths to resolve log objects.
	ResolveStrategyTypeDirect ResolveStrategyType = iota

	// ResolveStrategyTypeIndex is the strategy for resolving data objects using the index.
	// It uses the index objects to resolve log objects.
	ResolveStrategyTypeIndex
)

func (r ResolveStrategyType) String() string {
	switch r {
	case ResolveStrategyTypeDirect:
		return "direct"
	case ResolveStrategyTypeIndex:
		return "index"
	}
	return "unknown"
}

type Metastore interface {
	// ResolveStrategy returns the type of metastore resolver to use.
	// This is used to determine which strategy to use for resolving log data objects.
	ResolveStrategy(tenants []string) ResolveStrategyType

	// Streams returns all streams corresponding to the given matchers between [start,end]
	Streams(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]*labels.Labels, error)

	// DataObjects returns paths to all matching the given matchers between [start,end]
	// TODO(chaudum); The comment is not correct, because the implementation does not filter by matchers, only by [start, end].
	DataObjects(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error)

	// StreamsIDs returns object store paths and stream IDs for all matching objects for the given matchers between [start,end]
	StreamIDs(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, [][]int64, []int, error)

	// Sections returns a list of SectionDescriptors, including metadata (stream IDs, start & end times, bytes), for the given matchers & predicates between [start,end]
	Sections(ctx context.Context, start, end time.Time, matchers []*labels.Matcher, predicates []*labels.Matcher) ([]*DataobjSectionDescriptor, error)

	// Labels returns all possible labels from matching streams between [start,end]
	Labels(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) // Used to get possible labels for a given stream

	// Values returns all possible values for the given label matchers between [start,end]
	Values(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) // Used to get all values for a given set of label matchers
}
