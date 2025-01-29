// Package chash implements an set of low-level consistent hashing algorithms.
// A higher level API is exposed by the shard package.
package chash

// Hash is a consistent hashing algorithm. Implementations of Hash are
// goroutine safe.
type Hash interface {
	// Get will retrieve the n owners for key. Get will return an error if there
	// are not at least n nodes.
	Get(key uint64, n int) ([]string, error)

	// SetNodes updates the set of nodes used for hashing.
	SetNodes(nodes []string)
}
