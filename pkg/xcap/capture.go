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

func (c *Capture) Regions() []*Region {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.regions
}

// GetAllStatistics returns statistics used across all regions
// in this capture.
func (c *Capture) getAllStatistics() []Statistic {
	c.mu.RLock()
	defer c.mu.RUnlock()

	statistics := make(map[StatisticKey]Statistic)
	for _, region := range c.regions {
		region.mu.RLock()
		for key, obs := range region.observations {
			// Statistics with the same definition will have the same key.
			if _, exists := statistics[key]; !exists {
				statistics[key] = obs.Statistic
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

// MergeCaptures returns a new [Capture] by merging all regions from the input captures.
//
// If linkByAttribute is non-empty, MergeCaptures will establish parent-child relationships
// between regions that don't already have a parent. For each region without a parent:
//   - It extracts the value of the linkByAttribute (must be a string attribute)
//   - It calls resolveParent() with that value to determine the parent's attribute value
//   - It finds the region with the matching attribute value and sets it as the parent
//
// Use a linkByAttribute that is unique for each region.
func (c *Capture) LinkRegions(linkByAttribute string, resolveParent func(string) (string, bool)) {
	if linkByAttribute == "" {
		return
	}

	getAttributeValue := func(r *Region) (string, bool) {
		if attr := r.getAttribute(linkByAttribute); attr.Valid() && attr.Value.Type() == attribute.STRING {
			return attr.Value.AsString(), true
		}

		return "", false
	}

	attrToRegion := make(map[string]*Region, len(c.regions))
	for _, r := range c.regions {
		// regions without the link attribute are not added to the map.
		if val, ok := getAttributeValue(r); ok {
			attrToRegion[val] = r
		}
	}

	for _, r := range c.regions {
		if !r.parentID.IsZero() {
			// region already has a parent. No linking required.
			continue
		}

		attrVal, ok := getAttributeValue(r)
		if !ok {
			continue
		}

		parentAttrVal, ok := resolveParent(attrVal)
		if !ok {
			continue
		}

		parentRegion, ok := attrToRegion[parentAttrVal]
		if !ok {
			continue
		}

		r.parentID = parentRegion.id
	}
}
