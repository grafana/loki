package metastore

import (
	"context"
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

type StreamMeta struct {
	Labels *labels.Labels
}

type DataObjectMeta struct {
	Path string
}

type Metastore interface {
	// Streams returns all streams corresponding to the given matchers between [start,end]
	Streams(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]StreamMeta, error)

	// DataObjects returns metadata about all dataobjects matching the given matchers between [start,end]
	DataObjects(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]DataObjectMeta, error)

	// Labels returns all possible labels from matching streams between [start,end]
	Labels(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) // Used to get possible labels for a given stream

	// Values returns all possible values for the given label matchers between [start,end]
	Values(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) // Used to get all values for a given set of label matchers
}
