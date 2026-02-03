// Copyright 2025 Google LLC
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

package storage

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"cloud.google.com/go/storage/internal/apiv2/storagepb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	gax "github.com/googleapis/gax-go/v2"
)

const (
	mrdCommandChannelSize  = 1
	mrdResponseChannelSize = 100
	// This should never be hit in practice, but is a safety valve to prevent
	// unbounded memory usage if the user is adding ranges faster than they
	// can be processed.
	mrdAddInternalQueueMaxSize = 50000
)

// --- internalMultiRangeDownloader Interface ---
// This provides an internal wrapper for the gRPC methods to avoid polluting
// reader.go with gRPC implementation details. The only implementation
// currently is for the gRPC transport with bidi APIs enabled. Creating
// a MultiRangeDownloader with any other client type will fail.
type internalMultiRangeDownloader interface {
	add(output io.Writer, offset, length int64, callback func(int64, int64, error))
	close(err error) error
	wait()
	getHandle() []byte
	getPermanentError() error
	getSpanCtx() context.Context
}

// --- grpcStorageClient method ---
// Top level entry point into the MultiRangeDownloader via the storageClient interface.
func (c *grpcStorageClient) NewMultiRangeDownloader(ctx context.Context, params *newMultiRangeDownloaderParams, opts ...storageOption) (*MultiRangeDownloader, error) {
	if !c.config.grpcBidiReads {
		return nil, errors.New("storage: MultiRangeDownloader requires the experimental.WithGRPCBidiReads option")
	}
	s := callSettings(c.settings, opts...)
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}
	if s.retry == nil {
		s.retry = defaultRetry
	}

	b := bucketResourceName(globalProjectAlias, params.bucket)
	readSpec := &storagepb.BidiReadObjectSpec{
		Bucket:                    b,
		Object:                    params.object,
		CommonObjectRequestParams: toProtoCommonObjectRequestParams(params.encryptionKey),
	}
	if params.gen >= 0 {
		readSpec.Generation = params.gen
	}
	if params.handle != nil && len(*params.handle) > 0 {
		readSpec.ReadHandle = &storagepb.BidiReadHandle{
			Handle: *params.handle,
		}
	}

	mCtx, cancel := context.WithCancel(ctx)

	// Create the manager
	manager := &multiRangeDownloaderManager{
		ctx:            mCtx,
		cancel:         cancel,
		client:         c,
		settings:       s,
		params:         params,
		cmds:           make(chan mrdCommand, mrdCommandChannelSize),
		sessionResps:   make(chan mrdSessionResult, mrdResponseChannelSize),
		pendingRanges:  make(map[int64]*rangeRequest),
		readIDCounter:  1,
		readSpec:       readSpec,
		attrsReady:     make(chan struct{}),
		spanCtx:        ctx,
		unsentRequests: newRequestQueue(),
	}

	mrd := &MultiRangeDownloader{
		impl: manager,
	}

	manager.wg.Add(1)
	go func() {
		defer manager.wg.Done()
		manager.eventLoop()
	}()

	// Wait for attributes to be ready
	select {
	case <-manager.attrsReady:
		if manager.permanentErr != nil {
			cancel()
			manager.wg.Wait()
			return nil, manager.permanentErr
		}
		if manager.attrs != nil {
			mrd.Attrs = *manager.attrs
		}
		return mrd, nil
	case <-ctx.Done():
		cancel()
		manager.wg.Wait()
		return nil, ctx.Err()
	}
}

// --- mrdCommand Interface and Implementations ---
// Used to pass commands from the user-facing code to the MRD manager.
// mrdCommand handlers are applied sequentially in the event loop. Therefore, it's okay
// for them to read/modify the manager state without concern for thread safety.
type mrdCommand interface {
	apply(ctx context.Context, m *multiRangeDownloaderManager)
}
type mrdAddCmd struct {
	output   io.Writer
	offset   int64
	length   int64
	callback func(int64, int64, error)
}

