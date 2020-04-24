package fnv1a

const (
	// FNV-1a
	offset32 = uint32(2166136261)
	prime32  = uint32(16777619)

	// Init32 is what 32 bits hash values should be initialized with.
	Init32 = offset32
)

// HashString32 returns the hash of s.
func HashString32(s string) uint32 {
	return AddString32(Init32, s)
}

// HashUint32 returns the hash of u.
func HashUint32(u uint32) uint32 {
	return AddUint32(Init32, u)
}

// AddString32 adds the hash of s to the precomputed hash value h.
func AddString32(h uint32, s string) uint32 {
	i := 0
	n := (len(s) / 8) * 8

	for i != n {
		h = (h ^ uint32(s[i])) * prime32
		h = (h ^ uint32(s[i+1])) * prime32
		h = (h ^ uint32(s[i+2])) * prime32
		h = (h ^ uint32(s[i+3])) * prime32
		h = (h ^ uint32(s[i+4])) * prime32
		h = (h ^ uint32(s[i+5])) * prime32
		h = (h ^ uint32(s[i+6])) * prime32
		h = (h ^ uint32(s[i+7])) * prime32
		i += 8
	}

	for _, c := range s[i:] {
		h = (h ^ uint32(c)) * prime32
	}

	return h
}

// AddUint32 adds the hash value of the 8 bytes of u to h.
func AddUint32(h, u uint32) uint32 {
	h = (h ^ ((u >> 24) & 0xFF)) * prime32
	h = (h ^ ((u >> 16) & 0xFF)) * prime32
	h = (h ^ ((u >> 8) & 0xFF)) * prime32
	h = (h ^ ((u >> 0) & 0xFF)) * prime32
	return h
}
