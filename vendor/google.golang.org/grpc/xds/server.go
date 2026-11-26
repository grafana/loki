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

package xds

import (
	"errors"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	istats "google.golang.org/grpc/internal/stats"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/server"
	"google.golang.org/grpc/internal/xds/xdsclient"
)

const serverPrefix = "[xds-server %p] "

var (
	// These will be overridden in unit tests.
	xdsClientPool = xdsclient.DefaultPool
	newGRPCServer = func(opts ...grpc.ServerOption) grpcServer {
		return grpc.NewServer(opts...)
	}
)

// grpcServer contains methods from grpc.Server which are used by the
// GRPCServer type here. This is useful for overriding in unit tests.
type grpcServer interface {
	RegisterService(*grpc.ServiceDesc, any)
	Serve(net.Listener) error
	Stop()
	GracefulStop()
	GetServiceInfo() map[string]grpc.ServiceInfo
}

// GRPCServer wraps a gRPC server and provides server-side xDS functionality, by
// communication with a management server using xDS APIs. It implements the
// grpc.ServiceRegistrar interface and can be passed to service registration
// functions in IDL generated code.
type GRPCServer struct {
	gs             grpcServer
	quit           *grpcsync.Event
	logger         *internalgrpclog.PrefixLogger
	opts           *server.Options
	xdsC           xdsclient.XDSClient
	xdsClientClose func()
}

// NewGRPCServer creates an xDS-enabled gRPC server using the passed in opts.
// The underlying gRPC server has no service registered and has not started to
// accept requests yet.
func NewGRPCServer(opts ...grpc.ServerOption) (*GRPCServer, error) {
	// xdsFilterWrapper is a callback passed to the underlying gRPC server.
	// It adapts the generic stream parameter and delegates to RouteAndProcess
	// to execute xDS HTTP filter interceptors for each incoming RPC.
	xDSFilterWrapper := func(ss grpc.ServerStream) (grpc.ServerStream, error) {
		return server.RouteAndProcess(ss)
	}
	// Construct a grpc.ServerOption that registers xDSFilterWrapper on
	// the server.
	xDSFilterWrapperOption := internal.XDSFilterWrapperOption.(func(func(grpc.ServerStream) (grpc.ServerStream, error)) grpc.ServerOption)
	newOpts := append([]grpc.ServerOption{xDSFilterWrapperOption(xDSFilterWrapper)}, opts...)
	s := &GRPCServer{
		gs:   newGRPCServer(newOpts...),
		quit: grpcsync.NewEvent(),
	}
	s.handleServerOptions(opts)

	var mrl estats.MetricsRecorder
	mrl = istats.NewMetricsRecorderList(nil)
	if srv, ok := s.gs.(*grpc.Server); ok { // Will hit in prod but not for testing.
		mrl = internal.MetricsRecorderForServer.(func(*grpc.Server) estats.MetricsRecorder)(srv)
	}

	// Initializing the xDS client upfront (instead of at serving time)
	// simplifies the code by eliminating the need for a mutex to protect the
	// xdsC and xdsClientClose fields.
	pool := xdsClientPool
	if s.opts.ClientPoolForTesting != nil {
		pool = s.opts.ClientPoolForTesting
	}
	xdsClient, xdsClientClose, err := pool.NewClient(xdsclient.NameForServer, mrl)
	if err != nil {
		return nil, fmt.Errorf("xDS client creation failed: %v", err)
	}

	// Validate the bootstrap configuration for server specific fields.

	// Listener resource name template is mandatory on the server side unless a
	// listener resource name override is provided.
	if s.opts.OverrideListenerResourceName == nil {
		cfg := xdsClient.BootstrapConfig()
		if cfg.ServerListenerResourceNameTemplate() == "" {
			xdsClientClose()
			return nil, errors.New("missing server_listener_resource_name_template in the bootstrap configuration")
		}
	}

	s.xdsC = xdsClient
	s.xdsClientClose = xdsClientClose

	s.logger = internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf(serverPrefix, s))
	s.logger.Infof("Created xds.GRPCServer")

	return s, nil
}

