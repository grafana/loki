/*
 * Copyright 2025 gRPC authors.
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

// Package xdsdepmgr provides the implementation of the xDS dependency manager
// that manages all the xDS watches and resources as described in gRFC A74.
package xdsdepmgr

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"sync"

	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

const prefix = "[xdsdepmgr %p] "

var logger = grpclog.Component("xds")

func prefixLogger(p *DependencyManager) *internalgrpclog.PrefixLogger {
	return internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf(prefix, p))
}

// ConfigWatcher is the interface for consumers of aggregated xDS configuration
// from the DependencyManager. The only consumer of this configuration is
// currently the xDS resolver.
type ConfigWatcher interface {
	// Update is invoked when a new, validated xDS configuration is available.
	//
	// Implementations must treat the received config as read-only and should
	// not modify it.
	Update(*xdsresource.XDSConfig)

	// Error is invoked when an error is received from the listener or route
	// resource watcher. This includes cases where:
	//  - The listener or route resource watcher reports a resource error.
	//  - The received listener resource is a socket listener, not an API
	//    listener. TODO(i/8114): Implement this check.
	//  - The received route configuration does not contain a virtual host
	//    matching the channel's default authority.
	Error(error)
}

// xdsResourceState is a generic struct to hold the state of a watched xDS
// resource.
type xdsResourceState[T any, U any] struct {
	lastUpdate     *T
	lastErr        error
	updateReceived bool
	stop           func()
	extras         U // to store any additional state specific to the watcher
}

func (x *xdsResourceState[T, U]) setLastUpdate(update *T) {
	x.lastUpdate = update
	x.updateReceived = true
	x.lastErr = nil
}

func (x *xdsResourceState[T, U]) setLastError(err error) {
	x.lastErr = err
	x.updateReceived = true
	x.lastUpdate = nil
}

type dnsExtras struct {
	dnsR resolver.Resolver
}

type routeExtras struct {
	virtualHost *xdsresource.VirtualHost
}

// DependencyManager registers watches on the xDS client for all required xDS
// resources, resolves dependencies between them, and returns a complete
// configuration to the xDS resolver.
type DependencyManager struct {
	// The following fields are initialized at creation time and are read-only
	// after that.
	logger             *internalgrpclog.PrefixLogger
	watcher            ConfigWatcher
	xdsClient          xdsclient.XDSClient
	ldsResourceName    string
	dataplaneAuthority string
	nodeID             string

	// Used to serialize callbacks from DNS resolvers to avoid deadlocks. Since
	// the resolver's Build() is called with the dependency manager lock held,
	// direct callbacks to ClientConn (which also require that lock) would
	// deadlock.
	dnsSerializer       *grpcsync.CallbackSerializer
	dnsSerializerCancel func()

	// All the fields below are protected by mu.
	mu                          sync.Mutex
	stopped                     bool
	listenerWatcher             *xdsResourceState[xdsresource.ListenerUpdate, struct{}]
	rdsResourceName             string
	routeConfigWatcher          *xdsResourceState[xdsresource.RouteConfigUpdate, routeExtras]
	clustersFromLastRouteConfig map[string]bool
	clusterWatchers             map[string]*xdsResourceState[xdsresource.ClusterUpdate, struct{}]
	endpointWatchers            map[string]*xdsResourceState[xdsresource.EndpointsUpdate, struct{}]
	dnsResolvers                map[string]*xdsResourceState[xdsresource.DNSUpdate, dnsExtras]
	clusterSubscriptions        map[string]*clusterRef
}

// New creates a new DependencyManager.
//
//   - listenerName is the name of the Listener resource to request from the
//     management server.
//   - dataplaneAuthority is used to select the best matching virtual host from
//     the route configuration received from the management server.
//   - xdsClient is the xDS client to use to register resource watches.
//   - watcher is the ConfigWatcher interface that will receive the aggregated
//     XDSConfig updates and errors.
func New(listenerName, dataplaneAuthority string, xdsClient xdsclient.XDSClient, watcher ConfigWatcher) *DependencyManager {
	ctx, cancel := context.WithCancel(context.Background())
	dm := &DependencyManager{
		ldsResourceName:             listenerName,
		dataplaneAuthority:          dataplaneAuthority,
		xdsClient:                   xdsClient,
		watcher:                     watcher,
		nodeID:                      xdsClient.BootstrapConfig().Node().GetId(),
		dnsSerializer:               grpcsync.NewCallbackSerializer(ctx),
		dnsSerializerCancel:         cancel,
		clustersFromLastRouteConfig: make(map[string]bool),
		endpointWatchers:            make(map[string]*xdsResourceState[xdsresource.EndpointsUpdate, struct{}]),
		dnsResolvers:                make(map[string]*xdsResourceState[xdsresource.DNSUpdate, dnsExtras]),
		clusterWatchers:             make(map[string]*xdsResourceState[xdsresource.ClusterUpdate, struct{}]),
		clusterSubscriptions:        make(map[string]*clusterRef),
	}
	dm.logger = prefixLogger(dm)

	// The dependency manager starts by watching the listener resource and
	// discovers other resources as required. For example, the listener resource
	// will contain the name of the route configuration resource, which will be
	// subsequently watched.œ
	dm.listenerWatcher = &xdsResourceState[xdsresource.ListenerUpdate, struct{}]{}
	lw := &xdsResourceWatcher[xdsresource.ListenerUpdate]{
		onUpdate: func(update *xdsresource.ListenerUpdate, onDone func()) {
			dm.onListenerResourceUpdate(update, onDone)
		},
		onError: func(err error, onDone func()) {
			dm.onListenerResourceError(err, onDone)
		},
		onAmbientError: func(err error, onDone func()) {
			dm.onListenerResourceAmbientError(err, onDone)
		},
	}
	dm.listenerWatcher.stop = xdsresource.WatchListener(dm.xdsClient, listenerName, lw)
	return dm
}

// Close cancels all registered resource watches.
func (m *DependencyManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return
	}

	m.stopped = true
	m.listenerWatcher.stop()
	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop()
	}
	for name, cluster := range m.clusterWatchers {
		cluster.stop()
		delete(m.clusterWatchers, name)
	}

	for name, endpoint := range m.endpointWatchers {
		endpoint.stop()
		delete(m.endpointWatchers, name)
	}

	// We cannot wait for the dns serializer to finish here, as the callbacks
	// try to grab the dependency manager lock, which is already held here.
	m.dnsSerializerCancel()
	for name, dnsResolver := range m.dnsResolvers {
		dnsResolver.stop()
		delete(m.dnsResolvers, name)
	}
}

// annotateErrorWithNodeID annotates the given error with the provided xDS node
// ID.
func (m *DependencyManager) annotateErrorWithNodeID(err error) error {
	return fmt.Errorf("[xDS node id: %v]: %v", m.nodeID, err)
}

// maybeSendUpdateLocked verifies that all expected resources have been
// received, and if so, delivers the complete xDS configuration to the watcher.
func (m *DependencyManager) maybeSendUpdateLocked() {
	if m.listenerWatcher.lastUpdate == nil || m.routeConfigWatcher == nil || m.routeConfigWatcher.lastUpdate == nil {
		return
	}
	config := &xdsresource.XDSConfig{
		Listener:    m.listenerWatcher.lastUpdate,
		RouteConfig: m.routeConfigWatcher.lastUpdate,
		VirtualHost: m.routeConfigWatcher.extras.virtualHost,
		Clusters:    make(map[string]*xdsresource.ClusterResult),
	}

	edsResourcesSeen := make(map[string]bool)
	dnsResourcesSeen := make(map[string]bool)
	clusterResourcesSeen := make(map[string]bool)
	haveAllResources := true

	// Start watches for all clusters. Wait for all the clusters with static
	// reference(from route config) to be resolved before sending the update.
	for cluster := range m.clusterSubscriptions {
		clusterConfig := make(map[string]*xdsresource.ClusterResult)
		ok, leafClusters, err := m.populateClusterConfigLocked(cluster, 0, clusterConfig, edsResourcesSeen, dnsResourcesSeen, clusterResourcesSeen)
		if !ok && m.clusterSubscriptions[cluster].staticRefCount > 0 {
			haveAllResources = false
		}
		// If there are no leaf clusters, add that as error.
		if ok && len(leafClusters) == 0 {
			config.Clusters[cluster] = &xdsresource.ClusterResult{Err: m.annotateErrorWithNodeID(fmt.Errorf("aggregate cluster graph has no leaf clusters"))}
		}
		if err != nil {
			config.Clusters[cluster] = &xdsresource.ClusterResult{Err: err}
		}
		// Only if all the dependencies for the cluster is resolved, add the
		// clusters to the config. This is to ensure we do not send partial
		// updates for dynamic clusters.
		if ok {
			maps.Copy(config.Clusters, clusterConfig)
		}
	}

	// Cancel resources not seen in the tree.
	for name, ep := range m.endpointWatchers {
		if _, ok := edsResourcesSeen[name]; !ok {
			ep.stop()
			delete(m.endpointWatchers, name)
		}
	}
	for name, dr := range m.dnsResolvers {
		if _, ok := dnsResourcesSeen[name]; !ok {
			dr.stop()
			delete(m.dnsResolvers, name)
		}
	}
	for name, cluster := range m.clusterWatchers {
		if _, ok := clusterResourcesSeen[name]; !ok {
			cluster.stop()
			delete(m.clusterWatchers, name)
		}
	}
	if haveAllResources {
		m.watcher.Update(config)
	}
}

// populateClusterConfigLocked resolves and populates the
// configuration for the given cluster and its children, including its
// associated endpoint or aggregate children. For aggregate clusters, it
// recursively resolves the configuration for its child clusters.
//
// This function traverses the cluster dependency graph (e.g., from an Aggregate
// cluster down to its leaf clusters and their endpoints/DNS resources) to
// ensure all necessary xDS resources are watched and fully resolved before
// configuration is considered ready.
//
// Parameters:
//
//	clusterName:          The name of the cluster resource to resolve.
//	depth:                The current recursion depth.
//	clusterConfigs:       Map to store the resolved cluster configuration.
//	endpointResourcesSeen: Stores which EDS resource names have been encountered.
//	dnsResourcesSeen:      Stores which DNS resource names have been encountered.
//	clustersSeen:         Stores which cluster resource names have been encountered.
//
// Returns:
//
//	bool:                 Returns true if the cluster configuration (and all its
//	                      dependencies) is fully resolved (i.e either update or
//	                      error has been received).
//	[]string:             A slice of all "leaf" cluster names discovered in the
//	                      traversal starting from `clusterName`. For
//	                      non-aggregate clusters, this will contain only `clusterName`.
//	error:                Error that needs to be propagated up the tree (like
//	                      max depth exceeded or an error propagated from a
//	                      child cluster).
func (m *DependencyManager) populateClusterConfigLocked(clusterName string, depth int, clusterConfigs map[string]*xdsresource.ClusterResult, endpointResourcesSeen, dnsResourcesSeen, clustersSeen map[string]bool) (bool, []string, error) {
	const aggregateClusterMaxDepth = 16
	clustersSeen[clusterName] = true

	if depth >= aggregateClusterMaxDepth {
		err := m.annotateErrorWithNodeID(fmt.Errorf("aggregate cluster graph exceeds max depth (%d)", aggregateClusterMaxDepth))
		clusterConfigs[clusterName] = &xdsresource.ClusterResult{Err: err}
		return true, nil, err
	}

	// If cluster is already seen in the tree, return.
	if _, ok := clusterConfigs[clusterName]; ok {
		return true, nil, nil
	}

	// If cluster watcher does not exist, create one.
	state, ok := m.clusterWatchers[clusterName]
	if !ok {
		m.clusterWatchers[clusterName] = newClusterWatcher(clusterName, m)
		return false, nil, nil
	}

	// If a watch exists but no update received yet, return.
	if !state.updateReceived {
		return false, nil, nil
	}

	// If there was a resource error, propagate it up.
	if state.lastErr != nil {
		return true, nil, state.lastErr
	}

	clusterConfigs[clusterName] = &xdsresource.ClusterResult{
		Config: xdsresource.ClusterConfig{
			Cluster: state.lastUpdate,
		},
	}
	update := state.lastUpdate

	switch update.ClusterType {
	case xdsresource.ClusterTypeEDS:
		return m.populateEDSClusterLocked(clusterName, update, clusterConfigs, endpointResourcesSeen)
	case xdsresource.ClusterTypeLogicalDNS:
		return m.populateLogicalDNSClusterLocked(clusterName, update, clusterConfigs, dnsResourcesSeen)
	case xdsresource.ClusterTypeAggregate:
		return m.populateAggregateClusterLocked(clusterName, update, depth, clusterConfigs, endpointResourcesSeen, dnsResourcesSeen, clustersSeen)
	default:
		clusterConfigs[clusterName] = &xdsresource.ClusterResult{Err: m.annotateErrorWithNodeID(fmt.Errorf("cluster type %v of cluster %s not supported", update.ClusterType, clusterName))}
		return true, nil, nil
	}
}

func (m *DependencyManager) populateEDSClusterLocked(clusterName string, update *xdsresource.ClusterUpdate, clusterConfigs map[string]*xdsresource.ClusterResult, endpointResourcesSeen map[string]bool) (bool, []string, error) {
	edsName := clusterName
	if update.EDSServiceName != "" {
		edsName = update.EDSServiceName
	}
	endpointResourcesSeen[edsName] = true

	// If endpoint watcher does not exist, create one.
	if _, ok := m.endpointWatchers[edsName]; !ok {
		m.endpointWatchers[edsName] = newEndpointWatcher(edsName, m)
		return false, nil, nil
	}
	endpointState := m.endpointWatchers[edsName]

	// If the resource does not have any update yet, return.
	if !endpointState.updateReceived {
		return false, nil, nil
	}

	// Store the update and error.
	clusterConfigs[clusterName].Config.EndpointConfig = &xdsresource.EndpointConfig{
		EDSUpdate:      endpointState.lastUpdate,
		ResolutionNote: endpointState.lastErr,
	}
	return true, []string{clusterName}, nil
}

func (m *DependencyManager) populateLogicalDNSClusterLocked(clusterName string, update *xdsresource.ClusterUpdate, clusterConfigs map[string]*xdsresource.ClusterResult, dnsResourcesSeen map[string]bool) (bool, []string, error) {
	target := update.DNSHostName
	dnsResourcesSeen[target] = true

	// If dns resolver does not exist, create one.
	if _, ok := m.dnsResolvers[target]; !ok {
		state := m.newDNSResolver(target)
		if state == nil {
			return false, nil, nil
		}
		m.dnsResolvers[target] = state
		return false, nil, nil
	}
	dnsState := m.dnsResolvers[target]

	// If no update received, return false.
	if !dnsState.updateReceived {
		return false, nil, nil
	}

	clusterConfigs[clusterName].Config.EndpointConfig = &xdsresource.EndpointConfig{
		DNSEndpoints:   dnsState.lastUpdate,
		ResolutionNote: dnsState.lastErr,
	}
	return true, []string{clusterName}, nil
}

func (m *DependencyManager) populateAggregateClusterLocked(clusterName string, update *xdsresource.ClusterUpdate, depth int, clusterConfigs map[string]*xdsresource.ClusterResult, endpointResourcesSeen, dnsResourcesSeen, clustersSeen map[string]bool) (bool, []string, error) {
	var leafClusters []string
	haveAllResources := true
	for _, child := range update.PrioritizedClusterNames {
		ok, childLeafClusters, err := m.populateClusterConfigLocked(child, depth+1, clusterConfigs, endpointResourcesSeen, dnsResourcesSeen, clustersSeen)
		if !ok {
			haveAllResources = false
		}
		if err != nil {
			clusterConfigs[clusterName] = &xdsresource.ClusterResult{Err: err}
			return true, leafClusters, err
		}
		leafClusters = append(leafClusters, childLeafClusters...)
	}
	if !haveAllResources {
		return false, leafClusters, nil
	}
	if haveAllResources && len(leafClusters) == 0 {
		clusterConfigs[clusterName] = &xdsresource.ClusterResult{Err: m.annotateErrorWithNodeID(fmt.Errorf("aggregate cluster graph has no leaf clusters"))}
		return true, leafClusters, nil
	}
	clusterConfigs[clusterName].Config.AggregateConfig = &xdsresource.AggregateConfig{
		LeafClusters: leafClusters,
	}
	return true, leafClusters, nil
}

func (m *DependencyManager) applyRouteConfigUpdateLocked(update *xdsresource.RouteConfigUpdate) {
	matchVH := xdsresource.FindBestMatchingVirtualHost(m.dataplaneAuthority, update.VirtualHosts)
	if matchVH == nil {
		err := m.annotateErrorWithNodeID(fmt.Errorf("could not find VirtualHost for %q", m.dataplaneAuthority))
		m.routeConfigWatcher.setLastError(err)
		m.watcher.Error(err)
		return
	}
	m.routeConfigWatcher.setLastUpdate(update)
	m.routeConfigWatcher.extras.virtualHost = matchVH

	// Get the clusters to be watched from the routes in the virtual host.
	// If the ClusterSpecifierPlugin field is set, we ignore it for now as the
	// clusters will be determined dynamically for it.
	newClusters := make(map[string]bool)
	for _, rt := range matchVH.Routes {
		for _, cluster := range rt.WeightedClusters {
			newClusters[cluster.Name] = true
		}
	}

	// Add subscriptions for all new clusters.
	for cluster := range newClusters {
		// If the cluster already has a reference, increase its static
		// reference.
		if sub, ok := m.clusterSubscriptions[cluster]; ok {
			sub.staticRefCount++
			continue
		}
		// If cluster is not present in subscriptions, add it with static
		// ref count as 1.
		m.clusterSubscriptions[cluster] = &clusterRef{
			staticRefCount: 1,
		}
	}

	// Unsubscribe to clusters from last route config.
	for cluster := range m.clustersFromLastRouteConfig {
		clusterRef, ok := m.clusterSubscriptions[cluster]
		if !ok {
			// Should not reach here as the cluster was present in last
			// route config so should be present in current cluster
			// subscriptions.
			continue
		}
		clusterRef.staticRefCount--
		if clusterRef.staticRefCount == 0 && clusterRef.dynamicRefCount == 0 {
			delete(m.clusterSubscriptions, cluster)
		}
	}
	m.clustersFromLastRouteConfig = newClusters

	// maybeSendUpdate is called to update the configuration with the new route,
	// start watching the newly added clusters and stop watching clusters that
	// are not needed anymore.
	m.maybeSendUpdateLocked()
}

func (m *DependencyManager) onListenerResourceUpdate(update *xdsresource.ListenerUpdate, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped {
		return
	}

	if m.logger.V(2) {
		m.logger.Infof("Received update for Listener resource %q: %+v", m.ldsResourceName, update)
	}

	m.listenerWatcher.setLastUpdate(update)

	if update.APIListener != nil && update.APIListener.InlineRouteConfig != nil {
		// If there was a previous route config watcher because of a non-inline
		// route configuration, cancel it.
		m.rdsResourceName = ""
		if m.routeConfigWatcher != nil {
			m.routeConfigWatcher.stop()
		}
		m.routeConfigWatcher = &xdsResourceState[xdsresource.RouteConfigUpdate, routeExtras]{stop: func() {}}
		m.applyRouteConfigUpdateLocked(update.APIListener.InlineRouteConfig)
		return
	}

	// We get here only if there was no inline route configuration. If the route
	// config name has not changed, send an update with existing route
	// configuration and the newly received listener configuration.
	if update.APIListener == nil {
		m.logger.Errorf("Received a listener resource with no api_listener configuration")
		return
	}
	if m.rdsResourceName == update.APIListener.RouteConfigName {
		m.maybeSendUpdateLocked()
		return
	}

	// If the route config name has changed, cancel the old watcher and start a
	// new one. At this point, since the new route config name has not yet been
	// resolved, no update is sent to the channel, and therefore the old route
	// configuration (if received) is used until the new one is received.
	m.rdsResourceName = update.APIListener.RouteConfigName
	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop()
	}
	rw := &xdsResourceWatcher[xdsresource.RouteConfigUpdate]{
		onUpdate: func(update *xdsresource.RouteConfigUpdate, onDone func()) {
			m.onRouteConfigResourceUpdate(m.rdsResourceName, update, onDone)
		},
		onError: func(err error, onDone func()) {
			m.onRouteConfigResourceError(m.rdsResourceName, err, onDone)
		},
		onAmbientError: func(err error, onDone func()) {
			m.onRouteConfigResourceAmbientError(m.rdsResourceName, err, onDone)
		},
	}
	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop = xdsresource.WatchRouteConfig(m.xdsClient, m.rdsResourceName, rw)
	} else {
		m.routeConfigWatcher = &xdsResourceState[xdsresource.RouteConfigUpdate, routeExtras]{
			stop: xdsresource.WatchRouteConfig(m.xdsClient, m.rdsResourceName, rw),
		}
	}
}

func (m *DependencyManager) onListenerResourceError(err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped {
		return
	}

	m.logger.Warningf("Received resource error for Listener resource %q: %v", m.ldsResourceName, m.annotateErrorWithNodeID(err))

	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop()
	}
	m.listenerWatcher.setLastError(err)
	m.rdsResourceName = ""
	m.routeConfigWatcher = nil
	m.watcher.Error(fmt.Errorf("listener resource error: %v", m.annotateErrorWithNodeID(err)))
}

// onListenerResourceAmbientError handles ambient errors received from the
// listener resource watcher. Since ambient errors do not impact the current
// state of the resource, no change is made to the current configuration and the
// errors are only logged for visibility.
func (m *DependencyManager) onListenerResourceAmbientError(err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped {
		return
	}

	m.logger.Warningf("Listener resource ambient error: %v", m.annotateErrorWithNodeID(err))
}

func (m *DependencyManager) onRouteConfigResourceUpdate(resourceName string, update *xdsresource.RouteConfigUpdate, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.rdsResourceName != resourceName {
		return
	}

	if m.logger.V(2) {
		m.logger.Infof("Received update for RouteConfiguration resource %q: %+v", resourceName, update)
	}
	m.applyRouteConfigUpdateLocked(update)
}

func (m *DependencyManager) onRouteConfigResourceError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.rdsResourceName != resourceName {
		return
	}
	m.routeConfigWatcher.setLastError(err)
	m.logger.Warningf("Received resource error for RouteConfiguration resource %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	m.watcher.Error(fmt.Errorf("route resource error: %v", m.annotateErrorWithNodeID(err)))
}

// onRouteResourceAmbientError handles ambient errors received from the route
// resource watcher. Since ambient errors do not impact the current state of the
// resource, no change is made to the current configuration and the errors are
// only logged for visibility.
func (m *DependencyManager) onRouteConfigResourceAmbientError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.rdsResourceName != resourceName {
		return
	}

	m.logger.Warningf("Route resource ambient error %q: %v", resourceName, m.annotateErrorWithNodeID(err))
}

func newClusterWatcher(resourceName string, depMgr *DependencyManager) *xdsResourceState[xdsresource.ClusterUpdate, struct{}] {
	w := &xdsResourceWatcher[xdsresource.ClusterUpdate]{
		onUpdate: func(u *xdsresource.ClusterUpdate, onDone func()) {
			depMgr.onClusterResourceUpdate(resourceName, u, onDone)
		},
		onError: func(err error, onDone func()) {
			depMgr.onClusterResourceError(resourceName, err, onDone)
		},
		onAmbientError: func(err error, onDone func()) {
			depMgr.onClusterAmbientError(resourceName, err, onDone)
		},
	}
	return &xdsResourceState[xdsresource.ClusterUpdate, struct{}]{
		stop: xdsresource.WatchCluster(depMgr.xdsClient, resourceName, w),
	}
}

// Records a successful Cluster resource update, clears any previous error.
func (m *DependencyManager) onClusterResourceUpdate(resourceName string, update *xdsresource.ClusterUpdate, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.clusterWatchers[resourceName] == nil {
		return
	}

	if m.logger.V(2) {
		m.logger.Infof("Received update for Cluster resource %q: %+v", resourceName, update)
	}
	m.clusterWatchers[resourceName].setLastUpdate(update)
	m.maybeSendUpdateLocked()
}

// Records a resource error for a Cluster resource, clears the last successful
// update since we want to stop using the resource if we get a resource error.
func (m *DependencyManager) onClusterResourceError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.clusterWatchers[resourceName] == nil {
		return
	}
	m.logger.Warningf("Received resource error for Cluster resource %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	m.clusterWatchers[resourceName].setLastError(err)
	m.maybeSendUpdateLocked()
}

// Ambient errors from cluster resource are logged and the last successful
// update is retained because it should continue to be used.
func (m *DependencyManager) onClusterAmbientError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.clusterWatchers[resourceName] == nil {
		return
	}
	m.logger.Warningf("Cluster resource ambient error %q: %v", resourceName, m.annotateErrorWithNodeID(err))
}

func newEndpointWatcher(resourceName string, depMgr *DependencyManager) *xdsResourceState[xdsresource.EndpointsUpdate, struct{}] {
	w := &xdsResourceWatcher[xdsresource.EndpointsUpdate]{
		onUpdate: func(u *xdsresource.EndpointsUpdate, onDone func()) {
			depMgr.onEndpointUpdate(resourceName, u, onDone)
		},
		onError: func(err error, onDone func()) {
			depMgr.onEndpointResourceError(resourceName, err, onDone)
		},
		onAmbientError: func(err error, onDone func()) {
			depMgr.onEndpointAmbientError(resourceName, err, onDone)
		},
	}
	return &xdsResourceState[xdsresource.EndpointsUpdate, struct{}]{
		stop: xdsresource.WatchEndpoints(depMgr.xdsClient, resourceName, w),
	}
}

// Records a successful Endpoint resource update, clears any previous error from
// the state.
func (m *DependencyManager) onEndpointUpdate(resourceName string, update *xdsresource.EndpointsUpdate, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.endpointWatchers[resourceName] == nil {
		return
	}

	if m.logger.V(2) {
		m.logger.Infof("Received update for Endpoint resource %q: %+v", resourceName, update)
	}
	m.endpointWatchers[resourceName].setLastUpdate(update)
	m.maybeSendUpdateLocked()
}

// Records a resource error and clears the last successful update since the
// endpoints should not be used after getting a resource error.
func (m *DependencyManager) onEndpointResourceError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.endpointWatchers[resourceName] == nil {
		return
	}
	m.logger.Warningf("Received resource error for Endpoint resource %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	// Send an empty EndpointsUpdate instead of nil to avoid nil-check handling
	// in the CDS balancer. The priority balancer will handle the case of having
	// no endpoints and transition the channel to Transient Failure if needed.
	m.endpointWatchers[resourceName].lastUpdate = &xdsresource.EndpointsUpdate{}
	m.endpointWatchers[resourceName].lastErr = err
	m.endpointWatchers[resourceName].updateReceived = true
	m.maybeSendUpdateLocked()
}

// Logs the ambient error and does not update the state, as the last successful
// update for endpoints should continue to be used.
func (m *DependencyManager) onEndpointAmbientError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	defer onDone()
	if m.stopped || m.endpointWatchers[resourceName] == nil {
		return
	}

	m.logger.Warningf("Endpoint resource ambient error %q: %v", resourceName, m.annotateErrorWithNodeID(err))
}

// Converts the DNS resolver state to an internal update, handling address-only
// updates by wrapping them into endpoints. It records the update and clears any
// previous error.
func (m *DependencyManager) onDNSUpdate(resourceName string, update *resolver.State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped || m.dnsResolvers[resourceName] == nil {
		return
	}

	if m.logger.V(2) {
		m.logger.Infof("Received update from DNS resolver for resource %q: %+v", resourceName, update)
	}
	m.dnsResolvers[resourceName].setLastUpdate(&xdsresource.DNSUpdate{Endpoints: update.Endpoints})
	m.maybeSendUpdateLocked()
}

// Records a DNS resolver error. It clears the last update only if no successful
// update has been received yet, then triggers a dependency update.
//
// If a previous good update was received, the error is logged and the previous
// update is retained for continued use. Errors are suppressed if a resource
// error was already received, as further propagation would have no downstream
// effect.
func (m *DependencyManager) onDNSError(resourceName string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped || m.dnsResolvers[resourceName] == nil {
		return
	}

	err = fmt.Errorf("dns resolver error for target %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	m.logger.Warningf("%v", err)
	state := m.dnsResolvers[resourceName]
	if state.updateReceived {
		return
	}

	// Send an empty DNSUpdate instead of nil to avoid nil-check handling in the
	// CDS balancer. The priority balancer will handle the case of having no
	// endpoints and transition the channel to Transient Failure if needed.
	state.lastUpdate = &xdsresource.DNSUpdate{}
	state.lastErr = err
	state.updateReceived = true
	m.maybeSendUpdateLocked()
}

// RequestDNSReresolution calls all the DNS resolver's ResolveNow.
func (m *DependencyManager) RequestDNSReresolution(opt resolver.ResolveNowOptions) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, res := range m.dnsResolvers {
		if res.extras.dnsR != nil {
			res.extras.dnsR.ResolveNow(opt)
		}
	}
}

type resolverClientConn struct {
	target string
	depMgr *DependencyManager
}

func (rcc *resolverClientConn) UpdateState(state resolver.State) error {
	rcc.depMgr.dnsSerializer.TrySchedule(func(context.Context) {
		rcc.depMgr.onDNSUpdate(rcc.target, &state)
	})
	return nil
}

func (rcc *resolverClientConn) ReportError(err error) {
	rcc.depMgr.dnsSerializer.TrySchedule(func(context.Context) {
		rcc.depMgr.onDNSError(rcc.target, err)
	})
}

func (rcc *resolverClientConn) NewAddress(addresses []resolver.Address) {
	rcc.UpdateState(resolver.State{Addresses: addresses})
}

func (rcc *resolverClientConn) ParseServiceConfig(string) *serviceconfig.ParseResult {
	return &serviceconfig.ParseResult{Err: fmt.Errorf("service config not supported")}
}

func (m *DependencyManager) newDNSResolver(target string) *xdsResourceState[xdsresource.DNSUpdate, dnsExtras] {
	rcc := &resolverClientConn{
		target: target,
		depMgr: m,
	}
	u, err := url.Parse("dns:///" + target)
	if err != nil {
		err := fmt.Errorf("failed to parse DNS target %q: %v", target, m.annotateErrorWithNodeID(err))
		m.logger.Warningf("%v", err)
		rcc.ReportError(err)
		return &xdsResourceState[xdsresource.DNSUpdate, dnsExtras]{
			stop: func() {},
		}
	}

	r, err := resolver.Get("dns").Build(resolver.Target{URL: *u}, rcc, resolver.BuildOptions{})
	if err != nil {
		rcc.ReportError(err)
		err := fmt.Errorf("failed to build DNS resolver for target %q: %v", target, m.annotateErrorWithNodeID(err))
		m.logger.Warningf("%v", err)
		return &xdsResourceState[xdsresource.DNSUpdate, dnsExtras]{
			stop: func() {},
		}
	}

	return &xdsResourceState[xdsresource.DNSUpdate, dnsExtras]{
		extras: dnsExtras{dnsR: r},
		stop:   r.Close,
	}
}

// xdsResourceWatcher is a generic implementation of the xdsresource.Watcher
// interface.
type xdsResourceWatcher[T any] struct {
	onUpdate       func(*T, func())
	onError        func(error, func())
	onAmbientError func(error, func())
}

func (x *xdsResourceWatcher[T]) ResourceChanged(update *T, onDone func()) {
	x.onUpdate(update, onDone)
}

func (x *xdsResourceWatcher[T]) ResourceError(err error, onDone func()) {
	x.onError(err, onDone)
}

func (x *xdsResourceWatcher[T]) AmbientError(err error, onDone func()) {
	x.onAmbientError(err, onDone)
}

// xdsClusterSubscriberKey is the type used as the key to store the
// ClusterSubscriber interface in the Attributes field of resolver.states.
type xdsClusterSubscriberKey struct{}

// SetXDSClusterSubscriber returns a copy of state in which the Attributes field
// is updated with the ClusterSubscriber interface.
func SetXDSClusterSubscriber(state resolver.State, subs ClusterSubscriber) resolver.State {
	state.Attributes = state.Attributes.WithValue(xdsClusterSubscriberKey{}, subs)
	return state
}

// XDSClusterSubscriberFromResolverState returns ClusterSubscriber interface
// stored as an attribute in the resolver state.
func XDSClusterSubscriberFromResolverState(state resolver.State) ClusterSubscriber {
	if v := state.Attributes.Value(xdsClusterSubscriberKey{}); v != nil {
		return v.(ClusterSubscriber)
	}
	return nil
}

// ClusterSubscriber allows dynamic subscription to clusters. This is useful for
// scenarios where the cluster name was not present in the RouteConfiguration,
// e.g. when the route uses a ClusterSpecifierPlugin.
//
// The xDS resolver will pass this interface to the LB policies as an attribute
// in the resolver update.
type ClusterSubscriber interface {
	// SubscribeToCluster creates a dynamic subscription for the named cluster.
	//
	// The returned cancel function must be called when the subscription is no
	// longer needed. It is safe to call cancel multiple times.
	SubscribeToCluster(clusterName string) (cancel func())
}

// clusterRef represents a reference to a cluster and maintains reference count
// of the number of users of the cluster.
type clusterRef struct {
	// Access to these field is protected by DependencyManager's mutex and so
	// they don't need to be atomic.

	// staticRefCount comes from cluster being specified in the route
	// configuration.
	staticRefCount int32
	// dynamicRefCount comes from cluster being referenced by RPCs or being
	// dynamically subscribed by the balancers in case of cluster specifier
	// plugin being used.
	dynamicRefCount int32
}

// SubscribeToCluster increments the reference count for the cluster. If the
// cluster is not already being tracked, it is added to the clusterSubscriptions
// map. It returns a function to unsubscribe from the cluster i.e. decrease its
// refcount. This returned function is idempotent, meaning it can be called
// multiple times without any additional effect. Calling Subscribe in a blocking
// manner while handling an update will lead to a deadlock.
func (m *DependencyManager) SubscribeToCluster(name string) func() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If the cluster is already being tracked, increment its dynamic refcount
	// and return.
	subs, ok := m.clusterSubscriptions[name]
	if ok {
		subs.dynamicRefCount++
		return sync.OnceFunc(func() { m.unsubscribeFromCluster(name) })
	}

	// If the cluster is not being tracked, add it with dynamic refcount as 1.
	m.clusterSubscriptions[name] = &clusterRef{
		dynamicRefCount: 1,
	}
	m.maybeSendUpdateLocked()
	return sync.OnceFunc(func() { m.unsubscribeFromCluster(name) })
}

// unsubscribeFromCluster decrements the reference count for the cluster. If
// both the reference counts reaches zero, it removes the cluster from the
// clusterSubscriptions map in the DependencyManager. Calling
// unsubscribeFromCluster in a blocking manner while handling an update will
// lead to a deadlock.
func (m *DependencyManager) unsubscribeFromCluster(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.clusterSubscriptions[name]
	c.dynamicRefCount--
	// This should not happen as unsubscribe returned from the
	// ClusterSubscription is wrapped in sync.OnceFunc()
	if c.dynamicRefCount < 0 {
		m.logger.Errorf("Reference count for a cluster dropped below zero")
	}
	if c.dynamicRefCount == 0 && c.staticRefCount == 0 {
		delete(m.clusterSubscriptions, name)
		// Since this cluster has no more references, cancel the watch for it.
		m.maybeSendUpdateLocked()
	}
}
