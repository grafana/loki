package util

import "hash/fnv"

func TokenFor(userID, labels string) uint32 {
	h := fnv.New32()
	h.Write([]byte(userID))
	h.Write([]byte(labels))
	return h.Sum32()
}
