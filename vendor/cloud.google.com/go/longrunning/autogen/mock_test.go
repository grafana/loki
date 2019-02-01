// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// AUTO-GENERATED CODE. DO NOT EDIT.

package longrunning

import (
	emptypb "github.com/golang/protobuf/ptypes/empty"
	longrunningpb "google.golang.org/genproto/googleapis/longrunning"
)

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/api/option"
	status "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	gstatus "google.golang.org/grpc/status"
)

var _ = io.EOF
var _ = ptypes.MarshalAny
var _ status.Status

type mockOperationsServer struct {
	// Embed for forward compatibility.
	// Tests will keep working if more methods are added
	// in the future.
	longrunningpb.OperationsServer

	reqs []proto.Message

	// If set, all calls return this error.
	err error

	// responses to return if err == nil
	resps []proto.Message
}

func (s *mockOperationsServer) ListOperations(ctx context.Context, req *longrunningpb.ListOperationsRequest) (*longrunningpb.ListOperationsResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if xg := md["x-goog-api-client"]; len(xg) == 0 || !strings.Contains(xg[0], "gl-go/") {
		return nil, fmt.Errorf("x-goog-api-client = %v, expected gl-go key", xg)
	}
	s.reqs = append(s.reqs, req)
	if s.err != nil {
		return nil, s.err
	}
	return s.resps[0].(*longrunningpb.ListOperationsResponse), nil
}

func (s *mockOperationsServer) GetOperation(ctx context.Context, req *longrunningpb.GetOperationRequest) (*longrunningpb.Operation, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if xg := md["x-goog-api-client"]; len(xg) == 0 || !strings.Contains(xg[0], "gl-go/") {
		return nil, fmt.Errorf("x-goog-api-client = %v, expected gl-go key", xg)
	}
	s.reqs = append(s.reqs, req)
	if s.err != nil {
		return nil, s.err
	}
	return s.resps[0].(*longrunningpb.Operation), nil
}

func (s *mockOperationsServer) DeleteOperation(ctx context.Context, req *longrunningpb.DeleteOperationRequest) (*emptypb.Empty, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if xg := md["x-goog-api-client"]; len(xg) == 0 || !strings.Contains(xg[0], "gl-go/") {
		return nil, fmt.Errorf("x-goog-api-client = %v, expected gl-go key", xg)
	}
	s.reqs = append(s.reqs, req)
	if s.err != nil {
		return nil, s.err
	}
	return s.resps[0].(*emptypb.Empty), nil
}

func (s *mockOperationsServer) CancelOperation(ctx context.Context, req *longrunningpb.CancelOperationRequest) (*emptypb.Empty, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if xg := md["x-goog-api-client"]; len(xg) == 0 || !strings.Contains(xg[0], "gl-go/") {
		return nil, fmt.Errorf("x-goog-api-client = %v, expected gl-go key", xg)
	}
	s.reqs = append(s.reqs, req)
	if s.err != nil {
		return nil, s.err
	}
	return s.resps[0].(*emptypb.Empty), nil
}

// clientOpt is the option tests should use to connect to the test server.
// It is initialized by TestMain.
var clientOpt option.ClientOption

var (
	mockOperations mockOperationsServer
)

func TestMain(m *testing.M) {
	flag.Parse()

	serv := grpc.NewServer()
	longrunningpb.RegisterOperationsServer(serv, &mockOperations)

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		log.Fatal(err)
	}
	go serv.Serve(lis)

	conn, err := grpc.Dial(lis.Addr().String(), grpc.WithInsecure())
	if err != nil {
		log.Fatal(err)
	}
	clientOpt = option.WithGRPCConn(conn)

	os.Exit(m.Run())
}

func TestOperationsGetOperation(t *testing.T) {
	var name2 string = "name2-1052831874"
	var done bool = true
	var expectedResponse = &longrunningpb.Operation{
		Name: name2,
		Done: done,
	}

	mockOperations.err = nil
	mockOperations.reqs = nil

	mockOperations.resps = append(mockOperations.resps[:0], expectedResponse)

	var name string = "name3373707"
	var request = &longrunningpb.GetOperationRequest{
		Name: name,
	}

	c, err := NewOperationsClient(context.Background(), clientOpt)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := c.GetOperation(context.Background(), request)

	if err != nil {
		t.Fatal(err)
	}

	if want, got := request, mockOperations.reqs[0]; !proto.Equal(want, got) {
		t.Errorf("wrong request %q, want %q", got, want)
	}

	if want, got := expectedResponse, resp; !proto.Equal(want, got) {
		t.Errorf("wrong response %q, want %q)", got, want)
	}
}

