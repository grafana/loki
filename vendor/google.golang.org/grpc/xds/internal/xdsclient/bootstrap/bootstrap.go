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
	"net/url"
	"os"
	"strings"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/golang/protobuf/jsonpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/xds/bootstrap"
)

const (
	// The "server_features" field in the bootstrap file contains a list of
	// features supported by the server:
	// - A value of "xds_v3" indicates that the server supports the v3 version of
	//   the xDS transport protocol.
	// - A value of "ignore_resource_deletion" indicates that the client should
	//   ignore deletion of Listener and Cluster resources in updates from the
	//   server.
	serverFeaturesV3                     = "xds_v3"
	serverFeaturesIgnoreResourceDeletion = "ignore_resource_deletion"

	gRPCUserAgentName               = "gRPC Go"
	clientFeatureNoOverprovisioning = "envoy.lb.does_not_support_overprovisioning"
	clientFeatureResourceWrapper    = "xds.config.resource-in-sotw"
)

func init() {
	bootstrap.RegisterCredentials(&insecureCredsBuilder{})
	bootstrap.RegisterCredentials(&googleDefaultCredsBuilder{})
}

// For overriding in unit tests.
var bootstrapFileReadFunc = os.ReadFile

// insecureCredsBuilder implements the `Credentials` interface defined in
// package `xds/bootstrap` and encapsulates an insecure credential.
type insecureCredsBuilder struct{}

func (i *insecureCredsBuilder) Build(json.RawMessage) (credentials.Bundle, error) {
	return insecure.NewBundle(), nil
}

func (i *insecureCredsBuilder) Name() string {
	return "insecure"
}

// googleDefaultCredsBuilder implements the `Credentials` interface defined in
// package `xds/boostrap` and encapsulates a Google Default credential.
type googleDefaultCredsBuilder struct{}

func (d *googleDefaultCredsBuilder) Build(json.RawMessage) (credentials.Bundle, error) {
	return google.NewDefaultCredentials(), nil
}

func (d *googleDefaultCredsBuilder) Name() string {
	return "google_default"
}

// ChannelCreds contains the credentials to be used while communicating with an
// xDS server. It is also used to dedup servers with the same server URI.
type ChannelCreds struct {
	// Type contains a unique name identifying the credentials type. The only
	// supported types currently are "google_default" and "insecure".
	Type string
	// Config contains the JSON configuration associated with the credentials.
	Config json.RawMessage
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
	// after a successful unmarshaling from JSON configuration. Therefore,
	// it is safe to ignore the error here.
	b, _ := json.Marshal(cc.Config)
	return cc.Type + "-" + string(b)
}

// ServerConfig contains the configuration to connect to a server, including
// URI, creds, and transport API version (e.g. v2 or v3).
//
// It contains unexported fields that are initialized when unmarshaled from JSON
// using either the UnmarshalJSON() method or the ServerConfigFromJSON()
// function. Hence users are strongly encouraged not to use a literal struct
// initialization to create an instance of this type, but instead unmarshal from
// JSON using one of the two available options.
type ServerConfig struct {
	// ServerURI is the management server to connect to.
	//
	// The bootstrap file contains an ordered list of xDS servers to contact for
	// this authority. The first one is picked.
	ServerURI string
	// Creds contains the credentials to be used while communicationg with this
	// xDS server. It is also used to dedup servers with the same server URI.
	Creds ChannelCreds
	// ServerFeatures contains a list of features supported by this xDS server.
	// It is also used to dedup servers with the same server URI and creds.
	ServerFeatures []string

	// As part of unmarshaling the JSON config into this struct, we ensure that
	// the credentials config is valid by building an instance of the specified
	// credentials and store it here as a grpc.DialOption for easy access when
	// dialing this xDS server.
	credsDialOption grpc.DialOption

	// IgnoreResourceDeletion controls the behavior of the xDS client when the
	// server deletes a previously sent Listener or Cluster resource. If set, the
	// xDS client will not invoke the watchers' OnResourceDoesNotExist() method
	// when a resource is deleted, nor will it remove the existing resource value
	// from its cache.
	IgnoreResourceDeletion bool
}

// CredsDialOption returns the configured credentials as a grpc dial option.
func (sc *ServerConfig) CredsDialOption() grpc.DialOption {
	return sc.credsDialOption
}

// String returns the string representation of the ServerConfig.
//
// This string representation will be used as map keys in federation
// (`map[ServerConfig]authority`), so that the xDS ClientConn and stream will be
// shared by authorities with different names but the same server config.
//
// It covers (almost) all the fields so the string can represent the config
// content. It doesn't cover NodeProto because NodeProto isn't used by
// federation.
func (sc *ServerConfig) String() string {
	features := strings.Join(sc.ServerFeatures, "-")
	return strings.Join([]string{sc.ServerURI, sc.Creds.String(), features}, "-")
}

