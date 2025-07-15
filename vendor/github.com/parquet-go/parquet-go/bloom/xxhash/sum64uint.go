package xxhash

func Sum64Uint8(v uint8) uint64 {
	h := prime5 + 1
	h ^= uint64(v) * prime5
	return avalanche(rol11(h) * prime1)
}

func Sum64Uint16(v uint16) uint64 {
	h := prime5 + 2
	h ^= uint64(v&0xFF) * prime5
	h = rol11(h) * prime1
	h ^= uint64(v>>8) * prime5
	h = rol11(h) * prime1
	return avalanche(h)
}

func Sum64Uint32(v uint32) uint64 {
	h := prime5 + 4
	h ^= uint64(v) * prime1
	return avalanche(rol23(h)*prime2 + prime3)
}

func Sum64Uint64(v uint64) uint64 {
	h := prime5 + 8
	h ^= round(0, v)
	return avalanche(rol27(h)*prime1 + prime4)
}

func Sum64Uint128(v [16]byte) uint64 {
	h := prime5 + 16
	h ^= round(0, u64(v[:8]))
	h = rol27(h)*prime1 + prime4
	h ^= round(0, u64(v[8:]))
	h = rol27(h)*prime1 + prime4
	return avalanche(h)
}
