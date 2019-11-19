package querier

import "github.com/grafana/loki/internal/logproto"

func mockTailResponse(stream *logproto.Stream) *logproto.TailResponse {
	return &logproto.TailResponse{
		Stream:         stream,
		DroppedStreams: []*logproto.DroppedStream{},
	}
}
