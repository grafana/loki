/*
 *
 * Copyright 2024 gRPC authors.
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

// Package grpctransport provides an implementation of the transport interface
// using gRPC.
package grpctransport

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/xds/internal/xdsclient/internal"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"

	v3adsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	v3adspb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	v3lrsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v3"
	v3lrspb "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v3"
)

func init() {
	internal.GRPCNewClient = grpc.NewClient
	internal.NewADSStream = func(ctx context.Context, cc *grpc.ClientConn) (v3adsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesClient, error) {
		return v3adsgrpc.NewAggregatedDiscoveryServiceClient(cc).StreamAggregatedResources(ctx)
	}
}

// Builder provides a way to build a gRPC-based transport to an xDS server.
type Builder struct{}

// Build creates a new gRPC-based transport to an xDS server using the provided
// options. This involves creating a grpc.ClientConn to the server identified by
// the server URI in the provided options.
func (b *Builder) Build(opts transport.BuildOptions) (transport.Transport, error) {
	if opts.ServerConfig == nil {
		return nil, fmt.Errorf("ServerConfig field in opts cannot be nil")
	}

	// NOTE: The bootstrap package ensures that the server_uri and credentials
	// inside the server config are always populated. If we end up using a
	// different type in BuildOptions to specify the server configuration, we
	// must ensure that those fields are not empty before proceeding.

	// Dial the xDS management server with dial options specified by the server
	// configuration and a static keepalive configuration that is common across
	// gRPC language implementations.
	kpCfg := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    5 * time.Minute,
		Timeout: 20 * time.Second,
	})
	dopts := append(opts.ServerConfig.DialOptions(), kpCfg)
	dialer := internal.GRPCNewClient.(func(string, ...grpc.DialOption) (*grpc.ClientConn, error))
	cc, err := dialer(opts.ServerConfig.ServerURI(), dopts...)
	if err != nil {
		// An error from a non-blocking dial indicates something serious.
		return nil, fmt.Errorf("failed to create a grpc transport to the management server %q: %v", opts.ServerConfig.ServerURI(), err)
	}
	cc.Connect()

	return &grpcTransport{cc: cc}, nil
}

type grpcTransport struct {
	cc *grpc.ClientConn
}

func (g *grpcTransport) CreateStreamingCall(ctx context.Context, method string) (transport.StreamingCall, error) {
	switch method {
	case v3adsgrpc.AggregatedDiscoveryService_StreamAggregatedResources_FullMethodName:
		return g.newADSStreamingCall(ctx)
	case v3lrsgrpc.LoadReportingService_StreamLoadStats_FullMethodName:
		return g.newLRSStreamingCall(ctx)
	default:
		return nil, fmt.Errorf("unsupported method: %v", method)
	}
}

func (g *grpcTransport) newADSStreamingCall(ctx context.Context) (transport.StreamingCall, error) {
	newStream := internal.NewADSStream.(func(context.Context, *grpc.ClientConn) (v3adsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesClient, error))
	stream, err := newStream(ctx, g.cc)
	if err != nil {
		return nil, fmt.Errorf("failed to create an ADS stream: %v", err)
	}
	return &adsStream{stream: stream}, nil
}

func (g *grpcTransport) newLRSStreamingCall(ctx context.Context) (transport.StreamingCall, error) {
	stream, err := v3lrsgrpc.NewLoadReportingServiceClient(g.cc).StreamLoadStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create an LRS stream: %v", err)
	}
	return &lrsStream{stream: stream}, nil
}

func (g *grpcTransport) Close() error {
	return g.cc.Close()
}

type adsStream struct {
	stream v3adsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesClient
}

func (a *adsStream) Send(msg any) error {
	return a.stream.Send(msg.(*v3adspb.DiscoveryRequest))
}

func (a *adsStream) Recv() (any, error) {
	return a.stream.Recv()
}

type lrsStream struct {
	stream v3lrsgrpc.LoadReportingService_StreamLoadStatsClient
}

func (l *lrsStream) Send(msg any) error {
	return l.stream.Send(msg.(*v3lrspb.LoadStatsRequest))
}

func (l *lrsStream) Recv() (any, error) {
	return l.stream.Recv()
}
