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
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	gapic "cloud.google.com/go/storage/internal/apiv2"
	"cloud.google.com/go/storage/internal/apiv2/storagepb"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	// defaultWriteChunkRetryDeadline is the default deadline for the upload
	// of a single chunk. It can be overwritten by Writer.ChunkRetryDeadline.
	defaultWriteChunkRetryDeadline = 32 * time.Second
	// maxPerMessageWriteSize is the maximum amount of content that can be sent
	// per WriteObjectRequest message. A buffer reaching this amount will
	// precipitate a flush of the buffer. It is only used by the gRPC Writer
	// implementation.
	maxPerMessageWriteSize int = int(storagepb.ServiceConstants_MAX_WRITE_CHUNK_BYTES)
)

func (w *gRPCWriter) Write(p []byte) (n int, err error) {
	done := make(chan struct{})
	cmd := &gRPCWriterCommandWrite{p: p, done: done}
	select {
	case <-w.donec:
		return 0, w.streamResult
	case w.writesChan <- cmd:
		// update fullObjectChecksum on every write and send it on finalWrite
		if !w.disableAutoChecksum {
			w.fullObjectChecksum = crc32.Update(w.fullObjectChecksum, crc32cTable, p)
		}
		// write command successfully delivered to sender. We no longer own cmd.
		break
	}

	select {
	case <-w.donec:
		return 0, w.streamResult
	case <-done:
		return len(p), nil
	}
}

func (w *gRPCWriter) Flush() (int64, error) {
	done := make(chan int64)
	cmd := &gRPCWriterCommandFlush{done: done}
	select {
	case <-w.donec:
		return 0, w.streamResult
	case w.writesChan <- cmd:
		// flush command successfully delivered to sender. We no longer own cmd.
		break
	}

	select {
	case <-w.donec:
		return 0, w.streamResult
	case f := <-done:
		return f, nil
	}
}

func (w *gRPCWriter) Close() error {
	w.CloseWithError(nil)
	return w.streamResult
}

func (w *gRPCWriter) CloseWithError(err error) error {
	// N.B. CloseWithError always returns nil!
	select {
	case <-w.donec:
		return nil
	case w.writesChan <- &gRPCWriterCommandClose{err: err}:
		break
	}
	<-w.donec
	return nil
}

func (c *grpcStorageClient) OpenWriter(params *openWriterParams, opts ...storageOption) (internalWriter, error) {
	if params.attrs.Retention != nil {
		// TO-DO: remove once ObjectRetention is available - see b/308194853
		return nil, status.Errorf(codes.Unimplemented, "storage: object retention is not supported in gRPC")
	}

	spec := &storagepb.WriteObjectSpec{
		Resource:   params.attrs.toProtoObject(params.bucket),
		Appendable: proto.Bool(params.append),
	}
	// WriteObject doesn't support the generation condition, so use default.
	if err := applyCondsProto("WriteObject", defaultGen, params.conds, spec); err != nil {
		return nil, err
	}

	s := callSettings(c.settings, opts...)

	if s.retry == nil {
		s.retry = defaultRetry.clone()
	}
	if params.append {
		s.retry = withBidiWriteObjectRedirectionErrorRetries(s)
	}

	chunkRetryDeadline := defaultWriteChunkRetryDeadline
	if params.chunkRetryDeadline != 0 {
		chunkRetryDeadline = params.chunkRetryDeadline
	}

	ctx := params.ctx
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}

	chunkSize := gRPCChunkSize(params.chunkSize)
	writeQuantum := maxPerMessageWriteSize
	if writeQuantum > chunkSize {
		writeQuantum = chunkSize
	}
	sendableUnits := chunkSize / writeQuantum
	// There's no strict requirement that the chunk size be an exact multiple of
	// the writeQuantum. In that case, there will be a tail segment of less than
	// writeQuantum.
	lastSegmentStart := sendableUnits * writeQuantum
	if lastSegmentStart < chunkSize {
		sendableUnits++
	}

	if params.append && params.appendGen >= 0 && params.setTakeoverOffset == nil {
		return nil, errors.New("storage: no way to report offset for appendable takeover")
	}

	w := &gRPCWriter{
		preRunCtx: ctx,
		c:         c,
		settings:  s,

		bucket:        params.bucket,
		attrs:         params.attrs,
		conds:         params.conds,
		spec:          spec,
		encryptionKey: params.encryptionKey,

		setError:          params.setError,
		progress:          params.progress,
		setObj:            params.setObj,
		setSize:           params.setSize,
		setTakeoverOffset: params.setTakeoverOffset,

		flushSupported:        params.append,
		sendCRC32C:            params.sendCRC32C,
		disableAutoChecksum:   params.disableAutoChecksum,
		forceOneShot:          params.chunkSize <= 0,
		forceEmptyContentType: params.forceEmptyContentType,
		append:                params.append,
		appendGen:             params.appendGen,
		finalizeOnClose:       params.finalizeOnClose,

		buf:              make([]byte, 0, chunkSize),
		writeQuantum:     writeQuantum,
		lastSegmentStart: lastSegmentStart,
		sendableUnits:    sendableUnits,
		bufUnsentIdx:     0,
		bufFlushedIdx:    -1, // Handle flushes to length 0
		bufBaseOffset:    0,

		chunkRetryDeadline: chunkRetryDeadline,
		abandonRetriesTime: time.Time{},
		attempts:           0,
		lastErr:            nil,
		streamSender:       nil,

		writesChan:     make(chan gRPCWriterCommand, 1),
		currentCommand: nil,
		streamResult:   nil,
		donec:          params.donec,
	}

	go func() {
		if err := w.gatherFirstBuffer(); err != nil {
			w.streamResult = err
			w.setError(err)
			close(w.donec)
			return
		}

		if w.attrs.ContentType == "" && !w.forceEmptyContentType {
			w.spec.Resource.ContentType = w.detectContentType()
		}
		w.streamSender = w.pickBufferSender()

		// Writer does not use maxRetryDuration from retryConfig to maintain
		// consistency with HTTP client behavior. Writers should use
		// ChunkRetryDeadline for per-chunk timeouts and context for overall timeouts.
		writerRetry := w.settings.retry
		if writerRetry != nil {
			writerRetry = writerRetry.clone()
			writerRetry.maxRetryDuration = 0
		}
		w.streamResult = checkCanceled(run(w.preRunCtx, func(ctx context.Context) error {
			w.lastErr = w.writeLoop(ctx)
			return w.lastErr
		}, writerRetry, w.settings.idempotent))
		w.setError(w.streamResult)
		close(w.donec)
	}()

	return w, nil
}

