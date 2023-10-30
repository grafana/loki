/*
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
 */

// Package cdsbalancer implements a balancer to handle CDS responses.
package cdsbalancer

import (
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal/balancer/nop"
	"google.golang.org/grpc/internal/buffer"
	xdsinternal "google.golang.org/grpc/internal/credentials/xds"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/clusterresolver"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

const (
	cdsName = "cds_experimental"
)

var (
	errBalancerClosed = errors.New("cds_experimental LB policy is closed")

	// newChildBalancer is a helper function to build a new cluster_resolver
	// balancer and will be overridden in unittests.
	newChildBalancer = func(cc balancer.ClientConn, opts balancer.BuildOptions) (balancer.Balancer, error) {
		builder := balancer.Get(clusterresolver.Name)
		if builder == nil {
			return nil, fmt.Errorf("xds: no balancer builder with name %v", clusterresolver.Name)
		}
		// We directly pass the parent clientConn to the underlying
		// cluster_resolver balancer because the cdsBalancer does not deal with
		// subConns.
		return builder.Build(cc, opts), nil
	}
	buildProvider = buildProviderFunc
)

func init() {
	balancer.Register(bb{})
}

// bb implements the balancer.Builder interface to help build a cdsBalancer.
// It also implements the balancer.ConfigParser interface to help parse the
// JSON service config, to be passed to the cdsBalancer.
type bb struct{}

// Build creates a new CDS balancer with the ClientConn.
func (bb) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	builder := balancer.Get(clusterresolver.Name)
	if builder == nil {
		// Shouldn't happen, registered through imported Cluster Resolver,
		// defensive programming.
		logger.Errorf("%q LB policy is needed but not registered", clusterresolver.Name)
		return nop.NewBalancer(cc, fmt.Errorf("%q LB policy is needed but not registered", clusterresolver.Name))
	}
	crParser, ok := builder.(balancer.ConfigParser)
	if !ok {
		// Shouldn't happen, imported Cluster Resolver builder has this method.
		logger.Errorf("%q LB policy does not implement a config parser", clusterresolver.Name)
		return nop.NewBalancer(cc, fmt.Errorf("%q LB policy does not implement a config parser", clusterresolver.Name))
	}
	b := &cdsBalancer{
		bOpts:    opts,
		updateCh: buffer.NewUnbounded(),
		closed:   grpcsync.NewEvent(),
		done:     grpcsync.NewEvent(),
		crParser: crParser,
		xdsHI:    xdsinternal.NewHandshakeInfo(nil, nil),
	}
	b.logger = prefixLogger((b))
	b.logger.Infof("Created")
	var creds credentials.TransportCredentials
	switch {
	case opts.DialCreds != nil:
		creds = opts.DialCreds
	case opts.CredsBundle != nil:
		creds = opts.CredsBundle.TransportCredentials()
	}
	if xc, ok := creds.(interface{ UsesXDS() bool }); ok && xc.UsesXDS() {
		b.xdsCredsInUse = true
	}
	b.logger.Infof("xDS credentials in use: %v", b.xdsCredsInUse)
	b.clusterHandler = newClusterHandler(b)
	b.ccw = &ccWrapper{
		ClientConn: cc,
		xdsHI:      b.xdsHI,
	}
	go b.run()
	return b
}

// Name returns the name of balancers built by this builder.
func (bb) Name() string {
	return cdsName
}

// lbConfig represents the loadBalancingConfig section of the service config
// for the cdsBalancer.
type lbConfig struct {
	serviceconfig.LoadBalancingConfig
	ClusterName string `json:"Cluster"`
}

// ParseConfig parses the JSON load balancer config provided into an
// internal form or returns an error if the config is invalid.
func (bb) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	var cfg lbConfig
	if err := json.Unmarshal(c, &cfg); err != nil {
		return nil, fmt.Errorf("xds: unable to unmarshal lbconfig: %s, error: %v", string(c), err)
	}
	return &cfg, nil
}

// ccUpdate wraps a clientConn update received from gRPC (pushed from the
// xdsResolver). A valid clusterName causes the cdsBalancer to register a CDS
// watcher with the xdsClient, while a non-nil error causes it to cancel the
// existing watch and propagate the error to the underlying cluster_resolver
// balancer.
type ccUpdate struct {
	clusterName string
	err         error
}

