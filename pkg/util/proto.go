package util //nolint:revive

import (
	"github.com/gogo/protobuf/proto"
)

// ParseProto unmarshals an []byte into dst.
//
// We re-implement proto.Unmarshal here as it calls XXX_Unmarshal first,
// which we can't override without upsetting golint.
func ParseProto(src []byte, dst proto.Message) error {
	dst.Reset()
	if u, ok := dst.(proto.Unmarshaler); ok {
		return u.Unmarshal(src)
	}
	return proto.NewBuffer(src).Unmarshal(dst)
}