// gRPCWriter is a wrapper around the the gRPC client-stream API that manages
// sending chunks of data provided by the user over the stream.
type gRPCWriter struct {
	preRunCtx context.Context
	c         *grpcStorageClient
	settings  *settings

	bucket        string
	attrs         *ObjectAttrs
	conds         *Conditions
	spec          *storagepb.WriteObjectSpec
	encryptionKey []byte

	setError          func(error)
	progress          func(int64)
	setObj            func(*ObjectAttrs)
	setSize           func(int64)
	setTakeoverOffset func(int64)

	fullObjectChecksum uint32

	flushSupported        bool
	sendCRC32C            bool
	disableAutoChecksum   bool
	forceOneShot          bool
	forceEmptyContentType bool
	append                bool
	appendGen             int64
	finalizeOnClose       bool

	buf []byte
	// A writeQuantum is the largest quantity of data which can be sent to the
	// service in a single message.
	writeQuantum     int
	lastSegmentStart int
	sendableUnits    int
	bufUnsentIdx     int
	bufFlushedIdx    int
	bufBaseOffset    int64

	chunkRetryDeadline time.Duration
	abandonRetriesTime time.Time
	attempts           int
	lastErr            error
	streamSender       gRPCBidiWriteBufferSender

	// Communication from the user goroutine to the stream management goroutines
	writesChan         chan gRPCWriterCommand
	currentCommand     gRPCWriterCommand
	forcedStreamResult error
	streamResult       error
	donec              chan struct{}
}

func (w *gRPCWriter) pickBufferSender() gRPCBidiWriteBufferSender {
	if w.append {
		// Appendable object semantics
		if w.appendGen >= 0 {
			return w.newGRPCAppendTakeoverWriteBufferSender()
		}
		return w.newGRPCAppendableObjectBufferSender()
	}
	if w.forceOneShot {
		// One shot semantics - no progress reports
		w.progress = func(int64) {}
		return w.newGRPCOneshotBidiWriteBufferSender()
	}
	// Resumable write semantics
	return w.newGRPCResumableBidiWriteBufferSender()
}

// sendBufferToTarget uses cs to send slices of buf, which starts at baseOffset
// bytes into the object. Slices are sent until flushAt bytes have sent, in
// which case the final request is a flush, or until len(buf) < w.writeQuantum.
//
// handleCompletion is called for any completions that arrive during sends.
//
// Returns the last byte offset sent. Returns true if all desired requests were
// delivered, and false if cs.completions was closed before all requests could
// be delivered.
func (w *gRPCWriter) sendBufferToTarget(cs gRPCWriterCommandHandleChans, buf []byte, baseOffset int64, flushAt int, handleCompletion func(gRPCBidiWriteCompletion)) (int64, bool) {
	sent := 0
	if len(buf) > flushAt {
		buf = buf[:flushAt]
	}
	for len(buf) > 0 && (len(buf) >= w.writeQuantum || len(buf) >= flushAt-sent) {
		q := w.writeQuantum
		if flushAt-sent < w.writeQuantum {
			q = flushAt - sent
		}
		req := gRPCBidiWriteRequest{
			buf:    buf[:q],
			offset: baseOffset + int64(sent),
			flush:  q == flushAt-sent,
		}
		if !cs.deliverRequestUnlessCompleted(req, handleCompletion) {
			return baseOffset + int64(sent), false
		}
		buf = buf[q:]
		sent += q
	}
	return baseOffset + int64(sent), true
}

func (w *gRPCWriter) handleCompletion(c gRPCBidiWriteCompletion) {
	if c.resource != nil {
		w.setObj(newObjectFromProto(c.resource))
	}

	// Already handled this completion
	if c.flushOffset <= w.bufBaseOffset+int64(w.bufFlushedIdx) {
		return
	}

	w.bufFlushedIdx = int(c.flushOffset - w.bufBaseOffset)
	if w.bufFlushedIdx >= len(w.buf) {
		// We can clear w.buf
		w.bufBaseOffset = c.flushOffset
		w.bufUnsentIdx = 0
		w.bufFlushedIdx = 0
		w.buf = w.buf[:0]
	}
	w.setSize(c.flushOffset)
	w.progress(c.flushOffset)
}

func (w *gRPCWriter) withCommandRetryDeadline(f func() error) error {
	w.abandonRetriesTime = time.Now().Add(w.chunkRetryDeadline)
	err := f()
	if err == nil {
		w.abandonRetriesTime = time.Time{}
	}
	return err
}

// Gather write commands before starting the actual write. Returns nil if the
// stream should be started, and an error otherwise.
func (w *gRPCWriter) gatherFirstBuffer() error {
	if w.append && w.appendGen >= 0 {
		// For takeovers, kick off the stream immediately since we need to know the
		// takeover offset to issue writes.
		return nil
	}

	for cmd := range w.writesChan {
		switch v := cmd.(type) {
		case *gRPCWriterCommandWrite:
			if len(w.buf)+len(v.p) <= cap(w.buf) {
				// We have not started sending yet, and we can stage all data without
				// starting a send. Compare against cap(w.buf) instead of
				// w.writeQuantum: that way we can perform a oneshot upload for objects
				// which fit in one chunk, even though we will cut the request into
				// w.writeQuantum units when we do start sending.
				origLen := len(w.buf)
				w.buf = w.buf[:origLen+len(v.p)]
				copy(w.buf[origLen:], v.p)
				close(v.done)
			} else {
				// Too large. Handle it in writeLoop.
				w.currentCommand = cmd
				return nil
			}
			break
		case *gRPCWriterCommandClose:
			// If we get here, data (if any) fits in w.buf, so we can force oneshot.
			w.forceOneShot = true
			w.currentCommand = cmd
			// No need to start sending if v.err is not nil.
			return v.err
		default:
			// Have to start sending!
			w.currentCommand = cmd
			return nil
		}
	}
	// Nothing should ever close w.writesChan, so we should never get here
	return errors.New("storage.Writer: unexpectedly closed w.writesChan")
}