type exitIdle struct{}

// cdsBalancer implements a CDS based LB policy. It instantiates a
// cluster_resolver balancer to further resolve the serviceName received from
// CDS, into localities and endpoints. Implements the balancer.Balancer
// interface which is exposed to gRPC and implements the balancer.ClientConn
// interface which is exposed to the cluster_resolver balancer.
type cdsBalancer struct {
	ccw            *ccWrapper            // ClientConn interface passed to child LB.
	bOpts          balancer.BuildOptions // BuildOptions passed to child LB.
	updateCh       *buffer.Unbounded     // Channel for gRPC and xdsClient updates.
	xdsClient      xdsclient.XDSClient   // xDS client to watch Cluster resource.
	clusterHandler *clusterHandler       // To watch the clusters.
	childLB        balancer.Balancer
	logger         *grpclog.PrefixLogger
	closed         *grpcsync.Event
	done           *grpcsync.Event
	crParser       balancer.ConfigParser

	// The certificate providers are cached here to that they can be closed when
	// a new provider is to be created.
	cachedRoot     certprovider.Provider
	cachedIdentity certprovider.Provider
	xdsHI          *xdsinternal.HandshakeInfo
	xdsCredsInUse  bool
}

// handleClientConnUpdate handles a ClientConnUpdate received from gRPC. Good
// updates lead to registration of a CDS watch. Updates with error lead to
// cancellation of existing watch and propagation of the same error to the
// cluster_resolver balancer.
func (b *cdsBalancer) handleClientConnUpdate(update *ccUpdate) {
	// We first handle errors, if any, and then proceed with handling the
	// update, only if the status quo has changed.
	if err := update.err; err != nil {
		b.handleErrorFromUpdate(err, true)
		return
	}
	b.clusterHandler.updateRootCluster(update.clusterName)
}

// handleSecurityConfig processes the security configuration received from the
// management server, creates appropriate certificate provider plugins, and
// updates the HandhakeInfo which is added as an address attribute in
// NewSubConn() calls.
func (b *cdsBalancer) handleSecurityConfig(config *xdsresource.SecurityConfig) error {
	// If xdsCredentials are not in use, i.e, the user did not want to get
	// security configuration from an xDS server, we should not be acting on the
	// received security config here. Doing so poses a security threat.
	if !b.xdsCredsInUse {
		return nil
	}

	// Security config being nil is a valid case where the management server has
	// not sent any security configuration. The xdsCredentials implementation
	// handles this by delegating to its fallback credentials.
	if config == nil {
		// We need to explicitly set the fields to nil here since this might be
		// a case of switching from a good security configuration to an empty
		// one where fallback credentials are to be used.
		b.xdsHI.SetRootCertProvider(nil)
		b.xdsHI.SetIdentityCertProvider(nil)
		b.xdsHI.SetSANMatchers(nil)
		return nil
	}

	bc := b.xdsClient.BootstrapConfig()
	if bc == nil || bc.CertProviderConfigs == nil {
		// Bootstrap did not find any certificate provider configs, but the user
		// has specified xdsCredentials and the management server has sent down
		// security configuration.
		return errors.New("xds: certificate_providers config missing in bootstrap file")
	}
	cpc := bc.CertProviderConfigs

	// A root provider is required whether we are using TLS or mTLS.
	rootProvider, err := buildProvider(cpc, config.RootInstanceName, config.RootCertName, false, true)
	if err != nil {
		return err
	}

	// The identity provider is only present when using mTLS.
	var identityProvider certprovider.Provider
	if name, cert := config.IdentityInstanceName, config.IdentityCertName; name != "" {
		var err error
		identityProvider, err = buildProvider(cpc, name, cert, true, false)
		if err != nil {
			return err
		}
	}

	// Close the old providers and cache the new ones.
	if b.cachedRoot != nil {
		b.cachedRoot.Close()
	}
	if b.cachedIdentity != nil {
		b.cachedIdentity.Close()
	}
	b.cachedRoot = rootProvider
	b.cachedIdentity = identityProvider

	// We set all fields here, even if some of them are nil, since they
	// could have been non-nil earlier.
	b.xdsHI.SetRootCertProvider(rootProvider)
	b.xdsHI.SetIdentityCertProvider(identityProvider)
	b.xdsHI.SetSANMatchers(config.SubjectAltNameMatchers)
	return nil
}

