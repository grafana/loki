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
 */

package server

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/status"
)

const (
	// An unspecified destination or source prefix should be considered a less
	// specific match than a wildcard prefix, `0.0.0.0/0` or `::/0`. Also, an
	// unspecified prefix should match most v4 and v6 addresses compared to the
	// wildcard prefixes which match only a specific network (v4 or v6).
	//
	// We use these constants when looking up the most specific prefix match. A
	// wildcard prefix will match 0 bits, and to make sure that a wildcard
	// prefix is considered a more specific match than an unspecified prefix, we
	// use a value of -1 for the latter.
	noPrefixMatch          = -2
	unspecifiedPrefixMatch = -1
)

// filterChainManager contains the match criteria specified through filter
// chains in a single Listener resource.
//
// For the match criteria and the filter chains, we need to use package local
// structs that are very similar to the xdsresource structs. This is because the
// xdsresource structs are meant to contain only configuration and not runtime
// state. Here, we need to store runtime state such as the usable route
// configuration.
type filterChainManager struct {
	dstPrefixes        []*destPrefixEntry // Linear lookup list.
	defaultFilterChain *filterChain       // Default filter chain, if specified.
	filterChains       []*filterChain     // Filter chains managed by this filter chain manager.
	routeConfigNames   map[string]bool    // Route configuration names which need to be dynamically queried.
}

func newFilterChainManager(filterChainConfigs *xdsresource.NetworkFilterChainMap, defFilterChainConfig *xdsresource.NetworkFilterChainConfig) *filterChainManager {
	fcMgr := &filterChainManager{routeConfigNames: make(map[string]bool)}

	if filterChainConfigs != nil {
		for _, entry := range filterChainConfigs.DstPrefixes {
			dstEntry := &destPrefixEntry{prefix: entry.Prefix}

			for i, srcPrefixes := range entry.SourceTypeArr {
				if len(srcPrefixes.Entries) == 0 {
					continue
				}
				stDest := &sourcePrefixes{}
				dstEntry.srcTypeArr[i] = stDest
				for _, srcEntryConfig := range srcPrefixes.Entries {
					srcEntry := &sourcePrefixEntry{
						prefix:     srcEntryConfig.Prefix,
						srcPortMap: make(map[int]*filterChain, len(srcEntryConfig.PortMap)),
					}
					stDest.srcPrefixes = append(stDest.srcPrefixes, srcEntry)

					for port, fcConfig := range srcEntryConfig.PortMap {
						fc := fcMgr.filterChainFromConfig(&fcConfig)
						if fc.routeConfigName != "" {
							fcMgr.routeConfigNames[fc.routeConfigName] = true
						}
						srcEntry.srcPortMap[port] = fc
						fcMgr.filterChains = append(fcMgr.filterChains, fc)
					}
				}
			}
			fcMgr.dstPrefixes = append(fcMgr.dstPrefixes, dstEntry)
		}
	}

	if defFilterChainConfig != nil && !defFilterChainConfig.IsEmpty() {
		fc := fcMgr.filterChainFromConfig(defFilterChainConfig)
		if fc.routeConfigName != "" {
			fcMgr.routeConfigNames[fc.routeConfigName] = true
		}
		fcMgr.defaultFilterChain = fc
		fcMgr.filterChains = append(fcMgr.filterChains, fc)
	}

	return fcMgr
}

func (fcm *filterChainManager) filterChainFromConfig(config *xdsresource.NetworkFilterChainConfig) *filterChain {
	fc := &filterChain{
		securityCfg:              config.SecurityCfg,
		routeConfigName:          config.HTTPConnMgr.RouteConfigName,
		inlineRouteConfig:        config.HTTPConnMgr.InlineRouteConfig,
		httpFilters:              config.HTTPConnMgr.HTTPFilters,
		usableRouteConfiguration: &atomic.Pointer[usableRouteConfiguration]{}, // Active state
	}
	fc.usableRouteConfiguration.Store(&usableRouteConfiguration{})
	return fc
}

