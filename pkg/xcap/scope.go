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

// ScopeOption applies options to a Scope.
type ScopeOption interface {
	apply(*scopeConfig)
}

type scopeConfig struct {
	attributes []attribute.KeyValue
}

type scopeOptionFunc func(*scopeConfig)

func (f scopeOptionFunc) apply(cfg *scopeConfig) {
	f(cfg)
}

// WithScopeAttributes adds attributes related to the scope.
func WithScopeAttributes(attrs ...attribute.KeyValue) ScopeOption {
	return scopeOptionFunc(func(cfg *scopeConfig) {
		cfg.attributes = append(cfg.attributes, attrs...)
	})
}

// Scope captures the lifetime of a specific operation within a capture.
type Scope struct {
	mu sync.RWMutex

	// capture is the capture this scope belongs to.
	capture *Capture

	// name is the name of the scope.
	name string

	// attributes are the attributes associated with this scope.
	attributes []attribute.KeyValue

	// startTime is when the scope was created.
	startTime time.Time

	// endTime is when the scope ended. Zero if not ended.
	endTime time.Time

	// observations are all observations recorded in this scope.
	// Map from statistic unique identifier to aggregated observation value.
	observations map[string]AggregatedObservation

	// ended indicates whether End() has been called.
	ended bool

	// span is the OpenTelemetry span backing this scope.
	span trace.Span
}

// NewScope creates a new Scope with the given data.
// This function is mainly used for unmarshaling from protobuf.
func NewScope(name string, startTime, endTime time.Time, observations map[string]AggregatedObservation, ended bool, capture *Capture) *Scope {
	return &Scope{
		capture:      capture,
		name:         name,
		attributes:   nil, // Attributes not available during unmarshaling
		startTime:    startTime,
		endTime:      endTime,
		observations: observations,
		ended:        ended,
	}
}

// StartScope starts recording the lifetime of a specific operation
// within an overall capture.
//
// It adds the scope to the Capture found in the context. If no Capture
// is found, it returns a nil value.
//
// It also creates a corresponding OpenTelemetry span for the scope linking it to
// the span in the context.
func StartScope(ctx context.Context, name string, opts ...ScopeOption) *Scope {
	capture := FromContext(ctx)
	if capture == nil {
		return nil
	}

	// Apply options
	cfg := &scopeConfig{}
	for _, opt := range opts {
		opt.apply(cfg)
	}

	// Create OpenTelemetry span for this scope.
	ctx, span := xcapTracer.Start(ctx, name, trace.WithAttributes(cfg.attributes...))

	scope := &Scope{
		capture:      capture,
		name:         name,
		attributes:   cfg.attributes,
		span:         span,
		startTime:    time.Now(),
		observations: make(map[string]AggregatedObservation),
	}

	// Add scope to capture.
	capture.AddScope(scope)
	return scope
}

// Record records the statistic Observation o into the scope. Calling
// Record multiple times for the same Statistic aggregates values based
// on the aggregation type of the Statistic.
func (s *Scope) Record(o Observation) {
	if s.isZero() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		return
	}

	key := o.statistic().UniqueIdentifier()
	if _, ok := s.observations[key]; !ok {
		// First observation for this statistic.
		s.observations[key] = AggregatedObservation{
			Statistic: o.statistic(),
			Value:     o.value(),
			Count:     1,
		}
		return
	}

	// Aggregate with existing observations.
	agg := s.observations[key]
	agg.Record(o)
	s.observations[key] = agg
}

// End completes the Scope. Updates to the Scope are ignored after calling End.
func (s *Scope) End() {
	if s.isZero() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		return
	}

	s.endTime = time.Now()
	s.ended = true

	// Add observations as span attributes before ending
	if s.span != nil {
		attrs := s.observationsToAttributes()
		if len(attrs) > 0 {
			s.span.SetAttributes(attrs...)
		}
		s.span.End()
	}
}

// Attributes returns the attributes associated with this scope.
func (s *Scope) Attributes() []attribute.KeyValue {
	if s.isZero() {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	attrs := make([]attribute.KeyValue, len(s.attributes))
	copy(attrs, s.attributes)
	return attrs
}

// Name returns the name of the scope.
func (s *Scope) Name() string {
	if s.isZero() {
		return ""
	}

	return s.name
}

// StartTime returns when the scope was created.
func (s *Scope) StartTime() time.Time {
	if s.isZero() {
		return time.Time{}
	}

	return s.startTime
}

// EndTime returns when the scope ended, or zero if not ended.
func (s *Scope) EndTime() time.Time {
	if s.isZero() {
		return time.Time{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.endTime
}

// IsEnded returns whether End() has been called on this scope.
func (s *Scope) IsEnded() bool {
	if s.isZero() {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.ended
}

// GetObservations returns all observations recorded in this scope.
func (s *Scope) GetObservations() []AggregatedObservation {
	if s.isZero() {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]AggregatedObservation, 0, len(s.observations))
	for _, obs := range s.observations {
		result = append(result, obs)
	}
	return result
}

// observationsToAttributes converts observations to OpenTelemetry span attributes.
func (s *Scope) observationsToAttributes() []attribute.KeyValue {
	if len(s.observations) == 0 {
		return nil
	}

	attrs := make([]attribute.KeyValue, 0, len(s.observations))
	for _, obs := range s.observations {
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

func (s *Scope) isZero() bool {
	return s == nil || s.capture == nil
}