func (c *mrdAddCmd) apply(ctx context.Context, m *multiRangeDownloaderManager) {
	m.handleAddCmd(ctx, c)
}

type mrdCloseCmd struct {
	err error
}

func (c *mrdCloseCmd) apply(ctx context.Context, m *multiRangeDownloaderManager) {
	m.handleCloseCmd(ctx, c)
}

type mrdWaitCmd struct {
	doneC chan struct{}
}

func (c *mrdWaitCmd) apply(ctx context.Context, m *multiRangeDownloaderManager) {
	m.handleWaitCmd(ctx, c)
}

type mrdGetHandleCmd struct {
	respC chan []byte
}

func (c *mrdGetHandleCmd) apply(ctx context.Context, m *multiRangeDownloaderManager) {
	select {
	case <-m.attrsReady:
		select {
		case c.respC <- m.lastReadHandle:
		case <-m.ctx.Done():
			close(c.respC)
		}
	case <-m.ctx.Done():
		close(c.respC)
	}
}

type mrdErrorCmd struct {
	respC chan error
}

func (c *mrdErrorCmd) apply(ctx context.Context, m *multiRangeDownloaderManager) {
	select {
	case c.respC <- m.permanentErr:
	case <-ctx.Done():
		close(c.respC)
	}
}

// --- mrdSessionResult ---
// This is used to pass the zero-copy decoded response from the recv stream
// back up to the multiRangeDownloadManager for processing, or to pass
// an error if the session failed.
type mrdSessionResult struct {
	decoder  *readResponseDecoder
	err      error
	redirect *storagepb.BidiReadObjectRedirectedError
}

var errClosed = errors.New("downloader closed")

// --- multiRangeDownloaderManager ---
// Manages main event loop for MRD commands and processing responses.
// Spawns bidiStreamSession to deal with actual stream management, retries, etc.
type multiRangeDownloaderManager struct {
	ctx          context.Context
	cancel       context.CancelFunc
	client       *grpcStorageClient
	settings     *settings
	params       *newMultiRangeDownloaderParams
	wg           sync.WaitGroup // syncs completion of event loop.
	cmds         chan mrdCommand
	sessionResps chan mrdSessionResult

	// State
	currentSession *bidiReadStreamSession
	readIDCounter  int64
	pendingRanges  map[int64]*rangeRequest
	permanentErr   error
	waiters        []chan struct{}
	readSpec       *storagepb.BidiReadObjectSpec
	lastReadHandle []byte
	attrs          *ReaderObjectAttrs
	attrsReady     chan struct{}
	attrsOnce      sync.Once
	spanCtx        context.Context
	callbackWg     sync.WaitGroup
	unsentRequests *requestQueue
}

type rangeRequest struct {
	output   io.Writer
	offset   int64
	length   int64
	callback func(int64, int64, error)

	origOffset int64
	origLength int64

	readID       int64
	bytesWritten int64
	completed    bool
}

// Methods implementing internalMultiRangeDownloader
func (m *multiRangeDownloaderManager) add(output io.Writer, offset, length int64, callback func(int64, int64, error)) {
	if err := m.ctx.Err(); err != nil {
		if m.permanentErr != nil {
			err = m.permanentErr
		}
		m.runCallback(offset, length, err, callback)
		return
	}
	if length < 0 {
		m.runCallback(offset, length, fmt.Errorf("storage: MultiRangeDownloader.Add limit cannot be negative"), callback)
		return
	}

	cmd := &mrdAddCmd{output: output, offset: offset, length: length, callback: callback}
	select {
	case m.cmds <- cmd:
	case <-m.ctx.Done():
		err := m.ctx.Err()
		if m.permanentErr != nil {
			err = m.permanentErr
		}
		m.runCallback(offset, length, err, callback)
	}
}

