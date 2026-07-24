// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package directaccess

import (
	"context"
	"fmt"
	"net"
	"strconv"

	btopt "cloud.google.com/go/bigtable/internal/option"
	clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/google"
)

// Note: we do not call the bigtable.googleapis.com (LDS) directly as we don't have RLS proto to send the RLS request and determine the RLS region
// For simplificity, we just call the client region CDS address and get the endpoints.

// xdsTarget is TD address
const (
	xdsTarget     = "directpath-pa.googleapis.com:443"
	xdsCdsTypeURL = "type.googleapis.com/envoy.config.cluster.v3.Cluster"
	xdsEdsTypeURL = "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment"
)

// FetchXdsEndpoints connects to Traffic Director, performs a CDS check for the given cluster URI,
// extracts the EDS service name, and fetches the endpoints (IPs) using a single multiplexed ADS stream.
func FetchXdsEndpoints(ctx context.Context, nodeID, zone, cdsResourceURI string) ([]string, string, error) {
	btopt.Debugf(nil, "directaccess: Dialing Traffic Director at %s", xdsTarget)

	// 1. Dial Traffic Director
	conn, err := grpc.NewClient(xdsTarget, grpc.WithCredentialsBundle(google.NewDefaultCredentials()))
	if err != nil {
		return nil, "xds_reachability_failed", fmt.Errorf("failed to create xDS client: %w", err)
	}
	defer conn.Close()

	// 2. Establish ADS Stream
	client := discoverypb.NewAggregatedDiscoveryServiceClient(conn)
	stream, err := client.StreamAggregatedResources(ctx)
	if err != nil {
		return nil, "xds_reachability_failed", fmt.Errorf("failed to create ADS stream: %w", err)
	}
	defer stream.CloseSend() // Clean up stream when done

	node := &corepb.Node{
		Id:       nodeID,
		Locality: &corepb.Locality{Zone: zone},
	}

	// 3. Step 1: Perform CDS request to verify routing and get EDS resource name
	edsResourceName, err := fetchEDSResourceName(stream, node, cdsResourceURI)
	if err != nil {
		return nil, "xds_reachability_failed", err
	}

	// 4. Step 2 & 3: Perform EDS request and parse out the backend IPs
	ips, err := fetchEndpoints(stream, node, edsResourceName)
	if err != nil {
		return nil, "xds_eds_failed", err
	}

	return ips, "", nil
}

// fetchEDSResourceName sends a CDS request on the provided stream and extracts the target EDS Resource Name.
func fetchEDSResourceName(stream discoverypb.AggregatedDiscoveryService_StreamAggregatedResourcesClient, node *corepb.Node, cdsResourceURI string) (string, error) {
	btopt.Debugf(nil, "directaccess: Sending CDS request for URI: %s", cdsResourceURI)

	req := &discoverypb.DiscoveryRequest{
		Node:          node,
		TypeUrl:       xdsCdsTypeURL,
		ResourceNames: []string{cdsResourceURI},
	}

	if err := stream.Send(req); err != nil {
		return "", fmt.Errorf("failed to send CDS request: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return "", fmt.Errorf("failed to receive CDS response: %w", err)
	}

	// Extract the EDS Resource Name from the CDS Response
	for _, res := range resp.GetResources() {
		if res.TypeUrl != xdsCdsTypeURL {
			continue
		}

		cluster := &clusterpb.Cluster{}
		if err := res.UnmarshalTo(cluster); err == nil {
			// Envoy uses the EdsClusterConfig ServiceName if present; otherwise, it defaults to the CDS name
			if edsConfig := cluster.GetEdsClusterConfig(); edsConfig != nil && edsConfig.GetServiceName() != "" {
				return edsConfig.GetServiceName(), nil
			}
			return cluster.GetName(), nil
		}
	}

	// Fallback to the original CDS URI if we couldn't parse the cluster properly but received a response
	return cdsResourceURI, nil
}

// fetchEndpoints sends an EDS request on the provided stream and returns the parsed IP addresses.
func fetchEndpoints(stream discoverypb.AggregatedDiscoveryService_StreamAggregatedResourcesClient, node *corepb.Node, edsResourceName string) ([]string, error) {
	btopt.Debugf(nil, "directaccess: Sending EDS request for cluster name: %s", edsResourceName)

	req := &discoverypb.DiscoveryRequest{
		Node:          node,
		TypeUrl:       xdsEdsTypeURL,
		ResourceNames: []string{edsResourceName},
	}

	if err := stream.Send(req); err != nil {
		return nil, fmt.Errorf("failed to send EDS request: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("failed to receive EDS response: %w", err)
	}

	var discoveredIPs []string

	for _, res := range resp.GetResources() {
		if res.TypeUrl != xdsEdsTypeURL {
			continue
		}

		cla := &endpointpb.ClusterLoadAssignment{}
		if err := res.UnmarshalTo(cla); err != nil {
			continue
		}

		for _, locality := range cla.Endpoints {
			for _, lbEndpoint := range locality.LbEndpoints {
				addr := lbEndpoint.GetEndpoint().GetAddress().GetSocketAddress()
				ipStr := addr.GetAddress()
				portStr := strconv.Itoa(int(addr.GetPortValue()))

				discoveredIPs = append(discoveredIPs, net.JoinHostPort(ipStr, portStr))
			}
		}
	}

	if len(discoveredIPs) == 0 {
		btopt.Debugf(nil, "directaccess: Traffic Director returned an EDS response, but with zero endpoints")
		return nil, fmt.Errorf("found no endpoints in the EDS response")
	}

	btopt.Debugf(nil, "directaccess: Traffic Director successfully returned %d endpoints", len(discoveredIPs))
	return discoveredIPs, nil
}
