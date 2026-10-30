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

package internal

import (
	"encoding/base64"
	"os"
	"strconv"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// FeatureFlagsHeader is the gRPC metadata key that carries the
// base64-encoded FeatureFlags proto to the Bigtable service on every
// request.
const FeatureFlagsHeader = "bigtable-features"

// FeatureFlagsInput bundles the per-client-path knobs used to
// construct the FeatureFlags proto and matching bigtable-features
// header. Both the classic and session data paths populate this
// struct and call NewFeatureFlagsProto — single source of truth so
// the two paths cannot drift.
type FeatureFlagsInput struct {
	ClientSideMetricsEnabled bool
	EnableDirectAccess       bool
}

// NewFeatureFlagsProto builds the on-wire FeatureFlags proto used by
// both the bigtable-features gRPC header and (on the session path)
// OpenSessionRequest.Flags. The server rejects OpenSession with
// INVALID_ARGUMENT when the two disagree, so both must derive from
// the same proto instance — call this once at construction and
// share the result.
//
// SessionsCompatible / SessionsRequired are driven off the
// CBT_FORCE_SESSION env var (Google-internal tri-state; server gates
// the actual capability): unset → (true, false); "true" → (true, true);
// "false" → (false, false). Applied uniformly across classic and
// session paths so the header stays byte-identical on both.
func NewFeatureFlagsProto(in FeatureFlagsInput) *btpb.FeatureFlags {
	sessionsCompatible, sessionsRequired := true, false
	if v, ok := os.LookupEnv("CBT_FORCE_SESSION"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			sessionsCompatible, sessionsRequired = b, b
		}
	}
	return &btpb.FeatureFlags{
		RoutingCookie:            true,
		ReverseScans:             true,
		LastScannedRowResponses:  true,
		ClientSideMetricsEnabled: in.ClientSideMetricsEnabled,
		// RetryInfo is unconditionally advertised — this client always
		// honors server-attached retry hints; classic and session both
		// ship the same fixed value.
		RetryInfo:              true,
		TrafficDirectorEnabled: in.EnableDirectAccess,
		DirectAccessRequested:  in.EnableDirectAccess,
		SessionsCompatible:     sessionsCompatible,
		SessionsRequired:       sessionsRequired,
		// PeerInfo asks the server to send bigtable-peer-info sideband
		// metadata that populates transport_type/region/zone/subzone
		// on the per-attempt tracer.
		PeerInfo: true,
	}
}

// MarshalFeatureFlagsMD serializes a FeatureFlags proto into the
// base64 bigtable-features gRPC metadata attached to every RPC.
// A marshal failure produces an empty header value — the server
// treats that as "no client-side feature support" rather than a
// hard error.
func MarshalFeatureFlagsMD(ff *btpb.FeatureFlags) metadata.MD {
	val := ""
	if b, err := proto.Marshal(ff); err == nil {
		val = base64.URLEncoding.EncodeToString(b)
	}
	return metadata.Pairs(FeatureFlagsHeader, val)
}
