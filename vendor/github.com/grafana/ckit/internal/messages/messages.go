// Package messages allows for encoding and decoding messages to broadcast over
// gossip.
package messages

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/hashicorp/go-msgpack/codec"
)

// magicHeader is added to the start of every message.
const magicHeader uint16 = 0x1201

// Type of the message. Encoded along with the payload to be able to determine
// what message was sent during decoding.
type Type uint8

// Types for messages.
const (
	TypeInvalid Type = iota // TypeInvalid is an invalid type.
	TypeState               // TypeState is used for a State broadcast
)

var knownTypes = map[Type]string{
	TypeInvalid: "invalid",
	TypeState:   "state",
}

// String returns the string representation of t.
func (t Type) String() string {
	val, ok := knownTypes[t]
	if !ok {
		return fmt.Sprintf("<invalid type %d>", t)
	}
	return val
}

// Message is a payload that can be gossiped to other peers.
type Message interface {
	// Type returns the Type of the message. Type must be a known, valid type.
	Type() Type

	// Invalidates should return true if this message takes precedence over m.
	Invalidates(m Message) bool

	// Cache should return true if this Message should be cached into the local
	// state. Messages in the local state will be synchronized with peers over
	// time, and is useful for anti-entropy.
	Cache() bool
}

// Encode encodes m into a byte slice that can be broadcast to other peers.
// Encode will panic if the Type of m is invalid or unknown.
func Encode(m Message) (raw []byte, err error) {
	ty := m.Type()
	if _, known := knownTypes[ty]; !known || ty == TypeInvalid {
		panic("ty must be a known, valid type")
	}

	buf := bytes.NewBuffer(nil)

	// Write magic header and type
	_ = binary.Write(buf, binary.BigEndian, magicHeader)
	buf.WriteByte(uint8(ty))

	// Then add the message
	var handle codec.MsgpackHandle
	enc := codec.NewEncoder(buf, &handle)
	err = enc.Encode(m)
	return buf.Bytes(), err
}

// Parse parses an encoded buffer returned by Encode. The resulting buf can be
// passed to Decode. Returns an error if the magic header or type is invalid.
//
// buf will be a slice referencing data in raw; do not modify raw until you are
// finished with the message.
func Parse(raw []byte) (buf []byte, ty Type, err error) {
	if len(raw) < 3 {
		return nil, TypeInvalid, fmt.Errorf("payload too small for message")
	}

	magic := binary.BigEndian.Uint16(raw[0:2])
	if magic != magicHeader {
		return nil, TypeInvalid, fmt.Errorf("invalid magic header %x", magic)
	}

	ty = Type(raw[2])
	if _, known := knownTypes[ty]; !known || ty == TypeInvalid {
		return nil, TypeInvalid, fmt.Errorf("invalid message type %d", ty)
	}

	buf = raw[3:]
	return
}

// Decode decodes a message from Parse.
func Decode(buf []byte, m Message) error {
	r := bytes.NewReader(buf)
	var handle codec.MsgpackHandle
	dec := codec.NewDecoder(r, &handle)
	return dec.Decode(m)
}
