// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

// InternalError is an internal error type that all errors get wrapped in.
type InternalError struct {
	cause error
}

func (e *InternalError) Error() string {
	if (errors.Is(e.cause, StorageError{})) {
		return e.cause.Error()
	}

	return fmt.Sprintf("===== INTERNAL ERROR =====\n%s", e.cause.Error())
}

func (e *InternalError) Is(err error) bool {
	_, ok := err.(*InternalError)

	return ok
}

func (e *InternalError) As(target interface{}) bool {
	nt, ok := target.(**InternalError)

	if ok {
		*nt = e
		return ok
	}

	//goland:noinspection GoErrorsAs
	return errors.As(e.cause, target)
}

// StorageError is the internal struct that replaces the generated StorageError.
// TL;DR: This implements xml.Unmarshaler, and when the original StorageError is substituted, this unmarshaler kicks in.
// This handles the description and details. defunkifyStorageError handles the response, cause, and service code.
type StorageError struct {
	response    *http.Response
	description string

	ErrorCode StorageErrorCode
	details   map[string]string
}

func handleError(err error) error {
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		return &InternalError{responseErrorToStorageError(respErr)}
	}

	if err != nil {
		return &InternalError{err}
	}

	return nil
}

// converts an *azcore.ResponseError to a *StorageError, or if that fails, a *InternalError
func responseErrorToStorageError(responseError *azcore.ResponseError) error {
	var storageError StorageError
	body, err := runtime.Payload(responseError.RawResponse)
	if err != nil {
		goto Default
	}
	if len(body) > 0 {
		if err := xml.Unmarshal(body, &storageError); err != nil {
			goto Default
		}
	}

	storageError.response = responseError.RawResponse

	storageError.ErrorCode = StorageErrorCode(responseError.RawResponse.Header.Get("x-ms-error-code"))

	if code, ok := storageError.details["Code"]; ok {
		storageError.ErrorCode = StorageErrorCode(code)
		delete(storageError.details, "Code")
	}

	return &storageError

Default:
	return &InternalError{
		cause: responseError,
	}
}

// StatusCode returns service-error information. The caller may examine these values but should not modify any of them.
func (e *StorageError) StatusCode() int {
	return e.response.StatusCode
}

// Error implements the error interface's Error method to return a string representation of the error.
func (e StorageError) Error() string {
	b := &bytes.Buffer{}

	if e.response != nil {
		_, _ = fmt.Fprintf(b, "===== RESPONSE ERROR (ErrorCode=%s) =====\n", e.ErrorCode)
		_, _ = fmt.Fprintf(b, "Description=%s, Details: ", e.description)
		if len(e.details) == 0 {
			b.WriteString("(none)\n")
		} else {
			b.WriteRune('\n')
			keys := make([]string, 0, len(e.details))
			// Alphabetize the details
			for k := range e.details {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				_, _ = fmt.Fprintf(b, "   %s: %+v\n", k, e.details[k])
			}
		}
		// req := azcore.Request{Request: e.response.Request}.Copy() // Make a copy of the response's request
		// TODO: Come Here Mohit Adele
		//writeRequestWithResponse(b, &azcore.Request{Request: e.response.Request}, e.response)
	}

	return b.String()
	///azcore.writeRequestWithResponse(b, prepareRequestForLogging(req), e.response, nil)
	// return e.ErrorNode.Error(b.String())
}

func (e StorageError) Is(err error) bool {
	_, ok := err.(StorageError)
	_, ok2 := err.(*StorageError)

	return ok || ok2
}

func (e StorageError) Response() *http.Response {
	return e.response
}

//nolint
func writeRequestWithResponse(b *bytes.Buffer, request *policy.Request, response *http.Response) {
	// Write the request into the buffer.
	_, _ = fmt.Fprint(b, "   "+request.Raw().Method+" "+request.Raw().URL.String()+"\n")
	writeHeader(b, request.Raw().Header)
	if response != nil {
		_, _ = fmt.Fprintln(b, "   --------------------------------------------------------------------------------")
		_, _ = fmt.Fprint(b, "   RESPONSE Status: "+response.Status+"\n")
		writeHeader(b, response.Header)
	}
}

// formatHeaders appends an HTTP request's or response's header into a Buffer.
//nolint
func writeHeader(b *bytes.Buffer, header map[string][]string) {
	if len(header) == 0 {
		b.WriteString("   (no headers)\n")
		return
	}
	keys := make([]string, 0, len(header))
	// Alphabetize the headers
	for k := range header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		// Redact the value of any Authorization header to prevent security information from persisting in logs
		value := interface{}("REDACTED")
		if !strings.EqualFold(k, "Authorization") {
			value = header[k]
		}
		_, _ = fmt.Fprintf(b, "   %s: %+v\n", k, value)
	}
}

// Temporary returns true if the error occurred due to a temporary condition (including an HTTP status of 500 or 503).
func (e *StorageError) Temporary() bool {
	if e.response != nil {
		if (e.response.StatusCode == http.StatusInternalServerError) || (e.response.StatusCode == http.StatusServiceUnavailable) || (e.response.StatusCode == http.StatusBadGateway) {
			return true
		}
	}

	return false
}

// UnmarshalXML performs custom unmarshalling of XML-formatted Azure storage request errors.
//nolint
func (e *StorageError) UnmarshalXML(d *xml.Decoder, start xml.StartElement) (err error) {
	tokName := ""
	var t xml.Token
	for t, err = d.Token(); err == nil; t, err = d.Token() {
		switch tt := t.(type) {
		case xml.StartElement:
			tokName = tt.Name.Local
		case xml.EndElement:
			tokName = ""
		case xml.CharData:
			switch tokName {
			case "":
				continue
			case "Message":
				e.description = string(tt)
			default:
				if e.details == nil {
					e.details = map[string]string{}
				}
				e.details[tokName] = string(tt)
			}
		}
	}

	return nil
}
