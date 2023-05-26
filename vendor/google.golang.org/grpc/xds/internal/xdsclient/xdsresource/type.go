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

package xdsresource

import (
	"time"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/types/known/anypb"
)

// UpdateValidatorFunc performs validations on update structs using
// context/logic available at the xdsClient layer. Since these validation are
// performed on internal update structs, they can be shared between different
// API clients.
type UpdateValidatorFunc func(interface{}) error

// UpdateMetadata contains the metadata for each update, including timestamp,
// raw message, and so on.
type UpdateMetadata struct {
	// Status is the status of this resource, e.g. ACKed, NACKed, or
	// Not_exist(removed).
	Status ServiceStatus
	// Version is the version of the xds response. Note that this is the version
	// of the resource in use (previous ACKed). If a response is NACKed, the
	// NACKed version is in ErrState.
	Version string
	// Timestamp is when the response is received.
	Timestamp time.Time
	// ErrState is set when the update is NACKed.
	ErrState *UpdateErrorMetadata
}

// IsListenerResource returns true if the provider URL corresponds to an xDS
// Listener resource.
func IsListenerResource(url string) bool {
	return url == version.V2ListenerURL || url == version.V3ListenerURL
}

// IsHTTPConnManagerResource returns true if the provider URL corresponds to an xDS
// HTTPConnManager resource.
func IsHTTPConnManagerResource(url string) bool {
	return url == version.V2HTTPConnManagerURL || url == version.V3HTTPConnManagerURL
}

// IsRouteConfigResource returns true if the provider URL corresponds to an xDS
// RouteConfig resource.
func IsRouteConfigResource(url string) bool {
	return url == version.V2RouteConfigURL || url == version.V3RouteConfigURL
}

// IsClusterResource returns true if the provider URL corresponds to an xDS
// Cluster resource.
func IsClusterResource(url string) bool {
	return url == version.V2ClusterURL || url == version.V3ClusterURL
}

// IsEndpointsResource returns true if the provider URL corresponds to an xDS
// Endpoints resource.
func IsEndpointsResource(url string) bool {
	return url == version.V2EndpointsURL || url == version.V3EndpointsURL
}

// unwrapResource unwraps and returns the inner resource if it's in a resource
// wrapper. The original resource is returned if it's not wrapped.
func unwrapResource(r *anypb.Any) (*anypb.Any, error) {
	url := r.GetTypeUrl()
	if url != version.V2ResourceWrapperURL && url != version.V3ResourceWrapperURL {
		// Not wrapped.
		return r, nil
	}
	inner := &v3discoverypb.Resource{}
	if err := proto.Unmarshal(r.GetValue(), inner); err != nil {
		return nil, err
	}
	return inner.Resource, nil
}

// ServiceStatus is the status of the update.
type ServiceStatus int

const (
	// ServiceStatusUnknown is the default state, before a watch is started for
	// the resource.
	ServiceStatusUnknown ServiceStatus = iota
	// ServiceStatusRequested is when the watch is started, but before and
	// response is received.
	ServiceStatusRequested
	// ServiceStatusNotExist is when the resource doesn't exist in
	// state-of-the-world responses (e.g. LDS and CDS), which means the resource
	// is removed by the management server.
	ServiceStatusNotExist // Resource is removed in the server, in LDS/CDS.
	// ServiceStatusACKed is when the resource is ACKed.
	ServiceStatusACKed
	// ServiceStatusNACKed is when the resource is NACKed.
	ServiceStatusNACKed
)

// UpdateErrorMetadata is part of UpdateMetadata. It contains the error state
// when a response is NACKed.
type UpdateErrorMetadata struct {
	// Version is the version of the NACKed response.
	Version string
	// Err contains why the response was NACKed.
	Err error
	// Timestamp is when the NACKed response was received.
	Timestamp time.Time
}

// UpdateWithMD contains the raw message of the update and the metadata,
// including version, raw message, timestamp.
//
// This is to be used for config dump and CSDS, not directly by users (like
// resolvers/balancers).
type UpdateWithMD struct {
	MD  UpdateMetadata
	Raw *anypb.Any
}

// ResourceType identifies resources in a transport protocol agnostic way. These
// will be used in transport version agnostic code, while the versioned API
// clients will map these to appropriate version URLs.
type ResourceType int

// Version agnostic resource type constants.
const (
	UnknownResource ResourceType = iota
	ListenerResource
	HTTPConnManagerResource
	RouteConfigResource
	ClusterResource
	EndpointsResource
)

func (r ResourceType) String() string {
	switch r {
	case ListenerResource:
		return "ListenerResource"
	case HTTPConnManagerResource:
		return "HTTPConnManagerResource"
	case RouteConfigResource:
		return "RouteConfigResource"
	case ClusterResource:
		return "ClusterResource"
	case EndpointsResource:
		return "EndpointsResource"
	default:
		return "UnknownResource"
	}
}

var v2ResourceTypeToURL = map[ResourceType]string{
	ListenerResource:        version.V2ListenerURL,
	HTTPConnManagerResource: version.V2HTTPConnManagerURL,
	RouteConfigResource:     version.V2RouteConfigURL,
	ClusterResource:         version.V2ClusterURL,
	EndpointsResource:       version.V2EndpointsURL,
}
var v3ResourceTypeToURL = map[ResourceType]string{
	ListenerResource:        version.V3ListenerURL,
	HTTPConnManagerResource: version.V3HTTPConnManagerURL,
	RouteConfigResource:     version.V3RouteConfigURL,
	ClusterResource:         version.V3ClusterURL,
	EndpointsResource:       version.V3EndpointsURL,
}

// URL returns the transport protocol specific resource type URL.
func (r ResourceType) URL(v version.TransportAPI) string {
	var mapping map[ResourceType]string
	switch v {
	case version.TransportV2:
		mapping = v2ResourceTypeToURL
	case version.TransportV3:
		mapping = v3ResourceTypeToURL
	default:
		return "UnknownResource"
	}
	if url, ok := mapping[r]; ok {
		return url
	}
	return "UnknownResource"
}

var urlToResourceType = map[string]ResourceType{
	version.V2ListenerURL:        ListenerResource,
	version.V2RouteConfigURL:     RouteConfigResource,
	version.V2ClusterURL:         ClusterResource,
	version.V2EndpointsURL:       EndpointsResource,
	version.V2HTTPConnManagerURL: HTTPConnManagerResource,
	version.V3ListenerURL:        ListenerResource,
	version.V3RouteConfigURL:     RouteConfigResource,
	version.V3ClusterURL:         ClusterResource,
	version.V3EndpointsURL:       EndpointsResource,
	version.V3HTTPConnManagerURL: HTTPConnManagerResource,
}

// ResourceTypeFromURL returns the xDS resource type associated with the given
// resource type URL.
func ResourceTypeFromURL(url string) ResourceType {
	if typ, ok := urlToResourceType[url]; ok {
		return typ
	}
	return UnknownResource
}
