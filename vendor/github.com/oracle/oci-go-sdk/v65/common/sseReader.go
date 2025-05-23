// Copyright (c) 2016, 2018, 2025, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

package common

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
)

type SseReader struct {
	HttpBody     io.ReadCloser
	eventScanner bufio.Scanner
	OnClose      func(r *SseReader)
}

// InvalidSSEResponseError returned in the case that a nil response body  was given
// to NewSSEReader()
type InvalidSSEResponseError struct {
}

const InvalidResponseErrorMessage = "invalid response struct given to NewSSEReader"

func (e InvalidSSEResponseError) Error() string {
	return InvalidResponseErrorMessage
}

// NewSSEReader returns an SSE Reader given an sse response
func NewSSEReader(response *http.Response) (*SseReader, error) {

	if response == nil || response.Body == nil {
		return nil, InvalidSSEResponseError{}
	}

	reader := &SseReader{
		HttpBody:     response.Body,
		eventScanner: *bufio.NewScanner(response.Body),
		OnClose:      func(r *SseReader) { r.HttpBody.Close() }, // Default on close function, ensures body is closed after use
	}
	return reader, nil
}

// Take the response in bytes and trim it if necessary
func processEvent(e []byte) []byte {
	e = bytes.TrimPrefix(e, []byte("data: ")) // Text/event-stream always prefixed with 'data: '
	return e
}

// ReadNextEvent reads the next event in the stream, return it unmarshalled
func (r *SseReader) ReadNextEvent() (event []byte, err error) {
	if r.eventScanner.Scan() {
		eventBytes := r.eventScanner.Bytes()
		return processEvent(eventBytes), nil
	} else {

		// Close out the stream since we are finished reading from it
		if r.OnClose != nil {
			r.OnClose(r)
		}

		err := r.eventScanner.Err()
		if err == context.Canceled || err == nil {
			err = io.EOF
		}
		return nil, err
	}

}

// ReadAllEvents reads all events from the response stream, and processes each with given event handler
func (r *SseReader) ReadAllEvents(eventHandler func(e []byte)) error {
	for {

		event, err := r.ReadNextEvent()

		if err != nil {

			if err == io.EOF {
				err = nil
			}
			return err
		}

		// Ignore empty events
		if len(event) > 0 {
			eventHandler(event)
		}
	}
}
