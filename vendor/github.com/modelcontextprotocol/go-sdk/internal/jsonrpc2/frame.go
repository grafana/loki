// Copyright 2018 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package jsonrpc2

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// Reader abstracts the transport mechanics from the JSON RPC protocol.
// A Conn reads messages from the reader it was provided on construction,
// and assumes that each call to Read fully transfers a single message,
// or returns an error.
//
// A reader is not safe for concurrent use, it is expected it will be used by
// a single Conn in a safe manner.
type Reader interface {
	// Read gets the next message from the stream.
	Read(context.Context) (Message, error)
}

// Writer abstracts the transport mechanics from the JSON RPC protocol.
// A Conn writes messages using the writer it was provided on construction,
// and assumes that each call to Write fully transfers a single message,
// or returns an error.
//
// A writer must be safe for concurrent use, as writes may occur concurrently
// in practice: libraries may make calls or respond to requests asynchronously.
type Writer interface {
	// Write sends a message to the stream.
	Write(context.Context, Message) error
}

// Framer wraps low level byte readers and writers into jsonrpc2 message
// readers and writers.
// It is responsible for the framing and encoding of messages into wire form.
//
// TODO(rfindley): rethink the framer interface, as with JSONRPC2 batching
// there is a need for Reader and Writer to be correlated, and while the
// implementation of framing here allows that, it is not made explicit by the
// interface.
//
// Perhaps a better interface would be
//
//	Frame(io.ReadWriteCloser) (Reader, Writer).
type Framer interface {
	// Reader wraps a byte reader into a message reader.
	Reader(io.Reader) Reader
	// Writer wraps a byte writer into a message writer.
	Writer(io.Writer) Writer
}

// RawFramer returns a new Framer.
// The messages are sent with no wrapping, and rely on json decode consistency
// to determine message boundaries.
func RawFramer() Framer { return rawFramer{} }

type rawFramer struct{}
type rawReader struct{ in *json.Decoder }
type rawWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (rawFramer) Reader(rw io.Reader) Reader {
	return &rawReader{in: json.NewDecoder(rw)}
}

func (rawFramer) Writer(rw io.Writer) Writer {
	return &rawWriter{out: rw}
}

func (r *rawReader) Read(ctx context.Context) (Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	var raw json.RawMessage
	if err := r.in.Decode(&raw); err != nil {
		return nil, err
	}
	msg, err := DecodeMessage(raw)
	return msg, err
}

func (w *rawWriter) Write(ctx context.Context, msg Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	data, err := EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %v", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.out.Write(data)
	return err
}

// HeaderFramer returns a new Framer.
// The messages are sent with HTTP content length and MIME type headers.
// This is the format used by LSP and others.
func HeaderFramer() Framer { return headerFramer{} }

type headerFramer struct{}
type headerReader struct{ in *bufio.Reader }
type headerWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (headerFramer) Reader(rw io.Reader) Reader {
	return &headerReader{in: bufio.NewReader(rw)}
}

func (headerFramer) Writer(rw io.Writer) Writer {
	return &headerWriter{out: rw}
}

func (r *headerReader) Read(ctx context.Context) (Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	firstRead := true // to detect a clean EOF below
	var contentLength int64
	// read the header, stop on the first empty line
	for {
		line, err := r.in.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if firstRead && line == "" {
					return nil, io.EOF // clean EOF
				}
				err = io.ErrUnexpectedEOF
			}
			return nil, fmt.Errorf("failed reading header line: %w", err)
		}
		firstRead = false

		line = strings.TrimSpace(line)
		// check we have a header line
		if line == "" {
			break
		}
		colon := strings.IndexRune(line, ':')
		if colon < 0 {
			return nil, fmt.Errorf("invalid header line %q", line)
		}
		name, value := line[:colon], strings.TrimSpace(line[colon+1:])
		switch name {
		case "Content-Length":
			if contentLength, err = strconv.ParseInt(value, 10, 32); err != nil {
				return nil, fmt.Errorf("failed parsing Content-Length: %v", value)
			}
			if contentLength <= 0 {
				return nil, fmt.Errorf("invalid Content-Length: %v", contentLength)
			}
		default:
			// ignoring unknown headers
		}
	}
	if contentLength == 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	data := make([]byte, contentLength)
	_, err := io.ReadFull(r.in, data)
	if err != nil {
		return nil, err
	}
	msg, err := DecodeMessage(data)
	return msg, err
}

func (w *headerWriter) Write(ctx context.Context, msg Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %v", err)
	}
	_, err = fmt.Fprintf(w.out, "Content-Length: %v\r\n\r\n", len(data))
	if err == nil {
		_, err = w.out.Write(data)
	}
	return err
}
