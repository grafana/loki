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

// Merge appends all regions from other into this capture and establishes
// parent-child relationships. Leaf regions (regions with no children) in other
// are linked to root regions (regions with no parent) in this capture via parentID.
// This is used to link regions from different tasks in a workflow DAG.
// If the parent capture has multiple root regions, the first one (by order) is used.
func (c *Capture) Merge(other *Capture) {
	if other == nil {
		return
	}

	// TODO: This does not merge attributes, handle it if required.

	c.mu.Lock()
	defer c.mu.Unlock()

	other.mu.RLock()
	otherRegions := make([]*Region, len(other.regions))
	copy(otherRegions, other.regions)
	other.mu.RUnlock()

	if len(otherRegions) == 0 {
		return
	}

	// Find root regions (regions with no parent) in this capture.
	rootRegions := make([]*Region, 0)
	for _, r := range c.regions {
		r.mu.RLock()
		isRoot := r.parentID.IsZero()
		r.mu.RUnlock()
		if isRoot {
			rootRegions = append(rootRegions, r)
		}
	}

	// Find leaf regions (regions with no children) in other capture.
	// A region is a leaf if no other region has it as a parent.
	parentIDs := make(map[ID]bool)
	for _, r := range otherRegions {
		r.mu.RLock()
		parentID := r.parentID
		r.mu.RUnlock()
		if !parentID.IsZero() {
			parentIDs[parentID] = true
		}
	}

	leafRegions := make([]*Region, 0)
	for _, r := range otherRegions {
		r.mu.RLock()
		regionID := r.id
		r.mu.RUnlock()
		if !parentIDs[regionID] {
			leafRegions = append(leafRegions, r)
		}
	}

	// Link leaf regions from other to root regions in this capture via parentID.
	// If there are multiple root regions, use the first one (deterministic choice).
	var parentRootID ID
	if len(rootRegions) > 0 {
		rootRegions[0].mu.RLock()
		parentRootID = rootRegions[0].id
		rootRegions[0].mu.RUnlock()
	}

	// Update parentID of leaf regions to point to the parent root region.
	for _, leaf := range leafRegions {
		if !parentRootID.IsZero() {
			leaf.mu.Lock()
			leaf.parentID = parentRootID
			leaf.mu.Unlock()
		}
	}

	// Append all regions from other capture.
	c.regions = append(c.regions, otherRegions...)
}
