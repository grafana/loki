package xcap

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// Capture captures the lifetime of a query.
type Capture struct {
	mu sync.RWMutex

	// attributes are the attributes associated with this capture.
	attributes []attribute.KeyValue

	// startTime is when the capture was created.
	startTime time.Time

	// regions are all regions created within this capture.
	regions []*Region

	// ended indicates whether End() has been called.
	ended bool
}

// NewCapture creates a new Capture and attaches it to the provided [context.Context]
//
// If NewCapture is called from within the context of an existing Capture,
// a link to the parent Capture will be created but the two captures are
// otherwise treated as separate.
func NewCapture(ctx context.Context, attributes []attribute.KeyValue) (context.Context, *Capture) {
	capture := &Capture{
		attributes: attributes,
		startTime:  time.Now(),
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

// StartTime returns when the capture was created.
func (c *Capture) StartTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.startTime
}

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

// addRegion adds a region to this capture. This is called by Region
// when it is created.
func (c *Capture) addRegion(r *Region) {
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

// IsEnded returns whether End() has been called on this capture.
func (c *Capture) IsEnded() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.ended
}

// GetAllStatistics returns all unique statistics used across all regions
// in this capture. Statistics are deduplicated by name.
func (c *Capture) GetAllStatistics() []Statistic {
	c.mu.RLock()
	defer c.mu.RUnlock()

	statMap := make(map[string]Statistic)
	for _, region := range c.regions {
		region.mu.RLock()
		for _, obs := range region.observations {
			stat := obs.statistic
			statName := stat.Name()
			// Deduplicate by name - first occurrence wins
			if _, exists := statMap[statName]; !exists {
				statMap[statName] = stat
			}
		}
		region.mu.RUnlock()
	}

	result := make([]Statistic, 0, len(statMap))
	for _, stat := range statMap {
		result = append(result, stat)
	}
	return result
}

// Merge appends all regions from other into this capture. Regions are appended,
// not merged. If this capture has been ended, Merge does nothing.
func (c *Capture) Merge(other *Capture) {
	if other == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Get regions from other capture while holding its read lock
	other.mu.RLock()
	otherRegions := make([]*Region, len(other.regions))
	copy(otherRegions, other.regions)
	other.mu.RUnlock()

	// Append regions to this capture
	c.regions = append(c.regions, otherRegions...)
}

// NoopCapture is a no-operation capture that can be used in tests.
// All methods on NoopCapture are safe to call and do nothing.
var NoopCapture = &Capture{
	attributes: nil,
	startTime:  time.Time{},
	regions:    nil,
	ended:      true, // Already ended so no regions can be added
}

// NewNoopCapture returns a no-operation capture that can be used in tests.
// All methods on the returned capture are safe to call and do nothing.
func NewNoopCapture() *Capture {
	return NoopCapture
}
