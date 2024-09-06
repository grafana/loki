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

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/cache"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// New returns a new xDS client configured by the bootstrap file specified in env
// variable GRPC_XDS_BOOTSTRAP or GRPC_XDS_BOOTSTRAP_CONFIG.
//
// The returned client is a reference counted singleton instance. This function
// creates a new client only when one doesn't already exist.
//
// The second return value represents a close function which releases the
// caller's reference on the returned client.  The caller is expected to invoke
// it once they are done using the client. The underlying client will be closed
// only when all references are released, and it is safe for the caller to
// invoke this close function multiple times.
func New() (XDSClient, func(), error) {
	return newRefCountedWithConfig(nil)
}

// NewWithConfig is similar to New, except that it uses the provided bootstrap
// configuration to create the xDS client if and only if the bootstrap
// environment variables are not defined.
//
// The returned client is a reference counted singleton instance. This function
// creates a new client only when one doesn't already exist.
//
// The second return value represents a close function which releases the
// caller's reference on the returned client.  The caller is expected to invoke
// it once they are done using the client. The underlying client will be closed
// only when all references are released, and it is safe for the caller to
// invoke this close function multiple times.
//
// # Internal Only
//
// This function should ONLY be used by the internal google-c2p resolver.
// DO NOT use this elsewhere. Use New() instead.
func NewWithConfig(config *bootstrap.Config) (XDSClient, func(), error) {
	return newRefCountedWithConfig(config)
}

// newWithConfig returns a new xdsClient with the given config.
func newWithConfig(config *bootstrap.Config, watchExpiryTimeout time.Duration, idleAuthorityDeleteTimeout time.Duration) (*clientImpl, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &clientImpl{
		done:               grpcsync.NewEvent(),
		config:             config,
		watchExpiryTimeout: watchExpiryTimeout,
		serializer:         grpcsync.NewCallbackSerializer(ctx),
		serializerClose:    cancel,
		resourceTypes:      newResourceTypeRegistry(),
		authorities:        make(map[string]*authority),
		idleAuthorities:    cache.NewTimeoutCache(idleAuthorityDeleteTimeout),
	}

	c.logger = prefixLogger(c)
	c.logger.Infof("Created client to xDS management server: %s", config.XDSServer)
	return c, nil
}

// OptionsForTesting contains options to configure xDS client creation for
// testing purposes only.
type OptionsForTesting struct {
	// Contents contain a JSON representation of the bootstrap configuration to
	// be used when creating the xDS client.
	Contents []byte

	// WatchExpiryTimeout is the timeout for xDS resource watch expiry. If
	// unspecified, uses the default value used in non-test code.
	WatchExpiryTimeout time.Duration

	// AuthorityIdleTimeout is the timeout before idle authorities are deleted.
	// If unspecified, uses the default value used in non-test code.
	AuthorityIdleTimeout time.Duration
}

// NewForTesting returns an xDS client configured with the provided options.
//
// The second return value represents a close function which the caller is
// expected to invoke once they are done using the client.  It is safe for the
// caller to invoke this close function multiple times.
//
// # Testing Only
//
// This function should ONLY be used for testing purposes.
func NewForTesting(opts OptionsForTesting) (XDSClient, func(), error) {
	if opts.WatchExpiryTimeout == 0 {
		opts.WatchExpiryTimeout = defaultWatchExpiryTimeout
	}
	if opts.AuthorityIdleTimeout == 0 {
		opts.AuthorityIdleTimeout = defaultIdleAuthorityDeleteTimeout
	}

	// Normalize the input configuration, as this is used as the key in the map
	// of xDS clients created for testing.
	buf := bytes.Buffer{}
	err := json.Indent(&buf, opts.Contents, "", "")
	if err != nil {
		return nil, nil, fmt.Errorf("xds: error normalizing JSON: %v", err)
	}
	opts.Contents = bytes.TrimSpace(buf.Bytes())

	clientsMu.Lock()
	defer clientsMu.Unlock()

	var client *clientRefCounted
	closeFunc := grpcsync.OnceFunc(func() {
		clientsMu.Lock()
		defer clientsMu.Unlock()
		if client.decrRef() == 0 {
			client.close()
			delete(clients, string(opts.Contents))
		}
	})

	// If an xDS client exists for the given configuration, increment its
	// reference count and return it.
	if c := clients[string(opts.Contents)]; c != nil {
		c.incrRef()
		client = c
		return c, closeFunc, nil
	}

	// Create a new xDS client for the given configuration
	bcfg, err := bootstrap.NewConfigFromContents(opts.Contents)
	if err != nil {
		return nil, nil, fmt.Errorf("bootstrap config %s: %v", string(opts.Contents), err)
	}
	cImpl, err := newWithConfig(bcfg, opts.WatchExpiryTimeout, opts.AuthorityIdleTimeout)
	if err != nil {
		return nil, nil, fmt.Errorf("creating xDS client: %v", err)
	}
	client = &clientRefCounted{clientImpl: cImpl, refCount: 1}
	clients[string(opts.Contents)] = client

	return client, closeFunc, nil
}

func init() {
	internal.TriggerXDSResourceNotFoundForTesting = triggerXDSResourceNotFoundForTesting
}

func triggerXDSResourceNotFoundForTesting(client XDSClient, typ xdsresource.Type, name string) error {
	crc, ok := client.(*clientRefCounted)
	if !ok {
		return fmt.Errorf("xDS client is of type %T, want %T", client, &clientRefCounted{})
	}
	return crc.clientImpl.triggerResourceNotFoundForTesting(typ, name)
}

var (
	clients   = map[string]*clientRefCounted{}
	clientsMu sync.Mutex
)
