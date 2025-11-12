package xcap

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var xcapTracer = otel.Tracer("pkg/xcap")

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
	mu sync.RWMutex

	// capture is the capture this region belongs to.
	capture *Capture

	// name is the name of the region.
	name string

	// attributes are the attributes associated with this region.
	attributes []attribute.KeyValue

	// startTime is when the region was created.
	startTime time.Time

	// endTime is when the region ended. Zero if not ended.
	endTime time.Time

	// observations are all observations recorded in this region.
	// Map from statistic unique identifier to aggregated observation value.
	observations map[string]AggregatedObservation

	// ended indicates whether End() has been called.
	ended bool

	// span is the OpenTelemetry span backing this region.
	span trace.Span
}

// NewRegion creates a new Region with the given data.
// This function is mainly used for unmarshaling from protobuf.
func NewRegion(name string, startTime, endTime time.Time, observations map[string]AggregatedObservation, ended bool, capture *Capture) *Region {
	return &Region{
		capture:      capture,
		name:         name,
		attributes:   nil, // Attributes not available during unmarshaling
		startTime:    startTime,
		endTime:      endTime,
		observations: observations,
		ended:        ended,
	}
}

// StartRegion starts recording the lifetime of a specific operation
// within an overall capture.
//
// It adds the region to the Capture found in the context. If no Capture
// is found, it returns a nil value.
//
// It also creates a corresponding OpenTelemetry span for the region linking it to
// the span in the context.
func StartRegion(ctx context.Context, name string, opts ...RegionOption) *Region {
	capture := FromContext(ctx)
	if capture == nil {
		return nil
	}

	// Apply options
	cfg := &regionConfig{}
	for _, opt := range opts {
		opt.apply(cfg)
	}

	// Create OpenTelemetry span for this region.
	ctx, span := xcapTracer.Start(ctx, name, trace.WithAttributes(cfg.attributes...))

	region := &Region{
		capture:      capture,
		name:         name,
		attributes:   cfg.attributes,
		span:         span,
		startTime:    time.Now(),
		observations: make(map[string]AggregatedObservation),
	}

	// Add region to capture.
	capture.AddRegion(region)
	return region
}

// Record records the statistic Observation o into the region. Calling
// Record multiple times for the same Statistic aggregates values based
// on the aggregation type of the Statistic.
func (r *Region) Record(o Observation) {
	if r.isZero() {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ended {
		return
	}

	key := o.statistic().UniqueIdentifier()
	if _, ok := r.observations[key]; !ok {
		// First observation for this statistic.
		r.observations[key] = AggregatedObservation{
			Statistic: o.statistic(),
			Value:     o.value(),
			Count:     1,
		}
		return
	}

	// Aggregate with existing observations.
	agg := r.observations[key]
	agg.Record(o)
	r.observations[key] = agg
}

// End completes the Region. Updates to the Region are ignored after calling End.
func (r *Region) End() {
	if r.isZero() {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ended {
		return
	}

	r.endTime = time.Now()
	r.ended = true

	// Add observations as span attributes before ending
	if r.span != nil {
		attrs := r.observationsToAttributes()
		if len(attrs) > 0 {
			r.span.SetAttributes(attrs...)
		}
		r.span.End()
	}
}

// Attributes returns the attributes associated with this region.
func (r *Region) Attributes() []attribute.KeyValue {
	if r.isZero() {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	attrs := make([]attribute.KeyValue, len(r.attributes))
	copy(attrs, r.attributes)
	return attrs
}

// Name returns the name of the region.
func (r *Region) Name() string {
	if r.isZero() {
		return ""
	}

	return r.name
}

// StartTime returns when the region was created.
func (r *Region) StartTime() time.Time {
	if r.isZero() {
		return time.Time{}
	}

	return r.startTime
}

// EndTime returns when the region ended, or zero if not ended.
func (r *Region) EndTime() time.Time {
	if r.isZero() {
		return time.Time{}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.endTime
}

// IsEnded returns whether End() has been called on this region.
func (r *Region) IsEnded() bool {
	if r.isZero() {
		return false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.ended
}

// GetObservations returns all observations recorded in this region.
func (r *Region) GetObservations() []AggregatedObservation {
	if r.isZero() {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]AggregatedObservation, 0, len(r.observations))
	for _, obs := range r.observations {
		result = append(result, obs)
	}
	return result
}

// observationsToAttributes converts observations to OpenTelemetry span attributes.
func (r *Region) observationsToAttributes() []attribute.KeyValue {
	if len(r.observations) == 0 {
		return nil
	}

	attrs := make([]attribute.KeyValue, 0, len(r.observations))
	for _, obs := range r.observations {
		attrName := obs.Statistic.Name()
		switch v := obs.Value.(type) {
		case int64:
			attrs = append(attrs, attribute.Int64(attrName, v))
		case float64:
			attrs = append(attrs, attribute.Float64(attrName, v))
		case bool:
			attrs = append(attrs, attribute.Bool(attrName, v))
		case string:
			attrs = append(attrs, attribute.String(attrName, v))
		default:
			// Fallback to string representation
			attrs = append(attrs, attribute.String(attrName, fmt.Sprintf("%v", v)))
		}
	}
	return attrs
}

func (r *Region) isZero() bool {
	return r == nil || r.capture == nil
}

