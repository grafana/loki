// Package xcap provides a utility to capture statistical information about the
// lifetime of a query.
//
// Basic usage:
//
//	ctx, capture := xcap.NewCapture(context.Background(), nil)
//	defer capture.End()
//
//	ctx, region := xcap.StartRegion(ctx, "work")
//	defer region.End()
//
//	region.Record(bytesRead.Observe(1024))

package xcap

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
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

	ctx = WithCapture(ctx, capture)
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

// Attributes returns the attributes associated with this capture.
func (c *Capture) Attributes() []attribute.KeyValue {
	c.mu.RLock()
	defer c.mu.RUnlock()

	attrs := make([]attribute.KeyValue, len(c.attributes))
	copy(attrs, c.attributes)
	return attrs
}

// GetRegion returns the region with the given name, or nil if no such region exists.
func (c *Capture) GetRegion(name string) *Region {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, region := range c.regions {
		if region.name == name {
			return region
		}
	}

	return nil
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

	regions := make([]*Region, len(c.regions))
	copy(regions, c.regions)
	return regions
}

// GetAllStatistics returns statistics used across all regions
// in this capture.
func (c *Capture) GetAllStatistics() []Statistic {
	c.mu.RLock()
	defer c.mu.RUnlock()

	statistics := make(map[string]Statistic)
	for _, region := range c.regions {
		region.mu.RLock()
		for id, obs := range region.observations {
			// Statistics with the same definition will have the same identifier.
			if _, exists := statistics[id]; !exists {
				statistics[id] = obs.Statistic
			}
		}
		region.mu.RUnlock()
	}

	result := make([]Statistic, 0, len(statistics))
	for _, stat := range statistics {
		result = append(result, stat)
	}

	return result
}

// Merge appends all regions from other into this capture.
func (c *Capture) Merge(other *Capture) {
	if other == nil {
		return
	}

	// TODO: This does not merge attributes, handle it if required.

	c.mu.Lock()
	defer c.mu.Unlock()

	c.regions = append(c.regions, other.Regions()...)
}

// NoopCapture is a noop capture that can be used in tests.
var NoopCapture = &Capture{
	attributes: nil,
	regions:    nil,
	ended:      true, // Already ended so no regions can be added
}
