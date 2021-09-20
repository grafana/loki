package configstore

import (
	"context"

	"github.com/grafana/agent/pkg/metrics/instance"
)

// Mock is a Mock Store. Useful primarily for testing.
type Mock struct {
	ListFunc   func(ctx context.Context) ([]string, error)
	GetFunc    func(ctx context.Context, key string) (instance.Config, error)
	PutFunc    func(ctx context.Context, c instance.Config) (created bool, err error)
	DeleteFunc func(ctx context.Context, key string) error
	AllFunc    func(ctx context.Context, keep func(key string) bool) (<-chan instance.Config, error)
	WatchFunc  func() <-chan WatchEvent
	CloseFunc  func() error
}

// List implements Store.
func (s *Mock) List(ctx context.Context) ([]string, error) {
	if s.ListFunc != nil {
		return s.ListFunc(ctx)
	}
	panic("List not implemented")
}

// Get implements Store.
func (s *Mock) Get(ctx context.Context, key string) (instance.Config, error) {
	if s.GetFunc != nil {
		return s.GetFunc(ctx, key)
	}
	panic("Get not implemented")
}

// Put implements Store.
func (s *Mock) Put(ctx context.Context, c instance.Config) (created bool, err error) {
	if s.PutFunc != nil {
		return s.PutFunc(ctx, c)
	}
	panic("Put not implemented")
}

// Delete implements Store.
func (s *Mock) Delete(ctx context.Context, key string) error {
	if s.DeleteFunc != nil {
		return s.DeleteFunc(ctx, key)
	}
	panic("Delete not implemented")
}

// All implements Store.
func (s *Mock) All(ctx context.Context, keep func(key string) bool) (<-chan instance.Config, error) {
	if s.AllFunc != nil {
		return s.AllFunc(ctx, keep)
	}
	panic("All not implemented")
}

// Watch implements Store.
func (s *Mock) Watch() <-chan WatchEvent {
	if s.WatchFunc != nil {
		return s.WatchFunc()
	}
	panic("Watch not implemented")
}

// Close implements Store.
func (s *Mock) Close() error {
	if s.CloseFunc != nil {
		return s.CloseFunc()
	}
	panic("Close not implemented")
}
