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
 */

package xdsclient

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc/xds/internal/xdsclient/transport/ads"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// WatchResource uses xDS to discover the resource associated with the provided
// resource name. The resource type implementation determines how xDS responses
// are are deserialized and validated, as received from the xDS management
// server. Upon receipt of a response from the management server, an
// appropriate callback on the watcher is invoked.
func (c *clientImpl) WatchResource(rType xdsresource.Type, resourceName string, watcher xdsresource.ResourceWatcher) (cancel func()) {
	// Return early if the client is already closed.
	//
	// The client returned from the top-level API is a ref-counted client which
	// contains a pointer to `clientImpl`. When all references are released, the
	// ref-counted client sets its pointer to `nil`. And if any watch APIs are
	// made on such a closed client, we will get here with a `nil` receiver.
	if c == nil || c.done.HasFired() {
		logger.Warningf("Watch registered for name %q of type %q, but client is closed", rType.TypeName(), resourceName)
		return func() {}
	}

	if err := c.resourceTypes.maybeRegister(rType); err != nil {
		logger.Warningf("Watch registered for name %q of type %q which is already registered", rType.TypeName(), resourceName)
		c.serializer.TrySchedule(func(context.Context) { watcher.OnError(err, func() {}) })
		return func() {}
	}

	n := xdsresource.ParseName(resourceName)
	a := c.getAuthorityForResource(n)
	if a == nil {
		logger.Warningf("Watch registered for name %q of type %q, authority %q is not found", rType.TypeName(), resourceName, n.Authority)
		c.serializer.TrySchedule(func(context.Context) {
			watcher.OnError(fmt.Errorf("authority %q not found in bootstrap config for resource %q", n.Authority, resourceName), func() {})
		})
		return func() {}
	}
	// The watchResource method on the authority is invoked with n.String()
	// instead of resourceName because n.String() canonicalizes the given name.
	// So, two resource names which don't differ in the query string, but only
	// differ in the order of context params will result in the same resource
	// being watched by the authority.
	return a.watchResource(rType, n.String(), watcher)
}

// Gets the authority for the given resource name.
//
// See examples in this section of the gRFC:
// https://github.com/grpc/proposal/blob/master/A47-xds-federation.md#bootstrap-config-changes
func (c *clientImpl) getAuthorityForResource(name *xdsresource.Name) *authority {
	// For new-style resource names, always lookup the authorities map. If the
	// name does not specify an authority, we will end up looking for an entry
	// in the map with the empty string as the key.
	if name.Scheme == xdsresource.FederationScheme {
		return c.authorities[name.Authority]
	}

	// For old-style resource names, we use the top-level authority if the name
	// does not specify an authority.
	if name.Authority == "" {
		return c.topLevelAuthority
	}
	return c.authorities[name.Authority]
}

// A registry of xdsresource.Type implementations indexed by their corresponding
// type URLs. Registration of an xdsresource.Type happens the first time a watch
// for a resource of that type is invoked.
type resourceTypeRegistry struct {
	mu    sync.Mutex
	types map[string]xdsresource.Type
}

func newResourceTypeRegistry() *resourceTypeRegistry {
	return &resourceTypeRegistry{types: make(map[string]xdsresource.Type)}
}

func (r *resourceTypeRegistry) get(url string) xdsresource.Type {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.types[url]
}

func (r *resourceTypeRegistry) maybeRegister(rType xdsresource.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	url := rType.TypeURL()
	typ, ok := r.types[url]
	if ok && typ != rType {
		return fmt.Errorf("attempt to re-register a resource type implementation for %v", rType.TypeName())
	}
	r.types[url] = rType
	return nil
}

func (c *clientImpl) triggerResourceNotFoundForTesting(rType xdsresource.Type, resourceName string) error {
	c.channelsMu.Lock()
	defer c.channelsMu.Unlock()

	if c.logger.V(2) {
		c.logger.Infof("Triggering resource not found for type: %s, resource name: %s", rType.TypeName(), resourceName)
	}

	for _, state := range c.xdsActiveChannels {
		if err := state.channel.triggerResourceNotFoundForTesting(rType, resourceName); err != nil {
			return err
		}
	}
	return nil
}

func (c *clientImpl) resourceWatchStateForTesting(rType xdsresource.Type, resourceName string) (ads.ResourceWatchState, error) {
	c.channelsMu.Lock()
	defer c.channelsMu.Unlock()

	for _, state := range c.xdsActiveChannels {
		if st, err := state.channel.ads.ResourceWatchStateForTesting(rType, resourceName); err == nil {
			return st, nil
		}
	}
	return ads.ResourceWatchState{}, fmt.Errorf("unable to find watch state for resource type %q and name %q", rType.TypeName(), resourceName)
}
