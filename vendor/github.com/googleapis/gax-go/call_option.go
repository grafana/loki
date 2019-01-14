// Copyright 2016, Google Inc.
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//     * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//     * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//     * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package gax

import (
	v2 "github.com/googleapis/gax-go/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// CallOption is an option used by Invoke to control behaviors of RPC calls.
// CallOption works by modifying relevant fields of CallSettings.
type CallOption = v2.CallOption

// Retryer is used by Invoke to determine retry behavior.
type Retryer = v2.Retryer

// WithRetry sets CallSettings.Retry to fn.
func WithRetry(fn func() Retryer) CallOption {
	return v2.WithRetry(fn)
}

// OnCodes returns a Retryer that retries if and only if
// the previous attempt returns a GRPC error whose error code is stored in cc.
// Pause times between retries are specified by bo.
//
// bo is only used for its parameters; each Retryer has its own copy.
func OnCodes(cc []codes.Code, bo Backoff) Retryer {
	return v2.OnCodes(cc, bo)
}

// Backoff implements exponential backoff.
// The wait time between retries is a random value between 0 and the "retry envelope".
// The envelope starts at Initial and increases by the factor of Multiplier every retry,
// but is capped at Max.
type Backoff = v2.Backoff

// WithGRPCOptions allows passing gRPC call options during client creation.
func WithGRPCOptions(opt ...grpc.CallOption) CallOption {
	return v2.WithGRPCOptions(opt...)
}

// CallSettings allow fine-grained control over how calls are made.
type CallSettings = v2.CallSettings
