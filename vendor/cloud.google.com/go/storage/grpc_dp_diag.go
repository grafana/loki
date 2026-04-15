// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"context"
	"os"
	"strings"

	"cloud.google.com/go/compute/metadata"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	_ "google.golang.org/grpc/balancer/rls"
	_ "google.golang.org/grpc/xds/googledirectpath"
)

const (
	reasonCustomGRPCConn           = "custom_grpc_conn"
	reasonCustomGRPCConnPool       = "custom_grpc_conn_pool"
	reasonEndpointFetchError       = "endpoint_fetch_error"
	reasonXDSNotEnabled            = "xds_not_enabled"
	reasonOptionDisabled           = "option_disabled"
	reasonUnsupportedEndpoint      = "unsupported_endpoint"
	reasonEnvVarDisabled           = "env_disabled"
	reasonNotOnGCE                 = "not_on_gce"
	reasonNoAuth                   = "no_auth"
	reasonAPIKey                   = "api_key"
	reasonTokenFetchError          = "token_fetch_error"
	reasonNotDefaultServiceAccount = "not_default_service_account"
	reasonCustomHTTPClient         = "custom_http_client"
	reasonUndetermined             = "undetermined"
	reasonInternalError            = "internal_error"

	directPathDisableEnvVar = "GOOGLE_CLOUD_DISABLE_DIRECT_PATH"

	defaultKey             = "default"
	serviceAccountTokenKey = "instance/service-accounts/default/token"
)

// directPathDiagnostic evaluates the provided options and environment to determine
// why gRPC DirectPath (high-throughput VPC routing) is not being utilized.
func directPathDiagnostic(ctx context.Context, opts ...option.ClientOption) string {
	if strings.EqualFold(os.Getenv(directPathDisableEnvVar), "true") {
		return reasonEnvVarDisabled
	}

	res, err := internaloption.NewUnsafeResolver(opts...)
	if err != nil {
		return reasonInternalError
	}

	if !res.ResolvedEnableDirectPath() {
		return reasonOptionDisabled
	}

	endpoint, err := res.ResolvedGRPCEndpoint()
	if err != nil {
		return reasonEndpointFetchError
	}

	if !isDirectPathCompatible(endpoint) {
		return reasonUnsupportedEndpoint
	}

	if !res.ResolvedEnableDirectPathXds() {
		return reasonXDSNotEnabled
	}

	if res.ResolvedGRPCConnIsCustom() {
		return reasonCustomGRPCConn
	}

	if res.ResolvedHTTPClientIsCustom() {
		return reasonCustomHTTPClient
	}

	if !metadata.OnGCE() {
		return reasonNotOnGCE
	}

	return authDiagnostic(res)
}

func authDiagnostic(res *internaloption.UnsafeResolver) string {
	if res.ResolvedWithoutAuthentication() {
		return reasonNoAuth
	}
	if res.ResolvedWithAPIKeyIsCustom() {
		return reasonAPIKey
	}

	// Verify that a default service account is attached.
	if _, err := metadata.Email(defaultKey); err != nil {
		return reasonNotDefaultServiceAccount
	}

	// Verify that a token can be fetched.
	if _, err := metadata.Get(serviceAccountTokenKey); err != nil {
		return reasonTokenFetchError
	}

	return reasonUndetermined
}

func isDirectPathCompatible(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	// DirectPath requires no scheme or the dns:/// scheme specifically.
	if strings.Contains(endpoint, "://") && !strings.HasPrefix(endpoint, "dns:///") {
		return false
	}
	return true
}
