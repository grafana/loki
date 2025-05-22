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
	"io"
	"time"

	gapic "cloud.google.com/go/storage/internal/apiv2"
	"cloud.google.com/go/storage/internal/apiv2/storagepb"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const defaultWriteChunkRetryDeadline = 32 * time.Second

type gRPCAppendBidiWriteBufferSender struct {
	bucket          string
	routingToken    *string
	raw             *gapic.Client
	settings        *settings
	stream          storagepb.Storage_BidiWriteObjectClient
	firstMessage    *storagepb.BidiWriteObjectRequest
	objectChecksums *storagepb.ObjectChecksums

	finalizeOnClose bool

	forceFirstMessage bool
	progress          func(int64)
	flushOffset       int64
	takeoverOffset    int64
	objResource       *storagepb.Object // Captures received obj to set w.Attrs.

	// Fields used to report responses from the receive side of the stream
	// recvs is closed when the current recv goroutine is complete. recvErr is set
	// to the result of that stream (including io.EOF to indicate success)
	recvs   <-chan *storagepb.BidiWriteObjectResponse
	recvErr error
}

// Use for a newly created appendable object.
func (w *gRPCWriter) newGRPCAppendableObjectBufferSender() (*gRPCAppendBidiWriteBufferSender, error) {
	s := &gRPCAppendBidiWriteBufferSender{
		bucket:   w.spec.GetResource().GetBucket(),
		raw:      w.c.raw,
		settings: w.c.settings,
		firstMessage: &storagepb.BidiWriteObjectRequest{
			FirstMessage: &storagepb.BidiWriteObjectRequest_WriteObjectSpec{
				WriteObjectSpec: w.spec,
			},
			CommonObjectRequestParams: toProtoCommonObjectRequestParams(w.encryptionKey),
		},
		objectChecksums:   toProtoChecksums(w.sendCRC32C, w.attrs),
		finalizeOnClose:   w.finalizeOnClose,
		forceFirstMessage: true,
		progress:          w.progress,
	}
	return s, nil
}

// Use for a takeover of an appendable object.
// Unlike newGRPCAppendableObjectBufferSender, this blocks until the stream is
// open because it needs to get the append offset from the server.
func (w *gRPCWriter) newGRPCAppendTakeoverWriteBufferSender(ctx context.Context) (*gRPCAppendBidiWriteBufferSender, error) {
	s := &gRPCAppendBidiWriteBufferSender{
		bucket:   w.spec.GetResource().GetBucket(),
		raw:      w.c.raw,
		settings: w.c.settings,
		firstMessage: &storagepb.BidiWriteObjectRequest{
			FirstMessage: &storagepb.BidiWriteObjectRequest_AppendObjectSpec{
				AppendObjectSpec: w.appendSpec,
			},
		},
		objectChecksums:   toProtoChecksums(w.sendCRC32C, w.attrs),
		finalizeOnClose:   w.finalizeOnClose,
		forceFirstMessage: true,
		progress:          w.progress,
	}
	if err := s.connect(ctx); err != nil {
		return nil, fmt.Errorf("storage: opening appendable write stream: %w", err)
	}
	_, err := s.sendOnConnectedStream(nil, 0, false, false, true)
	if err != nil {
		return nil, err
	}
	firstResp := <-s.recvs
	// Object resource is returned in the first response on takeover, so capture
	// this now.
	s.objResource = firstResp.GetResource()
	s.takeoverOffset = firstResp.GetResource().GetSize()
	return s, nil
}

func (s *gRPCAppendBidiWriteBufferSender) connect(ctx context.Context) (err error) {
	err = func() error {
		// If this is a forced first message, we've already determined it's safe to
		// send.
		if s.forceFirstMessage {
			s.forceFirstMessage = false
			return nil
		}

		// It's always ok to reconnect if there is a handle. This is the common
		// case.
		if s.firstMessage.GetAppendObjectSpec().GetWriteHandle() != nil {
			return nil
		}
		// Also always okay to reconnect if there is a generation.
		if s.firstMessage.GetAppendObjectSpec().GetGeneration() != 0 {
			return nil
		}

		// We can also reconnect if the first message has an if_generation_match or
		// if_metageneration_match condition. Note that negative conditions like
		// if_generation_not_match are not necessarily safe to retry.
		aos := s.firstMessage.GetAppendObjectSpec()
		wos := s.firstMessage.GetWriteObjectSpec()

		if aos != nil && aos.IfMetagenerationMatch != nil {
			return nil
		}

		if wos != nil && wos.IfGenerationMatch != nil {
			return nil
		}
		if wos != nil && wos.IfMetagenerationMatch != nil {
			return nil
		}

		// Otherwise, it is not safe to reconnect.
		return errors.New("cannot safely reconnect; no write handle or preconditions")
	}()
	if err != nil {
		return err
	}

	return s.startReceiver(ctx)
}

func (s *gRPCAppendBidiWriteBufferSender) withRequestParams(ctx context.Context) context.Context {
	param := fmt.Sprintf("appendable=true&bucket=%s", s.bucket)
	if s.routingToken != nil {
		param = param + fmt.Sprintf("&routing_token=%s", *s.routingToken)
	}
	return gax.InsertMetadataIntoOutgoingContext(ctx, "x-goog-request-params", param)
}

