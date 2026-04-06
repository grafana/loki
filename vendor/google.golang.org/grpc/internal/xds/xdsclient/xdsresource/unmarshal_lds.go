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
 */

package xdsresource

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strconv"

	v1xdsudpatypepb "github.com/cncf/xds/go/udpa/type/v1"
	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func unmarshalListenerResource(r *anypb.Any, opts *xdsclient.DecodeOptions) (string, ListenerUpdate, error) {
	r, err := UnwrapResource(r)
	if err != nil {
		return "", ListenerUpdate{}, fmt.Errorf("failed to unwrap resource: %v", err)
	}

	if !IsListenerResource(r.GetTypeUrl()) {
		return "", ListenerUpdate{}, fmt.Errorf("unexpected listener resource type: %q ", r.GetTypeUrl())
	}
	lis := &v3listenerpb.Listener{}
	if err := proto.Unmarshal(r.GetValue(), lis); err != nil {
		return "", ListenerUpdate{}, fmt.Errorf("failed to unmarshal resource: %v", err)
	}

	lu, err := processListener(lis, opts)
	if err != nil {
		return lis.GetName(), ListenerUpdate{}, err
	}
	lu.Raw = r
	return lis.GetName(), *lu, nil
}

func processListener(lis *v3listenerpb.Listener, opts *xdsclient.DecodeOptions) (*ListenerUpdate, error) {
	if lis.GetApiListener() != nil {
		return processClientSideListener(lis, opts)
	}
	return processServerSideListener(lis)
}

// processClientSideListener checks if the provided Listener proto meets
// the expected criteria. If so, it returns a non-empty routeConfigName.
func processClientSideListener(lis *v3listenerpb.Listener, opts *xdsclient.DecodeOptions) (*ListenerUpdate, error) {
	apiLisAny := lis.GetApiListener().GetApiListener()
	if !IsHTTPConnManagerResource(apiLisAny.GetTypeUrl()) {
		return nil, fmt.Errorf("unexpected http connection manager resource type: %q", apiLisAny.GetTypeUrl())
	}
	apiLis := &v3httppb.HttpConnectionManager{}
	if err := proto.Unmarshal(apiLisAny.GetValue(), apiLis); err != nil {
		return nil, fmt.Errorf("failed to unmarshal api_listener: %v", err)
	}
	// "HttpConnectionManager.xff_num_trusted_hops must be unset or zero and
	// HttpConnectionManager.original_ip_detection_extensions must be empty. If
	// either field has an incorrect value, the Listener must be NACKed." - A41
	if apiLis.XffNumTrustedHops != 0 {
		return nil, fmt.Errorf("xff_num_trusted_hops must be unset or zero %+v", apiLis)
	}
	if len(apiLis.OriginalIpDetectionExtensions) != 0 {
		return nil, fmt.Errorf("original_ip_detection_extensions must be empty %+v", apiLis)
	}

	hcm := &HTTPConnectionManagerConfig{}
	switch apiLis.RouteSpecifier.(type) {
	case *v3httppb.HttpConnectionManager_Rds:
		if configsource := apiLis.GetRds().GetConfigSource(); configsource.GetAds() == nil && configsource.GetSelf() == nil {
			return nil, fmt.Errorf("LDS's RDS configSource is not ADS or Self: %+v", lis)
		}
		name := apiLis.GetRds().GetRouteConfigName()
		if name == "" {
			return nil, fmt.Errorf("empty route_config_name: %+v", lis)
		}
		hcm.RouteConfigName = name
	case *v3httppb.HttpConnectionManager_RouteConfig:
		routeU, err := generateRDSUpdateFromRouteConfiguration(apiLis.GetRouteConfig(), opts)
		if err != nil {
			return nil, fmt.Errorf("failed to parse inline RDS resp: %v", err)
		}
		hcm.InlineRouteConfig = &routeU
	case nil:
		return nil, fmt.Errorf("no RouteSpecifier: %+v", apiLis)
	default:
		return nil, fmt.Errorf("unsupported type %T for RouteSpecifier", apiLis.RouteSpecifier)
	}

	// The following checks and fields only apply to xDS protocol versions v3+.

	hcm.MaxStreamDuration = apiLis.GetCommonHttpProtocolOptions().GetMaxStreamDuration().AsDuration()

	var err error
	if hcm.HTTPFilters, err = processHTTPFilters(apiLis.GetHttpFilters(), false); err != nil {
		return nil, err
	}

	return &ListenerUpdate{APIListener: hcm}, nil
}

