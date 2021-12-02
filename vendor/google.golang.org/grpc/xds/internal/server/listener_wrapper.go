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

// Package server contains internal server-side functionality used by the public
// facing xds package.
package server

import (
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/grpclog"
	internalbackoff "google.golang.org/grpc/internal/backoff"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
)

var (
	logger = grpclog.Component("xds")

	// Backoff strategy for temporary errors received from Accept(). If this
	// needs to be configurable, we can inject it through ListenerWrapperParams.
	bs = internalbackoff.Exponential{Config: backoff.Config{
		BaseDelay:  5 * time.Millisecond,
		Multiplier: 2.0,
		MaxDelay:   1 * time.Second,
	}}
	backoffFunc = bs.Backoff
)

// ServingMode indicates the current mode of operation of the server.
//
// This API exactly mirrors the one in the public xds package. We have to
// redefine it here to avoid a cyclic dependency.
type ServingMode int

const (
	// ServingModeStarting indicates that the serving is starting up.
	ServingModeStarting ServingMode = iota
	// ServingModeServing indicates the the server contains all required xDS
	// configuration is serving RPCs.
	ServingModeServing
	// ServingModeNotServing indicates that the server is not accepting new
	// connections. Existing connections will be closed gracefully, allowing
	// in-progress RPCs to complete. A server enters this mode when it does not
	// contain the required xDS configuration to serve RPCs.
	ServingModeNotServing
)

func (s ServingMode) String() string {
	switch s {
	case ServingModeNotServing:
		return "not-serving"
	case ServingModeServing:
		return "serving"
	default:
		return "starting"
	}
}

// ServingModeCallback is the callback that users can register to get notified
// about the server's serving mode changes. The callback is invoked with the
// address of the listener and its new mode. The err parameter is set to a
// non-nil error if the server has transitioned into not-serving mode.
type ServingModeCallback func(addr net.Addr, mode ServingMode, err error)

func prefixLogger(p *listenerWrapper) *internalgrpclog.PrefixLogger {
	return internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf("[xds-server-listener %p] ", p))
}

// XDSClient wraps the methods on the XDSClient which are required by
// the listenerWrapper.
type XDSClient interface {
	WatchListener(string, func(xdsclient.ListenerUpdate, error)) func()
	BootstrapConfig() *bootstrap.Config
}

// ListenerWrapperParams wraps parameters required to create a listenerWrapper.
type ListenerWrapperParams struct {
	// Listener is the net.Listener passed by the user that is to be wrapped.
	Listener net.Listener
	// ListenerResourceName is the xDS Listener resource to request.
	ListenerResourceName string
	// XDSCredsInUse specifies whether or not the user expressed interest to
	// receive security configuration from the control plane.
	XDSCredsInUse bool
	// XDSClient provides the functionality from the XDSClient required here.
	XDSClient XDSClient
	// ModeCallback is the callback to invoke when the serving mode changes.
	ModeCallback ServingModeCallback
}

// NewListenerWrapper creates a new listenerWrapper with params. It returns a
// net.Listener and a channel which is written to, indicating that the former is
// ready to be passed to grpc.Serve().
//
// Only TCP listeners are supported.
func NewListenerWrapper(params ListenerWrapperParams) (net.Listener, <-chan struct{}) {
	lw := &listenerWrapper{
		Listener:          params.Listener,
		name:              params.ListenerResourceName,
		xdsCredsInUse:     params.XDSCredsInUse,
		xdsC:              params.XDSClient,
		modeCallback:      params.ModeCallback,
		isUnspecifiedAddr: params.Listener.Addr().(*net.TCPAddr).IP.IsUnspecified(),

		closed:     grpcsync.NewEvent(),
		goodUpdate: grpcsync.NewEvent(),
	}
	lw.logger = prefixLogger(lw)

	// Serve() verifies that Addr() returns a valid TCPAddr. So, it is safe to
	// ignore the error from SplitHostPort().
	lisAddr := lw.Listener.Addr().String()
	lw.addr, lw.port, _ = net.SplitHostPort(lisAddr)

	cancelWatch := lw.xdsC.WatchListener(lw.name, lw.handleListenerUpdate)
	lw.logger.Infof("Watch started on resource name %v", lw.name)
	lw.cancelWatch = func() {
		cancelWatch()
		lw.logger.Infof("Watch cancelled on resource name %v", lw.name)
	}
	return lw, lw.goodUpdate.Done()
}

