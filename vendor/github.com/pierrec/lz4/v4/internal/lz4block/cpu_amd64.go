//go:build gc && !noasm

package lz4block

// hasAVX2 selects decodeBlock's AVX2 loops, and hasPrefetchW the variant of
// them that prefetches for ownership. Tests clear them to exercise the
// fallbacks on hardware that has both.
var (
	hasAVX2      = cpuHasAVX2()
	hasPrefetchW = cpuHasPrefetchW()
)

// cpuHasAVX2 reports whether the CPU supports AVX2 and the OS saves the
// YMM registers.
//
//go:noescape
func cpuHasAVX2() bool

// cpuHasPrefetchW reports whether the CPU enumerates PREFETCHW
// (CPUID.80000001H:ECX.PRFCHW[bit 8]).
//
//go:noescape
func cpuHasPrefetchW() bool
