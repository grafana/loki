/*
 *
 * Copyright 2025 gRPC authors.
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

// Package metrics defines all metrics that can be produced by an xDS client.
// All calls to the MetricsRecorder by the xDS client will contain a struct
// from this package passed by pointer.
//
// For definitions of these metrics and their labels, see gRFC A78:
// https://github.com/grpc/proposal/blob/master/A78-grpc-metrics-wrr-xds.md
package metrics

// ResourceUpdateValid is a metric to report a valid resource update from
// the xDS management server for a given resource type.
type ResourceUpdateValid struct {
	ServerURI    string
	ResourceType string
}

// ResourceUpdateInvalid is a metric to report an invalid resource update
// from the xDS management server for a given resource type.
type ResourceUpdateInvalid struct {
	ServerURI    string
	ResourceType string
}

// ServerFailure is a metric to report a server failure of the xDS
// management server.
type ServerFailure struct {
	ServerURI string
}

// XDSClientConnected reports the connectivity state of the ADS stream.
// Per gRFC A78, Value is 1 if connected, and 0 otherwise.
// This metric provides the labels grpc.target and grpc.xds.server.
type XDSClientConnected struct {
	ServerURI string
	Value     int64
}

// XDSClientResourceStats reports the current cache states of xDS resources.
// For label definitions, see gRFC A78.
// This metric provides the labels grpc.target, grpc.xds.authority,
// grpc.xds.cache_state, and grpc.xds.resource_type.
type XDSClientResourceStats struct {
	Authority    string
	ResourceType string
	CacheState   string
	Count        int64
}
