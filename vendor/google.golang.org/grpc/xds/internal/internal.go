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
 */

// Package internal contains functions/structs shared by xds
// balancers/resolvers.
package internal

import (
	"fmt"

	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/clients"
)

// LocalityString generates a string representation of clients.Locality in the
// format specified in gRFC A76.
func LocalityString(l clients.Locality) string {
	return fmt.Sprintf("{region=%q, zone=%q, sub_zone=%q}", l.Region, l.Zone, l.SubZone)
}

// IsLocalityEqual allows the values to be compared by Attributes.Equal.
func IsLocalityEqual(l clients.Locality, o any) bool {
	ol, ok := o.(clients.Locality)
	if !ok {
		return false
	}
	return l.Region == ol.Region && l.Zone == ol.Zone && l.SubZone == ol.SubZone
}

// LocalityFromString converts a string representation of clients.locality as
// specified in gRFC A76, into a LocalityID struct.
func LocalityFromString(s string) (ret clients.Locality, _ error) {
	_, err := fmt.Sscanf(s, "{region=%q, zone=%q, sub_zone=%q}", &ret.Region, &ret.Zone, &ret.SubZone)
	if err != nil {
		return clients.Locality{}, fmt.Errorf("%s is not a well formatted locality ID, error: %v", s, err)
	}
	return ret, nil
}

type localityKeyType string

const localityKey = localityKeyType("grpc.xds.internal.address.locality")

// GetLocalityID returns the locality ID of addr.
func GetLocalityID(addr resolver.Address) clients.Locality {
	path, _ := addr.BalancerAttributes.Value(localityKey).(clients.Locality)
	return path
}

// SetLocalityID sets locality ID in addr to l.
func SetLocalityID(addr resolver.Address, l clients.Locality) resolver.Address {
	addr.BalancerAttributes = addr.BalancerAttributes.WithValue(localityKey, l)
	return addr
}

// SetLocalityIDInEndpoint sets locality ID in endpoint to l.
func SetLocalityIDInEndpoint(endpoint resolver.Endpoint, l clients.Locality) resolver.Endpoint {
	endpoint.Attributes = endpoint.Attributes.WithValue(localityKey, l)
	return endpoint
}

// ResourceTypeMapForTesting maps TypeUrl to corresponding ResourceType.
var ResourceTypeMapForTesting map[string]any

// UnknownCSMLabels are TelemetryLabels emitted from CDS if CSM Telemetry Label
// data is not present in the CDS Resource.
var UnknownCSMLabels = map[string]string{
	"csm.service_name":           "unknown",
	"csm.service_namespace_name": "unknown",
}
