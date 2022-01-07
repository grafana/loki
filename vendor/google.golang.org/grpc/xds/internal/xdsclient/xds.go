/*
 *
 * Copyright 2020 gRPC authors.
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

package xdsclient

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	v1typepb "github.com/cncf/udpa/go/udpa/type/v1"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3aggregateclusterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/clusters/aggregate/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/protobuf/types/known/anypb"

	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/xds/env"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/httpfilter"
	"google.golang.org/grpc/xds/internal/version"
)

// TransportSocket proto message has a `name` field which is expected to be set
// to this value by the management server.
const transportSocketName = "envoy.transport_sockets.tls"

// UnmarshalListener processes resources received in an LDS response, validates
// them, and transforms them into a native struct which contains only fields we
// are interested in.
func UnmarshalListener(version string, resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]ListenerUpdate, UpdateMetadata, error) {
	update := make(map[string]ListenerUpdate)
	md, err := processAllResources(version, resources, logger, update)
	return update, md, err
}

func unmarshalListenerResource(r *anypb.Any, logger *grpclog.PrefixLogger) (string, ListenerUpdate, error) {
	if !IsListenerResource(r.GetTypeUrl()) {
		return "", ListenerUpdate{}, fmt.Errorf("unexpected resource type: %q ", r.GetTypeUrl())
	}
	// TODO: Pass version.TransportAPI instead of relying upon the type URL
	v2 := r.GetTypeUrl() == version.V2ListenerURL
	lis := &v3listenerpb.Listener{}
	if err := proto.Unmarshal(r.GetValue(), lis); err != nil {
		return "", ListenerUpdate{}, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	logger.Infof("Resource with name: %v, type: %T, contains: %v", lis.GetName(), lis, pretty.ToJSON(lis))

	lu, err := processListener(lis, logger, v2)
	if err != nil {
		return lis.GetName(), ListenerUpdate{}, err
	}
	lu.Raw = r
	return lis.GetName(), *lu, nil
}

func processListener(lis *v3listenerpb.Listener, logger *grpclog.PrefixLogger, v2 bool) (*ListenerUpdate, error) {
	if lis.GetApiListener() != nil {
		return processClientSideListener(lis, logger, v2)
	}
	return processServerSideListener(lis)
}

// processClientSideListener checks if the provided Listener proto meets
// the expected criteria. If so, it returns a non-empty routeConfigName.
func processClientSideListener(lis *v3listenerpb.Listener, logger *grpclog.PrefixLogger, v2 bool) (*ListenerUpdate, error) {
	update := &ListenerUpdate{}

	apiLisAny := lis.GetApiListener().GetApiListener()
	if !IsHTTPConnManagerResource(apiLisAny.GetTypeUrl()) {
		return nil, fmt.Errorf("unexpected resource type: %q", apiLisAny.GetTypeUrl())
	}
	apiLis := &v3httppb.HttpConnectionManager{}
	if err := proto.Unmarshal(apiLisAny.GetValue(), apiLis); err != nil {
		return nil, fmt.Errorf("failed to unmarshal api_listner: %v", err)
	}

	switch apiLis.RouteSpecifier.(type) {
	case *v3httppb.HttpConnectionManager_Rds:
		if apiLis.GetRds().GetConfigSource().GetAds() == nil {
			return nil, fmt.Errorf("ConfigSource is not ADS: %+v", lis)
		}
		name := apiLis.GetRds().GetRouteConfigName()
		if name == "" {
			return nil, fmt.Errorf("empty route_config_name: %+v", lis)
		}
		update.RouteConfigName = name
	case *v3httppb.HttpConnectionManager_RouteConfig:
		routeU, err := generateRDSUpdateFromRouteConfiguration(apiLis.GetRouteConfig(), logger, v2)
		if err != nil {
			return nil, fmt.Errorf("failed to parse inline RDS resp: %v", err)
		}
		update.InlineRouteConfig = &routeU
	case nil:
		return nil, fmt.Errorf("no RouteSpecifier: %+v", apiLis)
	default:
		return nil, fmt.Errorf("unsupported type %T for RouteSpecifier", apiLis.RouteSpecifier)
	}

	if v2 {
		return update, nil
	}

	// The following checks and fields only apply to xDS protocol versions v3+.

	update.MaxStreamDuration = apiLis.GetCommonHttpProtocolOptions().GetMaxStreamDuration().AsDuration()

	var err error
	if update.HTTPFilters, err = processHTTPFilters(apiLis.GetHttpFilters(), false); err != nil {
		return nil, err
	}

	return update, nil
}

func unwrapHTTPFilterConfig(config *anypb.Any) (proto.Message, string, error) {
	// The real type name is inside the TypedStruct.
	s := new(v1typepb.TypedStruct)
	if !ptypes.Is(config, s) {
		return config, config.GetTypeUrl(), nil
	}
	if err := ptypes.UnmarshalAny(config, s); err != nil {
		return nil, "", fmt.Errorf("error unmarshalling TypedStruct filter config: %v", err)
	}
	return s, s.GetTypeUrl(), nil
}

func validateHTTPFilterConfig(cfg *anypb.Any, lds, optional bool) (httpfilter.Filter, httpfilter.FilterConfig, error) {
	config, typeURL, err := unwrapHTTPFilterConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	filterBuilder := httpfilter.Get(typeURL)
	if filterBuilder == nil {
		if optional {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("no filter implementation found for %q", typeURL)
	}
	parseFunc := filterBuilder.ParseFilterConfig
	if !lds {
		parseFunc = filterBuilder.ParseFilterConfigOverride
	}
	filterConfig, err := parseFunc(config)
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing config for filter %q: %v", typeURL, err)
	}
	return filterBuilder, filterConfig, nil
}

func processHTTPFilterOverrides(cfgs map[string]*anypb.Any) (map[string]httpfilter.FilterConfig, error) {
	if len(cfgs) == 0 {
		return nil, nil
	}
	m := make(map[string]httpfilter.FilterConfig)
	for name, cfg := range cfgs {
		optional := false
		s := new(v3routepb.FilterConfig)
		if ptypes.Is(cfg, s) {
			if err := ptypes.UnmarshalAny(cfg, s); err != nil {
				return nil, fmt.Errorf("filter override %q: error unmarshalling FilterConfig: %v", name, err)
			}
			cfg = s.GetConfig()
			optional = s.GetIsOptional()
		}

		httpFilter, config, err := validateHTTPFilterConfig(cfg, false, optional)
		if err != nil {
			return nil, fmt.Errorf("filter override %q: %v", name, err)
		}
		if httpFilter == nil {
			// Optional configs are ignored.
			continue
		}
		m[name] = config
	}
	return m, nil
}

func processHTTPFilters(filters []*v3httppb.HttpFilter, server bool) ([]HTTPFilter, error) {
	ret := make([]HTTPFilter, 0, len(filters))
	seenNames := make(map[string]bool, len(filters))
	for _, filter := range filters {
		name := filter.GetName()
		if name == "" {
			return nil, errors.New("filter missing name field")
		}
		if seenNames[name] {
			return nil, fmt.Errorf("duplicate filter name %q", name)
		}
		seenNames[name] = true

		httpFilter, config, err := validateHTTPFilterConfig(filter.GetTypedConfig(), true, filter.GetIsOptional())
		if err != nil {
			return nil, err
		}
		if httpFilter == nil {
			// Optional configs are ignored.
			continue
		}
		if server {
			if _, ok := httpFilter.(httpfilter.ServerInterceptorBuilder); !ok {
				if filter.GetIsOptional() {
					continue
				}
				return nil, fmt.Errorf("HTTP filter %q not supported server-side", name)
			}
		} else if _, ok := httpFilter.(httpfilter.ClientInterceptorBuilder); !ok {
			if filter.GetIsOptional() {
				continue
			}
			return nil, fmt.Errorf("HTTP filter %q not supported client-side", name)
		}

		// Save name/config
		ret = append(ret, HTTPFilter{Name: name, Filter: httpFilter, Config: config})
	}
	return ret, nil
}

func processServerSideListener(lis *v3listenerpb.Listener) (*ListenerUpdate, error) {
	if n := len(lis.ListenerFilters); n != 0 {
		return nil, fmt.Errorf("unsupported field 'listener_filters' contains %d entries", n)
	}
	if useOrigDst := lis.GetUseOriginalDst(); useOrigDst != nil && useOrigDst.GetValue() {
		return nil, errors.New("unsupported field 'use_original_dst' is present and set to true")
	}
	addr := lis.GetAddress()
	if addr == nil {
		return nil, fmt.Errorf("no address field in LDS response: %+v", lis)
	}
	sockAddr := addr.GetSocketAddress()
	if sockAddr == nil {
		return nil, fmt.Errorf("no socket_address field in LDS response: %+v", lis)
	}
	lu := &ListenerUpdate{
		InboundListenerCfg: &InboundListenerConfig{
			Address: sockAddr.GetAddress(),
			Port:    strconv.Itoa(int(sockAddr.GetPortValue())),
		},
	}

	fcMgr, err := NewFilterChainManager(lis)
	if err != nil {
		return nil, err
	}
	lu.InboundListenerCfg.FilterChains = fcMgr
	return lu, nil
}

func processNetworkFilters(filters []*v3listenerpb.Filter) ([]HTTPFilter, error) {
	seenNames := make(map[string]bool, len(filters))
	seenHCM := false
	var httpFilters []HTTPFilter
	for _, filter := range filters {
		name := filter.GetName()
		if name == "" {
			return nil, fmt.Errorf("network filters {%+v} is missing name field in filter: {%+v}", filters, filter)
		}
		if seenNames[name] {
			return nil, fmt.Errorf("network filters {%+v} has duplicate filter name %q", filters, name)
		}
		seenNames[name] = true

		// Network filters have a oneof field named `config_type` where we
		// only support `TypedConfig` variant.
		switch typ := filter.GetConfigType().(type) {
		case *v3listenerpb.Filter_TypedConfig:
			// The typed_config field has an `anypb.Any` proto which could
			// directly contain the serialized bytes of the actual filter
			// configuration, or it could be encoded as a `TypedStruct`.
			// TODO: Add support for `TypedStruct`.
			tc := filter.GetTypedConfig()

			// The only network filter that we currently support is the v3
			// HttpConnectionManager. So, we can directly check the type_url
			// and unmarshal the config.
			// TODO: Implement a registry of supported network filters (like
			// we have for HTTP filters), when we have to support network
			// filters other than HttpConnectionManager.
			if tc.GetTypeUrl() != version.V3HTTPConnManagerURL {
				return nil, fmt.Errorf("network filters {%+v} has unsupported network filter %q in filter {%+v}", filters, tc.GetTypeUrl(), filter)
			}
			hcm := &v3httppb.HttpConnectionManager{}
			if err := ptypes.UnmarshalAny(tc, hcm); err != nil {
				return nil, fmt.Errorf("network filters {%+v} failed unmarshaling of network filter {%+v}: %v", filters, filter, err)
			}
			// "Any filters after HttpConnectionManager should be ignored during
			// connection processing but still be considered for validity.
			// HTTPConnectionManager must have valid http_filters." - A36
			filters, err := processHTTPFilters(hcm.GetHttpFilters(), true)
			if err != nil {
				return nil, fmt.Errorf("network filters {%+v} had invalid server side HTTP Filters {%+v}", filters, hcm.GetHttpFilters())
			}
			if !seenHCM {
				// TODO: Implement terminal filter logic, as per A36.
				httpFilters = filters
				seenHCM = true
			}
		default:
			return nil, fmt.Errorf("network filters {%+v} has unsupported config_type %T in filter %s", filters, typ, filter.GetName())
		}
	}
	if !seenHCM {
		return nil, fmt.Errorf("network filters {%+v} missing HttpConnectionManager filter", filters)
	}
	return httpFilters, nil
}

// UnmarshalRouteConfig processes resources received in an RDS response,
// validates them, and transforms them into a native struct which contains only
// fields we are interested in. The provided hostname determines the route
// configuration resources of interest.
func UnmarshalRouteConfig(version string, resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]RouteConfigUpdate, UpdateMetadata, error) {
	update := make(map[string]RouteConfigUpdate)
	md, err := processAllResources(version, resources, logger, update)
	return update, md, err
}

func unmarshalRouteConfigResource(r *anypb.Any, logger *grpclog.PrefixLogger) (string, RouteConfigUpdate, error) {
	if !IsRouteConfigResource(r.GetTypeUrl()) {
		return "", RouteConfigUpdate{}, fmt.Errorf("unexpected resource type: %q ", r.GetTypeUrl())
	}
	rc := &v3routepb.RouteConfiguration{}
	if err := proto.Unmarshal(r.GetValue(), rc); err != nil {
		return "", RouteConfigUpdate{}, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	logger.Infof("Resource with name: %v, type: %T, contains: %v.", rc.GetName(), rc, pretty.ToJSON(rc))

	// TODO: Pass version.TransportAPI instead of relying upon the type URL
	v2 := r.GetTypeUrl() == version.V2RouteConfigURL
	u, err := generateRDSUpdateFromRouteConfiguration(rc, logger, v2)
	if err != nil {
		return rc.GetName(), RouteConfigUpdate{}, err
	}
	u.Raw = r
	return rc.GetName(), u, nil
}

// generateRDSUpdateFromRouteConfiguration checks if the provided
// RouteConfiguration meets the expected criteria. If so, it returns a
// RouteConfigUpdate with nil error.
//
// A RouteConfiguration resource is considered valid when only if it contains a
// VirtualHost whose domain field matches the server name from the URI passed
// to the gRPC channel, and it contains a clusterName or a weighted cluster.
//
// The RouteConfiguration includes a list of VirtualHosts, which may have zero
// or more elements. We are interested in the element whose domains field
// matches the server name specified in the "xds:" URI. The only field in the
// VirtualHost proto that the we are interested in is the list of routes. We
// only look at the last route in the list (the default route), whose match
// field must be empty and whose route field must be set.  Inside that route
// message, the cluster field will contain the clusterName or weighted clusters
// we are looking for.
func generateRDSUpdateFromRouteConfiguration(rc *v3routepb.RouteConfiguration, logger *grpclog.PrefixLogger, v2 bool) (RouteConfigUpdate, error) {
	var vhs []*VirtualHost
	for _, vh := range rc.GetVirtualHosts() {
		routes, err := routesProtoToSlice(vh.Routes, logger, v2)
		if err != nil {
			return RouteConfigUpdate{}, fmt.Errorf("received route is invalid: %v", err)
		}
		vhOut := &VirtualHost{
			Domains: vh.GetDomains(),
			Routes:  routes,
		}
		if !v2 {
			cfgs, err := processHTTPFilterOverrides(vh.GetTypedPerFilterConfig())
			if err != nil {
				return RouteConfigUpdate{}, fmt.Errorf("virtual host %+v: %v", vh, err)
			}
			vhOut.HTTPFilterConfigOverride = cfgs
		}
		vhs = append(vhs, vhOut)
	}
	return RouteConfigUpdate{VirtualHosts: vhs}, nil
}

func routesProtoToSlice(routes []*v3routepb.Route, logger *grpclog.PrefixLogger, v2 bool) ([]*Route, error) {
	var routesRet []*Route

	for _, r := range routes {
		match := r.GetMatch()
		if match == nil {
			return nil, fmt.Errorf("route %+v doesn't have a match", r)
		}

		if len(match.GetQueryParameters()) != 0 {
			// Ignore route with query parameters.
			logger.Warningf("route %+v has query parameter matchers, the route will be ignored", r)
			continue
		}

		pathSp := match.GetPathSpecifier()
		if pathSp == nil {
			return nil, fmt.Errorf("route %+v doesn't have a path specifier", r)
		}

		var route Route
		switch pt := pathSp.(type) {
		case *v3routepb.RouteMatch_Prefix:
			route.Prefix = &pt.Prefix
		case *v3routepb.RouteMatch_Path:
			route.Path = &pt.Path
		case *v3routepb.RouteMatch_SafeRegex:
			regex := pt.SafeRegex.GetRegex()
			re, err := regexp.Compile(regex)
			if err != nil {
				return nil, fmt.Errorf("route %+v contains an invalid regex %q", r, regex)
			}
			route.Regex = re
		default:
			return nil, fmt.Errorf("route %+v has an unrecognized path specifier: %+v", r, pt)
		}

		if caseSensitive := match.GetCaseSensitive(); caseSensitive != nil {
			route.CaseInsensitive = !caseSensitive.Value
		}

		for _, h := range match.GetHeaders() {
			var header HeaderMatcher
			switch ht := h.GetHeaderMatchSpecifier().(type) {
			case *v3routepb.HeaderMatcher_ExactMatch:
				header.ExactMatch = &ht.ExactMatch
			case *v3routepb.HeaderMatcher_SafeRegexMatch:
				regex := ht.SafeRegexMatch.GetRegex()
				re, err := regexp.Compile(regex)
				if err != nil {
					return nil, fmt.Errorf("route %+v contains an invalid regex %q", r, regex)
				}
				header.RegexMatch = re
			case *v3routepb.HeaderMatcher_RangeMatch:
				header.RangeMatch = &Int64Range{
					Start: ht.RangeMatch.Start,
					End:   ht.RangeMatch.End,
				}
			case *v3routepb.HeaderMatcher_PresentMatch:
				header.PresentMatch = &ht.PresentMatch
			case *v3routepb.HeaderMatcher_PrefixMatch:
				header.PrefixMatch = &ht.PrefixMatch
			case *v3routepb.HeaderMatcher_SuffixMatch:
				header.SuffixMatch = &ht.SuffixMatch
			default:
				return nil, fmt.Errorf("route %+v has an unrecognized header matcher: %+v", r, ht)
			}
			header.Name = h.GetName()
			invert := h.GetInvertMatch()
			header.InvertMatch = &invert
			route.Headers = append(route.Headers, &header)
		}

		if fr := match.GetRuntimeFraction(); fr != nil {
			d := fr.GetDefaultValue()
			n := d.GetNumerator()
			switch d.GetDenominator() {
			case v3typepb.FractionalPercent_HUNDRED:
				n *= 10000
			case v3typepb.FractionalPercent_TEN_THOUSAND:
				n *= 100
			case v3typepb.FractionalPercent_MILLION:
			}
			route.Fraction = &n
		}

		route.WeightedClusters = make(map[string]WeightedCluster)
		action := r.GetRoute()

		// Hash Policies are only applicable for a Ring Hash LB.
		if env.RingHashSupport {
			hp, err := hashPoliciesProtoToSlice(action.HashPolicy, logger)
			if err != nil {
				return nil, err
			}
			route.HashPolicies = hp
		}

		switch a := action.GetClusterSpecifier().(type) {
		case *v3routepb.RouteAction_Cluster:
			route.WeightedClusters[a.Cluster] = WeightedCluster{Weight: 1}
		case *v3routepb.RouteAction_WeightedClusters:
			wcs := a.WeightedClusters
			var totalWeight uint32
			for _, c := range wcs.Clusters {
				w := c.GetWeight().GetValue()
				if w == 0 {
					continue
				}
				wc := WeightedCluster{Weight: w}
				if !v2 {
					cfgs, err := processHTTPFilterOverrides(c.GetTypedPerFilterConfig())
					if err != nil {
						return nil, fmt.Errorf("route %+v, action %+v: %v", r, a, err)
					}
					wc.HTTPFilterConfigOverride = cfgs
				}
				route.WeightedClusters[c.GetName()] = wc
				totalWeight += w
			}
			// envoy xds doc
			// default TotalWeight https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto.html#envoy-v3-api-field-config-route-v3-weightedcluster-total-weight
			wantTotalWeight := uint32(100)
			if tw := wcs.GetTotalWeight(); tw != nil {
				wantTotalWeight = tw.GetValue()
			}
			if totalWeight != wantTotalWeight {
				return nil, fmt.Errorf("route %+v, action %+v, weights of clusters do not add up to total total weight, got: %v, expected total weight from response: %v", r, a, totalWeight, wantTotalWeight)
			}
			if totalWeight == 0 {
				return nil, fmt.Errorf("route %+v, action %+v, has no valid cluster in WeightedCluster action", r, a)
			}
		case *v3routepb.RouteAction_ClusterHeader:
			continue
		}

		msd := action.GetMaxStreamDuration()
		// Prefer grpc_timeout_header_max, if set.
		dur := msd.GetGrpcTimeoutHeaderMax()
		if dur == nil {
			dur = msd.GetMaxStreamDuration()
		}
		if dur != nil {
			d := dur.AsDuration()
			route.MaxStreamDuration = &d
		}

		if !v2 {
			cfgs, err := processHTTPFilterOverrides(r.GetTypedPerFilterConfig())
			if err != nil {
				return nil, fmt.Errorf("route %+v: %v", r, err)
			}
			route.HTTPFilterConfigOverride = cfgs
		}
		routesRet = append(routesRet, &route)
	}
	return routesRet, nil
}

func hashPoliciesProtoToSlice(policies []*v3routepb.RouteAction_HashPolicy, logger *grpclog.PrefixLogger) ([]*HashPolicy, error) {
	var hashPoliciesRet []*HashPolicy
	for _, p := range policies {
		policy := HashPolicy{Terminal: p.Terminal}
		switch p.GetPolicySpecifier().(type) {
		case *v3routepb.RouteAction_HashPolicy_Header_:
			policy.HashPolicyType = HashPolicyTypeHeader
			policy.HeaderName = p.GetHeader().GetHeaderName()
			if rr := p.GetHeader().GetRegexRewrite(); rr != nil {
				regex := rr.GetPattern().GetRegex()
				re, err := regexp.Compile(regex)
				if err != nil {
					return nil, fmt.Errorf("hash policy %+v contains an invalid regex %q", p, regex)
				}
				policy.Regex = re
				policy.RegexSubstitution = rr.GetSubstitution()
			}
		case *v3routepb.RouteAction_HashPolicy_FilterState_:
			if p.GetFilterState().GetKey() != "io.grpc.channel_id" {
				logger.Infof("hash policy %+v contains an invalid key for filter state policy %q", p, p.GetFilterState().GetKey())
				continue
			}
			policy.HashPolicyType = HashPolicyTypeChannelID
		default:
			logger.Infof("hash policy %T is an unsupported hash policy", p.GetPolicySpecifier())
			continue
		}

		hashPoliciesRet = append(hashPoliciesRet, &policy)
	}
	return hashPoliciesRet, nil
}

// UnmarshalCluster processes resources received in an CDS response, validates
// them, and transforms them into a native struct which contains only fields we
// are interested in.
func UnmarshalCluster(version string, resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]ClusterUpdate, UpdateMetadata, error) {
	update := make(map[string]ClusterUpdate)
	md, err := processAllResources(version, resources, logger, update)
	return update, md, err
}

func unmarshalClusterResource(r *anypb.Any, logger *grpclog.PrefixLogger) (string, ClusterUpdate, error) {
	if !IsClusterResource(r.GetTypeUrl()) {
		return "", ClusterUpdate{}, fmt.Errorf("unexpected resource type: %q ", r.GetTypeUrl())
	}

	cluster := &v3clusterpb.Cluster{}
	if err := proto.Unmarshal(r.GetValue(), cluster); err != nil {
		return "", ClusterUpdate{}, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	logger.Infof("Resource with name: %v, type: %T, contains: %v", cluster.GetName(), cluster, pretty.ToJSON(cluster))
	cu, err := validateClusterAndConstructClusterUpdate(cluster)
	if err != nil {
		return cluster.GetName(), ClusterUpdate{}, err
	}
	cu.Raw = r

	return cluster.GetName(), cu, nil
}

func validateClusterAndConstructClusterUpdate(cluster *v3clusterpb.Cluster) (ClusterUpdate, error) {
	if cluster.GetLbPolicy() != v3clusterpb.Cluster_ROUND_ROBIN {
		return ClusterUpdate{}, fmt.Errorf("unexpected lbPolicy %v in response: %+v", cluster.GetLbPolicy(), cluster)
	}

	// Process security configuration received from the control plane iff the
	// corresponding environment variable is set.
	var sc *SecurityConfig
	if env.ClientSideSecuritySupport {
		var err error
		if sc, err = securityConfigFromCluster(cluster); err != nil {
			return ClusterUpdate{}, err
		}
	}

	ret := ClusterUpdate{
		ClusterName: cluster.GetName(),
		EnableLRS:   cluster.GetLrsServer().GetSelf() != nil,
		SecurityCfg: sc,
		MaxRequests: circuitBreakersFromCluster(cluster),
	}

	// Validate and set cluster type from the response.
	switch {
	case cluster.GetType() == v3clusterpb.Cluster_EDS:
		if cluster.GetEdsClusterConfig().GetEdsConfig().GetAds() == nil {
			return ClusterUpdate{}, fmt.Errorf("unexpected edsConfig in response: %+v", cluster)
		}
		ret.ClusterType = ClusterTypeEDS
		ret.EDSServiceName = cluster.GetEdsClusterConfig().GetServiceName()
		return ret, nil
	case cluster.GetType() == v3clusterpb.Cluster_LOGICAL_DNS:
		if !env.AggregateAndDNSSupportEnv {
			return ClusterUpdate{}, fmt.Errorf("unsupported cluster type (%v, %v) in response: %+v", cluster.GetType(), cluster.GetClusterType(), cluster)
		}
		ret.ClusterType = ClusterTypeLogicalDNS
		dnsHN, err := dnsHostNameFromCluster(cluster)
		if err != nil {
			return ClusterUpdate{}, err
		}
		ret.DNSHostName = dnsHN
		return ret, nil
	case cluster.GetClusterType() != nil && cluster.GetClusterType().Name == "envoy.clusters.aggregate":
		if !env.AggregateAndDNSSupportEnv {
			return ClusterUpdate{}, fmt.Errorf("unsupported cluster type (%v, %v) in response: %+v", cluster.GetType(), cluster.GetClusterType(), cluster)
		}
		clusters := &v3aggregateclusterpb.ClusterConfig{}
		if err := proto.Unmarshal(cluster.GetClusterType().GetTypedConfig().GetValue(), clusters); err != nil {
			return ClusterUpdate{}, fmt.Errorf("failed to unmarshal resource: %v", err)
		}
		ret.ClusterType = ClusterTypeAggregate
		ret.PrioritizedClusterNames = clusters.Clusters
		return ret, nil
	default:
		return ClusterUpdate{}, fmt.Errorf("unsupported cluster type (%v, %v) in response: %+v", cluster.GetType(), cluster.GetClusterType(), cluster)
	}
}

// dnsHostNameFromCluster extracts the DNS host name from the cluster's load
// assignment.
//
// There should be exactly one locality, with one endpoint, whose address
// contains the address and port.
func dnsHostNameFromCluster(cluster *v3clusterpb.Cluster) (string, error) {
	loadAssignment := cluster.GetLoadAssignment()
	if loadAssignment == nil {
		return "", fmt.Errorf("load_assignment not present for LOGICAL_DNS cluster")
	}
	if len(loadAssignment.GetEndpoints()) != 1 {
		return "", fmt.Errorf("load_assignment for LOGICAL_DNS cluster must have exactly one locality, got: %+v", loadAssignment)
	}
	endpoints := loadAssignment.GetEndpoints()[0].GetLbEndpoints()
	if len(endpoints) != 1 {
		return "", fmt.Errorf("locality for LOGICAL_DNS cluster must have exactly one endpoint, got: %+v", endpoints)
	}
	endpoint := endpoints[0].GetEndpoint()
	if endpoint == nil {
		return "", fmt.Errorf("endpoint for LOGICAL_DNS cluster not set")
	}
	socketAddr := endpoint.GetAddress().GetSocketAddress()
	if socketAddr == nil {
		return "", fmt.Errorf("socket address for endpoint for LOGICAL_DNS cluster not set")
	}
	if socketAddr.GetResolverName() != "" {
		return "", fmt.Errorf("socket address for endpoint for LOGICAL_DNS cluster not set has unexpected custom resolver name: %v", socketAddr.GetResolverName())
	}
	host := socketAddr.GetAddress()
	if host == "" {
		return "", fmt.Errorf("host for endpoint for LOGICAL_DNS cluster not set")
	}
	port := socketAddr.GetPortValue()
	if port == 0 {
		return "", fmt.Errorf("port for endpoint for LOGICAL_DNS cluster not set")
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

// securityConfigFromCluster extracts the relevant security configuration from
// the received Cluster resource.
func securityConfigFromCluster(cluster *v3clusterpb.Cluster) (*SecurityConfig, error) {
	// The Cluster resource contains a `transport_socket` field, which contains
	// a oneof `typed_config` field of type `protobuf.Any`. The any proto
	// contains a marshaled representation of an `UpstreamTlsContext` message.
	ts := cluster.GetTransportSocket()
	if ts == nil {
		return nil, nil
	}
	if name := ts.GetName(); name != transportSocketName {
		return nil, fmt.Errorf("transport_socket field has unexpected name: %s", name)
	}
	any := ts.GetTypedConfig()
	if any == nil || any.TypeUrl != version.V3UpstreamTLSContextURL {
		return nil, fmt.Errorf("transport_socket field has unexpected typeURL: %s", any.TypeUrl)
	}
	upstreamCtx := &v3tlspb.UpstreamTlsContext{}
	if err := proto.Unmarshal(any.GetValue(), upstreamCtx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal UpstreamTlsContext in CDS response: %v", err)
	}
	if upstreamCtx.GetCommonTlsContext() == nil {
		return nil, errors.New("UpstreamTlsContext in CDS response does not contain a CommonTlsContext")
	}

	sc, err := securityConfigFromCommonTLSContext(upstreamCtx.GetCommonTlsContext())
	if err != nil {
		return nil, err
	}
	if sc.RootInstanceName == "" {
		return nil, errors.New("security configuration on the client-side does not contain root certificate provider instance name")
	}
	return sc, nil
}

// common is expected to be not nil.
func securityConfigFromCommonTLSContext(common *v3tlspb.CommonTlsContext) (*SecurityConfig, error) {
	// The `CommonTlsContext` contains a
	// `tls_certificate_certificate_provider_instance` field of type
	// `CertificateProviderInstance`, which contains the provider instance name
	// and the certificate name to fetch identity certs.
	sc := &SecurityConfig{}
	if identity := common.GetTlsCertificateCertificateProviderInstance(); identity != nil {
		sc.IdentityInstanceName = identity.GetInstanceName()
		sc.IdentityCertName = identity.GetCertificateName()
	}

	// The `CommonTlsContext` contains a `validation_context_type` field which
	// is a oneof. We can get the values that we are interested in from two of
	// those possible values:
	//  - combined validation context:
	//    - contains a default validation context which holds the list of
	//      matchers for accepted SANs.
	//    - contains certificate provider instance configuration
	//  - certificate provider instance configuration
	//    - in this case, we do not get a list of accepted SANs.
	switch t := common.GetValidationContextType().(type) {
	case *v3tlspb.CommonTlsContext_CombinedValidationContext:
		combined := common.GetCombinedValidationContext()
		var matchers []matcher.StringMatcher
		if def := combined.GetDefaultValidationContext(); def != nil {
			for _, m := range def.GetMatchSubjectAltNames() {
				matcher, err := matcher.StringMatcherFromProto(m)
				if err != nil {
					return nil, err
				}
				matchers = append(matchers, matcher)
			}
		}
		sc.SubjectAltNameMatchers = matchers
		if pi := combined.GetValidationContextCertificateProviderInstance(); pi != nil {
			sc.RootInstanceName = pi.GetInstanceName()
			sc.RootCertName = pi.GetCertificateName()
		}
	case *v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance:
		pi := common.GetValidationContextCertificateProviderInstance()
		sc.RootInstanceName = pi.GetInstanceName()
		sc.RootCertName = pi.GetCertificateName()
	case nil:
		// It is valid for the validation context to be nil on the server side.
	default:
		return nil, fmt.Errorf("validation context contains unexpected type: %T", t)
	}
	return sc, nil
}

// circuitBreakersFromCluster extracts the circuit breakers configuration from
// the received cluster resource. Returns nil if no CircuitBreakers or no
// Thresholds in CircuitBreakers.
func circuitBreakersFromCluster(cluster *v3clusterpb.Cluster) *uint32 {
	for _, threshold := range cluster.GetCircuitBreakers().GetThresholds() {
		if threshold.GetPriority() != v3corepb.RoutingPriority_DEFAULT {
			continue
		}
		maxRequestsPb := threshold.GetMaxRequests()
		if maxRequestsPb == nil {
			return nil
		}
		maxRequests := maxRequestsPb.GetValue()
		return &maxRequests
	}
	return nil
}

// UnmarshalEndpoints processes resources received in an EDS response,
// validates them, and transforms them into a native struct which contains only
// fields we are interested in.
func UnmarshalEndpoints(version string, resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]EndpointsUpdate, UpdateMetadata, error) {
	update := make(map[string]EndpointsUpdate)
	md, err := processAllResources(version, resources, logger, update)
	return update, md, err
}

func unmarshalEndpointsResource(r *anypb.Any, logger *grpclog.PrefixLogger) (string, EndpointsUpdate, error) {
	if !IsEndpointsResource(r.GetTypeUrl()) {
		return "", EndpointsUpdate{}, fmt.Errorf("unexpected resource type: %q ", r.GetTypeUrl())
	}

	cla := &v3endpointpb.ClusterLoadAssignment{}
	if err := proto.Unmarshal(r.GetValue(), cla); err != nil {
		return "", EndpointsUpdate{}, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	logger.Infof("Resource with name: %v, type: %T, contains: %v", cla.GetClusterName(), cla, pretty.ToJSON(cla))

	u, err := parseEDSRespProto(cla)
	if err != nil {
		return cla.GetClusterName(), EndpointsUpdate{}, err
	}
	u.Raw = r
	return cla.GetClusterName(), u, nil
}

func parseAddress(socketAddress *v3corepb.SocketAddress) string {
	return net.JoinHostPort(socketAddress.GetAddress(), strconv.Itoa(int(socketAddress.GetPortValue())))
}

func parseDropPolicy(dropPolicy *v3endpointpb.ClusterLoadAssignment_Policy_DropOverload) OverloadDropConfig {
	percentage := dropPolicy.GetDropPercentage()
	var (
		numerator   = percentage.GetNumerator()
		denominator uint32
	)
	switch percentage.GetDenominator() {
	case v3typepb.FractionalPercent_HUNDRED:
		denominator = 100
	case v3typepb.FractionalPercent_TEN_THOUSAND:
		denominator = 10000
	case v3typepb.FractionalPercent_MILLION:
		denominator = 1000000
	}
	return OverloadDropConfig{
		Category:    dropPolicy.GetCategory(),
		Numerator:   numerator,
		Denominator: denominator,
	}
}

func parseEndpoints(lbEndpoints []*v3endpointpb.LbEndpoint) []Endpoint {
	endpoints := make([]Endpoint, 0, len(lbEndpoints))
	for _, lbEndpoint := range lbEndpoints {
		endpoints = append(endpoints, Endpoint{
			HealthStatus: EndpointHealthStatus(lbEndpoint.GetHealthStatus()),
			Address:      parseAddress(lbEndpoint.GetEndpoint().GetAddress().GetSocketAddress()),
			Weight:       lbEndpoint.GetLoadBalancingWeight().GetValue(),
		})
	}
	return endpoints
}

func parseEDSRespProto(m *v3endpointpb.ClusterLoadAssignment) (EndpointsUpdate, error) {
	ret := EndpointsUpdate{}
	for _, dropPolicy := range m.GetPolicy().GetDropOverloads() {
		ret.Drops = append(ret.Drops, parseDropPolicy(dropPolicy))
	}
	priorities := make(map[uint32]struct{})
	for _, locality := range m.Endpoints {
		l := locality.GetLocality()
		if l == nil {
			return EndpointsUpdate{}, fmt.Errorf("EDS response contains a locality without ID, locality: %+v", locality)
		}
		lid := internal.LocalityID{
			Region:  l.Region,
			Zone:    l.Zone,
			SubZone: l.SubZone,
		}
		priority := locality.GetPriority()
		priorities[priority] = struct{}{}
		ret.Localities = append(ret.Localities, Locality{
			ID:        lid,
			Endpoints: parseEndpoints(locality.GetLbEndpoints()),
			Weight:    locality.GetLoadBalancingWeight().GetValue(),
			Priority:  priority,
		})
	}
	for i := 0; i < len(priorities); i++ {
		if _, ok := priorities[uint32(i)]; !ok {
			return EndpointsUpdate{}, fmt.Errorf("priority %v missing (with different priorities %v received)", i, priorities)
		}
	}
	return ret, nil
}

// processAllResources unmarshals and validates the resources, populates the
// provided ret (a map), and returns metadata and error.
//
// The type of the resource is determined by the type of ret. E.g.
// map[string]ListenerUpdate means this is for LDS.
func processAllResources(version string, resources []*anypb.Any, logger *grpclog.PrefixLogger, ret interface{}) (UpdateMetadata, error) {
	timestamp := time.Now()
	md := UpdateMetadata{
		Version:   version,
		Timestamp: timestamp,
	}
	var topLevelErrors []error
	perResourceErrors := make(map[string]error)

	for _, r := range resources {
		switch ret2 := ret.(type) {
		case map[string]ListenerUpdate:
			name, update, err := unmarshalListenerResource(r, logger)
			if err == nil {
				ret2[name] = update
				continue
			}
			if name == "" {
				topLevelErrors = append(topLevelErrors, err)
				continue
			}
			perResourceErrors[name] = err
			// Add place holder in the map so we know this resource name was in
			// the response.
			ret2[name] = ListenerUpdate{}
		case map[string]RouteConfigUpdate:
			name, update, err := unmarshalRouteConfigResource(r, logger)
			if err == nil {
				ret2[name] = update
				continue
			}
			if name == "" {
				topLevelErrors = append(topLevelErrors, err)
				continue
			}
			perResourceErrors[name] = err
			// Add place holder in the map so we know this resource name was in
			// the response.
			ret2[name] = RouteConfigUpdate{}
		case map[string]ClusterUpdate:
			name, update, err := unmarshalClusterResource(r, logger)
			if err == nil {
				ret2[name] = update
				continue
			}
			if name == "" {
				topLevelErrors = append(topLevelErrors, err)
				continue
			}
			perResourceErrors[name] = err
			// Add place holder in the map so we know this resource name was in
			// the response.
			ret2[name] = ClusterUpdate{}
		case map[string]EndpointsUpdate:
			name, update, err := unmarshalEndpointsResource(r, logger)
			if err == nil {
				ret2[name] = update
				continue
			}
			if name == "" {
				topLevelErrors = append(topLevelErrors, err)
				continue
			}
			perResourceErrors[name] = err
			// Add place holder in the map so we know this resource name was in
			// the response.
			ret2[name] = EndpointsUpdate{}
		}
	}

	if len(topLevelErrors) == 0 && len(perResourceErrors) == 0 {
		md.Status = ServiceStatusACKed
		return md, nil
	}

	var typeStr string
	switch ret.(type) {
	case map[string]ListenerUpdate:
		typeStr = "LDS"
	case map[string]RouteConfigUpdate:
		typeStr = "RDS"
	case map[string]ClusterUpdate:
		typeStr = "CDS"
	case map[string]EndpointsUpdate:
		typeStr = "EDS"
	}

	md.Status = ServiceStatusNACKed
	errRet := combineErrors(typeStr, topLevelErrors, perResourceErrors)
	md.ErrState = &UpdateErrorMetadata{
		Version:   version,
		Err:       errRet,
		Timestamp: timestamp,
	}
	return md, errRet
}

func combineErrors(rType string, topLevelErrors []error, perResourceErrors map[string]error) error {
	var errStrB strings.Builder
	errStrB.WriteString(fmt.Sprintf("error parsing %q response: ", rType))
	if len(topLevelErrors) > 0 {
		errStrB.WriteString("top level errors: ")
		for i, err := range topLevelErrors {
			if i != 0 {
				errStrB.WriteString(";\n")
			}
			errStrB.WriteString(err.Error())
		}
	}
	if len(perResourceErrors) > 0 {
		var i int
		for name, err := range perResourceErrors {
			if i != 0 {
				errStrB.WriteString(";\n")
			}
			i++
			errStrB.WriteString(fmt.Sprintf("resource %q: %v", name, err.Error()))
		}
	}
	return errors.New(errStrB.String())
}