func (fcm *filterChainManager) stop() {
	for _, fc := range fcm.filterChains {
		urc := fc.usableRouteConfiguration.Load()
		if urc.err != nil {
			continue
		}
		for _, vh := range urc.vhs {
			for _, r := range vh.routes {
				if r.interceptor != nil {
					r.interceptor.Close()
				}
			}
		}
		fc.stop()
	}
}

// destPrefixEntry contains a destination prefix entry and associated source
// type matchers.
type destPrefixEntry struct {
	prefix     netip.Prefix
	srcTypeArr sourceTypesArray
}

// An array for the fixed number of source types that we have.
type sourceTypesArray [3]*sourcePrefixes

// sourceType specifies the connection source IP match type.
type sourceType int

const (
	sourceTypeAny            sourceType = iota // matches connection attempts from any source
	sourceTypeSameOrLoopback                   // matches connection attempts from the same host
	sourceTypeExternal                         // matches connection attempts from a different host
)

// sourcePrefixes contains a list of source prefix entries.
type sourcePrefixes struct {
	srcPrefixes []*sourcePrefixEntry
}

// sourcePrefixEntry contains a source prefix entry and associated source port
// matchers.
type sourcePrefixEntry struct {
	prefix     netip.Prefix
	srcPortMap map[int]*filterChain
}

// filterChain captures information from within a FilterChain message in a
// Listener resource. This struct contains the active state of a filter chain,
// which includes the usable route configuration.
type filterChain struct {
	securityCfg              *xdsresource.SecurityConfig
	httpFilters              []xdsresource.HTTPFilter
	serverFilters            []httpfilter.ServerFilter // Server filters with reference counts, stored for cleanup purposes.
	routeConfigName          string
	inlineRouteConfig        *xdsresource.RouteConfigUpdate
	usableRouteConfiguration *atomic.Pointer[usableRouteConfiguration]
}

// usableRouteConfiguration contains a matchable route configuration, with
// instantiated HTTP Filters per route.
type usableRouteConfiguration struct {
	vhs    []virtualHostWithInterceptors
	err    error
	nodeID string // For logging purposes. Populated by the listener wrapper.
}

// virtualHostWithInterceptors captures information present in a VirtualHost
// update, and also contains routes with instantiated HTTP Filters.
type virtualHostWithInterceptors struct {
	domains []string
	routes  []routeWithInterceptors
}

// routeWithInterceptors captures information in a Route, and contains
// a usable matcher and also instantiated HTTP Filters.
type routeWithInterceptors struct {
	matcher     *xdsresource.CompositeMatcher
	actionType  xdsresource.RouteActionType
	interceptor resolver.ServerInterceptor
}

type lookupParams struct {
	isUnspecifiedListener bool       // Whether the server is listening on a wildcard address.
	dstAddr               netip.Addr // dstAddr is the local address of an incoming connection.
	srcAddr               netip.Addr // srcAddr is the remote address of an incoming connection.
	srcPort               int        // srcPort is the remote port of an incoming connection.
}

// lookup returns the most specific matching filter chain to be used for an
// incoming connection on the server side. Returns a non-nil error if no
// matching filter chain could be found.
func (fcm *filterChainManager) lookup(params lookupParams) (*filterChain, error) {
	dstPrefixes := filterByDestinationPrefixes(fcm.dstPrefixes, params.isUnspecifiedListener, params.dstAddr)
	if len(dstPrefixes) == 0 {
		if fcm.defaultFilterChain != nil {
			return fcm.defaultFilterChain, nil
		}
		return nil, fmt.Errorf("no matching filter chain based on destination prefix match for %+v", params)
	}

	srcType := sourceTypeExternal
	if params.srcAddr == params.dstAddr || params.srcAddr.IsLoopback() {
		srcType = sourceTypeSameOrLoopback
	}
	srcPrefixes := filterBySourceType(dstPrefixes, srcType)
	if len(srcPrefixes) == 0 {
		if fcm.defaultFilterChain != nil {
			return fcm.defaultFilterChain, nil
		}
		return nil, fmt.Errorf("no matching filter chain based on source type match for %+v", params)
	}
	srcPrefixEntry, err := filterBySourcePrefixes(srcPrefixes, params.srcAddr)
	if err != nil {
		return nil, err
	}
	if fc := filterBySourcePorts(srcPrefixEntry, params.srcPort); fc != nil {
		return fc, nil
	}
	if fcm.defaultFilterChain != nil {
		return fcm.defaultFilterChain, nil
	}
	return nil, fmt.Errorf("no matching filter chain after all match criteria for %+v", params)
}

