//go:build go1.18
// +build go1.18

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

// getResponseFromError retrieves the response carried by
// an AuthenticationFailedError or MSAL CallErr, if any
func getResponseFromError(err error) *http.Response {
	var a *AuthenticationFailedError
	var c msal.CallErr
	var res *http.Response
	if errors.As(err, &c) {
		res = c.Resp
	} else if errors.As(err, &a) {
		res = a.RawResponse
	}
	return res
}

// AuthenticationFailedError indicates an authentication request has failed.
type AuthenticationFailedError struct {
	// RawResponse is the HTTP response motivating the error, if available.
	RawResponse *http.Response

	credType string
	message  string
	err      error
}

func newAuthenticationFailedError(credType string, message string, resp *http.Response, err error) error {
	return &AuthenticationFailedError{credType: credType, message: message, RawResponse: resp, err: err}
}

// Error implements the error interface. Note that the message contents are not contractual and can change over time.
func (e *AuthenticationFailedError) Error() string {
	if e.RawResponse == nil {
		return e.credType + ": " + e.message
	}
	msg := &bytes.Buffer{}
	fmt.Fprintf(msg, e.credType+" authentication failed\n")
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
	var anchor string
	switch e.credType {
	case credNameAzureCLI:
		anchor = "azure-cli"
	case credNameCert:
		anchor = "client-cert"
	case credNameSecret:
		anchor = "client-secret"
	case credNameManagedIdentity:
		anchor = "managed-id"
	case credNameUserPassword:
		anchor = "username-password"
	case credNameWorkloadIdentity:
		anchor = "workload"
	}
	if anchor != "" {
		fmt.Fprintf(msg, "To troubleshoot, visit https://aka.ms/azsdk/go/identity/troubleshoot#%s", anchor)
	}
	return msg.String()
}

// NonRetriable indicates the request which provoked this error shouldn't be retried.
func (*AuthenticationFailedError) NonRetriable() {
	// marker method
}

var _ errorinfo.NonRetriable = (*AuthenticationFailedError)(nil)

// credentialUnavailableError indicates a credential can't attempt authentication because it lacks required
// data or state
type credentialUnavailableError struct {
	message string
}

// newCredentialUnavailableError is an internal helper that ensures consistent error message formatting
func newCredentialUnavailableError(credType, message string) error {
	msg := fmt.Sprintf("%s: %s", credType, message)
	return &credentialUnavailableError{msg}
}

// NewCredentialUnavailableError constructs an error indicating a credential can't attempt authentication
// because it lacks required data or state. When [ChainedTokenCredential] receives this error it will try
// its next credential, if any.
func NewCredentialUnavailableError(message string) error {
	return &credentialUnavailableError{message}
}

// Error implements the error interface. Note that the message contents are not contractual and can change over time.
func (e *credentialUnavailableError) Error() string {
	return e.message
}

// NonRetriable is a marker method indicating this error should not be retried. It has no implementation.
func (e *credentialUnavailableError) NonRetriable() {}

var _ errorinfo.NonRetriable = (*credentialUnavailableError)(nil)
