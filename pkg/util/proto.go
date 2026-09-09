package util //nolint:revive

import (
	"github.com/gogo/protobuf/proto"
)

// ParseProto unmarshals a decompressed proto message body into req.
//
// We re-implement proto.Unmarshal here as it calls XXX_Unmarshal first,
// which we can't override without upsetting golint.
func ParseProto(body []byte, req proto.Message) error {
	req.Reset()
	if u, ok := req.(proto.Unmarshaler); ok {
		return u.Unmarshal(body)
	}
	return proto.NewBuffer(body).Unmarshal(req)
}
