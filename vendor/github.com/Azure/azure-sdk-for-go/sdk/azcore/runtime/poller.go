//go:build go1.16
// +build go1.16

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/pipeline"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/pollers"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/pollers/loc"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/pollers/op"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/log"
)

// NewPoller creates a Poller based on the provided initial response.
// pollerID - a unique identifier for an LRO, it's usually the client.Method string.
func NewPoller(pollerID string, resp *http.Response, pl pipeline.Pipeline) (*pollers.Poller, error) {
	defer resp.Body.Close()
	// this is a back-stop in case the swagger is incorrect (i.e. missing one or more status codes for success).
	// ideally the codegen should return an error if the initial response failed and not even create a poller.
	if !pollers.StatusCodeValid(resp) {
		return nil, errors.New("the operation failed or was cancelled")
	}
	// determine the polling method
	var lro pollers.Operation
	var err error
	// op poller must be checked first as it can also have a location header
	if op.Applicable(resp) {
		lro, err = op.New(resp, pollerID)
	} else if loc.Applicable(resp) {
		lro, err = loc.New(resp, pollerID)
	} else {
		lro = &pollers.NopPoller{}
	}
	if err != nil {
		return nil, err
	}
	return pollers.NewPoller(lro, resp, pl), nil
}

// NewPollerFromResumeToken creates a Poller from a resume token string.
// pollerID - a unique identifier for an LRO, it's usually the client.Method string.
func NewPollerFromResumeToken(pollerID string, token string, pl pipeline.Pipeline) (*pollers.Poller, error) {
	kind, err := pollers.KindFromToken(pollerID, token)
	if err != nil {
		return nil, err
	}
	// now rehydrate the poller based on the encoded poller type
	var lro pollers.Operation
	switch kind {
	case loc.Kind:
		log.Writef(log.EventLRO, "Resuming %s poller.", loc.Kind)
		lro = &loc.Poller{}
	case op.Kind:
		log.Writef(log.EventLRO, "Resuming %s poller.", op.Kind)
		lro = &op.Poller{}
	default:
		return nil, fmt.Errorf("unhandled poller type %s", kind)
	}
	if err = json.Unmarshal([]byte(token), lro); err != nil {
		return nil, err
	}
	return pollers.NewPoller(lro, nil, pl), nil
}
