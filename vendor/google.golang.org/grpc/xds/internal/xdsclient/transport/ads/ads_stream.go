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

// Package ads provides the implementation of an ADS (Aggregated Discovery
// Service) stream for the xDS client.
package ads

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/buffer"
	igrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/types/known/anypb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
)

// Any per-RPC level logs which print complete request or response messages
// should be gated at this verbosity level. Other per-RPC level logs which print
// terse output should be at `INFO` and verbosity 2.
const perRPCVerbosityLevel = 9

// Response represents a response received on the ADS stream. It contains the
// type URL, version, and resources for the response.
type Response struct {
	TypeURL   string
	Version   string
	Resources []*anypb.Any
}

// DataAndErrTuple is a struct that holds a resource and an error. It is used to
// return a resource and any associated error from a function.
type DataAndErrTuple struct {
	Resource xdsresource.ResourceData
	Err      error
}

// StreamEventHandler is an interface that defines the callbacks for events that
// occur on the ADS stream. Methods on this interface may be invoked
// concurrently and implementations need to handle them in a thread-safe manner.
type StreamEventHandler interface {
	OnADSStreamError(error)                           // Called when the ADS stream breaks.
	OnADSWatchExpiry(xdsresource.Type, string)        // Called when the watch timer expires for a resource.
	OnADSResponse(Response, func()) ([]string, error) // Called when a response is received on the ADS stream.
}

// WatchState is a enum that describes the watch state of a particular
// resource.
type WatchState int

const (
	// ResourceWatchStateStarted is the state where a watch for a resource was
	// started, but a request asking for that resource is yet to be sent to the
	// management server.
	ResourceWatchStateStarted WatchState = iota
	// ResourceWatchStateRequested is the state when a request has been sent for
	// the resource being watched.
	ResourceWatchStateRequested
	// ResourceWatchStateReceived is the state when a response has been received
	// for the resource being watched.
	ResourceWatchStateReceived
	// ResourceWatchStateTimeout is the state when the watch timer associated
	// with the resource expired because no response was received.
	ResourceWatchStateTimeout
)

// ResourceWatchState is the state corresponding to a resource being watched.
type ResourceWatchState struct {
	State       WatchState  // Watch state of the resource.
	ExpiryTimer *time.Timer // Timer for the expiry of the watch.
}

// State corresponding to a resource type.
type resourceTypeState struct {
	version             string                         // Last acked version. Should not be reset when the stream breaks.
	nonce               string                         // Last received nonce. Should be reset when the stream breaks.
	bufferedRequests    chan struct{}                  // Channel to buffer requests when writing is blocked.
	subscribedResources map[string]*ResourceWatchState // Map of subscribed resource names to their state.
	pendingWrite        bool                           // True if there is a pending write for this resource type.
}

// StreamImpl provides the functionality associated with an ADS (Aggregated
// Discovery Service) stream on the client side. It manages the lifecycle of the
// ADS stream, including creating the stream, sending requests, and handling
// responses. It also handles flow control and retries for the stream.
type StreamImpl struct {
	// The following fields are initialized from arguments passed to the
	// constructor and are read-only afterwards, and hence can be accessed
	// without a mutex.
	transport          transport.Transport     // Transport to use for ADS stream.
	eventHandler       StreamEventHandler      // Callbacks into the xdsChannel.
	backoff            func(int) time.Duration // Backoff for retries, after stream failures.
	nodeProto          *v3corepb.Node          // Identifies the gRPC application.
	watchExpiryTimeout time.Duration           // Resource watch expiry timeout
	logger             *igrpclog.PrefixLogger

	// The following fields are initialized in the constructor and are not
	// written to afterwards, and hence can be accessed without a mutex.
	streamCh     chan transport.StreamingCall // New ADS streams are pushed here.
	requestCh    *buffer.Unbounded            // Subscriptions and unsubscriptions are pushed here.
	runnerDoneCh chan struct{}                // Notify completion of runner goroutine.
	cancel       context.CancelFunc           // To cancel the context passed to the runner goroutine.

	// Guards access to the below fields (and to the contents of the map).
	mu                sync.Mutex
	resourceTypeState map[xdsresource.Type]*resourceTypeState // Map of resource types to their state.
	fc                *adsFlowControl                         // Flow control for ADS stream.
	firstRequest      bool                                    // False after the first request is sent out.
}

