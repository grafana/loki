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
	"sync"
	"sync/atomic"
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
	// defaultWriteRetryInitialBackoff is the initial retry backoff for writes
	// when none is configured. It matches the HTTP writer, whose resumable
	// uploads default to this value.
	defaultWriteRetryInitialBackoff = 100 * time.Millisecond
	// maxPerMessageWriteSize is the maximum amount of content that can be sent
	// per WriteObjectRequest message. A buffer reaching this amount will
	// precipitate a flush of the buffer. It is only used by the gRPC Writer
	// implementation.
	maxPerMessageWriteSize int = int(storagepb.ServiceConstants_MAX_WRITE_CHUNK_BYTES)
)

// errStallTimeout is returned when a gRPC write stream or handshake call
// exceeds Writer.ChunkTransferTimeout without server acknowledgement.
var errStallTimeout = errors.New("storage: chunk transfer timeout")

// RPCs a gRPC write stall can occur in. They name the stage in
// stallTimeoutError and the rpc.method attribute of the stall metric.
const (
	rpcStartResumableWrite = "StartResumableWrite"
	rpcQueryWriteStatus    = "QueryWriteStatus"
	rpcBidiWriteObject     = "BidiWriteObject"
)

type stallTimeoutError struct {
	timeout time.Duration
	stage   string
}

func (e *stallTimeoutError) Error() string {
	if e.stage != "" {
		return fmt.Sprintf("%s (%s exceeded %s)", errStallTimeout.Error(), e.stage, e.timeout)
	}
	return fmt.Sprintf("%s (exceeded %s)", errStallTimeout.Error(), e.timeout)
}

func (e *stallTimeoutError) Unwrap() error {
	return errStallTimeout
}

func (e *stallTimeoutError) Temporary() bool {
	return true
}

func (e *stallTimeoutError) Timeout() bool {
	return true
}