func buildProviderFunc(configs map[string]*certprovider.BuildableConfig, instanceName, certName string, wantIdentity, wantRoot bool) (certprovider.Provider, error) {
	cfg, ok := configs[instanceName]
	if !ok {
		return nil, fmt.Errorf("certificate provider instance %q not found in bootstrap file", instanceName)
	}
	provider, err := cfg.Build(certprovider.BuildOptions{
		CertName:     certName,
		WantIdentity: wantIdentity,
		WantRoot:     wantRoot,
	})
	if err != nil {
		// This error is not expected since the bootstrap process parses the
		// config and makes sure that it is acceptable to the plugin. Still, it
		// is possible that the plugin parses the config successfully, but its
		// Build() method errors out.
		return nil, fmt.Errorf("xds: failed to get security plugin instance (%+v): %v", cfg, err)
	}
	return provider, nil
}

// handleWatchUpdate handles a watch update from the xDS Client. Good updates
// lead to clientConn updates being invoked on the underlying cluster_resolver balancer.
func (b *cdsBalancer) handleWatchUpdate(update clusterHandlerUpdate) {
	if err := update.err; err != nil {
		b.logger.Warningf("Watch error from xds-client %p: %v", b.xdsClient, err)
		b.handleErrorFromUpdate(err, false)
		return
	}

	b.logger.Infof("Received Cluster resource contains content: %s, security config: %s", pretty.ToJSON(update.updates), pretty.ToJSON(update.securityCfg))

	// Process the security config from the received update before building the
	// child policy or forwarding the update to it. We do this because the child
	// policy may try to create a new subConn inline. Processing the security
	// configuration here and setting up the handshakeInfo will make sure that
	// such attempts are handled properly.
	if err := b.handleSecurityConfig(update.securityCfg); err != nil {
		// If the security config is invalid, for example, if the provider
		// instance is not found in the bootstrap config, we need to put the
		// channel in transient failure.
		b.logger.Warningf("Received Cluster resource contains invalid security config: %v", err)
		b.handleErrorFromUpdate(err, false)
		return
	}

	// The first good update from the watch API leads to the instantiation of an
	// cluster_resolver balancer. Further updates/errors are propagated to the existing
	// cluster_resolver balancer.
	if b.childLB == nil {
		childLB, err := newChildBalancer(b.ccw, b.bOpts)
		if err != nil {
			b.logger.Errorf("Failed to create child policy of type %s: %v", clusterresolver.Name, err)
			return
		}
		b.childLB = childLB
		b.logger.Infof("Created child policy %p of type %s", b.childLB, clusterresolver.Name)
	}

	dms := make([]clusterresolver.DiscoveryMechanism, len(update.updates))
	for i, cu := range update.updates {
		switch cu.ClusterType {
		case xdsresource.ClusterTypeEDS:
			dms[i] = clusterresolver.DiscoveryMechanism{
				Type:                  clusterresolver.DiscoveryMechanismTypeEDS,
				Cluster:               cu.ClusterName,
				EDSServiceName:        cu.EDSServiceName,
				MaxConcurrentRequests: cu.MaxRequests,
			}
			if cu.LRSServerConfig == xdsresource.ClusterLRSServerSelf {
				bootstrapConfig := b.xdsClient.BootstrapConfig()
				parsedName := xdsresource.ParseName(cu.ClusterName)
				if parsedName.Scheme == xdsresource.FederationScheme {
					// Is a federation resource name, find the corresponding
					// authority server config.
					if cfg, ok := bootstrapConfig.Authorities[parsedName.Authority]; ok {
						dms[i].LoadReportingServer = cfg.XDSServer
					}
				} else {
					// Not a federation resource name, use the default
					// authority.
					dms[i].LoadReportingServer = bootstrapConfig.XDSServer
				}
			}
		case xdsresource.ClusterTypeLogicalDNS:
			dms[i] = clusterresolver.DiscoveryMechanism{
				Type:        clusterresolver.DiscoveryMechanismTypeLogicalDNS,
				Cluster:     cu.ClusterName,
				DNSHostname: cu.DNSHostName,
			}
		default:
			b.logger.Infof("Unexpected cluster type %v when handling update from cluster handler", cu.ClusterType)
		}
		if envconfig.XDSOutlierDetection {
			odJSON := cu.OutlierDetection
			// "In the cds LB policy, if the outlier_detection field is not set in
			// the Cluster resource, a "no-op" outlier_detection config will be
			// generated in the corresponding DiscoveryMechanism config, with all
			// fields unset." - A50
			if odJSON == nil {
				// This will pick up top level defaults in Cluster Resolver
				// ParseConfig, but sre and fpe will be nil still so still a
				// "no-op" config.
				odJSON = json.RawMessage(`{}`)
			}
			dms[i].OutlierDetection = odJSON
		}
	}

	// Prepare Cluster Resolver config, marshal into JSON, and then Parse it to
	// get configuration to send downward to Cluster Resolver.
	lbCfg := &clusterresolver.LBConfig{
		DiscoveryMechanisms: dms,
		XDSLBPolicy:         update.lbPolicy,
	}
	crLBCfgJSON, err := json.Marshal(lbCfg)
	if err != nil {
		// Shouldn't happen, since we just prepared struct.
		b.logger.Errorf("cds_balancer: error marshalling prepared config: %v", lbCfg)
		return
	}

	var sc serviceconfig.LoadBalancingConfig
	if sc, err = b.crParser.ParseConfig(crLBCfgJSON); err != nil {
		b.logger.Errorf("cds_balancer: cluster_resolver config generated %v is invalid: %v", string(crLBCfgJSON), err)
		return
	}

	ccState := balancer.ClientConnState{
		ResolverState:  xdsclient.SetClient(resolver.State{}, b.xdsClient),
		BalancerConfig: sc,
	}
	if err := b.childLB.UpdateClientConnState(ccState); err != nil {
		b.logger.Errorf("Encountered error when sending config {%+v} to child policy: %v", ccState, err)
	}
}

