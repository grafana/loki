package kv

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// The mockClient does not anything.
// This is used for testing only.
type mockClient struct{}

func buildMockClient(logger log.Logger) (Client, error) {
	level.Warn(logger).Log("msg", "created mockClient for testing only")
	return mockClient{}, nil
}

func (m mockClient) List(_ context.Context, _ string) ([]string, error) {
	return []string{}, nil
}

func (m mockClient) Get(_ context.Context, _ string) (interface{}, error) {
	return "", nil
}

func (m mockClient) Delete(_ context.Context, _ string) error {
	return nil
}

func (m mockClient) CAS(_ context.Context, _ string, _ func(in interface{}) (out interface{}, retry bool, err error)) error {
	return nil
}

func (m mockClient) WatchKey(_ context.Context, _ string, _ func(interface{}) bool) {
}

func (m mockClient) WatchPrefix(_ context.Context, _ string, _ func(string, interface{}) bool) {
}
