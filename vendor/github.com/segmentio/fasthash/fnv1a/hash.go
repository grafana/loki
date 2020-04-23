package fnv1a

const (
	// FNV-1a
	offset64 = uint64(14695981039346656037)
	prime64  = uint64(1099511628211)

	// Init64 is what 64 bits hash values should be initialized with.
	Init64 = offset64
)

// HashString64 returns the hash of s.
func HashString64(s string) uint64 {
	return AddString64(Init64, s)
}

// HashUint64 returns the hash of u.
func HashUint64(u uint64) uint64 {
	return AddUint64(Init64, u)
}

// AddString64 adds the hash of s to the precomputed hash value h.
func AddString64(h uint64, s string) uint64 {
	/*
		This is an unrolled version of this algorithm:

		for _, c := range s {
			h = (h ^ uint64(c)) * prime64
		}

		It seems to be ~1.5x faster than the simple loop in BenchmarkHash64:

		- BenchmarkHash64/hash_function-4   30000000   56.1 ns/op   642.15 MB/s   0 B/op   0 allocs/op
		- BenchmarkHash64/hash_function-4   50000000   38.6 ns/op   932.35 MB/s   0 B/op   0 allocs/op

	*/

	i := 0
	n := (len(s) / 8) * 8

	for i != n {
		h = (h ^ uint64(s[i])) * prime64
		h = (h ^ uint64(s[i+1])) * prime64
		h = (h ^ uint64(s[i+2])) * prime64
		h = (h ^ uint64(s[i+3])) * prime64
		h = (h ^ uint64(s[i+4])) * prime64
		h = (h ^ uint64(s[i+5])) * prime64
		h = (h ^ uint64(s[i+6])) * prime64
		h = (h ^ uint64(s[i+7])) * prime64
		i += 8
	}

	for _, c := range s[i:] {
		h = (h ^ uint64(c)) * prime64
	}

	return h
}

// AddUint64 adds the hash value of the 8 bytes of u to h.
func AddUint64(h uint64, u uint64) uint64 {
	h = (h ^ ((u >> 56) & 0xFF)) * prime64
	h = (h ^ ((u >> 48) & 0xFF)) * prime64
	h = (h ^ ((u >> 40) & 0xFF)) * prime64
	h = (h ^ ((u >> 32) & 0xFF)) * prime64
	h = (h ^ ((u >> 24) & 0xFF)) * prime64
	h = (h ^ ((u >> 16) & 0xFF)) * prime64
	h = (h ^ ((u >> 8) & 0xFF)) * prime64
	h = (h ^ ((u >> 0) & 0xFF)) * prime64
	return h
}
