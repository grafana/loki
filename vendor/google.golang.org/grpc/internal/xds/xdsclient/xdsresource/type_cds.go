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
	"encoding/json"

	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/protobuf/types/known/anypb"
)

// ClusterType is the type of cluster from a received CDS response.
type ClusterType int

const (
	// ClusterTypeEDS represents the EDS cluster type, which will delegate endpoint
	// discovery to the management server.
	ClusterTypeEDS ClusterType = iota
	// ClusterTypeLogicalDNS represents the Logical DNS cluster type, which essentially
	// maps to the gRPC behavior of using the DNS resolver with pick_first LB policy.
	ClusterTypeLogicalDNS
	// ClusterTypeAggregate represents the Aggregate Cluster type, which provides a
	// prioritized list of clusters to use. It is used for failover between clusters
	// with a different configuration.
	ClusterTypeAggregate
)

// ClusterUpdate contains information from a received CDS response, which is of
// interest to the registered CDS watcher.
type ClusterUpdate struct {
	ClusterType ClusterType
	// ClusterName is the clusterName being watched for through CDS.
	ClusterName string
	// EDSServiceName is an optional name for EDS. If it's not set, the balancer
	// should watch ClusterName for the EDS resources.
	EDSServiceName string
	// LRSServerConfig contains configuration about the xDS server that sent
	// this cluster resource. This is also the server where load reports are to
	// be sent, for this cluster.
	LRSServerConfig *bootstrap.ServerConfig
	// SecurityCfg contains security configuration sent by the control plane.
	SecurityCfg *SecurityConfig
	// MaxRequests for circuit breaking, if any (otherwise nil).
	MaxRequests *uint32
	// DNSHostName is used only for cluster type DNS. It's the DNS name to
	// resolve in "host:port" form
	DNSHostName string
	// PrioritizedClusterNames is used only for cluster type aggregate. It represents
	// a prioritized list of cluster names.
	PrioritizedClusterNames []string

	// LBPolicy represents the locality and endpoint picking policy in JSON,
	// which will be the child policy of xds_cluster_impl.
	LBPolicy json.RawMessage

	// OutlierDetection is the outlier detection configuration for this cluster.
	// If nil, it means this cluster does not use the outlier detection feature.
	OutlierDetection json.RawMessage

	// Raw is the resource from the xds response.
	Raw *anypb.Any
	// TelemetryLabels are the string valued metadata of filter_metadata type
	// "com.google.csm.telemetry_labels" with keys "service_name" or
	// "service_namespace".
	TelemetryLabels map[string]string

	// LRSReportEndpointMetrics specifies the subset of ORCA metrics that
	// should be propagated to the LRS server.
	LRSReportEndpointMetrics *LRSReportEndpointMetricsConfig

	// Metadata contains the metadata from the cluster resource.
	Metadata map[string]any
}

// LRSReportEndpointMetricsConfig holds the configuration for propagating ORCA
// metrics to the LRS server.
type LRSReportEndpointMetricsConfig struct {
	CPUUtilization         bool
	MemUtilization         bool
	ApplicationUtilization bool
	// NamedMetricsAll specifies whether all named metrics should be propagated.
	// If true, NamedMetrics is ignored.
	NamedMetricsAll bool
	// NamedMetrics specifies the set of named metrics to propagate when
	// NamedMetricsAll is false.
	NamedMetrics map[string]struct{}
}

// Equal returns whether the two LRSReportEndpointMetricsConfig configurations
// are identical.
func (bmp *LRSReportEndpointMetricsConfig) Equal(other *LRSReportEndpointMetricsConfig) bool {
	switch {
	case bmp == nil && other == nil:
		return true
	case (bmp != nil) != (other != nil):
		return false
	}
	if bmp.CPUUtilization != other.CPUUtilization ||
		bmp.MemUtilization != other.MemUtilization ||
		bmp.ApplicationUtilization != other.ApplicationUtilization ||
		bmp.NamedMetricsAll != other.NamedMetricsAll {
		return false
	}
	if len(bmp.NamedMetrics) != len(other.NamedMetrics) {
		return false
	}
	for k := range bmp.NamedMetrics {
		if _, ok := other.NamedMetrics[k]; !ok {
			return false
		}
	}
	return true
}

