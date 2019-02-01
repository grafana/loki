/*
 *
 * Copyright 2017 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package status

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/gogo/googleapis/google/rpc"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/any"
	"google.golang.org/grpc/codes"
	gstatus "google.golang.org/grpc/status"
)

func TestErrorsWithSameParameters(t *testing.T) {
	const description = "some description"
	e1 := Errorf(codes.AlreadyExists, description)
	e2 := Errorf(codes.AlreadyExists, description)
	if e1 == e2 || !reflect.DeepEqual(e1, e2) {
		t.Fatalf("Errors should be equivalent but unique - e1: %v, %v  e2: %p, %v", e1.(*statusError), e1, e2.(*statusError), e2)
	}
}

func TestFromToProto(t *testing.T) {
	s := &rpc.Status{
		Code:    int32(codes.Internal),
		Message: "test test test",
		Details: []*types.Any{{TypeUrl: "foo", Value: []byte{3, 2, 1}}},
	}

	err := FromProto(s)
	if got := err.Proto(); !proto.Equal(s, got) {
		t.Fatalf("Expected errors to be identical - s: %v  got: %v", s, got)
	}
}

func TestFromNilProto(t *testing.T) {
	tests := []*Status{nil, FromProto(nil)}
	for _, s := range tests {
		if c := s.Code(); c != codes.OK {
			t.Errorf("s: %v - Expected s.Code() = OK; got %v", s, c)
		}
		if m := s.Message(); m != "" {
			t.Errorf("s: %v - Expected s.Message() = \"\"; got %q", s, m)
		}
		if p := s.Proto(); p != nil {
			t.Errorf("s: %v - Expected s.Proto() = nil; got %q", s, p)
		}
		if e := s.Err(); e != nil {
			t.Errorf("s: %v - Expected s.Err() = nil; got %v", s, e)
		}
	}
}

func TestError(t *testing.T) {
	err := Error(codes.Internal, "test description")
	if got, want := err.Error(), "rpc error: code = Internal desc = test description"; got != want {
		t.Fatalf("err.Error() = %q; want %q", got, want)
	}
	s, _ := FromError(err)
	if got, want := s.Code(), codes.Internal; got != want {
		t.Fatalf("err.Code() = %s; want %s", got, want)
	}
	if got, want := s.Message(), "test description"; got != want {
		t.Fatalf("err.Message() = %s; want %s", got, want)
	}
}

func TestErrorOK(t *testing.T) {
	err := Error(codes.OK, "foo")
	if err != nil {
		t.Fatalf("Error(codes.OK, _) = %p; want nil", err.(*statusError))
	}
}

func TestErrorProtoOK(t *testing.T) {
	s := &rpc.Status{Code: int32(codes.OK)}
	if got := ErrorProto(s); got != nil {
		t.Fatalf("ErrorProto(%v) = %v; want nil", s, got)
	}
}

func TestFromError(t *testing.T) {
	code, message := codes.Internal, "test description"
	err := Error(code, message)
	s, ok := FromError(err)
	if !ok || s.Code() != code || s.Message() != message || s.Err() == nil {
		t.Fatalf("FromError(%v) = %v, %v; want <Code()=%s, Message()=%q, Err()!=nil>, true", err, s, ok, code, message)
	}
}

func TestFromErrorOK(t *testing.T) {
	code, message := codes.OK, ""
	s, ok := FromError(nil)
	if !ok || s.Code() != code || s.Message() != message || s.Err() != nil {
		t.Fatalf("FromError(nil) = %v, %v; want <Code()=%s, Message()=%q, Err=nil>, true", s, ok, code, message)
	}
}

func TestGRPCFromErrorImplementsInterface(t *testing.T) {
	code, message := codes.Internal, "test description"
	st := New(code, message)
	detail := &rpc.RequestInfo{
		RequestId:   "1234",
		ServingData: "data",
	}
	st, err := st.WithDetails(detail)
	if err != nil {
		t.Fatalf("(%v).WithDetails(%+v) failed: %v", str(st), detail, err)
	}
	s, ok := gstatus.FromError(st.Err())
	if !ok || s.Code() != code || s.Message() != message || s.Err() == nil {
		t.Fatalf("FromError(%v) = %v, %v; want <Code()=%s, Message()=%q, Err()!=nil>, true", st.Err(), s, ok, code, message)
	}
	pd := s.Proto().GetDetails()
	stpd := st.Proto().GetDetails()
	if len(pd) != len(stpd) || len(pd) != 1 {
		t.Fatalf("gRPC Status details incorrect: was %v wanted %v", pd, stpd)
	}
	if !bytes.Equal(pd[0].GetValue(), stpd[0].GetValue()) || pd[0].GetTypeUrl() != stpd[0].GetTypeUrl() {
		t.Fatalf("s.Proto.GetDetails() = %v; want <Details()=%s>", pd, stpd)
	}
}

func TestFromErrorGRPCStatus(t *testing.T) {
	code, message := codes.Internal, "test description"
	detail := &rpc.RequestInfo{
		RequestId:   "1234",
		ServingData: "data",
	}
	a, err := types.MarshalAny(detail)
	if err != nil {
		t.Fatalf("types.MarshalAny(%#v) failed: %v", detail, err)
	}
	pb := gstatus.New(code, message).Proto()
	pb.Details = append(pb.GetDetails(), &any.Any{
		TypeUrl: a.GetTypeUrl(),
		Value:   a.GetValue(),
	})
	s, ok := FromError(gstatus.ErrorProto(pb))
	if !ok || s.Code() != code || s.Message() != message || s.Err() == nil {
		t.Fatalf("FromError(%v) = %v, %v; want <Code()=%s, Message()=%q, Err()!=nil>, true", err, s, ok, code, message)
	}
	if len(s.Details()) != 1 {
		t.Fatalf("(%#v).Details() = %v, wanted %v", s, s.Details(), detail)
	}
	if r, ok := s.Details()[0].(*rpc.RequestInfo); !ok || !reflect.DeepEqual(r, detail) {
		t.Fatalf("(%#v).Details()[0] = %v, wanted %v", s, s.Details()[0], detail)
	}
}

func TestFromErrorUnknownError(t *testing.T) {
	code, message := codes.Unknown, "unknown error"
	err := errors.New("unknown error")
	s, ok := FromError(err)
	if ok || s.Code() != code || s.Message() != message {
		t.Fatalf("FromError(%v) = %v, %v; want <Code()=%s, Message()=%q>, false", err, s, ok, code, message)
	}
}

func TestFromGRPCStatus(t *testing.T) {
	code, message := codes.Internal, "test description"
	detail := &rpc.RequestInfo{
		RequestId:   "1234",
		ServingData: "data",
	}
	a, err := types.MarshalAny(detail)
	if err != nil {
		t.Fatalf("types.MarshalAny(%#v) failed: %v", detail, err)
	}
	pb := gstatus.New(code, message).Proto()
	pb.Details = append(pb.GetDetails(), &any.Any{
		TypeUrl: a.GetTypeUrl(),
		Value:   a.GetValue(),
	})
	s := FromGRPCStatus(gstatus.FromProto(pb))
	if s.Code() != code || s.Message() != message || s.Err() == nil {
		t.Fatalf("FromError(%v) = %v; want <Code()=%s, Message()=%q, Err()!=nil>", err, s, code, message)
	}
	if len(s.Details()) != 1 {
		t.Fatalf("(%#v).Details() = %v, wanted %v", s, s.Details(), detail)
	}
	if r, ok := s.Details()[0].(*rpc.RequestInfo); !ok || !reflect.DeepEqual(r, detail) {
		t.Fatalf("(%#v).Details()[0] = %v, wanted %v", s, s.Details()[0], detail)
	}
}

func TestConvertKnownError(t *testing.T) {
	code, message := codes.Internal, "test description"
	err := Error(code, message)
	s := Convert(err)
	if s.Code() != code || s.Message() != message {
		t.Fatalf("Convert(%v) = %v; want <Code()=%s, Message()=%q>", err, s, code, message)
	}
}

func TestConvertUnknownError(t *testing.T) {
	code, message := codes.Unknown, "unknown error"
	err := errors.New("unknown error")
	s := Convert(err)
	if s.Code() != code || s.Message() != message {
		t.Fatalf("Convert(%v) = %v; want <Code()=%s, Message()=%q>", err, s, code, message)
	}
}

func TestCode(t *testing.T) {
	tests := []struct {
		err  error
		code codes.Code
	}{
		{
			err:  nil,
			code: codes.OK,
		},
		{
			err:  errors.New("unknown error"),
			code: codes.Unknown,
		},
		{
			err:  Errorf(codes.Internal, "internal error"),
			code: codes.Internal,
		},
		{
			err:  Errorf(codes.Unknown, "explicitly unknown error"),
			code: codes.Unknown,
		},
	}

	for _, tc := range tests {
		code := Code(tc.err)
		if code != tc.code {
			t.Fatalf("Code(%v) = %v; want %v", tc.err, code, tc.code)
		}
	}
}

func TestStatus_ErrorDetails(t *testing.T) {
	tests := []struct {
		code    codes.Code
		details []proto.Message
	}{
		{
			code:    codes.NotFound,
			details: nil,
		},
		{
			code: codes.NotFound,
			details: []proto.Message{
				&rpc.ResourceInfo{
					ResourceType: "book",
					ResourceName: "projects/1234/books/5678",
					Owner:        "User",
				},
			},
		},
		{
			code: codes.Internal,
			details: []proto.Message{
				&rpc.DebugInfo{
					StackEntries: []string{
						"first stack",
						"second stack",
					},
				},
			},
		},
		{
			code: codes.Unavailable,
			details: []proto.Message{
				&rpc.RetryInfo{
					RetryDelay: &types.Duration{Seconds: 60},
				},
				&rpc.ResourceInfo{
					ResourceType: "book",
					ResourceName: "projects/1234/books/5678",
					Owner:        "User",
				},
			},
		},
	}

	for _, tc := range tests {
		s, err := New(tc.code, "").WithDetails(tc.details...)
		if err != nil {
			t.Fatalf("(%v).WithDetails(%+v) failed: %v", str(s), tc.details, err)
		}
		details := s.Details()
		for i := range details {
			if !proto.Equal(details[i].(proto.Message), tc.details[i]) {
				t.Fatalf("(%v).Details()[%d] = %+v, want %+v", str(s), i, details[i], tc.details[i])
			}
		}
	}
}

func TestStatus_WithDetails_Fail(t *testing.T) {
	tests := []*Status{
		nil,
		FromProto(nil),
		New(codes.OK, ""),
	}
	for _, s := range tests {
		if s, err := s.WithDetails(); err == nil || s != nil {
			t.Fatalf("(%v).WithDetails(%+v) = %v, %v; want nil, non-nil", str(s), []proto.Message{}, s, err)
		}
	}
}

func TestStatus_ErrorDetails_Fail(t *testing.T) {
	tests := []struct {
		s *Status
		i []interface{}
	}{
		{
			nil,
			nil,
		},
		{
			FromProto(nil),
			nil,
		},
		{
			New(codes.OK, ""),
			[]interface{}{},
		},
		{
			FromProto(&rpc.Status{
				Code: int32(rpc.CANCELLED),
				Details: []*types.Any{
					{
						TypeUrl: "",
						Value:   []byte{},
					},
					mustMarshalAny(&rpc.ResourceInfo{
						ResourceType: "book",
						ResourceName: "projects/1234/books/5678",
						Owner:        "User",
					}),
				},
			}),
			[]interface{}{
				errors.New(`message type url "" is invalid`),
				&rpc.ResourceInfo{
					ResourceType: "book",
					ResourceName: "projects/1234/books/5678",
					Owner:        "User",
				},
			},
		},
	}
	for _, tc := range tests {
		got := tc.s.Details()
		if !reflect.DeepEqual(got, tc.i) {
			t.Errorf("(%v).Details() = %+v, want %+v", str(tc.s), got, tc.i)
		}
	}
}

func str(s *Status) string {
	if s == nil {
		return "nil"
	}
	if s.s == nil {
		return "<Code=OK>"
	}
	return fmt.Sprintf("<Code=%v, Message=%q, Details=%+v>", codes.Code(s.s.GetCode()), s.s.GetMessage(), s.s.GetDetails())
}

// mustMarshalAny converts a protobuf message to an any.
func mustMarshalAny(msg proto.Message) *types.Any {
	any, err := types.MarshalAny(msg)
	if err != nil {
		panic(fmt.Sprintf("types.MarshalAny(%+v) failed: %v", msg, err))
	}
	return any
}