func (w *gRPCWriter) Write(p []byte) (n int, err error) {
	done := make(chan struct{})
	cmd := &gRPCWriterCommandWrite{p: p, done: done}
	select {
	case <-w.donec:
		return 0, w.streamResult
	case w.writesChan <- cmd:
		md5Provided := w.attrs != nil && w.attrs.MD5 != nil
		// Update fullObjectChecksum on every write and send it on finalWrite if not disabled.
		// Skip checksum calculation if user configures MD5 or CRC32C themselves.
		if !w.disableAutoChecksum &&
			!w.sendCRC32C &&
			!md5Provided &&
			!w.append {
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

func (w *gRPCWriter) setAppendFinalCRC32C(sendAppendFinalCRC32C bool, c uint32) {
	w.sendAppendFinalCRC32C = sendAppendFinalCRC32C
	w.appendFinalCRC32C = c
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

		buf:              nil, // Allocated lazily on first buffered write.
		chunkSize:        chunkSize,
		writeQuantum:     writeQuantum,
		lastSegmentStart: lastSegmentStart,
		sendableUnits:    sendableUnits,
		bufUnsentIdx:     0,
		bufFlushedIdx:    0,
		bufBaseOffset:    0,
		awaitingFirstAck: true,

		budget: chunkRetryBudget{
			deadline: chunkRetryDeadline,
			now:      time.Now,
		},
		chunkTransferTimeout: params.chunkTransferTimeout,
		lastErr:              nil,
		streamSender:         nil,

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
		writerRetry := w.writerRetryConfig()
		w.streamResult = checkCanceled(run(w.preRunCtx, func(ctx context.Context) error {
			w.lastErr = w.writeLoop(ctx)
			return w.lastErr
		}, writerRetry, w.settings.idempotent, withOperation("WriteObject"), withBucket(w.bucket), withObject(w.attrs.Name), withProgressReset(w.budget.consumeProgress)))
		w.setError(w.streamResult)
		close(w.donec)
	}()

	return w, nil
}

// gRPCWriter is a wrapper around the gRPC client-stream API that manages
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

	fullObjectChecksum    uint32
	appendFinalCRC32C     uint32
	sendAppendFinalCRC32C bool

	flushSupported        bool
	sendCRC32C            bool
	disableAutoChecksum   bool
	forceOneShot          bool
	forceEmptyContentType bool
	append                bool
	appendGen             int64
	finalizeOnClose       bool

	buf       []byte
	chunkSize int
	// A writeQuantum is the largest quantity of data which can be sent to the
	// service in a single message.
	writeQuantum     int
	lastSegmentStart int
	sendableUnits    int
	bufUnsentIdx     int
	bufFlushedIdx    int
	bufBaseOffset    int64
	// awaitingFirstAck is true until the server has confirmed any offset,
	// including offset 0. Flush waits for that first confirmation.
	awaitingFirstAck bool

	budget               chunkRetryBudget
	chunkTransferTimeout time.Duration
	lastErr              error
	streamSender         gRPCBidiWriteBufferSender
	watchdog             *writeStallWatchdog

	// Communication from the user goroutine to the stream management goroutines
	writesChan     chan gRPCWriterCommand
	currentCommand gRPCWriterCommand
	streamResult   error
	donec          chan struct{}
}

// chunkRetryBudget bounds how long the writer keeps retrying without the
// server confirming new bytes. The stopwatch runs only while data is in
// flight (see gRPCWriter.isActive) and restarts whenever a completion
// confirms strictly more bytes than before.
//
// All methods must be called from the writer goroutine.
type chunkRetryBudget struct {
	// deadline is the configured ChunkRetryDeadline. Zero disables the budget.
	deadline time.Duration
	// now returns the current time; nil means time.Now.
	now func() time.Time

	// expiresAt is zero while the stopwatch is stopped.
	expiresAt time.Time
	// attempts counts writeLoop invocations since the last confirmed progress.
	attempts int
	// progressMade is set by recordProgress and cleared by consumeProgress.
	progressMade bool
}

func (b *chunkRetryBudget) clock() time.Time {
	if b.now == nil {
		return time.Now()
	}
	return b.now()
}

func (b *chunkRetryBudget) enabled() bool { return b.deadline > 0 }

// start arms the stopwatch if it is not already running.
func (b *chunkRetryBudget) start() {
	if b.enabled() && b.expiresAt.IsZero() {
		b.expiresAt = b.clock().Add(b.deadline)
	}
}

// stop clears the stopwatch so idle time does not count against the chunk.
func (b *chunkRetryBudget) stop() { b.expiresAt = time.Time{} }

// expired reports whether a running stopwatch has elapsed.
func (b *chunkRetryBudget) expired() bool {
	return !b.expiresAt.IsZero() && b.clock().After(b.expiresAt)
}

// recordProgress resets the budget after the server confirmed new bytes and
// restarts the stopwatch if data is still in flight.
func (b *chunkRetryBudget) recordProgress(stillActive bool) {
	b.stop()
	b.attempts = 0
	b.progressMade = true
	if stillActive {
		b.start()
	}
}

// consumeProgress reports and clears whether progress was recorded since the
// last call. run() uses it, via withProgressReset, to reset its own attempt
// counter and backoff.
func (b *chunkRetryBudget) consumeProgress() bool {
	if b.progressMade {
		b.progressMade = false
		return true
	}
	return false
}

// chunkRetryDeadlineError is returned by writeLoop once ChunkRetryDeadline
// elapses without the server confirming new bytes. It wraps the last transport
// error for errors.Is/As; writerRetryConfig treats it as terminal regardless
// of what it wraps.
type chunkRetryDeadlineError struct {
	deadline time.Duration
	attempts int
	err      error
}

func (e *chunkRetryDeadlineError) Error() string {
	return fmt.Sprintf("storage: retry deadline of %s reached after %v attempts; last error: %v", e.deadline, e.attempts, e.err)
}

func (e *chunkRetryDeadlineError) Unwrap() error { return e.err }

// isChunkRetryDeadlineError reports whether err is, or wraps, a
// chunkRetryDeadlineError.
func isChunkRetryDeadlineError(err error) bool {
	var target *chunkRetryDeadlineError
	return errors.As(err, &target)
}

// usesOneshotSender reports whether the writer uploads with a single
// BidiWriteObject call and no upload session. Must match pickBufferSender.
func (w *gRPCWriter) usesOneshotSender() bool {
	return w.forceOneShot && !w.append
}

// flushOutstanding reports whether a flush has been sent that GCS has not yet
// fully acknowledged. In the buffered path a flush is sent exactly when w.buf
// is full and entirely sent, and w.buf is cleared once that flush is acked.
// Unflushed data does not count: GCS does not acknowledge it, so the writer is
// waiting on the application to complete the chunk, not on the server.
func (w *gRPCWriter) flushOutstanding() bool {
	n := len(w.buf)
	return n > 0 && n == cap(w.buf) && w.bufUnsentIdx == n
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

		w.budget.start()

		req := gRPCBidiWriteRequest{
			buf:    buf[:q],
			offset: baseOffset + int64(sent),
			flush:  q == flushAt-sent,
		}
		if req.flush && w.watchdog != nil {
			w.watchdog.resume()
		}
		if !cs.deliverRequestUnlessCompleted(req, handleCompletion) {
			return baseOffset + int64(sent), false
		}
		buf = buf[q:]
		sent += q
	}
	return baseOffset + int64(sent), true
}

