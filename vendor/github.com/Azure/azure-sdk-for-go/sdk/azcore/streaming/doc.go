// Copyright 2017 Microsoft Corporation. All rights reserved.
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.

// Package streaming contains helpers for streaming IO operations and progress reporting.
//
// # Consuming server-sent events in Azure SDKs
//
// Server-sent events (SSE) let a service send incremental results over a single
// HTTP response. Azure SDK use cases include displaying AI-generated content as
// it arrives, processing intermediate tool or agent activity, and receiving
// progress updates before a final operation result. Finite result streams and
// long-lived notification feeds have different completion and resumption
// semantics; consult the service operation's documentation.
//
// Generated SDK operations open the initial connection before returning a
// response containing an [SSEReader]. Call [SSEReader.Next] to receive typed
// events in order. For example, the following helper consumes a reader returned
// by an SDK operation; T is that operation's generated event type:
//
//	func consume[T any](reader *streaming.SSEReader[T], handle func(T) error) error {
//		defer reader.Close()
//		for {
//			event, err := reader.Next()
//			if errors.Is(err, io.EOF) {
//				return nil
//			}
//			if err != nil {
//				return err
//			}
//			if err := handle(event); err != nil {
//				return err
//			}
//		}
//	}
//
// Always close the reader when finished, even after EOF or an error. Closing
// releases the response body and cancels the reader's connection context.
// Defer Close as soon as the SDK operation succeeds, especially when processing
// might stop early, such as when a user dismisses a UI or rejects an AI result.
// Do not close the reader before handing it to another function that will
// consume it. No event value should be processed when Next returns an error.
//
// A terminal model event is delivered once before EOF. A literal completion
// sentinel, such as "[DONE]", ends iteration without being exposed as an event.
// Other errors, including malformed payloads and transport failures, must be
// handled separately from normal EOF. Successful transport completion does not
// replace any service-specific checks on the final event's result.
//
// # Cancellation and resource limits
//
// The operation context governs the lifetime of the stream, not just the
// initial HTTP request. Keep it alive while consuming events. Cancel it when
// the caller no longer needs the result, or give it a deadline to bound the
// entire operation.
//
// Next blocks waiting for a complete data event and has no built-in idle
// timeout. A stream can wait indefinitely, including when it receives only
// heartbeat comments or metadata. Close can be called concurrently with Next
// to interrupt a pending read. Do not advance a reader concurrently from
// multiple goroutines.
//
// The parser permits SSE lines up to approximately 1 MiB; longer lines fail
// with bufio.ErrTooLong. This is a per-line limit, not an event-size limit:
// multiple data lines are combined into one event.
//
// # Resuming and reconnecting
//
// By default, EOF ends the stream without reconnecting. Do not automatically
// restart a finite operation, such as an AI generation request, merely because
// its response ended; doing so can repeat work or incur additional charges.
//
// If a service supports resumption, save [SSEReader.LastEventID] after
// successfully processing an event, close the old reader, and pass the saved
// checkpoint in the SDK operation's LastEventID option when opening a new
// stream. Event IDs alone do not guarantee resumption support or exactly-once
// delivery. The service defines checkpoint validity and replay behavior; callers
// may need to tolerate duplicate events. An empty checkpoint lets the service
// determine the starting position, which need not be the beginning of history.
//
// SSEFrame.ID and SSEFrame.Retry describe only valid metadata explicitly
// supplied in that frame. Nil means no update; a pointer to an empty ID resets
// the cursor, and a pointer to zero Retry requests immediate reconnection.
// The reader retains inherited state separately. Use LastEventID to retrieve
// the current resumption cursor rather than a frame's optional ID.
//
// # Implementing SDK streaming operations
//
// [NewSSEReader] eagerly opens an SSE connection through an [SSEConnector].
// Pass the original operation context, not the response's per-attempt context.
// The connector must use the context passed to it for each request, validate
// the response, and return its body, typically using runtime.SSEResponse. Returned
// bodies become the reader's responsibility and must support concurrent Read
// and Close, with Close unblocking a pending Read.
//
// Continuous operations can opt into reconnection with [SSEHandler.Reconnect]
// only when reopening the operation is supported by the service. The connector
// opens each fresh connection through the client pipeline. SSE reconnection
// delays govern when to open a new stream; the pipeline's retry policy governs
// attempts to establish that connection. A final connector error stops the
// reader. A connector io.EOF signals service completion, such as HTTP 204,
// including an initially exhausted reader.
//
// Decoders return terminal=true to deliver a final event before iteration ends.
// Return io.EOF from a decoder to consume a terminal sentinel without exposing
// it. Both forms of completion stop iteration without reconnecting.
package streaming
