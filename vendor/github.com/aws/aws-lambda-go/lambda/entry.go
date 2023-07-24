// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.

package lambda

import (
	"context"
	"errors"
	"log"
	"os"
)

// Start takes a handler and talks to an internal Lambda endpoint to pass requests to the handler. If the
// handler does not match one of the supported types an appropriate error message will be returned to the caller.
// Start blocks, and does not return after being called.
//
// Rules:
//
// 	* handler must be a function
// 	* handler may take between 0 and two arguments.
// 	* if there are two arguments, the first argument must satisfy the "context.Context" interface.
// 	* handler may return between 0 and two arguments.
// 	* if there are two return values, the second argument must be an error.
// 	* if there is one return value it must be an error.
//
// Valid function signatures:
//
// 	func ()
// 	func () error
// 	func (TIn) error
// 	func () (TOut, error)
// 	func (TIn) (TOut, error)
// 	func (context.Context) error
// 	func (context.Context, TIn) error
// 	func (context.Context) (TOut, error)
// 	func (context.Context, TIn) (TOut, error)
//
// Where "TIn" and "TOut" are types compatible with the "encoding/json" standard library.
// See https://golang.org/pkg/encoding/json/#Unmarshal for how deserialization behaves
func Start(handler interface{}) {
	StartWithContext(context.Background(), handler)
}

// StartWithContext is the same as Start except sets the base context for the function.
func StartWithContext(ctx context.Context, handler interface{}) {
	StartHandlerWithContext(ctx, NewHandler(handler))
}

// StartHandler takes in a Handler wrapper interface which can be implemented either by a
// custom function or a struct.
//
// Handler implementation requires a single "Invoke()" function:
//
//  func Invoke(context.Context, []byte) ([]byte, error)
func StartHandler(handler Handler) {
	StartHandlerWithContext(context.Background(), handler)
}

type startFunction struct {
	env string
	f   func(ctx context.Context, envValue string, handler Handler) error
}

var (
	// This allows users to save a little bit of coldstart time in the download, by the dependencies brought in for RPC support.
	// The tradeoff is dropping compatibility with the go1.x runtime, functions must be "Custom Runtime" instead.
	// To drop the rpc dependencies, compile with `-tags lambda.norpc`
	rpcStartFunction = &startFunction{
		env: "_LAMBDA_SERVER_PORT",
		f: func(c context.Context, p string, h Handler) error {
			return errors.New("_LAMBDA_SERVER_PORT was present but the function was compiled without RPC support")
		},
	}
	runtimeAPIStartFunction = &startFunction{
		env: "AWS_LAMBDA_RUNTIME_API",
		f:   startRuntimeAPILoop,
	}
	startFunctions = []*startFunction{rpcStartFunction, runtimeAPIStartFunction}

	// This allows end to end testing of the Start functions, by tests overwriting this function to keep the program alive
	logFatalf = log.Fatalf
)

// StartHandlerWithContext is the same as StartHandler except sets the base context for the function.
//
// Handler implementation requires a single "Invoke()" function:
//
//  func Invoke(context.Context, []byte) ([]byte, error)
func StartHandlerWithContext(ctx context.Context, handler Handler) {
	var keys []string
	for _, start := range startFunctions {
		config := os.Getenv(start.env)
		if config != "" {
			// in normal operation, the start function never returns
			// if it does, exit!, this triggers a restart of the lambda function
			err := start.f(ctx, config, handler)
			logFatalf("%v", err)
		}
		keys = append(keys, start.env)
	}
	logFatalf("expected AWS Lambda environment variables %s are not defined", keys)
}
