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

// Package credentials provides the credentials used for xDS-configured side
// channels (gRFC A102): built channel and call credentials paired with the
// identity of the configuration they were built from, and registries of
// credential builders keyed by proto type URL.
package credentials

import (
	"bytes"
	"fmt"
	"sync"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	xdscredspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/xds/v3"
)

const (
	insecureCredsTypeURL      = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials"
	googleDefaultCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.google_default.v3.GoogleDefaultCredentials"
	xdsCredsTypeURL           = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.xds.v3.XdsCredentials"
)

func init() {
	RegisterChannelCredsBuilder(insecureCredsTypeURL, func(*anypb.Any, CertProviderConfigResolver) (credentials.Bundle, func(), error) {
		return insecure.NewBundle(), func() {}, nil
	})
	RegisterChannelCredsBuilder(googleDefaultCredsTypeURL, func(*anypb.Any, CertProviderConfigResolver) (credentials.Bundle, func(), error) {
		return google.NewDefaultCredentials(), func() {}, nil
	})
	// The xds credential resolves to its required fallback credential, whose
	// builder is looked up in the registry: a side-channel target is not an
	// xDS cluster, so there is no xDS security configuration for it.
	RegisterChannelCredsBuilder(xdsCredsTypeURL, func(config *anypb.Any, resolver CertProviderConfigResolver) (credentials.Bundle, func(), error) {
		var xdsCfg xdscredspb.XdsCredentials
		if err := anypb.UnmarshalTo(config, &xdsCfg, proto.UnmarshalOptions{}); err != nil {
			return nil, nil, fmt.Errorf("credentials: failed to unmarshal XdsCredentials: %v", err)
		}
		fallback := xdsCfg.GetFallbackCredentials()
		if fallback == nil {
			return nil, nil, fmt.Errorf("credentials: xds credentials missing required fallback credentials")
		}
		if b := GetChannelCredsBuilder(fallback.GetTypeUrl()); b != nil {
			return b(fallback, resolver)
		}
		return nil, nil, fmt.Errorf("credentials: unsupported fallback credentials type %q in xds credentials", fallback.GetTypeUrl())
	})
}

// CertProviderConfigResolver resolves certificate provider instance names to
// their configuration. It is implemented by the bootstrap config, and
// declared here so that credential builders do not depend on the bootstrap
// package.
type CertProviderConfigResolver interface {
	CertProviderConfigs() map[string]*certprovider.BuildableConfig
}

// ChannelCredsBuilder builds a channel credentials bundle from a GrpcService
// channel credentials plugin config. The resolver gives access to the
// certificate provider instances configured in the bootstrap config; builders
// that do not reference them ignore it. The returned function releases the
// resources held by the bundle when it is no longer needed.
type ChannelCredsBuilder func(config *anypb.Any, resolver CertProviderConfigResolver) (credentials.Bundle, func(), error)

// CallCredsBuilder builds per-RPC credentials from a GrpcService call
// credentials plugin config. The returned function releases the resources
// held by the credentials when they are no longer needed.
type CallCredsBuilder func(config *anypb.Any) (credentials.PerRPCCredentials, func(), error)

var (
	// channelCredsBuilders is a map from proto type URL to
	// ChannelCredsBuilder.
	channelCredsBuilders = make(map[string]ChannelCredsBuilder)
	// callCredsBuilders is a map from proto type URL to CallCredsBuilder.
	callCredsBuilders = make(map[string]CallCredsBuilder)
)

// RegisterChannelCredsBuilder registers the builder for the given proto type
// URL. Must be called at init time. Not thread safe.
func RegisterChannelCredsBuilder(typeURL string, b ChannelCredsBuilder) {
	channelCredsBuilders[typeURL] = b
}

// GetChannelCredsBuilder returns the builder registered for the given proto
// type URL, or nil if there is none.
func GetChannelCredsBuilder(typeURL string) ChannelCredsBuilder {
	return channelCredsBuilders[typeURL]
}

// RegisterCallCredsBuilder registers the builder for the given proto type
// URL. Must be called at init time. Not thread safe.
func RegisterCallCredsBuilder(typeURL string, b CallCredsBuilder) {
	callCredsBuilders[typeURL] = b
}

// GetCallCredsBuilder returns the builder registered for the given proto type
// URL, or nil if there is none.
func GetCallCredsBuilder(typeURL string) CallCredsBuilder {
	return callCredsBuilders[typeURL]
}

// Identity identifies the configuration a credential was built from.
type Identity struct {
	// Type is the bootstrap credential type name for JSON-sourced
	// credentials, or the proto type URL for proto-sourced ones. The two
	// namespaces cannot collide: proto type URLs contain dots and slashes.
	Type string
	// Data is the raw JSON configuration, or the proto config value bytes.
	Data []byte
}

// Equal reports whether i and other describe the same configuration.
func (i Identity) Equal(other Identity) bool {
	return i.Type == other.Type && bytes.Equal(i.Data, other.Data)
}

// ChannelCreds pairs a built credentials bundle with the identity of the
// configuration it was built from.
type ChannelCreds struct {
	bundle   credentials.Bundle
	identity Identity
	cleanup  func()
}

// NewChannelCreds pairs the given bundle with its identity. cleanup releases
// the resources held by the bundle and is run by Close; it must be nil when
// the bundle is owned by another component (e.g. the bootstrap config), in
// which case Close is a no-op.
func NewChannelCreds(bundle credentials.Bundle, identity Identity, cleanup func()) *ChannelCreds {
	if cleanup != nil {
		cleanup = sync.OnceFunc(cleanup)
	}
	return &ChannelCreds{bundle: bundle, identity: identity, cleanup: cleanup}
}

// Bundle returns the built credentials bundle.
func (c *ChannelCreds) Bundle() credentials.Bundle {
	return c.bundle
}

// Equal reports whether c and other were built from the same configuration.
func (c *ChannelCreds) Equal(other *ChannelCreds) bool {
	if c == nil || other == nil {
		return c == other
	}
	return c.identity.Equal(other.identity)
}

// Close releases the resources held by the bundle, if owned. It is
// idempotent.
func (c *ChannelCreds) Close() {
	if c.cleanup != nil {
		c.cleanup()
	}
}

// CallCreds pairs built per-RPC credentials with the identity of the
// configuration they were built from.
type CallCreds struct {
	creds    credentials.PerRPCCredentials
	identity Identity
	cleanup  func()
}

// NewCallCreds pairs the given per-RPC credentials with their identity.
// cleanup releases the resources held by the credentials and is run by Close;
// it must be nil when the credentials are owned by another component (e.g.
// the bootstrap config), in which case Close is a no-op.
func NewCallCreds(creds credentials.PerRPCCredentials, identity Identity, cleanup func()) *CallCreds {
	if cleanup != nil {
		cleanup = sync.OnceFunc(cleanup)
	}
	return &CallCreds{creds: creds, identity: identity, cleanup: cleanup}
}

// Credentials returns the built per-RPC credentials.
func (c *CallCreds) Credentials() credentials.PerRPCCredentials {
	return c.creds
}

// Equal reports whether c and other were built from the same configuration.
func (c *CallCreds) Equal(other *CallCreds) bool {
	if c == nil || other == nil {
		return c == other
	}
	return c.identity.Equal(other.identity)
}

// Close releases the resources held by the credentials, if owned. It is
// idempotent.
func (c *CallCreds) Close() {
	if c.cleanup != nil {
		c.cleanup()
	}
}