// run is a long-running goroutine which handles all updates from gRPC. All
// methods which are invoked directly by gRPC or xdsClient simply push an
// update onto a channel which is read and acted upon right here.
func (b *cdsBalancer) run() {
	for {
		select {
		case u, ok := <-b.updateCh.Get():
			if !ok {
				return
			}
			b.updateCh.Load()
			switch update := u.(type) {
			case *ccUpdate:
				b.handleClientConnUpdate(update)
			case exitIdle:
				if b.childLB == nil {
					b.logger.Errorf("Received ExitIdle with no child policy")
					break
				}
				// This implementation assumes the child balancer supports
				// ExitIdle (but still checks for the interface's existence to
				// avoid a panic if not).  If the child does not, no subconns
				// will be connected.
				if ei, ok := b.childLB.(balancer.ExitIdler); ok {
					ei.ExitIdle()
				}
			}
		case u := <-b.clusterHandler.updateChannel:
			b.handleWatchUpdate(u)
		case <-b.closed.Done():
			b.clusterHandler.close()
			if b.childLB != nil {
				b.childLB.Close()
				b.childLB = nil
			}
			if b.cachedRoot != nil {
				b.cachedRoot.Close()
			}
			if b.cachedIdentity != nil {
				b.cachedIdentity.Close()
			}
			b.updateCh.Close()
			b.logger.Infof("Shutdown")
			b.done.Fire()
			return
		}
	}
}

// handleErrorFromUpdate handles both the error from parent ClientConn (from
// resolver) and the error from xds client (from the watcher). fromParent is
// true if error is from parent ClientConn.
//
// If the error is connection error, it's passed down to the child policy.
// Nothing needs to be done in CDS (e.g. it doesn't go into fallback).
//
// If the error is resource-not-found:
// - If it's from resolver, it means LDS resources were removed. The CDS watch
// should be canceled.
// - If it's from xds client, it means CDS resource were removed. The CDS
// watcher should keep watching.
//
// In both cases, the error will be forwarded to the child balancer. And if
// error is resource-not-found, the child balancer will stop watching EDS.
func (b *cdsBalancer) handleErrorFromUpdate(err error, fromParent bool) {
	// This is not necessary today, because xds client never sends connection
	// errors.
	if fromParent && xdsresource.ErrType(err) == xdsresource.ErrorTypeResourceNotFound {
		b.clusterHandler.close()
	}
	if b.childLB != nil {
		if xdsresource.ErrType(err) != xdsresource.ErrorTypeConnection {
			// Connection errors will be sent to the child balancers directly.
			// There's no need to forward them.
			b.childLB.ResolverError(err)
		}
	} else {
		// If child balancer was never created, fail the RPCs with
		// errors.
		b.ccw.UpdateState(balancer.State{
			ConnectivityState: connectivity.TransientFailure,
			Picker:            base.NewErrPicker(err),
		})
	}
}

