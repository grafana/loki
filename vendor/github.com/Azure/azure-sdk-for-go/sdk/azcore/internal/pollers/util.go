//go:build go1.16
// +build go1.16

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package pollers

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/shared"
)

const (
	StatusSucceeded  = "Succeeded"
	StatusCanceled   = "Canceled"
	StatusFailed     = "Failed"
	StatusInProgress = "InProgress"
)

// Operation abstracts the differences between concrete poller types.
type Operation interface {
	Done() bool
	Update(resp *http.Response) error
	FinalGetURL() string
	URL() string
	Status() string
}

// IsTerminalState returns true if the LRO's state is terminal.
func IsTerminalState(s string) bool {
	return strings.EqualFold(s, StatusSucceeded) || strings.EqualFold(s, StatusFailed) || strings.EqualFold(s, StatusCanceled)
}

// Failed returns true if the LRO's state is terminal failure.
func Failed(s string) bool {
	return strings.EqualFold(s, StatusFailed) || strings.EqualFold(s, StatusCanceled)
}

// returns true if the LRO response contains a valid HTTP status code
func StatusCodeValid(resp *http.Response) bool {
	return shared.HasStatusCode(resp, http.StatusOK, http.StatusAccepted, http.StatusCreated, http.StatusNoContent)
}

// IsValidURL verifies that the URL is valid and absolute.
func IsValidURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.IsAbs()
}

const idSeparator = ";"

// MakeID returns the poller ID from the provided values.
func MakeID(pollerID string, kind string) string {
	return fmt.Sprintf("%s%s%s", pollerID, idSeparator, kind)
}

// DecodeID decodes the poller ID, returning [pollerID, kind] or an error.
func DecodeID(tk string) (string, string, error) {
	raw := strings.Split(tk, idSeparator)
	// strings.Split will include any/all whitespace strings, we want to omit those
	parts := []string{}
	for _, r := range raw {
		if s := strings.TrimSpace(r); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid token %s", tk)
	}
	return parts[0], parts[1], nil
}

// used if the operation synchronously completed
type NopPoller struct{}

func (*NopPoller) URL() string {
	return ""
}

func (*NopPoller) Done() bool {
	return true
}

func (*NopPoller) Update(*http.Response) error {
	return nil
}

func (*NopPoller) FinalGetURL() string {
	return ""
}

func (*NopPoller) Status() string {
	return StatusSucceeded
}