// StreamOpts contains the options for creating a new ADS Stream.
type StreamOpts struct {
	Transport          transport.Transport     // xDS transport to create the stream on.
	EventHandler       StreamEventHandler      // Callbacks for stream events.
	Backoff            func(int) time.Duration // Backoff for retries, after stream failures.
	NodeProto          *v3corepb.Node          // Node proto to identify the gRPC application.
	WatchExpiryTimeout time.Duration           // Resource watch expiry timeout.
	LogPrefix          string                  // Prefix to be used for log messages.
}

// NewStreamImpl initializes a new StreamImpl instance using the given
// parameters.  It also launches goroutines responsible for managing reads and
// writes for messages of the underlying stream.
func NewStreamImpl(opts StreamOpts) *StreamImpl {
	s := &StreamImpl{
		transport:          opts.Transport,
		eventHandler:       opts.EventHandler,
		backoff:            opts.Backoff,
		nodeProto:          opts.NodeProto,
		watchExpiryTimeout: opts.WatchExpiryTimeout,

		streamCh:          make(chan transport.StreamingCall, 1),
		requestCh:         buffer.NewUnbounded(),
		runnerDoneCh:      make(chan struct{}),
		resourceTypeState: make(map[xdsresource.Type]*resourceTypeState),
	}

	l := grpclog.Component("xds")
	s.logger = igrpclog.NewPrefixLogger(l, opts.LogPrefix+fmt.Sprintf("[ads-stream %p] ", s))

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.runner(ctx)
	return s
}

// Stop blocks until the stream is closed and all spawned goroutines exit.
func (s *StreamImpl) Stop() {
	s.cancel()
	s.requestCh.Close()
	<-s.runnerDoneCh
	s.logger.Infof("Stopping ADS stream")
}

