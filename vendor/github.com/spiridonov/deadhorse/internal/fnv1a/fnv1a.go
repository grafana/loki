// Package fnv1a is the 64-bit FNV-1a hash shared by every key-hashing site in
// this module (client.ShardedClient.shardFor picks a shard with it; the
// server's store picks a stripe with it). The two uses don't need to agree
// with each other -- each hashes into its own, independently-sized space --
// but a single implementation means there's only one copy of the
// offset/prime constants to keep correct.
package fnv1a

// Hash computes the 64-bit FNV-1a hash of s.
func Hash(s string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211

	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}