func (w *gRPCWriter) writeLoop(ctx context.Context) error {
	w.attempts++
	// Return an error if we've been waiting for a single operation for too long.
	if !w.abandonRetriesTime.IsZero() && time.Now().After(w.abandonRetriesTime) {
		return fmt.Errorf("storage: retry deadline of %s reached after %v attempts; last error: %w", w.chunkRetryDeadline, w.attempts, w.lastErr)
	}
	// Allow each request in w.buf to be sent and result in a completion without
	// blocking.
	requests := make(chan gRPCBidiWriteRequest, w.sendableUnits)
	completions := make(chan gRPCBidiWriteCompletion, w.sendableUnits)
	// Only one request ack will be outstanding at a time.
	requestAcks := make(chan struct{}, 1)
	chcs := gRPCWriterCommandHandleChans{requests, requestAcks, completions}
	bscs := gRPCBufSenderChans{requests, requestAcks, completions}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w.streamSender.connect(ctx, bscs, w.settings.gax...)

	// Send any full quantum in w.buf, possibly including a flush
	if err := w.withCommandRetryDeadline(func() error {
		sentOffset, ok := w.sendBufferToTarget(chcs, w.buf, w.bufBaseOffset, cap(w.buf),
			w.handleCompletion)
		if !ok {
			return w.streamSender.err()
		}
		w.bufUnsentIdx = int(sentOffset - w.bufBaseOffset)
		// We may have observed a completion that is after all of w.buf if we also
		// have a write command in w.currentCommand which sent a flush, but failed
		// before the completion could be delivered.
		if w.bufUnsentIdx < 0 {
			w.bufUnsentIdx = 0
		}
		return nil
	}); err != nil {
		return err
	}

	err := func() error {
		for {
			if w.currentCommand != nil {
				if err := w.withCommandRetryDeadline(func() error {
					return w.currentCommand.handle(w, chcs)
				}); err != nil {
					return err
				}
				w.currentCommand = nil
			}
			select {
			case c, ok := <-completions:
				if !ok {
					return w.streamSender.err()
				}
				w.handleCompletion(c)
			case cmd, ok := <-w.writesChan:
				if !ok {
					// Nothing should ever close w.writesChan, so we should never get here
					return errors.New("storage.Writer: unexpectedly closed w.writesChan")
				}
				w.currentCommand = cmd
			}
		}
	}()
	if err == nil {
		err = errors.New("storage.Writer: unexpected nil error from write loop")
	}
	var closeErr *gRPCWriterCommandClose
	if !errors.As(err, &closeErr) {
		// Not a shutdown.
		return err
	}

	if closeErr.err == nil {
		// Clean shutdown. Send any remaining tail.
		req := gRPCBidiWriteRequest{
			buf:         w.buf[w.bufUnsentIdx:],
			offset:      w.bufBaseOffset + int64(w.bufUnsentIdx),
			flush:       true,
			finishWrite: true,
		}
		if err := w.withCommandRetryDeadline(func() error {
			if !chcs.deliverRequestUnlessCompleted(req, w.handleCompletion) {
				return w.streamSender.err()
			}
			return nil
		}); err != nil {
			return err
		}
	} else {
		// Unclean shutdown. Cancel the context so we clean up expeditiously.
		cancel()
	}

	close(requests)
	for c := range completions {
		w.handleCompletion(c)
	}
	if closeErr.err == nil {
		return w.streamSender.err()
	}
	return closeErr.err
}

// gRPCWriterCommandHandleChans contains the channels that a gRPCWriterCommand
// implementation must use to send requests and get notified of completions.
// Requests are delivered on a write-only channel, request acks and completions
// arrive on read-only channels.
type gRPCWriterCommandHandleChans struct {
	requests    chan<- gRPCBidiWriteRequest
	requestAcks <-chan struct{}
	completions <-chan gRPCBidiWriteCompletion
}

// gRPCBufSenderChans contains the channels that a gRPCBidiWriteBufferSender
// must use to get notified of requests and deliver completions. Requests arrive
// on a read-only channel, request acks and completions are delivered on
// write-only channels.
type gRPCBufSenderChans struct {
	requests    <-chan gRPCBidiWriteRequest
	requestAcks chan<- struct{}
	completions chan<- gRPCBidiWriteCompletion
}

// deliverRequestUnlessCompleted submits req to cs.requests, unless
// cs.completions is closed first. If a completion arrives before the request is
// enqueued, handleCompletion is called.
//
// Returns true if request was successfully enqueued, and false if completions
// was closed first.
func (cs gRPCWriterCommandHandleChans) deliverRequestUnlessCompleted(req gRPCBidiWriteRequest, handleCompletion func(gRPCBidiWriteCompletion)) bool {
	for {
		select {
		case cs.requests <- req:
			return true
		case c, ok := <-cs.completions:
			if !ok {
				return false
			}
			handleCompletion(c)
		}
	}
}

// gRPCWriterCommand represents an operation on a gRPCWriter
type gRPCWriterCommand interface {
	// handle applies the command to a gRPCWriter.
	//
	// Implementations may return an error. In that case, the command may be
	// retried with a new gRPCWriterCommandHandleChans instance.
	handle(*gRPCWriter, gRPCWriterCommandHandleChans) error
}

type gRPCWriterCommandWrite struct {
	p    []byte
	done chan struct{}
}

