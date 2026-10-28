// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"context"

	bigtablepb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// FetchClientConfigurationOnce issues a single GetClientConfiguration RPC
// against stub with the standard {instance, appProfile} request shape and
// the given outgoing metadata attached to ctx. Header and trailer are
// captured for callers that surface them (e.g. the debug UI on
// ClientConfigurationManager); the checker path ignores them.
//
// Callers own retry / timeout / metric policy — ClientConfigurationManager
// wraps this in an exponential-backoff loop keyed off the current config's
// MaxRpcRetryCount; the session DirectAccessChecker calls it once and
// treats a failure as a signal to run the async investigation.
func FetchClientConfigurationOnce(
	ctx context.Context,
	stub bigtablepb.BigtableClient,
	instanceName, appProfileID string,
	md metadata.MD,
) (resp *bigtablepb.ClientConfiguration, header, trailer metadata.MD, err error) {
	req := &bigtablepb.GetClientConfigurationRequest{
		InstanceName: instanceName,
		AppProfileId: appProfileID,
	}
	if md != nil {
		ctx = metadata.NewOutgoingContext(ctx, md)
	}
	resp, err = stub.GetClientConfiguration(ctx, req, grpc.Header(&header), grpc.Trailer(&trailer))
	return resp, header, trailer, err
}
