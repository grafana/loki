// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.

package lambda

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda/messages"
	"github.com/aws/aws-lambda-go/lambdacontext"
)

// Function struct which wrap the Handler
type Function struct {
	handler Handler
	ctx     context.Context
}

// NewFunction which creates a Function with a given Handler
func NewFunction(handler Handler) *Function {
	return &Function{handler: handler}
}

// Ping method which given a PingRequest and a PingResponse parses the PingResponse
func (fn *Function) Ping(req *messages.PingRequest, response *messages.PingResponse) error {
	*response = messages.PingResponse{}
	return nil
}

// Invoke method try to perform a command given an InvokeRequest and an InvokeResponse
func (fn *Function) Invoke(req *messages.InvokeRequest, response *messages.InvokeResponse) error {
	defer func() {
		if err := recover(); err != nil {
			response.Error = lambdaPanicResponse(err)
		}
	}()

	deadline := time.Unix(req.Deadline.Seconds, req.Deadline.Nanos).UTC()
	invokeContext, cancel := context.WithDeadline(fn.context(), deadline)
	defer cancel()

	lc := &lambdacontext.LambdaContext{
		AwsRequestID:       req.RequestId,
		InvokedFunctionArn: req.InvokedFunctionArn,
		Identity: lambdacontext.CognitoIdentity{
			CognitoIdentityID:     req.CognitoIdentityId,
			CognitoIdentityPoolID: req.CognitoIdentityPoolId,
		},
	}
	if len(req.ClientContext) > 0 {
		if err := json.Unmarshal(req.ClientContext, &lc.ClientContext); err != nil {
			response.Error = lambdaErrorResponse(err)
			return nil
		}
	}
	invokeContext = lambdacontext.NewContext(invokeContext, lc)

	// nolint:staticcheck
	invokeContext = context.WithValue(invokeContext, "x-amzn-trace-id", req.XAmznTraceId)
	os.Setenv("_X_AMZN_TRACE_ID", req.XAmznTraceId)

	payload, err := fn.handler.Invoke(invokeContext, req.Payload)
	if err != nil {
		response.Error = lambdaErrorResponse(err)
		return nil
	}
	response.Payload = payload
	return nil
}

// context returns the base context used for the fn.
func (fn *Function) context() context.Context {
	if fn.ctx == nil {
		return context.Background()
	}

	return fn.ctx
}

// withContext returns a shallow copy of Function with its context changed
// to the provided ctx. If the provided ctx is non-nil a Background context is set.
func (fn *Function) withContext(ctx context.Context) *Function {
	if ctx == nil {
		ctx = context.Background()
	}

	fn2 := new(Function)
	*fn2 = *fn

	fn2.ctx = ctx

	return fn2
}