// UpdateClientConnState receives the serviceConfig (which contains the
// clusterName to watch for in CDS) and the xdsClient object from the
// xdsResolver.
func (b *cdsBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	if b.closed.HasFired() {
		b.logger.Errorf("Received balancer config after close")
		return errBalancerClosed
	}

	if b.xdsClient == nil {
		c := xdsclient.FromResolverState(state.ResolverState)
		if c == nil {
			return balancer.ErrBadResolverState
		}
		b.xdsClient = c
	}
	b.logger.Infof("Received balancer config update: %s", pretty.ToJSON(state.BalancerConfig))

	// The errors checked here should ideally never happen because the
	// ServiceConfig in this case is prepared by the xdsResolver and is not
	// something that is received on the wire.
	lbCfg, ok := state.BalancerConfig.(*lbConfig)
	if !ok {
		b.logger.Warningf("Received unexpected balancer config type: %T", state.BalancerConfig)
		return balancer.ErrBadResolverState
	}
	if lbCfg.ClusterName == "" {
		b.logger.Warningf("Received balancer config with no cluster name")
		return balancer.ErrBadResolverState
	}
	b.updateCh.Put(&ccUpdate{clusterName: lbCfg.ClusterName})
	return nil
}

// ResolverError handles errors reported by the xdsResolver.
func (b *cdsBalancer) ResolverError(err error) {
	if b.closed.HasFired() {
		b.logger.Warningf("Received resolver error after close: %v", err)
		return
	}
	b.updateCh.Put(&ccUpdate{err: err})
}

// UpdateSubConnState handles subConn updates from gRPC.
func (b *cdsBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	b.logger.Errorf("UpdateSubConnState(%v, %+v) called unexpectedly", sc, state)
}

// Close cancels the CDS watch, closes the child policy and closes the
// cdsBalancer.
func (b *cdsBalancer) Close() {
	b.closed.Fire()
	<-b.done.Done()
}

func (b *cdsBalancer) ExitIdle() {
	b.updateCh.Put(exitIdle{})
}

// ccWrapper wraps the balancer.ClientConn passed to the CDS balancer at
// creation and intercepts the NewSubConn() and UpdateAddresses() call from the
// child policy to add security configuration required by xDS credentials.
//
// Other methods of the balancer.ClientConn interface are not overridden and
// hence get the original implementation.
type ccWrapper struct {
	balancer.ClientConn

	// The certificate providers in this HandshakeInfo are updated based on the
	// received security configuration in the Cluster resource.
	xdsHI *xdsinternal.HandshakeInfo
}

// NewSubConn intercepts NewSubConn() calls from the child policy and adds an
// address attribute which provides all information required by the xdsCreds
// handshaker to perform the TLS handshake.
func (ccw *ccWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	newAddrs := make([]resolver.Address, len(addrs))
	for i, addr := range addrs {
		newAddrs[i] = xdsinternal.SetHandshakeInfo(addr, ccw.xdsHI)
	}
	// No need to override opts.StateListener; just forward all calls to the
	// child that created the SubConn.
	return ccw.ClientConn.NewSubConn(newAddrs, opts)
}

func (ccw *ccWrapper) UpdateAddresses(sc balancer.SubConn, addrs []resolver.Address) {
	newAddrs := make([]resolver.Address, len(addrs))
	for i, addr := range addrs {
		newAddrs[i] = xdsinternal.SetHandshakeInfo(addr, ccw.xdsHI)
	}
	ccw.ClientConn.UpdateAddresses(sc, newAddrs)
}
