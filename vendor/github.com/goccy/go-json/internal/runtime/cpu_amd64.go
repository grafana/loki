package runtime

func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)
func xgetbv() (eax, edx uint32)

// HasAVX2 is whether the CPU and the OS support AVX2.
var HasAVX2 = detectAVX2()

func detectAVX2() bool {
	maxID, _, _, _ := cpuid(0, 0)
	if maxID < 7 {
		return false
	}
	_, _, ecx1, _ := cpuid(1, 0)
	const osxsave, avx = 1 << 27, 1 << 28
	if ecx1&osxsave == 0 || ecx1&avx == 0 {
		return false
	}
	// the OS saves the YMM registers.
	if eax, _ := xgetbv(); eax&6 != 6 {
		return false
	}
	_, ebx7, _, _ := cpuid(7, 0)
	const avx2 = 1 << 5
	return ebx7&avx2 != 0
}
