package logproto

import (
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/push"
)

// Aliases to avoid renaming all the imports of logproto

type Entry = push.Entry
type Stream = push.Stream
type PushRequest = push.PushRequest
type PushResponse = push.PushResponse
type PusherClient = push.PusherClient
type PusherServer = push.PusherServer

func NewPusherClient(cc *grpc.ClientConn) PusherClient {
	return push.NewPusherClient(cc)
}

func RegisterPusherServer(s *grpc.Server, srv PusherServer) {
	push.RegisterPusherServer(s, srv)
}
