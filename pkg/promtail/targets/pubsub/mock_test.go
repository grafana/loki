// NOTE(kavi): This is inspired from https://github.com/googleapis/google-cloud-go/blob/pubsub/v1.9.1/pubsub/mock_test.go

// Copyright 2017 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pubsub

// This file provides a mock in-memory pubsub server for streaming pull testing.

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strconv"
	"sync"
	"time"

	emptypb "github.com/golang/protobuf/ptypes/empty"
	pb "google.golang.org/genproto/googleapis/pubsub/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mockServer struct {
	srv *testServer

	pb.SubscriberServer

	Addr string

	mu            sync.Mutex
	Acked         map[string]bool  // acked message IDs
	Deadlines     map[string]int32 // deadlines by message ID
	pullResponses []*pullResponse
	ackErrs       []error
	modAckErrs    []error
	wg            sync.WaitGroup
	sub           *pb.Subscription
}

type pullResponse struct {
	msgs []*pb.ReceivedMessage
	err  error
}

func newMockServer(port int) (*mockServer, error) {
	srv, err := NewTestServerWithPort(port)
	if err != nil {
		return nil, err
	}
	mock := &mockServer{
		srv:       srv,
		Addr:      srv.Addr,
		Acked:     map[string]bool{},
		Deadlines: map[string]int32{},
		sub: &pb.Subscription{
			AckDeadlineSeconds: 10,
			PushConfig:         &pb.PushConfig{},
		},
	}
	pb.RegisterSubscriberServer(srv.Gsrv, mock)
	srv.Start()
	return mock, nil
}

// Each call to addStreamingPullMessages results in one StreamingPullResponse.
func (s *mockServer) addStreamingPullMessages(msgs []*pb.ReceivedMessage) {
	s.mu.Lock()
	s.pullResponses = append(s.pullResponses, &pullResponse{msgs, nil})
	s.mu.Unlock()
}

func (s *mockServer) addStreamingPullError(err error) {
	s.mu.Lock()
	s.pullResponses = append(s.pullResponses, &pullResponse{nil, err})
	s.mu.Unlock()
}

func (s *mockServer) addAckResponse(err error) {
	s.mu.Lock()
	s.ackErrs = append(s.ackErrs, err)
	s.mu.Unlock()
}

func (s *mockServer) addModAckResponse(err error) {
	s.mu.Lock()
	s.modAckErrs = append(s.modAckErrs, err)
	s.mu.Unlock()
}

func (s *mockServer) wait() {
	s.wg.Wait()
}

func (s *mockServer) StreamingPull(stream pb.Subscriber_StreamingPullServer) error {
	s.wg.Add(1)
	defer s.wg.Done()
	errc := make(chan error, 1)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			req, err := stream.Recv()
			if err != nil {
				errc <- err
				return
			}
			s.mu.Lock()
			for _, id := range req.AckIds {
				s.Acked[id] = true
			}
			for i, id := range req.ModifyDeadlineAckIds {
				s.Deadlines[id] = req.ModifyDeadlineSeconds[i]
			}
			s.mu.Unlock()
		}
	}()
	// Send responses.
	for {
		s.mu.Lock()
		if len(s.pullResponses) == 0 {
			s.mu.Unlock()
			// Nothing to send, so wait for the client to shut down the stream.
			err := <-errc // a real error, or at least EOF
			if err == io.EOF {
				return nil
			}
			return err
		}
		pr := s.pullResponses[0]
		s.pullResponses = s.pullResponses[1:]
		s.mu.Unlock()
		if pr.err != nil {
			// Add a slight delay to ensure the server receives any
			// messages en route from the client before shutting down the stream.
			// This reduces flakiness of tests involving retry.
			time.Sleep(200 * time.Millisecond)
		}
		if pr.err == io.EOF {
			return nil
		}
		if pr.err != nil {
			return pr.err
		}
		// Return any error from Recv.
		select {
		case err := <-errc:
			return err
		default:
		}
		res := &pb.StreamingPullResponse{ReceivedMessages: pr.msgs}
		if err := stream.Send(res); err != nil {
			return err
		}
	}
}