func (m *multiRangeDownloaderManager) close(err error) error {
	cmd := &mrdCloseCmd{err: err}
	select {
	case m.cmds <- cmd:
		<-m.ctx.Done()
		m.wg.Wait()
		if m.permanentErr != nil && !errors.Is(m.permanentErr, errClosed) {
			return m.permanentErr
		}
		return nil
	case <-m.ctx.Done():
		m.wg.Wait()
		return m.ctx.Err()
	}
}

func (m *multiRangeDownloaderManager) wait() {
	doneC := make(chan struct{})
	cmd := &mrdWaitCmd{doneC: doneC}
	select {
	case m.cmds <- cmd:
		select {
		case <-doneC:
			m.callbackWg.Wait()
			return
		case <-m.ctx.Done():
			m.callbackWg.Wait()
			return
		}
	case <-m.ctx.Done():
		m.callbackWg.Wait()
		return
	}
}

func (m *multiRangeDownloaderManager) getHandle() []byte {
	select {
	case <-m.attrsReady:
	case <-m.ctx.Done():
		return nil
	}

	respC := make(chan []byte, 1)
	cmd := &mrdGetHandleCmd{respC: respC}
	select {
	case m.cmds <- cmd:
		select {
		case h, ok := <-respC:
			if !ok {
				return nil
			}
			return h
		case <-m.ctx.Done():
			return nil
		}
	case <-m.ctx.Done():
		return nil
	}
}

func (m *multiRangeDownloaderManager) getPermanentError() error {
	return m.permanentErr
}

func (m *multiRangeDownloaderManager) getSpanCtx() context.Context {
	return m.spanCtx
}

func (m *multiRangeDownloaderManager) runCallback(origOffset, numBytes int64, err error, cb func(int64, int64, error)) {
	m.callbackWg.Add(1)
	go func() {
		defer m.callbackWg.Done()
		cb(origOffset, numBytes, err)
	}()
}

func (m *multiRangeDownloaderManager) eventLoop() {
	defer func() {
		if m.currentSession != nil {
			m.currentSession.Shutdown()
		}
		finalErr := m.permanentErr
		if finalErr == nil {
			if ctxErr := m.ctx.Err(); ctxErr != nil {
				finalErr = ctxErr
			}
		}
		if finalErr == nil {
			finalErr = errClosed
		}
		m.failAllPending(finalErr)
		for _, waiter := range m.waiters {
			close(waiter)
		}
		m.attrsOnce.Do(func() { close(m.attrsReady) })
		m.callbackWg.Wait()
	}()

	// Blocking call to establish the first session and get attributes.
	if err := m.establishInitialSession(); err != nil {
		// permanentErr is set within establishInitialSession if necessary.
		return // Exit eventLoop if we can't start.
	}

	for {
		var nextReq *storagepb.BidiReadObjectRequest
		var targetChan chan<- *storagepb.BidiReadObjectRequest

		// Only try to send if we have queued requests
		if m.unsentRequests.Len() > 0 && m.currentSession != nil {
			nextReq = m.unsentRequests.Front()
			if nextReq != nil {
				targetChan = m.currentSession.reqC
			}
		}
		// Only read from cmds if we have space in the unsentRequests queue.
		var cmdsChan chan mrdCommand
		if m.unsentRequests.Len() < mrdAddInternalQueueMaxSize {
			cmdsChan = m.cmds
		}
		select {
		case <-m.ctx.Done():
			return
		// This path only triggers if space is available in the channel.
		// It never blocks the eventLoop.
		case targetChan <- nextReq:
			m.unsentRequests.RemoveFront()
		case cmd := <-cmdsChan:
			cmd.apply(m.ctx, m)
			if _, ok := cmd.(*mrdCloseCmd); ok {
				return
			}
		case result := <-m.sessionResps:
			m.processSessionResult(result)
		}

		if len(m.pendingRanges) == 0 && m.unsentRequests.Len() == 0 {
			for _, waiter := range m.waiters {
				close(waiter)
			}
			m.waiters = nil
		}
	}
}

