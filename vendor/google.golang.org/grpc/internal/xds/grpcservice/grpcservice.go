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

// Package grpcservice parses and validates envoy GrpcService protos into a
// form usable for creating side-channel gRPC connections.
package grpcservice

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"google.golang.org/grpc"
	imetadata "google.golang.org/grpc/internal/metadata"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	maxHeaderKeyLen   = 16384
	maxHeaderValueLen = 16384
)

// Config is the parsed form of a GrpcService proto. It carries the built,
// ready-to-use credentials for the side channel.
type Config struct {
	// TargetURI is the gRPC target URI of the side-channel service.
	TargetURI string
	// Timeout, if non-zero, is the deadline to use for RPCs on the side
	// channel.
	Timeout time.Duration
	// InitialMetadata is the metadata to add to RPCs on the side channel.
	InitialMetadata metadata.MD
	// ChannelCredentials are the channel credentials to create the side
	// channel with, paired with the identity of their source configuration.
	ChannelCredentials *xdscreds.ChannelCreds
	// CallCredentials are the call credentials to apply to RPCs sent on the
	// side channel, paired with the identities of their source
	// configurations, preserving order.
	CallCredentials []*xdscreds.CallCreds
}

// Equal reports whether c and other are equal.
func (c *Config) Equal(other *Config) bool {
	if c == nil || other == nil {
		return c == other
	}
	targetEqual := c.TargetURI == other.TargetURI
	timeoutEqual := c.Timeout == other.Timeout
	metadataEqual := maps.EqualFunc(c.InitialMetadata, other.InitialMetadata, slices.Equal)
	channelCredsEqual := c.ChannelCredentials.Equal(other.ChannelCredentials)
	callCredsEqual := slices.EqualFunc(c.CallCredentials, other.CallCredentials, (*xdscreds.CallCreds).Equal)
	return targetEqual && timeoutEqual && metadataEqual && channelCredsEqual && callCredsEqual
}

// Close releases the credentials owned by the config. It is idempotent, and a
// no-op for credentials owned by another component (e.g. the allowlisted
// credentials owned by the bootstrap config).
func (c *Config) Close() {
	c.ChannelCredentials.Close()
	for _, cc := range c.CallCredentials {
		cc.Close()
	}
}

// Dial creates a channel to the side-channel service, using the channel and
// call credentials from the config along with the provided dial options.
//
// Dial does not take ownership of the config: the caller releases the
// config's credentials via Close when the config is no longer needed, after
// closing any channel created from it.
func (c *Config) Dial(opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	// The credentials are appended after the provided options so that they
	// cannot be overridden.
	dialOpts := make([]grpc.DialOption, 0, len(opts)+len(c.CallCredentials)+1)
	dialOpts = append(dialOpts, opts...)
	dialOpts = append(dialOpts, grpc.WithCredentialsBundle(c.ChannelCredentials.Bundle()))
	for _, cc := range c.CallCredentials {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(cc.Credentials()))
	}
	return grpc.NewClient(c.TargetURI, dialOpts...)
}

// Parse parses and validates a GrpcService proto into a Config, applying the
// gRFC A102 trust policy. bc is the bootstrap configuration of the xDS client
// that received the resource, and sc is the configuration of the management
// server that sent it; both must be non-nil.
//
// Credentials configured in the proto are honored only when the delivering
// management server is trusted, i.e. configured with the trusted_xds_server
// server feature. For untrusted management servers the target must be present
// in the bootstrap allowed_grpc_services map, and the returned Config carries
// the credentials configured there.
//
// The credentials in the returned Config are built and ready to use. The
// caller owns the Config and releases its credentials via Close when it is no
// longer needed, after closing any channel dialed from it.
func Parse(gs *v3corepb.GrpcService, bc *bootstrap.Config, sc *bootstrap.ServerConfig) (*Config, error) {
	googleGrpc := gs.GetGoogleGrpc()
	if googleGrpc == nil {
		return nil, fmt.Errorf("grpcservice: only google_grpc GrpcService config is supported")
	}

	targetURI := googleGrpc.GetTargetUri()
	if targetURI == "" {
		return nil, fmt.Errorf("grpcservice: target_uri must be non-empty")
	}
	if err := validateTargetURI(targetURI); err != nil {
		return nil, err
	}

	cfg := &Config{TargetURI: targetURI}
	var err error
	if cfg.Timeout, err = parseTimeout(gs); err != nil {
		return nil, err
	}
	if cfg.InitialMetadata, err = parseInitialMetadata(gs.GetInitialMetadata()); err != nil {
		return nil, err
	}

	// Credentials are built last, so that no error path can drop built
	// credentials.
	if sc.ServerFeaturesTrustedXDSServer() {
		if cfg.ChannelCredentials, err = buildChannelCredentials(googleGrpc.GetChannelCredentialsPlugin(), bc); err != nil {
			return nil, err
		}
		if cfg.CallCredentials, err = buildCallCredentials(googleGrpc.GetCallCredentialsPlugin()); err != nil {
			cfg.ChannelCredentials.Close()
			return nil, err
		}
	} else {
		svc := bc.AllowedGRPCService(targetURI)
		if svc == nil {
			return nil, fmt.Errorf("grpcservice: target_uri %q is not present in allowed_grpc_services", targetURI)
		}
		// The allowlisted credentials are owned by the bootstrap config:
		// their pairs carry no cleanup, so Config.Close does not affect
		// them.
		cfg.ChannelCredentials, cfg.CallCredentials = svc.SideChannelCredentials()
	}
	return cfg, nil
}

