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

	"google.golang.org/grpc/internal/pretty"
)

type watchInfoState int

const (
	watchInfoStateStarted watchInfoState = iota
	watchInfoStateRespReceived
	watchInfoStateTimeout
	watchInfoStateCanceled
)

// watchInfo holds all the information from a watch() call.
type watchInfo struct {
	c      *clientImpl
	rType  ResourceType
	target string

	ldsCallback func(ListenerUpdate, error)
	rdsCallback func(RouteConfigUpdate, error)
	cdsCallback func(ClusterUpdate, error)
	edsCallback func(EndpointsUpdate, error)

	expiryTimer *time.Timer

	// mu protects state, and c.scheduleCallback().
	// - No callback should be scheduled after watchInfo is canceled.
	// - No timeout error should be scheduled after watchInfo is resp received.
	mu    sync.Mutex
	state watchInfoState
}

func (wi *watchInfo) newUpdate(update interface{}) {
	wi.mu.Lock()
	defer wi.mu.Unlock()
	if wi.state == watchInfoStateCanceled {
		return
	}
	wi.state = watchInfoStateRespReceived
	wi.expiryTimer.Stop()
	wi.c.scheduleCallback(wi, update, nil)
}

func (wi *watchInfo) newError(err error) {
	wi.mu.Lock()
	defer wi.mu.Unlock()
	if wi.state == watchInfoStateCanceled {
		return
	}
	wi.state = watchInfoStateRespReceived
	wi.expiryTimer.Stop()
	wi.sendErrorLocked(err)
}

func (wi *watchInfo) resourceNotFound() {
	wi.mu.Lock()
	defer wi.mu.Unlock()
	if wi.state == watchInfoStateCanceled {
		return
	}
	wi.state = watchInfoStateRespReceived
	wi.expiryTimer.Stop()
	wi.sendErrorLocked(NewErrorf(ErrorTypeResourceNotFound, "xds: %v target %s not found in received response", wi.rType, wi.target))
}

func (wi *watchInfo) timeout() {
	wi.mu.Lock()
	defer wi.mu.Unlock()
	if wi.state == watchInfoStateCanceled || wi.state == watchInfoStateRespReceived {
		return
	}
	wi.state = watchInfoStateTimeout
	wi.sendErrorLocked(fmt.Errorf("xds: %v target %s not found, watcher timeout", wi.rType, wi.target))
}

// Caller must hold wi.mu.
func (wi *watchInfo) sendErrorLocked(err error) {
	var (
		u interface{}
	)
	switch wi.rType {
	case ListenerResource:
		u = ListenerUpdate{}
	case RouteConfigResource:
		u = RouteConfigUpdate{}
	case ClusterResource:
		u = ClusterUpdate{}
	case EndpointsResource:
		u = EndpointsUpdate{}
	}
	wi.c.scheduleCallback(wi, u, err)
}

func (wi *watchInfo) cancel() {
	wi.mu.Lock()
	defer wi.mu.Unlock()
	if wi.state == watchInfoStateCanceled {
		return
	}
	wi.expiryTimer.Stop()
	wi.state = watchInfoStateCanceled
}

