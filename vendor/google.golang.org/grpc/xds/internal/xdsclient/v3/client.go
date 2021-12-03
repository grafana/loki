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

// Package v3 provides xDS v3 transport protocol specific functionality.
package v3

import (
	"context"
	"fmt"

	"github.com/golang/protobuf/proto"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/grpc/xds/internal/xdsclient"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3adsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

func init() {
	xdsclient.RegisterAPIClientBuilder(clientBuilder{})
}

var (
	resourceTypeToURL = map[xdsclient.ResourceType]string{
		xdsclient.ListenerResource:    version.V3ListenerURL,
		xdsclient.RouteConfigResource: version.V3RouteConfigURL,
		xdsclient.ClusterResource:     version.V3ClusterURL,
		xdsclient.EndpointsResource:   version.V3EndpointsURL,
	}
)

type clientBuilder struct{}

func (clientBuilder) Build(cc *grpc.ClientConn, opts xdsclient.BuildOptions) (xdsclient.APIClient, error) {
	return newClient(cc, opts)
}

func (clientBuilder) Version() version.TransportAPI {
	return version.TransportV3
}

func newClient(cc *grpc.ClientConn, opts xdsclient.BuildOptions) (xdsclient.APIClient, error) {
	nodeProto, ok := opts.NodeProto.(*v3corepb.Node)
	if !ok {
		return nil, fmt.Errorf("xds: unsupported Node proto type: %T, want %T", opts.NodeProto, v3corepb.Node{})
	}
	v3c := &client{
		cc:        cc,
		parent:    opts.Parent,
		nodeProto: nodeProto,
		logger:    opts.Logger,
	}
	v3c.ctx, v3c.cancelCtx = context.WithCancel(context.Background())
	v3c.TransportHelper = xdsclient.NewTransportHelper(v3c, opts.Logger, opts.Backoff)
	return v3c, nil
}

type adsStream v3adsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesClient

// client performs the actual xDS RPCs using the xDS v3 API. It creates a
// single ADS stream on which the different types of xDS requests and responses
// are multiplexed.
type client struct {
	*xdsclient.TransportHelper

	ctx       context.Context
	cancelCtx context.CancelFunc
	parent    xdsclient.UpdateHandler
	logger    *grpclog.PrefixLogger

	// ClientConn to the xDS gRPC server. Owned by the parent xdsClient.
	cc        *grpc.ClientConn
	nodeProto *v3corepb.Node
}

func (v3c *client) NewStream(ctx context.Context) (grpc.ClientStream, error) {
	return v3adsgrpc.NewAggregatedDiscoveryServiceClient(v3c.cc).StreamAggregatedResources(v3c.ctx, grpc.WaitForReady(true))
}

// sendRequest sends out a DiscoveryRequest for the given resourceNames, of type
// rType, on the provided stream.
//
// version is the ack version to be sent with the request
// - If this is the new request (not an ack/nack), version will be empty.
// - If this is an ack, version will be the version from the response.
// - If this is a nack, version will be the previous acked version (from
//   versionMap). If there was no ack before, it will be empty.
func (v3c *client) SendRequest(s grpc.ClientStream, resourceNames []string, rType xdsclient.ResourceType, version, nonce, errMsg string) error {
	stream, ok := s.(adsStream)
	if !ok {
		return fmt.Errorf("xds: Attempt to send request on unsupported stream type: %T", s)
	}
	req := &v3discoverypb.DiscoveryRequest{
		Node:          v3c.nodeProto,
		TypeUrl:       resourceTypeToURL[rType],
		ResourceNames: resourceNames,
		VersionInfo:   version,
		ResponseNonce: nonce,
	}
	if errMsg != "" {
		req.ErrorDetail = &statuspb.Status{
			Code: int32(codes.InvalidArgument), Message: errMsg,
		}
	}
	if err := stream.Send(req); err != nil {
		return fmt.Errorf("xds: stream.Send(%+v) failed: %v", req, err)
	}
	v3c.logger.Debugf("ADS request sent: %v", pretty.ToJSON(req))
	return nil
}

// RecvResponse blocks on the receipt of one response message on the provided
// stream.
func (v3c *client) RecvResponse(s grpc.ClientStream) (proto.Message, error) {
	stream, ok := s.(adsStream)
	if !ok {
		return nil, fmt.Errorf("xds: Attempt to receive response on unsupported stream type: %T", s)
	}

	resp, err := stream.Recv()
	if err != nil {
		v3c.parent.NewConnectionError(err)
		return nil, fmt.Errorf("xds: stream.Recv() failed: %v", err)
	}
	v3c.logger.Infof("ADS response received, type: %v", resp.GetTypeUrl())
	v3c.logger.Debugf("ADS response received: %+v", pretty.ToJSON(resp))
	return resp, nil
}

func (v3c *client) HandleResponse(r proto.Message) (xdsclient.ResourceType, string, string, error) {
	rType := xdsclient.UnknownResource
	resp, ok := r.(*v3discoverypb.DiscoveryResponse)
	if !ok {
		return rType, "", "", fmt.Errorf("xds: unsupported message type: %T", resp)
	}

	// Note that the xDS transport protocol is versioned independently of
	// the resource types, and it is supported to transfer older versions
	// of resource types using new versions of the transport protocol, or
	// vice-versa. Hence we need to handle v3 type_urls as well here.
	var err error
	url := resp.GetTypeUrl()
	switch {
	case xdsclient.IsListenerResource(url):
		err = v3c.handleLDSResponse(resp)
		rType = xdsclient.ListenerResource
	case xdsclient.IsRouteConfigResource(url):
		err = v3c.handleRDSResponse(resp)
		rType = xdsclient.RouteConfigResource
	case xdsclient.IsClusterResource(url):
		err = v3c.handleCDSResponse(resp)
		rType = xdsclient.ClusterResource
	case xdsclient.IsEndpointsResource(url):
		err = v3c.handleEDSResponse(resp)
		rType = xdsclient.EndpointsResource
	default:
		return rType, "", "", xdsclient.ErrResourceTypeUnsupported{
			ErrStr: fmt.Sprintf("Resource type %v unknown in response from server", resp.GetTypeUrl()),
		}
	}
	return rType, resp.GetVersionInfo(), resp.GetNonce(), err
}

// handleLDSResponse processes an LDS response received from the management
// server. On receipt of a good response, it also invokes the registered watcher
// callback.
func (v3c *client) handleLDSResponse(resp *v3discoverypb.DiscoveryResponse) error {
	update, md, err := xdsclient.UnmarshalListener(resp.GetVersionInfo(), resp.GetResources(), v3c.logger)
	v3c.parent.NewListeners(update, md)
	return err
}

// handleRDSResponse processes an RDS response received from the management
// server. On receipt of a good response, it caches validated resources and also
// invokes the registered watcher callback.
func (v3c *client) handleRDSResponse(resp *v3discoverypb.DiscoveryResponse) error {
	update, md, err := xdsclient.UnmarshalRouteConfig(resp.GetVersionInfo(), resp.GetResources(), v3c.logger)
	v3c.parent.NewRouteConfigs(update, md)
	return err
}

// handleCDSResponse processes an CDS response received from the management
// server. On receipt of a good response, it also invokes the registered watcher
// callback.
func (v3c *client) handleCDSResponse(resp *v3discoverypb.DiscoveryResponse) error {
	update, md, err := xdsclient.UnmarshalCluster(resp.GetVersionInfo(), resp.GetResources(), v3c.logger)
	v3c.parent.NewClusters(update, md)
	return err
}

func (v3c *client) handleEDSResponse(resp *v3discoverypb.DiscoveryResponse) error {
	update, md, err := xdsclient.UnmarshalEndpoints(resp.GetVersionInfo(), resp.GetResources(), v3c.logger)
	v3c.parent.NewEndpoints(update, md)
	return err
}
