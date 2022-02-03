// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/internal/errorinfo"
	msal "github.com/AzureAD/microsoft-authentication-library-for-go/apps/errors"
)

// AuthenticationFailedError indicates an authentication request has failed.
type AuthenticationFailedError struct {
	err error

	// RawResponse is the HTTP response motivating the error, if available.
	RawResponse *http.Response
}

func newAuthenticationFailedError(err error, resp *http.Response) AuthenticationFailedError {
	if resp == nil {
		var e msal.CallErr
		if errors.As(err, &e) {
			return AuthenticationFailedError{err: e, RawResponse: e.Resp}
		}
	}
	return AuthenticationFailedError{err: err, RawResponse: resp}
}

// Error implements the error interface for type ResponseError.
// Note that the message contents are not contractual and can change over time.
func (e AuthenticationFailedError) Error() string {
	if e.RawResponse == nil {
		return e.err.Error()
	}
	msg := &bytes.Buffer{}
	fmt.Fprintf(msg, "%s %s://%s%s\n", e.RawResponse.Request.Method, e.RawResponse.Request.URL.Scheme, e.RawResponse.Request.URL.Host, e.RawResponse.Request.URL.Path)
	fmt.Fprintln(msg, "--------------------------------------------------------------------------------")
	fmt.Fprintf(msg, "RESPONSE %s\n", e.RawResponse.Status)
	fmt.Fprintln(msg, "--------------------------------------------------------------------------------")
	body, err := io.ReadAll(e.RawResponse.Body)
	e.RawResponse.Body.Close()
	if err != nil {
		fmt.Fprintf(msg, "Error reading response body: %v", err)
	} else if len(body) > 0 {
		e.RawResponse.Body = io.NopCloser(bytes.NewReader(body))
		if err := json.Indent(msg, body, "", "  "); err != nil {
			// failed to pretty-print so just dump it verbatim
			fmt.Fprint(msg, string(body))
		}
	} else {
		fmt.Fprint(msg, "Response contained no body")
	}
	fmt.Fprintln(msg, "\n--------------------------------------------------------------------------------")
	return msg.String()
}

// NonRetriable indicates that this error should not be retried.
func (AuthenticationFailedError) NonRetriable() {
	// marker method
}

var _ errorinfo.NonRetriable = (*AuthenticationFailedError)(nil)

// credentialUnavailableError indicates a credential can't attempt
// authentication because it lacks required data or state.
type credentialUnavailableError struct {
	credType string
	message  string
}

func newCredentialUnavailableError(credType, message string) credentialUnavailableError {
	return credentialUnavailableError{credType: credType, message: message}
}

func (e credentialUnavailableError) Error() string {
	return e.credType + ": " + e.message
}

// NonRetriable indicates that this error should not be retried.
func (e credentialUnavailableError) NonRetriable() {
	// marker method
}

var _ errorinfo.NonRetriable = (*credentialUnavailableError)(nil)
