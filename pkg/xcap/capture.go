// Package xcap provides a utility to capture statistical information about the
// lifetime of a query.
//
// xcap can be used in two ways, both starting with a [Capture]:
//
// # Standalone: regions and log summaries
//
// Use [StartRegion] when you only need observation aggregation and
// structured log output, without creating OTel spans.
//
//	// Create a capture to collect observations.
//	ctx, capture := xcap.NewCapture(ctx, nil)
//	defer capture.End()
//
//	// Create a named region to scope observations.
//	ctx, region := xcap.StartRegion(ctx, "DataObjScan")
//	defer region.End()
//
//	// Record observations — multiple calls aggregate by statistic.
//	region.Record(xcap.StatDatasetPagesScanned.Observe(1))
//	region.Record(xcap.StatDatasetPagesScanned.Observe(1))
//	// pages.scanned is now 2 (sum aggregation)
//
//	// After the capture ends, summarise all observations as log values.
//	logValues := xcap.SummaryLogValues(capture)
//	level.Info(logger).Log(logValues...)
//
// # With OTel tracing: spans, observations and log summaries
//
// Use [StartSpan] when you want observations flushed as OTel span
// attributes in addition to the log summary. [StartSpan] takes a
// standard [trace.Tracer] and returns a [Span] whose End method writes
// the aggregated observations as span attributes before ending the
// span. The observations are also registered with the [Capture] for
// log summaries.
//
//	// Create a capture to collect observations.
//	ctx, capture := xcap.NewCapture(ctx, nil)
//	defer capture.End()
//
//	// Start a span — this also creates a linked region.
//	ctx, span := xcap.StartSpan(ctx, otel.Tracer("engine"), "DataObjScan",
//	    trace.WithAttributes(attribute.Int("num_targets", 5)),
//	)
//	defer span.End()
//
//	// Deep in the call stack, retrieve the region from context.
//	region := xcap.RegionFromContext(ctx)
//	region.Record(xcap.StatDatasetPagesScanned.Observe(1))
//	region.Record(xcap.StatDatasetPagesScanned.Observe(1))
//	// When span.End() is called:
//	//   1. pages.scanned=2 is set as a span attribute.
//	//   2. The observation is also available via SummaryLogValues(capture).
package xcap

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/gogo/protobuf/proto"
	"go.opentelemetry.io/otel/attribute"

	internal "github.com/grafana/loki/v3/pkg/xcap/internal/proto"
)

// Capture captures statistical information about the lifetime of a query.
type Capture struct {
	mu sync.RWMutex

	// attributes are the attributes associated with this capture.
	attributes []attribute.KeyValue

	// regions are all regions created within this capture.
	regions []*Region

	// ended indicates whether End() has been called.
	ended bool
}

// NewCapture creates a new Capture and attaches it to the provided [context.Context]
func NewCapture(ctx context.Context, attributes []attribute.KeyValue) (context.Context, *Capture) {
	capture := &Capture{
		attributes: attributes,
		regions:    make([]*Region, 0),
	}

	ctx = contextWithCapture(ctx, capture)
	return ctx, capture
}

// End marks the end of the capture. After End is called, no new
// Regions can be created from this Capture.
func (c *Capture) End() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ended {
		return
	}

	c.ended = true
}

// AddRegion adds a region to this capture. This is called by Region
// when it is created.
func (c *Capture) AddRegion(r *Region) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ended {
		return
	}

	c.regions = append(c.regions, r)
}

// Regions returns all regions in this capture.
func (c *Capture) Regions() []*Region {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.regions
}

// LinkParent assigns the provided region as the parent
// to all root regions of the capture.
func (c *Capture) LinkParent(parent *Region) {
	c.mu.RLock()
	regions := make([]*Region, len(c.regions))
	copy(regions, c.regions)
	c.mu.RUnlock()

	for _, region := range regions {
		region.mu.Lock()
		if region.parentID.IsZero() {
			region.parentID = parent.id
		}
		region.mu.Unlock()
	}
}

// Merge incorporates all regions from src into c. If parent is non-nil, the
// root regions of src are re-parented onto it before being added to c,
// preserving the parent/child relationships when src's data is presented as
// part of c's hierarchy.
//
// Regions are shared by reference between c and src after Merge returns.
//
// Merge is a no-op if c or src is nil, or if c has already been ended.
func (c *Capture) Merge(parent *Region, src *Capture) {
	if c == nil || src == nil {
		return
	}

	if parent != nil {
		src.LinkParent(parent)
	}

	for _, region := range src.Regions() {
		c.AddRegion(region)
	}
}

