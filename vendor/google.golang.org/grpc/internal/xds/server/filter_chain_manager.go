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
	"errors"
	"fmt"
	"net"
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
			dstEntry := &destPrefixEntry{net: entry.Prefix}

			for i, srcPrefixes := range entry.SourceTypeArr {
				if len(srcPrefixes.Entries) == 0 {
					continue
				}
				stDest := &sourcePrefixes{}
				dstEntry.srcTypeArr[i] = stDest
				for _, srcEntryConfig := range srcPrefixes.Entries {
					srcEntry := &sourcePrefixEntry{
						net:        srcEntryConfig.Prefix,
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

// destPrefixEntry contains a destination prefix entry and associated source
// type matchers.
type destPrefixEntry struct {
	net        *net.IPNet
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
	net        *net.IPNet
	srcPortMap map[int]*filterChain
}

// filterChain captures information from within a FilterChain message in a
// Listener resource. This struct contains the active state of a filter chain,
// which includes the usable route configuration.
type filterChain struct {
	securityCfg              *xdsresource.SecurityConfig
	httpFilters              []xdsresource.HTTPFilter
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
	matcher      *xdsresource.CompositeMatcher
	actionType   xdsresource.RouteActionType
	interceptors []resolver.ServerInterceptor
}

type lookupParams struct {
	isUnspecifiedListener bool   // Whether the server is listening on a wildcard address.
	dstAddr               net.IP // dstAddr is the local address of an incoming connection.
	srcAddr               net.IP // srcAddr is the remote address of an incoming connection.
	srcPort               int    // srcPort is the remote port of an incoming connection.
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
	if params.srcAddr.Equal(params.dstAddr) || params.srcAddr.IsLoopback() {
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
func filterByDestinationPrefixes(dstPrefixes []*destPrefixEntry, isUnspecified bool, dstAddr net.IP) []*destPrefixEntry {
	if !isUnspecified {
		// Destination prefix matchers are considered only when the listener is
		// bound to the wildcard address.
		return dstPrefixes
	}

	var matchingDstPrefixes []*destPrefixEntry
	maxSubnetMatch := noPrefixMatch
	for _, prefix := range dstPrefixes {
		if prefix.net != nil && !prefix.net.Contains(dstAddr) {
			// Skip prefixes which don't match.
			continue
		}
		// For unspecified prefixes, since we do not store a real net.IPNet
		// inside prefix, we do not perform a match. Instead we simply set
		// the matchSize to -1, which is less than the matchSize (0) for a
		// wildcard prefix.
		matchSize := unspecifiedPrefixMatch
		if prefix.net != nil {
			matchSize, _ = prefix.net.Mask.Size()
		}
		if matchSize < maxSubnetMatch {
			continue
		}
		if matchSize > maxSubnetMatch {
			maxSubnetMatch = matchSize
			matchingDstPrefixes = make([]*destPrefixEntry, 0, 1)
		}
		matchingDstPrefixes = append(matchingDstPrefixes, prefix)
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
	for _, prefix := range dstPrefixes {
		match := srcType
		srcPrefix := prefix.srcTypeArr[srcType]
		if srcPrefix == nil {
			match = sourceTypeAny
			srcPrefix = prefix.srcTypeArr[sourceTypeAny]
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
func filterBySourcePrefixes(srcPrefixes []*sourcePrefixes, srcAddr net.IP) (*sourcePrefixEntry, error) {
	var matchingSrcPrefixes []*sourcePrefixEntry
	maxSubnetMatch := noPrefixMatch
	for _, sp := range srcPrefixes {
		for _, prefix := range sp.srcPrefixes {
			if prefix.net != nil && !prefix.net.Contains(srcAddr) {
				// Skip prefixes which don't match.
				continue
			}
			// For unspecified prefixes, since we do not store a real net.IPNet
			// inside prefix, we do not perform a match. Instead we simply set
			// the matchSize to -1, which is less than the matchSize (0) for a
			// wildcard prefix.
			matchSize := unspecifiedPrefixMatch
			if prefix.net != nil {
				matchSize, _ = prefix.net.Mask.Size()
			}
			if matchSize < maxSubnetMatch {
				continue
			}
			if matchSize > maxSubnetMatch {
				maxSubnetMatch = matchSize
				matchingSrcPrefixes = make([]*sourcePrefixEntry, 0, 1)
			}
			matchingSrcPrefixes = append(matchingSrcPrefixes, prefix)
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

// constructUsableRouteConfiguration takes Route Configuration and converts it
// into matchable route configuration, with instantiated HTTP Filters per route.
func (fc *filterChain) constructUsableRouteConfiguration(config xdsresource.RouteConfigUpdate) *usableRouteConfiguration {
	vhs := make([]virtualHostWithInterceptors, 0, len(config.VirtualHosts))
	for _, vh := range config.VirtualHosts {
		vhwi, err := fc.convertVirtualHost(vh)
		if err != nil {
			// Non nil if (lds + rds) fails, shouldn't happen since validated by
			// xDS Client, treat as L7 error but shouldn't happen.
			return &usableRouteConfiguration{err: fmt.Errorf("virtual host construction: %v", err)}
		}
		vhs = append(vhs, vhwi)
	}
	return &usableRouteConfiguration{vhs: vhs}
}

func (fc *filterChain) convertVirtualHost(virtualHost *xdsresource.VirtualHost) (virtualHostWithInterceptors, error) {
	rs := make([]routeWithInterceptors, len(virtualHost.Routes))
	for i, r := range virtualHost.Routes {
		rs[i].actionType = r.ActionType
		rs[i].matcher = xdsresource.RouteToMatcher(r)
		for _, filter := range fc.httpFilters {
			// Route is highest priority on server side, as there is no concept
			// of an upstream cluster on server side.
			override := r.HTTPFilterConfigOverride[filter.Name]
			if override == nil {
				// Virtual Host is second priority.
				override = virtualHost.HTTPFilterConfigOverride[filter.Name]
			}
			sb, ok := filter.Filter.(httpfilter.ServerInterceptorBuilder)
			if !ok {
				// Should not happen if it passed xdsClient validation.
				return virtualHostWithInterceptors{}, fmt.Errorf("filter does not support use in server")
			}
			si, err := sb.BuildServerInterceptor(filter.Config, override)
			if err != nil {
				return virtualHostWithInterceptors{}, fmt.Errorf("filter construction: %v", err)
			}
			if si != nil {
				rs[i].interceptors = append(rs[i].interceptors, si)
			}
		}
	}
	return virtualHostWithInterceptors{domains: virtualHost.Domains, routes: rs}, nil
}

// statusErrWithNodeID returns an error produced by the status package with the
// specified code and message, and includes the xDS node ID.
func (rc *usableRouteConfiguration) statusErrWithNodeID(c codes.Code, msg string, args ...any) error {
	return status.Error(c, fmt.Sprintf("[xDS node id: %v]: %s", rc.nodeID, fmt.Sprintf(msg, args...)))
}
