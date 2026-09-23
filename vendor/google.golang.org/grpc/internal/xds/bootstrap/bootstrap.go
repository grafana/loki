/*
 *
 * Copyright 2019 gRPC authors.
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

// Package bootstrap provides the functionality to initialize certain aspects
// of an xDS client by reading a bootstrap file.
package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/xds/bootstrap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

const (
	serverFeaturesIgnoreResourceDeletion = "ignore_resource_deletion"
	serverFeaturesTrustedXDSServer       = "trusted_xds_server"
	gRPCUserAgentName                    = "gRPC Go"
	clientFeatureNoOverprovisioning      = "envoy.lb.does_not_support_overprovisioning"
	clientFeatureResourceWrapper         = "xds.config.resource-in-sotw"
)

// For overriding in unit tests.
var bootstrapFileReadFunc = os.ReadFile

// ChannelCreds contains the credentials to be used while communicating with an
// xDS server. It is also used to dedup servers with the same server URI.
//
// This type does not implement custom JSON marshal/unmarshal logic because it
// is straightforward to accomplish the same with json struct tags.
type ChannelCreds struct {
	// Type contains a unique name identifying the credentials type. The only
	// supported types currently are "google_default" and "insecure".
	Type string `json:"type,omitempty"`
	// Config contains the JSON configuration associated with the credentials.
	Config json.RawMessage `json:"config,omitempty"`
}

// Equal reports whether cc and other are considered equal.
func (cc ChannelCreds) Equal(other ChannelCreds) bool {
	return cc.Type == other.Type && bytes.Equal(cc.Config, other.Config)
}

// String returns a string representation of the credentials. It contains the
// type and the config (if non-nil) separated by a "-".
func (cc ChannelCreds) String() string {
	if cc.Config == nil {
		return cc.Type
	}

	// We do not expect the Marshal call to fail since we wrote to cc.Config
	// after a successful unmarshalling from JSON configuration. Therefore,
	// it is safe to ignore the error here.
	b, _ := json.Marshal(cc.Config)
	return cc.Type + "-" + string(b)
}

// CallCredsConfig contains the call credentials configuration to be used on
// RPCs to the management server.
type CallCredsConfig struct {
	// Type contains a name identifying the call credentials type.
	Type string `json:"type,omitempty"`
	// Config contains the JSON configuration for this call credentials.
	// Optional as per gRFC A97.
	Config json.RawMessage `json:"config,omitempty"`
}

// Equal reports whether cc and other are considered equal.
func (cc CallCredsConfig) Equal(other CallCredsConfig) bool {
	return cc.Type == other.Type && bytes.Equal(cc.Config, other.Config)
}

func (cc CallCredsConfig) String() string {
	if len(cc.Config) == 0 {
		return cc.Type
	}
	// We do not expect the Marshal call to fail since we wrote to cc.Config.
	b, _ := json.Marshal(cc.Config)
	return cc.Type + "-" + string(b)
}

// CallCredsConfigs represents a collection of call credentials configurations.
type CallCredsConfigs []CallCredsConfig

func (ccs CallCredsConfigs) String() string {
	var creds []string
	for _, cc := range ccs {
		creds = append(creds, cc.String())
	}
	return strings.Join(creds, ",")
}

// AllowedGRPCService contains credentials config for an allowed gRPC service.
type AllowedGRPCService struct {
	// targetURI is the fully-qualified side-channel target this entry
	// applies to. It is the map key under which the service is stored,
	// copied in during parsing so the service is self-describing.
	targetURI string
	// channelCreds is the list of channel-credential configs from the
	// bootstrap JSON. Kept for Equal and MarshalJSON.
	channelCreds []ChannelCreds
	// callCredsConfigs is the list of call-credential configs from the
	// bootstrap JSON. Kept for Equal and MarshalJSON.
	callCredsConfigs []CallCredsConfig
	// selectedChannelCreds is the first channel-creds entry whose type the
	// client supports; it is the one used to build the side channel.
	selectedChannelCreds ChannelCreds
	// dialOptions are built from the selected channel and call credentials
	// and passed to grpc.NewClient when creating the side channel.
	dialOptions []grpc.DialOption
	// cleanups release resources (credential bundles, file watchers) built
	// for this service; run when the owning Config is no longer needed.
	cleanups []func()
}

// TargetURI returns the fully-qualified target URI this service applies to.
func (a *AllowedGRPCService) TargetURI() string {
	return a.targetURI
}

// DialOptions returns the dial options built from this service's selected
// channel and call credentials, for use when creating the side channel.
func (a *AllowedGRPCService) DialOptions() []grpc.DialOption {
	return a.dialOptions
}

// Cleanups returns cleanups to run when the service is no longer needed.
func (a *AllowedGRPCService) Cleanups() []func() {
	return a.cleanups
}

// Equal reports whether a and other are considered equal.
func (a *AllowedGRPCService) Equal(other *AllowedGRPCService) bool {
	if a == nil && other == nil {
		return true
	}
	if a == nil || other == nil {
		return false
	}
	if a.targetURI != other.targetURI {
		return false
	}
	if !slices.EqualFunc(a.channelCreds, other.channelCreds, ChannelCreds.Equal) {
		return false
	}
	return slices.EqualFunc(a.callCredsConfigs, other.callCredsConfigs, CallCredsConfig.Equal)
}

type allowedGRPCServiceJSON struct {
	ChannelCreds     []ChannelCreds    `json:"channel_creds,omitempty"`
	CallCredsConfigs []CallCredsConfig `json:"call_creds,omitempty"`
}

// allowedGRPCServicesEnabled reports whether any feature that consumes the
// allowed_grpc_services field is enabled. Per gRFC A102 the field has no
// dedicated env var; it is guarded by the env vars of its consuming features
// (currently only ext_proc on the client; OR in ext_authz/RLQS guards as
// those features are implemented).
func allowedGRPCServicesEnabled() bool {
	return envconfig.XDSClientExtProcEnabled
}

// AllowedGRPCServices maps a target URI to the credentials gRPC may use for
// that side channel when the xDS server that configured it is untrusted
// (gRFC A102).
type AllowedGRPCServices map[string]*AllowedGRPCService

// UnmarshalJSON parses and validates the allowed_grpc_services field. The
// field is parsed only when a consuming feature is enabled, so a disabled
// feature can neither build credentials nor fail bootstrap.
func (s *AllowedGRPCServices) UnmarshalJSON(data []byte) error {
	if !allowedGRPCServicesEnabled() {
		return nil
	}
	services := make(map[string]*AllowedGRPCService)
	if err := json.Unmarshal(data, &services); err != nil {
		return err
	}
	// A JSON null decodes to a nil entry without calling UnmarshalJSON; reject
	// it since channel_creds is required, and stamp the target on each service.
	for target, svc := range services {
		if svc == nil {
			return fmt.Errorf("xds: allowed_grpc_services entry %q has no configuration in bootstrap config", target)
		}
		svc.targetURI = target
	}
	*s = services
	return nil
}

// UnmarshalJSON parses the JSON data and validates its credentials.
func (a *AllowedGRPCService) UnmarshalJSON(data []byte) (err error) {
	var jsonS allowedGRPCServiceJSON
	if err := json.Unmarshal(data, &jsonS); err != nil {
		return err
	}

	// Build into locals and assign the receiver only on success, so a
	// mid-parse error never leaves the receiver partially mutated. Credentials
	// built before a failure are released by this deferred cleanup.
	var cleanups []func()
	defer func() {
		if err != nil {
			for _, f := range cleanups {
				f()
			}
		}
	}()

	var credsDialOption grpc.DialOption
	var selectedChannelCreds ChannelCreds
	for _, cc := range jsonS.ChannelCreds {
		c := bootstrap.GetChannelCredentials(cc.Type)
		if c == nil {
			continue
		}
		bundle, cancel, err := c.Build(cc.Config)
		if err != nil {
			return fmt.Errorf("xds: failed to build credentials bundle from bootstrap for allowed grpc service: type %q, err: %v", cc.Type, err)
		}
		selectedChannelCreds = cc
		credsDialOption = grpc.WithCredentialsBundle(bundle)
		cleanups = append(cleanups, cancel)
		break
	}

	// If no channel-creds type in the list was supported, credsDialOption is
	// still nil after the loop; that is a validation error.
	if credsDialOption == nil {
		return fmt.Errorf("xds: no supported channel credentials found for allowed grpc service in config:\n%s", string(data))
	}
	dialOptions := []grpc.DialOption{credsDialOption}

	for _, cfg := range jsonS.CallCredsConfigs {
		c := bootstrap.GetCallCredentials(cfg.Type)
		if c == nil {
			continue
		}
		callCreds, cancel, err := c.Build(cfg.Config)
		if err != nil {
			return fmt.Errorf("xds: failed to build call credentials from bootstrap for allowed grpc service: type %q, err: %v", cfg.Type, err)
		}
		dialOptions = append(dialOptions, grpc.WithPerRPCCredentials(callCreds))
		cleanups = append(cleanups, cancel)
	}

	a.channelCreds = jsonS.ChannelCreds
	a.callCredsConfigs = jsonS.CallCredsConfigs
	a.selectedChannelCreds = selectedChannelCreds
	a.dialOptions = dialOptions
	a.cleanups = cleanups
	return nil
}

// MarshalJSON marshals the allowed gRPC service into JSON format.
func (a *AllowedGRPCService) MarshalJSON() ([]byte, error) {
	return json.Marshal(allowedGRPCServiceJSON{
		ChannelCreds:     a.channelCreds,
		CallCredsConfigs: a.callCredsConfigs,
	})
}

// ServerConfigs represents a collection of server configurations.
type ServerConfigs []*ServerConfig

// Equal returns true if scs equals other.
func (scs *ServerConfigs) Equal(other *ServerConfigs) bool {
	if len(*scs) != len(*other) {
		return false
	}
	for i := range *scs {
		if !(*scs)[i].Equal((*other)[i]) {
			return false
		}
	}
	return true
}

// UnmarshalJSON takes the json data (a list of server configurations) and
// unmarshals it to the struct.
func (scs *ServerConfigs) UnmarshalJSON(data []byte) error {
	servers := []*ServerConfig{}
	if err := json.Unmarshal(data, &servers); err != nil {
		return fmt.Errorf("xds: failed to JSON unmarshal server configurations during bootstrap: %v, config:\n%s", err, string(data))
	}
	*scs = servers
	return nil
}

// String returns a string representation of the ServerConfigs, by concatenating
// the string representations of the underlying server configs.
func (scs *ServerConfigs) String() string {
	ret := ""
	for i, sc := range *scs {
		if i > 0 {
			ret += ", "
		}
		ret += sc.String()
	}
	return ret
}

// Authority contains configuration for an xDS control plane authority.
//
// This type does not implement custom JSON marshal/unmarshal logic because it
// is straightforward to accomplish the same with json struct tags.
type Authority struct {
	// ClientListenerResourceNameTemplate is template for the name of the
	// Listener resource to subscribe to for a gRPC client channel.  Used only
	// when the channel is created using an "xds:" URI with this authority name.
	//
	// The token "%s", if present in this string, will be replaced
	// with %-encoded service authority (i.e., the path part of the target
	// URI used to create the gRPC channel).
	//
	// Must start with "xdstp://<authority_name>/".  If it does not,
	// that is considered a bootstrap file parsing error.
	//
	// If not present in the bootstrap file, defaults to
	// "xdstp://<authority_name>/envoy.config.listener.v3.Listener/%s".
	ClientListenerResourceNameTemplate string `json:"client_listener_resource_name_template,omitempty"`
	// XDSServers contains the list of server configurations for this authority.
	XDSServers ServerConfigs `json:"xds_servers,omitempty"`
}

// Equal returns true if a equals other.
func (a *Authority) Equal(other *Authority) bool {
	switch {
	case a == nil && other == nil:
		return true
	case (a != nil) != (other != nil):
		return false
	case a.ClientListenerResourceNameTemplate != other.ClientListenerResourceNameTemplate:
		return false
	case !a.XDSServers.Equal(&other.XDSServers):
		return false
	}
	return true
}

// ServerConfig contains the configuration to connect to a server.
type ServerConfig struct {
	serverURI string
	// TODO: rename ChannelCreds to ChannelCredsConfigs for consistency with
	// CallCredsConfigs.
	channelCreds     []ChannelCreds
	callCredsConfigs []CallCredsConfig
	serverFeatures   []string

	// As part of unmarshalling the JSON config into this struct, we ensure that
	// the credentials config is valid by building an instance of the specified
	// credentials and store it here for easy access.
	selectedChannelCreds ChannelCreds
	selectedCallCreds    []credentials.PerRPCCredentials
	credsDialOption      grpc.DialOption
	extraDialOptions     []grpc.DialOption

	cleanups []func()
}

// ServerURI returns the URI of the management server to connect to.
func (sc *ServerConfig) ServerURI() string {
	return sc.serverURI
}

// ChannelCreds returns the credentials configuration to use when communicating
// with this server. Also used to dedup servers with the same server URI.
func (sc *ServerConfig) ChannelCreds() []ChannelCreds {
	return sc.channelCreds
}

// ServerFeatures returns the list of features supported by this server. Also
// used to dedup servers with the same server URI and channel creds.
func (sc *ServerConfig) ServerFeatures() []string {
	return sc.serverFeatures
}

// CallCredsConfigs returns the call credentials configuration for this server.
func (sc *ServerConfig) CallCredsConfigs() CallCredsConfigs {
	return sc.callCredsConfigs
}

// ServerFeaturesIgnoreResourceDeletion returns true if this server supports a
// feature where the xDS client can ignore resource deletions from this server,
// as described in gRFC A53.
//
// This feature controls the behavior of the xDS client when the server deletes
// a previously sent Listener or Cluster resource. If set, the xDS client will
// not invoke the watchers' ResourceError() method when a resource is
// deleted, nor will it remove the existing resource value from its cache.
func (sc *ServerConfig) ServerFeaturesIgnoreResourceDeletion() bool {
	for _, sf := range sc.serverFeatures {
		if sf == serverFeaturesIgnoreResourceDeletion {
			return true
		}
	}
	return false
}

// ServerFeaturesTrustedXDSServer returns true if this server is trusted,
// and gRPC should accept security-config-affecting fields from the server
// as described in gRFC A81.
func (sc *ServerConfig) ServerFeaturesTrustedXDSServer() bool {
	for _, sf := range sc.serverFeatures {
		if sf == serverFeaturesTrustedXDSServer {
			return true
		}
	}
	return false
}

// SelectedChannelCreds returns the selected credentials configuration for
// communicating with this server.
func (sc *ServerConfig) SelectedChannelCreds() ChannelCreds {
	return sc.selectedChannelCreds
}

// DialOptions returns a slice of all the configured dial options for this
// server except grpc.WithCredentialsBundle().
func (sc *ServerConfig) DialOptions() []grpc.DialOption {
	var dopts []grpc.DialOption
	if sc.extraDialOptions != nil {
		dopts = append(dopts, sc.extraDialOptions...)
	}
	return dopts
}

// Cleanups returns a collection of functions to be called when the xDS client
// for this server is closed. Allows cleaning up resources created specifically
// for this server.
func (sc *ServerConfig) Cleanups() []func() {
	return sc.cleanups
}

// Equal reports whether sc and other are considered equal.
func (sc *ServerConfig) Equal(other *ServerConfig) bool {
	switch {
	case sc == nil && other == nil:
		return true
	case (sc != nil) != (other != nil):
		return false
	case sc.serverURI != other.serverURI:
		return false
	case !slices.EqualFunc(sc.channelCreds, other.channelCreds, func(a, b ChannelCreds) bool { return a.Equal(b) }):
		return false
	case !slices.EqualFunc(sc.callCredsConfigs, other.callCredsConfigs, func(a, b CallCredsConfig) bool { return a.Equal(b) }):
		return false
	case !slices.Equal(sc.serverFeatures, other.serverFeatures):
		return false
	}
	return true
}

// String returns the string representation of the ServerConfig.
func (sc *ServerConfig) String() string {
	if len(sc.serverFeatures) == 0 {
		return strings.Join([]string{sc.serverURI, sc.selectedChannelCreds.String(), sc.CallCredsConfigs().String()}, "-")
	}
	features := strings.Join(sc.serverFeatures, "-")
	return strings.Join([]string{sc.serverURI, sc.selectedChannelCreds.String(), features, sc.CallCredsConfigs().String()}, "-")
}

// The following fields correspond 1:1 with the JSON schema for ServerConfig.
type serverConfigJSON struct {
	ServerURI        string            `json:"server_uri,omitempty"`
	ChannelCreds     []ChannelCreds    `json:"channel_creds,omitempty"`
	CallCredsConfigs []CallCredsConfig `json:"call_creds,omitempty"`
	ServerFeatures   []string          `json:"server_features,omitempty"`
}

// MarshalJSON returns marshaled JSON bytes corresponding to this server config.
func (sc *ServerConfig) MarshalJSON() ([]byte, error) {
	server := &serverConfigJSON{
		ServerURI:        sc.serverURI,
		ChannelCreds:     sc.channelCreds,
		CallCredsConfigs: sc.callCredsConfigs,
		ServerFeatures:   sc.serverFeatures,
	}
	return json.Marshal(server)
}

// extraDialOptions captures custom dial options specified via
// credentials.Bundle.
type extraDialOptions interface {
	DialOptions() []grpc.DialOption
}

// UnmarshalJSON takes the json data (a server) and unmarshals it to the struct.
func (sc *ServerConfig) UnmarshalJSON(data []byte) error {
	server := serverConfigJSON{}
	if err := json.Unmarshal(data, &server); err != nil {
		return fmt.Errorf("xds: failed to JSON unmarshal server configuration during bootstrap: %v, config:\n%s", err, string(data))
	}

	sc.serverURI = server.ServerURI
	sc.channelCreds = server.ChannelCreds
	sc.callCredsConfigs = server.CallCredsConfigs
	sc.serverFeatures = server.ServerFeatures

	for _, cc := range server.ChannelCreds {
		// We stop at the first credential type that we support.
		c := bootstrap.GetChannelCredentials(cc.Type)
		if c == nil {
			continue
		}
		bundle, cancel, err := c.Build(cc.Config)
		if err != nil {
			return fmt.Errorf("failed to build credentials bundle from bootstrap for %q: %v", cc.Type, err)
		}
		sc.selectedChannelCreds = cc
		sc.credsDialOption = grpc.WithCredentialsBundle(bundle)
		if d, ok := bundle.(extraDialOptions); ok {
			sc.extraDialOptions = d.DialOptions()
		}
		sc.cleanups = append(sc.cleanups, cancel)
		break
	}

	if envconfig.XDSBootstrapCallCredsEnabled {
		// Process call credentials - unlike channel creds, we use ALL supported
		// types. Also, call credentials are optional as per gRFC A97.
		for _, cfg := range server.CallCredsConfigs {
			c := bootstrap.GetCallCredentials(cfg.Type)
			if c == nil {
				// Skip unsupported call credential types (don't fail bootstrap).
				continue
			}
			callCreds, cancel, err := c.Build(cfg.Config)
			if err != nil {
				// Call credential validation failed - this should fail bootstrap.
				return fmt.Errorf("failed to build call credentials from bootstrap for %q: %v", cfg.Type, err)
			}
			sc.selectedCallCreds = append(sc.selectedCallCreds, callCreds)
			sc.extraDialOptions = append(sc.extraDialOptions, grpc.WithPerRPCCredentials(callCreds))
			sc.cleanups = append(sc.cleanups, cancel)
		}
	}

	if sc.serverURI == "" {
		return fmt.Errorf("xds: `server_uri` field in server config cannot be empty: %s", string(data))
	}
	if sc.credsDialOption == nil {
		return fmt.Errorf("xds: `channel_creds` field in server config cannot be empty: %s", string(data))
	}
	return nil
}

// ServerConfigTestingOptions specifies options for creating a new ServerConfig
// for testing purposes.
//
// # Testing-Only
type ServerConfigTestingOptions struct {
	// URI is the name of the server corresponding to this server config.
	URI string
	// ChannelCreds contains a list of channel credentials to use when talking
	// to this server. If unspecified, `insecure` credentials will be used.
	ChannelCreds []ChannelCreds
	// CallCredsConfigs contains a list of call credentials to use for individual RPCs
	// to this server. Optional.
	CallCredsConfigs []CallCredsConfig
	// ServerFeatures represents the list of features supported by this server.
	ServerFeatures []string
}

// ServerConfigForTesting creates a new ServerConfig from the passed in options,
// for testing purposes.
//
// # Testing-Only
func ServerConfigForTesting(opts ServerConfigTestingOptions) (*ServerConfig, error) {
	cc := opts.ChannelCreds
	if cc == nil {
		cc = []ChannelCreds{{Type: "insecure"}}
	}
	scInternal := &serverConfigJSON{
		ServerURI:        opts.URI,
		ChannelCreds:     cc,
		CallCredsConfigs: opts.CallCredsConfigs,
		ServerFeatures:   opts.ServerFeatures,
	}
	scJSON, err := json.Marshal(scInternal)
	if err != nil {
		return nil, err
	}

	sc := new(ServerConfig)
	if err := sc.UnmarshalJSON(scJSON); err != nil {
		return nil, err
	}
	return sc, nil
}

// Config is the internal representation of the bootstrap configuration provided
// to the xDS client.
type Config struct {
	xDSServers                                ServerConfigs
	cpcs                                      map[string]certproviderNameAndConfig
	serverListenerResourceNameTemplate        string
	clientDefaultListenerResourceNameTemplate string
	authorities                               map[string]*Authority
	node                                      node

	// A map from certprovider instance names to parsed buildable configs.
	certProviderConfigs map[string]*certprovider.BuildableConfig

	// allowedGRPCServices is the side-channel allowlist parsed from the
	// allowed_grpc_services field (gRFC A102).
	allowedGRPCServices AllowedGRPCServices
}

// AllowedGRPCServices returns the allowlist of gRPC services.
// Callers must not modify the returned map.
func (c *Config) AllowedGRPCServices() AllowedGRPCServices {
	return c.allowedGRPCServices
}

// XDSServers returns the top-level list of management servers to connect to,
// ordered by priority.
func (c *Config) XDSServers() ServerConfigs {
	return c.xDSServers
}

// CertProviderConfigs returns a map from certificate provider plugin instance
// name to their configuration. Callers must not modify the returned map.
func (c *Config) CertProviderConfigs() map[string]*certprovider.BuildableConfig {
	return c.certProviderConfigs
}

// ServerListenerResourceNameTemplate returns template for the name of the
// Listener resource to subscribe to for a gRPC server.
//
// If starts with "xdstp:", will be interpreted as a new-style name,
// in which case the authority of the URI will be used to select the
// relevant configuration in the "authorities" map.
//
// The token "%s", if present in this string, will be replaced with the IP
// and port on which the server is listening.  (e.g., "0.0.0.0:8080",
// "[::]:8080"). For example, a value of "example/resource/%s" could become
// "example/resource/0.0.0.0:8080". If the template starts with "xdstp:",
// the replaced string will be %-encoded.
//
// There is no default; if unset, xDS-based server creation fails.
func (c *Config) ServerListenerResourceNameTemplate() string {
	return c.serverListenerResourceNameTemplate
}

// ClientDefaultListenerResourceNameTemplate returns a template for the name of
// the Listener resource to subscribe to for a gRPC client channel.  Used only
// when the channel is created with an "xds:" URI with no authority.
//
// If starts with "xdstp:", will be interpreted as a new-style name,
// in which case the authority of the URI will be used to select the
// relevant configuration in the "authorities" map.
//
// The token "%s", if present in this string, will be replaced with
// the service authority (i.e., the path part of the target URI
// used to create the gRPC channel).  If the template starts with
// "xdstp:", the replaced string will be %-encoded.
//
// Defaults to "%s".
func (c *Config) ClientDefaultListenerResourceNameTemplate() string {
	return c.clientDefaultListenerResourceNameTemplate
}

// Authorities returns a map of authority name to corresponding configuration.
// Callers must not modify the returned map.
//
// This is used in the following cases:
//   - A gRPC client channel is created using an "xds:" URI that includes
//     an authority.
//   - A gRPC client channel is created using an "xds:" URI with no
//     authority, but the "client_default_listener_resource_name_template"
//     field above turns it into an "xdstp:" URI.
//   - A gRPC server is created and the
//     "server_listener_resource_name_template" field is an "xdstp:" URI.
//
// In any of those cases, it is an error if the specified authority is
// not present in this map.
func (c *Config) Authorities() map[string]*Authority {
	return c.authorities
}

// Node returns xDS a v3 Node proto corresponding to the node field in the
// bootstrap configuration, which identifies a specific gRPC instance.
func (c *Config) Node() *v3corepb.Node {
	return c.node.toProto()
}

// Equal returns true if c equals other.
func (c *Config) Equal(other *Config) bool {
	switch {
	case c == nil && other == nil:
		return true
	case (c != nil) != (other != nil):
		return false
	case !c.xDSServers.Equal(&other.xDSServers):
		return false
	case !maps.EqualFunc(c.certProviderConfigs, other.certProviderConfigs, func(a, b *certprovider.BuildableConfig) bool { return a.String() == b.String() }):
		return false
	case c.serverListenerResourceNameTemplate != other.serverListenerResourceNameTemplate:
		return false
	case c.clientDefaultListenerResourceNameTemplate != other.clientDefaultListenerResourceNameTemplate:
		return false
	case !maps.EqualFunc(c.authorities, other.authorities, (*Authority).Equal):
		return false
	case !maps.EqualFunc(c.allowedGRPCServices, other.allowedGRPCServices, (*AllowedGRPCService).Equal):
		return false
	case !c.node.Equal(other.node):
		return false
	}
	return true
}

// String returns a string representation of the Config.
func (c *Config) String() string {
	s, _ := c.MarshalJSON()
	return string(s)
}

// The following fields correspond 1:1 with the JSON schema for Config.
type configJSON struct {
	XDSServers                                ServerConfigs                        `json:"xds_servers,omitempty"`
	CertificateProviders                      map[string]certproviderNameAndConfig `json:"certificate_providers,omitempty"`
	ServerListenerResourceNameTemplate        string                               `json:"server_listener_resource_name_template,omitempty"`
	ClientDefaultListenerResourceNameTemplate string                               `json:"client_default_listener_resource_name_template,omitempty"`
	Authorities                               map[string]*Authority                `json:"authorities,omitempty"`
	Node                                      node                                 `json:"node,omitempty"`
	AllowedGRPCServices                       AllowedGRPCServices                  `json:"allowed_grpc_services,omitempty"`
}

// MarshalJSON returns marshaled JSON bytes corresponding to this config.
func (c *Config) MarshalJSON() ([]byte, error) {
	config := &configJSON{
		XDSServers:                                c.xDSServers,
		CertificateProviders:                      c.cpcs,
		ServerListenerResourceNameTemplate:        c.serverListenerResourceNameTemplate,
		ClientDefaultListenerResourceNameTemplate: c.clientDefaultListenerResourceNameTemplate,
		Authorities:                               c.authorities,
		Node:                                      c.node,
		AllowedGRPCServices:                       c.allowedGRPCServices,
	}
	return json.MarshalIndent(config, " ", " ")
}

// UnmarshalJSON takes the json data (the complete bootstrap configuration) and
// unmarshals it to the struct.
func (c *Config) UnmarshalJSON(data []byte) error {
	// Initialize the node field with client controlled values. This ensures
	// even if the bootstrap configuration did not contain the node field, we
	// will have a node field with client controlled fields alone.
	config := configJSON{Node: newNode()}

	// Credentials (bundles, file watchers) eagerly built during unmarshaling
	// are intentionally not released if parsing fails below: a failed
	// bootstrap is parsed once per process (the xDS client pool memoizes
	// GetConfiguration via sync.OnceValues) and is treated as fatal, so the
	// leak is bounded and one-time.
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("xds: json.Unmarshal(%s) failed during bootstrap: %v", string(data), err)
	}

	c.xDSServers = config.XDSServers
	c.cpcs = config.CertificateProviders
	c.serverListenerResourceNameTemplate = config.ServerListenerResourceNameTemplate
	c.clientDefaultListenerResourceNameTemplate = config.ClientDefaultListenerResourceNameTemplate
	c.authorities = config.Authorities
	c.node = config.Node
	c.allowedGRPCServices = config.AllowedGRPCServices

	// Build the certificate providers configuration to ensure that it is valid.
	cpcCfgs := make(map[string]*certprovider.BuildableConfig)
	getBuilder := internal.GetCertificateProviderBuilder.(func(string) certprovider.Builder)
	for instance, nameAndConfig := range c.cpcs {
		name := nameAndConfig.PluginName
		parser := getBuilder(nameAndConfig.PluginName)
		if parser == nil {
			// We ignore plugins that we do not know about.
			continue
		}
		bc, err := parser.ParseConfig(nameAndConfig.Config)
		if err != nil {
			return fmt.Errorf("xds: config parsing for certificate provider plugin %q failed during bootstrap: %v", name, err)
		}
		cpcCfgs[instance] = bc
	}
	c.certProviderConfigs = cpcCfgs

	// Default value of the default client listener name template is "%s".
	if c.clientDefaultListenerResourceNameTemplate == "" {
		c.clientDefaultListenerResourceNameTemplate = "%s"
	}
	if len(c.xDSServers) == 0 {
		return fmt.Errorf("xds: required field `xds_servers` not found in bootstrap configuration: %s", string(data))
	}

	// Post-process the authorities' client listener resource template field:
	// - if set, it must start with "xdstp://<authority_name>/"
	// - if not set, it defaults to "xdstp://<authority_name>/envoy.config.listener.v3.Listener/%s"
	for name, authority := range c.authorities {
		prefix := fmt.Sprintf("xdstp://%s", url.PathEscape(name))
		if authority.ClientListenerResourceNameTemplate == "" {
			authority.ClientListenerResourceNameTemplate = prefix + "/envoy.config.listener.v3.Listener/%s"
			continue
		}
		if !strings.HasPrefix(authority.ClientListenerResourceNameTemplate, prefix) {
			return fmt.Errorf("xds: field clientListenerResourceNameTemplate %q of authority %q doesn't start with prefix %q", authority.ClientListenerResourceNameTemplate, name, prefix)
		}
	}
	return nil
}

// GetConfiguration returns the bootstrap configuration initialized by reading
// the bootstrap file found at ${GRPC_XDS_BOOTSTRAP} or bootstrap contents
// specified at ${GRPC_XDS_BOOTSTRAP_CONFIG}. If both env vars are set, the
// former is preferred.
//
// This function tries to process as much of the bootstrap file as possible (in
// the presence of the errors) and may return a Config object with certain
// fields left unspecified, in which case the caller should use some sane
// defaults.
//
// This function returns an error if it's unable to parse the contents of the
// bootstrap config. It returns (nil, nil) if none of the env vars are set.
func GetConfiguration() (*Config, error) {
	fName := envconfig.XDSBootstrapFileName
	fContent := envconfig.XDSBootstrapFileContent

	if fName != "" {
		if logger.V(2) {
			logger.Infof("Using bootstrap file with name %q from GRPC_XDS_BOOTSTRAP environment variable", fName)
		}
		cfg, err := bootstrapFileReadFunc(fName)
		if err != nil {
			return nil, fmt.Errorf("xds: failed to read bootstrap config from file %q: %v", fName, err)
		}
		return NewConfigFromContents(cfg)
	}

	if fContent != "" {
		if logger.V(2) {
			logger.Infof("Using bootstrap contents from GRPC_XDS_BOOTSTRAP_CONFIG environment variable")
		}
		return NewConfigFromContents([]byte(fContent))
	}

	return nil, nil
}

// NewConfigFromContents creates a new bootstrap configuration from the provided
// contents.
func NewConfigFromContents(data []byte) (*Config, error) {
	// Normalize the input configuration.
	buf := bytes.Buffer{}
	err := json.Indent(&buf, data, "", "")
	if err != nil {
		return nil, fmt.Errorf("xds: error normalizing JSON bootstrap configuration: %v", err)
	}
	data = bytes.TrimSpace(buf.Bytes())

	config := &Config{}
	if err := config.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return config, nil
}

// ConfigOptionsForTesting specifies options for creating a new bootstrap
// configuration for testing purposes.
//
// # Testing-Only
type ConfigOptionsForTesting struct {
	// Servers is the top-level xDS server configuration. It contains a list of
	// server configurations.
	Servers json.RawMessage
	// CertificateProviders is the certificate providers configuration.
	CertificateProviders map[string]json.RawMessage
	// ServerListenerResourceNameTemplate is the listener resource name template
	// to be used on the gRPC server.
	ServerListenerResourceNameTemplate string
	// ClientDefaultListenerResourceNameTemplate is the default listener
	// resource name template to be used on the gRPC client.
	ClientDefaultListenerResourceNameTemplate string
	// Authorities is a list of non-default authorities.
	Authorities map[string]json.RawMessage
	// Node identifies the gRPC client/server node in the
	// proxyless service mesh.
	Node json.RawMessage
	// AllowedGRPCServices is the allowlist of gRPC services.
	AllowedGRPCServices json.RawMessage
}

// NewContentsForTesting creates a new bootstrap configuration from the passed in
// options, for testing purposes.
//
// # Testing-Only
func NewContentsForTesting(opts ConfigOptionsForTesting) ([]byte, error) {
	var servers ServerConfigs
	if err := json.Unmarshal(opts.Servers, &servers); err != nil {
		return nil, err
	}
	certProviders := make(map[string]certproviderNameAndConfig)
	for k, v := range opts.CertificateProviders {
		cp := certproviderNameAndConfig{}
		if err := json.Unmarshal(v, &cp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal certificate provider configuration for %s: %s", k, string(v))
		}
		certProviders[k] = cp
	}
	authorities := make(map[string]*Authority)
	for k, v := range opts.Authorities {
		a := &Authority{}
		if err := json.Unmarshal(v, a); err != nil {
			return nil, fmt.Errorf("failed to unmarshal authority configuration for %s: %s", k, string(v))
		}
		authorities[k] = a
	}
	node := newNode()
	if err := json.Unmarshal(opts.Node, &node); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node configuration %s: %v", string(opts.Node), err)
	}
	allowedGRPCServices := make(AllowedGRPCServices)
	if len(opts.AllowedGRPCServices) > 0 {
		if err := json.Unmarshal(opts.AllowedGRPCServices, &allowedGRPCServices); err != nil {
			return nil, fmt.Errorf("failed to unmarshal allowed_grpc_services configuration: %v", err)
		}
		// Only the parsed config is needed to marshal the bootstrap JSON below;
		// release the credentials built during unmarshal so this test helper
		// does not leak file watchers.
		for _, svc := range allowedGRPCServices {
			if svc == nil {
				continue
			}
			for _, cleanup := range svc.Cleanups() {
				cleanup()
			}
		}
	}
	cfgJSON := configJSON{
		XDSServers:                                servers,
		CertificateProviders:                      certProviders,
		ServerListenerResourceNameTemplate:        opts.ServerListenerResourceNameTemplate,
		ClientDefaultListenerResourceNameTemplate: opts.ClientDefaultListenerResourceNameTemplate,
		Authorities:                               authorities,
		Node:                                      node,
		AllowedGRPCServices:                       allowedGRPCServices,
	}
	contents, err := json.MarshalIndent(cfgJSON, " ", " ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bootstrap configuration for provided options %+v: %v", opts, err)
	}
	return contents, nil
}

// certproviderNameAndConfig is the internal representation of
// the`certificate_providers` field in the bootstrap configuration.
type certproviderNameAndConfig struct {
	PluginName string          `json:"plugin_name"`
	Config     json.RawMessage `json:"config"`
}

// locality is the internal representation of the locality field within node.
type locality struct {
	Region  string `json:"region,omitempty"`
	Zone    string `json:"zone,omitempty"`
	SubZone string `json:"sub_zone,omitempty"`
}

func (l locality) Equal(other locality) bool {
	return l.Region == other.Region && l.Zone == other.Zone && l.SubZone == other.SubZone
}

func (l locality) isEmpty() bool {
	return l.Equal(locality{})
}

type userAgentVersion struct {
	UserAgentVersion string `json:"user_agent_version,omitempty"`
}

// node is the internal representation of the node field in the bootstrap
// configuration.
type node struct {
	ID       string           `json:"id,omitempty"`
	Cluster  string           `json:"cluster,omitempty"`
	Locality locality         `json:"locality,omitempty"`
	Metadata *structpb.Struct `json:"metadata,omitempty"`

	// The following fields are controlled by the client implementation and
	// should not unmarshaled from JSON.
	userAgentName        string
	userAgentVersionType userAgentVersion
	clientFeatures       []string
}

// newNode is a convenience function to create a new node instance with fields
// controlled by the client implementation set to the desired values.
func newNode() node {
	return node{
		userAgentName:        gRPCUserAgentName,
		userAgentVersionType: userAgentVersion{UserAgentVersion: grpc.Version},
		clientFeatures:       []string{clientFeatureNoOverprovisioning, clientFeatureResourceWrapper},
	}
}

func (n node) Equal(other node) bool {
	switch {
	case n.ID != other.ID:
		return false
	case n.Cluster != other.Cluster:
		return false
	case !n.Locality.Equal(other.Locality):
		return false
	case n.userAgentName != other.userAgentName:
		return false
	case n.userAgentVersionType != other.userAgentVersionType:
		return false
	}

	// Consider failures in JSON marshaling as being unable to perform the
	// comparison, and hence return false.
	nMetadata, err := n.Metadata.MarshalJSON()
	if err != nil {
		return false
	}
	otherMetadata, err := other.Metadata.MarshalJSON()
	if err != nil {
		return false
	}
	if !bytes.Equal(nMetadata, otherMetadata) {
		return false
	}

	return slices.Equal(n.clientFeatures, other.clientFeatures)
}

func (n node) toProto() *v3corepb.Node {
	return &v3corepb.Node{
		Id:      n.ID,
		Cluster: n.Cluster,
		Locality: func() *v3corepb.Locality {
			if n.Locality.isEmpty() {
				return nil
			}
			return &v3corepb.Locality{
				Region:  n.Locality.Region,
				Zone:    n.Locality.Zone,
				SubZone: n.Locality.SubZone,
			}
		}(),
		Metadata:             proto.Clone(n.Metadata).(*structpb.Struct),
		UserAgentName:        n.userAgentName,
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: n.userAgentVersionType.UserAgentVersion},
		ClientFeatures:       slices.Clone(n.clientFeatures),
	}
}
