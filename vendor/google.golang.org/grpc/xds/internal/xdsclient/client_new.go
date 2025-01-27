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
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdsclientinternal "google.golang.org/grpc/xds/internal/xdsclient/internal"
	"google.golang.org/grpc/xds/internal/xdsclient/transport/ads"
	"google.golang.org/grpc/xds/internal/xdsclient/transport/grpctransport"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// NameForServer represents the value to be passed as name when creating an xDS
// client from xDS-enabled gRPC servers. This is a well-known dedicated key
// value, and is defined in gRFC A71.
const NameForServer = "#server"

// New returns an xDS client configured with bootstrap configuration specified
// by the ordered list:
// - file name containing the configuration specified by GRPC_XDS_BOOTSTRAP
// - actual configuration specified by GRPC_XDS_BOOTSTRAP_CONFIG
// - fallback configuration set using bootstrap.SetFallbackBootstrapConfig
//
// gRPC client implementations are expected to pass the channel's target URI for
// the name field, while server implementations are expected to pass a dedicated
// well-known value "#server", as specified in gRFC A71. The returned client is
// a reference counted implementation shared among callers using the same name.
//
// The second return value represents a close function which releases the
// caller's reference on the returned client.  The caller is expected to invoke
// it once they are done using the client. The underlying client will be closed
// only when all references are released, and it is safe for the caller to
// invoke this close function multiple times.
func New(name string) (XDSClient, func(), error) {
	config, err := bootstrap.GetConfiguration()
	if err != nil {
		return nil, nil, fmt.Errorf("xds: failed to get xDS bootstrap config: %v", err)
	}
	return newRefCounted(name, config, defaultWatchExpiryTimeout, backoff.DefaultExponential.Backoff)
}

// newClientImpl returns a new xdsClient with the given config.
func newClientImpl(config *bootstrap.Config, watchExpiryTimeout time.Duration, streamBackoff func(int) time.Duration) (*clientImpl, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &clientImpl{
		done:               grpcsync.NewEvent(),
		authorities:        make(map[string]*authority),
		config:             config,
		watchExpiryTimeout: watchExpiryTimeout,
		backoff:            streamBackoff,
		serializer:         grpcsync.NewCallbackSerializer(ctx),
		serializerClose:    cancel,
		transportBuilder:   &grpctransport.Builder{},
		resourceTypes:      newResourceTypeRegistry(),
		xdsActiveChannels:  make(map[string]*channelState),
	}

	for name, cfg := range config.Authorities() {
		// If server configs are specified in the authorities map, use that.
		// Else, use the top-level server configs.
		serverCfg := config.XDSServers()
		if len(cfg.XDSServers) >= 1 {
			serverCfg = cfg.XDSServers
		}
		c.authorities[name] = newAuthority(authorityBuildOptions{
			serverConfigs:    serverCfg,
			name:             name,
			serializer:       c.serializer,
			getChannelForADS: c.getChannelForADS,
			logPrefix:        clientPrefix(c),
		})
	}
	c.topLevelAuthority = newAuthority(authorityBuildOptions{
		serverConfigs:    config.XDSServers(),
		name:             "",
		serializer:       c.serializer,
		getChannelForADS: c.getChannelForADS,
		logPrefix:        clientPrefix(c),
	})
	c.logger = prefixLogger(c)
	return c, nil
}

// OptionsForTesting contains options to configure xDS client creation for
// testing purposes only.
type OptionsForTesting struct {
	// Name is a unique name for this xDS client.
	Name string

	// Contents contain a JSON representation of the bootstrap configuration to
	// be used when creating the xDS client.
	Contents []byte

	// WatchExpiryTimeout is the timeout for xDS resource watch expiry. If
	// unspecified, uses the default value used in non-test code.
	WatchExpiryTimeout time.Duration

	// StreamBackoffAfterFailure is the backoff function used to determine the
	// backoff duration after stream failures.
	// If unspecified, uses the default value used in non-test code.
	StreamBackoffAfterFailure func(int) time.Duration
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
	if opts.Name == "" {
		return nil, nil, fmt.Errorf("opts.Name field must be non-empty")
	}
	if opts.WatchExpiryTimeout == 0 {
		opts.WatchExpiryTimeout = defaultWatchExpiryTimeout
	}
	if opts.StreamBackoffAfterFailure == nil {
		opts.StreamBackoffAfterFailure = defaultStreamBackoffFunc
	}

	config, err := bootstrap.NewConfigForTesting(opts.Contents)
	if err != nil {
		return nil, nil, err
	}
	return newRefCounted(opts.Name, config, opts.WatchExpiryTimeout, opts.StreamBackoffAfterFailure)
}

// GetForTesting returns an xDS client created earlier using the given name.
//
// The second return value represents a close function which the caller is
// expected to invoke once they are done using the client.  It is safe for the
// caller to invoke this close function multiple times.
//
// # Testing Only
//
// This function should ONLY be used for testing purposes.
func GetForTesting(name string) (XDSClient, func(), error) {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	c, ok := clients[name]
	if !ok {
		return nil, nil, fmt.Errorf("xDS client with name %q not found", name)
	}
	c.incrRef()
	return c, grpcsync.OnceFunc(func() { clientRefCountedClose(name) }), nil
}

func init() {
	internal.TriggerXDSResourceNotFoundForTesting = triggerXDSResourceNotFoundForTesting
	xdsclientinternal.ResourceWatchStateForTesting = resourceWatchStateForTesting
}

func triggerXDSResourceNotFoundForTesting(client XDSClient, typ xdsresource.Type, name string) error {
	crc, ok := client.(*clientRefCounted)
	if !ok {
		return fmt.Errorf("xDS client is of type %T, want %T", client, &clientRefCounted{})
	}
	return crc.clientImpl.triggerResourceNotFoundForTesting(typ, name)
}

func resourceWatchStateForTesting(client XDSClient, typ xdsresource.Type, name string) (ads.ResourceWatchState, error) {
	crc, ok := client.(*clientRefCounted)
	if !ok {
		return ads.ResourceWatchState{}, fmt.Errorf("xDS client is of type %T, want %T", client, &clientRefCounted{})
	}
	return crc.clientImpl.resourceWatchStateForTesting(typ, name)
}

var (
	clients   = map[string]*clientRefCounted{}
	clientsMu sync.Mutex
)