func unwrapHTTPFilterConfig(config *anypb.Any) (proto.Message, string, error) {
	switch {
	case config.MessageIs(&v3xdsxdstypepb.TypedStruct{}):
		// The real type name is inside the new TypedStruct message.
		s := new(v3xdsxdstypepb.TypedStruct)
		if err := config.UnmarshalTo(s); err != nil {
			return nil, "", fmt.Errorf("error unmarshalling TypedStruct filter config: %v", err)
		}
		return s, s.GetTypeUrl(), nil
	case config.MessageIs(&v1xdsudpatypepb.TypedStruct{}):
		// The real type name is inside the old TypedStruct message.
		s := new(v1xdsudpatypepb.TypedStruct)
		if err := config.UnmarshalTo(s); err != nil {
			return nil, "", fmt.Errorf("error unmarshalling TypedStruct filter config: %v", err)
		}
		return s, s.GetTypeUrl(), nil
	default:
		return config, config.GetTypeUrl(), nil
	}
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
		if cfg.MessageIs(s) {
			if err := cfg.UnmarshalTo(s); err != nil {
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
	// "Validation will fail if a terminal filter is not the last filter in the
	// chain or if a non-terminal filter is the last filter in the chain." - A39
	if len(ret) == 0 {
		return nil, fmt.Errorf("http filters list is empty")
	}
	var i int
	for ; i < len(ret)-1; i++ {
		if ret[i].Filter.IsTerminal() {
			return nil, fmt.Errorf("http filter %q is a terminal filter but it is not last in the filter chain", ret[i].Name)
		}
	}
	if !ret[i].Filter.IsTerminal() {
		return nil, fmt.Errorf("http filter %q is not a terminal filter", ret[len(ret)-1].Name)
	}
	return ret, nil
}

func processServerSideListener(lis *v3listenerpb.Listener) (*ListenerUpdate, error) {
	if n := len(lis.ListenerFilters); n != 0 {
		return nil, fmt.Errorf("unsupported field 'listener_filters' contains %d entries", n)
	}
	if lis.GetUseOriginalDst().GetValue() {
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
		TCPListener: &InboundListenerConfig{
			Address: sockAddr.GetAddress(),
			Port:    strconv.Itoa(int(sockAddr.GetPortValue())),
		},
	}

	// Populate the default filter chain.
	if dfc := lis.GetDefaultFilterChain(); dfc != nil {
		fc, err := filterChainFromProto(dfc)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal default filter chain: %v", err)
		}
		lu.TCPListener.DefaultFilterChain = fc
	}

	// Populated the filter chain map.
	fcm, err := buildFilterChainMap(lis.GetFilterChains())
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal filter chains: %v", err)
	}
	lu.TCPListener.FilterChains = fcm

	// If there are no supported filter chains and no default filter chain, we
	// fail here. This will cause the Listener resource to be NACK'ed.
	if len(lu.TCPListener.FilterChains.DstPrefixes) == 0 && lu.TCPListener.DefaultFilterChain.IsEmpty() {
		return nil, fmt.Errorf("no supported filter chains and no default filter chain")
	}

	return lu, nil
}