func (s *gRPCAppendBidiWriteBufferSender) startReceiver(ctx context.Context) (err error) {
	s.stream, err = s.raw.BidiWriteObject(s.withRequestParams(ctx), s.settings.gax...)
	if err != nil {
		return
	}

	recvs := make(chan *storagepb.BidiWriteObjectResponse)
	s.recvs = recvs
	s.recvErr = nil
	go s.receiveMessages(recvs)
	return
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
	return "BidiWriteObjectRedirectedError"
}

func (s *gRPCAppendBidiWriteBufferSender) handleRedirectionError(e *storagepb.BidiWriteObjectRedirectedError) bool {
	if e.RoutingToken == nil {
		// This shouldn't happen, but we don't want to blindly retry here. Instead,
		// surface the error to the caller.
		return false
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
	return true
}

func (s *gRPCAppendBidiWriteBufferSender) receiveMessages(resps chan<- *storagepb.BidiWriteObjectResponse) {
	resp, err := s.stream.Recv()
	for err == nil {
		s.maybeUpdateFirstMessage(resp)

		if resp.WriteStatus != nil {
			// We only get a WriteStatus if this was a solicited message (either
			// state_lookup: true or finish_write: true). Unsolicited messages may
			// arrive to update our handle if necessary. We don't want to block on
			// this channel write if this was an unsolicited message.
			resps <- resp
		}

		resp, err = s.stream.Recv()
	}

	if st, ok := status.FromError(err); ok && st.Code() == codes.Aborted {
		for _, d := range st.Details() {
			if e, ok := d.(*storagepb.BidiWriteObjectRedirectedError); ok {
				// If we can handle this error, replace it with the sentinel. Otherwise,
				// report it to the user.
				if ok := s.handleRedirectionError(e); ok {
					err = bidiWriteObjectRedirectionError{}
				}
			}
		}
	}

	// TODO: automatically reconnect on retriable recv errors, even if there are
	// no sends occurring.
	s.recvErr = err
	close(resps)
}

func (s *gRPCAppendBidiWriteBufferSender) sendOnConnectedStream(buf []byte, offset int64, flush, finishWrite, sendFirstMessage bool) (obj *storagepb.Object, err error) {
	var req *storagepb.BidiWriteObjectRequest
	finalizeObject := finishWrite && s.finalizeOnClose
	if finishWrite {
		// Always flush when finishing the Write, even if not finalizing.
		req = bidiWriteObjectRequest(buf, offset, true, finalizeObject)
	} else {
		req = bidiWriteObjectRequest(buf, offset, flush, false)
	}
	if finalizeObject {
		// appendable objects pass checksums on the finalize message only
		req.ObjectChecksums = s.objectChecksums
	}
	if sendFirstMessage {
		proto.Merge(req, s.firstMessage)
	}

	if err = s.stream.Send(req); err != nil {
		return nil, err
	}

	if finishWrite {
		s.stream.CloseSend()
		for resp := range s.recvs {
			if resp.GetResource() != nil {
				obj = resp.GetResource()
			}
			// When closing the stream, update the object resource to reflect
			// the persisted size. We get a new object from the stream if
			// the object was finalized, but not if it's unfinalized.
			if s.objResource != nil && resp.GetPersistedSize() > 0 {
				s.objResource.Size = resp.GetPersistedSize()
			}
		}
		if s.recvErr != io.EOF {
			return nil, s.recvErr
		}
		if obj.GetSize() > s.flushOffset {
			s.flushOffset = obj.GetSize()
			s.progress(s.flushOffset)
		}
		return
	}

	if flush {
		// We don't necessarily expect multiple responses for a single flush, but
		// this allows the server to send multiple responses if it wants to.
		flushOffset := s.flushOffset

		// Await a response on the stream. Loop at least once or until the
		// persisted offset matches the flush offset.
		for {
			resp, ok := <-s.recvs
			if !ok {
				return nil, s.recvErr
			}
			pSize := resp.GetPersistedSize()
			rSize := resp.GetResource().GetSize()
			if flushOffset < pSize {
				flushOffset = pSize
			}
			if flushOffset < rSize {
				flushOffset = rSize
			}
			// On the first flush, we expect to get an object resource back and
			// should return it.
			if resp.GetResource() != nil {
				obj = resp.GetResource()
			}
			if flushOffset <= offset+int64(len(buf)) {
				break
			}
		}
		if s.flushOffset < flushOffset {
			s.flushOffset = flushOffset
			s.progress(s.flushOffset)
		}
	}
	return
}

func (s *gRPCAppendBidiWriteBufferSender) sendBuffer(ctx context.Context, buf []byte, offset int64, flush, finishWrite bool) (obj *storagepb.Object, err error) {
	for {
		sendFirstMessage := false
		if s.stream == nil {
			sendFirstMessage = true
			if err = s.connect(ctx); err != nil {
				return
			}
		}

		obj, err = s.sendOnConnectedStream(buf, offset, flush, finishWrite, sendFirstMessage)
		if obj != nil {
			s.objResource = obj
		}
		if err == nil {
			return
		}

		// await recv stream termination
		for range s.recvs {
		}
		if s.recvErr != io.EOF {
			err = s.recvErr
		}
		s.stream = nil

		// Retry transparently on a redirection error
		if _, ok := err.(bidiWriteObjectRedirectionError); ok {
			s.forceFirstMessage = true
			continue
		}
		return
	}
}
