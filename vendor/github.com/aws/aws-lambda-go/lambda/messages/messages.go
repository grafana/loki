// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.

package messages

type PingRequest struct {
}

type PingResponse struct {
}

type InvokeRequest_Timestamp struct {
	Seconds int64
	Nanos   int64
}

type InvokeRequest struct {
	Payload               []byte
	RequestId             string
	XAmznTraceId          string
	Deadline              InvokeRequest_Timestamp
	InvokedFunctionArn    string
	CognitoIdentityId     string
	CognitoIdentityPoolId string
	ClientContext         []byte
}

type InvokeResponse struct {
	Payload []byte
	Error   *InvokeResponse_Error
}

type InvokeResponse_Error struct {
	Message    string
	Type       string
	StackTrace []*InvokeResponse_Error_StackFrame
	ShouldExit bool
}

type InvokeResponse_Error_StackFrame struct {
	Path  string `json:"path"`
	Line  int32  `json:"line"`
	Label string `json:"label"`
}
