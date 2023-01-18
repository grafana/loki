/*
 *
 * Copyright 2020 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package xdsclient

import (
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
)

const (
	defaultWatchExpiryTimeout         = 15 * time.Second
	defaultIdleAuthorityDeleteTimeout = 5 * time.Minute
)

var (
	// This is the client returned by New(). It contains one client implementation,
	// and maintains the refcount.
	singletonClient = &clientRefCounted{}

	// The following functions are no-ops in the actual code, but can be
	// overridden in tests to give them visibility into certain events.
	singletonClientImplCreateHook = func() {}
	singletonClientImplCloseHook  = func() {}
)

// To override in tests.
var bootstrapNewConfig = bootstrap.NewConfig

// onceClosingClient is a thin wrapper around clientRefCounted. The Close()
// method is overridden such that the underlying reference counted client's
// Close() is called at most once, thereby making Close() idempotent.
//
// This is the type which is returned by New() and NewWithConfig(), making it
// safe for these callers to call Close() any number of times.
type onceClosingClient struct {
	XDSClient

	once sync.Once
}

func (o *onceClosingClient) Close() {
	o.once.Do(o.XDSClient.Close)
}

func newRefCountedWithConfig(config *bootstrap.Config) (XDSClient, error) {
	singletonClient.mu.Lock()
	defer singletonClient.mu.Unlock()

	// If the client implementation was created, increment ref count and return
	// the client.
	if singletonClient.clientImpl != nil {
		singletonClient.refCount++
		return &onceClosingClient{XDSClient: singletonClient}, nil
	}

	// If the passed in config is nil, perform bootstrap to read config.
	if config == nil {
		var err error
		config, err = bootstrapNewConfig()
		if err != nil {
			return nil, fmt.Errorf("xds: failed to read bootstrap file: %v", err)
		}
	}

	// Create the new client implementation.
	c, err := newWithConfig(config, defaultWatchExpiryTimeout, defaultIdleAuthorityDeleteTimeout)
	if err != nil {
		return nil, err
	}

	singletonClient.clientImpl = c
	singletonClient.refCount++
	singletonClientImplCreateHook()
	return &onceClosingClient{XDSClient: singletonClient}, nil
}

// clientRefCounted is ref-counted, and to be shared by the xds resolver and
// balancer implementations, across multiple ClientConns and Servers.
type clientRefCounted struct {
	*clientImpl

	// This mu protects all the fields, including the embedded clientImpl above.
	mu       sync.Mutex
	refCount int
}

// Close closes the client. It does ref count of the xds client implementation,
// and closes the gRPC connection to the management server when ref count
// reaches 0.
func (c *clientRefCounted) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refCount--
	if c.refCount == 0 {
		c.clientImpl.Close()
		// Set clientImpl back to nil. So if New() is called after this, a new
		// implementation will be created.
		c.clientImpl = nil
		singletonClientImplCloseHook()
	}
}
