/*
 *
 * Copyright 2022 gRPC authors.
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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/internal/cache"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
)

// New returns a new xDS client configured by the bootstrap file specified in env
// variable GRPC_XDS_BOOTSTRAP or GRPC_XDS_BOOTSTRAP_CONFIG.
//
// The returned client is a reference counted singleton instance. This function
// creates a new client only when one doesn't already exist.
//
// Note that the first invocation of New() or NewWithConfig() sets the client
// singleton. The following calls will return the singleton client without
// checking or using the config.
func New() (XDSClient, error) {
	return newRefCountedWithConfig(nil)
}

// NewWithConfig returns a new xDS client configured by the given config.
//
// Internal/Testing Only
//
// This function should ONLY be used for internal (c2p resolver) and/or testing
// purposese. DO NOT use this elsewhere. Use New() instead.
func NewWithConfig(config *bootstrap.Config) (XDSClient, error) {
	return newRefCountedWithConfig(config)
}

// newWithConfig returns a new xdsClient with the given config.
func newWithConfig(config *bootstrap.Config, watchExpiryTimeout time.Duration, idleAuthorityDeleteTimeout time.Duration) (*clientImpl, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &clientImpl{
		done:               grpcsync.NewEvent(),
		config:             config,
		watchExpiryTimeout: watchExpiryTimeout,
		serializer:         newCallbackSerializer(ctx),
		serializerClose:    cancel,
		resourceTypes:      newResourceTypeRegistry(),
		authorities:        make(map[string]*authority),
		idleAuthorities:    cache.NewTimeoutCache(idleAuthorityDeleteTimeout),
	}

	c.logger = prefixLogger(c)
	c.logger.Infof("Created ClientConn to xDS management server: %s", config.XDSServer)
	c.logger.Infof("Created")
	return c, nil
}

// NewWithConfigForTesting returns an xDS client for the specified bootstrap
// config, separate from the global singleton.
//
// # Testing Only
//
// This function should ONLY be used for testing purposes.
func NewWithConfigForTesting(config *bootstrap.Config, watchExpiryTimeout, authorityIdleTimeout time.Duration) (XDSClient, error) {
	cl, err := newWithConfig(config, watchExpiryTimeout, authorityIdleTimeout)
	if err != nil {
		return nil, err
	}
	return &clientRefCounted{clientImpl: cl, refCount: 1}, nil
}

// NewWithBootstrapContentsForTesting returns an xDS client for this config,
// separate from the global singleton.
//
// # Testing Only
//
// This function should ONLY be used for testing purposes.
func NewWithBootstrapContentsForTesting(contents []byte) (XDSClient, error) {
	// Normalize the contents
	buf := bytes.Buffer{}
	err := json.Indent(&buf, contents, "", "")
	if err != nil {
		return nil, fmt.Errorf("xds: error normalizing JSON: %v", err)
	}
	contents = bytes.TrimSpace(buf.Bytes())

	clientsMu.Lock()
	defer clientsMu.Unlock()
	if c := clients[string(contents)]; c != nil {
		c.mu.Lock()
		// Since we don't remove the *Client from the map when it is closed, we
		// need to recreate the impl if the ref count dropped to zero.
		if c.refCount > 0 {
			c.refCount++
			c.mu.Unlock()
			return c, nil
		}
		c.mu.Unlock()
	}

	bcfg, err := bootstrap.NewConfigFromContentsForTesting(contents)
	if err != nil {
		return nil, fmt.Errorf("xds: error with bootstrap config: %v", err)
	}

	cImpl, err := newWithConfig(bcfg, defaultWatchExpiryTimeout, defaultIdleAuthorityDeleteTimeout)
	if err != nil {
		return nil, err
	}

	c := &clientRefCounted{clientImpl: cImpl, refCount: 1}
	clients[string(contents)] = c
	return c, nil
}

var (
	clients   = map[string]*clientRefCounted{}
	clientsMu sync.Mutex
)
