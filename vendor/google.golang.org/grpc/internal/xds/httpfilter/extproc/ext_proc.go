/*
 *
 * Copyright 2026 gRPC authors.
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

// Package extproc implements the Envoy external processing HTTP filter.
package extproc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/optional"
	"google.golang.org/grpc/internal/resolver"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/internal/xds/grpcservice"
	"google.golang.org/grpc/internal/xds/httpfilter"
	iextproc "google.golang.org/grpc/internal/xds/httpfilter/extproc/internal"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	v3procfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	v3procservicegrpc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	v3procservicepb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

func init() {
	if envconfig.XDSClientExtProcEnabled {
		httpfilter.Register(builder{})
	}
	iextproc.RegisterForTesting = func() {
		httpfilter.Register(builder{})
	}
	iextproc.UnregisterForTesting = func() {
		for _, typeURL := range builder.TypeURLs(builder{}) {
			httpfilter.UnregisterForTesting(typeURL)
		}
	}
	iextproc.TimeNowFunc = time.Now
	iextproc.TimeSinceFunc = time.Since
}

var metadataFromOutgoingContextRaw = internal.FromOutgoingContextRaw.(func(context.Context) (metadata.MD, [][]string, bool))

const defaultDeferredCloseTimeout = 5 * time.Second

func timeNow() time.Time {
	return iextproc.TimeNowFunc()
}

func timeSince(t time.Time) time.Duration {
	return iextproc.TimeSinceFunc(t)
}

type builder struct{}

func (builder) TypeURLs() []string {
	return []string{
		"type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor",
		"type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExtProcPerRoute",
	}
}

// validateBodyProcessingMode ensures that the body processing mode is either
// NONE or GRPC. Also ensures that if response body mode is GRPC then response
// trailer mode must be SEND.
func validateBodyProcessingMode(mode *v3procfilterpb.ProcessingMode) error {
	if m := mode.GetRequestBodyMode(); m != v3procfilterpb.ProcessingMode_NONE && m != v3procfilterpb.ProcessingMode_GRPC {
		return fmt.Errorf("extproc: invalid request body mode %v: want %q or %q", m, "NONE", "GRPC")
	}
	if m := mode.GetResponseBodyMode(); m != v3procfilterpb.ProcessingMode_NONE && m != v3procfilterpb.ProcessingMode_GRPC {
		return fmt.Errorf("extproc: invalid response body mode %v: want %q or %q", m, "NONE", "GRPC")
	}
	if mode.GetResponseBodyMode() == v3procfilterpb.ProcessingMode_GRPC && mode.GetResponseTrailerMode() != v3procfilterpb.ProcessingMode_SEND {
		return fmt.Errorf("extproc: invalid response trailer mode %v: must be %q when response body mode is %q", mode.GetResponseTrailerMode(), "SEND", "GRPC")
	}
	return nil
}

// ParseFilterConfig parses the provided filter configuration. The GrpcService
// identifying the external processor server is validated against the provided
// parse options, as per gRFC A102.
func (builder) ParseFilterConfig(cfg proto.Message, opts httpfilter.ParseOptions) (httpfilter.FilterConfig, error) {
	m, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extproc: error parsing config %v: unknown type %T, want *anypb.Any", cfg, cfg)
	}
	msg := new(v3procfilterpb.ExternalProcessor)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extproc: failed to unmarshal config %v: %v", cfg, err)
	}
	if msg.GetProcessingMode() == nil {
		return nil, fmt.Errorf("extproc: missing processing_mode in config %v", cfg)
	}
	if err := validateBodyProcessingMode(msg.GetProcessingMode()); err != nil {
		return nil, err
	}

	mutationRules, err := httpfilter.HeaderMutationRulesFromProto(msg.GetMutationRules())
	if err != nil {
		return nil, err
	}

	var allowedHeaders, disallowedHeaders []matcher.StringMatcher
	if allowed := msg.GetForwardRules().GetAllowedHeaders(); allowed != nil {
		allowedHeaders, err = httpfilter.ConvertStringMatchers(allowed.GetPatterns())
		if err != nil {
			return nil, err
		}
	}

	if disallowed := msg.GetForwardRules().GetDisallowedHeaders(); disallowed != nil {
		disallowedHeaders, err = httpfilter.ConvertStringMatchers(disallowed.GetPatterns())
		if err != nil {
			return nil, err
		}
	}

	deferredCloseTimeout := defaultDeferredCloseTimeout
	if msg.GetDeferredCloseTimeout() != nil {
		deferredCloseTimeout = msg.GetDeferredCloseTimeout().AsDuration()
	}

	// Parse the GrpcService last, so that no error path can drop the built
	// credentials: the caller owns them from here on.
	if msg.GetGrpcService() == nil {
		return nil, fmt.Errorf("extproc: empty grpc_service provided in config %v", cfg)
	}
	server, err := grpcservice.Parse(msg.GetGrpcService(), opts.BootstrapConfig, opts.ServerConfig)
	if err != nil {
		return nil, fmt.Errorf("extproc: failed to parse grpc_service: %v", err)
	}

	return baseConfig{
		processingModes:          processingModesFromProto(msg.GetProcessingMode()),
		requestAttributes:        msg.GetRequestAttributes(),
		responseAttributes:       msg.GetResponseAttributes(),
		disableImmediateResponse: msg.GetDisableImmediateResponse(),
		observabilityMode:        msg.GetObservabilityMode(),
		failureModeAllow:         msg.GetFailureModeAllow(),
		server:                   server,
		mutationRules:            mutationRules,
		allowedHeaders:           allowedHeaders,
		disallowedHeaders:        disallowedHeaders,
		deferredCloseTimeout:     deferredCloseTimeout,
	}, nil
}

// ParseFilterConfigOverride parses the provided override filter
// configuration. The GrpcService identifying the external processor server is
// validated against the provided parse options, as per gRFC A102.
func (builder) ParseFilterConfigOverride(ov proto.Message, opts httpfilter.ParseOptions) (httpfilter.FilterConfig, error) {
	m, ok := ov.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extproc: error parsing override %v: unknown type %T, want *anypb.Any", ov, ov)
	}
	msg := new(v3procfilterpb.ExtProcPerRoute)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extproc: failed to unmarshal override %v: %v", ov, err)
	}
	override := msg.GetOverrides()

	var processingModesOpt optional.Optional[processingModes]
	if pm := override.GetProcessingMode(); pm != nil {
		if err := validateBodyProcessingMode(pm); err != nil {
			return nil, err
		}
		processingModesOpt = optional.New(processingModesFromProto(pm))
	}

	var failureModeAllowOpt optional.Optional[bool]
	if override.GetFailureModeAllow() != nil {
		failureModeAllowOpt = optional.New(override.GetFailureModeAllow().GetValue())
	}

	// Parse the GrpcService last, so that no error path can drop the built
	// credentials: the caller owns them from here on.
	var serverOpt optional.Optional[*grpcservice.Config]
	if gs := override.GetGrpcService(); gs != nil {
		server, err := grpcservice.Parse(gs, opts.BootstrapConfig, opts.ServerConfig)
		if err != nil {
			return nil, fmt.Errorf("extproc: failed to parse grpc_service: %v", err)
		}
		serverOpt = optional.New(server)
	}

	return overrideConfig{
		server:             serverOpt,
		processingModes:    processingModesOpt,
		failureModeAllow:   failureModeAllowOpt,
		requestAttributes:  override.GetRequestAttributes(),
		responseAttributes: override.GetResponseAttributes(),
	}, nil
}

func (builder) IsTerminal() bool {
	return false
}

func (builder) BuildClientFilter(opts httpfilter.ClientFilterOptions) httpfilter.ClientFilter {
	return &clientFilter{
		metricsRecorder: opts.MetricsRecorder,
		target:          opts.Target,
	}
}

var _ httpfilter.ClientFilterBuilder = builder{}

// procChannelEntry is a shared external processor channel, together with the
// GrpcService config it was created from. The config is used for channel
// sharing comparisons when deciding whether the channel can be reused, and
// its credentials are released when the channel is closed.
type procChannelEntry struct {
	server *grpcservice.Config
	rc     *grpcsync.RefCounted[v3procservicegrpc.ExternalProcessorClient]
}

type clientFilter struct {
	metricsRecorder estats.MetricsRecorder
	target          string

	mu           sync.Mutex
	procChannels []*procChannelEntry
}

func (*clientFilter) Close() {}

// sharesChannel reports whether two GrpcService configs may share a channel
// to the external processor server: the same target with the same channel
// and call credential identities. Timeout and initial metadata are applied
// per-RPC and do not affect sharing; call credentials do, because Dial
// attaches them to the channel.
func sharesChannel(a, b *grpcservice.Config) bool {
	targetEqual := a.TargetURI == b.TargetURI
	channelCredsEqual := a.ChannelCredentials.Equal(b.ChannelCredentials)
	callCredsEqual := slices.EqualFunc(a.CallCredentials, b.CallCredentials, (*xdscreds.CallCreds).Equal)
	return targetEqual && channelCredsEqual && callCredsEqual
}

// getProcChannel returns an existing refcounted client for a channel-sharing
// compatible GrpcService config if present and its refcount is incremented
// successfully.
func (cf *clientFilter) getProcChannel(server *grpcservice.Config) *grpcsync.RefCounted[v3procservicegrpc.ExternalProcessorClient] {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	if i := slices.IndexFunc(cf.procChannels, func(e *procChannelEntry) bool {
		return sharesChannel(e.server, server) && e.rc.TryIncrement()
	}); i != -1 {
		return cf.procChannels[i].rc
	}
	return nil
}

// storeProcChannel stores the created channel entry if no valid channel
// exists for a channel-sharing compatible GrpcService config. If another
// goroutine already stored one while unlocked, it increments the existing
// channel's refcount and returns it.
func (cf *clientFilter) storeProcChannel(entry *procChannelEntry) *grpcsync.RefCounted[v3procservicegrpc.ExternalProcessorClient] {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	if i := slices.IndexFunc(cf.procChannels, func(e *procChannelEntry) bool {
		return sharesChannel(e.server, entry.server) && e.rc.TryIncrement()
	}); i != -1 {
		return cf.procChannels[i].rc
	}
	cf.procChannels = append(cf.procChannels, entry)
	return entry.rc
}

// removeProcChannel removes the entry from the list. When a channel's
// reference count drops to zero this cleanup runs asynchronously, so a
// replacement entry with an equal config may already have been stored;
// removal by entry pointer ensures only the expiring entry is removed.
func (cf *clientFilter) removeProcChannel(entry *procChannelEntry) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	cf.procChannels = slices.DeleteFunc(cf.procChannels, func(e *procChannelEntry) bool { return e == entry })
}

// getOrCreateExtProcChannel retrieves an existing refcounted external processor
// client for an equal server config and increases its refcount, or creates a
// new one if there is none.
func (cf *clientFilter) getOrCreateExtProcChannel(server *grpcservice.Config) (*grpcsync.RefCounted[v3procservicegrpc.ExternalProcessorClient], error) {
	// If a channel for a sharing-compatible GrpcService config is present and
	// its refcount is greater than 0, increment the refcount and return the
	// channel.
	if rc := cf.getProcChannel(server); rc != nil {
		return rc, nil
	}

	// Create the external processor channel without holding the lock. The
	// release function closes the channel.
	cc, release, err := iextproc.CreateExtProcChannel(server)
	if err != nil {
		return nil, fmt.Errorf("extproc: failed to create channel to the external processor server %q: %v", server.TargetURI, err)
	}

	client := v3procservicegrpc.NewExternalProcessorClient(cc)
	// Create a new refcounted client. The onZero cleanup function removes the
	// entry from the list, closes the underlying channel, and releases the
	// credentials of the config generation the channel was created from.
	entry := &procChannelEntry{server: server}
	entry.rc = grpcsync.NewRefCounted(client, func() {
		cf.removeProcChannel(entry)
		release()
		entry.server.Close()
	})

	// Double-check if another goroutine created and stored a channel for an
	// equal config while we were unlocked.
	if existing := cf.storeProcChannel(entry); existing != entry.rc {
		entry.rc.Decrement()
		return existing, nil
	}
	return entry.rc, nil
}

func (cf *clientFilter) BuildClientInterceptor(base, override httpfilter.FilterConfig) (httpfilter.ClientInterceptor, error) {
	b, ok := base.(baseConfig)
	if !ok {
		return nil, fmt.Errorf("extproc: incorrect config type provided (%T): %v", base, base)
	}

	var ov overrideConfig
	if override != nil {
		ov, ok = override.(overrideConfig)
		if !ok {
			return nil, fmt.Errorf("extproc: incorrect override config type provided (%T): %v", override, override)
		}
	}

	config := newInterceptorConfig(b, ov)

	// Create or reuse a refcounted channel to the external processor server.
	rc, err := cf.getOrCreateExtProcChannel(config.server)
	if err != nil {
		return nil, err
	}
	return &clientInterceptor{
		config:          config,
		procClient:      rc,
		metricsRecorder: cf.metricsRecorder,
		target:          cf.target,
	}, nil
}

type clientInterceptor struct {
	config          baseConfig
	procClient      *grpcsync.RefCounted[v3procservicegrpc.ExternalProcessorClient]
	metricsRecorder estats.MetricsRecorder
	target          string
}

func (i *clientInterceptor) Close() {
	i.procClient.Decrement()
}

// createProcContext creates a new child context for the RPC to the external
// processor server to be able to cancel it independently. This context has a
// deadline of the timeout specified in the config, if present, and contains the
// initial metadata specified in the config.
func createProcContext(ctx context.Context, server *grpcservice.Config) (procCtx context.Context, cancel context.CancelFunc) {
	if server.Timeout != 0 {
		procCtx, cancel = context.WithTimeout(ctx, server.Timeout)
	} else {
		procCtx, cancel = context.WithCancel(ctx)
	}
	procCtx = metadata.NewOutgoingContext(procCtx, server.InitialMetadata)
	return procCtx, cancel
}

func (i *clientInterceptor) NewStream(ctx context.Context, ri resolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
	// Increment procClient's refcount so this RPC stream keeps the connection
	// open even if the interceptor is closed. Decremented when the Process
	// stream finishes (via grpc.OnFinish) or fails to start.
	i.procClient.Increment()
	outgoingMD, added, _ := metadataFromOutgoingContextRaw(ctx)
	// Create a cancelable context to cancel the dataplane stream and close any
	// goroutines in case of error.
	cancelCtx, cancel := context.WithCancel(ctx)
	procCtx, procCancel := createProcContext(cancelCtx, i.config.server)

	// Collect OnFinishCallOption functions from the call options.
	var onFinish []func(error)
	for _, o := range opts {
		if onFinishOpt, ok := o.(grpc.OnFinishCallOption); ok && onFinishOpt.OnFinish != nil {
			onFinish = append(onFinish, onFinishOpt.OnFinish)
		}
	}

	csCommon := &commonStream{
		ctx:                    cancelCtx,
		cancel:                 cancel,
		procCancel:             procCancel,
		config:                 i.config,
		metricsRecorder:        i.metricsRecorder,
		target:                 i.target,
		clientHeadersStartTime: timeNow(),
		// Construct request attributes once during stream initialization to capture
		// the original, unmutated request metadata and RPC info. For streaming
		// RPCs, where stream creation and message transmission are separable,
		// initializing attributes upfront avoids repeated protobuf construction on
		// subsequent SendMsg and RecvMsg calls.
		reqAttrs: constructRequestAttributes(ri, outgoingMD, added, i.config.requestAttributes),
		onFinish: onFinish,
	}

	// In the ClientStream API, outgoing headers are sent immediately when the
	// stream is created. Because we need to send or mutate these headers before
	// they go over the wire, we must establish the ext_proc stream now rather
	// than deferring it.
	var err error
	procClient := i.procClient.Value()
	if csCommon.procStream, err = procClient.Process(procCtx, grpc.OnFinish(func(error) {
		i.procClient.Decrement()
	})); err != nil {
		return csCommon.handleInitError(fmt.Errorf("failed to create a stream to external processor: %w", err), newStream, opts...)
	}

	// Observability mode.
	if i.config.observabilityMode {
		ocs := &observabilityClientStream{
			commonStream: csCommon,
			procRecvDone: make(chan struct{}),
		}

		// Defer the closing of ext proc stream by the defined deferred close
		// timeout to allow the server to read all messages from the proc stream.
		onFinishFunc := func(error) {
			time.AfterFunc(ocs.config.deferredCloseTimeout, ocs.procCancel)
		}
		newOpts := append(opts, grpc.OnFinish(onFinishFunc))

		if ocs.dataplaneStream, err = newStream(ocs.ctx, newOpts...); err != nil {
			ocs.cancel()
			return nil, ocs.streamError(err)
		}
		ocs.recordMetric(clientHeadersDurationMetric, timeSince(ocs.clientHeadersStartTime).Seconds())
		// Start background goroutine to receive any messages from the external
		// processor server and discard them.
		go ocs.discardProcessorResponsesLoop()

		// If the request header processing mode is set to "Send", forward the
		// headers to the external processor server.
		if i.config.processingModes.requestHeaderMode == modeSend {
			if err = ocs.sendToProcessor(ocs.requestHeaders(outgoingMD, added)); err != nil {
				return ocs.handleInitError(fmt.Errorf("failed to send client headers to external processor server: %w", err), newStream, opts...)
			}
		}

		return ocs, nil
	}

	// Normal mode.
	cs := &clientStream{
		commonStream:                           csCommon,
		procStreamFailed:                       grpcsync.NewEvent(),
		procStreamBypass:                       grpcsync.NewEvent(),
		mutatedReqBuffer:                       buffer.NewUnbounded[*v3procservicepb.StreamedBodyResponse](),
		mutatedRespBuffer:                      buffer.NewUnbounded[*v3procservicepb.StreamedBodyResponse](),
		responseHeadersReady:                   grpcsync.NewEvent(),
		responseTrailerReady:                   grpcsync.NewEvent(),
		dataplaneSetup:                         make(chan struct{}),
		procSendCh:                             make(chan *v3procservicepb.ProcessingRequest),
		requestForwardLoopDoneCh:               make(chan struct{}),
		procRecvLoopDone:                       grpcsync.NewEvent(),
		downstreamToSidestreamPositiveUpdateCh: make(chan struct{}, 1),
		upstreamToSidestreamPositiveUpdateCh:   make(chan struct{}, 1),
	}
	cs.downstreamToSidestreamWindow.Store(iextproc.DefaultFlowControlWindowSize)
	cs.upstreamToSidestreamWindow.Store(iextproc.DefaultFlowControlWindowSize)

	// When request header processing is "Send", defer creating the dataplane
	// stream until the external processor responds, because gRPC transmits
	// outgoing headers immediately upon stream creation and we must apply any
	// header mutations first. If header processing is skipped, create the
	// dataplane stream immediately.
	if i.config.processingModes.requestHeaderMode == modeSend {
		if err = cs.procStream.Send(cs.requestHeaders(outgoingMD, added)); err != nil {
			if err == io.EOF {
				_, err = cs.procStream.Recv()
			}
			return cs.handleInitError(fmt.Errorf("failed to send client headers to external processor server: %w", err), newStream, opts...)
		}
	} else {
		if err = cs.createDataplaneStream(cs.ctx, newStream, opts); err != nil {
			return nil, err
		}
	}

	// Start a background loop for sending messages to the external processor
	// server. Use a single goroutine to ensure no concurrent sends.
	go cs.sendToProcServerLoop()

	// Start a background loop to receive messages from the external processor
	// server and send them on the dataplane stream in either direction.
	go cs.recvFromProcServerLoop(newStream, opts)

	return cs, nil
}

// commonStream contains state and synchronization primitives shared between the
// normal and observability mode client stream implementations.
type commonStream struct {
	// ctx is stored to allow blocking ClientStream interface methods (which do
	// not accept context parameters) to respect context cancellation/timeout and
	// retrieve ctx.Err() for returning the correct gRPC status error. This
	// context is directly derived from the dataplane RPC's context.
	ctx        context.Context
	cancel     context.CancelFunc
	procCancel context.CancelFunc
	config     baseConfig

	dataplaneStream  grpc.ClientStream                                 // underlying gRPC stream to the backend
	procStream       v3procservicegrpc.ExternalProcessor_ProcessClient // bidirectional stream to the external processor
	procStreamClosed atomic.Bool                                       // ensures the stream closure logic is executed exactly once
	procStreamErr    atomic.Value                                      // holds the terminal error causing the external processor stream to fail

	reqAttrs            map[string]*structpb.Struct // attributes to be sent to proc server with client message
	reqAttrsSent        atomic.Bool                 // tracks whether the first client message has been sent to ensure request attributes are sent only once
	firstProcReqSent    atomic.Bool                 // tracks whether the first proc req has been sent
	trailersOnly        bool                        // tracks whether the backend response is trailers-only
	responseHeaderOnce  atomic.Bool                 // ensures response headers are forwarded to the external processor only once
	responseTrailerOnce atomic.Bool                 // ensures response trailers are forwarded to the external processor only once

	metricsRecorder        estats.MetricsRecorder
	target                 string
	clientHeadersStartTime time.Time // holds the start time of client headers processing for metrics and relative timestamp calculations

	// onFinish stores OnFinishCallOption callbacks to execute if calling the
	// delegate (newStream) fails or if the RPC fails before delegating.
	onFinish            []func(err error)
	dataplaneRecvCalled atomic.Bool // tracks if RecvMsg has been called on dataplaneStream by the application
}

func (cs *commonStream) recordMetric(handle *estats.Float64HistoHandle, duration float64) {
	if cs.metricsRecorder == nil {
		return
	}
	handle.Record(cs.metricsRecorder, duration, cs.target)
}

// handleInitError handles failures during the initialization of the external
// processor stream in NewStream. In deny mode, it returns an internal error. In
// allow mode, it bypasses the external processor and creates and returns the
// dataplane stream directly.
//
// The error passed into handleInitError is a wrapped error and must not be
// returned directly to the caller to avoid exposing it as part of the public
// API.
func (cs *commonStream) handleInitError(err error, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
	cs.procCancel()

	if !errors.Is(err, io.EOF) && !cs.config.failureModeAllow {
		cs.cancel()
		return nil, status.Errorf(codes.Internal, "extproc: %v", err)
	}
	cs.recordMetric(clientHeadersDurationMetric, timeSince(cs.clientHeadersStartTime).Seconds())
	if cs.dataplaneStream, err = newStream(cs.ctx, opts...); err != nil {
		cs.cancel()
		return nil, err
	}
	cs.recordMetric(clientHeadersDurationMetric, timeSince(cs.clientHeadersStartTime).Seconds())
	return cs.dataplaneStream, nil
}

// callOnFinish executes all registered OnFinish CallOption callbacks.
func (cs *commonStream) callOnFinish(err error) {
	for _, f := range cs.onFinish {
		f(err)
	}
}

// cleanupDataplane cancels the dataplane stream's context to immediately tear
// down the active dataplane connection and unblock any pending client I/O.
//
// Additionally, after context cancellation, if the application has not called
// RecvMsg on the dataplane stream, it invokes a dummy RecvMsg on it. In
// gRPC-Go, unary RPCs do not start a background goroutine waiting for context
// expiration to call cs.finish(err). If an application or interceptor layer
// abandons an RPC after NewStream without calling SendMsg, CloseSend, or
// RecvMsg on the dataplane stream, calling RecvMsg on the canceled stream
// forces gRPC-Go's internal clientStream to encounter context.Canceled and
// execute cleanup. This guarantees that all registered OnFinish CallOption
// callbacks are executed and reference counts are decremented so that channels
// do not leak.
//
// For streaming RPCs where RecvMsg has already been called by the application,
// we avoid invoking a dummy RecvMsg because ClientStream.RecvMsg is not safe
// for concurrent calls by multiple goroutines on the same stream. Multiple
// goroutines may enter here concurrently; mutual exclusion is not required
// because cs.cancel() makes subsequent RecvMsg calls after context cancellation
// are idempotent.
func (cs *commonStream) cleanupDataplane() {
	cs.cancel()
	if cs.dataplaneStream != nil && !cs.dataplaneRecvCalled.Load() {
		cs.dataplaneStream.RecvMsg(new(any))
	}
}

// newProcessingRequest creates a new ProcessingRequest with ObservabilityMode,
// ProtocolConfig, FlowControlInit and Attributes fields initialized.
func (cs *commonStream) newProcessingRequest(isClientMessage bool) *v3procservicepb.ProcessingRequest {
	req := &v3procservicepb.ProcessingRequest{
		ObservabilityMode: cs.config.observabilityMode,
	}

	if cs.firstProcReqSent.CompareAndSwap(false, true) {
		req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
			RequestBodyMode:  convertBodyMode(cs.config.processingModes.requestBodyMode),
			ResponseBodyMode: convertBodyMode(cs.config.processingModes.responseBodyMode),
		}
		if !cs.config.observabilityMode {
			req.FlowControlInit = &v3procservicepb.ProcessingRequest_FlowControlInit{
				InitialWindowDownstreamToSidestream: iextproc.DefaultFlowControlWindowSize,
				InitialWindowUpstreamToSidestream:   iextproc.DefaultFlowControlWindowSize,
				InitialWindowSidestreamToUpstream:   iextproc.DefaultFlowControlWindowSize,
				InitialWindowSidestreamToDownstream: iextproc.DefaultFlowControlWindowSize,
			}
		}
	}

	if isClientMessage && cs.reqAttrsSent.CompareAndSwap(false, true) {
		req.Attributes = cs.reqAttrs
	}
	return req
}

// requestHeaders creates a ProcessingRequest for sending request headers.
func (cs *commonStream) requestHeaders(md metadata.MD, added [][]string) *v3procservicepb.ProcessingRequest {
	req := cs.newProcessingRequest(true)
	req.Request = &v3procservicepb.ProcessingRequest_RequestHeaders{
		RequestHeaders: &v3procservicepb.HttpHeaders{
			Headers: httpfilter.ConstructHeaderMap(md, added, cs.config.allowedHeaders, cs.config.disallowedHeaders),
		},
	}
	return req
}

// responseHeaders creates a ProcessingRequest for sending response
// headers.
func (cs *commonStream) responseHeaders(header metadata.MD) *v3procservicepb.ProcessingRequest {
	req := cs.newProcessingRequest(false)
	req.Request = &v3procservicepb.ProcessingRequest_ResponseHeaders{ResponseHeaders: &v3procservicepb.HttpHeaders{
		Headers:     httpfilter.ConstructHeaderMap(header, nil, cs.config.allowedHeaders, cs.config.disallowedHeaders),
		EndOfStream: cs.trailersOnly,
	}}
	return req
}

// responseTrailers creates a ProcessingRequest for sending response
// trailers.
func (cs *commonStream) responseTrailers(trailers metadata.MD) *v3procservicepb.ProcessingRequest {
	req := cs.newProcessingRequest(false)
	req.Request = &v3procservicepb.ProcessingRequest_ResponseTrailers{ResponseTrailers: &v3procservicepb.HttpTrailers{
		Trailers: httpfilter.ConstructHeaderMap(trailers, nil, cs.config.allowedHeaders, cs.config.disallowedHeaders),
	}}
	return req
}

// halfClose creates a ProcessingRequest indicating end of stream without
// a message body.
func (cs *commonStream) halfClose() *v3procservicepb.ProcessingRequest {
	req := cs.newProcessingRequest(true)
	req.Request = &v3procservicepb.ProcessingRequest_RequestBody{
		RequestBody: &v3procservicepb.HttpBody{
			EndOfStreamWithoutMessage: true,
			EndOfStream:               true,
		},
	}
	return req
}

// marshalAndCreateBodyReq marshals the message and creates a request or
// response body ProcessingRequest.
func (cs *commonStream) marshalAndCreateBodyReq(m any, isClientMessage bool) (*v3procservicepb.ProcessingRequest, error) {
	msg, ok := m.(proto.Message)
	if !ok {
		return nil, status.Errorf(codes.Internal, "extproc: message does not implement proto.Message")
	}
	bodyBytes, err := proto.Marshal(msg)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "extproc: failed to marshal message: %v", err)
	}

	req := cs.newProcessingRequest(isClientMessage)
	if isClientMessage {
		req.Request = &v3procservicepb.ProcessingRequest_RequestBody{
			RequestBody: &v3procservicepb.HttpBody{Body: bodyBytes},
		}
		return req, nil
	}
	req.Request = &v3procservicepb.ProcessingRequest_ResponseBody{
		ResponseBody: &v3procservicepb.HttpBody{Body: bodyBytes},
	}
	return req, nil
}

// observabilityClientStream implements resolver.ClientStream to coordinate
// message exchanges between the application client, the external processor, and
// the backend dataplane in observability mode.
type observabilityClientStream struct {
	*commonStream

	procStreamBypass atomic.Bool   // set to true when the external processor stream should be bypassed
	procRecvDone     chan struct{} // closed when discardProcessorResponsesLoop detects stream closure/failure

	responseRecvStarted bool        // tracks whether RecvMsg has started processing response messages
	responseHeader      metadata.MD // stores response headers received from dataplane
	responseHeaderErr   error       // stores response header error received from dataplane
	mu                  sync.Mutex  // guards concurrent writes to the external processor stream
}

// streamError returns the appropriate error when a stream operation fails.
// It prioritizes the external processor stream's terminal error.
func (ocs *observabilityClientStream) streamError(err error) error {
	if fatalErr, ok := ocs.procStreamErr.Load().(error); ok {
		return fatalErr
	}
	return err
}

func (ocs *observabilityClientStream) Header() (metadata.MD, error) {
	if fatalErr, ok := ocs.procStreamErr.Load().(error); ok {
		return nil, fatalErr
	}
	return ocs.initiateResponseHeaderProcessing()
}

func (ocs *observabilityClientStream) Trailer() metadata.MD {
	if _, ok := ocs.procStreamErr.Load().(error); ok {
		return nil
	}

	// If trailer is not yet ready, return nil. This may happen when Trailer() is
	// called before the server sends trailers, or before the entire response has
	// been received.
	if tr := ocs.dataplaneStream.Trailer(); tr != nil {
		ocs.initiateResponseTrailerProcessing(tr)
		return tr
	}
	return nil
}

func (ocs *observabilityClientStream) CloseSend() error {
	if fatalErr, ok := ocs.procStreamErr.Load().(error); ok {
		return fatalErr
	}
	startTime := timeNow()
	if !ocs.procStreamBypass.Load() && ocs.config.processingModes.requestBodyMode == modeSend {
		if err := ocs.sendToProcessor(ocs.halfClose()); err != nil {
			return err
		}
	}
	err := ocs.dataplaneStream.CloseSend()
	ocs.recordMetric(clientHalfCloseDurationMetric, timeSince(startTime).Seconds())
	return ocs.streamError(err)
}

func (ocs *observabilityClientStream) Context() context.Context {
	return ocs.dataplaneStream.Context()
}

func (ocs *observabilityClientStream) SendMsg(m any) error {
	if fatalErr, ok := ocs.procStreamErr.Load().(error); ok {
		return fatalErr
	}
	if ocs.config.processingModes.requestBodyMode == modeSend && !ocs.procStreamBypass.Load() {
		if err := ocs.sendBodyToProcessor(m, true); err != nil {
			return err
		}
	}
	err := ocs.dataplaneStream.SendMsg(m)
	return ocs.streamError(err)
}

func (ocs *observabilityClientStream) RecvMsg(m any) error {
	if fatalErr, ok := ocs.procStreamErr.Load().(error); ok {
		return fatalErr
	}

	// If this is the first time RecvMsg is called, initiate response header
	// processing.
	if !ocs.responseRecvStarted {
		ocs.responseRecvStarted = true
		if _, err := ocs.initiateResponseHeaderProcessing(); err != nil {
			return err
		}
	}

	if !ocs.dataplaneRecvCalled.Load() {
		ocs.dataplaneRecvCalled.Store(true)
	}
	if err := ocs.dataplaneStream.RecvMsg(m); err != nil {
		if trerr := ocs.initiateResponseTrailerProcessing(ocs.dataplaneStream.Trailer()); trerr != nil {
			return trerr
		}
		return ocs.streamError(err)
	}

	if ocs.config.processingModes.responseBodyMode == modeSend && !ocs.procStreamBypass.Load() {
		if err := ocs.sendBodyToProcessor(m, false); err != nil {
			return err
		}
	}
	return ocs.streamError(nil)
}

// sendBodyToProcessor marshals the given message and forwards it as a request
// or response body ProcessingRequest to the external processor stream.
func (ocs *observabilityClientStream) sendBodyToProcessor(m any, isClientMessage bool) error {
	req, err := ocs.marshalAndCreateBodyReq(m, isClientMessage)
	if err != nil {
		return err
	}
	return ocs.sendToProcessor(req)
}

// initiateResponseTrailerProcessing fetches response trailers from the
// dataplane stream and forwards them to the external processor if configured.
// It is guarded by responseTrailerOnce to run at most once.
func (ocs *observabilityClientStream) initiateResponseTrailerProcessing(trailers metadata.MD) error {
	if ocs.responseTrailerOnce.Load() || !ocs.responseTrailerOnce.CompareAndSwap(false, true) {
		return nil
	}
	startTime := timeNow()
	if ocs.config.processingModes.responseTrailerMode == modeSend && !ocs.procStreamBypass.Load() && !ocs.trailersOnly {
		if err := ocs.sendToProcessor(ocs.responseTrailers(trailers)); err != nil {
			return err
		}
	}
	ocs.recordMetric(serverTrailersDurationMetric, timeSince(startTime).Seconds())

	// Half-close the external processor stream immediately so the server can
	// shut down gracefully.
	if !ocs.procStreamBypass.Load() {
		ocs.mu.Lock()
		ocs.procStream.CloseSend()
		ocs.mu.Unlock()
	}
	return nil
}

// initiateResponseHeaderProcessing fetches response headers from the dataplane
// stream, detects if the response is trailers-only, and forwards the response
// headers to the external processor if configured. It is guarded by
// responseHeaderOnce to run at most once.
func (ocs *observabilityClientStream) initiateResponseHeaderProcessing() (metadata.MD, error) {
	if ocs.responseHeaderOnce.Load() || !ocs.responseHeaderOnce.CompareAndSwap(false, true) {
		return ocs.responseHeader, ocs.streamError(ocs.responseHeaderErr)
	}
	ocs.responseHeader, ocs.responseHeaderErr = ocs.dataplaneStream.Header()
	if ocs.responseHeaderErr != nil {
		return nil, ocs.streamError(ocs.responseHeaderErr)
	}
	startTime := timeNow()
	// A trailers-only response returns nil from dataplaneStream.Header().
	procHeader := ocs.responseHeader
	if ocs.responseHeader == nil {
		procHeader = ocs.dataplaneStream.Trailer()
		if len(procHeader) > 0 {
			ocs.trailersOnly = true
		}
	}

	if ocs.config.processingModes.responseHeaderMode == modeSend && !ocs.procStreamBypass.Load() {
		if err := ocs.sendToProcessor(ocs.responseHeaders(procHeader)); err != nil {
			return nil, err
		}
	}
	ocs.recordMetric(serverHeadersDurationMetric, timeSince(startTime).Seconds())
	return ocs.responseHeader, nil
}

// sendToProcessor sends a ProcessingRequest to the external processor stream.
// If the write fails, it waits for the receive background loop to process the
// closure and returns the resulting error.
func (ocs *observabilityClientStream) sendToProcessor(req *v3procservicepb.ProcessingRequest) error {
	ocs.mu.Lock()
	err := ocs.procStream.Send(req)
	ocs.mu.Unlock()
	if err != nil {
		select {
		case <-ocs.procRecvDone:
		case <-ocs.ctx.Done():
		}
		if fatalErr, ok := ocs.procStreamErr.Load().(error); ok {
			return fatalErr
		}
		if ocs.procStreamBypass.Load() {
			return nil
		}
		return err
	}
	return nil
}

// discardProcessorResponsesLoop runs in a background goroutine to continuously
// read and discard messages from the external processor server, while also
// monitoring the stream for closure or failure.
//
// Per the Envoy ExtProc protobuf specification for observability mode,
// "External processor should not send back processing response, as any
// responses will be ignored."
func (ocs *observabilityClientStream) discardProcessorResponsesLoop() {
	for {
		if _, err := ocs.procStream.Recv(); err != nil {
			ocs.failObsProcStream(err)
			return
		}
	}
}

// failObsProcStream handles proc stream closure or failure by updating the
// stream state. If the failure is a non-EOF error and failure mode is deny, it
// stores the error to fail the dataplane RPC. Otherwise, it enables bypass
// mode.
func (ocs *observabilityClientStream) failObsProcStream(err error) {
	if !ocs.procStreamClosed.CompareAndSwap(false, true) {
		return
	}
	defer close(ocs.procRecvDone)
	ocs.procCancel()
	if err != io.EOF && !ocs.config.failureModeAllow {
		ocs.procStreamErr.Store(status.Errorf(codes.Internal, "extproc: external processor RPC failed: %v", err))
		ocs.cleanupDataplane()
		return
	}
	ocs.procStreamBypass.Store(true)
}

// clientStream implements resolver.ClientStream to coordinate bidirectional
// message exchanges between the application client, the external processor, and
// the backend dataplane.
type clientStream struct {
	*commonStream

	procStreamFailed *grpcsync.Event // fired when external processor stream has closed and RPC should be failed
	procStreamBypass *grpcsync.Event // fired when the external processor stream should be bypassed or drained

	ignoreFailureMode    atomic.Bool                                              // tracks whether the failureModeAllow setting should be ignored (e.g. after sending body messages)
	discardRequests      atomic.Bool                                              // set when ext_proc server signals end_of_stream to stop client sends
	mutatedReqBuffer     *buffer.Unbounded[*v3procservicepb.StreamedBodyResponse] // buffers mutated request body messages from the ext_proc server
	reqForwardingStarted bool                                                     // tracks whether request forwarding loop to the dataplane has started
	procSendCh           chan *v3procservicepb.ProcessingRequest                  // serializes writes to the external processor stream to ensure thread-safety

	responseHeader        metadata.MD                                              // stores headers received from the dataplane stream
	responseHeadersReady  *grpcsync.Event                                          // signals that response headers are ready for client
	responseHeaderSent    atomic.Bool                                              // tracks whether response headers have been dispatched to external processor
	responseTrailers      metadata.MD                                              // stores trailers received from the dataplane stream
	responseTrailerReady  *grpcsync.Event                                          // signals that response trailers are ready for client
	trailerSent           atomic.Bool                                              // tracks whether response trailers have been dispatched
	trailerErr            atomic.Value                                             // holds any terminal status returned upon processing response trailers (e.g. ImmediateResponse)
	mutatedRespBuffer     *buffer.Unbounded[*v3procservicepb.StreamedBodyResponse] // buffers mutated response body messages from the ext_proc server
	responseDrained       atomic.Bool                                              // tracks whether all buffered response body messages from the ext_proc server have been drained
	dataplaneSetup        chan struct{}                                            // closed once dataplaneStream creation attempt is complete
	dataplaneCreationErr  error                                                    // stores the error from the dataplane stream creation attempt
	respForwardingStarted bool                                                     // tracks whether response forwarding loop to the external processor has started

	requestForwardLoopDoneCh chan struct{}   // closed when request forwarding loop finishes draining
	procRecvLoopDone         *grpcsync.Event // fires when external processor stream receive loop finishes

	// The following start times are accessed concurrently across goroutines
	// during the stream lifetime. We store them as atomic.Int64 nanosecond
	// offsets from clientHeadersStartTime instead of time.Time because time.Time
	// cannot be accessed atomically without a mutex and atomic.Int64 provides
	// lock-free atomic operations.
	clientHalfCloseStartTime atomic.Int64
	serverHeadersStartTime   atomic.Int64
	serverTrailersStartTime  atomic.Int64

	downstreamToSidestreamWindow           atomic.Int64  // downstream to sidestream window size remaining
	downstreamToSidestreamPositiveUpdateCh chan struct{} // signals the downstream to sidestream window becoming positive

	upstreamToSidestreamWindow           atomic.Int64  // upstream to sidestream window size remaining
	upstreamToSidestreamPositiveUpdateCh chan struct{} // signals the upstream to sidestream window becoming positive

	pendingRespWindowUpdate atomic.Int64
	pendingReqWindowUpdate  atomic.Int64
}

// recordDuration records the elapsed time since the event start timestamp
// stored in val to the histogram metric. val holds the start timestamp as a
// nanosecond offset from cs.clientHeadersStartTime. It is a no-op if val has
// not been set (value is 0).
func (cs *clientStream) recordDuration(handle *estats.Float64HistoHandle, val *atomic.Int64) {
	if offsetNs := val.Load(); offsetNs != 0 {
		startTime := cs.clientHeadersStartTime.Add(time.Duration(offsetNs))
		cs.recordMetric(handle, timeSince(startTime).Seconds())
	}
}

// fireResponseHeadersReady fires the responseHeadersReady event, and records
// the server_headers_duration metric exactly once upon firing.
func (cs *clientStream) fireResponseHeadersReady() {
	if cs.responseHeadersReady.Fire() {
		cs.recordDuration(serverHeadersDurationMetric, &cs.serverHeadersStartTime)
	}
}

// fireResponseTrailerReady fires the responseTrailerReady event, and records
// the server_trailers_duration metric exactly once upon firing.
func (cs *clientStream) fireResponseTrailerReady() {
	if cs.responseTrailerReady.Fire() {
		cs.recordDuration(serverTrailersDurationMetric, &cs.serverTrailersStartTime)
	}
}

// Header returns the response headers received from the backend, potentially
// modified by the external processor if configured. It blocks until header
// processing is complete, or returns nil if the response is trailers-only.
func (cs *clientStream) Header() (metadata.MD, error) {
	if err := cs.initiateResponseHeaderProcessing(); err != nil {
		return nil, err
	}
	// Wait for the response headers to be modified.
	select {
	case <-cs.responseHeadersReady.Done():
		if cs.trailersOnly {
			return nil, nil
		}
		return cs.responseHeader, nil
	case <-cs.procStreamFailed.Done():
		return nil, cs.streamError()
	case <-cs.ctx.Done():
		return nil, cs.streamError()
	}
}

// Trailer returns the trailer metadata received from the server, potentially
// modified by the external processor if configured. It returns nil or empty map
// immediately if trailers are not yet received from the backend. If backend
// trailers are available, it blocks until any configured trailer processing by
// the external processor completes.
func (cs *clientStream) Trailer() metadata.MD {
	// If trailers are already modified and ready, return them immediately.
	if cs.responseTrailerReady.HasFired() {
		return cs.responseTrailers
	}
	// Checking if backend stream has trailers ready now.
	s, err := cs.waitForDataplaneStream(cs.ctx)
	// Since Trailer is not supposed to be a blocking function and should be
	// called after CloseRecv or after Recv() returns an error, it should have the
	// dataplane stream created. If it does not, then Trailer has been called
	// prematurely, which means we need to return nil and not block here.
	if err != nil {
		return nil
	}
	// If no backend trailers are ready yet, return nil to preserve standard
	// non-blocking behaviour.
	if len(s.Trailer()) == 0 {
		return nil
	}

	cs.initiateResponseTrailerProcessing()
	select {
	case <-cs.responseTrailerReady.Done():
		return cs.responseTrailers
	case <-cs.procStreamFailed.Done():
		return nil
	case <-cs.ctx.Done():
		return nil
	}
}

func (cs *clientStream) CloseSend() error {
	if cs.discardRequests.Load() {
		return nil
	}
	if cs.clientHalfCloseStartTime.Load() == 0 {
		offset := timeSince(cs.clientHeadersStartTime)
		cs.clientHalfCloseStartTime.CompareAndSwap(0, int64(offset))
	}

	s, err := cs.bypassProcStreamForClientMsg()
	if err != nil {
		return err
	}
	if s != nil {
		cs.recordDuration(clientHalfCloseDurationMetric, &cs.clientHalfCloseStartTime)
		err = s.CloseSend()
		return err
	}
	// If external processor stream is active, client CloseSend is sent to the
	// processor server as request message with `EndOfStreamWithoutMessage` set.
	s, err = cs.sendClientReqToProcServer(cs.halfClose())
	if s != nil {
		cs.recordDuration(clientHalfCloseDurationMetric, &cs.clientHalfCloseStartTime)
		err = s.CloseSend()
		return err
	}
	return err
}

func (cs *clientStream) Context() context.Context {
	s, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return cs.ctx
	}
	return s.Context()
}

func (cs *clientStream) RecvMsg(m any) error {
	// Initiate response header processing because external processor requires the
	// events to be sent in the correct order, i.e. response header before
	// response message. And if Header() has not already been called, send the
	// response headers to external processor server first.
	if err := cs.initiateResponseHeaderProcessing(); err != nil {
		return err
	}

	// If all the responses from external processor server have been drained or if
	// the external processor is bypassed, or if the response body mode is skip,
	// then receive directly from the dataplane stream.
	if cs.responseDrained.Load() || (cs.procStreamBypass.HasFired() && !cs.respForwardingStarted) || cs.config.processingModes.responseBodyMode == modeSkip {
		return cs.recvFromDataplane(m)
	}

	msg, ok := m.(proto.Message)
	if !ok {
		return fmt.Errorf("extproc: response message does not implement proto.Message")
	}

	// Start the background receiving loop on the first RecvMsg call to capture
	// the type of message to be received.
	if !cs.respForwardingStarted {
		cs.respForwardingStarted = true
		go cs.responseForwardingToProcServerLoop(msg.ProtoReflect().Type())
	}

	// Pull response messages from mutatedRespBuffer.
	select {
	case streamedResp, ok := <-cs.mutatedRespBuffer.Get():
		cs.mutatedRespBuffer.Load()
		if !ok {
			// Closed channel implies that all messages from the external processor
			// have been received. Start receiving directly from dataplane stream.
			cs.responseDrained.Store(true)
			return cs.recvFromDataplane(m)
		}
		// Update the pending window update after pulling from the buffer. If the
		// pending update exceeds the threshold of defaultWindowSize/2, send a
		// standalone processing request with window update and clear the pending
		// update.
		if bodySize := int64(len(streamedResp.GetBody())); bodySize > 0 {
			if cs.pendingRespWindowUpdate.Add(bodySize) >= iextproc.DefaultFlowControlWindowSize/2 {
				if err := cs.sendClientWindowUpdate(); err != nil {
					return err
				}
			}
		}
		return proto.Unmarshal(streamedResp.GetBody(), msg)
	case <-cs.ctx.Done():
		return cs.streamError()
	case <-cs.procStreamFailed.Done():
		return cs.streamError()
	}
}

func (cs *clientStream) SendMsg(m any) error {
	s, err := cs.bypassProcStreamForClientMsg()
	if err != nil {
		return err
	}
	if s != nil {
		return s.SendMsg(m)
	}

	req, err := cs.marshalAndCreateBodyReq(m, true)
	if err != nil {
		return err
	}
	msg := m.(proto.Message)

	// Start request forwarding loop on the first send because we need the message
	// type to send the data to the dataplane server.
	if !cs.reqForwardingStarted {
		cs.reqForwardingStarted = true
		go cs.requestForwardingToDataplaneLoop(msg.ProtoReflect().Type())
	}

	// If the ExtProc server has already signaled end_of_stream, discard any
	// subsequent client messages.
	if cs.discardRequests.Load() {
		return nil
	}

	s, err = cs.sendClientReqToProcServer(req)
	if s != nil {
		return s.SendMsg(m)
	}
	return err
}

// recvFromDataplane receives directly from the dataplane stream. If the
// dataplane stream returns an error, it initiates and waits for response
// trailers.
func (cs *clientStream) recvFromDataplane(m any) error {
	if cs.procStreamFailed.HasFired() {
		return cs.procStreamErr.Load().(error)
	}
	s, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return err
	}
	if !cs.dataplaneRecvCalled.Load() {
		cs.dataplaneRecvCalled.Store(true)
	}
	if err := s.RecvMsg(m); err != nil {
		// If RecvMsg returns error, fail the RPC in case external processor stream
		// has failed. Otherwise process the trailers.
		if cs.procStreamFailed.HasFired() {
			return cs.procStreamErr.Load().(error)
		}
		cs.initiateResponseTrailerProcessing()
		// Wait for trailer processing to be done to make sure we are able to return
		// correct error and fail RPC if the trailer processing fails. This needs to
		// be done because Trailer() does not return an error. TODO: Verify if the
		// wait is acceptable.
		return cs.waitForTrailerProcessing(err)
	}
	return nil
}

// sendClientReqToProcServer attempts to send the given request for client to
// server msg to the external processor. If the processor stream is bypassed
// while waiting, it blocks until all queued messages are forwarded, and returns
// the dataplane stream so the caller can send the message directly to the
// dataplane stream instead.
func (cs *clientStream) sendClientReqToProcServer(req *v3procservicepb.ProcessingRequest) (grpc.ClientStream, error) {
	// Ignoring the failureMode as we will be sending the request to proc server.
	if !cs.ignoreFailureMode.Load() {
		cs.ignoreFailureMode.Store(true)
	}

	// Acquire flow control window for downstream to sidestream only if request
	// body is present.
	if bodysize := len(req.GetRequestBody().GetBody()); bodysize > 0 {
		if !cs.acquireDownstreamToSidestreamWindow(int64(bodysize)) {
			return cs.handleClientReqProcStreamFallback()
		}
	}

	select {
	case cs.procSendCh <- req:
		return nil, nil
	case <-cs.procStreamBypass.Done():
		return cs.handleClientReqProcStreamFallback()
	case <-cs.procStreamFailed.Done():
		return nil, cs.streamError()
	case <-cs.ctx.Done():
		return nil, cs.streamError()
	}
}

// handleClientReqProcStreamFallback handles processor stream bypass during
// client request processing. If the processor stream is bypassed, it blocks
// until all queued client messages are forwarded, and returns the dataplane
// stream so the caller can send directly to the dataplane stream. Otherwise, it
// returns the stream error.
func (cs *clientStream) handleClientReqProcStreamFallback() (grpc.ClientStream, error) {
	if cs.procStreamBypass.HasFired() {
		if cs.reqForwardingStarted {
			// When the drain is triggered, wait for all the queued requests to be sent
			// to dataplane server before forwarding current request directly to
			// dataplane server.
			if err := cs.waitChannel(cs.requestForwardLoopDoneCh); err != nil {
				return nil, err
			}
		}
		return cs.waitForDataplaneStream(cs.ctx)
	}
	return nil, cs.streamError()
}

// bypassProcStreamForClientMsg checks if the external processor should be
// bypassed for the current request message or CloseSend. If so, it returns the
// dataplane stream to be used directly. It blocks until all client messages
// received from the proc server are forwarded to ensure correct sequence. It
// also blocks to wait for stream creation if necessary. If the message needs to
// be sent to the external processor server, it returns (nil, nil).
func (cs *clientStream) bypassProcStreamForClientMsg() (grpc.ClientStream, error) {
	if cs.procStreamFailed.HasFired() {
		return nil, cs.procStreamErr.Load().(error)
	}

	bypass := cs.procStreamBypass.HasFired()
	if bypass && cs.reqForwardingStarted {
		// We started forwarding messages to the proc server and then it
		// initiated a drain. We must wait until all messages received from
		// the proc server are sent on the dataplane stream, before
		// unblocking the application's Send() or CloseSend(), to ensure
		// correct message sequencing.
		if err := cs.waitChannel(cs.requestForwardLoopDoneCh); err != nil {
			return nil, err
		}
	}

	if bypass || cs.config.processingModes.requestBodyMode == modeSkip {
		return cs.waitForDataplaneStream(cs.ctx)
	}

	return nil, nil // Do not bypass.
}

// responseForwardingToProcServerLoop continuously receives response messages
// from the underlying dataplane stream, marshals them, and forwards them to the
// external processor server via procSendCh.
func (cs *clientStream) responseForwardingToProcServerLoop(msgType protoreflect.MessageType) {
	defer func() {
		// Once the external processor server receive loop finishes, close the
		// buffer to signal end of processed responses.
		cs.waitChannel(cs.procRecvLoopDone.Done())
		cs.mutatedRespBuffer.Close()
	}()

	dataplaneStream, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return
	}

	for {
		// If the processor stream has closed or the receive loop finishes, we can
		// stop receiving messages in the background and Recv should now directly be
		// called from the cs.RecvMsg() function.
		if cs.procStreamBypass.HasFired() || cs.procRecvLoopDone.HasFired() {
			return
		}

		newMsg := msgType.New().Interface()
		if !cs.dataplaneRecvCalled.Load() {
			cs.dataplaneRecvCalled.Store(true)
		}
		if err := dataplaneStream.RecvMsg(newMsg); err != nil {
			cs.initiateResponseTrailerProcessing()
			return
		}

		req, err := cs.marshalAndCreateBodyReq(newMsg, false)
		if err != nil {
			cs.failProcStream(err)
			return
		}

		if !cs.ignoreFailureMode.Load() {
			cs.ignoreFailureMode.Store(true)
		}

		// Acquire upstream to sidestream window before sending the response body to
		// proc server.
		if bodySize := int64(len(req.GetResponseBody().GetBody())); bodySize > 0 {
			if !cs.acquireUpstreamToSidestreamWindow(bodySize) {
				// If proc stream is bypassed before acquiring positive window, wait for
				// external processor server receive loop to finish before pushing this
				// message on the mutatedRespBuffer to ensure correct order of messages.
				if cs.procStreamBypass.HasFired() {
					if err := cs.waitChannel(cs.procRecvLoopDone.Done()); err != nil {
						return
					}
					resp := &v3procservicepb.StreamedBodyResponse{Body: req.GetResponseBody().GetBody()}
					cs.mutatedRespBuffer.Put(resp)
				}
				return
			}
		}

		select {
		case cs.procSendCh <- req:
		case <-cs.procStreamBypass.Done():
			// If drain is triggered, wait for external processor server receive loop
			// to finish before pushing this message on the mutatedRespBuffer
			// to ensure correct order of messages.
			if err := cs.waitChannel(cs.procRecvLoopDone.Done()); err != nil {
				return
			}
			resp := &v3procservicepb.StreamedBodyResponse{Body: req.GetResponseBody().GetBody()}
			cs.mutatedRespBuffer.Put(resp)
			return
		case <-cs.procStreamFailed.Done():
			return
		case <-cs.ctx.Done():
			return
		}
	}
}

// requestForwardingToDataplaneLoop continuously consumes mutated request body
// from mutatedReqBuffer (populated by the external processor server),
// unmarshals them, and forwards the final messages to the backend server.
func (cs *clientStream) requestForwardingToDataplaneLoop(msgType protoreflect.MessageType) {
	defer close(cs.requestForwardLoopDoneCh)
	dataplaneStream, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return
	}
	for {
		select {
		case streamedResp, ok := <-cs.mutatedReqBuffer.Get():
			if !ok {
				return
			}
			cs.mutatedReqBuffer.Load()

			// As per gRFC A93, ignore `end_of_stream_without_message` if
			// `end_of_stream` is false.
			if streamedResp.GetEndOfStream() && streamedResp.GetEndOfStreamWithoutMessage() {
				cs.recordDuration(clientHalfCloseDurationMetric, &cs.clientHalfCloseStartTime)
				dataplaneStream.CloseSend()
				return
			}

			newMsg := msgType.New().Interface()
			if err := proto.Unmarshal(streamedResp.GetBody(), newMsg); err != nil {
				cs.failProcStream(err)
				return
			}
			if err := dataplaneStream.SendMsg(newMsg); err != nil {
				cs.cancelStream(err)
				return
			}
			// Update the pending window update after pulling from the buffer. If the
			// pending update exceeds the threshold of defaultWindowSize/2, send a
			// standalone processing request with window update and clear the pending
			// update.
			if bodySize := int64(len(streamedResp.GetBody())); bodySize > 0 {
				if cs.pendingReqWindowUpdate.Add(bodySize) >= iextproc.DefaultFlowControlWindowSize/2 {
					if err := cs.sendClientWindowUpdate(); err != nil {
						return
					}
				}
			}
			if streamedResp.GetEndOfStream() {
				cs.recordDuration(clientHalfCloseDurationMetric, &cs.clientHalfCloseStartTime)
				dataplaneStream.CloseSend()
				return
			}
		case <-cs.ctx.Done():
			return
		case <-cs.procStreamFailed.Done():
			return
		}
	}
}

// recvFromProcServerLoop continuously receives messages from the external
// processor server and redirects them according to the response type.
func (cs *clientStream) recvFromProcServerLoop(newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) {
	defer func() {
		cs.procRecvLoopDone.Fire()
		// Close mutatedReqBuffer to indicate completion of receiving
		// the mutated requests. Do not close mutatedResponseBuffer because we
		// might push the message that has been read when drain is triggered.
		cs.mutatedReqBuffer.Close()
	}()

	// If request header mode is send, the first response should be the mutation
	// for request header. Create the dataplane stream using the mutated header.
	if cs.config.processingModes.requestHeaderMode == modeSend {
		if !cs.processInitialHeaders(cs.ctx, newStream, opts) {
			return
		}
	}

	// If header mode is not send or if we receive the header and create the
	// dataplane stream, start receiving from the external processor server.
	for {
		resp, err := cs.procStream.Recv()
		if err != nil {
			cs.failProcStream(err)
			return
		}
		if resp.GetRequestDrain() {
			// Trigger the drain but continue receiving the drained messages until we
			// get io.EOF.
			cs.triggerBypass()
		}

		if resp.GetImmediateResponse() != nil {
			cs.handleImmediateResponse(resp.GetImmediateResponse(), newStream, opts)
			return
		}

		if windowUpdate := resp.GetServerWindowUpdate(); windowUpdate != nil {
			deltaDownstreamToSidestream := windowUpdate.GetWindowIncrementDownstreamToSidestream()
			if deltaDownstreamToSidestream != 0 {
				// Add the window update to the existing window.
				newQuota := cs.downstreamToSidestreamWindow.Add(deltaDownstreamToSidestream)
				previousQuota := newQuota - deltaDownstreamToSidestream
				// If the window turned from negative to positive, send a signal to the
				// `acquireDownstreamToSidestreamWindow` function to wake it up.
				if previousQuota <= 0 && newQuota > 0 {
					select {
					case cs.downstreamToSidestreamPositiveUpdateCh <- struct{}{}:
					default:
					}
				}
			}
			deltaUpstreamToSidestream := windowUpdate.GetWindowIncrementUpstreamToSidestream()
			if deltaUpstreamToSidestream != 0 {
				// Add the window update to the existing window.
				newQuota := cs.upstreamToSidestreamWindow.Add(deltaUpstreamToSidestream)
				previousQuota := newQuota - deltaUpstreamToSidestream
				// If the window turned from negative to positive, send a signal to the
				// `acquireUpstreamToSidestreamWindow` function to wake it up.
				if previousQuota <= 0 && newQuota > 0 {
					select {
					case cs.upstreamToSidestreamPositiveUpdateCh <- struct{}{}:
					default:
					}
				}
			}
		}

		switch {
		case resp.GetRequestBody() != nil:
			if cs.config.processingModes.requestBodyMode == modeSkip {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent request body when request body processing is disabled"))
				return
			}

			streamedResp, ok := cs.validateBodyResponse(resp.GetRequestBody())
			if !ok {
				return
			}
			if streamedResp.GetEndOfStream() {
				cs.discardRequests.Store(true)
			}
			cs.mutatedReqBuffer.Put(streamedResp)

		case resp.GetResponseBody() != nil:
			if cs.config.processingModes.responseBodyMode == modeSkip {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent response body when response body processing is disabled"))
				return
			}

			// If response headers have been sent and mutated response headers have
			// not been received before receiving the response body message, fail the
			// RPC.
			if cs.config.processingModes.responseHeaderMode == modeSend && !cs.responseHeadersReady.HasFired() {
				cs.failProcStream(fmt.Errorf("external processor sent response body before sending response headers"))
				return
			}

			// If mutated response trailers have been received before receiving the
			// response body message, fail the RPC.
			if cs.config.processingModes.responseTrailerMode == modeSend && cs.responseTrailerReady.HasFired() {
				cs.failProcStream(fmt.Errorf("external processor sent response body after response trailers were already processed"))
				return
			}

			streamedResp, ok := cs.validateBodyResponse(resp.GetResponseBody())
			if !ok {
				return
			}
			if streamedResp.GetEndOfStream() {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly set end of stream in response body mutation"))
				return
			}
			cs.mutatedRespBuffer.Put(streamedResp)

		case resp.GetResponseHeaders() != nil:
			if cs.config.processingModes.responseHeaderMode == modeSkip {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent response headers when response header processing is disabled"))
				return
			}
			if !cs.responseHeaderSent.Load() {
				cs.failProcStream(fmt.Errorf("external processor sent response headers before response headers were sent to it"))
				return
			}
			if cs.responseHeadersReady.HasFired() {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent duplicate response headers after response headers were already processed"))
				return
			}

			header := resp.GetResponseHeaders()
			// Check if the status in the header response is CONTINUE; if not, fail
			// the stream.
			if status := header.GetResponse().GetStatus(); status != v3procservicepb.CommonResponse_CONTINUE {
				cs.failProcStream(fmt.Errorf("external processor returned unexpected status %v for response headers, expected %v", status, v3procservicepb.CommonResponse_CONTINUE))
				return
			}
			if err = cs.applyMutations(header.GetResponse().GetHeaderMutation(), cs.responseHeader); err != nil {
				cs.failProcStream(err)
				return
			}
			// Signal that the response header is modified and ready to be sent to the
			// client, so that if there is any buffered response body, it can be sent
			// after the header.
			cs.fireResponseHeadersReady()

		case resp.GetResponseTrailers() != nil:
			if cs.config.processingModes.responseTrailerMode == modeSkip {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent response trailers when response trailer processing is disabled"))
				return
			}
			if !cs.trailerSent.Load() {
				cs.failProcStream(fmt.Errorf("external processor sent response trailers before response trailers were sent to it"))
				return
			}
			if cs.responseTrailerReady.HasFired() {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent duplicate response trailers after response trailers were already processed"))
				return
			}
			trailer := resp.GetResponseTrailers()
			if err = cs.applyMutations(trailer.GetHeaderMutation(), cs.responseTrailers); err != nil {
				cs.failProcStream(err)
				return
			}
			// Signal that the response trailer is modified and ready to be sent to
			// the client.
			cs.fireResponseTrailerReady()
		}
	}
}

func (cs *clientStream) validateBodyResponse(bodyResp *v3procservicepb.BodyResponse) (*v3procservicepb.StreamedBodyResponse, bool) {
	if status := bodyResp.GetResponse().GetStatus(); status != v3procservicepb.CommonResponse_CONTINUE {
		cs.failProcStream(fmt.Errorf("external processor returned unexpected status %v for body response, expected %v", status, v3procservicepb.CommonResponse_CONTINUE))
		return nil, false
	}
	streamedResp := bodyResp.GetResponse().GetBodyMutation().GetStreamedResponse()
	if streamedResp == nil {
		cs.failProcStream(fmt.Errorf("external processor returned invalid body mutation in body response"))
		return nil, false
	}
	if streamedResp.GetGrpcMessageCompressed() {
		cs.failProcStream(fmt.Errorf("external processor returned compressed grpc message which is not supported"))
		return nil, false
	}
	return streamedResp, true
}

func (cs *clientStream) applyMutations(mutation *v3procservicepb.HeaderMutation, md metadata.MD) error {
	if mutation == nil {
		return nil
	}
	if err := cs.config.mutationRules.ApplyAdditions(mutation.GetSetHeaders(), md); err != nil {
		return err
	}
	return cs.config.mutationRules.ApplyRemovals(mutation.GetRemoveHeaders(), md)
}

// closeProcSend gracefully half-closes the external processor stream across the
// background send goroutine by pushing a nil sentinel onto procSendCh.
func (cs *clientStream) closeProcSend() {
	select {
	case cs.procSendCh <- nil:
	case <-cs.ctx.Done():
	case <-cs.procStreamFailed.Done():
	case <-cs.procStreamBypass.Done():
	}
}

// sendToProcServer runs as a dedicated background goroutine that serializes all
// outbound messages to the external processor server. It listens on procSendCh
// for messages to forward. It actively monitors stream lifecycle events:
//   - Drain/Bypass: If procStreamBypass fires, it initiates a graceful shutdown
//     by sending a half-close (CloseSend) to the external processor server and
//     returning so that no more messages can be sent to the external processor.
//   - Cancellation/Failure: It aborts immediately if the context is canceled or
//     the underlying stream fails.
func (cs *clientStream) sendToProcServerLoop() {
	for {
		select {
		case req := <-cs.procSendCh:
			if req == nil {
				// A nil request is used as a sentinel message to gracefully half-close
				// the processor stream from this background thread. This ensures that
				// CloseSend is called sequentially after all preceding requests in the
				// channel are successfully sent, avoiding out-of-order data races.
				cs.procStream.CloseSend()
				return
			}
			if req.GetResponseHeaders() != nil {
				cs.responseHeaderSent.Store(true)
			}
			if req.GetResponseTrailers() != nil {
				cs.trailerSent.Store(true)
			}

			// Merge any pending window updates into the outgoing request. If the
			// request is already a standalone ClientWindowUpdate, we must accumulate
			// (add) the newly drained increments to any existing increments rather
			// than replacing the struct, ensuring no pending window quota is dropped.
			respInc := cs.pendingRespWindowUpdate.Swap(0)
			reqInc := cs.pendingReqWindowUpdate.Swap(0)
			if respInc > 0 || reqInc > 0 {
				if req.ClientWindowUpdate == nil {
					req.ClientWindowUpdate = &v3procservicepb.ProcessingRequest_ClientWindowUpdate{}
				}
				req.ClientWindowUpdate.WindowIncrementSidestreamToDownstream += respInc
				req.ClientWindowUpdate.WindowIncrementSidestreamToUpstream += reqInc
			}

			if err := cs.procStream.Send(req); err != nil {
				if err != io.EOF {
					// For non-EOF client-side send errors, fail the stream immediately.
					cs.failProcStream(err)
				}
				// If Send returned io.EOF, the server closed the stream early. Let
				// the Recv loop retrieve the actual status error from the server and
				// propagate it.
				return
			}
		case <-cs.procStreamBypass.Done():
			cs.procStream.CloseSend()
			return
		case <-cs.procStreamFailed.Done():
			return
		case <-cs.ctx.Done():
			return
		}
	}
}

// streamError returns the appropriate error when a stream operation is
// preempted by a stream failure or context cancellation. It prioritizes the
// external processor stream's terminal error over the context error.
func (cs *clientStream) streamError() error {
	if cs.procStreamFailed.HasFired() {
		return cs.procStreamErr.Load().(error)
	}
	return status.FromContextError(cs.ctx.Err()).Err()
}

// waitChannel waits for the provided channel to be closed, while also
// respecting context cancellation and stream failures.
func (cs *clientStream) waitChannel(ch <-chan struct{}) error {
	select {
	case <-ch:
		return nil
	case <-cs.ctx.Done():
		return cs.streamError()
	case <-cs.procStreamFailed.Done():
		return cs.streamError()
	}
}

// createDataplaneStream initializes the underlying gRPC dataplane stream using
// the provided newStream function. If dataplane stream creation fails, the
// top-level context used by the extproc filter is canceled and thereby all
// spawned goroutines terminate.
func (cs *clientStream) createDataplaneStream(ctx context.Context, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) error {
	defer close(cs.dataplaneSetup)
	cs.dataplaneStream, cs.dataplaneCreationErr = newStream(ctx, opts...)
	if cs.dataplaneCreationErr != nil {
		// Execute the OnFinish call options if the delegate failed.
		cs.callOnFinish(cs.dataplaneCreationErr)
		cs.cancel()
		return cs.dataplaneCreationErr
	}
	cs.recordMetric(clientHeadersDurationMetric, timeSince(cs.clientHeadersStartTime).Seconds())
	return nil
}

// failProcStream handles stream failures, recording errors or bypassing the
// external processor based on failureModeAllow configuration. It returns true
// if the stream is bypassed, and false if the connection is torn down.
func (cs *clientStream) failProcStream(err error) bool {
	if !cs.procStreamClosed.CompareAndSwap(false, true) {
		return cs.procStreamBypass.HasFired()
	}
	cs.procCancel()
	if err != io.EOF && (cs.ignoreFailureMode.Load() || !cs.config.failureModeAllow) {
		// Execute the OnFinish call options if the delegate is never called.
		if cs.dataplaneStream == nil {
			cs.callOnFinish(err)
		}
		cs.procStreamErr.Store(status.Errorf(codes.Internal, "extproc: %v", err))
		cs.procStreamFailed.Fire()
		cs.cleanupDataplane()
		return false
	}
	cs.triggerBypass()
	return true
}

// cancelStream immediately terminates the stream with the specified error and
// fires the failure event. It does not respect the failure mode.
func (cs *clientStream) cancelStream(err error) {
	if !cs.procStreamClosed.CompareAndSwap(false, true) {
		return
	}
	// Execute the OnFinish call options if the delegate is never called.
	if cs.dataplaneStream == nil {
		cs.callOnFinish(err)
	}
	cs.procStreamErr.Store(err)
	cs.procStreamFailed.Fire()
	cs.cleanupDataplane()
}

// handleHeaderError handles failures that occur during receiving or processing
// the initial request headers. If the failure mode allows it, the external
// processor is bypassed and the direct dataplane stream is created.
func (cs *clientStream) handleHeaderError(err error, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) {
	if cs.failProcStream(err) {
		cs.createDataplaneStream(cs.ctx, newStream, opts)
	}
}

// processInitialHeaders waits for the initial header response from the external
// processor server, applies any requested header mutations to the outgoing
// context, and initializes the data plane stream. It returns true if the
// headers were processed successfully and dataplane stream was created. It
// returns false if the proc stream needs to be bypassed, or failed due to an
// error, or aborted early by a processor-initiated immediate response.
func (cs *clientStream) processInitialHeaders(ctx context.Context, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) bool {
	resp, err := cs.procStream.Recv()
	if err != nil {
		cs.handleHeaderError(err, newStream, opts)
		return false
	}
	if resp.GetRequestDrain() {
		cs.triggerBypass()
	}
	if resp.GetImmediateResponse() != nil {
		cs.handleImmediateResponse(resp.GetImmediateResponse(), newStream, opts)
		return false
	}

	header := resp.GetRequestHeaders()
	if header == nil {
		err := fmt.Errorf("external processor returned an unexpected message type %T, expected request headers response", resp.GetResponse())
		cs.handleHeaderError(err, newStream, opts)
		return false
	}
	// Check if status in header response is CONTINUE; if not, fail the stream.
	if status := header.GetResponse().GetStatus(); status != v3procservicepb.CommonResponse_CONTINUE {
		cs.handleHeaderError(fmt.Errorf("external processor returned unexpected status %v for request headers, expected %v", status, v3procservicepb.CommonResponse_CONTINUE), newStream, opts)
		return false
	}
	// Mutate the outgoing headers with additions and removals received from the
	// external processor.
	outgoingMD, _ := metadata.FromOutgoingContext(ctx)
	if outgoingMD == nil {
		outgoingMD = metadata.MD{}
	}
	if err = cs.applyMutations(header.GetResponse().GetHeaderMutation(), outgoingMD); err != nil {
		cs.handleHeaderError(err, newStream, opts)
		return false
	}
	dataplaneCtx := metadata.NewOutgoingContext(ctx, outgoingMD)
	if err = cs.createDataplaneStream(dataplaneCtx, newStream, opts); err != nil {
		return false
	}
	return true
}

func (cs *clientStream) handleImmediateResponse(imm *v3procservicepb.ImmediateResponse, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) {
	if cs.config.disableImmediateResponse {
		err := fmt.Errorf("external processor sent an immediate response but immediate responses are disabled in configuration")
		if cs.dataplaneStream == nil {
			cs.handleHeaderError(err, newStream, opts)
		} else {
			cs.failProcStream(err)
		}
		return
	}

	statusCode := codes.Internal
	if imm.GetGrpcStatus() != nil {
		statusCode = codes.Code(imm.GetGrpcStatus().GetStatus())
		if statusCode > codes.Code(16) {
			statusCode = codes.Unknown
		}
	}
	err := status.Error(statusCode, imm.GetDetails())

	if cs.trailerSent.Load() {
		if mutation := imm.GetHeaders(); mutation != nil {
			cs.applyMutations(mutation, cs.responseTrailers)
		}
		cs.trailerErr.Store(err)
		cs.fireResponseTrailerReady()
	} else {
		cs.cancelStream(err)
	}
}

func (cs *clientStream) triggerBypass() {
	if cs.procStreamBypass.Fire() {
		cs.fireResponseHeadersReady()
		cs.fireResponseTrailerReady()
	}
}

// waitForDataplaneStream waits for the dataplane stream to be created or for
// the context to be done. It also checks if the processor stream has not ended
// abruptly with a non-io.EOF error.
func (cs *clientStream) waitForDataplaneStream(ctx context.Context) (grpc.ClientStream, error) {
	select {
	case <-cs.dataplaneSetup:
		if cs.dataplaneCreationErr != nil {
			return nil, cs.dataplaneCreationErr
		}
		return cs.dataplaneStream, nil
	case <-ctx.Done():
		// Prioritize returning the underlying dataplane creation failure or
		// external processor error over the context cancellation error.
		if cs.dataplaneCreationErr != nil {
			return nil, cs.dataplaneCreationErr
		}
		return nil, cs.streamError()
	case <-cs.procStreamFailed.Done():
		return nil, cs.procStreamErr.Load().(error)
	}
}

// initiateResponseHeaderProcessing receives initial response headers exactly
// once, forwards them to the external processor if configured, or unblocks
// readers immediately.
func (cs *clientStream) initiateResponseHeaderProcessing() error {
	if cs.procStreamFailed.HasFired() {
		return cs.procStreamErr.Load().(error)
	}
	if cs.responseHeaderOnce.Load() || !cs.responseHeaderOnce.CompareAndSwap(false, true) {
		return nil
	}
	dataplaneStream, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return err
	}
	header, err := dataplaneStream.Header()
	if err != nil {
		// If the processor stream failed first and canceled cs.ctx, prefer
		// that root error over the secondary dataplaneStream.Header() failure.
		if cs.procStreamFailed.HasFired() {
			return cs.procStreamErr.Load().(error)
		}
		return err
	}
	// Capture the start time for response headers after they have been
	// successfully retrieved from the dataplane stream.
	cs.serverHeadersStartTime.Store(int64(timeSince(cs.clientHeadersStartTime)))

	if header == nil {
		// A trailers-only response returns nil from dataplaneStream.Header().
		header = dataplaneStream.Trailer()
		if len(header) > 0 {
			cs.trailersOnly = true
			// For trailers-only, the trailers are retrieved during the header
			// phase. Start timing them now since they will be processed as part
			// of the header phase.
			cs.serverTrailersStartTime.Store(int64(timeSince(cs.clientHeadersStartTime)))
		}
	}
	cs.responseHeader = header
	if cs.config.processingModes.responseHeaderMode != modeSend || cs.procStreamBypass.HasFired() {
		// If header does not need to be sent to the external processor, unblock
		// the functions waiting on header modifications.
		cs.fireResponseHeadersReady()
		return nil
	}

	select {
	case cs.procSendCh <- cs.responseHeaders(header):
		return nil
	case <-cs.procStreamBypass.Done():
		return nil
	case <-cs.procStreamFailed.Done():
		return cs.streamError()
	case <-cs.ctx.Done():
		return cs.streamError()
	}
}

func (cs *clientStream) initiateResponseTrailerProcessing() {
	if cs.responseTrailerOnce.Load() || !cs.responseTrailerOnce.CompareAndSwap(false, true) {
		return
	}
	if cs.trailersOnly {
		// For trailers-only response, the trailers metadata map is processed and
		// mutated during the response header phase. Wait for those mutations to
		// be received and applied.
		select {
		case <-cs.responseHeadersReady.Done():
		case <-cs.procStreamFailed.Done():
		case <-cs.ctx.Done():
		}
		cs.responseTrailers = cs.responseHeader
		cs.fireResponseTrailerReady()
		// Gracefully half-close the external processor stream for trailers-only
		// responses once header modifications finish.
		cs.closeProcSend()
		return
	}
	cs.responseTrailers = cs.dataplaneStream.Trailer()
	// Capture the start time for response trailers after they have been
	// successfully retrieved from the dataplane stream.
	offset := timeSince(cs.clientHeadersStartTime)
	cs.serverTrailersStartTime.Store(int64(offset))

	if cs.config.processingModes.responseTrailerMode == modeSend && !cs.procStreamBypass.HasFired() {
		select {
		case cs.procSendCh <- cs.commonStream.responseTrailers(cs.responseTrailers):
		case <-cs.ctx.Done():
		case <-cs.procStreamFailed.Done():
		case <-cs.procStreamBypass.Done():
		}
	} else {
		cs.fireResponseTrailerReady()
	}
	// Gracefully half-close the external processor stream after forwarding response
	// trailers to signal that all responses have completely processed.
	cs.closeProcSend()
}

func (cs *clientStream) waitForTrailerProcessing(recvErr error) error {
	select {
	case <-cs.responseTrailerReady.Done():
		if val := cs.trailerErr.Load(); val != nil {
			return val.(error)
		}
		if cs.procStreamFailed.HasFired() {
			return cs.procStreamErr.Load().(error)
		}
		return recvErr
	case <-cs.procStreamBypass.Done():
		return recvErr
	case <-cs.procStreamFailed.Done():
		return cs.streamError()
	case <-cs.ctx.Done():
		return cs.streamError()
	}
}

// acquireDownstreamToSidestreamWindow checks available flow control window for
// downstream to sidestream. If window is positive, it deducts bodySize and
// returns true immediately. If window is <= 0, it blocks until a positive
// window update is received, or returns false if bypassed, failed, or context
// is done.
func (cs *clientStream) acquireDownstreamToSidestreamWindow(bodySize int64) bool {
	// Re-check the window in a loop. A signal on the buffered channel may be
	// stale if a previous caller acquired the positive window on the fast path
	// without draining the channel.
	for {
		// If enough window is available, deduct body size and return immediately.
		if cs.downstreamToSidestreamWindow.Load() > 0 {
			cs.downstreamToSidestreamWindow.Add(-bodySize)
			return true
		}
		// If window is not available, wait for the window to become positive or
		// return false if bypassed, failed, or context is done.
		select {
		case <-cs.downstreamToSidestreamPositiveUpdateCh:
			continue
		case <-cs.procStreamBypass.Done():
			return false
		case <-cs.procStreamFailed.Done():
			return false
		case <-cs.ctx.Done():
			return false
		}
	}
}

// acquireUpstreamToSidestreamWindow checks available flow control window for
// upstream to sidestream. If window is positive, it deducts bodySize and
// returns true immediately. If window is <= 0, it blocks until a positive
// window update is received, or returns false if bypassed, failed, or context
// is done.
func (cs *clientStream) acquireUpstreamToSidestreamWindow(bodySize int64) bool {
	// Re-check the window in a loop. A signal on the buffered channel may be
	// stale if a previous caller acquired the positive window on the fast path
	// without draining the channel.
	for {
		if cs.upstreamToSidestreamWindow.Load() > 0 {
			cs.upstreamToSidestreamWindow.Add(-bodySize)
			return true
		}
		select {
		case <-cs.upstreamToSidestreamPositiveUpdateCh:
			continue
		case <-cs.procStreamBypass.Done():
			return false
		case <-cs.procStreamFailed.Done():
			return false
		case <-cs.ctx.Done():
			return false
		}
	}
}

// sendClientWindowUpdate pushes a standalone ClientWindowUpdate
// ProcessingRequest to procSendCh to replenish the sidestream to downstream and
// sidestream to upstream flow control windows.
func (cs *clientStream) sendClientWindowUpdate() error {
	respInc := cs.pendingRespWindowUpdate.Swap(0)
	reqInc := cs.pendingReqWindowUpdate.Swap(0)
	if respInc == 0 && reqInc == 0 {
		return nil
	}

	req := &v3procservicepb.ProcessingRequest{
		ClientWindowUpdate: &v3procservicepb.ProcessingRequest_ClientWindowUpdate{
			WindowIncrementSidestreamToDownstream: respInc,
			WindowIncrementSidestreamToUpstream:   reqInc,
		},
	}

	select {
	case cs.procSendCh <- req:
		return nil
	case <-cs.procStreamBypass.Done():
		return nil
	case <-cs.procStreamFailed.Done():
		return cs.streamError()
	case <-cs.ctx.Done():
		return cs.streamError()
	}
}

// convertBodyMode converts the body mode from processingMode to
// v3procfilterpb.ProcessingMode_BodySendMode.
func convertBodyMode(mode processingMode) v3procfilterpb.ProcessingMode_BodySendMode {
	if mode == modeSend {
		return v3procfilterpb.ProcessingMode_GRPC
	}
	return v3procfilterpb.ProcessingMode_NONE
}

func getHeader(md metadata.MD, added [][]string, key string) string {
	var vals []string
	if md != nil {
		vals = append(vals, md.Get(key)...)
	}
	for _, kvs := range added {
		for i := 0; i < len(kvs); i += 2 {
			if strings.EqualFold(kvs[i], key) {
				vals = append(vals, kvs[i+1])
			}
		}
	}
	return strings.Join(vals, ",")
}

// constructRequestAttributes builds a map of request attributes specified in
// the interceptor config.
func constructRequestAttributes(rpcInfo resolver.RPCInfo, md metadata.MD, added [][]string, requestedAttributes []string) map[string]*structpb.Struct {
	if len(requestedAttributes) == 0 {
		return nil
	}

	reqFields := make(map[string]any)
	for _, attr := range requestedAttributes {
		switch attr {
		case "request.path", "request.url_path":
			reqFields[attr] = rpcInfo.Method
		case "request.host":
			reqFields[attr] = rpcInfo.Authority
		case "request.method":
			reqFields[attr] = "POST"
		case "request.headers":
			headers := make(map[string]any)
			for k, values := range md {
				headers[strings.ToLower(k)] = strings.Join(values, ",")
			}
			for _, kvs := range added {
				for i := 0; i < len(kvs); i += 2 {
					k := strings.ToLower(kvs[i])
					v := kvs[i+1]
					if prev, ok := headers[k].(string); ok {
						headers[k] = prev + "," + v
					} else {
						headers[k] = v
					}
				}
			}
			reqFields[attr] = headers
		case "request.referer":
			if val := getHeader(md, added, "referer"); val != "" {
				reqFields[attr] = val
			}
		case "request.useragent":
			if val := getHeader(md, added, "user-agent"); val != "" {
				reqFields[attr] = val
			}
		case "request.id":
			if val := getHeader(md, added, "x-request-id"); val != "" {
				reqFields[attr] = val
			}
		case "request.query":
			reqFields[attr] = ""
		default:
			// Unknown/unsupported attributes are ignored.
		}
	}

	reqStruct := &structpb.Struct{Fields: make(map[string]*structpb.Value)}
	for k, v := range reqFields {
		val, err := structpb.NewValue(v)
		if err != nil {
			// Skip values that cannot be represented in a structpb.Value to
			// match Envoy's behavior of encoding as many attributes as possible.
			continue
		}
		reqStruct.Fields[k] = val
	}

	if len(reqStruct.Fields) == 0 {
		return nil
	}

	return map[string]*structpb.Struct{
		"envoy.filters.http.ext_proc": reqStruct,
	}
}
