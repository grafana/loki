package kv

import (
	"context"

	"github.com/go-kit/kit/log/level"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
)

// The mockClient does not anything.
// This is used for testing only.
type mockClient struct{}

func buildMockClient() (Client, error) {
	level.Warn(util_log.Logger).Log("msg", "created mockClient for testing only")
	return mockClient{}, nil
}

func (m mockClient) List(ctx context.Context, prefix string) ([]string, error) {
	return []string{}, nil
}

func (m mockClient) Get(ctx context.Context, key string) (interface{}, error) {
	return "", nil
}

func (m mockClient) Delete(ctx context.Context, key string) error {
	return nil
}

func (m mockClient) CAS(ctx context.Context, key string, f func(in interface{}) (out interface{}, retry bool, err error)) error {
	return nil
}

func (m mockClient) WatchKey(ctx context.Context, key string, f func(interface{}) bool) {
}

func (m mockClient) WatchPrefix(ctx context.Context, prefix string, f func(string, interface{}) bool) {
}
