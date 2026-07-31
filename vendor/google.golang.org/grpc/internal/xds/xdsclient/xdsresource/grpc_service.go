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
 */

package xdsresource

import (
	"time"

	"google.golang.org/grpc/metadata"
)

// GRPCServiceConfig contains the configuration for an external server. It is
// the parsed configuration for the GrpcService proto message.
// See: https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/core/v3/grpc_service.proto
type GRPCServiceConfig struct {
	// TargetURI is the name of the external server.
	TargetURI string
	// ChannelCredentials specifies the configuration for the transport
	// credentials to use to connect to the external server, as a JSON string.
	ChannelCredentials string
	// CallCredentials specifies the configuration for the per-RPC credentials to
	// use when making calls to the external server, as a JSON string.
	CallCredentials string
	// Timeout is the RPC timeout for the call to the external server. If unset,
	// the timeout depends on the usage of this external server. For example,
	// cases like ext_authz and ext_proc, where there is a 1:1 mapping between the
	// data plane RPC and the external server call, the timeout will be capped by
	// the timeout on the data plane RPC. For cases like RLQS where there is a
	// side channel to the external server, an unset timeout will result in no
	// timeout being applied to the external server call.
	Timeout time.Duration
	// InitialMetadata is the additional metadata to include in all RPCs sent to
	// the external server.
	InitialMetadata metadata.MD
}