func (w *gRPCWriter) isActive() bool {
	hasUnackedData := w.bufUnsentIdx > 0 && w.bufUnsentIdx > w.bufFlushedIdx
	unsentBytes := len(w.buf) - w.bufUnsentIdx
	if unsentBytes < 0 {
		unsentBytes = 0
	}
	hasReadyData := unsentBytes >= w.writeQuantum
	return w.currentCommand != nil || hasUnackedData || hasReadyData || len(w.writesChan) > 0
}

func (w *gRPCWriter) handleCompletion(c gRPCBidiWriteCompletion) {
	if c.resource != nil {
		w.setObj(newObjectFromProto(c.resource))
	}

	// prevConfirmed is the highest offset GCS had confirmed before this
	// completion; it is meaningless until the first completion.
	prevConfirmed := w.bufBaseOffset + int64(w.bufFlushedIdx)
	if !w.awaitingFirstAck && c.flushOffset <= prevConfirmed {
		// Already handled this completion.
		return
	}
	w.awaitingFirstAck = false

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

	// Only a completion strictly past prevConfirmed counts as progress: a
	// reconnect that reports the same persisted size must not reset the retry
	// budget. The stopwatch restarts only if more data is waiting to be sent.
	progressed := c.flushOffset > prevConfirmed || c.resource != nil
	if progressed {
		w.budget.recordProgress(w.isActive())
	}

	// Restart the stall timer on forward progress. If nothing remains
	// outstanding, the write loop pauses it before waiting on the application.
	if progressed && w.watchdog != nil {
		w.watchdog.reset()
	}
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
			// If zero-copy one-shot is requested, OR the payload is larger than the buffer,
			// bypass buffering entirely and hand off to the writeLoop immediately.
			if w.forceOneShot || len(w.buf)+len(v.p) > w.chunkSize {
				w.currentCommand = cmd
				return nil
			}

			// Otherwise, lazily allocate and stage the small write (normal buffered path)
			if w.buf == nil {
				w.buf = make([]byte, 0, w.chunkSize)
			}
			// We have not started sending yet, and we can stage all data without
			// starting a send. Compare against w.chunkSize instead of
			// w.writeQuantum: that way we can perform a oneshot upload for objects
			// which fit in one chunk, even though we will cut the request into
			// w.writeQuantum units when we do start sending.
			origLen := len(w.buf)
			w.buf = w.buf[:origLen+len(v.p)]
			copy(w.buf[origLen:], v.p)
			close(v.done)
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

func (w *gRPCWriter) writeLoop(ctx context.Context) (retErr error) {
	w.budget.attempts++
	if w.isActive() {
		w.budget.start()
		// Give up if we have been retrying the same chunk for too long.
		if w.budget.expired() {
			return &chunkRetryDeadlineError{deadline: w.budget.deadline, attempts: w.budget.attempts, err: w.lastErr}
		}
	} else {
		w.budget.stop()
	}
	// Allow each request in w.buf to be sent and result in a completion without
	// blocking.
	requests := make(chan gRPCBidiWriteRequest, w.sendableUnits)
	completions := make(chan gRPCBidiWriteCompletion, w.sendableUnits)
	// Only one request ack will be outstanding at a time.
	requestAcks := make(chan struct{}, 1)
	chcs := gRPCWriterCommandHandleChans{requests, requestAcks, completions}
	bscs := gRPCBufSenderChans{requests, requestAcks, completions}
	attemptCtx, attemptCancel := context.WithCancel(ctx)
	defer attemptCancel()

	// Oneshot uploads are excluded: there is no upload session, so a commit
	// abandoned by a stall-cancelled attempt could still land after the retry.
	if w.chunkTransferTimeout > 0 && !w.usesOneshotSender() {
		stage := rpcBidiWriteObject
		if s, ok := w.streamSender.(interface{ connectStage() string }); ok {
			stage = s.connectStage()
		}
		watchdog := newWriteStallWatchdog(w.chunkTransferTimeout, stage, func(stage string) {
			if w.c != nil && w.c.metrics != nil {
				target := stripPort(metricsStateFromContext(w.preRunCtx).getTarget())
				w.c.metrics.recordStallDuration(attemptCtx, w.chunkTransferTimeout, stage, "grpc", target)
			}
			attemptCancel()
		})
		w.watchdog = watchdog
		defer func() {
			watchdog.stop()
			// Report a stall only if it is what ended a failed attempt.
			if retErr != nil && ctx.Err() == nil && watchdog.isStalled() {
				retErr = &stallTimeoutError{
					timeout: w.chunkTransferTimeout,
					stage:   watchdog.currentStage(),
				}
			}
			w.watchdog = nil
		}()
		// Stage 1: StartResumableWrite, QueryWriteStatus, and initial BidiWriteObject
		// stream connection share the ChunkTransferTimeout duration.
		watchdog.resume()
	}

	w.streamSender.connect(attemptCtx, bscs, w.settings.gax...)
	if w.watchdog != nil {
		w.watchdog.setStage(rpcBidiWriteObject)
	}

	// Drain any initial completions (like QueryWriteStatus results).
Loop:
	for {
		select {
		case c, ok := <-completions:
			if !ok {
				return w.streamSender.err()
			}
			w.handleCompletion(c)
		default:
			break Loop
		}
	}
	if w.watchdog != nil {
		w.watchdog.pause()
	}

	if w.bufFlushedIdx > 0 {
		copy(w.buf, w.buf[w.bufFlushedIdx:])
		w.buf = w.buf[:len(w.buf)-w.bufFlushedIdx]
		w.bufBaseOffset += int64(w.bufFlushedIdx)
		w.bufUnsentIdx -= w.bufFlushedIdx
		if w.bufUnsentIdx < 0 {
			w.bufUnsentIdx = 0
		}
		w.bufFlushedIdx = 0
	}

	// Send any full quantum in w.buf, possibly including a flush
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

	err := func() error {
		for {
			if w.currentCommand != nil {
				if err := w.currentCommand.handle(w, chcs); err != nil {
					return err
				}
				w.currentCommand = nil
				// Stop the stopwatch if we are completely idle and waiting for the user's next Write() call.
				if !w.isActive() {
					w.budget.stop()
				}
			}
			if w.watchdog != nil && !w.flushOutstanding() {
				w.watchdog.pause()
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

	finalOffset := int64(-1)
	if closeErr.err == nil {
		// Clean shutdown. Send any remaining tail.
		w.budget.start()
		if w.watchdog != nil {
			w.watchdog.resume()
		}

		req := gRPCBidiWriteRequest{
			buf:         w.buf[w.bufUnsentIdx:],
			offset:      w.bufBaseOffset + int64(w.bufUnsentIdx),
			flush:       true,
			finishWrite: true,
		}
		finalOffset = req.offset + int64(len(req.buf))
		if !chcs.deliverRequestUnlessCompleted(req, w.handleCompletion) {
			return w.streamSender.err()
		}
	} else {
		// Unclean shutdown. Cancel the context so we clean up expeditiously.
		attemptCancel()
	}

	close(requests)
	finalizes := !w.append || w.finalizeOnClose
	for c := range completions {
		w.handleCompletion(c)
		// Once every byte is acknowledged and, if the close finalizes, the
		// object has been returned, only stream teardown remains.
		if w.watchdog != nil && c.flushOffset >= finalOffset && (c.resource != nil || !finalizes) {
			w.watchdog.pause()
		}
	}
	if closeErr.err == nil {
		return w.streamSender.err()
	}
	return closeErr.err
}

type writeStallWatchdog struct {
	mu      sync.Mutex
	timeout time.Duration
	timer   *time.Timer
	// gen identifies the current timer. Every arm and disarm increments it.
	// time.Timer.Stop cannot cancel a callback that has already started, so
	// a callback that was waiting on mu when its timer was stopped compares
	// its generation against gen and returns without firing.
	gen           uint64
	running       bool
	stallOccurred bool
	// stage names the RPC being timed. It is frozen once a stall fires.
	stage   string
	onStall func(stage string)
}

func newWriteStallWatchdog(timeout time.Duration, stage string, onStall func(stage string)) *writeStallWatchdog {
	return &writeStallWatchdog{
		timeout: timeout,
		stage:   stage,
		onStall: onStall,
	}
}

// setStage names the RPC being timed. It has no effect after a stall.
func (w *writeStallWatchdog) setStage(stage string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stallOccurred {
		w.stage = stage
	}
}

// currentStage returns the RPC being timed, or the one that stalled.
func (w *writeStallWatchdog) currentStage() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stage
}

// arm stops any current timer and starts a new one for the full timeout.
// w.mu must be held.
func (w *writeStallWatchdog) arm() {
	w.disarm()
	w.running = true
	gen := w.gen
	w.timer = time.AfterFunc(w.timeout, func() { w.fire(gen) })
}

// disarm stops the current timer and invalidates its callback. w.mu must be
// held.
func (w *writeStallWatchdog) disarm() {
	if w.timer != nil {
		w.timer.Stop()
	}
	w.gen++
	w.running = false
}

func (w *writeStallWatchdog) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stallOccurred {
		return
	}
	w.arm()
}

func (w *writeStallWatchdog) pause() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stallOccurred || !w.running {
		return
	}
	w.disarm()
}

func (w *writeStallWatchdog) resume() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stallOccurred || w.running {
		return
	}
	w.arm()
}