func filterChainFromProto(fc *v3listenerpb.FilterChain) (NetworkFilterChainConfig, error) {
	var emptyFilterChain NetworkFilterChainConfig

	hcmConfig, err := processNetworkFilters(fc.GetFilters())
	if err != nil {
		return emptyFilterChain, err
	}

	fcc := NetworkFilterChainConfig{HTTPConnMgr: hcmConfig}
	// If the transport_socket field is not specified, it means that the control
	// plane has not sent us any security config. This is fine and the server
	// will use the fallback credentials configured as part of the
	// xdsCredentials.
	ts := fc.GetTransportSocket()
	if ts == nil {
		return fcc, nil
	}
	if name := ts.GetName(); name != transportSocketName {
		return emptyFilterChain, fmt.Errorf("transport_socket field has unexpected name: %s", name)
	}
	tc := ts.GetTypedConfig()
	if typeURL := tc.GetTypeUrl(); typeURL != version.V3DownstreamTLSContextURL {
		return emptyFilterChain, fmt.Errorf("transport_socket missing typed_config or wrong type_url: %q", typeURL)
	}
	downstreamCtx := &v3tlspb.DownstreamTlsContext{}
	if err := proto.Unmarshal(tc.GetValue(), downstreamCtx); err != nil {
		return emptyFilterChain, fmt.Errorf("failed to unmarshal DownstreamTlsContext in LDS response: %v", err)
	}
	if downstreamCtx.GetRequireSni().GetValue() {
		return emptyFilterChain, fmt.Errorf("require_sni field set to true in DownstreamTlsContext message: %v", downstreamCtx)
	}
	if downstreamCtx.GetOcspStaplePolicy() != v3tlspb.DownstreamTlsContext_LENIENT_STAPLING {
		return emptyFilterChain, fmt.Errorf("ocsp_staple_policy field set to unsupported value in DownstreamTlsContext message: %v", downstreamCtx)
	}
	if downstreamCtx.GetCommonTlsContext() == nil {
		return emptyFilterChain, errors.New("DownstreamTlsContext in LDS response does not contain a CommonTlsContext")
	}
	sc, err := securityConfigFromCommonTLSContext(downstreamCtx.GetCommonTlsContext(), true)
	if err != nil {
		return emptyFilterChain, err
	}
	if sc != nil {
		sc.RequireClientCert = downstreamCtx.GetRequireClientCertificate().GetValue()
		if sc.RequireClientCert && sc.RootInstanceName == "" {
			return emptyFilterChain, errors.New("security configuration on the server-side does not contain root certificate provider instance name, but require_client_cert field is set")
		}
		fcc.SecurityCfg = sc
	}
	return fcc, nil
}

// dstPrefixEntry wraps DestinationPrefixEntry to track build state.
type dstPrefixEntry struct {
	entry         DestinationPrefixEntry
	rawBufferSeen bool
}

func buildFilterChainMap(fcs []*v3listenerpb.FilterChain) (NetworkFilterChainMap, error) {
	dstPrefixEntries := []*dstPrefixEntry{}
	for _, fc := range fcs {
		fcMatch := fc.GetFilterChainMatch()
		if fcMatch.GetDestinationPort().GetValue() != 0 {
			// Destination port is the first match criteria and we do not
			// support filter chains that contain this match criteria.
			logger.Warningf("Dropping filter chain %q since it contains unsupported destination_port match field", fc.GetName())
			continue
		}

		var err error
		dstPrefixEntries, err = addFilterChainsForDestPrefixes(dstPrefixEntries, fc)
		if err != nil {
			return NetworkFilterChainMap{}, err
		}
	}

	entries := []DestinationPrefixEntry{}
	for _, bEntry := range dstPrefixEntries {
		fcSeen := false
		for _, srcPrefixes := range bEntry.entry.SourceTypeArr {
			if len(srcPrefixes.Entries) == 0 {
				continue
			}
			for _, srcPrefix := range srcPrefixes.Entries {
				for _, fc := range srcPrefix.PortMap {
					if !fc.IsEmpty() {
						fcSeen = true
					}
				}
			}
		}
		if fcSeen {
			entries = append(entries, bEntry.entry)
		}
	}
	return NetworkFilterChainMap{DstPrefixes: entries}, nil
}