// listenerWrapper wraps the net.Listener associated with the listening address
// passed to Serve(). It also contains all other state associated with this
// particular invocation of Serve().
type listenerWrapper struct {
	net.Listener
	logger *internalgrpclog.PrefixLogger

	name          string
	xdsCredsInUse bool
	xdsC          XDSClient
	cancelWatch   func()
	modeCallback  ServingModeCallback

	// Set to true if the listener is bound to the IP_ANY address (which is
	// "0.0.0.0" for IPv4 and "::" for IPv6).
	isUnspecifiedAddr bool
	// Listening address and port. Used to validate the socket address in the
	// Listener resource received from the control plane.
	addr, port string

	// This is used to notify that a good update has been received and that
	// Serve() can be invoked on the underlying gRPC server. Using an event
	// instead of a vanilla channel simplifies the update handler as it need not
	// keep track of whether the received update is the first one or not.
	goodUpdate *grpcsync.Event
	// A small race exists in the XDSClient code between the receipt of an xDS
	// response and the user cancelling the associated watch. In this window,
	// the registered callback may be invoked after the watch is canceled, and
	// the user is expected to work around this. This event signifies that the
	// listener is closed (and hence the watch is cancelled), and we drop any
	// updates received in the callback if this event has fired.
	closed *grpcsync.Event

	// mu guards access to the current serving mode and the filter chains. The
	// reason for using an rw lock here is that these fields are read in
	// Accept() for all incoming connections, but writes happen rarely (when we
	// get a Listener resource update).
	mu sync.RWMutex
	// Current serving mode.
	mode ServingMode
	// Filter chains received as part of the last good update.
	filterChains *xdsclient.FilterChainManager
}

// Accept blocks on an Accept() on the underlying listener, and wraps the
// returned net.connWrapper with the configured certificate providers.
func (l *listenerWrapper) Accept() (net.Conn, error) {
	var retries int
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			// Temporary() method is implemented by certain error types returned
			// from the net package, and it is useful for us to not shutdown the
			// server in these conditions. The listen queue being full is one
			// such case.
			if ne, ok := err.(interface{ Temporary() bool }); !ok || !ne.Temporary() {
				return nil, err
			}
			retries++
			timer := time.NewTimer(backoffFunc(retries))
			select {
			case <-timer.C:
			case <-l.closed.Done():
				timer.Stop()
				// Continuing here will cause us to call Accept() again
				// which will return a non-temporary error.
				continue
			}
			continue
		}
		// Reset retries after a successful Accept().
		retries = 0

		// Since the net.Conn represents an incoming connection, the source and
		// destination address can be retrieved from the local address and
		// remote address of the net.Conn respectively.
		destAddr, ok1 := conn.LocalAddr().(*net.TCPAddr)
		srcAddr, ok2 := conn.RemoteAddr().(*net.TCPAddr)
		if !ok1 || !ok2 {
			// If the incoming connection is not a TCP connection, which is
			// really unexpected since we check whether the provided listener is
			// a TCP listener in Serve(), we return an error which would cause
			// us to stop serving.
			return nil, fmt.Errorf("received connection with non-TCP address (local: %T, remote %T)", conn.LocalAddr(), conn.RemoteAddr())
		}

		l.mu.RLock()
		if l.mode == ServingModeNotServing {
			// Close connections as soon as we accept them when we are in
			// "not-serving" mode. Since we accept a net.Listener from the user
			// in Serve(), we cannot close the listener when we move to
			// "not-serving". Closing the connection immediately upon accepting
			// is one of the other ways to implement the "not-serving" mode as
			// outlined in gRFC A36.
			l.mu.RUnlock()
			conn.Close()
			continue
		}
		fc, err := l.filterChains.Lookup(xdsclient.FilterChainLookupParams{
			IsUnspecifiedListener: l.isUnspecifiedAddr,
			DestAddr:              destAddr.IP,
			SourceAddr:            srcAddr.IP,
			SourcePort:            srcAddr.Port,
		})
		l.mu.RUnlock()
		if err != nil {
			// When a matching filter chain is not found, we close the
			// connection right away, but do not return an error back to
			// `grpc.Serve()` from where this Accept() was invoked. Returning an
			// error to `grpc.Serve()` causes the server to shutdown. If we want
			// to avoid the server from shutting down, we would need to return
			// an error type which implements the `Temporary() bool` method,
			// which is invoked by `grpc.Serve()` to see if the returned error
			// represents a temporary condition. In the case of a temporary
			// error, `grpc.Serve()` method sleeps for a small duration and
			// therefore ends up blocking all connection attempts during that
			// time frame, which is also not ideal for an error like this.
			l.logger.Warningf("connection from %s to %s failed to find any matching filter chain", conn.RemoteAddr().String(), conn.LocalAddr().String())
			conn.Close()
			continue
		}
		return &connWrapper{Conn: conn, filterChain: fc, parent: l}, nil
	}
}

