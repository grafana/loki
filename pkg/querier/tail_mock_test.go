package querier

import "github.com/MarkWang2/loki/pkg/logproto"

func mockTailResponse(stream logproto.Stream) *logproto.TailResponse {
	return &logproto.TailResponse{
		Stream:         &stream,
		DroppedStreams: []*logproto.DroppedStream{},
	}
}