func (w *writeStallWatchdog) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.disarm()
}

func (w *writeStallWatchdog) isStalled() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stallOccurred
}

// fire is the timer callback for generation gen.
func (w *writeStallWatchdog) fire(gen uint64) {
	w.mu.Lock()
	if gen != w.gen || !w.running || w.stallOccurred {
		w.mu.Unlock()
		return
	}
	w.stallOccurred = true
	w.running = false
	onStall, stage := w.onStall, w.stage
	w.mu.Unlock()

	if onStall != nil {
		onStall(stage)
	}
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
	p             []byte
	done          chan struct{}
	initialOffset int64
	hasStarted    bool
	closeOnce     sync.Once
}

func (c *gRPCWriterCommandWrite) handle(w *gRPCWriter, cs gRPCWriterCommandHandleChans) error {
	if len(c.p) == 0 {
		// No data to write.
		c.markDone()
		return nil
	}

	if !c.hasStarted {
		c.initialOffset = w.bufBaseOffset + int64(len(w.buf))
		c.hasStarted = true
	} else {
		// Retrying this command; check if server has persisted some bytes of this command's payload.
		bytesPersisted := w.bufBaseOffset - c.initialOffset
		if bytesPersisted > 0 {
			if int64(len(c.p)) < bytesPersisted {
				bytesPersisted = int64(len(c.p))
			}
			c.p = c.p[bytesPersisted:]
			c.initialOffset = w.bufBaseOffset

			if len(c.p) == 0 {
				c.markDone()
				return nil
			}
		}
	}

	// Zero-Copy send.
	if w.forceOneShot {
		err := c.zeroCopyWrite(w, cs)
		if err != nil {
			return err
		}
		// If zeroCopyWrite returns without error, the write is done.
		return nil
	}

	if w.buf == nil {
		w.buf = make([]byte, 0, w.chunkSize)
	}
	wblen := len(w.buf)
	allKnownBytes := wblen + len(c.p)
	fullBufs := allKnownBytes / cap(w.buf)
	if allKnownBytes%cap(w.buf) == 0 {
		// If we would exactly fill some number of cap(w.buf) units, we don't need
		// to block on the flush for the last one. We know that c.p is not empty, so
		// allKnownBytes is not 0 and therefore fullBufs is not 0.
		fullBufs--
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
		c.markDone()
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
	c.initialOffset += int64(copied)
	firstFullBufFromCmd := cap(w.buf) - len(w.buf)

	sending := w.buf[w.bufUnsentIdx:]
	if _, ok := w.sendBufferToTarget(cs, sending, w.bufBaseOffset+int64(w.bufUnsentIdx), cap(sending),
		w.handleCompletion); !ok {
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
		c.initialOffset += int64(trim)
		cmdBaseOffset = bufTail
	}
	offset := cmdBaseOffset
	sentOffset, ok := w.sendBufferToTarget(cs, cmdBuf, offset, firstFullBufFromCmd,
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
	for ackOutstanding || w.awaitingFirstAck || (w.bufBaseOffset+int64(w.bufFlushedIdx)) < offset {
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
	c.markDone()
	return nil
}

func (c *gRPCWriterCommandWrite) zeroCopyWrite(w *gRPCWriter, cs gRPCWriterCommandHandleChans) error {
	// Pre-emptively get the context channel to avoid closure overhead in the loop.
	ctxDone := w.preRunCtx.Done()

	// sendBufferToTarget handles the quantum breakdown.
	newOffset, ok := w.sendBufferToTarget(cs, c.p, w.bufBaseOffset, len(c.p), w.handleCompletion)
	if !ok {
		return w.streamSender.err()
	}

	// Request an ack from the sender goroutine to ensure the buffer has been
	// dispatched to gRPC and is safe for the user to reuse.
	if !cs.deliverRequestUnlessCompleted(gRPCBidiWriteRequest{requestAck: true}, w.handleCompletion) {
		return w.streamSender.err()
	}

	ackOutstanding := true

	// Wait for server acknowledgement and sender transmissions to enable incremental progress.
	for ackOutstanding || w.bufBaseOffset < newOffset {
		select {
		case completion, ok := <-cs.completions:
			if !ok {
				return w.streamSender.err()
			}
			w.handleCompletion(completion)
		case <-cs.requestAcks:
			ackOutstanding = false
		case <-ctxDone:
			return w.preRunCtx.Err()
		}
	}

	c.p = nil
	c.markDone()
	return nil
}

// Helper to ensure we don't close done twice and keep the main logic clean.
func (c *gRPCWriterCommandWrite) markDone() {
	c.closeOnce.Do(func() { close(c.done) })
}

type gRPCWriterCommandFlush struct {
	done chan int64
}

func (c *gRPCWriterCommandFlush) handle(w *gRPCWriter, cs gRPCWriterCommandHandleChans) error {
	flushTarget := w.bufBaseOffset + int64(len(w.buf))
	// We know that there are at most w.writeQuantum bytes in
	// w.buf[w.bufUnsentIdx:], because we send anything more inline when handling
	// a write.
	w.budget.start()
	if w.watchdog != nil {
		w.watchdog.resume()
	}

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
	for w.awaitingFirstAck || (w.bufBaseOffset+int64(w.bufFlushedIdx)) < flushTarget {
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
	fullObjectChecksum  func() *uint32
	finishWrite         bool
	append              bool
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

	// For append operations, send user's final append checksum on last write op if available.
	// Auto checksum is not supported for appendable writes.
	var crc32c *uint32
	if params.fullObjectChecksum != nil {
		crc32c = params.fullObjectChecksum()
	}

	if params.append && crc32c != nil {
		return &storagepb.ObjectChecksums{Crc32C: crc32c}
	}

	// send user's checksum on last write op if available
	if params.sendCRC32C || (params.objectAttrs != nil && params.objectAttrs.MD5 != nil) {
		return toProtoChecksums(params.sendCRC32C, params.objectAttrs)
	}

	if params.append || params.disableAutoChecksum || params.fullObjectChecksum == nil {
		return nil
	}

	if crc32c != nil {
		return &storagepb.ObjectChecksums{Crc32C: crc32c}
	}
	return nil
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

	// canResumeSession reports whether an upload session (resumable upload ID
	// or appendable write handle / takeover generation) has been established,
	// making subsequent reconnects idempotent even without initial object
	// preconditions.
	canResumeSession() bool
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
	fullObjectChecksum  func() *uint32
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
		fullObjectChecksum: func() *uint32 {
			checksum := w.fullObjectChecksum
			return &checksum
		},
	}
}

func (s *gRPCOneshotBidiWriteBufferSender) err() error             { return s.streamErr }
func (s *gRPCOneshotBidiWriteBufferSender) canResumeSession() bool { return false }

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
							proto.Merge(req, s.firstMessage)
							firstSend = false
						}

						if err := stream.Send(req); err != nil {
							return err
						}

						if r.finishWrite {
							stream.CloseSend()
							return nil
						}

						// Oneshot uploads assume all flushes succeed.
						if r.flush {
							select {
							case cs.completions <- gRPCBidiWriteCompletion{flushOffset: r.offset + int64(len(r.buf))}:
							case <-stream.Context().Done():
								return stream.Context().Err()
							}
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
						select {
						case cs.completions <- *c:
						case <-stream.Context().Done():
							return stream.Context().Err()
						}
					}
				}
			}()
			close(recvDone)
		}()

		<-sendDone
		<-recvDone
		s.streamErr = pickStreamError(recvErr, sendErr)
		close(cs.completions)
	}()
}

