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

package cdsbalancer

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"google.golang.org/grpc/internal/balancer/weight"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/hierarchy"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	xdsinternal "google.golang.org/grpc/internal/xds"
	"google.golang.org/grpc/internal/xds/balancer/clusterimpl"
	"google.golang.org/grpc/internal/xds/balancer/outlierdetection"
	"google.golang.org/grpc/internal/xds/balancer/priority"
	"google.golang.org/grpc/internal/xds/balancer/wrrlocality"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/resolver"
)

const million = 1000000

// priorityConfig is config for one priority. For example, if there's an EDS and
// a DNS, the priority list will be [priorityConfig{EDS}, priorityConfig{DNS}].
//
// Each priorityConfig corresponds to one leaf cluster retrieved from XDSConfig
// for the top-level cluster.
type priorityConfig struct {
	// clusterConfig has the cluster update as well as EDS or DNS endpoints
	// depending on the leaf cluster type.
	clusterConfig *xdsresource.ClusterConfig
	// outlierDetection is the Outlier Detection LB configuration for this
	// priority.
	outlierDetection outlierdetection.LBConfig
	// Each leaf cluster has a name generator so that the child policies can
	// reuse names between updates (EDS updates for example).
	childNameGen *nameGenerator
}

// hostName returns the name of the host for the given cluster.
//
// For EDS, it's the EDSServiceName (or ClusterName if empty).
// For DNS, it's the DNSHostName.
func hostName(clusterName string, update xdsresource.ClusterUpdate) string {
	switch update.ClusterType {
	case xdsresource.ClusterTypeEDS:
		if update.EDSServiceName != "" {
			return update.EDSServiceName
		}
		return clusterName
	case xdsresource.ClusterTypeLogicalDNS:
		return update.DNSHostName
	default:
		return ""
	}
}

// buildPriorityConfigJSON builds balancer config for the passed in
// priorities.
//
// The built tree of balancers (see test for the output struct).
//
//	          ┌────────┐
//	          │priority│
//	          └┬──────┬┘
//	           │      │
//	┌──────────▼─┐  ┌─▼──────────┐
//	│cluster_impl│  │cluster_impl│
//	└──────┬─────┘  └─────┬──────┘
//	       │              │
//	┌──────▼─────┐  ┌─────▼──────┐
//	│xDSLBPolicy │  │xDSLBPolicy │ (Locality and Endpoint picking layer)
//	└────────────┘  └────────────┘
func buildPriorityConfigJSON(priorities []*priorityConfig, xdsLBPolicy *internalserviceconfig.BalancerConfig) ([]byte, []resolver.Endpoint, error) {
	pc, endpoints, err := buildPriorityConfig(priorities, xdsLBPolicy)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build priority config: %v", err)
	}
	ret, err := json.Marshal(pc)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal built priority config struct into json: %v", err)
	}
	return ret, endpoints, nil
}

func buildPriorityConfig(priorities []*priorityConfig, xdsLBPolicy *internalserviceconfig.BalancerConfig) (*priority.LBConfig, []resolver.Endpoint, error) {
	var (
		retConfig    = &priority.LBConfig{Children: make(map[string]*priority.Child)}
		retEndpoints []resolver.Endpoint
	)
	for _, p := range priorities {
		clusterUpdate := p.clusterConfig.Cluster
		switch clusterUpdate.ClusterType {
		case xdsresource.ClusterTypeEDS:
			names, configs, endpoints, err := buildClusterImplConfigForEDS(p.childNameGen, p.clusterConfig, xdsLBPolicy)
			if err != nil {
				return nil, nil, err
			}
			retConfig.Priorities = append(retConfig.Priorities, names...)
			retEndpoints = append(retEndpoints, endpoints...)
			odCfgs := convertClusterImplMapToOutlierDetection(configs, p.outlierDetection)
			for n, c := range odCfgs {
				retConfig.Children[n] = &priority.Child{
					Config: &internalserviceconfig.BalancerConfig{Name: outlierdetection.Name, Config: c},
					// Ignore all re-resolution from EDS children.
					IgnoreReresolutionRequests: true,
				}
			}
			continue
		case xdsresource.ClusterTypeLogicalDNS:
			name, config, endpoints := buildClusterImplConfigForDNS(p.childNameGen, p.clusterConfig, xdsLBPolicy)
			retConfig.Priorities = append(retConfig.Priorities, name)
			retEndpoints = append(retEndpoints, endpoints...)
			odCfg := makeClusterImplOutlierDetectionChild(config, p.outlierDetection)
			retConfig.Children[name] = &priority.Child{
				Config: &internalserviceconfig.BalancerConfig{Name: outlierdetection.Name, Config: odCfg},
				// Not ignore re-resolution from DNS children, they will trigger
				// DNS to re-resolve.
				IgnoreReresolutionRequests: false,
			}
			continue
		}
	}
	return retConfig, retEndpoints, nil
}

