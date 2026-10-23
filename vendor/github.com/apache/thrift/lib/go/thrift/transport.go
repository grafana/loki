/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements. See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership. The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License. You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package thrift

import (
	"context"
	"errors"
	"io"
)

var errTransportInterrupted = errors.New("Transport Interrupted")

type Flusher interface {
	Flush() (err error)
}

type ContextFlusher interface {
	Flush(ctx context.Context) (err error)
}

// UnknownRemainingBytes is returned by RemainingBytes implementations that
// cannot tell how many bytes are still readable, for example an unframed
// socket.
const UnknownRemainingBytes = ^uint64(0)

type ReadSizeProvider interface {
	// RemainingBytes returns the number of bytes that can still be read,
	// or UnknownRemainingBytes when the transport has no way of knowing.
	//
	// The value is used as an upper bound when validating element counts
	// read off the wire, so it must never be smaller than the number of
	// bytes a well formed peer can still deliver for the current message,
	// or valid messages will be rejected. A layered transport that cannot
	// express its own readable bytes in terms of the transport below it
	// (because it decompresses, or because it buffers ahead) must return
	// UnknownRemainingBytes rather than pass the lower value through.
	RemainingBytes() (num_bytes uint64)
}

// addRemainingBytes adds extra to a RemainingBytes value, saturating at
// UnknownRemainingBytes rather than wrapping around.
func addRemainingBytes(remaining, extra uint64) uint64 {
	if remaining >= UnknownRemainingBytes-extra {
		return UnknownRemainingBytes
	}
	return remaining + extra
}

// Encapsulates the I/O layer
type TTransport interface {
	io.ReadWriteCloser
	ContextFlusher
	ReadSizeProvider

	// Opens the transport for communication
	Open() error

	// Returns true if the transport is open
	IsOpen() bool
}

type stringWriter interface {
	WriteString(s string) (n int, err error)
}

// This is "enchanced" transport with extra capabilities. You need to use one of these
// to construct protocol.
// Notably, TSocket does not implement this interface, and it is always a mistake to use
// TSocket directly in protocol.
type TRichTransport interface {
	io.ReadWriter
	io.ByteReader
	io.ByteWriter
	stringWriter
	ContextFlusher
	ReadSizeProvider
}