// Subscribe subscribes to the given resource. It is assumed that multiple
// subscriptions for the same resource is deduped at the caller. A discovery
// request is sent out on the underlying stream for the resource type when there
// is sufficient flow control quota.
func (s *StreamImpl) Subscribe(typ xdsresource.Type, name string) {
	if s.logger.V(2) {
		s.logger.Infof("Subscribing to resource %q of type %q", name, typ.TypeName())
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.resourceTypeState[typ]
	if !ok {
		// An entry in the type state map is created as part of the first
		// subscription request for this type.
		state = &resourceTypeState{
			subscribedResources: make(map[string]*ResourceWatchState),
			bufferedRequests:    make(chan struct{}, 1),
		}
		s.resourceTypeState[typ] = state
	}

	// Create state for the newly subscribed resource. The watch timer will
	// be started when a request for this resource is actually sent out.
	state.subscribedResources[name] = &ResourceWatchState{State: ResourceWatchStateStarted}
	state.pendingWrite = true

	// Send a request for the resource type with updated subscriptions.
	s.requestCh.Put(typ)
}

// Unsubscribe cancels the subscription to the given resource. It is a no-op if
// the given resource does not exist. The watch expiry timer associated with the
// resource is stopped if one is active. A discovery request is sent out on the
// stream for the resource type when there is sufficient flow control quota.
func (s *StreamImpl) Unsubscribe(typ xdsresource.Type, name string) {
	if s.logger.V(2) {
		s.logger.Infof("Unsubscribing to resource %q of type %q", name, typ.TypeName())
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.resourceTypeState[typ]
	if !ok {
		return
	}

	rs, ok := state.subscribedResources[name]
	if !ok {
		return
	}
	if rs.ExpiryTimer != nil {
		rs.ExpiryTimer.Stop()
	}
	delete(state.subscribedResources, name)
	state.pendingWrite = true

	// Send a request for the resource type with updated subscriptions.
	s.requestCh.Put(typ)
}

// runner is a long-running goroutine that handles the lifecycle of the ADS
// stream. It spwans another goroutine to handle writes of discovery request
// messages on the stream. Whenever an existing stream fails, it performs
// exponential backoff (if no messages were received on that stream) before
// creating a new stream.
func (s *StreamImpl) runner(ctx context.Context) {
	defer close(s.runnerDoneCh)

	go s.send(ctx)

	runStreamWithBackoff := func() error {
		stream, err := s.transport.CreateStreamingCall(ctx, "/envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources")
		if err != nil {
			s.logger.Warningf("Failed to create a new ADS streaming RPC: %v", err)
			s.onError(err, false)
			return nil
		}
		if s.logger.V(2) {
			s.logger.Infof("ADS stream created")
		}

		s.mu.Lock()
		// Flow control is a property of the underlying streaming RPC call and
		// needs to be initialized everytime a new one is created.
		s.fc = newADSFlowControl(s.logger)
		s.firstRequest = true
		s.mu.Unlock()

		// Ensure that the most recently created stream is pushed on the
		// channel for the `send` goroutine to consume.
		select {
		case <-s.streamCh:
		default:
		}
		s.streamCh <- stream

		// Backoff state is reset upon successful receipt of at least one
		// message from the server.
		if s.recv(ctx, stream) {
			return backoff.ErrResetBackoff
		}
		return nil
	}
	backoff.RunF(ctx, runStreamWithBackoff, s.backoff)
}

// send is a long running goroutine that handles sending discovery requests for
// two scenarios:
// - a new subscription or unsubscription request is received
// - a new stream is created after the previous one failed
func (s *StreamImpl) send(ctx context.Context) {
	// Stores the most recent stream instance received on streamCh.
	var stream transport.StreamingCall
	for {
		select {
		case <-ctx.Done():
			return
		case stream = <-s.streamCh:
			if err := s.sendExisting(stream); err != nil {
				// Send failed, clear the current stream. Attempt to resend will
				// only be made after a new stream is created.
				stream = nil
				continue
			}
		case req, ok := <-s.requestCh.Get():
			if !ok {
				return
			}
			s.requestCh.Load()

			typ := req.(xdsresource.Type)
			if err := s.sendNew(stream, typ); err != nil {
				stream = nil
				continue
			}
		}
	}
}

// sendNew attempts to send a discovery request based on a new subscription or
// unsubscription. If there is no flow control quota, the request is buffered
// and will be sent later. This method also starts the watch expiry timer for
// resources that were sent in the request for the first time, i.e. their watch
// state is `watchStateStarted`.
func (s *StreamImpl) sendNew(stream transport.StreamingCall, typ xdsresource.Type) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If there's no stream yet, skip the request. This request will be resent
	// when a new stream is created. If no stream is created, the watcher will
	// timeout (same as server not sending response back).
	if stream == nil {
		return nil
	}

	// If local processing of the most recently received response is not yet
	// complete, i.e. fc.pending == true, queue this write and return early.
	// This allows us to batch writes for requests which are generated as part
	// of local processing of a received response.
	state := s.resourceTypeState[typ]
	if s.fc.pending.Load() {
		select {
		case state.bufferedRequests <- struct{}{}:
		default:
		}
		return nil
	}

	return s.sendMessageIfWritePendingLocked(stream, typ, state)
}

// sendExisting sends out discovery requests for existing resources when
// recovering from a broken stream.
//
// The stream argument is guaranteed to be non-nil.
func (s *StreamImpl) sendExisting(stream transport.StreamingCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for typ, state := range s.resourceTypeState {
		// Reset only the nonces map when the stream restarts.
		//
		// xDS spec says the following. See section:
		// https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#ack-nack-and-resource-type-instance-version
		//
		// Note that the version for a resource type is not a property of an
		// individual xDS stream but rather a property of the resources
		// themselves. If the stream becomes broken and the client creates a new
		// stream, the client’s initial request on the new stream should
		// indicate the most recent version seen by the client on the previous
		// stream
		state.nonce = ""

		if len(state.subscribedResources) == 0 {
			continue
		}

		state.pendingWrite = true
		if err := s.sendMessageIfWritePendingLocked(stream, typ, state); err != nil {
			return err
		}
	}
	return nil
}

// sendBuffered sends out discovery requests for resources that were buffered
// when they were subscribed to, because local processing of the previously
// received response was not yet complete.
//
// The stream argument is guaranteed to be non-nil.
func (s *StreamImpl) sendBuffered(stream transport.StreamingCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for typ, state := range s.resourceTypeState {
		select {
		case <-state.bufferedRequests:
			if err := s.sendMessageIfWritePendingLocked(stream, typ, state); err != nil {
				return err
			}
		default:
			// No buffered request.
			continue
		}
	}
	return nil
}

// sendMessageIfWritePendingLocked attempts to sends a discovery request to the
// server, if there is a pending write for the given resource type.
//
// If the request is successfully sent, the pending write field is cleared and
// watch timers are started for the resources in the request.
//
// Caller needs to hold c.mu.
func (s *StreamImpl) sendMessageIfWritePendingLocked(stream transport.StreamingCall, typ xdsresource.Type, state *resourceTypeState) error {
	if !state.pendingWrite {
		if s.logger.V(2) {
			s.logger.Infof("Skipping sending request for type %q, because all subscribed resources were already sent", typ.TypeURL())
		}
		return nil
	}

	names := resourceNames(state.subscribedResources)
	if err := s.sendMessageLocked(stream, names, typ.TypeURL(), state.version, state.nonce, nil); err != nil {
		return err
	}
	state.pendingWrite = false

	// Drain the buffered requests channel because we just sent a request for this
	// resource type.
	select {
	case <-state.bufferedRequests:
	default:
	}

	s.startWatchTimersLocked(typ, names)
	return nil
}

// sendMessageLocked sends a discovery request to the server, populating the
// different fields of the message with the given parameters. Returns a non-nil
// error if the request could not be sent.
//
// Caller needs to hold c.mu.
func (s *StreamImpl) sendMessageLocked(stream transport.StreamingCall, names []string, url, version, nonce string, nackErr error) error {
	req := &v3discoverypb.DiscoveryRequest{
		ResourceNames: names,
		TypeUrl:       url,
		VersionInfo:   version,
		ResponseNonce: nonce,
	}

	// The xDS protocol only requires that we send the node proto in the first
	// discovery request on every stream. Sending the node proto in every
	// request wastes CPU resources on the client and the server.
	if s.firstRequest {
		req.Node = s.nodeProto
	}

	if nackErr != nil {
		req.ErrorDetail = &statuspb.Status{
			Code: int32(codes.InvalidArgument), Message: nackErr.Error(),
		}
	}

	if err := stream.Send(req); err != nil {
		s.logger.Warningf("Sending ADS request for type %q, resources: %v, version: %q, nonce: %q failed: %v", url, names, version, nonce, err)
		return err
	}
	s.firstRequest = false

	if s.logger.V(perRPCVerbosityLevel) {
		s.logger.Infof("ADS request sent: %v", pretty.ToJSON(req))
	} else if s.logger.V(2) {
		s.logger.Warningf("ADS request sent for type %q, resources: %v, version: %q, nonce: %q", url, names, version, nonce)
	}
	return nil
}

// recv is responsible for receiving messages from the ADS stream.
//
// It performs the following actions:
//   - Waits for local flow control to be available before sending buffered
//     requests, if any.
//   - Receives a message from the ADS stream. If an error is encountered here,
//     it is handled by the onError method which propagates the error to all
//     watchers.
//   - Invokes the event handler's OnADSResponse method to process the message.
//   - Sends an ACK or NACK to the server based on the response.
//
// It returns a boolean indicating whether at least one message was received
// from the server.
func (s *StreamImpl) recv(ctx context.Context, stream transport.StreamingCall) bool {
	msgReceived := false
	for {
		// Wait for ADS stream level flow control to be available, and send out
		// a request if anything was buffered while we were waiting for local
		// processing of the previous response to complete.
		if !s.fc.wait(ctx) {
			if s.logger.V(2) {
				s.logger.Infof("ADS stream context canceled")
			}
			return msgReceived
		}
		s.sendBuffered(stream)

		resources, url, version, nonce, err := s.recvMessage(stream)
		if err != nil {
			s.onError(err, msgReceived)
			s.logger.Warningf("ADS stream closed: %v", err)
			return msgReceived
		}
		msgReceived = true

		// Invoke the onResponse event handler to parse the incoming message and
		// decide whether to send an ACK or NACK.
		resp := Response{
			Resources: resources,
			TypeURL:   url,
			Version:   version,
		}
		var resourceNames []string
		var nackErr error
		s.fc.setPending()
		resourceNames, nackErr = s.eventHandler.OnADSResponse(resp, s.fc.onDone)
		if xdsresource.ErrType(nackErr) == xdsresource.ErrorTypeResourceTypeUnsupported {
			// Based on gRFC A27, a general guiding principle is that if the
			// server sends something the client didn't actually subscribe to,
			// then the client ignores it. Here, we have received a response
			// with resources of a type that we don't know about.
			//
			// Sending a NACK doesn't really seem appropriate here, since we're
			// not actually validating what the server sent and therefore don't
			// know that it's invalid.  But we shouldn't ACK either, because we
			// don't know that it is valid.
			s.logger.Warningf("%v", nackErr)
			continue
		}

		s.onRecv(stream, resourceNames, url, version, nonce, nackErr)
	}
}

func (s *StreamImpl) recvMessage(stream transport.StreamingCall) (resources []*anypb.Any, url, version, nonce string, err error) {
	r, err := stream.Recv()
	if err != nil {
		return nil, "", "", "", err
	}
	resp, ok := r.(*v3discoverypb.DiscoveryResponse)
	if !ok {
		s.logger.Infof("Message received on ADS stream of unexpected type: %T", r)
		return nil, "", "", "", fmt.Errorf("unexpected message type %T", r)
	}

	if s.logger.V(perRPCVerbosityLevel) {
		s.logger.Infof("ADS response received: %v", pretty.ToJSON(resp))
	} else if s.logger.V(2) {
		s.logger.Infof("ADS response received for type %q, version %q, nonce %q", resp.GetTypeUrl(), resp.GetVersionInfo(), resp.GetNonce())
	}
	return resp.GetResources(), resp.GetTypeUrl(), resp.GetVersionInfo(), resp.GetNonce(), nil
}

// onRecv is invoked when a response is received from the server. The arguments
// passed to this method correspond to the most recently received response.
//
// It performs the following actions:
//   - updates resource type specific state
//   - updates resource specific state for resources in the response
//   - sends an ACK or NACK to the server based on the response
func (s *StreamImpl) onRecv(stream transport.StreamingCall, names []string, url, version, nonce string, nackErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Lookup the resource type specific state based on the type URL.
	var typ xdsresource.Type
	for t := range s.resourceTypeState {
		if t.TypeURL() == url {
			typ = t
			break
		}
	}
	typeState, ok := s.resourceTypeState[typ]
	if !ok {
		s.logger.Warningf("ADS stream received a response for type %q, but no state exists for it", url)
		return
	}

	// Update the resource type specific state. This includes:
	//   - updating the nonce unconditionally
	//   - updating the version only if the response is to be ACKed
	previousVersion := typeState.version
	typeState.nonce = nonce
	if nackErr == nil {
		typeState.version = version
	}

	// Update the resource specific state. For all resources received as
	// part of this response that are in state `started` or `requested`,
	// this includes:
	//   - setting the watch state to watchstateReceived
	//   - stopping the expiry timer, if one exists
	for _, name := range names {
		rs, ok := typeState.subscribedResources[name]
		if !ok {
			s.logger.Warningf("ADS stream received a response for resource %q, but no state exists for it", name)
			continue
		}
		if ws := rs.State; ws == ResourceWatchStateStarted || ws == ResourceWatchStateRequested {
			rs.State = ResourceWatchStateReceived
			if rs.ExpiryTimer != nil {
				rs.ExpiryTimer.Stop()
				rs.ExpiryTimer = nil
			}
		}
	}

	// Send an ACK or NACK.
	subscribedResourceNames := resourceNames(typeState.subscribedResources)
	if nackErr != nil {
		s.logger.Warningf("Sending NACK for resource type: %q, version: %q, nonce: %q, reason: %v", url, version, nonce, nackErr)
		s.sendMessageLocked(stream, subscribedResourceNames, url, previousVersion, nonce, nackErr)
		return
	}

	if s.logger.V(2) {
		s.logger.Infof("Sending ACK for resource type: %q, version: %q, nonce: %q", url, version, nonce)
	}
	s.sendMessageLocked(stream, subscribedResourceNames, url, version, nonce, nil)
}

// onError is called when an error occurs on the ADS stream. It stops any
// outstanding resource timers and resets the watch state to started for any
// resources that were in the requested state. It also handles the case where
// the ADS stream was closed after receiving a response, which is not
// considered an error.
func (s *StreamImpl) onError(err error, msgReceived bool) {
	// For resources that been requested but not yet responded to by the
	// management server, stop the resource timers and reset the watch state to
	// watchStateStarted. This is because we don't want the expiry timer to be
	// running when we don't have a stream open to the management server.
	s.mu.Lock()
	for _, state := range s.resourceTypeState {
		for _, rs := range state.subscribedResources {
			if rs.State != ResourceWatchStateRequested {
				continue
			}
			if rs.ExpiryTimer != nil {
				rs.ExpiryTimer.Stop()
				rs.ExpiryTimer = nil
			}
			rs.State = ResourceWatchStateStarted
		}
	}
	s.mu.Unlock()

	// Note that we do not consider it an error if the ADS stream was closed
	// after having received a response on the stream. This is because there
	// are legitimate reasons why the server may need to close the stream during
	// normal operations, such as needing to rebalance load or the underlying
	// connection hitting its max connection age limit.
	// (see [gRFC A9](https://github.com/grpc/proposal/blob/master/A9-server-side-conn-mgt.md)).
	if msgReceived {
		err = xdsresource.NewErrorf(xdsresource.ErrTypeStreamFailedAfterRecv, err.Error())
	}

	s.eventHandler.OnADSStreamError(err)
}

// startWatchTimersLocked starts the expiry timers for the given resource names
// of the specified resource type.  For each resource name, if the resource
// watch state is in the "started" state, it transitions the state to
// "requested" and starts an expiry timer. When the timer expires, the resource
// watch state is set to "timeout" and the event handler callback is called.
//
// The caller must hold the s.mu lock.
func (s *StreamImpl) startWatchTimersLocked(typ xdsresource.Type, names []string) {
	typeState := s.resourceTypeState[typ]
	for _, name := range names {
		resourceState, ok := typeState.subscribedResources[name]
		if !ok {
			continue
		}
		if resourceState.State != ResourceWatchStateStarted {
			continue
		}
		resourceState.State = ResourceWatchStateRequested

		rs := resourceState
		resourceState.ExpiryTimer = time.AfterFunc(s.watchExpiryTimeout, func() {
			s.mu.Lock()
			rs.State = ResourceWatchStateTimeout
			rs.ExpiryTimer = nil
			s.mu.Unlock()
			s.eventHandler.OnADSWatchExpiry(typ, name)
		})
	}
}

func resourceNames(m map[string]*ResourceWatchState) []string {
	ret := make([]string, len(m))
	idx := 0
	for name := range m {
		ret[idx] = name
		idx++
	}
	return ret
}

// TriggerResourceNotFoundForTesting triggers a resource not found event for the
// given resource type and name.  This is intended for testing purposes only, to
// simulate a resource not found scenario.
func (s *StreamImpl) TriggerResourceNotFoundForTesting(typ xdsresource.Type, resourceName string) {
	s.mu.Lock()

	state, ok := s.resourceTypeState[typ]
	if !ok {
		s.mu.Unlock()
		return
	}
	resourceState, ok := state.subscribedResources[resourceName]
	if !ok {
		s.mu.Unlock()
		return
	}

	if s.logger.V(2) {
		s.logger.Infof("Triggering resource not found for type: %s, resource name: %s", typ.TypeName(), resourceName)
	}
	resourceState.State = ResourceWatchStateTimeout
	if resourceState.ExpiryTimer != nil {
		resourceState.ExpiryTimer.Stop()
		resourceState.ExpiryTimer = nil
	}
	s.mu.Unlock()
	go s.eventHandler.OnADSWatchExpiry(typ, resourceName)
}

// ResourceWatchStateForTesting returns the ResourceWatchState for the given
// resource type and name.  This is intended for testing purposes only, to
// inspect the internal state of the ADS stream.
func (s *StreamImpl) ResourceWatchStateForTesting(typ xdsresource.Type, resourceName string) (ResourceWatchState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.resourceTypeState[typ]
	if !ok {
		return ResourceWatchState{}, fmt.Errorf("unknown resource type: %v", typ)
	}
	resourceState, ok := state.subscribedResources[resourceName]
	if !ok {
		return ResourceWatchState{}, fmt.Errorf("unknown resource name: %v", resourceName)
	}
	return *resourceState, nil
}

// adsFlowControl implements ADS stream level flow control that enables the
// transport to block the reading of the next message off of the stream until
// the previous update is consumed by all watchers.
//
// The lifetime of the flow control is tied to the lifetime of the stream.
type adsFlowControl struct {
	logger *igrpclog.PrefixLogger

	// Whether the most recent update is pending consumption by all watchers.
	pending atomic.Bool
	// Channel used to notify when all the watchers have consumed the most
	// recent update. Wait() blocks on reading a value from this channel.
	readyCh chan struct{}
}

// newADSFlowControl returns a new adsFlowControl.
func newADSFlowControl(logger *igrpclog.PrefixLogger) *adsFlowControl {
	return &adsFlowControl{
		logger:  logger,
		readyCh: make(chan struct{}, 1),
	}
}

// setPending changes the internal state to indicate that there is an update
// pending consumption by all watchers.
func (fc *adsFlowControl) setPending() {
	fc.pending.Store(true)
}

// wait blocks until all the watchers have consumed the most recent update and
// returns true. If the context expires before that, it returns false.
func (fc *adsFlowControl) wait(ctx context.Context) bool {
	// If there is no pending update, there is no need to block.
	if !fc.pending.Load() {
		// If all watchers finished processing the most recent update before the
		// `recv` goroutine made the next call to `Wait()`, there would be an
		// entry in the readyCh channel that needs to be drained to ensure that
		// the next call to `Wait()` doesn't unblock before it actually should.
		select {
		case <-fc.readyCh:
		default:
		}
		return true
	}

	select {
	case <-ctx.Done():
		return false
	case <-fc.readyCh:
		return true
	}
}

// onDone indicates that all watchers have consumed the most recent update.
func (fc *adsFlowControl) onDone() {
	select {
	// Writes to the readyCh channel should not block ideally. The default
	// branch here is to appease the paranoid mind.
	case fc.readyCh <- struct{}{}:
	default:
		if fc.logger.V(2) {
			fc.logger.Infof("ADS stream flow control readyCh is full")
		}
	}
	fc.pending.Store(false)
}
