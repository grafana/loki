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

// Package internal contains functionality internal to the extproc package.
package internal

import (
	"fmt"
	"time"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
)

var (
	// RegisterForTesting registers the external processor HTTP Filter for testing
	// purposes.
	RegisterForTesting func()

	// UnregisterForTesting unregisters the external processor HTTP Filter for
	// testing purposes.
	UnregisterForTesting func()

	// ParseGRPCServiceConfig parses the gRPC service configuration from the given
	// protobuf message.
	ParseGRPCServiceConfig = func(*v3corepb.GrpcService) (xdsresource.GRPCServiceConfig, error) {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("extproc: ParseGRPCServiceConfig not implemented")
	}

	// CreateExtProcChannel creates a gRPC client channel to the external
	// processing server.
	CreateExtProcChannel = func(xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
		return nil, nil, fmt.Errorf("extproc: dialing external processor server not implemented")
	}

	// TimeNowFunc returns the current time.Time, and can be overridden for
	// testing purposes.
	TimeNowFunc func() time.Time

	// TimeSinceFunc returns the time elapsed, and can be overridden for testing
	// purposes.
	TimeSinceFunc func(t time.Time) time.Duration
)
