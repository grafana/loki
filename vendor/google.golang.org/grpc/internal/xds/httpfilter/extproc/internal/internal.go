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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/xds/grpcservice"
)

var (
	// CreateExtProcChannel creates the channel to the external processing
	// server described by the given config. The returned function closes the
	// channel. It is a variable so that tests can intercept channel creation
	// and observe its release.
	CreateExtProcChannel = func(server *grpcservice.Config) (grpc.ClientConnInterface, func(), error) {
		conn, err := server.Dial()
		if err != nil {
			return nil, nil, err
		}
		return conn, func() { conn.Close() }, nil
	}

	// RegisterForTesting registers the external processor HTTP Filter for testing
	// purposes.
	RegisterForTesting func()

	// UnregisterForTesting unregisters the external processor HTTP Filter for
	// testing purposes.
	UnregisterForTesting func()

	// TimeNowFunc returns the current time.Time, and can be overridden for
	// testing purposes.
	TimeNowFunc func() time.Time

	// TimeSinceFunc returns the time elapsed, and can be overridden for testing
	// purposes.
	TimeSinceFunc func(t time.Time) time.Duration

	// DefaultFlowControlWindowSize is the initial flow control window size for
	// the external processor filter and can be overridden for testing purposes.
	DefaultFlowControlWindowSize int64 = 64 * 1024
)
