// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package storepb

import (
	"context"
	"io"

	"google.golang.org/grpc"
)

func ServerAsClient(srv StoreServer, clientReceiveBufferSize int) StoreClient {
	return &serverAsClient{srv: srv, clientReceiveBufferSize: clientReceiveBufferSize}
}

// serverAsClient allows to use servers as clients.
// NOTE: Passing CallOptions does not work - it would be needed to be implemented in grpc itself (before, after are private).
type serverAsClient struct {
	clientReceiveBufferSize int
	srv                     StoreServer
}

func (s serverAsClient) Info(ctx context.Context, in *InfoRequest, _ ...grpc.CallOption) (*InfoResponse, error) {
	return s.srv.Info(ctx, in)
}

func (s serverAsClient) LabelNames(ctx context.Context, in *LabelNamesRequest, _ ...grpc.CallOption) (*LabelNamesResponse, error) {
	return s.srv.LabelNames(ctx, in)
}

func (s serverAsClient) LabelValues(ctx context.Context, in *LabelValuesRequest, _ ...grpc.CallOption) (*LabelValuesResponse, error) {
	return s.srv.LabelValues(ctx, in)
}

func (s serverAsClient) Series(ctx context.Context, in *SeriesRequest, _ ...grpc.CallOption) (Store_SeriesClient, error) {
	inSrv := &inProcessStream{recv: make(chan *SeriesResponse, s.clientReceiveBufferSize), err: make(chan error)}
	inSrv.ctx, inSrv.cancel = context.WithCancel(ctx)
	go func() {
		inSrv.err <- s.srv.Series(in, inSrv)
		close(inSrv.err)
		close(inSrv.recv)
	}()
	return &inProcessClientStream{srv: inSrv}, nil
}

// TODO(bwplotka): Add streaming attributes, metadata etc. Currently those are disconnected. Follow up on https://github.com/grpc/grpc-go/issues/906.
// TODO(bwplotka): Use this in proxy.go and receiver multi tenant proxy.
type inProcessStream struct {
	grpc.ServerStream

	ctx    context.Context
	cancel context.CancelFunc
	recv   chan *SeriesResponse
	err    chan error
}

func (s *inProcessStream) Context() context.Context { return s.ctx }

func (s *inProcessStream) Send(r *SeriesResponse) error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.recv <- r:
		return nil
	}
}

type inProcessClientStream struct {
	grpc.ClientStream

	srv *inProcessStream
}

func (s *inProcessClientStream) Context() context.Context { return s.srv.ctx }

func (s *inProcessClientStream) CloseSend() error {
	s.srv.cancel()
	return nil
}

func (s *inProcessClientStream) Recv() (*SeriesResponse, error) {
	select {
	case <-s.srv.ctx.Done():
		return nil, s.srv.ctx.Err()
	case r, ok := <-s.srv.recv:
		if !ok {
			return nil, io.EOF
		}
		return r, nil
	case err := <-s.srv.err:
		if err == nil {
			return nil, io.EOF
		}
		return nil, err
	}
}