// MarshalJSON marshals the ServerConfig to json.
func (sc ServerConfig) MarshalJSON() ([]byte, error) {
	server := xdsServer{
		ServerURI:      sc.ServerURI,
		ChannelCreds:   []channelCreds{{Type: sc.Creds.Type, Config: sc.Creds.Config}},
		ServerFeatures: sc.ServerFeatures,
	}
	server.ServerFeatures = []string{serverFeaturesV3}
	if sc.IgnoreResourceDeletion {
		server.ServerFeatures = append(server.ServerFeatures, serverFeaturesIgnoreResourceDeletion)
	}
	return json.Marshal(server)
}

// UnmarshalJSON takes the json data (a server) and unmarshals it to the struct.
func (sc *ServerConfig) UnmarshalJSON(data []byte) error {
	var server xdsServer
	if err := json.Unmarshal(data, &server); err != nil {
		return fmt.Errorf("xds: json.Unmarshal(data) for field ServerConfig failed during bootstrap: %v", err)
	}

	sc.ServerURI = server.ServerURI
	sc.ServerFeatures = server.ServerFeatures
	for _, f := range server.ServerFeatures {
		if f == serverFeaturesIgnoreResourceDeletion {
			sc.IgnoreResourceDeletion = true
		}
	}
	for _, cc := range server.ChannelCreds {
		// We stop at the first credential type that we support.
		c := bootstrap.GetCredentials(cc.Type)
		if c == nil {
			continue
		}
		bundle, err := c.Build(cc.Config)
		if err != nil {
			return fmt.Errorf("failed to build credentials bundle from bootstrap for %q: %v", cc.Type, err)
		}
		sc.Creds = ChannelCreds(cc)
		sc.credsDialOption = grpc.WithCredentialsBundle(bundle)
		break
	}
	return nil
}

// ServerConfigFromJSON creates a new ServerConfig from the given JSON
// configuration. This is the preferred way of creating a ServerConfig when
// hand-crafting the JSON configuration.
func ServerConfigFromJSON(data []byte) (*ServerConfig, error) {
	sc := new(ServerConfig)
	if err := sc.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return sc, nil
}

// Equal reports whether sc and other are considered equal.
func (sc *ServerConfig) Equal(other *ServerConfig) bool {
	switch {
	case sc == nil && other == nil:
		return true
	case (sc != nil) != (other != nil):
		return false
	case sc.ServerURI != other.ServerURI:
		return false
	case !sc.Creds.Equal(other.Creds):
		return false
	case !equalStringSlice(sc.ServerFeatures, other.ServerFeatures):
		return false
	}
	return true
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// unmarshalJSONServerConfigSlice unmarshals JSON to a slice.
func unmarshalJSONServerConfigSlice(data []byte) ([]*ServerConfig, error) {
	var servers []*ServerConfig
	if err := json.Unmarshal(data, &servers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON to []*ServerConfig: %v", err)
	}
	if len(servers) < 1 {
		return nil, fmt.Errorf("no management server found in JSON")
	}
	return servers, nil
}

// Authority contains configuration for an Authority for an xDS control plane
// server. See the Authorities field in the Config struct for how it's used.
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
	ClientListenerResourceNameTemplate string
	// XDSServer contains the management server and config to connect to for
	// this authority.
	XDSServer *ServerConfig
}

// UnmarshalJSON implement json unmarshaller.
func (a *Authority) UnmarshalJSON(data []byte) error {
	var jsonData map[string]json.RawMessage
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return fmt.Errorf("xds: failed to parse authority: %v", err)
	}

	for k, v := range jsonData {
		switch k {
		case "xds_servers":
			servers, err := unmarshalJSONServerConfigSlice(v)
			if err != nil {
				return fmt.Errorf("xds: json.Unmarshal(data) for field %q failed during bootstrap: %v", k, err)
			}
			a.XDSServer = servers[0]
		case "client_listener_resource_name_template":
			if err := json.Unmarshal(v, &a.ClientListenerResourceNameTemplate); err != nil {
				return fmt.Errorf("xds: json.Unmarshal(%v) for field %q failed during bootstrap: %v", string(v), k, err)
			}
		}
	}
	return nil
}

