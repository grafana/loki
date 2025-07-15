//go:build s390x

package parquet

import (
	"encoding/binary"

	"github.com/parquet-go/parquet-go/deprecated"
)

func unsafecastInt96ToBytes(src []deprecated.Int96) []byte {
	out := make([]byte, len(src)*12)
	for i := range src {
		binary.LittleEndian.PutUint32(out[(i*12):4+(i*12)], uint32(src[i][0]))
		binary.LittleEndian.PutUint32(out[4+(i*12):8+(i*12)], uint32(src[i][1]))
		binary.LittleEndian.PutUint32(out[8+(i*12):12+(i*12)], uint32(src[i][2]))
	}
	return out
}