func convertClusterImplMapToOutlierDetection(ciCfgs map[string]*clusterimpl.LBConfig, odCfg outlierdetection.LBConfig) map[string]*outlierdetection.LBConfig {
	odCfgs := make(map[string]*outlierdetection.LBConfig, len(ciCfgs))
	for n, c := range ciCfgs {
		odCfgs[n] = makeClusterImplOutlierDetectionChild(c, odCfg)
	}
	return odCfgs
}

func makeClusterImplOutlierDetectionChild(ciCfg *clusterimpl.LBConfig, odCfg outlierdetection.LBConfig) *outlierdetection.LBConfig {
	odCfgRet := odCfg
	odCfgRet.ChildPolicy = &internalserviceconfig.BalancerConfig{Name: clusterimpl.Name, Config: ciCfg}
	return &odCfgRet
}

func buildClusterImplConfigForDNS(g *nameGenerator, config *xdsresource.ClusterConfig, xdsLBPolicy *internalserviceconfig.BalancerConfig) (string, *clusterimpl.LBConfig, []resolver.Endpoint) {
	pName := fmt.Sprintf("priority-%v", g.prefix)
	clusterUpdate := config.Cluster
	lbconfig := &clusterimpl.LBConfig{
		Cluster:     clusterUpdate.ClusterName,
		ChildPolicy: xdsLBPolicy,
	}
	endpoints := config.EndpointConfig.DNSEndpoints.Endpoints
	if len(endpoints) == 0 {
		return pName, lbconfig, nil
	}
	var retEndpoint resolver.Endpoint
	for _, e := range endpoints {
		// LOGICAL_DNS requires all resolved addresses to be grouped into a
		// single logical endpoint. We iterate over the input endpoints and
		// aggregate their addresses into a new endpoint variable.
		retEndpoint.Addresses = append(retEndpoint.Addresses, e.Addresses...)
	}
	// Even though localities are not a thing for the LOGICAL_DNS cluster and
	// its endpoint(s), we add an empty locality attribute here to ensure that
	// LB policies that rely on locality information (like weighted_target)
	// continue to work.
	localityStr := xdsinternal.LocalityString(clients.Locality{})
	retEndpoint = xdsresource.SetHostname(hierarchy.SetInEndpoint(retEndpoint, []string{pName, localityStr}), clusterUpdate.DNSHostName)
	// Set the locality weight to 1. This is required because the child policy
	// like weighted_target which relies on locality weights to distribute
	// traffic. These policies may drop traffic if the weight is 0.
	retEndpoint = wrrlocality.SetAddrInfo(retEndpoint, wrrlocality.AddrInfo{LocalityWeight: 1})
	return pName, lbconfig, []resolver.Endpoint{retEndpoint}
}