func (m *multiRangeDownloaderManager) establishInitialSession() error {
	retry := m.settings.retry

	var firstResult mrdSessionResult

	openStreamAndReceiveFirst := func(ctx context.Context, spec *storagepb.BidiReadObjectSpec) (*bidiReadStreamSession, mrdSessionResult) {
		session, err := newBidiReadStreamSession(m.ctx, m.sessionResps, m.client, m.settings, m.params, spec)
		if err != nil {
			return nil, mrdSessionResult{err: err}
		}

		select {
		case result := <-m.sessionResps:
			return session, result
		case <-ctx.Done():
			session.Shutdown()
			return nil, mrdSessionResult{err: ctx.Err()}
		}
	}

	err := run(m.ctx, func(ctx context.Context) error {
		if m.currentSession != nil {
			m.currentSession.Shutdown()
			m.currentSession = nil
		}

		currentSpec := proto.Clone(m.readSpec).(*storagepb.BidiReadObjectSpec)
		session, result := openStreamAndReceiveFirst(ctx, currentSpec)

		if result.err != nil {
			if result.redirect != nil {
				m.readSpec.RoutingToken = result.redirect.RoutingToken
				m.readSpec.ReadHandle = result.redirect.ReadHandle
				if session != nil {
					session.Shutdown()
				}

				// We might get a redirect error here for an out-of-region request.
				// Add the routing token and read handle to the request and do one
				// retry.
				currentSpec = proto.Clone(m.readSpec).(*storagepb.BidiReadObjectSpec)
				session, result = openStreamAndReceiveFirst(ctx, currentSpec)

				if result.err != nil {
					if session != nil {
						session.Shutdown()
					}
					return result.err
				}
			} else {
				// Not a redirect error, return to run()
				if session != nil {
					session.Shutdown()
				}
				return result.err
			}
		}

		// Success
		m.currentSession = session
		firstResult = result
		return nil
	}, retry, true)

	if err != nil {
		m.setPermanentError(err)
		return m.permanentErr
	}

	// Process the successful first result
	m.processSessionResult(firstResult)
	if m.permanentErr != nil {
		return m.permanentErr
	}
	return nil
}

func (m *multiRangeDownloaderManager) handleAddCmd(ctx context.Context, cmd *mrdAddCmd) {
	if m.permanentErr != nil {
		m.runCallback(cmd.offset, cmd.length, m.permanentErr, cmd.callback)
		return
	}

	req := &rangeRequest{
		output:     cmd.output,
		offset:     cmd.offset,
		length:     cmd.length,
		origOffset: cmd.offset,
		origLength: cmd.length,
		callback:   cmd.callback,
		readID:     m.readIDCounter,
	}
	m.readIDCounter++

	// Convert to positive offset only if attributes are available.
	if m.attrs != nil && req.offset < 0 {
		err := m.convertToPositiveOffset(req)
		if err != nil {
			return
		}
	}

	if m.currentSession == nil {
		// This should not happen if establishInitialSession was successful
		m.failRange(req, errors.New("storage: session not available"))
		return
	}

	m.pendingRanges[req.readID] = req

	protoReq := &storagepb.BidiReadObjectRequest{
		ReadRanges: []*storagepb.ReadRange{{
			ReadOffset: req.offset,
			ReadLength: req.length,
			ReadId:     req.readID,
		}},
	}
	m.unsentRequests.PushBack(protoReq)
}

func (m *multiRangeDownloaderManager) convertToPositiveOffset(req *rangeRequest) error {
	if req.offset >= 0 {
		return nil
	}
	var objSize int64
	if m.attrs != nil {
		objSize = m.attrs.Size
	}
	if objSize <= 0 {
		err := errors.New("storage: cannot resolve negative offset with object size as 0")
		m.failRange(req, err)
		return err
	}
	start := max(objSize+req.offset, 0)
	req.offset = start
	if req.length == 0 {
		req.length = objSize - start
	}
	return nil
}