func TestOperationsGetOperationError(t *testing.T) {
	errCode := codes.PermissionDenied
	mockOperations.err = gstatus.Error(errCode, "test error")

	var name string = "name3373707"
	var request = &longrunningpb.GetOperationRequest{
		Name: name,
	}

	c, err := NewOperationsClient(context.Background(), clientOpt)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := c.GetOperation(context.Background(), request)

	if st, ok := gstatus.FromError(err); !ok {
		t.Errorf("got error %v, expected grpc error", err)
	} else if c := st.Code(); c != errCode {
		t.Errorf("got error code %q, want %q", c, errCode)
	}
	_ = resp
}
func TestOperationsListOperations(t *testing.T) {
	var nextPageToken string = ""
	var operationsElement *longrunningpb.Operation = &longrunningpb.Operation{}
	var operations = []*longrunningpb.Operation{operationsElement}
	var expectedResponse = &longrunningpb.ListOperationsResponse{
		NextPageToken: nextPageToken,
		Operations:    operations,
	}

	mockOperations.err = nil
	mockOperations.reqs = nil

	mockOperations.resps = append(mockOperations.resps[:0], expectedResponse)

	var name string = "name3373707"
	var filter string = "filter-1274492040"
	var request = &longrunningpb.ListOperationsRequest{
		Name:   name,
		Filter: filter,
	}

	c, err := NewOperationsClient(context.Background(), clientOpt)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := c.ListOperations(context.Background(), request).Next()

	if err != nil {
		t.Fatal(err)
	}

	if want, got := request, mockOperations.reqs[0]; !proto.Equal(want, got) {
		t.Errorf("wrong request %q, want %q", got, want)
	}

	want := (interface{})(expectedResponse.Operations[0])
	got := (interface{})(resp)
	var ok bool

	switch want := (want).(type) {
	case proto.Message:
		ok = proto.Equal(want, got.(proto.Message))
	default:
		ok = want == got
	}
	if !ok {
		t.Errorf("wrong response %q, want %q)", got, want)
	}
}

func TestOperationsListOperationsError(t *testing.T) {
	errCode := codes.PermissionDenied
	mockOperations.err = gstatus.Error(errCode, "test error")

	var name string = "name3373707"
	var filter string = "filter-1274492040"
	var request = &longrunningpb.ListOperationsRequest{
		Name:   name,
		Filter: filter,
	}

	c, err := NewOperationsClient(context.Background(), clientOpt)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := c.ListOperations(context.Background(), request).Next()

	if st, ok := gstatus.FromError(err); !ok {
		t.Errorf("got error %v, expected grpc error", err)
	} else if c := st.Code(); c != errCode {
		t.Errorf("got error code %q, want %q", c, errCode)
	}
	_ = resp
}
func TestOperationsCancelOperation(t *testing.T) {
	var expectedResponse *emptypb.Empty = &emptypb.Empty{}

	mockOperations.err = nil
	mockOperations.reqs = nil

	mockOperations.resps = append(mockOperations.resps[:0], expectedResponse)

	var name string = "name3373707"
	var request = &longrunningpb.CancelOperationRequest{
		Name: name,
	}

	c, err := NewOperationsClient(context.Background(), clientOpt)
	if err != nil {
		t.Fatal(err)
	}

	err = c.CancelOperation(context.Background(), request)

	if err != nil {
		t.Fatal(err)
	}

	if want, got := request, mockOperations.reqs[0]; !proto.Equal(want, got) {
		t.Errorf("wrong request %q, want %q", got, want)
	}

}

func TestOperationsCancelOperationError(t *testing.T) {
	errCode := codes.PermissionDenied
	mockOperations.err = gstatus.Error(errCode, "test error")

	var name string = "name3373707"
	var request = &longrunningpb.CancelOperationRequest{
		Name: name,
	}

	c, err := NewOperationsClient(context.Background(), clientOpt)
	if err != nil {
		t.Fatal(err)
	}

	err = c.CancelOperation(context.Background(), request)

	if st, ok := gstatus.FromError(err); !ok {
		t.Errorf("got error %v, expected grpc error", err)
	} else if c := st.Code(); c != errCode {
		t.Errorf("got error code %q, want %q", c, errCode)
	}
}
func TestOperationsDeleteOperation(t *testing.T) {
	var expectedResponse *emptypb.Empty = &emptypb.Empty{}

	mockOperations.err = nil
	mockOperations.reqs = nil

	mockOperations.resps = append(mockOperations.resps[:0], expectedResponse)

	var name string = "name3373707"
	var request = &longrunningpb.DeleteOperationRequest{
		Name: name,
	}

	c, err := NewOperationsClient(context.Background(), clientOpt)
	if err != nil {
		t.Fatal(err)
	}

	err = c.DeleteOperation(context.Background(), request)

	if err != nil {
		t.Fatal(err)
	}

	if want, got := request, mockOperations.reqs[0]; !proto.Equal(want, got) {
		t.Errorf("wrong request %q, want %q", got, want)
	}

}

func TestOperationsDeleteOperationError(t *testing.T) {
	errCode := codes.PermissionDenied
	mockOperations.err = gstatus.Error(errCode, "test error")

	var name string = "name3373707"
	var request = &longrunningpb.DeleteOperationRequest{
		Name: name,
	}

	c, err := NewOperationsClient(context.Background(), clientOpt)
	if err != nil {
		t.Fatal(err)
	}

	err = c.DeleteOperation(context.Background(), request)

	if st, ok := gstatus.FromError(err); !ok {
		t.Errorf("got error %v, expected grpc error", err)
	} else if c := st.Code(); c != errCode {
		t.Errorf("got error code %q, want %q", c, errCode)
	}
}