func (c *gRPCWriterCommandWrite) handle(w *gRPCWriter, cs gRPCWriterCommandHandleChans) error {
	if len(c.p) == 0 {
		// No data to write.
		close(c.done)
		return nil
	}

	wblen := len(w.buf)
	allKnownBytes := wblen + len(c.p)
	fullBufs := allKnownBytes / cap(w.buf)
	partialBuf := allKnownBytes % cap(w.buf)
	if partialBuf == 0 {
		// If we would exactly fill some number of cap(w.buf) units, we don't need
		// to block on the flush for the last one. We know that c.p is not empty, so
		// allKnownBytes is not 0 and therefore if partialBuf is 0, fullBufs is not
		// 0.
		fullBufs--
		partialBuf = cap(w.buf)
	}

	if fullBufs == 0 {
		// Everything fits in w.buf. Copy in and send from there.
		w.buf = w.buf[:allKnownBytes]
		copied := copy(w.buf[wblen:], c.p)
		// Now that it's in w.buf, clear it from the command in case we retry.
		c.p = c.p[copied:]
		sending := w.buf[w.bufUnsentIdx:]
		sentOffset, ok := w.sendBufferToTarget(cs, sending, w.bufBaseOffset+int64(w.bufUnsentIdx), cap(sending),
			w.handleCompletion)
		if !ok {
			return w.streamSender.err()
		}
		w.bufUnsentIdx = int(sentOffset - w.bufBaseOffset)
		close(c.done)
		return nil
	}

	// We have at least one full buffer, followed by a partial. The first full
	// buffer is the interesting one. We don't actually have to copy all of c.p
	// in: we can send from it in place, except for any partial quantum at the
	// tail of w.buf. Send that quantum...
	toNextWriteQuantum := func() int {
		if wblen > w.lastSegmentStart {
			return cap(w.buf) - wblen
		}
		if wblen%w.writeQuantum == 0 {
			return 0
		}
		return w.writeQuantum - (wblen % w.writeQuantum)
	}()
	w.buf = w.buf[:wblen+toNextWriteQuantum]
	copied := copy(w.buf[wblen:], c.p)
	c.p = c.p[copied:]
	firstFullBufFromCmd := cap(w.buf) - len(w.buf)

	sending := w.buf[w.bufUnsentIdx:]
	sentOffset, ok := w.sendBufferToTarget(cs, sending, w.bufBaseOffset+int64(w.bufUnsentIdx), cap(sending),
		w.handleCompletion)
	if !ok {
		return w.streamSender.err()
	}

	// ...then send the prefix of c.p which could fill w.buf
	cmdBaseOffset := w.bufBaseOffset + int64(len(w.buf))
	cmdBuf := c.p
	trimCommandBuf := func(cmp gRPCBidiWriteCompletion) {
		w.handleCompletion(cmp)
		// After a completion, keep c.p up to date with w.buf's tail.
		bufTail := w.bufBaseOffset + int64(len(w.buf))
		if bufTail <= cmdBaseOffset {
			return
		}
		trim := int(bufTail - cmdBaseOffset)
		if len(c.p) < trim {
			trim = len(c.p)
		}
		c.p = c.p[trim:]
		cmdBaseOffset = bufTail
	}
	offset := cmdBaseOffset
	sentOffset, ok = w.sendBufferToTarget(cs, cmdBuf, offset, firstFullBufFromCmd,
		trimCommandBuf)
	if !ok {
		return w.streamSender.err()
	}
	cmdBuf = cmdBuf[int(sentOffset-offset):]
	offset = sentOffset

	// Remaining full buffers can be satisfied entirely from cmdBuf with no copies.
	for i := 0; i < fullBufs-1; i++ {
		sentOffset, ok = w.sendBufferToTarget(cs, cmdBuf, offset, cap(w.buf),
			trimCommandBuf)
		if !ok {
			return w.streamSender.err()
		}
		cmdBuf = cmdBuf[int(sentOffset-offset):]
		offset = sentOffset
	}

	// Send the last partial buffer. We need to flush to offset before we can copy
	// the rest of cmdBuf into w.buf and complete this command.
	sentOffset, ok = w.sendBufferToTarget(cs, cmdBuf, offset, cap(w.buf),
		trimCommandBuf)
	if !ok {
		return w.streamSender.err()
	}
	// Finally, we need the sender to ack to let us know c.p can be released.
	if !cs.deliverRequestUnlessCompleted(gRPCBidiWriteRequest{requestAck: true}, trimCommandBuf) {
		return w.streamSender.err()
	}
	ackOutstanding := true
	for ackOutstanding || (w.bufBaseOffset+int64(w.bufFlushedIdx)) < offset {
		select {
		case cmp, ok := <-cs.completions:
			if !ok {
				return w.streamSender.err()
			}
			trimCommandBuf(cmp)
		case <-cs.requestAcks:
			ackOutstanding = false
		}
	}
	toCopyIn := cmdBuf[int(w.bufBaseOffset-offset):]
	w.buf = w.buf[:len(toCopyIn)]
	copy(w.buf, toCopyIn)
	w.bufUnsentIdx = int(sentOffset - w.bufBaseOffset)
	close(c.done)
	return nil
}

type gRPCWriterCommandFlush struct {
	done chan int64
}

func (c *gRPCWriterCommandFlush) handle(w *gRPCWriter, cs gRPCWriterCommandHandleChans) error {
	flushTarget := w.bufBaseOffset + int64(len(w.buf))
	// We know that there are at most w.writeQuantum bytes in
	// w.buf[w.bufUnsentIdx:], because we send anything more inline when handling
	// a write.
	req := gRPCBidiWriteRequest{
		buf:         w.buf[w.bufUnsentIdx:],
		offset:      w.bufBaseOffset + int64(w.bufUnsentIdx),
		flush:       true,
		finishWrite: false,
	}
	if !cs.deliverRequestUnlessCompleted(req, w.handleCompletion) {
		return w.streamSender.err()
	}
	// Successful flushes will clear w.buf.
	for (w.bufBaseOffset + int64(w.bufFlushedIdx)) < flushTarget {
		c, ok := <-cs.completions
		if !ok {
			// Stream failure
			return w.streamSender.err()
		}
		w.handleCompletion(c)
	}
	// handleCompletion has cleared w.buf and updated w.bufUnsentIdx by now.
	c.done <- flushTarget
	return nil
}

type gRPCWriterCommandClose struct {
	err error
}

func (e *gRPCWriterCommandClose) Error() string {
	return e.err.Error()
}

func (c *gRPCWriterCommandClose) handle(w *gRPCWriter, cs gRPCWriterCommandHandleChans) error {
	// N.B. c is not nil, even if c.err is nil!
	return c
}

// Detect content type using bytes first from baseBuf, then from pendingBuf if
// there are not enough bytes in baseBuf.
func (w *gRPCWriter) detectContentType() string {
	wblen := len(w.buf)
	// If the current command is a write, we want to be able to update it in
	// place. If the
	cmdbuf := &([]byte{})
	if c, ok := w.currentCommand.(*gRPCWriterCommandWrite); ok {
		cmdbuf = &c.p
	}
	if wblen == 0 {
		// Use the command in place
		return http.DetectContentType(*cmdbuf)
	}
	if wblen >= w.writeQuantum {
		// Use w.buf in place
		return http.DetectContentType(w.buf)
	}

	// We need to put bytes from the command onto w.buf. Try to fill a
	// writeQuantum since we'll have to do that in order to send, anyway.
	newSz := w.writeQuantum
	if wblen+len(*cmdbuf) < newSz {
		newSz = wblen + len(*cmdbuf)
	}
	w.buf = w.buf[:newSz]
	copied := copy(w.buf[wblen:], *cmdbuf)
	*cmdbuf = (*cmdbuf)[copied:]
	return http.DetectContentType(w.buf)
}

