/*
 *
 * Copyright 2021 gRPC authors.
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
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
	"google.golang.org/grpc/xds/internal/xdsclient/pubsub"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// authority is a combination of pubsub and the controller for this authority.
//
// Note that it might make sense to use one pubsub for all the resources (for
// all the controllers). One downside is the handling of StoW APIs (LDS/CDS).
// These responses contain all the resources from that control plane, so pubsub
// will need to keep lists of resources from each control plane, to know what
// are removed.
type authority struct {
	config     *bootstrap.ServerConfig
	pubsub     *pubsub.Pubsub
	controller controllerInterface
	refCount   int
}

// caller must hold parent's authorityMu.
func (a *authority) ref() {
	a.refCount++
}

// caller must hold parent's authorityMu.
func (a *authority) unref() int {
	a.refCount--
	return a.refCount
}

func (a *authority) close() {
	if a.pubsub != nil {
		a.pubsub.Close()
	}
	if a.controller != nil {
		a.controller.Close()
	}
}

func (a *authority) watchListener(serviceName string, cb func(xdsresource.ListenerUpdate, error)) (cancel func()) {
	first, cancelF := a.pubsub.WatchListener(serviceName, cb)
	if first {
		a.controller.AddWatch(xdsresource.ListenerResource, serviceName)
	}
	return func() {
		if cancelF() {
			a.controller.RemoveWatch(xdsresource.ListenerResource, serviceName)
		}
	}
}

func (a *authority) watchRouteConfig(routeName string, cb func(xdsresource.RouteConfigUpdate, error)) (cancel func()) {
	first, cancelF := a.pubsub.WatchRouteConfig(routeName, cb)
	if first {
		a.controller.AddWatch(xdsresource.RouteConfigResource, routeName)
	}
	return func() {
		if cancelF() {
			a.controller.RemoveWatch(xdsresource.RouteConfigResource, routeName)
		}
	}
}

func (a *authority) watchCluster(clusterName string, cb func(xdsresource.ClusterUpdate, error)) (cancel func()) {
	first, cancelF := a.pubsub.WatchCluster(clusterName, cb)
	if first {
		a.controller.AddWatch(xdsresource.ClusterResource, clusterName)
	}
	return func() {
		if cancelF() {
			a.controller.RemoveWatch(xdsresource.ClusterResource, clusterName)
		}
	}
}

func (a *authority) watchEndpoints(clusterName string, cb func(xdsresource.EndpointsUpdate, error)) (cancel func()) {
	first, cancelF := a.pubsub.WatchEndpoints(clusterName, cb)
	if first {
		a.controller.AddWatch(xdsresource.EndpointsResource, clusterName)
	}
	return func() {
		if cancelF() {
			a.controller.RemoveWatch(xdsresource.EndpointsResource, clusterName)
		}
	}
}

func (a *authority) reportLoad() (*load.Store, func()) {
	// An empty string means to report load to the same same used for ADS. There
	// should never be a need to specify a string other than an empty string. If
	// a different server is to be used, a different authority (controller) will
	// be created.
	return a.controller.ReportLoad("")
}

func (a *authority) dump(t xdsresource.ResourceType) map[string]xdsresource.UpdateWithMD {
	return a.pubsub.Dump(t)
}
