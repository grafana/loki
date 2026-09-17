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
	"fmt"
	"net/netip"

	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
)

// NetworkFilterChainMap contains the match configuration for network filter
// chains on the server side. It is a multi-level map structure to facilitate
// efficient matching of incoming connections based on destination IP, source
// {type, IP and port}.
type NetworkFilterChainMap struct {
	// DstPrefixes is the list of destination prefix entries to match on.
	DstPrefixes []DestinationPrefixEntry
}

// DestinationPrefixEntry contains a destination prefix entry and the associated
// source type matchers.
type DestinationPrefixEntry struct {
	// Prefix is the destination IP prefix.
	Prefix netip.Prefix
	// SourceTypeArr contains the source type matchers. The supported source
	// types and their associated indices in the array are:
	//   - 0: Any: matches connection attempts from any source.
	//   - 1: SameOrLoopback: matches connection attempts from the same host.
	//   - 2: External: matches connection attempts from a different host.
	SourceTypeArr [3]SourcePrefixes
}

// SourcePrefixes contains a list of source prefix entries to match on.
type SourcePrefixes struct {
	// Entries is the list of source prefix entries.
	Entries []SourcePrefixEntry
}

// SourcePrefixEntry contains a source prefix entry and the associated source
// port matchers.
type SourcePrefixEntry struct {
	// Prefix is the source IP prefix.
	Prefix netip.Prefix
	// PortMap contains the matchers for source ports.
	PortMap map[int]NetworkFilterChainConfig
}

// NetworkFilterChainConfig contains the configuration for a network filter
// chain on the server side. The only support network filter is the HTTP
// connection manager.
type NetworkFilterChainConfig struct {
	// SecurityCfg contains transport socket security configuration.
	SecurityCfg *SecurityConfig
	// HTTPConnMgr contains the HTTP connection manager configuration.
	HTTPConnMgr *HTTPConnectionManagerConfig
}

// IsEmpty returns true if the NetworkFilterChainConfig contains no
// configuration.
func (n NetworkFilterChainConfig) IsEmpty() bool {
	return n.SecurityCfg == nil && n.HTTPConnMgr == nil
}

func processNetworkFilters(filters []*v3listenerpb.Filter) (*HTTPConnectionManagerConfig, error) {
	hcmConfig := &HTTPConnectionManagerConfig{}
	seenNames := make(map[string]bool, len(filters))
	seenHCM := false
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
			if err := tc.UnmarshalTo(hcm); err != nil {
				return nil, fmt.Errorf("network filters {%+v} failed unmarshalling of network filter {%+v}: %v", filters, filter, err)
			}
			// "Any filters after HttpConnectionManager should be ignored during
			// connection processing but still be considered for validity.
			// HTTPConnectionManager must have valid http_filters." - A36
			filters, err := processHTTPFilters(hcm.GetHttpFilters(), true)
			if err != nil {
				return nil, fmt.Errorf("network filters {%+v} had invalid server side HTTP Filters {%+v}: %v", filters, hcm.GetHttpFilters(), err)
			}
			if !seenHCM {
				// Validate for RBAC in only the HCM that will be used, since this isn't a logical validation failure,
				// it's simply a validation to support RBAC HTTP Filter.
				// "HttpConnectionManager.xff_num_trusted_hops must be unset or zero and
				// HttpConnectionManager.original_ip_detection_extensions must be empty. If
				// either field has an incorrect value, the Listener must be NACKed." - A41
				if hcm.XffNumTrustedHops != 0 {
					return nil, fmt.Errorf("xff_num_trusted_hops must be unset or zero %+v", hcm)
				}
				if len(hcm.OriginalIpDetectionExtensions) != 0 {
					return nil, fmt.Errorf("original_ip_detection_extensions must be empty %+v", hcm)
				}

				// TODO: Implement terminal filter logic, as per A36.
				hcmConfig.HTTPFilters = filters
				seenHCM = true
				switch hcm.RouteSpecifier.(type) {
				case *v3httppb.HttpConnectionManager_Rds:
					if hcm.GetRds().GetConfigSource().GetAds() == nil {
						return nil, fmt.Errorf("ConfigSource is not ADS: %+v", hcm)
					}
					name := hcm.GetRds().GetRouteConfigName()
					if name == "" {
						return nil, fmt.Errorf("empty route_config_name: %+v", hcm)
					}
					hcmConfig.RouteConfigName = name
				case *v3httppb.HttpConnectionManager_RouteConfig:
					// "RouteConfiguration validation logic inherits all
					// previous validations made for client-side usage as RDS
					// does not distinguish between client-side and
					// server-side." - A36
					// Can specify v3 here, as will never get to this function
					// if v2.
					routeU, err := generateRDSUpdateFromRouteConfiguration(hcm.GetRouteConfig(), nil)
					if err != nil {
						return nil, fmt.Errorf("failed to parse inline RDS resp: %v", err)
					}
					hcmConfig.InlineRouteConfig = &routeU
				case nil:
					return nil, fmt.Errorf("no RouteSpecifier: %+v", hcm)
				default:
					return nil, fmt.Errorf("unsupported type %T for RouteSpecifier", hcm.RouteSpecifier)
				}
			}
		default:
			return nil, fmt.Errorf("network filters {%+v} has unsupported config_type %T in filter %s", filters, typ, filter.GetName())
		}
	}
	if !seenHCM {
		return nil, fmt.Errorf("network filters {%+v} missing HttpConnectionManager filter", filters)
	}
	return hcmConfig, nil
}