// Close closes the underlying listener. It also cancels the xDS watch
// registered in Serve() and closes any certificate provider instances created
// based on security configuration received in the LDS response.
func (l *listenerWrapper) Close() error {
	l.closed.Fire()
	l.Listener.Close()
	if l.cancelWatch != nil {
		l.cancelWatch()
	}
	return nil
}

func (l *listenerWrapper) handleListenerUpdate(update xdsclient.ListenerUpdate, err error) {
	if l.closed.HasFired() {
		l.logger.Warningf("Resource %q received update: %v with error: %v, after listener was closed", l.name, update, err)
		return
	}

	if err != nil {
		l.logger.Warningf("Received error for resource %q: %+v", l.name, err)
		if xdsclient.ErrType(err) == xdsclient.ErrorTypeResourceNotFound {
			l.switchMode(nil, ServingModeNotServing, err)
		}
		// For errors which are anything other than "resource-not-found", we
		// continue to use the old configuration.
		return
	}
	l.logger.Infof("Received update for resource %q: %+v", l.name, update)

	// Make sure that the socket address on the received Listener resource
	// matches the address of the net.Listener passed to us by the user. This
	// check is done here instead of at the XDSClient layer because of the
	// following couple of reasons:
	// - XDSClient cannot know the listening address of every listener in the
	//   system, and hence cannot perform this check.
	// - this is a very context-dependent check and only the server has the
	//   appropriate context to perform this check.
	//
	// What this means is that the XDSClient has ACKed a resource which can push
	// the server into a "not serving" mode. This is not ideal, but this is
	// what we have decided to do. See gRPC A36 for more details.
	ilc := update.InboundListenerCfg
	if ilc.Address != l.addr || ilc.Port != l.port {
		l.switchMode(nil, ServingModeNotServing, fmt.Errorf("address (%s:%s) in Listener update does not match listening address: (%s:%s)", ilc.Address, ilc.Port, l.addr, l.port))
		return
	}

	l.switchMode(ilc.FilterChains, ServingModeServing, nil)
	l.goodUpdate.Fire()
}

func (l *listenerWrapper) switchMode(fcs *xdsclient.FilterChainManager, newMode ServingMode, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.filterChains = fcs
	l.mode = newMode
	if l.modeCallback != nil {
		l.modeCallback(l.Listener.Addr(), newMode, err)
	}
	l.logger.Warningf("Listener %q entering mode: %q due to error: %v", l.Addr(), newMode, err)
}
