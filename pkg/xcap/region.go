package xcap

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// RegionOption applies options to a Region.
type RegionOption interface {
	apply(*regionConfig)
}

type regionConfig struct {
	attributes []attribute.KeyValue
}

type regionOptionFunc func(*regionConfig)

func (f regionOptionFunc) apply(cfg *regionConfig) {
	f(cfg)
}

// WithRegionAttributes adds attributes related to the region.
func WithRegionAttributes(attrs ...attribute.KeyValue) RegionOption {
	return regionOptionFunc(func(cfg *regionConfig) {
		cfg.attributes = append(cfg.attributes, attrs...)
	})
}

// Region captures the lifetime of a specific operation within a capture.
type Region struct {
	// name is the name of the region.
	name string

	// identifier of the region.
	id identifier

	// parentID is the ID of the parent region. Set to zero value if root region.
	parentID identifier

	// attributes are the attributes associated with this region.
	attributes []attribute.KeyValue

	// startTime is when the region was created.
	startTime time.Time

	// mu protects the fields below.
	mu sync.RWMutex

	// endTime is when the region ended. Zero if not ended.
	endTime time.Time

	// observations are all observations recorded in this region.
	// Map from statistic key to aggregated observation value.
	observations map[StatisticKey]*AggregatedObservation

	// ended indicates whether End() has been called.
	ended bool
}

// StartRegion creates a new Region to record observations for a specific operation.
// It returns the new Region and a context containing the Region.
//
// It adds the region to the Capture found in the context. If no Capture
// is found, it returns the original context and a nil region.
//
// If the passed ctx contains a Region, the new Region will be a child of that Region.
func StartRegion(ctx context.Context, name string, opts ...RegionOption) (context.Context, *Region) {
	capture := CaptureFromContext(ctx)
	if capture == nil {
		// TODO: return noop Region instead of nil?
		return ctx, nil
	}

	// Apply options
	cfg := &regionConfig{}
	for _, opt := range opts {
		opt.apply(cfg)
	}

	r := &Region{
		id:           NewID(),
		name:         name,
		attributes:   cfg.attributes,
		startTime:    time.Now(),
		observations: make(map[StatisticKey]*AggregatedObservation),
	}

	// extract parentID from context
	if pr := regionFromContext(ctx); pr != nil {
		r.parentID = pr.id
	}

	// Add region to capture.
	capture.AddRegion(r)

	// Update context with the new region.
	return contextWithRegion(ctx, r), r
}

// Record records the statistic Observation o into the region. Calling
// Record multiple times for the same Statistic aggregates values based
// on the aggregation type of the Statistic.
func (r *Region) Record(o Observation) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ended {
		return
	}

	key := o.statistic().Key()
	if _, ok := r.observations[key]; !ok {
		// First observation for this statistic.
		r.observations[key] = &AggregatedObservation{
			Statistic: o.statistic(),
			Value:     o.value(),
			Count:     1,
		}
		return
	}

	// Aggregate with existing observations.
	agg := r.observations[key]
	agg.Record(o)
}

// End completes the Region. Updates to the Region are ignored after calling End.
func (r *Region) End() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ended {
		return
	}

	r.endTime = time.Now()
	r.ended = true
}

func (r *Region) getAttribute(key string) attribute.KeyValue {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, kv := range r.attributes {
		if string(kv.Key) == key {
			return kv
		}
	}

	return attribute.KeyValue{}
}