// filterByDestinationPrefixes is the first stage of the filter chain
// matching algorithm. It takes the complete set of configured filter chain
// matchers and returns the most specific matchers based on the destination
// prefix match criteria (the prefixes which match the most number of bits).
func filterByDestinationPrefixes(dstPrefixes []*destPrefixEntry, isUnspecified bool, dstAddr netip.Addr) []*destPrefixEntry {
	if !isUnspecified {
		// Destination prefix matchers are considered only when the listener is
		// bound to the wildcard address.
		return dstPrefixes
	}

	var matchingDstPrefixes []*destPrefixEntry
	maxSubnetMatch := noPrefixMatch
	for _, entry := range dstPrefixes {
		if entry.prefix.IsValid() && !entry.prefix.Contains(dstAddr) {
			// Skip prefixes which don't match.
			continue
		}
		// For unspecified prefixes, since we do not store a real netip.Prefix
		// inside prefix, we do not perform a match. Instead we simply set
		// the matchSize to -1, which is less than the matchSize (0) for a
		// wildcard prefix.
		matchSize := unspecifiedPrefixMatch
		if entry.prefix.IsValid() {
			matchSize = entry.prefix.Bits()
		}
		if matchSize < maxSubnetMatch {
			continue
		}
		if matchSize > maxSubnetMatch {
			maxSubnetMatch = matchSize
			matchingDstPrefixes = make([]*destPrefixEntry, 0, 1)
		}
		matchingDstPrefixes = append(matchingDstPrefixes, entry)
	}
	return matchingDstPrefixes
}

// filterBySourceType is the second stage of the matching algorithm. It
// trims the filter chains based on the most specific source type match.
//
// srcType is one of sourceTypeSameOrLoopback or sourceTypeExternal.
func filterBySourceType(dstPrefixes []*destPrefixEntry, srcType sourceType) []*sourcePrefixes {
	var (
		srcPrefixes      []*sourcePrefixes
		bestSrcTypeMatch sourceType
	)
	for _, entry := range dstPrefixes {
		match := srcType
		srcPrefix := entry.srcTypeArr[srcType]
		if srcPrefix == nil {
			match = sourceTypeAny
			srcPrefix = entry.srcTypeArr[sourceTypeAny]
		}
		if match < bestSrcTypeMatch {
			continue
		}
		if match > bestSrcTypeMatch {
			bestSrcTypeMatch = match
			srcPrefixes = make([]*sourcePrefixes, 0)
		}
		if srcPrefix != nil {
			// The source type array always has 3 entries, but these could be
			// nil if the appropriate source type match was not specified.
			srcPrefixes = append(srcPrefixes, srcPrefix)
		}
	}
	return srcPrefixes
}