// buildClusterImplConfigForEDS returns a list of cluster_impl configs, one for
// each priority, sorted by priority, and the addresses for each priority (with
// hierarchy attributes set).
//
// For example, if there are two priorities, the returned values will be
// - ["p0", "p1"]
// - map{"p0":p0_config, "p1":p1_config}
// - [p0_address_0, p0_address_1, p1_address_0, p1_address_1]
//   - p0 addresses' hierarchy attributes are set to p0
func buildClusterImplConfigForEDS(g *nameGenerator, config *xdsresource.ClusterConfig, xdsLBPolicy *internalserviceconfig.BalancerConfig) ([]string, map[string]*clusterimpl.LBConfig, []resolver.Endpoint, error) {
	edsUpdate := config.EndpointConfig.EDSUpdate

	// Localities of length 0 is triggered by an NACK or resource-not-found
	// error before update, or an empty localities list in an update. In either
	// case want to create a priority, and send down empty address list, causing
	// TF for that priority. "If any discovery mechanism instance experiences an
	// error retrieving data, and it has not previously reported any results, it
	// should report a result that is a single priority with no endpoints." -
	// A37
	priorities := [][]xdsresource.Locality{{}}
	if len(edsUpdate.Localities) != 0 {
		priorities = groupLocalitiesByPriority(edsUpdate.Localities)
	}
	retNames := g.generate(priorities)
	retConfigs := make(map[string]*clusterimpl.LBConfig, len(retNames))
	var retEndpoints []resolver.Endpoint
	for i, pName := range retNames {
		priorityLocalities := priorities[i]
		cfg, endpoints, err := priorityLocalitiesToClusterImpl(priorityLocalities, pName, *config.Cluster, xdsLBPolicy)
		if err != nil {
			return nil, nil, nil, err
		}
		retConfigs[pName] = cfg
		retEndpoints = append(retEndpoints, endpoints...)
	}
	return retNames, retConfigs, retEndpoints, nil
}

// groupLocalitiesByPriority returns the localities grouped by priority.
//
// The returned list is sorted from higher priority to lower. Each item in the
// list is a group of localities.
//
// For example, for L0-p0, L1-p0, L2-p1, results will be
// - [[L0, L1], [L2]]
func groupLocalitiesByPriority(localities []xdsresource.Locality) [][]xdsresource.Locality {
	priorities := make(map[int][]xdsresource.Locality)
	for _, locality := range localities {
		priority := int(locality.Priority)
		priorities[priority] = append(priorities[priority], locality)
	}
	// Sort the priorities based on the int value, deduplicate, and then turn
	// the sorted list into a string list. This will be child names, in priority
	// order.
	priorityIntSlice := slices.Sorted(maps.Keys(priorities))
	ret := make([][]xdsresource.Locality, 0, len(priorityIntSlice))
	for _, p := range priorityIntSlice {
		ret = append(ret, priorities[p])
	}
	return ret
}

