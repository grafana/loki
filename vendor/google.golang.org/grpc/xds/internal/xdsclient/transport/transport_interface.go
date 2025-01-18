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

// Package transport defines the interface that describe the functionality
// required to communicate with an xDS server using streaming calls.
package transport

import (
	"context"

	"google.golang.org/grpc/internal/xds/bootstrap"
)

// Builder is an interface for building a new xDS transport.
type Builder interface {
	// Build creates a new xDS transport with the provided options.
	Build(opts BuildOptions) (Transport, error)
}

// BuildOptions contains the options for building a new xDS transport.
type BuildOptions struct {
	// ServerConfig contains the configuration that controls how the transport
	// interacts with the xDS server. This includes the server URI and the
	// credentials to use to connect to the server, among other things.
	ServerConfig *bootstrap.ServerConfig
}

// Transport provides the functionality to communicate with an xDS server using
// streaming calls.
type Transport interface {
	// CreateStreamingCall creates a new streaming call to the xDS server for the
	// specified method name. The returned StreamingCall interface can be used to
	// send and receive messages on the stream.
	CreateStreamingCall(context.Context, string) (StreamingCall, error)

	// Close closes the underlying connection and cleans up any resources used by the
	// Transport.
	Close() error
}

// StreamingCall is an interface that provides a way to send and receive
// messages on a stream. The methods accept or return any.Any messages instead
// of concrete types to allow this interface to be used for both ADS and LRS.
type StreamingCall interface {
	// Send sends the provided message on the stream.
	Send(any) error

	// Recv block until the next message is received on the stream.
	Recv() (any, error)
}