type gRPCResumableBidiWriteBufferSender struct {
	raw    *gapic.Client
	bucket string

	startWriteRequest *storagepb.StartResumableWriteRequest
	upid              string

	chunkDelay *dynamicDelay

	// Checksum related settings.
	sendCRC32C          bool
	disableAutoChecksum bool
	objectAttrs         *ObjectAttrs
	fullObjectChecksum  func() *uint32

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
		chunkDelay:          newWriteChunkDelay(w.chunkSize, w.chunkTransferTimeout),
		sendCRC32C:          w.sendCRC32C,
		disableAutoChecksum: w.disableAutoChecksum,
		objectAttrs:         w.attrs,
		fullObjectChecksum: func() *uint32 {
			checksum := w.fullObjectChecksum
			return &checksum
		},
	}
}

func (s *gRPCResumableBidiWriteBufferSender) err() error             { return s.streamErr }
func (s *gRPCResumableBidiWriteBufferSender) canResumeSession() bool { return s.upid != "" }

// connectStage names the RPC that connect blocks on before it opens the
// stream.
func (s *gRPCResumableBidiWriteBufferSender) connectStage() string {
	if s.startWriteRequest != nil {
		return rpcStartResumableWrite
	}
	return rpcQueryWriteStatus
}

type flushTiming struct {
	sentAt       time.Time
	sendDuration time.Duration
}

