// Package xcap provides a utility to capture statistical information about the
// lifetime of a query.
//
// Basic usage:
//
//	ctx, capture := xcap.NewCapture(context.Background(), nil)
//	defer capture.End()
//
//	ctx, scope := xcap.StartScope(ctx, "work")
//	defer scope.End()
//
//	scope.Record(bytesRead.Observe(1024))

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

	// scopes are all scopes created within this capture.
	scopes []*Scope

	// ended indicates whether End() has been called.
	ended bool
}

// NewCapture creates a new Capture and attaches it to the provided [context.Context]
func NewCapture(ctx context.Context, attributes []attribute.KeyValue) (context.Context, *Capture) {
	capture := &Capture{
		attributes: attributes,
		scopes:     make([]*Scope, 0),
	}

	ctx = WithCapture(ctx, capture)
	return ctx, capture
}

// End marks the end of the capture. After End is called, no new
// Scopes can be created from this Capture.
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

// GetScope returns the scope with the given name, or nil if no such scope exists.
func (c *Capture) GetScope(name string) *Scope {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, scope := range c.scopes {
		if scope.name == name {
			return scope
		}
	}

	return nil
}

// AddScope adds a scope to this capture. This is called by Scope
// when it is created.
func (c *Capture) AddScope(s *Scope) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ended {
		return
	}

	c.scopes = append(c.scopes, s)
}

// Scopes returns all scopes in this capture.
func (c *Capture) Scopes() []*Scope {
	c.mu.RLock()
	defer c.mu.RUnlock()

	scopes := make([]*Scope, len(c.scopes))
	copy(scopes, c.scopes)
	return scopes
}

// GetAllStatistics returns statistics used across all scopes
// in this capture.
func (c *Capture) GetAllStatistics() []Statistic {
	c.mu.RLock()
	defer c.mu.RUnlock()

	statistics := make(map[string]Statistic)
	for _, scope := range c.scopes {
		scope.mu.RLock()
		for id, obs := range scope.observations {
			// Statistics with the same definition will have the same identifier.
			if _, exists := statistics[id]; !exists {
				statistics[id] = obs.Statistic
			}
		}
		scope.mu.RUnlock()
	}

	result := make([]Statistic, 0, len(statistics))
	for _, stat := range statistics {
		result = append(result, stat)
	}

	return result
}

// Merge appends all scopes from other into this capture.
func (c *Capture) Merge(other *Capture) {
	if other == nil {
		return
	}

	// TODO: This does not merge attributes, handle it if required.

	c.mu.Lock()
	defer c.mu.Unlock()

	c.scopes = append(c.scopes, other.Scopes()...)
}

// NoopCapture is a noop capture that can be used in tests.
var NoopCapture = &Capture{
	attributes: nil,
	scopes:     nil,
	ended:      true, // Already ended so no scopes can be added
}

