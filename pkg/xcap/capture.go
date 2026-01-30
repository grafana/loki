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

// MarshalBinary implements encoding.BinaryMarshaler for Capture.
// It serializes the Capture to its protobuf representation and returns the binary data.
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