// buildChannelCredentials builds the first channel credential from the plugin
// list whose proto type has a registered builder. It is an error if none of
// the configured plugins are supported, or if building the supported plugin
// fails.
func buildChannelCredentials(plugins []*anypb.Any, bc *bootstrap.Config) (*xdscreds.ChannelCreds, error) {
	for _, p := range plugins {
		if p == nil {
			continue
		}
		b := xdscreds.GetChannelCredsBuilder(p.GetTypeUrl())
		if b == nil {
			continue
		}
		bundle, cleanup, err := b(p, bc)
		if err != nil {
			return nil, fmt.Errorf("grpcservice: failed to build channel credentials %q: %v", p.GetTypeUrl(), err)
		}
		identity := xdscreds.Identity{Type: p.GetTypeUrl(), Data: p.GetValue()}
		return xdscreds.NewChannelCreds(bundle, identity, cleanup), nil
	}
	return nil, fmt.Errorf("grpcservice: no supported channel credentials found in grpc_service")
}

// buildCallCredentials builds the call credentials from the plugin list,
// preserving order. Plugins whose proto type has no registered builder are
// skipped; call credentials are optional, so an empty result is not an error.
func buildCallCredentials(plugins []*anypb.Any) (_ []*xdscreds.CallCreds, err error) {
	var out []*xdscreds.CallCreds
	// Release any credentials built before a mid-iteration failure.
	defer func() {
		if err != nil {
			for _, cc := range out {
				cc.Close()
			}
		}
	}()
	for _, p := range plugins {
		if p == nil {
			continue
		}
		b := xdscreds.GetCallCredsBuilder(p.GetTypeUrl())
		if b == nil {
			continue
		}
		cc, cleanup, err := b(p)
		if err != nil {
			return nil, fmt.Errorf("grpcservice: failed to build call credentials %q: %v", p.GetTypeUrl(), err)
		}
		identity := xdscreds.Identity{Type: p.GetTypeUrl(), Data: p.GetValue()}
		out = append(out, xdscreds.NewCallCreds(cc, identity, cleanup))
	}
	return out, nil
}

// validateTargetURI verifies that the target URI can be handled by a
// registered resolver.
func validateTargetURI(targetURI string) error {
	// Mirror the scheme resolution performed by grpc.NewClient: use the
	// target's scheme if it parses and is registered; otherwise fall back
	// to the default scheme with the whole target as the endpoint.
	if u, err := url.Parse(targetURI); err == nil && resolver.Get(u.Scheme) != nil {
		return nil
	}
	canonicalTarget := resolver.GetDefaultScheme() + ":///" + targetURI
	u, err := url.Parse(canonicalTarget)
	if err != nil {
		return fmt.Errorf("grpcservice: target_uri %q is invalid: %v", targetURI, err)
	}
	if resolver.Get(u.Scheme) == nil {
		return fmt.Errorf("grpcservice: no resolver for default scheme %q", u.Scheme)
	}
	return nil
}

// parseTimeout validates and converts the GrpcService timeout. A zero timeout
// (unset) is allowed; any set value must be strictly positive.
func parseTimeout(gs *v3corepb.GrpcService) (time.Duration, error) {
	d := gs.GetTimeout()
	if d == nil {
		return 0, nil
	}
	if err := d.CheckValid(); err != nil {
		return 0, fmt.Errorf("grpcservice: invalid timeout: %v", err)
	}
	timeout := d.AsDuration()
	if timeout <= 0 {
		return 0, fmt.Errorf("grpcservice: timeout must be strictly positive, got %v", timeout)
	}
	return timeout, nil
}

// parseInitialMetadata validates the HeaderValue protos and returns them as
// metadata.MD, preserving the control-plane order for each key.
func parseInitialMetadata(headers []*v3corepb.HeaderValue) (metadata.MD, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	md := metadata.MD{}
	for _, h := range headers {
		key := h.GetKey()
		// raw_value takes precedence over the legacy value field.
		val := h.GetValue()
		if len(h.GetRawValue()) > 0 {
			val = string(h.GetRawValue())
		}
		if err := validateHeaderKey(key); err != nil {
			return nil, fmt.Errorf("grpcservice: invalid header key %q: %v", key, err)
		}
		if err := validateHeaderValue(key, val); err != nil {
			return nil, fmt.Errorf("grpcservice: invalid value for header key %q: %v", key, err)
		}
		md.Append(key, val)
	}
	return md, nil
}

func validateHeaderKey(key string) error {
	switch {
	case len(key) > maxHeaderKeyLen:
		return fmt.Errorf("header key exceeds maximum allowed length of %d", maxHeaderKeyLen)
	case key == "host":
		return fmt.Errorf("header key cannot be %q", "host")
	case strings.HasPrefix(key, ":"):
		// imetadata.ValidateKey ignores pseudo-headers, but gRFC A102
		// requires them to be rejected.
		return fmt.Errorf("header key cannot start with %q", ":")
	case strings.HasPrefix(key, "grpc-"):
		return fmt.Errorf("header key cannot start with %q", "grpc-")
	}
	return imetadata.ValidateKey(key)
}

func validateHeaderValue(key, val string) error {
	if len(val) > maxHeaderValueLen {
		return fmt.Errorf("header value exceeds maximum allowed length of %d", maxHeaderValueLen)
	}
	// ValidatePair skips value validation for "-bin" keys, which may carry
	// arbitrary bytes.
	return imetadata.ValidatePair(key, val)
}
