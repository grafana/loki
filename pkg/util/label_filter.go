package util

import "context"

// ChunkFilterer filters chunks based on the metric.
type LabelValueFilterer interface {
	Filter(ctx context.Context, labelName string, labelValues []string) ([]string, error)
}
