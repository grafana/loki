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

	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// WatchListener uses LDS to discover information about the provided listener.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *clientImpl) WatchListener(serviceName string, cb func(xdsresource.ListenerUpdate, error)) (cancel func()) {
	n := xdsresource.ParseName(serviceName)
	a, unref, err := c.findAuthority(n)
	if err != nil {
		cb(xdsresource.ListenerUpdate{}, err)
		return func() {}
	}
	cancelF := a.watchListener(n.String(), cb)
	return func() {
		cancelF()
		unref()
	}
}

// WatchRouteConfig starts a listener watcher for the service.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *clientImpl) WatchRouteConfig(routeName string, cb func(xdsresource.RouteConfigUpdate, error)) (cancel func()) {
	n := xdsresource.ParseName(routeName)
	a, unref, err := c.findAuthority(n)
	if err != nil {
		cb(xdsresource.RouteConfigUpdate{}, err)
		return func() {}
	}
	cancelF := a.watchRouteConfig(n.String(), cb)
	return func() {
		cancelF()
		unref()
	}
}

// WatchCluster uses CDS to discover information about the provided
// clusterName.
//
// WatchCluster can be called multiple times, with same or different
// clusterNames. Each call will start an independent watcher for the resource.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *clientImpl) WatchCluster(clusterName string, cb func(xdsresource.ClusterUpdate, error)) (cancel func()) {
	n := xdsresource.ParseName(clusterName)
	a, unref, err := c.findAuthority(n)
	if err != nil {
		cb(xdsresource.ClusterUpdate{}, err)
		return func() {}
	}
	cancelF := a.watchCluster(n.String(), cb)
	return func() {
		cancelF()
		unref()
	}
}

// WatchEndpoints uses EDS to discover endpoints in the provided clusterName.
//
// WatchEndpoints can be called multiple times, with same or different
// clusterNames. Each call will start an independent watcher for the resource.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *clientImpl) WatchEndpoints(clusterName string, cb func(xdsresource.EndpointsUpdate, error)) (cancel func()) {
	n := xdsresource.ParseName(clusterName)
	a, unref, err := c.findAuthority(n)
	if err != nil {
		cb(xdsresource.EndpointsUpdate{}, err)
		return func() {}
	}
	cancelF := a.watchEndpoints(n.String(), cb)
	return func() {
		cancelF()
		unref()
	}
}

// WatchResource uses xDS to discover the resource associated with the provided
// resource name. The resource type implementation determines how xDS requests
// are sent out and how responses are deserialized and validated. Upon receipt
// of a response from the management server, an appropriate callback on the
// watcher is invoked.
func (c *clientImpl) WatchResource(rType xdsresource.Type, resourceName string, watcher xdsresource.ResourceWatcher) (cancel func()) {
	// Return early if the client is already closed.
	//
	// The client returned from the top-level API is a ref-counted client which
	// contains a pointer to `clientImpl`. When all references are released, the
	// ref-counted client sets its pointer to `nil`. And if any watch APIs are
	// made on such a closed client, we will get here with a `nil` receiver.
	if c == nil || c.done.HasFired() {
		logger.Warningf("Watch registered for name %q of type %q, but client is closed", rType.TypeEnum().String(), resourceName)
		return func() {}
	}

	if err := c.resourceTypes.maybeRegister(rType); err != nil {
		c.serializer.Schedule(func(context.Context) { watcher.OnError(err) })
		return func() {}
	}

	// TODO: replace this with the code does the following when we have
	// implemented generic watch API on the authority:
	//  - Parse the resource name and extract the authority.
	//  - Locate the corresponding authority object and acquire a reference to
	//    it. If the authority is not found, error out.
	//  - Call the watchResource() method on the authority.
	//  - Return a cancel function to cancel the watch on the authority and to
	//    release the reference.
	return func() {}
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

func (r *resourceTypeRegistry) maybeRegister(rType xdsresource.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	urls := []string{rType.V2TypeURL(), rType.V3TypeURL()}
	for _, u := range urls {
		if u == "" {
			// Silently ignore unsupported versions of the resource.
			continue
		}
		typ, ok := r.types[u]
		if ok && typ != rType {
			return fmt.Errorf("attempt to re-register a resource type implementation for %v", rType.TypeEnum())
		}
		r.types[u] = rType
	}
	return nil
}
