package fnv1

const (
	// FNV-1
	offset32 = uint32(2166136261)
	prime32  = uint32(16777619)

	// Init32 is what 32 bits hash values should be initialized with.
	Init32 = offset32
)

// HashString32 returns the hash of s.
func HashString32(s string) uint32 {
	return AddString32(Init32, s)
}

// HashBytes32 returns the hash of u.
func HashBytes32(b []byte) uint32 {
	return AddBytes32(Init32, b)
}

// HashUint32 returns the hash of u.
func HashUint32(u uint32) uint32 {
	return AddUint32(Init32, u)
}

// AddString32 adds the hash of s to the precomputed hash value h.
func AddString32(h uint32, s string) uint32 {
	for len(s) >= 8 {
		h = (h * prime32) ^ uint32(s[0])
		h = (h * prime32) ^ uint32(s[1])
		h = (h * prime32) ^ uint32(s[2])
		h = (h * prime32) ^ uint32(s[3])
		h = (h * prime32) ^ uint32(s[4])
		h = (h * prime32) ^ uint32(s[5])
		h = (h * prime32) ^ uint32(s[6])
		h = (h * prime32) ^ uint32(s[7])
		s = s[8:]
	}

	if len(s) >= 4 {
		h = (h * prime32) ^ uint32(s[0])
		h = (h * prime32) ^ uint32(s[1])
		h = (h * prime32) ^ uint32(s[2])
		h = (h * prime32) ^ uint32(s[3])
		s = s[4:]
	}

	if len(s) >= 2 {
		h = (h * prime32) ^ uint32(s[0])
		h = (h * prime32) ^ uint32(s[1])
		s = s[2:]
	}

	if len(s) > 0 {
		h = (h * prime32) ^ uint32(s[0])
	}

	return h
}

// AddBytes32 adds the hash of b to the precomputed hash value h.
func AddBytes32(h uint32, b []byte) uint32 {
	for len(b) >= 8 {
		h = (h * prime32) ^ uint32(b[0])
		h = (h * prime32) ^ uint32(b[1])
		h = (h * prime32) ^ uint32(b[2])
		h = (h * prime32) ^ uint32(b[3])
		h = (h * prime32) ^ uint32(b[4])
		h = (h * prime32) ^ uint32(b[5])
		h = (h * prime32) ^ uint32(b[6])
		h = (h * prime32) ^ uint32(b[7])
		b = b[8:]
	}

	if len(b) >= 4 {
		h = (h * prime32) ^ uint32(b[0])
		h = (h * prime32) ^ uint32(b[1])
		h = (h * prime32) ^ uint32(b[2])
		h = (h * prime32) ^ uint32(b[3])
		b = b[4:]
	}

	if len(b) >= 2 {
		h = (h * prime32) ^ uint32(b[0])
		h = (h * prime32) ^ uint32(b[1])
		b = b[2:]
	}

	if len(b) > 0 {
		h = (h * prime32) ^ uint32(b[0])
	}

	return h
}

// AddUint32 adds the hash value of the 8 bytes of u to h.
func AddUint32(h, u uint32) uint32 {
	h = (h * prime32) ^ ((u >> 24) & 0xFF)
	h = (h * prime32) ^ ((u >> 16) & 0xFF)
	h = (h * prime32) ^ ((u >> 8) & 0xFF)
	h = (h * prime32) ^ ((u >> 0) & 0xFF)
	return h
}
