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

package credentials

import (
	"context"
	"fmt"

	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	accesstokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
)

const accessTokenCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.call_credentials.access_token.v3.AccessTokenCredentials"

func init() {
	RegisterCallCredsBuilder(accessTokenCredsTypeURL, func(config *anypb.Any) (credentials.PerRPCCredentials, func(), error) {
		var accessToken accesstokenpb.AccessTokenCredentials
		if err := anypb.UnmarshalTo(config, &accessToken, proto.UnmarshalOptions{}); err != nil {
			return nil, nil, fmt.Errorf("credentials: failed to unmarshal AccessTokenCredentials: %v", err)
		}
		if accessToken.GetToken() == "" {
			return nil, nil, fmt.Errorf("credentials: access token must be non-empty")
		}
		// These credentials hold no resources; the no-op cleanup satisfies
		// the registry contract.
		return &accessTokenCallCreds{token: accessToken.GetToken()}, func() {}, nil
	})
}

// accessTokenCallCreds implements credentials.PerRPCCredentials by attaching
// a static bearer token to each RPC (gRFC A102). The credentials require
// transport security: the token is only ever sent on connections that provide
// privacy and integrity, and RPCs on weaker connections fail.
type accessTokenCallCreds struct {
	token string
}

// GetRequestMetadata returns the token as an authorization header. It fails
// if the connection does not provide privacy and integrity.
func (c *accessTokenCallCreds) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	ri, _ := credentials.RequestInfoFromContext(ctx)
	if err := credentials.CheckSecurityLevel(ri.AuthInfo, credentials.PrivacyAndIntegrity); err != nil {
		return nil, fmt.Errorf("credentials: unable to transfer access token PerRPCCredentials: %v", err)
	}
	return map[string]string{"authorization": "Bearer " + c.token}, nil
}

// RequireTransportSecurity indicates whether the credentials require
// transport security.
func (c *accessTokenCallCreds) RequireTransportSecurity() bool {
	return true
}
