/*
Copyright 2026 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package internal

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
)

const (
	defaultCluster = "<unspecified>"
	defaultZone    = "global"
	defaultTable   = "<unspecified>"

	// PeerInfoMDKey is the response-metadata key the server uses to
	// carry the serialized PeerInfo proto once the PeerInfo feature
	// flag is negotiated on. extractPeerInfo decodes the value.
	PeerInfoMDKey = "bigtable-peer-info"
)

// canonicalStatusStrings maps standard gRPC status codes to their
// canonical SCREAMING_SNAKE_CASE string form. Indexed by codes.Code so
// CanonicalString is an allocation-free lookup on the status-recording
// hot path.
//
// Hand-rolled rather than delegating to grpc-go's canonicalString
// (unexported: only reachable via grpc/internal/CanonicalString) or
// google.golang.org/genproto/googleapis/rpc/code.Code_name (exported
// but emits "CANCELLED" for Canceled). The bigtable metrics label
// history uses "CANCELED" (single L) — matching the pre-refactor
// upstream code that ran `strings.ToUpper` over grpc-go's
// `codes.Canceled.String()` = "Canceled". Changing to either helper
// would flip the emitted label and break downstream dashboards.
var canonicalStatusStrings = [...]string{
	codes.OK:                 "OK",
	codes.Canceled:           "CANCELED",
	codes.Unknown:            "UNKNOWN",
	codes.InvalidArgument:    "INVALID_ARGUMENT",
	codes.DeadlineExceeded:   "DEADLINE_EXCEEDED",
	codes.NotFound:           "NOT_FOUND",
	codes.AlreadyExists:      "ALREADY_EXISTS",
	codes.PermissionDenied:   "PERMISSION_DENIED",
	codes.ResourceExhausted:  "RESOURCE_EXHAUSTED",
	codes.FailedPrecondition: "FAILED_PRECONDITION",
	codes.Aborted:            "ABORTED",
	codes.OutOfRange:         "OUT_OF_RANGE",
	codes.Unimplemented:      "UNIMPLEMENTED",
	codes.Internal:           "INTERNAL",
	codes.Unavailable:        "UNAVAILABLE",
	codes.DataLoss:           "DATA_LOSS",
	codes.Unauthenticated:    "UNAUTHENTICATED",
}

// CanonicalString returns the SCREAMING_SNAKE_CASE form of a gRPC code.
// See canonicalStatusStrings for why this is hand-rolled.
func CanonicalString(c codes.Code) string {
	if int(c) >= 0 && int(c) < len(canonicalStatusStrings) {
		if s := canonicalStatusStrings[c]; s != "" {
			return s
		}
	}
	// Match grpc-go's canonicalString fallback for out-of-range codes so
	// tests (and log/metric consumers) that expect the "CODE(N)" shape
	// keep working — e.g. metrics_test.go's TestCanonicalString.
	return fmt.Sprintf("CODE(%d)", int(c))
}

// ConvertToGrpcStatusErr returns the gRPC status code for err along
// with an error canonicalized to a plain status.Error so downstream
// logging doesn't leak wrapping/details. Shared by the tracer's
// attempt-completion path and every classic-path RPC method's
// SetCurrOpStatus wrapper. Callers that only need the code use
// GrpcCodeOf instead.
func ConvertToGrpcStatusErr(err error) (codes.Code, error) {
	code := GrpcCodeOf(err)
	if err == nil {
		return code, nil
	}
	if s, ok := status.FromError(err); ok {
		return code, status.Error(code, s.Message())
	}
	if code != codes.Unknown {
		// Context error path — canonicalize with the ctx-derived message.
		return code, status.Error(code, status.FromContextError(err).Message())
	}
	return code, err
}

// extractServerLatency returns the GFE server latency (in milliseconds)
// carried by the server-timing header or trailer metadata, whichever is
// populated. Returns the parse error when the header is present but
// malformed.
func extractServerLatency(headerMD metadata.MD, trailerMD metadata.MD) (float64, error) {
	serverTimingStr := ""

	// Check whether server latency available in response header metadata
	if headerMD != nil {
		headerMDValues := headerMD.Get(ServerTimingMDKey)
		if len(headerMDValues) != 0 {
			serverTimingStr = headerMDValues[0]
		}
	}

	if len(serverTimingStr) == 0 {
		// Check whether server latency available in response trailer metadata
		if trailerMD != nil {
			trailerMDValues := trailerMD.Get(ServerTimingMDKey)
			if len(trailerMDValues) != 0 {
				serverTimingStr = trailerMDValues[0]
			}
		}
	}

	serverLatencyMillisStr := strings.TrimPrefix(serverTimingStr, serverTimingValPrefix)
	serverLatencyMillis, err := strconv.ParseFloat(strings.TrimSpace(serverLatencyMillisStr), 64)
	if !strings.HasPrefix(serverTimingStr, serverTimingValPrefix) || err != nil {
		return serverLatencyMillis, err
	}

	return serverLatencyMillis, nil
}

// extractLocation returns the (cluster, zone) pair encoded in the
// LocationMDKey response metadata. Checks headers first, then
// trailers — different serving environments emit the metadata in one
// or the other.
func extractLocation(headerMD metadata.MD, trailerMD metadata.MD) (string, string, error) {
	var locationMetadata []string

	// Check whether location metadata available in response header metadata
	if headerMD != nil {
		locationMetadata = headerMD.Get(LocationMDKey)
	}

	if locationMetadata == nil {
		// Check whether location metadata available in response trailer metadata
		// if none found in response header metadata
		if trailerMD != nil {
			locationMetadata = trailerMD.Get(LocationMDKey)
		}
	}

	if len(locationMetadata) < 1 {
		return defaultCluster, defaultZone, errors.New("failed to get location metadata")
	}

	// Unmarshal binary location metadata
	responseParams := &btpb.ResponseParams{}
	err := proto.Unmarshal([]byte(locationMetadata[0]), responseParams)
	if err != nil {
		return defaultCluster, defaultZone, err
	}

	return responseParams.GetClusterId(), responseParams.GetZoneId(), nil
}

// extractPeerInfo decodes the bigtable-peer-info sideband metadata (populated
// by the server when the PeerInfo feature flag is negotiated on) and returns
// the parsed PeerInfo. Returns (nil, nil) when the header is absent — the
// caller records the attempt without transport labels in that case. Server
// emits URL-safe base64; any '=' padding is stripped so a single
// RawURLEncoding decoder handles both padded and unpadded shapes (matches
// java-bigtable's Base64.getUrlDecoder()).
func extractPeerInfo(headerMD metadata.MD, trailerMD metadata.MD) (*btpb.PeerInfo, error) {
	var peerInfoData []string
	if headerMD != nil {
		peerInfoData = headerMD.Get(PeerInfoMDKey)
	}
	if len(peerInfoData) == 0 && trailerMD != nil {
		peerInfoData = trailerMD.Get(PeerInfoMDKey)
	}
	if len(peerInfoData) == 0 || peerInfoData[0] == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(peerInfoData[0], "="))
	if err != nil {
		return nil, fmt.Errorf("failed to decode %s from header: %w", PeerInfoMDKey, err)
	}
	var peerInfo btpb.PeerInfo
	if err := proto.Unmarshal(decoded, &peerInfo); err != nil {
		return nil, fmt.Errorf("failed to parse %s protobuf: %w", PeerInfoMDKey, err)
	}
	return &peerInfo, nil
}

// TransportTypeName maps the PeerInfo transport type enum to the short
// label used in metric attributes and debug UIs (e.g. "cloudpath",
// "session_directpath"). Prefer this over .String(), which yields the
// verbose "TRANSPORT_TYPE_…" proto enum names. Exported so the transport
// package's session tracer + debug surfaces share the same mapping
// without duplicating the switch.
func TransportTypeName(tt btpb.PeerInfo_TransportType) string {
	switch tt {
	case btpb.PeerInfo_TRANSPORT_TYPE_EXTERNAL:
		return "external"
	case btpb.PeerInfo_TRANSPORT_TYPE_CLOUD_PATH:
		return "cloudpath"
	case btpb.PeerInfo_TRANSPORT_TYPE_DIRECT_ACCESS:
		return "directpath"
	case btpb.PeerInfo_TRANSPORT_TYPE_SESSION_EXTERNAL:
		return "session_external"
	case btpb.PeerInfo_TRANSPORT_TYPE_SESSION_CLOUD_PATH:
		return "session_cloudpath"
	case btpb.PeerInfo_TRANSPORT_TYPE_SESSION_DIRECT_ACCESS:
		return "session_directpath"
	case btpb.PeerInfo_TRANSPORT_TYPE_SESSION_UNKNOWN:
		return "session_unknown"
	default:
		return "unknown"
	}
}

// ConvertToMs converts a time.Duration to a float64 millisecond value
// suitable for recording on the millisecond-bucketed latency
// histograms.
func ConvertToMs(d time.Duration) float64 {
	return float64(d.Nanoseconds()) / 1000000
}

// GrpcCodeOf extracts the gRPC status code from an error. Maps a nil
// error to codes.OK, a status.Error to its embedded code, a context
// deadline/canceled error to its canonical code, and anything else to
// codes.Unknown. Shared helper so tracer/session paths that only need
// the code (not the wrapped error) don't reimplement the walk.
func GrpcCodeOf(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	if s, ok := status.FromError(err); ok {
		return s.Code()
	}
	if s := status.FromContextError(err); s.Code() != codes.Unknown {
		return s.Code()
	}
	return codes.Unknown
}
