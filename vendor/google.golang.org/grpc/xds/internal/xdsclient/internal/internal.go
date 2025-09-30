/*
 *
 * Copyright 2024 gRPC authors.
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

// Package internal contains functionality internal to the xdsclient package.
package internal

// The following vars can be overridden by tests.
var (
	// GRPCNewClient returns a new gRPC Client.
	GRPCNewClient any // func(string, ...grpc.DialOption) (*grpc.ClientConn, error)

	// NewADSStream returns a new ADS stream.
	NewADSStream any // func(context.Context, *grpc.ClientConn) (v3adsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesClient, error)

	// ResourceWatchStateForTesting gets the watch state for the resource
	// identified by the given resource type and resource name. Returns a
	// non-nil error if there is no such resource being watched.
	ResourceWatchStateForTesting any // func(xdsclient.XDSClient, xdsresource.Type, string) error
)