// resumableStream encapsulates a single BidiWriteObject gRPC stream and its
// background receiver goroutine for a resumable upload session.
//
// Adaptive stream rotation state machine:
//  1. Active: The sender loop streams write quantums (r.flush == false) and
//     flushes (r.flush == true) over st, while the receiver goroutine reads
//     persisted_size acks, records chunk latency (sendDuration + commitWait),
//     and sets rotate = true when latency exceeds the adaptive threshold.
//  2. Drain & Half-Close (closeAndWait): At the next chunk boundary
//     (!r.finishWrite && !unflushedDataSent) when rotate is true, closeAndWait
//     calls CloseSend() and waits for the receiver goroutine to drain any
//     remaining persisted_size acks until the server ends the stream with
//     io.EOF (the normal gRPC response to CloseSend()).
//  3. Re-attach (openStream): Once drained, openStream opens a fresh
//     BidiWriteObject stream with the existing UploadId without calling
//     QueryWriteStatus. The current stream is only replaced on success.
type resumableStream struct {
	st      storagepb.Storage_BidiWriteObjectClient
	flushes chan flushTiming
	done    chan struct{}
	rErr    error
}

func (rs *resumableStream) closeAndWait() error {
	rs.st.CloseSend()
	<-rs.done
	return rs.rErr
}

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

	var rotate atomic.Bool

	openStream := func() (*resumableStream, error) {
		st, err := s.raw.BidiWriteObject(ctx, opts...)
		if err != nil {
			return nil, err
		}
		rotate.Store(false)
		s.chunkDelay.resetWarmUp()
		rs := &resumableStream{
			st:      st,
			flushes: make(chan flushTiming, 16),
			done:    make(chan struct{}),
		}
		go func() {
			rs.rErr = func() error {
				var lastAck time.Time
				for {
					resp, err := st.Recv()
					if err != nil {
						return err
					}
					if c := completion(resp); c != nil {
						now := time.Now()
						select {
						case f := <-rs.flushes:
							waitStart := f.sentAt
							if lastAck.After(waitStart) {
								waitStart = lastAck
							}
							var commitWait time.Duration
							if now.After(waitStart) {
								commitWait = now.Sub(waitStart)
							}
							if s.chunkDelay.recordLatency(f.sendDuration + commitWait) {
								rotate.Store(true)
							}
						default:
						}
						lastAck = now
						select {
						case cs.completions <- *c:
						case <-st.Context().Done():
							return st.Context().Err()
						}
					}
				}
			}()
			close(rs.done)
		}()
		return rs, nil
	}

	cur, err := openStream()
	if err != nil {
		s.streamErr = err
		close(cs.completions)
		return
	}

	go func() {
		var sendErr error
		firstSend := true
		unflushedDataSent := false
		var chunkSendDuration time.Duration

		sendErr = func() error {
			for {
				select {
				case <-cur.done:
					// Because `requests` is not connected to the gRPC machinery, we
					// have to check for asynchronous termination on the receive side.
					return nil
				case r, ok := <-cs.requests:
					if !ok {
						cur.st.CloseSend()
						return nil
					}
					if r.requestAck {
						cs.requestAcks <- struct{}{}
						continue
					}

					if !r.finishWrite && !unflushedDataSent && rotate.Load() {
						// Half-close the current stream and wait for the receiver
						// goroutine to drain any remaining persisted_size acks.
						// Receiving io.EOF from Recv() is the normal gRPC response
						// once the server finishes replying to a half-closed stream.
						if err := cur.closeAndWait(); err != nil && err != io.EOF {
							return err
						}
						next, err := openStream()
						if err != nil {
							return err
						}
						cur = next
						firstSend = true
						chunkSendDuration = 0
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
					sendStart := time.Now()
					if r.flush && !r.finishWrite && s.chunkDelay != nil {
						select {
						case cur.flushes <- flushTiming{sentAt: sendStart, sendDuration: chunkSendDuration}:
						case <-cur.done:
							return nil
						}
					}
					if err := cur.st.Send(req); err != nil {
						return err
					}
					if r.finishWrite {
						cur.st.CloseSend()
						return nil
					}
					if s.chunkDelay != nil {
						chunkSendDuration += time.Since(sendStart)
						if s.chunkDelay.exceeds(chunkSendDuration) {
							rotate.Store(true)
						}
					}
					if r.flush {
						chunkSendDuration = 0
						unflushedDataSent = false
					} else if len(r.buf) > 0 {
						unflushedDataSent = true
					}
				}
			}
		}()

		<-cur.done
		s.streamErr = pickStreamError(cur.rErr, sendErr)
		close(cs.completions)
	}()
}