// priorityLocalitiesToClusterImpl takes a list of localities (with the same
// priority), and generates a cluster impl policy config, and a list of
// addresses with their path hierarchy set to [priority-name, locality-name], so
// priority and the xDS LB Policy know which child policy each address is for.
func priorityLocalitiesToClusterImpl(localities []xdsresource.Locality, priorityName string, clusterUpdate xdsresource.ClusterUpdate, xdsLBPolicy *internalserviceconfig.BalancerConfig) (*clusterimpl.LBConfig, []resolver.Endpoint, error) {
	var retEndpoints []resolver.Endpoint

	// Compute the sum of locality weights to normalize locality weights. The
	// xDS client guarantees that the sum of locality weights (within a
	// priority) will not exceed MaxUint32.
	var localityWeightSum uint32
	for _, locality := range localities {
		localityWeightSum += locality.Weight
	}

	for _, locality := range localities {
		// Compute the sum of endpoint weights to normalize endpoint weights.
		// The xDS client does not currently guarantee that the sum of endpoint
		// weights (within a locality) will not exceed MaxUint32. TODO(i/8862):
		// Once the xDS client guarantees that the sum of endpoint weights does
		// not exceed MaxUint32, we can change the type of this variable from
		// uint64 to uint32.
		var endpointWeightSum uint64
		for _, endpoint := range locality.Endpoints {
			endpointWeightSum += uint64(endpoint.Weight)
		}

		localityStr := xdsinternal.LocalityString(locality.ID)
		for _, endpoint := range locality.Endpoints {
			// Filter out all "unhealthy" endpoints (unknown and healthy are
			// both considered to be healthy:
			// https://www.envoyproxy.io/docs/envoy/latest/api-v2/api/v2/core/health_check.proto#envoy-api-enum-core-healthstatus).
			if endpoint.HealthStatus != xdsresource.EndpointHealthStatusHealthy && endpoint.HealthStatus != xdsresource.EndpointHealthStatusUnknown {
				continue
			}

			// Create a copy of endpoint.ResolverEndpoint to avoid race between
			// the xDS Client (which owns this shared object in its cache) and
			// the Cluster Resolver (which is trying to modify attributes).
			resolverEndpoint := endpoint.ResolverEndpoint
			resolverEndpoint.Addresses = slices.Clone(endpoint.ResolverEndpoint.Addresses)

			resolverEndpoint = hierarchy.SetInEndpoint(resolverEndpoint, []string{priorityName, localityStr})
			resolverEndpoint = xdsinternal.SetLocalityIDInEndpoint(resolverEndpoint, locality.ID)
			// "To provide the xds_wrr_locality load balancer information about
			// locality weights received from EDS, the cluster resolver will
			// populate a new locality weight attribute for each address The
			// attribute will have the weight (as an integer) of the locality
			// the address is part of." - A52
			resolverEndpoint = wrrlocality.SetAddrInfo(resolverEndpoint, wrrlocality.AddrInfo{LocalityWeight: locality.Weight})

			if envconfig.PickFirstWeightedShuffling {
				normalizedLocalityWeight := fractionToFixedPoint(uint64(locality.Weight), uint64(localityWeightSum))
				normalizedEndpointWeight := fractionToFixedPoint(uint64(endpoint.Weight), endpointWeightSum)
				endpointWeight := fixedPointMultiply(normalizedEndpointWeight, normalizedLocalityWeight)
				if endpointWeight == 0 {
					endpointWeight = 1
				}
				resolverEndpoint = weight.Set(resolverEndpoint, weight.EndpointInfo{Weight: endpointWeight})
			} else {
				resolverEndpoint = weight.Set(resolverEndpoint, weight.EndpointInfo{Weight: locality.Weight * endpoint.Weight})
			}
			retEndpoints = append(retEndpoints, resolverEndpoint)
		}
	}
	return &clusterimpl.LBConfig{
		Cluster:     clusterUpdate.ClusterName,
		ChildPolicy: xdsLBPolicy,
	}, retEndpoints, nil
}

// fixedPointFractionalBits is the number of bits used for the fractional part
// of normalized endpoint and locality weights.
//
// We use the UQ1.31 fixed-point format (Unsigned, 1 integer bit, 31 fractional bits).
// This allows representing values in the range [0.0, 2.0) with a precision
// of 2^-31.
//
// Bit Layout:
// [ 31 ] [ 30 ................. 0 ]
//
//	|              |
//	|              +--- Fractional Part (31 bits)
//	+------------------ Integer Part (1 bit)
//
// See gRFC A113 for more details.
const fixedPointFractionalBits = 31

// fractionToFixedPoint converts a fraction represented by numerator and
// denominator to a fixed-point number between 0 and 1 represented as a uint32.
//
// The xDS client guarantees that the sum of locality weights (within a
// priority) will not exceed MaxUint32. TODO(i/8862): Once the xDS client
// guarantees that the sum of endpoint weights does not exceed MaxUint32, we can
// change the types of this function's arguments from uint64 to uint32.
func fractionToFixedPoint(numerator, denominator uint64) uint32 {
	return uint32(uint64(numerator) << fixedPointFractionalBits / uint64(denominator))
}

func fixedPointMultiply(a, b uint32) uint32 {
	return uint32((uint64(a) * uint64(b)) >> fixedPointFractionalBits)
}
