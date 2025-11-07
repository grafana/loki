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
