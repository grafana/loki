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
	DataObjects(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error)

	// Labels returns all possible labels from matching streams between [start,end]
	Labels(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) // Used to get possible labels for a given stream

	// Values returns all possible values for the given label matchers between [start,end]
	Values(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) // Used to get all values for a given set of label matchers
}
