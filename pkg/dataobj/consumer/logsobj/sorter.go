package logsobj

import (
	"context"
	"fmt"
	"io"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

// A Sorter sorts data objects.
type Sorter struct {
	factory  *BuilderFactory
	duration prometheus.Histogram
}

// NewSorter returns a new Sorter.
func NewSorter(factory *BuilderFactory, r prometheus.Registerer) *Sorter {
	return &Sorter{
		factory: factory,
		duration: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_dataobj_sort_duration_seconds",
			Help: "Histogram of time spent sorting data objects",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
	}
}

// Sort takes an existing data object and rewrites the logs sections so they are
// sorted object-wide.
func (s *Sorter) Sort(ctx context.Context, obj *dataobj.Object) (*dataobj.Object, io.Closer, error) {
	b, err := s.factory.NewBuilder(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create builder: %w", err)
	}
	t := prometheus.NewTimer(s.duration)
	defer t.ObserveDuration()
	// Don't need to reset b as it is discarded after each use.
	return b.CopyAndSort(ctx, obj)
}
