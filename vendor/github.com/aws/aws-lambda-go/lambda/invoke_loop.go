// Copyright 2020 Amazon.com, Inc. or its affiliates. All Rights Reserved

package lambda

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/lambda/messages"
)

const (
	msPerS  = int64(time.Second / time.Millisecond)
	nsPerMS = int64(time.Millisecond / time.Nanosecond)
)

// startRuntimeAPILoop will return an error if handling a particular invoke resulted in a non-recoverable error
func startRuntimeAPILoop(ctx context.Context, api string, handler Handler) error {
	client := newRuntimeAPIClient(api)
	function := NewFunction(handler).withContext(ctx)
	for {
		invoke, err := client.next()
		if err != nil {
			return err
		}

		err = handleInvoke(invoke, function)
		if err != nil {
			return err
		}
	}
}

// handleInvoke returns an error if the function panics, or some other non-recoverable error occurred
func handleInvoke(invoke *invoke, function *Function) error {
	functionRequest, err := convertInvokeRequest(invoke)
	if err != nil {
		return fmt.Errorf("unexpected error occurred when parsing the invoke: %v", err)
	}

	functionResponse := &messages.InvokeResponse{}
	if err := function.Invoke(functionRequest, functionResponse); err != nil {
		return fmt.Errorf("unexpected error occurred when invoking the handler: %v", err)
	}

	if functionResponse.Error != nil {
		payload := safeMarshal(functionResponse.Error)
		if err := invoke.failure(payload, contentTypeJSON); err != nil {
			return fmt.Errorf("unexpected error occurred when sending the function error to the API: %v", err)
		}
		if functionResponse.Error.ShouldExit {
			return fmt.Errorf("calling the handler function resulted in a panic, the process should exit")
		}
		return nil
	}

	if err := invoke.success(functionResponse.Payload, contentTypeJSON); err != nil {
		return fmt.Errorf("unexpected error occurred when sending the function functionResponse to the API: %v", err)
	}

	return nil
}

// convertInvokeRequest converts an invoke from the Runtime API, and unpacks it to be compatible with the shape of a `lambda.Function` InvokeRequest.
func convertInvokeRequest(invoke *invoke) (*messages.InvokeRequest, error) {
	deadlineEpochMS, err := strconv.ParseInt(invoke.headers.Get(headerDeadlineMS), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse contents of header: %s", headerDeadlineMS)
	}
	deadlineS := deadlineEpochMS / msPerS
	deadlineNS := (deadlineEpochMS % msPerS) * nsPerMS

	res := &messages.InvokeRequest{
		InvokedFunctionArn: invoke.headers.Get(headerInvokedFunctionARN),
		XAmznTraceId:       invoke.headers.Get(headerTraceID),
		RequestId:          invoke.id,
		Deadline: messages.InvokeRequest_Timestamp{
			Seconds: deadlineS,
			Nanos:   deadlineNS,
		},
		Payload: invoke.payload,
	}

	clientContextJSON := invoke.headers.Get(headerClientContext)
	if clientContextJSON != "" {
		res.ClientContext = []byte(clientContextJSON)
	}

	cognitoIdentityJSON := invoke.headers.Get(headerCognitoIdentity)
	if cognitoIdentityJSON != "" {
		if err := json.Unmarshal([]byte(invoke.headers.Get(headerCognitoIdentity)), res); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cognito identity json: %v", err)
		}
	}

	return res, nil
}

func safeMarshal(v interface{}) []byte {
	payload, err := json.Marshal(v)
	if err != nil {
		v := &messages.InvokeResponse_Error{
			Type:    "Runtime.SerializationError",
			Message: err.Error(),
		}
		payload, err := json.Marshal(v)
		if err != nil {
			panic(err) // never reach
		}
		return payload
	}
	return payload
}