// getAllStatistics returns statistics used across all regions
// in this capture.
func (c *Capture) getAllStatistics() map[StatisticKey]Statistic {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := make(map[StatisticKey]Statistic)
	for _, region := range c.regions {
		region.mu.RLock()
		for key, obs := range region.observations {
			if _, exists := stats[key]; !exists {
				stats[key] = obs.Statistic
			}
		}
		region.mu.RUnlock()
	}
	return stats
}

// MarshalBinary implements encoding.BinaryMarshaler for Capture.
// It serializes the Capture to its protobuf representation and returns
// the binary data.
func (c *Capture) MarshalBinary() ([]byte, error) {
	if c == nil {
		return nil, nil
	}

	protoCapture, err := toProtoCapture(c)
	if err != nil {
		return nil, fmt.Errorf("failed to convert capture to proto: %w", err)
	}

	if protoCapture == nil {
		return nil, nil
	}

	data, err := proto.Marshal(protoCapture)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal proto capture: %w", err)
	}

	return data, nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler for Capture.
// It deserializes binary data into a Capture from its protobuf representation.
func (c *Capture) UnmarshalBinary(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	protoCapture := &internal.Capture{}
	if err := proto.Unmarshal(data, protoCapture); err != nil {
		return fmt.Errorf("failed to unmarshal proto capture: %w", err)
	}

	err := fromProtoCapture(protoCapture, c)
	if err != nil {
		return fmt.Errorf("failed to convert proto to capture: %w", err)
	}

	return nil
}

// Value computes the value of a statistic from the capture, rolling up from all
// regions. If the statistic is not present in any region, Value returns nil.
func (c *Capture) Value(stat Statistic) *AggregatedObservation {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := stat.Key()
	var rolled *AggregatedObservation

	for _, region := range c.regions {
		region.mu.RLock()
		obs, ok := region.observations[key]
		if !ok {
			region.mu.RUnlock()
			continue
		}
		if rolled == nil {
			rolled = &AggregatedObservation{
				Statistic: obs.Statistic,
				Value:     obs.Value,
				Count:     obs.Count,
			}
		} else {
			rolled.Merge(obs)
		}
		region.mu.RUnlock()
	}

	return rolled
}

// ValueFromRegion computes the value of a statistic from regions with
// names that start with prefix. If the statistic is not present in any matching
// region, ValueFromRegion returns nil.
func (c *Capture) ValueFromRegion(prefix string, stat Statistic) *AggregatedObservation {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := stat.Key()
	var rolled *AggregatedObservation

	for _, region := range c.regions {
		if !strings.HasPrefix(region.name, prefix) {
			continue
		}

		region.mu.RLock()
		obs, ok := region.observations[key]
		if !ok {
			region.mu.RUnlock()
			continue
		}
		if rolled == nil {
			rolled = &AggregatedObservation{
				Statistic: obs.Statistic,
				Value:     obs.Value,
				Count:     obs.Count,
			}
		} else {
			rolled.Merge(obs)
		}
		region.mu.RUnlock()
	}

	return rolled
}

// Value gets a typed value from a capture. If the statistic it not present in
// any region from the capture, or the statistic is not of type T, Value returns
// the zero value for T.
//
// Use [TryValue] if you need to distinguish between a missing statistic and a
// zero value.
func Value[T any](c *Capture, stat Statistic) T {
	v, _ := TryValue[T](c, stat)
	return v
}

// ValueFromRegion gets a typed value from a capture, aggregating only
// regions with names that start with prefix. If the statistic is not present in
// any matching region, or the statistic is not of type T, ValueFromRegion
// returns the zero value for T.
func ValueFromRegion[T any](c *Capture, prefix string, stat Statistic) T {
	rolled := c.ValueFromRegion(prefix, stat)
	if rolled == nil {
		var zero T
		return zero
	}

	val, ok := rolled.Value.(T)
	if !ok {
		var zero T
		return zero
	}
	return val
}

// TryValue gets a typed value from a capture, returning both the value and a
// boolean indicating whether the statistic was present in the capture and
// matching type T.
func TryValue[T any](c *Capture, stat Statistic) (T, bool) {
	rolled := c.Value(stat)
	if rolled == nil {
		var zero T
		return zero, false
	}

	val, ok := rolled.Value.(T)
	return val, ok
}
