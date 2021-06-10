package uuid

import (
	"crypto/rand"
	"encoding/hex"
)

// Size of a UUID in bytes.
const Size = 16

// UUID versions
const (
	_ byte = iota
	_
	_
	_
	V4
	_

	_ byte = iota
	VariantRFC4122
)

type (
	// UUID representation compliant with specification
	// described in RFC 4122.
	UUID [Size]byte
)

var (
	randomReader = rand.Reader

	// Nil is special form of UUID that is specified to have all
	// 128 bits set to zero.
	Nil = UUID{}
)

// NewV4 returns random generated UUID.
func NewV4() (UUID, error) {
	u := UUID{}
	if _, err := randomReader.Read(u[:]); err != nil {
		return Nil, err
	}
	u.setVersion(V4)
	u.setVariant(VariantRFC4122)

	return u, nil
}

func (u *UUID) setVersion(v byte) {
	u[6] = (u[6] & 0x0f) | (v << 4)
}

func (u *UUID) setVariant(v byte) {
	u[8] = u[8]&(0xff>>2) | (0x02 << 6)
}

func (u UUID) String() string {
	buf := make([]byte, 36)

	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:], u[10:])

	return string(buf)
}
