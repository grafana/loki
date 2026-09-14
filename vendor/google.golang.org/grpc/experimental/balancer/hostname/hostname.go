/*
 *
 * Copyright 2026 gRPC authors.
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

// Package hostname contains utilities for the endpoint hostname attribute
// (used for per-endpoint :authority / SNI override).
//
// # Experimental
//
// Notice: All APIs in this package are EXPERIMENTAL and may be changed
// or removed in a later release.
package hostname

import "google.golang.org/grpc/resolver"

type hostnameKey struct{}

// Set returns a copy of the given endpoint with the hostname attribute
// set. If hostname is empty the endpoint is returned unmodified.
func Set(endpoint resolver.Endpoint, hostname string) resolver.Endpoint {
	if hostname == "" {
		return endpoint
	}
	endpoint.Attributes = endpoint.Attributes.WithValue(hostnameKey{}, hostname)
	return endpoint
}

// FromEndpoint returns the hostname attribute of endpoint. If this
// attribute is not set, it returns the empty string.
func FromEndpoint(endpoint resolver.Endpoint) string {
	h, _ := endpoint.Attributes.Value(hostnameKey{}).(string)
	return h
}