type gRPCAppendBidiWriteBufferSender struct {
	raw          *gapic.Client
	bucket       string
	routingToken *string

	firstMessage    *storagepb.BidiWriteObjectRequest
	finalizeOnClose bool

	// Checksum related settings.
	sendCRC32C          bool
	disableAutoChecksum bool
	objectAttrs         *ObjectAttrs
	fullObjectChecksum  func() *uint32

	takeoverWriter bool

	streamErr error
}

func (s *gRPCAppendBidiWriteBufferSender) err() error { return s.streamErr }
func (s *gRPCAppendBidiWriteBufferSender) canResumeSession() bool {
	return s.firstMessage.GetAppendObjectSpec().GetWriteHandle() != nil || s.takeoverWriter
}

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
		fullObjectChecksum: func() *uint32 {
			if !w.sendAppendFinalCRC32C {
				return nil
			}
			checksum := w.appendFinalCRC32C
			return &checksum
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
					select {
					case cs.completions <- *c:
					case <-stream.Context().Done():
						return stream.Context().Err()
					}
				}
			}
		}()
		close(recvDone)
	}()

	<-sendDone
	<-recvDone
	s.streamErr = pickStreamError(recvErr, sendErr)
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
			fullObjectChecksum: func() *uint32 {
				if !w.sendAppendFinalCRC32C {
					return nil
				}
				checksum := w.appendFinalCRC32C
				return &checksum
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
		append:              true,
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

func (w *gRPCWriter) writerRetryConfig() *retryConfig {
	oldr := w.settings.retry
	if oldr == nil {
		oldr = defaultRetry
	}
	newr := oldr.clone()
	// Writer does not use maxRetryDuration from retryConfig to maintain
	// consistency with HTTP client behavior. Writers should use
	// ChunkRetryDeadline for per-chunk timeouts and context for overall timeouts.
	newr.maxRetryDuration = 0
	if newr.backoff == nil {
		newr.backoff = &gax.Backoff{Initial: defaultWriteRetryInitialBackoff}
	}

	if oldr.policy == RetryNever {
		if w.append {
			// The protocol requires us to respect redirection errors even when
			// RetryNever is configured.
			newr.policy = RetryAlways
			newr.shouldRetry = func(err error, retryCtx *RetryContext) bool {
				if isChunkRetryDeadlineError(err) {
					return false
				}
				return errors.Is(err, bidiWriteObjectRedirectionError{})
			}
		}
		return newr
	}

	// Set policy to RetryAlways so run() enters the retry loop even when
	// w.settings.idempotent is initially false under RetryIdempotent. Once a
	// resumable upload ID (upid) or appendable WriteHandle is established,
	// resuming that session is idempotent and should be retried under
	// RetryIdempotent.
	newr.policy = RetryAlways
	newr.shouldRetry = func(err error, retryCtx *RetryContext) bool {
		// The per-chunk budget is exhausted; retrying would re-enter writeLoop
		// with the same expired budget. Checked before any user ErrorFunc.
		if isChunkRetryDeadlineError(err) {
			return false
		}
		if w.append && errors.Is(err, bidiWriteObjectRedirectionError{}) {
			return true
		}
		if oldr.policy == RetryIdempotent && !w.settings.idempotent &&
			!w.streamSender.canResumeSession() {
			return false
		}
		// Stalls are retried even if a custom error func rejects them, matching
		// the HTTP writer. Retries remain bounded by ChunkRetryDeadline.
		if errors.Is(err, errStallTimeout) {
			return true
		}
		return oldr.runShouldRetry(err, retryCtx)
	}
	return newr
}

// pickStreamError determines the final error to be reported by prioritizing recvErr.
// An io.EOF from a receiver is not considered an error.
func pickStreamError(recvErr, sendErr error) error {
	if recvErr != nil && recvErr != io.EOF {
		return recvErr
	}
	return sendErr
}
