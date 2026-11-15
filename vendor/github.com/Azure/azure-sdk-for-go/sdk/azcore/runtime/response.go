// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	azexported "github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/exported"
)

// Payload reads and returns the response body or an error.
// On a successful read, the response body is cached.
// Subsequent reads will access the cached value.
func Payload(resp *http.Response) ([]byte, error) {
	return exported.Payload(resp, nil)
}

// HasStatusCode returns true if the Response's status code is one of the specified values.
func HasStatusCode(resp *http.Response, statusCodes ...int) bool {
	return exported.HasStatusCode(resp, statusCodes...)
}

// UnmarshalAsByteArray will base-64 decode the received payload and place the result into the value pointed to by v.
func UnmarshalAsByteArray(resp *http.Response, v *[]byte, format Base64Encoding) error {
	p, err := Payload(resp)
	if err != nil {
		return err
	}
	return DecodeByteArray(string(p), v, format)
}

// UnmarshalAsJSON calls json.Unmarshal() to unmarshal the received payload into the value pointed to by v.
func UnmarshalAsJSON(resp *http.Response, v any) error {
	payload, err := Payload(resp)
	if err != nil {
		return err
	}
	// TODO: verify early exit is correct
	if len(payload) == 0 {
		return nil
	}
	err = removeBOM(resp)
	if err != nil {
		return err
	}
	err = json.Unmarshal(payload, v)
	if err != nil {
		err = fmt.Errorf("unmarshalling type %T: %s", v, err)
	}
	return err
}

// UnmarshalAsXML calls xml.Unmarshal() to unmarshal the received payload into the value pointed to by v.
func UnmarshalAsXML(resp *http.Response, v any) error {
	payload, err := Payload(resp)
	if err != nil {
		return err
	}
	// TODO: verify early exit is correct
	if len(payload) == 0 {
		return nil
	}
	err = removeBOM(resp)
	if err != nil {
		return err
	}
	err = xml.Unmarshal(payload, v)
	if err != nil {
		err = fmt.Errorf("unmarshalling type %T: %s", v, err)
	}
	return err
}

// Drain reads the response body to completion then closes it.  The bytes read are discarded.
func Drain(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		// TODO: this might not be necessary when the bodyDownloadPolicy is in play
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

// removeBOM removes any byte-order mark prefix from the payload if present.
func removeBOM(resp *http.Response) error {
	_, err := exported.Payload(resp, &exported.PayloadOptions{
		BytesModifier: func(b []byte) []byte {
			// UTF8
			return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
		},
	})
	if err != nil {
		return err
	}
	return nil
}

// DecodeByteArray will base-64 decode the provided string into v.
func DecodeByteArray(s string, v *[]byte, format Base64Encoding) error {
	return azexported.DecodeByteArray(s, v, format)
}

// SSEResponse validates a pipeline response for use by a streaming.SSEConnector.
// The request must use SkipBodyDownload to preserve the streaming response body.
// When err is nil, resp and resp.Body must be non-nil.
// HTTP 204 closes the body and returns io.EOF to signal completion, regardless of statusCodes.
// Other responses must match one of statusCodes and have a text/event-stream Content-Type.
// On success, the caller owns the returned body. Rejected response bodies are closed.
// Rejected responses below HTTP 400 discard unbuffered bodies without reading them.
// If err is non-nil, resp is ignored. Failure errors matching io.EOF are converted to
// io.ErrUnexpectedEOF so they aren't mistaken for completion.
func SSEResponse(resp *http.Response, err error, statusCodes ...int) (io.ReadCloser, error) {
	if err != nil {
		return nil, sseResponseError(err)
	}
	if HasStatusCode(resp, http.StatusNoContent) {
		_ = resp.Body.Close()
		return nil, io.EOF
	}
	if !HasStatusCode(resp, statusCodes...) {
		// Avoid draining a live stream, but preserve HTTP error bodies and any
		// already-downloaded payload for normal response error parsing.
		if resp.StatusCode < http.StatusBadRequest && !exported.PayloadDownloaded(resp) {
			_ = resp.Body.Close()
			resp.Body = http.NoBody
		}
		return nil, sseResponseError(NewResponseError(resp))
	}
	contentType := resp.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		err = fmt.Errorf("invalid SSE Content-Type %q: %s", contentType, err.Error())
	} else if mediaType != "text/event-stream" {
		err = fmt.Errorf("unexpected SSE Content-Type %q", contentType)
	}
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	return resp.Body, nil
}

func sseResponseError(err error) error {
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("SSE connection failed: %s: %w", err.Error(), io.ErrUnexpectedEOF)
	}
	return err
}