func (m *multiRangeDownloaderManager) handleCloseCmd(ctx context.Context, cmd *mrdCloseCmd) {
	var err error
	if cmd.err != nil {
		err = cmd.err
	} else {
		err = errClosed

	}
	m.setPermanentError(err)
	m.cancel()
}

func (m *multiRangeDownloaderManager) handleWaitCmd(ctx context.Context, cmd *mrdWaitCmd) {
	if len(m.pendingRanges) == 0 {
		close(cmd.doneC)
	} else {
		m.waiters = append(m.waiters, cmd.doneC)
	}
}

func (m *multiRangeDownloaderManager) processSessionResult(result mrdSessionResult) {
	if result.err != nil {
		m.handleStreamEnd(result)
		return
	}

	resp := result.decoder.msg
	if handle := resp.GetReadHandle().GetHandle(); len(handle) > 0 {
		m.lastReadHandle = handle
	}

	m.attrsOnce.Do(func() {
		defer close(m.attrsReady)
		if meta := resp.GetMetadata(); meta != nil {
			obj := newObjectFromProto(meta)
			attrs := readerAttrsFromObject(obj)
			m.attrs = &attrs
			for _, req := range m.pendingRanges {
				if req.offset < 0 {
					_ = m.convertToPositiveOffset(req)
				}
			}
		}
	})

	for _, dataRange := range resp.GetObjectDataRanges() {
		readID := dataRange.GetReadRange().GetReadId()
		req, exists := m.pendingRanges[readID]
		if !exists || req.completed {
			continue
		}
		written, _, err := result.decoder.writeToAndUpdateCRC(req.output, readID, nil)
		req.bytesWritten += written
		if err != nil {
			m.failRange(req, err)
			continue
		}

		if dataRange.GetRangeEnd() {
			req.completed = true
			delete(m.pendingRanges, req.readID)
			m.runCallback(req.origOffset, req.bytesWritten, nil, req.callback)
		}
	}
	// Once all data in the initial response has been read out, free buffers.
	result.decoder.databufs.Free()
}

// ensureSession is now only for reconnecting *after* the initial session is up.
func (m *multiRangeDownloaderManager) ensureSession(ctx context.Context) error {
	if m.currentSession != nil {
		return nil
	}
	if m.permanentErr != nil {
		return m.permanentErr
	}

	// Using run for retries
	return run(ctx, func(ctx context.Context) error {
		if m.currentSession != nil {
			return nil
		}
		if m.permanentErr != nil {
			return m.permanentErr
		}

		session, err := newBidiReadStreamSession(m.ctx, m.sessionResps, m.client, m.settings, m.params, proto.Clone(m.readSpec).(*storagepb.BidiReadObjectSpec))
		if err != nil {
			redirectErr, isRedirect := isRedirectError(err)
			if isRedirect {
				m.readSpec.RoutingToken = redirectErr.RoutingToken
				m.readSpec.ReadHandle = redirectErr.ReadHandle
				return fmt.Errorf("%w: %v", errBidiReadRedirect, err)
			}
			return err
		}
		m.currentSession = session

		var rangesToResend []*storagepb.ReadRange
		for _, req := range m.pendingRanges {
			if !req.completed {
				readLength := req.length
				if req.length > 0 {
					readLength -= req.bytesWritten
				}
				if readLength < 0 {
					readLength = 0
				}

				if req.length == 0 || readLength > 0 {
					rangesToResend = append(rangesToResend, &storagepb.ReadRange{
						ReadOffset: req.offset + req.bytesWritten,
						ReadLength: readLength,
						ReadId:     req.readID,
					})
				}
			}
		}
		if len(rangesToResend) > 0 {
			retryReq := &storagepb.BidiReadObjectRequest{ReadRanges: rangesToResend}
			m.unsentRequests.PushFront(retryReq)
		}
		return nil
	}, m.settings.retry, true)
}

var errBidiReadRedirect = errors.New("bidi read object redirected")

