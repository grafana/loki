/*
 *
 * Copyright 2023 gRPC authors.
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

// Package experimental is a collection of experimental features that might
// have some rough edges to them. Housing experimental features in this package
// results in a user accessing these APIs as `experimental.Foo`, thereby making
// it explicit that the feature is experimental and using them in production
// code is at their own risk.
//
// All APIs in this package are experimental.
package experimental

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal"
)

// WithRecvBufferPool returns a grpc.DialOption that configures the use of
// bufferPool for parsing incoming messages on a grpc.ClientConn. Depending on
// the application's workload, this could result in reduced memory allocation.
//
// If you are unsure about how to implement a memory pool but want to utilize
// one, begin with grpc.NewSharedBufferPool.
//
// Note: The shared buffer pool feature will not be active if any of the
// following options are used: WithStatsHandler, EnableTracing, or binary
// logging. In such cases, the shared buffer pool will be ignored.
//
// Note: It is not recommended to use the shared buffer pool when compression is
// enabled.
func WithRecvBufferPool(bufferPool grpc.SharedBufferPool) grpc.DialOption {
	return internal.WithRecvBufferPool.(func(grpc.SharedBufferPool) grpc.DialOption)(bufferPool)
}

// RecvBufferPool returns a grpc.ServerOption that configures the server to use
// the provided shared buffer pool for parsing incoming messages. Depending on
// the application's workload, this could result in reduced memory allocation.
//
// If you are unsure about how to implement a memory pool but want to utilize
// one, begin with grpc.NewSharedBufferPool.
//
// Note: The shared buffer pool feature will not be active if any of the
// following options are used: StatsHandler, EnableTracing, or binary logging.
// In such cases, the shared buffer pool will be ignored.
//
// Note: It is not recommended to use the shared buffer pool when compression is
// enabled.
func RecvBufferPool(bufferPool grpc.SharedBufferPool) grpc.ServerOption {
	return internal.RecvBufferPool.(func(grpc.SharedBufferPool) grpc.ServerOption)(bufferPool)
}