// Config provides the xDS client with several key bits of information that it
// requires in its interaction with the management server. The Config is
// initialized from the bootstrap file.
type Config struct {
	// XDSServer is the management server to connect to.
	//
	// The bootstrap file contains a list of servers (with name+creds), but we
	// pick the first one.
	XDSServer *ServerConfig
	// CertProviderConfigs contains a mapping from certificate provider plugin
	// instance names to parsed buildable configs.
	CertProviderConfigs map[string]*certprovider.BuildableConfig
	// ServerListenerResourceNameTemplate is a template for the name of the
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
	ServerListenerResourceNameTemplate string
	// A template for the name of the Listener resource to subscribe to
	// for a gRPC client channel.  Used only when the channel is created
	// with an "xds:" URI with no authority.
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
	ClientDefaultListenerResourceNameTemplate string
	// Authorities is a map of authority name to corresponding configuration.
	//
	// This is used in the following cases:
	// - A gRPC client channel is created using an "xds:" URI that includes
	//   an authority.
	// - A gRPC client channel is created using an "xds:" URI with no
	//   authority, but the "client_default_listener_resource_name_template"
	//   field above turns it into an "xdstp:" URI.
	// - A gRPC server is created and the
	//   "server_listener_resource_name_template" field is an "xdstp:" URI.
	//
	// In any of those cases, it is an error if the specified authority is
	// not present in this map.
	Authorities map[string]*Authority
	// NodeProto contains the Node proto to be used in xDS requests. This will be
	// of type *v3corepb.Node.
	NodeProto *v3corepb.Node
}

type channelCreds struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config,omitempty"`
}

type xdsServer struct {
	ServerURI      string         `json:"server_uri"`
	ChannelCreds   []channelCreds `json:"channel_creds"`
	ServerFeatures []string       `json:"server_features"`
}

func bootstrapConfigFromEnvVariable() ([]byte, error) {
	fName := envconfig.XDSBootstrapFileName
	fContent := envconfig.XDSBootstrapFileContent

	// Bootstrap file name has higher priority than bootstrap content.
	if fName != "" {
		// If file name is set
		// - If file not found (or other errors), fail
		// - Otherwise, use the content.
		//
		// Note that even if the content is invalid, we don't failover to the
		// file content env variable.
		logger.Debugf("Using bootstrap file with name %q", fName)
		return bootstrapFileReadFunc(fName)
	}

	if fContent != "" {
		return []byte(fContent), nil
	}

	return nil, fmt.Errorf("none of the bootstrap environment variables (%q or %q) defined",
		envconfig.XDSBootstrapFileNameEnv, envconfig.XDSBootstrapFileContentEnv)
}

// NewConfig returns a new instance of Config initialized by reading the
// bootstrap file found at ${GRPC_XDS_BOOTSTRAP} or bootstrap contents specified
// at ${GRPC_XDS_BOOTSTRAP_CONFIG}. If both env vars are set, the former is
// preferred.
//
// We support a credential registration mechanism and only credentials
// registered through that mechanism will be accepted here. See package
// `xds/bootstrap` for details.
//
// This function tries to process as much of the bootstrap file as possible (in
// the presence of the errors) and may return a Config object with certain
// fields left unspecified, in which case the caller should use some sane
// defaults.
func NewConfig() (*Config, error) {
	// Examples of the bootstrap json can be found in the generator tests
	// https://github.com/GoogleCloudPlatform/traffic-director-grpc-bootstrap/blob/master/main_test.go.
	data, err := bootstrapConfigFromEnvVariable()
	if err != nil {
		return nil, fmt.Errorf("xds: Failed to read bootstrap config: %v", err)
	}
	return newConfigFromContents(data)
}

// NewConfigFromContentsForTesting returns a new Config using the specified
// bootstrap file contents instead of reading the environment variable.
//
// This is only suitable for testing purposes.
func NewConfigFromContentsForTesting(data []byte) (*Config, error) {
	return newConfigFromContents(data)
}

