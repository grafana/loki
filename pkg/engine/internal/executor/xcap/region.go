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

var xcapTracer = otel.Tracer("pkg/engine/internal/executor/xcap")

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
	// Map from statistic name to aggregated observation value.
	observations map[string]observationValue

	// ended indicates whether End() has been called.
	ended bool

	// span is the OpenTelemetry span backing this region.
	span trace.Span
}

// StartRegion starts recording the lifetime of a specific operation
// within an overall capture.
//
// If StartRegion is called from within the context of an existing Region,
// the new Region will be nested inside of the old one.
//
// If StartRegion is called without the context of an existing Capture, a
// no-op Region is returned.
func StartRegion(ctx context.Context, name string, opts ...RegionOption) (context.Context, *Region) {
	capture := FromContext(ctx)
	if capture == nil {
		// Return a no-op region if there's no capture.
		return ctx, &Region{}
	}

	// Apply options
	cfg := &regionConfig{}
	for _, opt := range opts {
		opt.apply(cfg)
	}

	// Create OpenTelemetry span for this region.
	// This automatically handles hierarchy via context propagation.
	var spanAttrs []attribute.KeyValue
	if len(cfg.attributes) > 0 {
		spanAttrs = cfg.attributes
	}
	ctx, span := xcapTracer.Start(ctx, name, trace.WithAttributes(spanAttrs...))

	region := &Region{
		capture:      capture,
		name:         name,
		attributes:   cfg.attributes,
		span:         span,
		startTime:    time.Now(),
		observations: make(map[string]observationValue),
	}

	// Add region to capture.
	capture.addRegion(region)

	ctx = withRegion(ctx, region)
	return ctx, region
}

// Record records the statistic Observation o into the region. Calling
// Record multiple times for an Observation on the same Statistic changes
// behaviour based on the statistic type:
//
//   - Numerical observations will be aggregated together based on the
//     aggregation rules of the statistic.
//   - Flag observations will be overwritten with the most recent value.
func (r *Region) Record(o Observation) {
	if r == nil || r.capture == nil {
		// No-op region, do nothing.
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ended {
		return
	}

	stat := o.statistic()
	statName := stat.Name()

	existing, hasExisting := r.observations[statName]

	if !hasExisting {
		// First observation for this statistic.
		r.observations[statName] = observationValue{
			statistic: stat,
			value:     o.value(),
			count:     1,
		}
		return
	}

	// Aggregate with existing observation.
	r.observations[statName] = aggregateObservation(existing, o)
}

// End completes the Region. Updates to the Region are not permitted
// after calling End.
func (r *Region) End() {
	if r == nil || r.capture == nil {
		// No-op region, do nothing.
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
		obsAttrs := r.observationsToAttributes()
		if len(obsAttrs) > 0 {
			r.span.SetAttributes(obsAttrs...)
		}
		r.span.End()
	}
}

// Name returns the name of the region.
func (r *Region) Name() string {
	if r == nil {
		return ""
	}
	return r.name
}

// Attributes returns the attributes associated with this region.
func (r *Region) Attributes() []attribute.KeyValue {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	attrs := make([]attribute.KeyValue, len(r.attributes))
	copy(attrs, r.attributes)
	return attrs
}

// StartTime returns when the region was created.
func (r *Region) StartTime() time.Time {
	if r == nil {
		return time.Time{}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.startTime
}

// EndTime returns when the region ended, or zero if not ended.
func (r *Region) EndTime() time.Time {
	if r == nil {
		return time.Time{}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.endTime
}

// Duration returns the duration of the region. Returns zero if the
// region has not ended.
func (r *Region) Duration() time.Duration {
	if r == nil {
		return 0
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.endTime.IsZero() {
		return 0
	}

	return r.endTime.Sub(r.startTime)
}

// Observations returns all observations recorded in this region.
func (r *Region) Observations() map[string]interface{} {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]interface{}, len(r.observations))
	for name, obs := range r.observations {
		result[name] = obs.value
	}
	return result
}

// IsEnded returns whether End() has been called on this region.
func (r *Region) IsEnded() bool {
	if r == nil {
		return false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.ended
}

type regionCtxKeyType string

const (
	regionKey regionCtxKeyType = "xcap_region"
)

// withRegion returns a new context with the given Region.
func withRegion(ctx context.Context, region *Region) context.Context {
	return context.WithValue(ctx, regionKey, region)
}

// FromRegionContext returns the Region from the context, or nil if no Region
// is present.
func FromRegionContext(ctx context.Context) *Region {
	v, ok := ctx.Value(regionKey).(*Region)
	if !ok {
		return nil
	}
	return v
}

// NoopRegion is a no-operation region that can be used in tests.
// All methods on NoopRegion are safe to call and do nothing.
var NoopRegion = &Region{
	capture:      nil,
	name:         "",
	attributes:   nil,
	startTime:    time.Time{},
	endTime:      time.Time{},
	observations: nil,
	ended:        true, // Already ended so no observations can be recorded
	span:         nil,
}

// NewNoopRegion returns a no-operation region that can be used in tests.
// All methods on the returned region are safe to call and do nothing.
func NewNoopRegion() *Region {
	return NoopRegion
}

// ObservationDetails holds detailed information about an observation.
type ObservationDetails struct {
	Statistic Statistic
	Value     interface{}
	Count     int
}

// GetObservationDetails returns detailed information about all observations
// recorded in this region.
func (r *Region) GetObservationDetails() map[string]ObservationDetails {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]ObservationDetails, len(r.observations))
	for name, obs := range r.observations {
		result[name] = ObservationDetails{
			Statistic: obs.statistic,
			Value:     obs.value,
			Count:     obs.count,
		}
	}
	return result
}

// observationsToAttributes converts observations to OpenTelemetry span attributes.
func (r *Region) observationsToAttributes() []attribute.KeyValue {
	if r == nil || len(r.observations) == 0 {
		return nil
	}

	attrs := make([]attribute.KeyValue, 0, len(r.observations))
	for name, obs := range r.observations {
		attrName := fmt.Sprintf("xcap.%s", name)
		switch v := obs.value.(type) {
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
