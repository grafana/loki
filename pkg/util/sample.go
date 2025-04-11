package util

import (
	"github.com/cespare/xxhash/v2"
)

func UniqueSampleHash(lblString string, line []byte) uint64 {
	uniqueID := make([]byte, 0, len(lblString)+len(line))
	uniqueID = append(uniqueID, lblString...)
	uniqueID = append(uniqueID, ':')
	uniqueID = append(uniqueID, line...)
	return xxhash.Sum64(uniqueID)
}
