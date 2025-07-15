//go:build !purego

package bytealg

import "golang.org/x/sys/cpu"

var (
	hasAVX2 = cpu.X86.HasAVX2
	// These use AVX-512 instructions in the countByte algorithm relies
	// operations that are available in the AVX512BW extension:
	// * VPCMPUB
	// * KMOVQ
	//
	// Note that the function will fallback to an AVX2 version if those
	// instructions are not available.
	hasAVX512Count = cpu.X86.HasAVX512VL && cpu.X86.HasAVX512BW
)
