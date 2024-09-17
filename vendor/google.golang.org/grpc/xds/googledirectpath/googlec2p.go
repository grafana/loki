/*
 *
 * Copyright 2021 gRPC authors.
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

// Package googledirectpath implements a resolver that configures xds to make
// cloud to prod directpath connection.
//
// It's a combo of DNS and xDS resolvers. It delegates to DNS if
// - not on GCE, or
// - xDS bootstrap env var is set (so this client needs to do normal xDS, not
// direct path, and clients with this scheme is not part of the xDS mesh).
package googledirectpath

import (
	"fmt"
	"math/rand"
	"net/url"
	"time"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/googlecloud"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/xdsclient"

	_ "google.golang.org/grpc/xds" // To register xds resolvers and balancers.
)

const (
	c2pScheme    = "google-c2p"
	c2pAuthority = "traffic-director-c2p.xds.googleapis.com"

	tdURL          = "dns:///directpath-pa.googleapis.com"
	httpReqTimeout = 10 * time.Second
	zoneURL        = "http://metadata.google.internal/computeMetadata/v1/instance/zone"
	ipv6URL        = "http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/ipv6s"

	gRPCUserAgentName               = "gRPC Go"
	clientFeatureNoOverprovisioning = "envoy.lb.does_not_support_overprovisioning"
	clientFeatureResourceWrapper    = "xds.config.resource-in-sotw"
	ipv6CapableMetadataName         = "TRAFFICDIRECTOR_DIRECTPATH_C2P_IPV6_CAPABLE"

	logPrefix = "[google-c2p-resolver]"

	dnsName, xdsName = "dns", "xds"
)

// For overriding in unittests.
var (
	onGCE = googlecloud.OnGCE

	newClientWithConfig = func(config *bootstrap.Config) (xdsclient.XDSClient, func(), error) {
		return xdsclient.NewWithConfig(config)
	}

	logger = internalgrpclog.NewPrefixLogger(grpclog.Component("directpath"), logPrefix)
)

func init() {
	resolver.Register(c2pResolverBuilder{})
}

type c2pResolverBuilder struct{}

func (c2pResolverBuilder) Build(t resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	if t.URL.Host != "" {
		return nil, fmt.Errorf("google-c2p URI scheme does not support authorities")
	}

	if !runDirectPath() {
		// If not xDS, fallback to DNS.
		t.URL.Scheme = dnsName
		return resolver.Get(dnsName).Build(t, cc, opts)
	}

	// Note that the following calls to getZone() and getIPv6Capable() does I/O,
	// and has 10 seconds timeout each.
	//
	// This should be fine in most of the cases. In certain error cases, this
	// could block Dial() for up to 10 seconds (each blocking call has its own
	// goroutine).
	zoneCh, ipv6CapableCh := make(chan string), make(chan bool)
	go func() { zoneCh <- getZone(httpReqTimeout) }()
	go func() { ipv6CapableCh <- getIPv6Capable(httpReqTimeout) }()

	xdsServerURI := envconfig.C2PResolverTestOnlyTrafficDirectorURI
	if xdsServerURI == "" {
		xdsServerURI = tdURL
	}

	nodeCfg := newNodeConfig(<-zoneCh, <-ipv6CapableCh)
	xdsServerCfg := newXdsServerConfig(xdsServerURI)
	authoritiesCfg := newAuthoritiesConfig(xdsServerCfg)

	config, err := bootstrap.NewConfigFromContents([]byte(fmt.Sprintf(`
	{
		"xds_servers": [%s],
		"client_default_listener_resource_name_template": "%%s",
		"authorities": %s,
		"node": %s
	}`, xdsServerCfg, authoritiesCfg, nodeCfg)))

	if err != nil {
		return nil, fmt.Errorf("failed to build bootstrap configuration: %v", err)
	}

	// Create singleton xds client with this config. The xds client will be
	// used by the xds resolver later.
	_, close, err := newClientWithConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to start xDS client: %v", err)
	}

	t = resolver.Target{
		URL: url.URL{
			Scheme: xdsName,
			Host:   c2pAuthority,
			Path:   t.URL.Path,
		},
	}
	xdsR, err := resolver.Get(xdsName).Build(t, cc, opts)
	if err != nil {
		close()
		return nil, err
	}
	return &c2pResolver{
		Resolver:        xdsR,
		clientCloseFunc: close,
	}, nil
}

func (b c2pResolverBuilder) Scheme() string {
	return c2pScheme
}

type c2pResolver struct {
	resolver.Resolver
	clientCloseFunc func()
}

func (r *c2pResolver) Close() {
	r.Resolver.Close()
	r.clientCloseFunc()
}

var id = fmt.Sprintf("C2P-%d", rand.Int())

func newNodeConfig(zone string, ipv6Capable bool) string {
	metadata := ""
	if ipv6Capable {
		metadata = fmt.Sprintf(`, "metadata":  { "%s": true }`, ipv6CapableMetadataName)
	}

	return fmt.Sprintf(`
	{
		"id": "%s",
		"locality": {
			"zone": "%s"
		}
		%s
	}`, id, zone, metadata)
}

func newAuthoritiesConfig(xdsServer string) string {
	return fmt.Sprintf(`
	{
		"%s": {
			"xds_servers": [%s]
		}
	}
	`, c2pAuthority, xdsServer)
}

func newXdsServerConfig(xdsServerURI string) string {
	return fmt.Sprintf(`
	{
		"server_uri": "%s",
		"channel_creds": [{"type": "google_default"}],
		"server_features": ["xds_v3", "ignore_resource_deletion", "xds.config.resource-in-sotw"]
	}`, xdsServerURI)
}

// runDirectPath returns whether this resolver should use direct path.
//
// direct path is enabled if this client is running on GCE.
func runDirectPath() bool {
	return onGCE()
}