type gRPCBidiWriteRequest struct {
	buf         []byte
	offset      int64
	flush       bool
	finishWrite bool
	// If requestAck is true, no other message fields may be set. Buffer senders
	// must ack on the requestAcks channel if all prior messages on the requests
	// channel have been delivered to gRPC.
	requestAck bool
}

type gRPCBidiWriteCompletion struct {
	flushOffset int64
	resource    *storagepb.Object
}

func completion(r *storagepb.BidiWriteObjectResponse) *gRPCBidiWriteCompletion {
	switch c := r.WriteStatus.(type) {
	case *storagepb.BidiWriteObjectResponse_PersistedSize:
		return &gRPCBidiWriteCompletion{flushOffset: c.PersistedSize}
	case *storagepb.BidiWriteObjectResponse_Resource:
		return &gRPCBidiWriteCompletion{flushOffset: c.Resource.GetSize(), resource: c.Resource}
	default:
		return nil
	}
}

// Server contract expects full object checksum to be sent only on first or last write.
// Checksums of full object are already being sent on first Write during initialization of sender.
// Send objectChecksums only on final request and nil in other cases.
func bidiWriteObjectRequest(r gRPCBidiWriteRequest, bufChecksum *uint32, objectChecksums *storagepb.ObjectChecksums) *storagepb.BidiWriteObjectRequest {
	var data *storagepb.BidiWriteObjectRequest_ChecksummedData
	if r.buf != nil {
		data = &storagepb.BidiWriteObjectRequest_ChecksummedData{
			ChecksummedData: &storagepb.ChecksummedData{
				Content: r.buf,
				Crc32C:  bufChecksum,
			},
		}
	}
	req := &storagepb.BidiWriteObjectRequest{
		Data:            data,
		WriteOffset:     r.offset,
		FinishWrite:     r.finishWrite,
		Flush:           r.flush,
		StateLookup:     r.flush,
		ObjectChecksums: objectChecksums,
	}
	return req
}

type getObjectChecksumsParams struct {
	sendCRC32C          bool
	disableAutoChecksum bool
	objectAttrs         *ObjectAttrs
	fullObjectChecksum  func() uint32
	finishWrite         bool
	takeoverWriter      bool
}

// getObjectChecksums determines what checksum information to include in the final
// gRPC request
//
// function returns a populated ObjectChecksums only when finishWrite is true
// If CRC32C is disabled, it returns the user-provided checksum if available.
// If CRC32C is enabled, it returns the user-provided checksum if available,
// or the computed checksum of the entire object.
func getObjectChecksums(params *getObjectChecksumsParams) *storagepb.ObjectChecksums {
	if !params.finishWrite {
		return nil
	}

	// send user's checksum on last write op if available
	if params.sendCRC32C {
		return toProtoChecksums(params.sendCRC32C, params.objectAttrs)
	}
	// TODO(b/461982277): Enable checksum validation for appendable takeover writer gRPC
	if params.disableAutoChecksum || params.takeoverWriter {
		return nil
	}
	return &storagepb.ObjectChecksums{
		Crc32C: proto.Uint32(params.fullObjectChecksum()),
	}
}

type gRPCBidiWriteBufferSender interface {
	// connect implementations may attempt to establish a connection for issuing
	// writes.
	//
	// In case of an error, implementations must close the completion channel. The
	// write loop will inspect err() after that channel is closed and before any
	// subsequent calls to connect().
	//
	// If a request is delivered with flush true, implementations must request
	// that the service make the data stable. If a request is delivered with
	// finishWrite true, no subsequent messages will be delivered on the channel,
	// and implementations must attempt to tear down the connection cleanly after
	// sending the request. In both cases, the write loop will stall unless the
	// completion channel is closed or receives a completion indicating that
	// offset+len(buf) is persisted.
	connect(context.Context, gRPCBufSenderChans, ...gax.CallOption)

	// err implementations must return the error on the stream. err() must be safe
	// to call after the completion channel provided to connect() is closed. The
	// write loop will not make concurrent calls to connect() and err().
	err() error
}

type gRPCOneshotBidiWriteBufferSender struct {
	raw          *gapic.Client
	bucket       string
	firstMessage *storagepb.BidiWriteObjectRequest
	streamErr    error

	// Checksum related settings.
	sendCRC32C          bool
	disableAutoChecksum bool
	objectAttrs         *ObjectAttrs
	fullObjectChecksum  func() uint32
}

func (w *gRPCWriter) newGRPCOneshotBidiWriteBufferSender() *gRPCOneshotBidiWriteBufferSender {
	return &gRPCOneshotBidiWriteBufferSender{
		raw:    w.c.raw,
		bucket: w.bucket,
		firstMessage: &storagepb.BidiWriteObjectRequest{
			FirstMessage: &storagepb.BidiWriteObjectRequest_WriteObjectSpec{
				WriteObjectSpec: w.spec,
			},
			CommonObjectRequestParams: toProtoCommonObjectRequestParams(w.encryptionKey),
			ObjectChecksums:           toProtoChecksums(w.sendCRC32C, w.attrs),
		},
		sendCRC32C:          w.sendCRC32C,
		disableAutoChecksum: w.disableAutoChecksum,
		objectAttrs:         w.attrs,
		fullObjectChecksum: func() uint32 {
			return w.fullObjectChecksum
		},
	}
}

func (s *gRPCOneshotBidiWriteBufferSender) err() error { return s.streamErr }

// drainInboundStream calls stream.Recv() repeatedly until an error is returned.
// It returns the last Resource received on the stream, or nil if no Resource
// was returned. drainInboundStream always returns a non-nil error. io.EOF
// indicates all messages were successfully read.
func drainInboundStream(stream storagepb.Storage_BidiWriteObjectClient) (object *storagepb.Object, err error) {
	for err == nil {
		var resp *storagepb.BidiWriteObjectResponse
		resp, err = stream.Recv()
		// GetResource() returns nil on a nil response
		if resp.GetResource() != nil {
			object = resp.GetResource()
		}
	}
	return object, err
}

