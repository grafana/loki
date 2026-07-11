// Package xcap provides a utility to capture statistical information about the
// lifetime of a query.
//
// xcap can be used in two ways, both starting with a [Capture]:
//
// # Standalone: observation aggregation
//
// Use [StartRegion] when you only need observation aggregation without
// creating OTel spans.
//
//	// Create a capture to collect observations.
//	ctx, capture := xcap.NewCapture(ctx, nil)
//	defer capture.End()
//
//	// Create a named region to scope observations.
//	ctx, region := xcap.StartRegion(ctx, "DataObjScan")
//	defer region.End()
//
//	pagesScanned := xcap.NewStatisticInt64("pages.scanned", xcap.AggregationTypeSum)
//
//	// Record observations — multiple calls aggregate by statistic.
//	region.Record(pagesScanned.Observe(1))
//	region.Record(pagesScanned.Observe(1))
//	// pages.scanned is now 2 (sum aggregation)
//
//	// After the capture ends, get the value of a statistic from the capture.
//	capture.Value(pagesScanned) // returns 2
//
// # With OTel tracing: spans creation and observation aggregation
//
// Use [StartSpan] when you also want observations flushed as OTel span
// attributes. [StartSpan] takes a standard [trace.Tracer] and returns
// a [Span] whose End method writes the aggregated observations as span
// attributes before ending the span. The observations are also registered
// with the [Capture] for retrieval after the capture ends.
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
//	pagesScanned := xcap.NewStatisticInt64("pages.scanned", xcap.AggregationTypeSum)
//	region.Record(pagesScanned.Observe(1))
//	region.Record(pagesScanned.Observe(1))
//	// When span.End() is called:
//	//   1. pages.scanned=2 is set as a span attribute.
//	//   2. The observation is also available via SummaryLogValues(capture).
package xcap

import (
	"context"
	"fmt"
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

	// regionByName is a look-up table for regions by name.
	regionByName map[string][]*Region

	// ended indicates whether End() has been called.
	ended bool
}

// NewCapture creates a new Capture and attaches it to the provided [context.Context]
func NewCapture(ctx context.Context, attributes []attribute.KeyValue) (context.Context, *Capture) {
	capture := &Capture{
		attributes:   attributes,
		regions:      make([]*Region, 0),
		regionByName: make(map[string][]*Region),
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

// AddRegion adds a region to this capture. This is called by [StartRegion]
// when a region is created.
func (c *Capture) AddRegion(r *Region) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ended {
		return
	}

	c.regions = append(c.regions, r)
	c.regionByName[r.name] = append(c.regionByName[r.name], r)
}

// Regions returns all regions in this capture.
func (c *Capture) Regions() []*Region {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.regions
}

// Merge incorporates all regions from src into c by accumulating observations
// into matching regions. If a region whose name matches an existing region in
// c is encountered, its observations are folded into that existing region using
// each statistic's aggregation semantics ([AggregatedObservation.Merge]).
// If no region with that name exists in c, a new region is created and
// observations are copied from src.
//
// Unlike a naïve append-based merge, this keeps the number of regions in c
// bounded by the number of unique region names across all merged captures.
func (c *Capture) Merge(parent *Region, src *Capture) {
	if c == nil || src == nil {
		return
	}

	// snapshot src.regions under src's read lock to avoid holding two capture
	// locks simultaneously.
	src.mu.RLock()
	srcRegions := make([]*Region, len(src.regions))
	copy(srcRegions, src.regions)
	src.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ended {
		return
	}

	for _, srcRegion := range srcRegions {
		if dsts := c.regionByName[srcRegion.name]; len(dsts) > 0 {
			// A region with this name already exists: fold observations
			// into it. Lock order: c.mu (held) → srcRegion.mu → dst.mu
			// (inside MergeObservations).
			//
			// We always merge into the last region in the slice, there
			// is no strong reason to prefer the last one, it is just
			// a simple deterministic choice.
			dsts[len(dsts)-1].MergeObservations(srcRegion)
			continue
		}

		// no region with this name exists, create a new one.
		dst := &Region{
			id:           newID(),
			name:         srcRegion.name,
			observations: make(map[StatisticKey]*AggregatedObservation),
		}

		// Parent every merged region onto the provided parent.
		if parent != nil {
			dst.parentID = parent.id
		}
		dst.MergeObservations(srcRegion)

		c.regions = append(c.regions, dst)
		c.regionByName[dst.name] = append(c.regionByName[dst.name], dst)
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

// ValueFromRegion computes the value of a statistic from all regions with the
// given exact name. If the statistic is not present in any matching region,
// ValueFromRegion returns nil.
func (c *Capture) ValueFromRegion(name string, stat Statistic) *AggregatedObservation {
	c.mu.RLock()
	defer c.mu.RUnlock()

	regions := c.regionByName[name]
	if len(regions) == 0 {
		return nil
	}

	key := stat.Key()
	var rolled *AggregatedObservation

	for _, region := range regions {
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

// ValueFromRegion gets a typed value from a capture, aggregating only regions
// whose name exactly matches name. If the statistic is not present in any
// matching region, or the statistic is not of type T, ValueFromRegion returns
// the zero value for T.
func ValueFromRegion[T any](c *Capture, name string, stat Statistic) T {
	rolled := c.ValueFromRegion(name, stat)
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