func (m *multiRangeDownloaderManager) handleStreamEnd(result mrdSessionResult) {
	if m.currentSession != nil {
		m.currentSession.Shutdown()
		m.currentSession = nil
	}
	err := result.err
	var ensureErr error

	if result.redirect != nil {
		m.readSpec.RoutingToken = result.redirect.RoutingToken
		m.readSpec.ReadHandle = result.redirect.ReadHandle
		ensureErr = m.ensureSession(m.ctx)
	} else if m.settings.retry != nil && m.settings.retry.runShouldRetry(err) {
		ensureErr = m.ensureSession(m.ctx)
	} else {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, errClosed) {
			m.setPermanentError(err)
		} else if m.permanentErr == nil {
			m.setPermanentError(errClosed)
		}
		m.failAllPending(m.permanentErr)
	}

	// Handle error from ensureSession.
	if ensureErr != nil {
		m.setPermanentError(ensureErr)
		m.failAllPending(m.permanentErr)
	}
}

func (m *multiRangeDownloaderManager) failRange(req *rangeRequest, err error) {
	if req.completed {
		return
	}
	req.completed = true
	delete(m.pendingRanges, req.readID)
	m.runCallback(req.origOffset, req.bytesWritten, err, req.callback)
}

func (m *multiRangeDownloaderManager) failAllPending(err error) {
	for _, req := range m.pendingRanges {
		if !req.completed {
			req.completed = true
			m.runCallback(req.origOffset, req.bytesWritten, err, req.callback)
		}
	}
	m.pendingRanges = make(map[int64]*rangeRequest)
}

// Set permanent error to the provided error, if it hasn't been set already.
func (m *multiRangeDownloaderManager) setPermanentError(err error) {
	if m.permanentErr == nil {
		m.permanentErr = err
	}
}

// --- bidiReadStreamSession ---
// Controls lifespan of an individual bi-directional gRPC stream to the
// object in GCS. Spins up goroutines for the read and write sides of the
// stream.
type bidiReadStreamSession struct {
	ctx    context.Context
	cancel context.CancelFunc

	stream   storagepb.Storage_BidiReadObjectClient
	client   *grpcStorageClient
	settings *settings
	params   *newMultiRangeDownloaderParams
	readSpec *storagepb.BidiReadObjectSpec

	reqC  chan *storagepb.BidiReadObjectRequest
	respC chan<- mrdSessionResult
	wg    sync.WaitGroup

	errOnce   sync.Once
	streamErr error
}