func newConfigFromContents(data []byte) (*Config, error) {
	config := &Config{}

	var jsonData map[string]json.RawMessage
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return nil, fmt.Errorf("xds: failed to parse bootstrap config: %v", err)
	}

	var node *v3corepb.Node
	m := jsonpb.Unmarshaler{AllowUnknownFields: true}
	for k, v := range jsonData {
		switch k {
		case "node":
			node = &v3corepb.Node{}
			if err := m.Unmarshal(bytes.NewReader(v), node); err != nil {
				return nil, fmt.Errorf("xds: jsonpb.Unmarshal(%v) for field %q failed during bootstrap: %v", string(v), k, err)
			}
		case "xds_servers":
			servers, err := unmarshalJSONServerConfigSlice(v)
			if err != nil {
				return nil, fmt.Errorf("xds: json.Unmarshal(data) for field %q failed during bootstrap: %v", k, err)
			}
			config.XDSServer = servers[0]
		case "certificate_providers":
			var providerInstances map[string]json.RawMessage
			if err := json.Unmarshal(v, &providerInstances); err != nil {
				return nil, fmt.Errorf("xds: json.Unmarshal(%v) for field %q failed during bootstrap: %v", string(v), k, err)
			}
			configs := make(map[string]*certprovider.BuildableConfig)
			getBuilder := internal.GetCertificateProviderBuilder.(func(string) certprovider.Builder)
			for instance, data := range providerInstances {
				var nameAndConfig struct {
					PluginName string          `json:"plugin_name"`
					Config     json.RawMessage `json:"config"`
				}
				if err := json.Unmarshal(data, &nameAndConfig); err != nil {
					return nil, fmt.Errorf("xds: json.Unmarshal(%v) for field %q failed during bootstrap: %v", string(v), instance, err)
				}

				name := nameAndConfig.PluginName
				parser := getBuilder(nameAndConfig.PluginName)
				if parser == nil {
					// We ignore plugins that we do not know about.
					continue
				}
				bc, err := parser.ParseConfig(nameAndConfig.Config)
				if err != nil {
					return nil, fmt.Errorf("xds: config parsing for plugin %q failed: %v", name, err)
				}
				configs[instance] = bc
			}
			config.CertProviderConfigs = configs
		case "server_listener_resource_name_template":
			if err := json.Unmarshal(v, &config.ServerListenerResourceNameTemplate); err != nil {
				return nil, fmt.Errorf("xds: json.Unmarshal(%v) for field %q failed during bootstrap: %v", string(v), k, err)
			}
		case "client_default_listener_resource_name_template":
			if !envconfig.XDSFederation {
				logger.Warningf("Bootstrap field %v is not support when Federation is disabled", k)
				continue
			}
			if err := json.Unmarshal(v, &config.ClientDefaultListenerResourceNameTemplate); err != nil {
				return nil, fmt.Errorf("xds: json.Unmarshal(%v) for field %q failed during bootstrap: %v", string(v), k, err)
			}
		case "authorities":
			if !envconfig.XDSFederation {
				logger.Warningf("Bootstrap field %v is not support when Federation is disabled", k)
				continue
			}
			if err := json.Unmarshal(v, &config.Authorities); err != nil {
				return nil, fmt.Errorf("xds: json.Unmarshal(%v) for field %q failed during bootstrap: %v", string(v), k, err)
			}
		default:
			logger.Warningf("Bootstrap content has unknown field: %s", k)
		}
		// Do not fail the xDS bootstrap when an unknown field is seen. This can
		// happen when an older version client reads a newer version bootstrap
		// file with new fields.
	}

	if config.ClientDefaultListenerResourceNameTemplate == "" {
		// Default value of the default client listener name template is "%s".
		config.ClientDefaultListenerResourceNameTemplate = "%s"
	}
	if config.XDSServer == nil {
		return nil, fmt.Errorf("xds: required field %q not found in bootstrap %s", "xds_servers", jsonData["xds_servers"])
	}
	if config.XDSServer.ServerURI == "" {
		return nil, fmt.Errorf("xds: required field %q not found in bootstrap %s", "xds_servers.server_uri", jsonData["xds_servers"])
	}
	if config.XDSServer.CredsDialOption() == nil {
		return nil, fmt.Errorf("xds: required field %q doesn't contain valid value in bootstrap %s", "xds_servers.channel_creds", jsonData["xds_servers"])
	}
	// Post-process the authorities' client listener resource template field:
	// - if set, it must start with "xdstp://<authority_name>/"
	// - if not set, it defaults to "xdstp://<authority_name>/envoy.config.listener.v3.Listener/%s"
	for name, authority := range config.Authorities {
		prefix := fmt.Sprintf("xdstp://%s", url.PathEscape(name))
		if authority.ClientListenerResourceNameTemplate == "" {
			authority.ClientListenerResourceNameTemplate = prefix + "/envoy.config.listener.v3.Listener/%s"
			continue
		}
		if !strings.HasPrefix(authority.ClientListenerResourceNameTemplate, prefix) {
			return nil, fmt.Errorf("xds: field ClientListenerResourceNameTemplate %q of authority %q doesn't start with prefix %q", authority.ClientListenerResourceNameTemplate, name, prefix)
		}
	}

	// Performing post-production on the node information. Some additional fields
	// which are not expected to be set in the bootstrap file are populated here.
	if node == nil {
		node = &v3corepb.Node{}
	}
	node.UserAgentName = gRPCUserAgentName
	node.UserAgentVersionType = &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version}
	node.ClientFeatures = append(node.ClientFeatures, clientFeatureNoOverprovisioning, clientFeatureResourceWrapper)
	config.NodeProto = node

	logger.Debugf("Bootstrap config for creating xds-client: %v", pretty.ToJSON(config))
	return config, nil
}
