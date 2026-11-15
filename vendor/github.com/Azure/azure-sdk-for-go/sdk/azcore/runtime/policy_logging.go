// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/log"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/sanitizer"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/shared"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/diag"
)

type logPolicy struct {
	sanitizer   *sanitizer.Sanitizer
	includeBody bool
}

// NewLogPolicy creates a request/response logging policy object configured using the specified options.
// Pass nil to accept the default values; this is the same as passing a zero-value options.
func NewLogPolicy(o *policy.LogOptions) policy.Policy {
	if o == nil {
		o = &policy.LogOptions{}
	}

	return newLogPolicy(o, sanitizer.New(sanitizer.Options{
		AllowedHeaders:     o.AllowedHeaders,
		AllowedQueryParams: o.AllowedQueryParams,
	}))
}

func newLogPolicy(o *policy.LogOptions, s *sanitizer.Sanitizer) policy.Policy {
	return &logPolicy{
		sanitizer:   s,
		includeBody: o.IncludeBody,
	}
}

// logPolicyOpValues is the struct containing the per-operation values
type logPolicyOpValues struct {
	try   int32
	start time.Time
}

func (p *logPolicy) Do(req *policy.Request) (*http.Response, error) {
	// Get the per-operation values. These are saved in the Message's map so that they persist across each retry calling into this policy object.
	var opValues logPolicyOpValues
	if req.OperationValue(&opValues); opValues.start.IsZero() {
		opValues.start = time.Now() // If this is the 1st try, record this operation's start time
	}
	opValues.try++ // The first try is #1 (not #0)
	req.SetOperationValue(opValues)

	// Log the outgoing request as informational
	if log.Should(log.EventRequest) {
		b := &bytes.Buffer{}
		fmt.Fprintf(b, "==> OUTGOING REQUEST (Try=%d)\n", opValues.try)
		p.writeRequestWithResponse(b, req, nil, nil)
		var err error
		if p.includeBody {
			err = writeReqBody(req, b)
		}
		log.Write(log.EventRequest, b.String())
		if err != nil {
			return nil, err
		}
	}

	// Set the time for this particular retry operation and then Do the operation.
	tryStart := time.Now()
	response, err := req.Next() // Make the request
	tryEnd := time.Now()
	tryDuration := tryEnd.Sub(tryStart)
	opDuration := tryEnd.Sub(opValues.start)

	if log.Should(log.EventResponse) {
		// We're going to log this; build the string to log
		b := &bytes.Buffer{}
		fmt.Fprintf(b, "==> REQUEST/RESPONSE (Try=%d/%v, OpTime=%v) -- ", opValues.try, tryDuration, opDuration)
		if err != nil { // This HTTP request did not get a response from the service
			fmt.Fprint(b, "REQUEST ERROR\n")
		} else {
			fmt.Fprint(b, "RESPONSE RECEIVED\n")
		}

		p.writeRequestWithResponse(b, req, response, err)
		if err != nil {
			// skip frames runtime.Callers() and runtime.StackTrace()
			b.WriteString(diag.StackTrace(2, 32))
		} else if p.includeBody {
			if shouldSkipBodyDownload(req, response) {
				fmt.Fprintln(b, "   Skip logging body for streaming response")
			} else {
				err = writeRespBody(response, b)
			}
		}
		log.Write(log.EventResponse, b.String())
	}
	return response, err
}

// writeRequestWithResponse appends a formatted HTTP request into a Buffer. If request and/or err are
// not nil, then these are also written into the Buffer.
func (p *logPolicy) writeRequestWithResponse(b *bytes.Buffer, req *policy.Request, resp *http.Response, err error) {
	// Write the request into the buffer.
	fmt.Fprint(b, "   "+req.Raw().Method+" "+p.sanitizer.GetSanitizedURL(*req.Raw().URL)+"\n")
	p.writeHeader(b, req.Raw().Header)
	if resp != nil {
		fmt.Fprintln(b, "   --------------------------------------------------------------------------------")
		fmt.Fprint(b, "   RESPONSE Status: "+resp.Status+"\n")
		p.writeHeader(b, resp.Header)
	}
	if err != nil {
		fmt.Fprintln(b, "   --------------------------------------------------------------------------------")
		fmt.Fprint(b, "   ERROR:\n"+p.sanitizer.GetSanitizedURLString(err.Error(), req.Raw().URL)+"\n")
	}
}

// formatHeaders appends an HTTP request's or response's header into a Buffer.
func (p *logPolicy) writeHeader(b *bytes.Buffer, header http.Header) {
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
		fmt.Fprintf(b, "   %s: %+v\n", k, p.sanitizer.GetSanitizedHeader(k, header))
	}
}

// returns true if the request/response body should be logged.
// this is determined by looking at the content-type header value.
func shouldLogBody(b *bytes.Buffer, contentType string) bool {
	contentType = strings.ToLower(contentType)
	if strings.HasPrefix(contentType, "text") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "xml") {
		return true
	}
	fmt.Fprintf(b, "   Skip logging body for %s\n", contentType)
	return false
}

// writes to a buffer, used for logging purposes
func writeReqBody(req *policy.Request, b *bytes.Buffer) error {
	if req.Raw().Body == nil {
		fmt.Fprint(b, "   Request contained no body\n")
		return nil
	}
	if ct := req.Raw().Header.Get(shared.HeaderContentType); !shouldLogBody(b, ct) {
		return nil
	}
	body, err := io.ReadAll(req.Raw().Body)
	if err != nil {
		fmt.Fprintf(b, "   Failed to read request body: %s\n", err.Error())
		return err
	}
	if err := req.RewindBody(); err != nil {
		return err
	}
	logBody(b, body)
	return nil
}

// writes to a buffer, used for logging purposes
func writeRespBody(resp *http.Response, b *bytes.Buffer) error {
	ct := resp.Header.Get(shared.HeaderContentType)
	if ct == "" {
		fmt.Fprint(b, "   Response contained no body\n")
		return nil
	} else if !shouldLogBody(b, ct) {
		return nil
	}
	body, err := Payload(resp)
	if err != nil {
		fmt.Fprintf(b, "   Failed to read response body: %s\n", err.Error())
		return err
	}
	if len(body) > 0 {
		logBody(b, body)
	} else {
		fmt.Fprint(b, "   Response contained no body\n")
	}
	return nil
}

func logBody(b *bytes.Buffer, body []byte) {
	fmt.Fprintln(b, "   --------------------------------------------------------------------------------")
	fmt.Fprintln(b, string(body))
	fmt.Fprintln(b, "   --------------------------------------------------------------------------------")
}