// handleServerOptions iterates through the list of server options passed in by
// the user, and handles the xDS server specific options.
func (s *GRPCServer) handleServerOptions(opts []grpc.ServerOption) {
	so := s.defaultServerOptions()
	server.ApplyServerOptions(opts, so)
	s.opts = so
}

func (s *GRPCServer) defaultServerOptions() *server.Options {
	return &server.Options{
		// A default serving mode change callback which simply logs at the
		// default-visible log level. This will be used if the application does not
		// register a mode change callback.
		//
		// Note that this means that `s.opts.ModeCallback` will never be nil and can
		// safely be invoked directly from `handleServingModeChanges`.
		ModeCallback: s.loggingServerModeChangeCallback,
	}
}

func (s *GRPCServer) loggingServerModeChangeCallback(addr net.Addr, mode connectivity.ServingMode, err error) {
	switch mode {
	case connectivity.ServingModeServing:
		s.logger.Errorf("Listener %q entering mode: %q", addr.String(), mode)
	case connectivity.ServingModeNotServing:
		s.logger.Errorf("Listener %q entering mode: %q due to error: %v", addr.String(), mode, err)
	}
}

// RegisterService registers a service and its implementation to the underlying
// gRPC server. It is called from the IDL generated code. This must be called
// before invoking Serve.
func (s *GRPCServer) RegisterService(sd *grpc.ServiceDesc, ss any) {
	s.gs.RegisterService(sd, ss)
}

// GetServiceInfo returns a map from service names to ServiceInfo.
// Service names include the package names, in the form of <package>.<service>.
func (s *GRPCServer) GetServiceInfo() map[string]grpc.ServiceInfo {
	return s.gs.GetServiceInfo()
}

// Serve gets the underlying gRPC server to accept incoming connections on the
// listener lis, which is expected to be listening on a TCP port.
//
// A connection to the management server, to receive xDS configuration, is
// initiated here.
//
// Serve will return a non-nil error unless Stop or GracefulStop is called.
func (s *GRPCServer) Serve(lis net.Listener) error {
	s.logger.Infof("Serve() passed a net.Listener on %s", lis.Addr().String())
	if _, ok := lis.Addr().(*net.TCPAddr); !ok {
		return fmt.Errorf("xds: GRPCServer expects listener to return a net.TCPAddr. Got %T", lis.Addr())
	}

	if s.quit.HasFired() {
		return grpc.ErrServerStopped
	}

	// The server listener resource name template from the bootstrap
	// configuration contains a template for the name of the Listener resource
	// to subscribe to for a gRPC server. If the token `%s` is present in the
	// string, it will be replaced with the server's listening "IP:port" (e.g.,
	// "0.0.0.0:8080", "[::]:8080").
	var name string
	if s.opts.OverrideListenerResourceName != nil {
		name = s.opts.OverrideListenerResourceName(lis.Addr())
	} else {
		cfg := s.xdsC.BootstrapConfig()
		name = cfg.ServerListenerResourceNameTemplate()
		name = bootstrap.PopulateResourceTemplate(name, lis.Addr().String())
	}

	// Create a listenerWrapper which handles all functionality required by
	// this particular instance of Serve().
	lw := server.NewListenerWrapper(server.ListenerWrapperParams{
		Listener:             lis,
		ListenerResourceName: name,
		XDSClient:            s.xdsC,
		ModeCallback:         s.opts.ModeCallback,
	})
	return s.gs.Serve(lw)
}

// Stop stops the underlying gRPC server. It immediately closes all open
// connections. It cancels all active RPCs on the server side and the
// corresponding pending RPCs on the client side will get notified by connection
// errors.
func (s *GRPCServer) Stop() {
	s.quit.Fire()
	s.gs.Stop()
	if s.xdsC != nil {
		s.xdsClientClose()
	}
}

// GracefulStop stops the underlying gRPC server gracefully. It stops the server
// from accepting new connections and RPCs and blocks until all the pending RPCs
// are finished.
func (s *GRPCServer) GracefulStop() {
	s.quit.Fire()
	s.gs.GracefulStop()
	if s.xdsC != nil {
		s.xdsClientClose()
	}
}
