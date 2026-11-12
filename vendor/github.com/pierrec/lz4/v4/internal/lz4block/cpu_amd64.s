//go:build gc && !noasm

#include "textflag.h"

// func cpuHasAVX2() bool
TEXT ·cpuHasAVX2(SB), NOSPLIT, $0-1
	// Leaf 7 must exist.
	XORL  AX, AX
	CPUID
	CMPL  AX, $7
	JB    no

	// Leaf 1 ECX: OSXSAVE (bit 27) and AVX (bit 28).
	MOVL  $1, AX
	XORL  CX, CX
	CPUID
	ANDL  $0x18000000, CX
	CMPL  CX, $0x18000000
	JNE   no

	// XCR0: the OS saves XMM (bit 1) and YMM (bit 2) state.
	XORL  CX, CX
	XGETBV
	ANDL  $6, AX
	CMPL  AX, $6
	JNE   no

	// Leaf 7, subleaf 0, EBX: AVX2 (bit 5).
	MOVL  $7, AX
	XORL  CX, CX
	CPUID
	TESTL $0x20, BX
	JZ    no

	MOVB  $1, ret+0(FP)
	RET

no:
	MOVB  $0, ret+0(FP)
	RET

// func cpuHasPrefetchW() bool
TEXT ·cpuHasPrefetchW(SB), NOSPLIT, $0-1
	// Extended leaf 0x80000001 must exist.
	MOVL  $0x80000000, AX
	XORL  CX, CX
	CPUID
	CMPL  AX, $0x80000001
	JB    nopfw

	// Leaf 0x80000001 ECX: PRFCHW (bit 8).
	MOVL  $0x80000001, AX
	XORL  CX, CX
	CPUID
	TESTL $0x100, CX
	JZ    nopfw

	MOVB  $1, ret+0(FP)
	RET

nopfw:
	MOVB  $0, ret+0(FP)
	RET