// filterBySourcePrefixes is the third stage of the filter chain matching
// algorithm. It trims the filter chains based on the source prefix. At most one
// filter chain with the most specific match progress to the next stage.
func filterBySourcePrefixes(srcPrefixes []*sourcePrefixes, srcAddr netip.Addr) (*sourcePrefixEntry, error) {
	var matchingSrcPrefixes []*sourcePrefixEntry
	maxSubnetMatch := noPrefixMatch
	for _, sp := range srcPrefixes {
		for _, entry := range sp.srcPrefixes {
			if entry.prefix.IsValid() && !entry.prefix.Contains(srcAddr) {
				// Skip prefixes which don't match.
				continue
			}
			// For unspecified prefixes, since we do not store a real netip.Prefix
			// inside prefix, we do not perform a match. Instead we simply set
			// the matchSize to -1, which is less than the matchSize (0) for a
			// wildcard prefix.
			matchSize := unspecifiedPrefixMatch
			if entry.prefix.IsValid() {
				matchSize = entry.prefix.Bits()
			}
			if matchSize < maxSubnetMatch {
				continue
			}
			if matchSize > maxSubnetMatch {
				maxSubnetMatch = matchSize
				matchingSrcPrefixes = make([]*sourcePrefixEntry, 0, 1)
			}
			matchingSrcPrefixes = append(matchingSrcPrefixes, entry)
		}
	}
	if len(matchingSrcPrefixes) == 0 {
		// Finding no match is not an error condition. The caller will end up
		// using the default filter chain if one was configured.
		return nil, nil
	}
	if len(matchingSrcPrefixes) != 1 {
		// We expect at most a single matching source prefix entry at this
		// point. If we have multiple entries here, and some of their source
		// port matchers had wildcard entries, we could be left with more than
		// one matching filter chain and hence would have been flagged as an
		// invalid configuration at config validation time.
		return nil, errors.New("multiple matching filter chains")
	}
	return matchingSrcPrefixes[0], nil
}

// filterBySourcePorts is the last stage of the filter chain matching
// algorithm. It trims the filter chains based on the source ports.
func filterBySourcePorts(spe *sourcePrefixEntry, srcPort int) *filterChain {
	if spe == nil {
		return nil
	}
	// A match could be a wildcard match (this happens when the match
	// criteria does not specify source ports) or a specific port match (this
	// happens when the match criteria specifies a set of ports and the source
	// port of the incoming connection matches one of the specified ports). The
	// latter is considered to be a more specific match.
	if fc := spe.srcPortMap[srcPort]; fc != nil {
		return fc
	}
	if fc := spe.srcPortMap[0]; fc != nil {
		return fc
	}
	return nil
}

func (fc *filterChain) stop() {
	for _, sf := range fc.serverFilters {
		sf.Close()
	}
}

// serverFilterProvider is used to get a ServerFilter.
//
// This functionality is provided by the listener wrapper, which maintains a map
// of reference counted ServerFilters keyed by {filter_name + type_urls} for all
// filters across all filter chains, and ensures that the same filter instance
// is reused across resource updates. This allows filter instances to retain
// state across resource updates.
type serverFilterProvider func(filter xdsresource.HTTPFilter) (httpfilter.ServerFilter, error)

// constructUsableRouteConfiguration takes Route Configuration and converts it
// into matchable route configuration, with instantiated HTTP Filters per route.
func (fc *filterChain) constructUsableRouteConfiguration(config xdsresource.RouteConfigUpdate, provider serverFilterProvider) *usableRouteConfiguration {
	vhs := make([]virtualHostWithInterceptors, 0, len(config.VirtualHosts))
	var serverFilters []httpfilter.ServerFilter
	for _, vh := range config.VirtualHosts {
		vhwi, sfs, err := fc.convertVirtualHost(vh, provider)
		if err != nil {
			for _, sf := range serverFilters {
				sf.Close()
			}
			// Non nil if (lds + rds) fails, shouldn't happen since validated by
			// xDS Client, treat as L7 error but shouldn't happen.
			return &usableRouteConfiguration{err: fmt.Errorf("virtual host construction: %v", err)}
		}
		vhs = append(vhs, vhwi)
		serverFilters = append(serverFilters, sfs...)
	}

	// Release references to old server filters before replacing with new ones.
	for _, sf := range fc.serverFilters {
		sf.Close()
	}
	fc.serverFilters = serverFilters

	return &usableRouteConfiguration{vhs: vhs}
}