func addFilterChainsForDestPrefixes(dstPrefixEntries []*dstPrefixEntry, fc *v3listenerpb.FilterChain) ([]*dstPrefixEntry, error) {
	ranges := fc.GetFilterChainMatch().GetPrefixRanges()
	dstPrefixes := make([]*net.IPNet, 0, len(ranges))
	for _, pr := range ranges {
		cidr := fmt.Sprintf("%s/%d", pr.GetAddressPrefix(), pr.GetPrefixLen().GetValue())
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse destination prefix range: %+v", pr)
		}
		dstPrefixes = append(dstPrefixes, ipnet)
	}

	var entry *dstPrefixEntry
	if len(dstPrefixes) == 0 {
		// Use the unspecified entry when destination prefix is unspecified, and
		// set the `net` field to nil.
		dstPrefixEntries, entry = getOrCreateDestPrefixEntry(dstPrefixEntries, nil)
		if err := addFilterChainsForServerNames(entry, fc); err != nil {
			return nil, err
		}
		return dstPrefixEntries, nil
	}
	for _, prefix := range dstPrefixes {
		dstPrefixEntries, entry = getOrCreateDestPrefixEntry(dstPrefixEntries, prefix)
		if err := addFilterChainsForServerNames(entry, fc); err != nil {
			return nil, err
		}
	}
	return dstPrefixEntries, nil
}

// getOrCreateDestPrefixEntry looks for an existing dstPrefixEntry in the
// provided slice with the same destination prefix as the provided prefix. If
// such an entry is found, it is returned. Otherwise, a new entry is created and
// appended to the slice, and the new entry is returned.
func getOrCreateDestPrefixEntry(dstPrefixEntries []*dstPrefixEntry, prefix *net.IPNet) ([]*dstPrefixEntry, *dstPrefixEntry) {
	for _, e := range dstPrefixEntries {
		if ipNetEqual(e.entry.Prefix, prefix) {
			return dstPrefixEntries, e
		}
	}
	entry := &dstPrefixEntry{entry: DestinationPrefixEntry{Prefix: prefix}}
	dstPrefixEntries = append(dstPrefixEntries, entry)
	return dstPrefixEntries, entry
}

func addFilterChainsForServerNames(dstEntry *dstPrefixEntry, fc *v3listenerpb.FilterChain) error {
	// Filter chains specifying server names in their match criteria always fail
	// a match at connection time. So, these filter chains can be dropped now.
	if len(fc.GetFilterChainMatch().GetServerNames()) != 0 {
		logger.Warningf("Dropping filter chain %q since it contains unsupported server_names match field", fc.GetName())
		return nil
	}

	return addFilterChainsForTransportProtocols(dstEntry, fc)
}

func addFilterChainsForTransportProtocols(dstEntry *dstPrefixEntry, fc *v3listenerpb.FilterChain) error {
	tp := fc.GetFilterChainMatch().GetTransportProtocol()
	switch {
	case tp != "" && tp != "raw_buffer":
		// Only allow filter chains with transport protocol set to empty string
		// or "raw_buffer".
		logger.Warningf("Dropping filter chain %q since it contains unsupported value for transport_protocol match field", fc.GetName())
		return nil
	case tp == "" && dstEntry.rawBufferSeen:
		// If we have already seen filter chains with transport protocol set to
		// "raw_buffer", we can drop filter chains with transport protocol set
		// to empty string, since the former takes precedence.
		logger.Warningf("Dropping filter chain %q since it contains empty string for transport_protocol match field, but one with raw_buffer already exists", fc.GetName())
		return nil
	case tp != "" && !dstEntry.rawBufferSeen:
		// This is the first "raw_buffer" that we are seeing. Set the bit and
		// reset the source types array which might contain entries for filter
		// chains with transport protocol set to empty string.
		dstEntry.rawBufferSeen = true
		dstEntry.entry.SourceTypeArr = [3]SourcePrefixes{}
	}
	return addFilterChainsForApplicationProtocols(dstEntry, fc)
}

func addFilterChainsForApplicationProtocols(dstEntry *dstPrefixEntry, fc *v3listenerpb.FilterChain) error {
	if len(fc.GetFilterChainMatch().GetApplicationProtocols()) != 0 {
		logger.Warningf("Dropping filter chain %q since it contains unsupported application_protocols match field", fc.GetName())
		return nil
	}
	return addFilterChainsForSourceType(&dstEntry.entry, fc)
}

// sourceType specifies the connection source IP match type.
type sourceType int

