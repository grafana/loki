//go:build go1.16
// +build go1.16

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package op

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/pollers"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/shared"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/log"
)

// Kind is the identifier of this type in a resume token.
const Kind = "Operation-Location"

// Applicable returns true if the LRO is using Operation-Location.
func Applicable(resp *http.Response) bool {
	return resp.Header.Get(shared.HeaderOperationLocation) != ""
}

// Poller is an LRO poller that uses the Operation-Location pattern.
type Poller struct {
	Type     string `json:"type"`
	PollURL  string `json:"pollURL"`
	LocURL   string `json:"locURL"`
	FinalGET string `json:"finalGET"`
	CurState string `json:"state"`
}

// New creates a new Poller from the provided initial response.
func New(resp *http.Response, pollerID string) (*Poller, error) {
	log.Write(log.EventLRO, "Using Operation-Location poller.")
	opURL := resp.Header.Get(shared.HeaderOperationLocation)
	if opURL == "" {
		return nil, errors.New("response is missing Operation-Location header")
	}
	if !pollers.IsValidURL(opURL) {
		return nil, fmt.Errorf("invalid Operation-Location URL %s", opURL)
	}
	locURL := resp.Header.Get(shared.HeaderLocation)
	// Location header is optional
	if locURL != "" && !pollers.IsValidURL(locURL) {
		return nil, fmt.Errorf("invalid Location URL %s", locURL)
	}
	// default initial state to InProgress.  if the
	// service sent us a status then use that instead.
	curState := pollers.StatusInProgress
	status, err := getValue(resp, "status")
	if err != nil && !errors.Is(err, shared.ErrNoBody) {
		return nil, err
	}
	if status != "" {
		curState = status
	}
	// calculate the tentative final GET URL.
	// can change if we receive a resourceLocation.
	// it's ok for it to be empty in some cases.
	finalGET := ""
	if resp.Request.Method == http.MethodPatch || resp.Request.Method == http.MethodPut {
		finalGET = resp.Request.URL.String()
	} else if resp.Request.Method == http.MethodPost && locURL != "" {
		finalGET = locURL
	}
	return &Poller{
		Type:     pollers.MakeID(pollerID, Kind),
		PollURL:  opURL,
		LocURL:   locURL,
		FinalGET: finalGET,
		CurState: curState,
	}, nil
}

func (p *Poller) URL() string {
	return p.PollURL
}

func (p *Poller) Done() bool {
	return pollers.IsTerminalState(p.Status())
}

func (p *Poller) Update(resp *http.Response) error {
	status, err := getValue(resp, "status")
	if err != nil {
		return err
	} else if status == "" {
		return errors.New("the response did not contain a status")
	}
	p.CurState = status
	// if the endpoint returned an operation-location header, update cached value
	if opLoc := resp.Header.Get(shared.HeaderOperationLocation); opLoc != "" {
		p.PollURL = opLoc
	}
	// check for resourceLocation
	resLoc, err := getValue(resp, "resourceLocation")
	if err != nil && !errors.Is(err, shared.ErrNoBody) {
		return err
	} else if resLoc != "" {
		p.FinalGET = resLoc
	}
	return nil
}

func (p *Poller) FinalGetURL() string {
	return p.FinalGET
}

func (p *Poller) Status() string {
	return p.CurState
}

func getValue(resp *http.Response, val string) (string, error) {
	jsonBody, err := shared.GetJSON(resp)
	if err != nil {
		return "", err
	}
	v, ok := jsonBody[val]
	if !ok {
		// it might be ok if the field doesn't exist, the caller must make that determination
		return "", nil
	}
	vv, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("the %s value %v was not in string format", val, v)
	}
	return vv, nil
}
