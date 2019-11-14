package kv

import (
	"context"
	"fmt"
	"strings"
)

type prefixedKVClient struct {
	prefix string
	client Client
}

// PrefixClient takes a KVClient and forces a prefix on all its operations.
func PrefixClient(client Client, prefix string) Client {
	return &prefixedKVClient{prefix, client}
}

// CAS atomically modifies a value in a callback. If the value doesn't exist,
// you'll get 'nil' as an argument to your callback.
func (c *prefixedKVClient) CAS(ctx context.Context, key string, f func(in interface{}) (out interface{}, retry bool, err error)) error {
	return c.client.CAS(ctx, c.prefix+key, f)
}

// WatchKey watches a key.
func (c *prefixedKVClient) WatchKey(ctx context.Context, key string, f func(interface{}) bool) {
	c.client.WatchKey(ctx, c.prefix+key, f)
}

// WatchPrefix watches a prefix. For a prefix client it appends the prefix argument to the clients prefix.
func (c *prefixedKVClient) WatchPrefix(ctx context.Context, prefix string, f func(string, interface{}) bool) {
	c.client.WatchPrefix(ctx, fmt.Sprintf("%s%s", c.prefix, prefix), func(k string, i interface{}) bool {
		return f(strings.TrimPrefix(k, c.prefix), i)
	})
}

func (c *prefixedKVClient) Get(ctx context.Context, key string) (interface{}, error) {
	return c.client.Get(ctx, c.prefix+key)
}

func (c *prefixedKVClient) Stop() {
	c.client.Stop()
}
