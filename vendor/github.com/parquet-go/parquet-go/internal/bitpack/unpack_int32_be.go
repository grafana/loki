//go:build s390x

package bitpack

import "encoding/binary"

func unsafecastBytesToUint32(src []byte) []uint32 {
	out := make([]uint32, len(src)/4)
	idx := 0
	for k := range out {
		out[k] = binary.LittleEndian.Uint32((src)[idx:(4 + idx)])
		idx += 4
	}
	return out
}