func (s *gRPCOneshotBidiWriteBufferSender) connect(ctx context.Context, cs gRPCBufSenderChans, opts ...gax.CallOption) {
	s.streamErr = nil
	ctx = gRPCWriteRequestParams{bucket: s.bucket}.apply(ctx)
	stream, err := s.raw.BidiWriteObject(ctx, opts...)
	if err != nil {
		s.streamErr = err
		close(cs.completions)
		return
	}

	go func() {
		firstSend := true
		for r := range cs.requests {
			if r.requestAck {
				cs.requestAcks <- struct{}{}
				continue
			}

			var bufChecksum *uint32
			if !s.disableAutoChecksum {
				bufChecksum = proto.Uint32(crc32.Checksum(r.buf, crc32cTable))
			}
			objectChecksums := getObjectChecksums(&getObjectChecksumsParams{
				sendCRC32C:          s.sendCRC32C,
				objectAttrs:         s.objectAttrs,
				fullObjectChecksum:  s.fullObjectChecksum,
				disableAutoChecksum: s.disableAutoChecksum,
				finishWrite:         r.finishWrite,
			})
			req := bidiWriteObjectRequest(r, bufChecksum, objectChecksums)

			if firstSend {
				proto.Merge(req, s.firstMessage)
				firstSend = false
			}

			if err := stream.Send(req); err != nil {
				_, s.streamErr = drainInboundStream(stream)
				if err != io.EOF {
					s.streamErr = err
				}
				close(cs.completions)
				return
			}

			if r.finishWrite {
				stream.CloseSend()
				// Oneshot uploads only read from the response stream on completion or
				// failure
				obj, err := drainInboundStream(stream)
				if obj == nil || err != io.EOF {
					s.streamErr = err
				} else {
					cs.completions <- gRPCBidiWriteCompletion{flushOffset: obj.GetSize(), resource: obj}
				}
				close(cs.completions)
				return
			}

			// Oneshot uploads assume all flushes succeed
			if r.flush {
				cs.completions <- gRPCBidiWriteCompletion{flushOffset: r.offset + int64(len(r.buf))}
			}
		}
	}()
}

type gRPCResumableBidiWriteBufferSender struct {
	raw    *gapic.Client
	bucket string

	startWriteRequest *storagepb.StartResumableWriteRequest
	upid              string

	// Checksum related settings.
	sendCRC32C          bool
	disableAutoChecksum bool
	objectAttrs         *ObjectAttrs
	fullObjectChecksum  func() uint32

	streamErr error
}

func (w *gRPCWriter) newGRPCResumableBidiWriteBufferSender() *gRPCResumableBidiWriteBufferSender {
	return &gRPCResumableBidiWriteBufferSender{
		raw:    w.c.raw,
		bucket: w.bucket,
		startWriteRequest: &storagepb.StartResumableWriteRequest{
			WriteObjectSpec:           w.spec,
			CommonObjectRequestParams: toProtoCommonObjectRequestParams(w.encryptionKey),
			ObjectChecksums:           toProtoChecksums(w.sendCRC32C, w.attrs),
		},
		sendCRC32C:          w.sendCRC32C,
		disableAutoChecksum: w.disableAutoChecksum,
		objectAttrs:         w.attrs,
		fullObjectChecksum: func() uint32 {
			return w.fullObjectChecksum
		},
	}
}

func (s *gRPCResumableBidiWriteBufferSender) err() error { return s.streamErr }

func (s *gRPCResumableBidiWriteBufferSender) connect(ctx context.Context, cs gRPCBufSenderChans, opts ...gax.CallOption) {
	s.streamErr = nil
	ctx = gRPCWriteRequestParams{bucket: s.bucket}.apply(ctx)

	if s.startWriteRequest != nil {
		upres, err := s.raw.StartResumableWrite(ctx, s.startWriteRequest, opts...)
		if err != nil {
			s.streamErr = err
			close(cs.completions)
			return
		}
		s.upid = upres.GetUploadId()
		s.startWriteRequest = nil
	} else {
		q, err := s.raw.QueryWriteStatus(ctx, &storagepb.QueryWriteStatusRequest{UploadId: s.upid}, opts...)
		if err != nil {
			s.streamErr = err
			close(cs.completions)
			return
		}
		cs.completions <- gRPCBidiWriteCompletion{flushOffset: q.GetPersistedSize()}
	}

	stream, err := s.raw.BidiWriteObject(ctx, opts...)
	if err != nil {
		s.streamErr = err
		close(cs.completions)
		return
	}

	go func() {
		var sendErr, recvErr error
		sendDone := make(chan struct{})
		recvDone := make(chan struct{})

		go func() {
			sendErr = func() error {
				firstSend := true
				for {
					select {
					case <-recvDone:
						// Because `requests` is not connected to the gRPC machinery, we
						// have to check for asynchronous termination on the receive side.
						return nil
					case r, ok := <-cs.requests:
						if !ok {
							stream.CloseSend()
							return nil
						}
						if r.requestAck {
							cs.requestAcks <- struct{}{}
							continue
						}

						var bufChecksum *uint32
						if !s.disableAutoChecksum {
							bufChecksum = proto.Uint32(crc32.Checksum(r.buf, crc32cTable))
						}
						objectChecksums := getObjectChecksums(&getObjectChecksumsParams{
							sendCRC32C:          s.sendCRC32C,
							objectAttrs:         s.objectAttrs,
							fullObjectChecksum:  s.fullObjectChecksum,
							disableAutoChecksum: s.disableAutoChecksum,
							finishWrite:         r.finishWrite,
						})
						req := bidiWriteObjectRequest(r, bufChecksum, objectChecksums)

						if firstSend {
							req.FirstMessage = &storagepb.BidiWriteObjectRequest_UploadId{UploadId: s.upid}
							firstSend = false
						}
						if err := stream.Send(req); err != nil {
							return err
						}
						if r.finishWrite {
							stream.CloseSend()
							return nil
						}
					}
				}
			}()
			close(sendDone)
		}()

		go func() {
			recvErr = func() error {
				for {
					resp, err := stream.Recv()
					if err != nil {
						return err
					}
					if c := completion(resp); c != nil {
						cs.completions <- *c
					}
				}
			}()
			close(recvDone)
		}()

		<-sendDone
		<-recvDone
		// Prefer recvErr since that's where RPC errors are delivered
		if recvErr != nil {
			s.streamErr = recvErr
		} else if sendErr != nil {
			s.streamErr = sendErr
		}
		if s.streamErr == io.EOF {
			s.streamErr = nil
		}
		close(cs.completions)
	}()
}

type gRPCAppendBidiWriteBufferSender struct {
	raw          *gapic.Client
	bucket       string
	routingToken *string

	firstMessage    *storagepb.BidiWriteObjectRequest
	finalizeOnClose bool
	objResource     *storagepb.Object

	// Checksum related settings.
	sendCRC32C          bool
	disableAutoChecksum bool
	objectAttrs         *ObjectAttrs
	fullObjectChecksum  func() uint32

	takeoverWriter bool

	streamErr error
}