func (c *clientImpl) watch(wi *watchInfo) (cancel func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logger.Debugf("new watch for type %v, resource name %v", wi.rType, wi.target)
	var (
		watchers map[string]map[*watchInfo]bool
		mds      map[string]UpdateMetadata
	)
	switch wi.rType {
	case ListenerResource:
		watchers = c.ldsWatchers
		mds = c.ldsMD
	case RouteConfigResource:
		watchers = c.rdsWatchers
		mds = c.rdsMD
	case ClusterResource:
		watchers = c.cdsWatchers
		mds = c.cdsMD
	case EndpointsResource:
		watchers = c.edsWatchers
		mds = c.edsMD
	default:
		c.logger.Errorf("unknown watch type: %v", wi.rType)
		return nil
	}

	resourceName := wi.target
	s, ok := watchers[wi.target]
	if !ok {
		// If this is a new watcher, will ask lower level to send a new request
		// with the resource name.
		//
		// If this (type+name) is already being watched, will not notify the
		// underlying versioned apiClient.
		c.logger.Debugf("first watch for type %v, resource name %v, will send a new xDS request", wi.rType, wi.target)
		s = make(map[*watchInfo]bool)
		watchers[resourceName] = s
		mds[resourceName] = UpdateMetadata{Status: ServiceStatusRequested}
		c.apiClient.AddWatch(wi.rType, resourceName)
	}
	// No matter what, add the new watcher to the set, so it's callback will be
	// call for new responses.
	s[wi] = true

	// If the resource is in cache, call the callback with the value.
	switch wi.rType {
	case ListenerResource:
		if v, ok := c.ldsCache[resourceName]; ok {
			c.logger.Debugf("LDS resource with name %v found in cache: %+v", wi.target, pretty.ToJSON(v))
			wi.newUpdate(v)
		}
	case RouteConfigResource:
		if v, ok := c.rdsCache[resourceName]; ok {
			c.logger.Debugf("RDS resource with name %v found in cache: %+v", wi.target, pretty.ToJSON(v))
			wi.newUpdate(v)
		}
	case ClusterResource:
		if v, ok := c.cdsCache[resourceName]; ok {
			c.logger.Debugf("CDS resource with name %v found in cache: %+v", wi.target, pretty.ToJSON(v))
			wi.newUpdate(v)
		}
	case EndpointsResource:
		if v, ok := c.edsCache[resourceName]; ok {
			c.logger.Debugf("EDS resource with name %v found in cache: %+v", wi.target, pretty.ToJSON(v))
			wi.newUpdate(v)
		}
	}

	return func() {
		c.logger.Debugf("watch for type %v, resource name %v canceled", wi.rType, wi.target)
		wi.cancel()
		c.mu.Lock()
		defer c.mu.Unlock()
		if s := watchers[resourceName]; s != nil {
			// Remove this watcher, so it's callback will not be called in the
			// future.
			delete(s, wi)
			if len(s) == 0 {
				c.logger.Debugf("last watch for type %v, resource name %v canceled, will send a new xDS request", wi.rType, wi.target)
				// If this was the last watcher, also tell xdsv2Client to stop
				// watching this resource.
				delete(watchers, resourceName)
				delete(mds, resourceName)
				c.apiClient.RemoveWatch(wi.rType, resourceName)
				// Remove the resource from cache. When a watch for this
				// resource is added later, it will trigger a xDS request with
				// resource names, and client will receive new xDS responses.
				switch wi.rType {
				case ListenerResource:
					delete(c.ldsCache, resourceName)
				case RouteConfigResource:
					delete(c.rdsCache, resourceName)
				case ClusterResource:
					delete(c.cdsCache, resourceName)
				case EndpointsResource:
					delete(c.edsCache, resourceName)
				}
			}
		}
	}
}

// WatchListener uses LDS to discover information about the provided listener.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *clientImpl) WatchListener(serviceName string, cb func(ListenerUpdate, error)) (cancel func()) {
	wi := &watchInfo{
		c:           c,
		rType:       ListenerResource,
		target:      serviceName,
		ldsCallback: cb,
	}

	wi.expiryTimer = time.AfterFunc(c.watchExpiryTimeout, func() {
		wi.timeout()
	})
	return c.watch(wi)
}

// WatchRouteConfig starts a listener watcher for the service..
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *clientImpl) WatchRouteConfig(routeName string, cb func(RouteConfigUpdate, error)) (cancel func()) {
	wi := &watchInfo{
		c:           c,
		rType:       RouteConfigResource,
		target:      routeName,
		rdsCallback: cb,
	}

	wi.expiryTimer = time.AfterFunc(c.watchExpiryTimeout, func() {
		wi.timeout()
	})
	return c.watch(wi)
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
func (c *clientImpl) WatchCluster(clusterName string, cb func(ClusterUpdate, error)) (cancel func()) {
	wi := &watchInfo{
		c:           c,
		rType:       ClusterResource,
		target:      clusterName,
		cdsCallback: cb,
	}

	wi.expiryTimer = time.AfterFunc(c.watchExpiryTimeout, func() {
		wi.timeout()
	})
	return c.watch(wi)
}

// WatchEndpoints uses EDS to discover endpoints in the provided clusterName.
//
// WatchEndpoints can be called multiple times, with same or different
// clusterNames. Each call will start an independent watcher for the resource.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *clientImpl) WatchEndpoints(clusterName string, cb func(EndpointsUpdate, error)) (cancel func()) {
	wi := &watchInfo{
		c:           c,
		rType:       EndpointsResource,
		target:      clusterName,
		edsCallback: cb,
	}

	wi.expiryTimer = time.AfterFunc(c.watchExpiryTimeout, func() {
		wi.timeout()
	})
	return c.watch(wi)
}