const (
	// sourceTypeAny matches connection attempts from any source.
	sourceTypeAny sourceType = iota
	// sourceTypeSameOrLoopback matches connection attempts from the same host.
	sourceTypeSameOrLoopback
	// sourceTypeExternal matches connection attempts from a different host.
	sourceTypeExternal
)

func addFilterChainsForSourceType(entry *DestinationPrefixEntry, fc *v3listenerpb.FilterChain) error {
	var srcType sourceType
	switch st := fc.GetFilterChainMatch().GetSourceType(); st {
	case v3listenerpb.FilterChainMatch_ANY:
		srcType = sourceTypeAny
	case v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK:
		srcType = sourceTypeSameOrLoopback
	case v3listenerpb.FilterChainMatch_EXTERNAL:
		srcType = sourceTypeExternal
	default:
		return fmt.Errorf("unsupported source type: %v", st)
	}

	return addFilterChainsForSourcePrefixes(&entry.SourceTypeArr[srcType], fc)
}

func addFilterChainsForSourcePrefixes(srcPrefixes *SourcePrefixes, fc *v3listenerpb.FilterChain) error {
	ranges := fc.GetFilterChainMatch().GetSourcePrefixRanges()
	prefixes := make([]*net.IPNet, 0, len(ranges))
	for _, pr := range ranges {
		cidr := fmt.Sprintf("%s/%d", pr.GetAddressPrefix(), pr.GetPrefixLen().GetValue())
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("failed to parse source prefix range: %+v", pr)
		}
		prefixes = append(prefixes, ipnet)
	}

	if len(prefixes) == 0 {
		return getOrCreateSourcePrefixEntry(srcPrefixes, nil, fc)
	}
	for _, prefix := range prefixes {
		if err := getOrCreateSourcePrefixEntry(srcPrefixes, prefix, fc); err != nil {
			return err
		}
	}
	return nil
}

// getOrCreateSourcePrefixEntry looks for an existing SourcePrefixEntry in the
// provided SourcePrefixes with the same source prefix as the provided prefix. If
// such an entry is found, the provided filter chain is added to the entry and
// nil is returned. Otherwise, a new entry is created and appended to the
// SourcePrefixes, the provided filter chain is added to the new entry, and nil
// is returned. If there are multiple filter chains with overlapping matching
// rules, an error is returned.
func getOrCreateSourcePrefixEntry(srcPrefixes *SourcePrefixes, prefix *net.IPNet, fc *v3listenerpb.FilterChain) error {
	for i := range srcPrefixes.Entries {
		if ipNetEqual(srcPrefixes.Entries[i].Prefix, prefix) {
			return addFilterChainsForSourcePorts(&srcPrefixes.Entries[i], fc)
		}
	}

	// Not found, create a new entry.
	srcPrefixes.Entries = append(srcPrefixes.Entries, SourcePrefixEntry{
		Prefix:  prefix,
		PortMap: make(map[int]NetworkFilterChainConfig),
	})
	return addFilterChainsForSourcePorts(&srcPrefixes.Entries[len(srcPrefixes.Entries)-1], fc)
}

func addFilterChainsForSourcePorts(entry *SourcePrefixEntry, fc *v3listenerpb.FilterChain) error {
	ports := fc.GetFilterChainMatch().GetSourcePorts()
	srcPorts := make([]int, 0, len(ports))
	for _, port := range ports {
		srcPorts = append(srcPorts, int(port))
	}

	if len(srcPorts) == 0 {
		if !entry.PortMap[0].IsEmpty() {
			return errors.New("multiple filter chains with overlapping matching rules are defined")
		}
		fcc, err := filterChainFromProto(fc)
		if err != nil {
			return err
		}
		entry.PortMap[0] = fcc
		return nil
	}
	for _, port := range srcPorts {
		if !entry.PortMap[port].IsEmpty() {
			return errors.New("multiple filter chains with overlapping matching rules are defined")
		}
		fcc, err := filterChainFromProto(fc)
		if err != nil {
			return err
		}
		entry.PortMap[port] = fcc
	}
	return nil
}

func ipNetEqual(a, b *net.IPNet) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.IP.Equal(b.IP) && bytes.Equal(a.Mask, b.Mask)
}