func (s *gRPCAppendBidiWriteBufferSender) err() error { return s.streamErr }

// Use for a newly created appendable object.
func (w *gRPCWriter) newGRPCAppendableObjectBufferSender() *gRPCAppendBidiWriteBufferSender {
	return &gRPCAppendBidiWriteBufferSender{
		raw:    w.c.raw,
		bucket: w.bucket,
		firstMessage: &storagepb.BidiWriteObjectRequest{
			FirstMessage: &storagepb.BidiWriteObjectRequest_WriteObjectSpec{
				WriteObjectSpec: w.spec,
			},
			CommonObjectRequestParams: toProtoCommonObjectRequestParams(w.encryptionKey),
		},
		finalizeOnClose:     w.finalizeOnClose,
		sendCRC32C:          w.sendCRC32C,
		disableAutoChecksum: w.disableAutoChecksum,
		objectAttrs:         w.attrs,
		fullObjectChecksum: func() uint32 {
			return w.fullObjectChecksum
		},
	}
}

func (s *gRPCAppendBidiWriteBufferSender) connect(ctx context.Context, cs gRPCBufSenderChans, opts ...gax.CallOption) {
	s.streamErr = nil
	ctx = gRPCWriteRequestParams{appendable: true, bucket: s.bucket, routingToken: s.routingToken}.apply(ctx)

	stream, err := s.raw.BidiWriteObject(ctx, opts...)
	if err != nil {
		s.streamErr = err
		close(cs.completions)
		return
	}

	go s.handleStream(stream, cs, true)
}

func (s *gRPCAppendBidiWriteBufferSender) handleStream(stream storagepb.Storage_BidiWriteObjectClient, cs gRPCBufSenderChans, firstSend bool) {
	var sendErr, recvErr error
	sendDone := make(chan struct{})
	recvDone := make(chan struct{})

	go func() {
		sendErr = func() error {
			for {
				select {
				case <-recvDone:
					// Because `requests` is not connected to the gRPC machinery, we
					// have to check for asynchronous termination on the receive side.
					return nil
				case r, ok := <-cs.requests:
					if !ok {
						stream.CloseSend()
						return nil
					}
					if r.requestAck {
						cs.requestAcks <- struct{}{}
						continue
					}
					err := s.send(stream, r.buf, r.offset, r.flush, r.finishWrite, firstSend)
					firstSend = false
					if err != nil {
						return err
					}
					if r.finishWrite {
						stream.CloseSend()
						return nil
					}
				}
			}
		}()
		close(sendDone)
	}()

	go func() {
		recvErr = func() error {
			for {
				resp, err := stream.Recv()
				if err != nil {
					return s.maybeHandleRedirectionError(err)
				}
				s.maybeUpdateFirstMessage(resp)

				if c := completion(resp); c != nil {
					cs.completions <- *c
				}
			}
		}()
		close(recvDone)
	}()

	<-sendDone
	<-recvDone
	// Prefer recvErr since that's where RPC errors are delivered
	if recvErr != nil {
		s.streamErr = recvErr
	} else if sendErr != nil {
		s.streamErr = sendErr
	}
	if s.streamErr == io.EOF {
		s.streamErr = nil
	}
	close(cs.completions)
}

type gRPCAppendTakeoverBidiWriteBufferSender struct {
	gRPCAppendBidiWriteBufferSender
	takeoverReported         bool
	handleTakeoverCompletion func(gRPCBidiWriteCompletion)
}

func writeObjectSpecAsAppendObjectSpec(s *storagepb.WriteObjectSpec, gen int64) *storagepb.AppendObjectSpec {
	return &storagepb.AppendObjectSpec{
		Bucket:                   s.GetResource().GetBucket(),
		Object:                   s.GetResource().GetName(),
		Generation:               gen,
		IfMetagenerationMatch:    s.IfMetagenerationMatch,
		IfMetagenerationNotMatch: s.IfMetagenerationNotMatch,
	}
}

// Use for a takeover of an appendable object.
func (w *gRPCWriter) newGRPCAppendTakeoverWriteBufferSender() *gRPCAppendTakeoverBidiWriteBufferSender {
	return &gRPCAppendTakeoverBidiWriteBufferSender{
		gRPCAppendBidiWriteBufferSender: gRPCAppendBidiWriteBufferSender{
			raw:    w.c.raw,
			bucket: w.bucket,
			firstMessage: &storagepb.BidiWriteObjectRequest{
				FirstMessage: &storagepb.BidiWriteObjectRequest_AppendObjectSpec{
					AppendObjectSpec: writeObjectSpecAsAppendObjectSpec(w.spec, w.appendGen),
				},
			},
			finalizeOnClose:     w.finalizeOnClose,
			takeoverWriter:      true,
			sendCRC32C:          w.sendCRC32C,
			disableAutoChecksum: w.disableAutoChecksum,
			objectAttrs:         w.attrs,
			fullObjectChecksum: func() uint32 {
				return w.fullObjectChecksum
			},
		},
		takeoverReported: false,
		handleTakeoverCompletion: func(c gRPCBidiWriteCompletion) {
			w.handleCompletion(c)
			w.setTakeoverOffset(c.flushOffset)
		},
	}
}

func (s *gRPCAppendTakeoverBidiWriteBufferSender) connect(ctx context.Context, cs gRPCBufSenderChans, opts ...gax.CallOption) {
	s.streamErr = nil
	ctx = gRPCWriteRequestParams{appendable: true, bucket: s.bucket, routingToken: s.routingToken}.apply(ctx)

	stream, err := s.raw.BidiWriteObject(ctx, opts...)
	if err != nil {
		s.streamErr = err
		close(cs.completions)
		return
	}

	// This blocks until we know the takeover offset from the server on first
	// connection
	firstSend := true
	if !s.takeoverReported {
		if err := s.send(stream, nil, 0, false, false, true); err != nil {
			s.streamErr = err
			close(cs.completions)
			return
		}
		firstSend = false

		resp, err := stream.Recv()
		if err != nil {
			// A Recv() error may be a redirect.
			s.streamErr = s.maybeHandleRedirectionError(err)
			close(cs.completions)
			return
		}

		c := completion(resp)
		if c == nil {
			s.streamErr = fmt.Errorf("storage: unexpectedly no size in initial takeover response %+v", resp)
			close(cs.completions)
			return
		}

		s.maybeUpdateFirstMessage(resp)
		s.takeoverReported = true
		s.handleTakeoverCompletion(*c)
	}

	go s.handleStream(stream, cs, firstSend)
}