func (fc *filterChain) convertVirtualHost(virtualHost *xdsresource.VirtualHost, provider serverFilterProvider) (_ virtualHostWithInterceptors, _ []httpfilter.ServerFilter, err error) {
	var serverFilters []httpfilter.ServerFilter
	defer func() {
		if err != nil {
			for _, sf := range serverFilters {
				sf.Close()
			}
		}
	}()

	rs := make([]routeWithInterceptors, len(virtualHost.Routes))
	for i, r := range virtualHost.Routes {
		rs[i].actionType = r.ActionType
		rs[i].matcher = xdsresource.RouteToMatcher(r)
		interceptor, sfs, err := fc.newInterceptor(r.HTTPFilterConfigOverride, virtualHost.HTTPFilterConfigOverride, provider)
		if err != nil {
			return virtualHostWithInterceptors{}, nil, err
		}
		serverFilters = append(serverFilters, sfs...)
		rs[i].interceptor = interceptor
	}
	return virtualHostWithInterceptors{domains: virtualHost.Domains, routes: rs}, serverFilters, nil
}

// statusErrWithNodeID returns an error produced by the status package with the
// specified code and message, and includes the xDS node ID.
func (rc *usableRouteConfiguration) statusErrWithNodeID(c codes.Code, msg string, args ...any) error {
	return status.Error(c, fmt.Sprintf("[xDS node id: %v]: %s", rc.nodeID, fmt.Sprintf(msg, args...)))
}

func (fc *filterChain) newInterceptor(routeOverride, virtualHostOverride map[string]httpfilter.FilterConfig, provider serverFilterProvider) (_ resolver.ServerInterceptor, _ []httpfilter.ServerFilter, err error) {
	serverFilters := []httpfilter.ServerFilter{}
	interceptors := make([]resolver.ServerInterceptor, 0, len(fc.httpFilters))
	defer func() {
		if err != nil {
			for _, sf := range serverFilters {
				sf.Close()
			}
			for _, i := range interceptors {
				i.Close()
			}
		}
	}()

	for _, filter := range fc.httpFilters {
		// Route is highest priority on server side, as there is no concept
		// of an upstream cluster on server side.
		override := routeOverride[filter.Name]
		if override == nil {
			// Virtual Host is second priority.
			override = virtualHostOverride[filter.Name]
		}

		serverFilter, err := provider(filter)
		if err != nil {
			return nil, nil, err
		}
		serverFilters = append(serverFilters, serverFilter)
		i, err := serverFilter.BuildServerInterceptor(filter.Config, override)
		if err != nil {
			return nil, nil, fmt.Errorf("error constructing filter: %v", err)
		}
		if i != nil {
			interceptors = append(interceptors, i)
		}
	}

	return &interceptorList{interceptors: interceptors}, serverFilters, nil
}

type interceptorList struct {
	interceptors []resolver.ServerInterceptor
}

func (il *interceptorList) AllowRPC(ctx context.Context) error {
	for _, i := range il.interceptors {
		if err := i.AllowRPC(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (il *interceptorList) Close() {
	for _, i := range il.interceptors {
		i.Close()
	}
}

// refCountedServerFilter wraps a ServerFilter along with a reference count.
// This is used to manage server filters that are shared across filter chains
// within a filter chain manager.
type refCountedServerFilter struct {
	httpfilter.ServerFilter
	refCnt atomic.Int32
}

func (rsf *refCountedServerFilter) incRef() {
	rsf.refCnt.Add(1)
}

func (rsf *refCountedServerFilter) Close() {
	if rsf.refCnt.Add(-1) == 0 {
		rsf.ServerFilter.Close()
	}
}

// newServerFilterKey generates a key for the given filter using the filter name
// and type URLs. This is used for storing ServerFilters in a map.
func newServerFilterKey(f *xdsresource.HTTPFilter) serverFilterKey {
	typeURLs := slices.Clone(f.Filter.TypeURLs())
	slices.Sort(typeURLs)
	return serverFilterKey{
		name:     f.Name,
		typeURLs: strings.Join(typeURLs, ":"),
	}
}

type serverFilterKey struct {
	name     string
	typeURLs string
}

func (f *serverFilterKey) String() string {
	return f.name + ":" + f.typeURLs
}
