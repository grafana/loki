package wire

import (
	"context"
	"net"

	"google.golang.org/grpc/peer"

	"github.com/grafana/loki/v3/pkg/engine/internal/proto/wirepb"
)

var _ Conn = (*GrpcServerAdapter)(nil)

type GrpcServerAdapter struct {
	Conn wirepb.Transport_LoopServer
	Peer *peer.Peer
}

// Close implements Conn.
func (g *GrpcServerAdapter) Close() error {
	return nil
}

// LocalAddr implements Conn.
func (g *GrpcServerAdapter) LocalAddr() net.Addr {
	return g.Peer.LocalAddr
}

// Recv implements Conn.
func (g *GrpcServerAdapter) Recv(_ context.Context) (Frame, error) {
	data, err := g.Conn.Recv()
	if err != nil {
		return nil, err
	}
	frame, err := DefaultProtoFrameCodec.FrameFromProto(data)
	if err != nil {
		return nil, err
	}
	return frame, nil
}

// RemoteAddr implements Conn.
func (g *GrpcServerAdapter) RemoteAddr() net.Addr {
	return g.Peer.Addr
}

// Send implements Conn.
func (g *GrpcServerAdapter) Send(_ context.Context, frame Frame) error {
	data, err := DefaultProtoFrameCodec.FrameToProto(frame)
	if err != nil {
		return err
	}
	return g.Conn.Send(data)
}

var _ Conn = (*GrpcClientAdapter)(nil)

type GrpcClientAdapter struct {
	Conn wirepb.Transport_LoopClient
	Peer *peer.Peer
}

// Close implements Conn.
func (g *GrpcClientAdapter) Close() error {
	return g.Conn.CloseSend()
}

// LocalAddr implements Conn.
func (g *GrpcClientAdapter) LocalAddr() net.Addr {
	return g.Peer.LocalAddr
}

// Recv implements Conn.
func (g *GrpcClientAdapter) Recv(_ context.Context) (Frame, error) {
	data, err := g.Conn.Recv()
	if err != nil {
		return nil, err
	}
	frame, err := DefaultProtoFrameCodec.FrameFromProto(data)
	if err != nil {
		return nil, err
	}
	return frame, nil
}

// RemoteAddr implements Conn.
func (g *GrpcClientAdapter) RemoteAddr() net.Addr {
	return g.Peer.Addr
}

// Send implements Conn.
func (g *GrpcClientAdapter) Send(_ context.Context, frame Frame) error {
	data, err := DefaultProtoFrameCodec.FrameToProto(frame)
	if err != nil {
		return err
	}
	return g.Conn.Send(data)
}
