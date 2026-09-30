//go:build !amd64

package runtime

// HasAVX2 is whether the CPU and the OS support AVX2: never, on this architecture.
const HasAVX2 = false