func (s *mockServer) Acknowledge(ctx context.Context, req *pb.AcknowledgeRequest) (*emptypb.Empty, error) {
	var err error
	s.mu.Lock()
	if len(s.ackErrs) > 0 {
		err = s.ackErrs[0]
		s.ackErrs = s.ackErrs[1:]
	}
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	for _, id := range req.AckIds {
		s.Acked[id] = true
	}
	s.mu.Unlock()
	return &emptypb.Empty{}, nil
}

func (s *mockServer) ModifyAckDeadline(ctx context.Context, req *pb.ModifyAckDeadlineRequest) (*emptypb.Empty, error) {
	var err error
	s.mu.Lock()
	if len(s.modAckErrs) > 0 {
		err = s.modAckErrs[0]
		s.modAckErrs = s.modAckErrs[1:]
	}
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	for _, id := range req.AckIds {
		s.Deadlines[id] = req.AckDeadlineSeconds
	}
	s.mu.Unlock()
	return &emptypb.Empty{}, nil
}

func (s *mockServer) GetSubscription(ctx context.Context, req *pb.GetSubscriptionRequest) (*pb.Subscription, error) {
	return s.sub, nil
}

// A Server is an in-process gRPC server, listening on a system-chosen port on
// the local loopback interface. Servers are for testing only and are not
// intended to be used in production code.
//
// To create a server, make a new Server, register your handlers, then call
// Start:
//
//	srv, err := NewServer()
//	...
//	mypb.RegisterMyServiceServer(srv.Gsrv, &myHandler)
//	....
//	srv.Start()
//
// Clients should connect to the server with no security:
//
//	conn, err := grpc.Dial(srv.Addr, grpc.WithInsecure())
//	...
type testServer struct {
	Addr string
	Port int
	l    net.Listener
	Gsrv *grpc.Server
}

// NewServer creates a new Server. The Server will be listening for gRPC connections
// at the address named by the Addr field, without TLS.
func NewServer(opts ...grpc.ServerOption) (*testServer, error) {
	return NewTestServerWithPort(0, opts...)
}

// NewServerWithPort creates a new Server at a specific port. The Server will be listening
// for gRPC connections at the address named by the Addr field, without TLS.
func NewTestServerWithPort(port int, opts ...grpc.ServerOption) (*testServer, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return nil, err
	}
	s := &testServer{
		Addr: l.Addr().String(),
		Port: parsePort(l.Addr().String()),
		l:    l,
		Gsrv: grpc.NewServer(opts...),
	}
	return s, nil
}

// Start causes the server to start accepting incoming connections.
// Call Start after registering handlers.
func (s *testServer) Start() {
	go func() {
		if err := s.Gsrv.Serve(s.l); err != nil {
			log.Printf("testutil.Server.Start: %v", err)
		}
	}()
}

// Close shuts down the server.
func (s *testServer) Close() {
	s.Gsrv.Stop()
	s.l.Close()
}

// PageBounds converts an incoming page size and token from an RPC request into
// slice bounds and the outgoing next-page token.
//
// PageBounds assumes that the complete, unpaginated list of items exists as a
// single slice. In addition to the page size and token, PageBounds needs the
// length of that slice.
//
// PageBounds's first two return values should be used to construct a sub-slice of
// the complete, unpaginated slice. E.g. if the complete slice is s, then
// s[from:to] is the desired page. Its third return value should be set as the
// NextPageToken field of the RPC response.
func PageBounds(pageSize int, pageToken string, length int) (from, to int, nextPageToken string, err error) {
	from, to = 0, length
	if pageToken != "" {
		from, err = strconv.Atoi(pageToken)
		if err != nil {
			return 0, 0, "", status.Errorf(codes.InvalidArgument, "bad page token: %v", err)
		}
		if from >= length {
			return length, length, "", nil
		}
	}
	if pageSize > 0 && from+pageSize < length {
		to = from + pageSize
		nextPageToken = strconv.Itoa(to)
	}
	return from, to, nextPageToken, nil
}

var portParser = regexp.MustCompile(`:[0-9]+`)

func parsePort(addr string) int {
	res := portParser.FindAllString(addr, -1)
	if len(res) == 0 {
		panic(fmt.Errorf("parsePort: found no numbers in %s", addr))
	}
	stringPort := res[0][1:] // strip the :
	p, err := strconv.ParseInt(stringPort, 10, 32)
	if err != nil {
		panic(err)
	}
	return int(p)
}