func (s *gRPCAppendBidiWriteBufferSender) ensureFirstMessageAppendObjectSpec() {
	if s.firstMessage.GetWriteObjectSpec() != nil {
		w := s.firstMessage.GetWriteObjectSpec()
		s.firstMessage.FirstMessage = &storagepb.BidiWriteObjectRequest_AppendObjectSpec{
			AppendObjectSpec: &storagepb.AppendObjectSpec{
				Bucket:                   w.GetResource().GetBucket(),
				Object:                   w.GetResource().GetName(),
				IfMetagenerationMatch:    w.IfMetagenerationMatch,
				IfMetagenerationNotMatch: w.IfMetagenerationNotMatch,
			},
		}
	}
}

func (s *gRPCAppendBidiWriteBufferSender) maybeUpdateFirstMessage(resp *storagepb.BidiWriteObjectResponse) {
	// Any affirmative response should switch us to an AppendObjectSpec.
	s.ensureFirstMessageAppendObjectSpec()

	if r := resp.GetResource(); r != nil {
		aos := s.firstMessage.GetAppendObjectSpec()
		aos.Bucket = r.GetBucket()
		aos.Object = r.GetName()
		aos.Generation = r.GetGeneration()
	}

	if h := resp.GetWriteHandle(); h != nil {
		s.firstMessage.GetAppendObjectSpec().WriteHandle = h
	}
}

type bidiWriteObjectRedirectionError struct{}

func (e bidiWriteObjectRedirectionError) Error() string {
	return ""
}

func (s *gRPCAppendBidiWriteBufferSender) maybeHandleRedirectionError(err error) error {
	if st, ok := status.FromError(err); ok && st.Code() == codes.Aborted {
		for _, d := range st.Details() {
			if e, ok := d.(*storagepb.BidiWriteObjectRedirectedError); ok {
				if e.RoutingToken == nil {
					// This shouldn't happen, but we don't want to blindly retry here.
					// Instead, surface the error to the caller.
					return err
				}

				if e.WriteHandle != nil {
					// If we get back a write handle, we should use it. We can only use it
					// on an append object spec.
					s.ensureFirstMessageAppendObjectSpec()
					s.firstMessage.GetAppendObjectSpec().WriteHandle = e.WriteHandle
					// Generation is meant to only come with the WriteHandle, so ignore it
					// otherwise.
					if e.Generation != nil {
						s.firstMessage.GetAppendObjectSpec().Generation = e.GetGeneration()
					}
				}

				s.routingToken = e.RoutingToken
				return fmt.Errorf("%w%w", bidiWriteObjectRedirectionError{}, err)
			}
		}
	}
	return err
}

func (s *gRPCAppendBidiWriteBufferSender) send(stream storagepb.Storage_BidiWriteObjectClient, buf []byte, offset int64, flush, finishWrite, sendFirstMessage bool) error {
	finalizeObject := finishWrite && s.finalizeOnClose
	flush = flush || finishWrite
	r := gRPCBidiWriteRequest{
		buf:         buf,
		offset:      offset,
		flush:       flush,
		finishWrite: finalizeObject,
	}

	var bufChecksum *uint32
	if !s.disableAutoChecksum {
		bufChecksum = proto.Uint32(crc32.Checksum(r.buf, crc32cTable))
	}
	objectChecksums := getObjectChecksums(&getObjectChecksumsParams{
		sendCRC32C:          s.sendCRC32C,
		objectAttrs:         s.objectAttrs,
		fullObjectChecksum:  s.fullObjectChecksum,
		disableAutoChecksum: s.disableAutoChecksum,
		finishWrite:         finalizeObject,
		takeoverWriter:      s.takeoverWriter,
	})
	req := bidiWriteObjectRequest(r, bufChecksum, objectChecksums)
	if sendFirstMessage {
		proto.Merge(req, s.firstMessage)
	}

	return stream.Send(req)
}

func checkCanceled(err error) error {
	if status.Code(err) == codes.Canceled {
		return context.Canceled
	}

	return err
}

// gRPCChunkSize returns the chunk size to use based on the requested chunk
// size.
//
// The chunk size returned is always greater than 0 and a multiple of
// googleapi.MinUploadChunkSize
func gRPCChunkSize(requestSize int) int {
	size := googleapi.MinUploadChunkSize
	if requestSize > size {
		size = requestSize
	}

	if size%googleapi.MinUploadChunkSize != 0 {
		size += googleapi.MinUploadChunkSize - (size % googleapi.MinUploadChunkSize)
	}
	return size
}

type gRPCWriteRequestParams struct {
	appendable   bool
	bucket       string
	routingToken *string
}

func (p gRPCWriteRequestParams) apply(ctx context.Context) context.Context {
	hds := make([]string, 0, 3)
	if p.appendable {
		hds = append(hds, "appendable=true")
	}
	if p.bucket != "" {
		hds = append(hds, fmt.Sprintf("bucket=projects/_/buckets/%s", url.QueryEscape(p.bucket)))
	}
	if p.routingToken != nil {
		hds = append(hds, fmt.Sprintf("routing_token=%s", *p.routingToken))
	}
	return gax.InsertMetadataIntoOutgoingContext(ctx, "x-goog-request-params", strings.Join(hds, "&"))
}

func withBidiWriteObjectRedirectionErrorRetries(s *settings) (newr *retryConfig) {
	oldr := s.retry
	newr = oldr.clone()
	if newr == nil {
		newr = &retryConfig{}
	}
	if (oldr.policy == RetryIdempotent && !s.idempotent) || oldr.policy == RetryNever {
		// We still retry redirection errors even when settings indicate not to
		// retry.
		//
		// The protocol requires us to respect redirection errors, so RetryNever has
		// to ignore them.
		//
		// Idempotency is always protected by redirection errors: they either
		// contain a handle which can be used as idempotency information, or they do
		// not contain a handle and are "affirmative failures" which indicate that
		// no server-side action occurred.
		newr.policy = RetryAlways
		newr.shouldRetry = func(err error) bool {
			return errors.Is(err, bidiWriteObjectRedirectionError{})
		}
		return newr
	}
	// If retry settings allow retries normally, fall back to that behavior.
	newr.shouldRetry = func(err error) bool {
		if errors.Is(err, bidiWriteObjectRedirectionError{}) {
			return true
		}
		v := oldr.runShouldRetry(err)
		return v
	}
	return newr
}
