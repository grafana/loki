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
 *
 */

// Package httpfilter contains interface definitions for xDS-based HTTP filters
// and a registry for filter builders.
package httpfilter

import (
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/protobuf/proto"
)

// FilterConfig represents an opaque data structure holding configuration for a
// filter.  Embed this interface to implement it.
type FilterConfig interface {
	isFilterConfig()
}

// DisabledFilterConfig represents a disabled filter override. It implements the
// FilterConfig interface and can be returned by ParseFilterConfigOverride to
// indicate that the filter should be disabled. It is not used as a config for
// any filter, and is only used as a marker in the override configuration. For
// more information, see
// envoyproxy.io/docs/envoy/latest/intro/arch_overview/http/http_filters#route-based-filter-chain
type DisabledFilterConfig struct{}

func (DisabledFilterConfig) isFilterConfig() {}

// Builder defines the parsing functionality of an HTTP filter.  A Builder may
// optionally implement either ClientFilterBuilder or ServerFilterBuilder or
// both, indicating it is capable of working on the client side or server side
// or both, respectively.
type Builder interface {
	// TypeURLs are the proto message types supported by this filter.  A filter
	// will be registered by each of its supported message types.
	TypeURLs() []string
	// ParseFilterConfig parses the provided configuration proto.Message from
	// the LDS configuration of this filter.  This may be an anypb.Any, a
	// udpa.type.v1.TypedStruct, or an xds.type.v3.TypedStruct for filters that
	// do not accept a custom type. The resulting FilterConfig will later be
	// passed to Build.
	ParseFilterConfig(proto.Message) (FilterConfig, error)
	// ParseFilterConfigOverride parses the provided override configuration
	// proto.Message from the RDS override configuration of this filter.  This
	// may be an anypb.Any, a udpa.type.v1.TypedStruct, or an
	// xds.type.v3.TypedStruct for filters that do not accept a custom type.
	// The resulting FilterConfig will later be passed to Build.
	ParseFilterConfigOverride(proto.Message) (FilterConfig, error)
	// IsTerminal returns whether this Filter is terminal or not (i.e. it must
	// be last filter in the filter chain).
	IsTerminal() bool
}

// ClientFilterBuilder is an optional interface that a Builder can implement to
// indicate its capability to build client-side filters.
type ClientFilterBuilder interface {
	// BuildClientFilter constructs a ClientFilter.
	BuildClientFilter() ClientFilter
}

// ClientFilter represents the actual filter implementation on the client side.
// Implementations are free to maintain internal state when required, and share
// it across interceptors. Filter instances are retained by the resolver as long
// as they are present in the LDS configuration.
type ClientFilter interface {
	// BuildClientInterceptor uses the given FilterConfigs to produce an HTTP
	// filter interceptor for clients. config will always be non-nil, but
	// override may be nil if no override config exists for the filter.
	//
	// It is valid for this method to return a nil Interceptor and a nil error.
	// In this case, the RPC will not be intercepted by this filter.
	BuildClientInterceptor(config, override FilterConfig) (iresolver.ClientInterceptor, error)

	// Close is called when the filter is no longer needed.
	Close()
}

// ServerFilterBuilder is an optional interface that a Builder can implement to
// indicate its capability to build server-side filters.
type ServerFilterBuilder interface {
	// BuildServerFilter constructs a ServerFilter.
	BuildServerFilter() ServerFilter
}

// ServerFilter represents the actual filter implementation on the server side.
// Implementations are free to maintain internal state when required, and share
// it across interceptors. Filter instances are retained by the server as long
// as they are present in any of the filter chains in the LDS configuration.
type ServerFilter interface {
	// BuildServerInterceptor uses the given FilterConfigs to produce
	// an HTTP filter interceptor for servers. config will always be non-nil,
	// but override may be nil if no override config exists for the filter.
	//
	// It is valid for this method to return a nil Interceptor and a nil error.
	// In this case, the RPC will not be intercepted by this filter.
	BuildServerInterceptor(config, override FilterConfig) (iresolver.ServerInterceptor, error)

	// Close is called when the filter is no longer needed.
	Close()
}

var (
	// registeredBuilders is a map from scheme to filter builder.
	registeredBuilders = make(map[string]Builder)
)

// Register registers the HTTP Filter Builder with the registry. b.TypeURLs()
// will be used as the types for this filter.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple filters are
// registered with the same type URL, the one registered last will take effect.
func Register(b Builder) {
	for _, u := range b.TypeURLs() {
		registeredBuilders[u] = b
	}
}

// UnregisterForTesting unregisters the HTTP Filter Builder for testing purposes.
func UnregisterForTesting(typeURL string) {
	delete(registeredBuilders, typeURL)
}

// Get returns the HTTP Filter Builder registered with typeURL.
//
// If no filter builder is register with typeURL, nil will be returned.
func Get(typeURL string) Builder {
	return registeredBuilders[typeURL]
}