// SecurityConfig contains the security configuration received as part of the
// Cluster resource on the client-side, and as part of the Listener resource on
// the server-side.
type SecurityConfig struct {
	// RootInstanceName identifies the certProvider plugin to be used to fetch
	// root certificates. This instance name will be resolved to the plugin name
	// and its associated configuration from the certificate_providers field of
	// the bootstrap file.
	RootInstanceName string
	// RootCertName is the certificate name to be passed to the plugin (looked
	// up from the bootstrap file) while fetching root certificates.
	RootCertName string
	// IdentityInstanceName identifies the certProvider plugin to be used to
	// fetch identity certificates. This instance name will be resolved to the
	// plugin name and its associated configuration from the
	// certificate_providers field of the bootstrap file.
	IdentityInstanceName string
	// IdentityCertName is the certificate name to be passed to the plugin
	// (looked up from the bootstrap file) while fetching identity certificates.
	IdentityCertName string
	// SubjectAltNameMatchers is an optional list of match criteria for SANs
	// specified on the peer certificate. Used only on the client-side.
	//
	// Some intricacies:
	// - If this field is empty, then any peer certificate is accepted.
	// - If the peer certificate contains a wildcard DNS SAN, and an `exact`
	//   matcher is configured, a wildcard DNS match is performed instead of a
	//   regular string comparison.
	SubjectAltNameMatchers []matcher.StringMatcher
	// RequireClientCert indicates if the server handshake process expects the
	// client to present a certificate. Set to true when performing mTLS. Used
	// only on the server-side.
	RequireClientCert bool
	// UseSystemRootCerts indicates that the client should use system root
	// certificates to validate the server certificate. This field is mutually
	// exclusive with RootCertName and RootInstanceName. Validation performed
	// after unmarshalling xDS resources ensures that this field is set only
	// when both RootCertName and RootInstanceName are empty.
	UseSystemRootCerts bool
	// SNI is the string to be used as the Server Name when creating TLS
	// configurations for the handshake. An empty string for SNI value will be
	// treated as SNI not specified.
	SNI string
	// UseAutoHostSNI indicates whether to set the ServerName for the TLS handshake
	// configuration to the hostname (if available). The host is the DNS
	// hostname for DNS clusters, or Endpoint.hostname for EDS clusters. The
	// port will not be included in the SNI value.
	UseAutoHostSNI bool
	// AutoSNISANValidation indicates whether to replace any Subject Alternative
	// Name (SAN) matchers with a validation for a DNS SAN matching the SNI
	// value sent. This validation uses the SNI being set in the TLS
	// configuration, regardless of how the SNI is determined.
	AutoSNISANValidation bool
}

// Equal returns true if sc is equal to other.
func (sc *SecurityConfig) Equal(other *SecurityConfig) bool {
	switch {
	case sc == nil && other == nil:
		return true
	case (sc != nil) != (other != nil):
		return false
	}
	switch {
	case sc.RootInstanceName != other.RootInstanceName:
		return false
	case sc.RootCertName != other.RootCertName:
		return false
	case sc.IdentityInstanceName != other.IdentityInstanceName:
		return false
	case sc.IdentityCertName != other.IdentityCertName:
		return false
	case sc.RequireClientCert != other.RequireClientCert:
		return false
	case sc.UseSystemRootCerts != other.UseSystemRootCerts:
		return false
	case sc.SNI != other.SNI:
		return false
	case sc.UseAutoHostSNI != other.UseAutoHostSNI:
		return false
	case sc.AutoSNISANValidation != other.AutoSNISANValidation:
		return false
	default:
		if len(sc.SubjectAltNameMatchers) != len(other.SubjectAltNameMatchers) {
			return false
		}
		for i := 0; i < len(sc.SubjectAltNameMatchers); i++ {
			if !sc.SubjectAltNameMatchers[i].Equal(other.SubjectAltNameMatchers[i]) {
				return false
			}
		}
	}
	return true
}
