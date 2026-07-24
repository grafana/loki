package sarama

import "encoding/binary"

// murmur2 implements the same hashing algorithm used by the Apache Kafka Java
// client's DefaultPartitioner (org.apache.kafka.common.utils.Utils.murmur2).
// It is a variant of MurmurHash2 with a fixed seed of 0x9747b28c and
// little-endian byte ordering.
//
// Reference:
//   - https://github.com/apache/kafka/blob/4.3.0/clients/src/main/java/org/apache/kafka/common/utils/Utils.java#L496-L541
//   - https://github.com/aappleby/smhasher/blob/master/src/MurmurHash2.cpp
func murmur2(b []byte) uint32 {
	const (
		seed uint32 = 0x9747b28c
		m    uint32 = 0x5bd1e995
		r           = 24
	)
	h := seed ^ uint32(len(b))
	for len(b) >= 4 {
		k := binary.LittleEndian.Uint32(b)
		b = b[4:]
		k *= m
		k ^= k >> r
		k *= m
		h *= m
		h ^= k
	}
	switch len(b) {
	case 3:
		h ^= uint32(b[2]) << 16
		fallthrough
	case 2:
		h ^= uint32(b[1]) << 8
		fallthrough
	case 1:
		h ^= uint32(b[0])
		h *= m
	}
	h ^= h >> 13
	h *= m
	h ^= h >> 15
	return h
}