func newBidiReadStreamSession(ctx context.Context, respC chan<- mrdSessionResult, client *grpcStorageClient, settings *settings, params *newMultiRangeDownloaderParams, readSpec *storagepb.BidiReadObjectSpec) (*bidiReadStreamSession, error) {
	sCtx, cancel := context.WithCancel(ctx)

	s := &bidiReadStreamSession{
		ctx:      sCtx,
		cancel:   cancel,
		client:   client,
		settings: settings,
		params:   params,
		readSpec: readSpec,
		reqC:     make(chan *storagepb.BidiReadObjectRequest, 100),
		respC:    respC,
	}

	initialReq := &storagepb.BidiReadObjectRequest{
		ReadObjectSpec: s.readSpec,
	}
	reqCtx := gax.InsertMetadataIntoOutgoingContext(s.ctx, contextMetadataFromBidiReadObject(initialReq)...)
	// Force the use of the custom codec to enable zero-copy reads.
	s.settings.gax = append(s.settings.gax, gax.WithGRPCOptions(
		grpc.ForceCodecV2(bytesCodecV2{}),
	))

	var err error
	s.stream, err = client.raw.BidiReadObject(reqCtx, s.settings.gax...)
	if err != nil {
		cancel()
		return nil, err
	}

	if err := s.stream.Send(initialReq); err != nil {
		s.stream.CloseSend()
		cancel()
		return nil, err
	}

	s.wg.Add(2)
	go s.sendLoop()
	go s.receiveLoop()

	go func() {
		s.wg.Wait()
		s.cancel()
	}()

	return s, nil
}
func (s *bidiReadStreamSession) SendRequest(req *storagepb.BidiReadObjectRequest) {
	select {
	case s.reqC <- req:
	case <-s.ctx.Done():
	}
}
func (s *bidiReadStreamSession) Shutdown() {
	s.cancel()
	s.wg.Wait()
}
func (s *bidiReadStreamSession) setError(err error) {
	s.errOnce.Do(func() {
		s.streamErr = err
	})
}
func (s *bidiReadStreamSession) sendLoop() {
	defer s.wg.Done()
	defer s.stream.CloseSend()
	for {
		select {
		case req, ok := <-s.reqC:
			if !ok {
				return
			}
			if err := s.stream.Send(req); err != nil {
				s.setError(err)
				s.cancel()
				return
			}
		case <-s.ctx.Done():
			return
		}
	}
}
func (s *bidiReadStreamSession) receiveLoop() {
	defer s.wg.Done()
	defer s.cancel()
	for {
		if err := s.ctx.Err(); err != nil {
			return
		}

		// Receive message without a copy.
		databufs := mem.BufferSlice{}
		err := s.stream.RecvMsg(&databufs)
		var decoder *readResponseDecoder
		if err == nil {
			// Use the custom decoder to parse the raw buffer without copying object data.
			decoder = &readResponseDecoder{
				databufs: databufs,
			}
			err = decoder.readFullObjectResponse()
		}

		if err != nil {
			databufs.Free()
			redirectErr, isRedirect := isRedirectError(err)
			result := mrdSessionResult{err: err}
			if isRedirect {
				result.redirect = redirectErr
				err = fmt.Errorf("%w: %v", errBidiReadRedirect, err)
				result.err = err
			}
			s.setError(err)

			select {
			case s.respC <- result:
			case <-s.ctx.Done():
			}
			return
		}

		select {
		case s.respC <- mrdSessionResult{decoder: decoder}:
		case <-s.ctx.Done():
			return
		}
	}
}
func isRedirectError(err error) (*storagepb.BidiReadObjectRedirectedError, bool) {
	st, ok := status.FromError(err)
	if !ok {
		return nil, false
	}
	if st.Code() != codes.Aborted {
		return nil, false
	}
	for _, d := range st.Details() {
		if bidiError, ok := d.(*storagepb.BidiReadObjectRedirectedError); ok {
			if bidiError.RoutingToken != nil {
				return bidiError, true
			}
		}
	}
	return nil, false
}

func readerAttrsFromObject(o *ObjectAttrs) ReaderObjectAttrs {
	if o == nil {
		return ReaderObjectAttrs{}
	}
	return ReaderObjectAttrs{
		Size:            o.Size,
		ContentType:     o.ContentType,
		ContentEncoding: o.ContentEncoding,
		CacheControl:    o.CacheControl,
		LastModified:    o.Updated,
		Generation:      o.Generation,
		Metageneration:  o.Metageneration,
		CRC32C:          o.CRC32C,
	}
}

type requestQueue struct {
	l *list.List
}

func newRequestQueue() *requestQueue {
	return &requestQueue{l: list.New()}
}

func (q *requestQueue) PushBack(r *storagepb.BidiReadObjectRequest)  { q.l.PushBack(r) }
func (q *requestQueue) PushFront(r *storagepb.BidiReadObjectRequest) { q.l.PushFront(r) }
func (q *requestQueue) Len() int                                     { return q.l.Len() }

func (q *requestQueue) Front() *storagepb.BidiReadObjectRequest {
	if f := q.l.Front(); f != nil {
		return f.Value.(*storagepb.BidiReadObjectRequest)
	}
	return nil
}

func (q *requestQueue) RemoveFront() {
	if f := q.l.Front(); f != nil {
		q.l.Remove(f)
	}
}
