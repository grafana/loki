#include "textflag.h"

// MASK64 computes into R the 64-bit mask of the bytes of the block in Y0 ( the first 32 bytes ) and
// Y1 ( the last 32 bytes ) which are equal to the byte broadcast in Yc, using Yt as a temporary and
// Rt as a temporary register.
//
// Only VEX instructions are used: a legacy SSE instruction mixed with them costs a transition.
#define MASK64(Yc, Yt, R, Rt) \
	VPCMPEQB  Yc, Y0, Yt \
	VPMOVMSKB Yt, R \
	VPCMPEQB  Yc, Y1, Yt \
	VPMOVMSKB Yt, Rt \
	SHLQ      $32, Rt \
	ORQ       Rt, R

// func scanBlockAVX2(p unsafe.Pointer, m *scanMasks)
//
// It computes the masks of the 64 bytes at p: bit i of a mask is set if the byte i is the character.
// The brackets are folded by clearing the bit 5, which makes '{' a '[' and '}' a ']'.
TEXT ·scanBlockAVX2(SB), NOSPLIT, $0-16
	MOVQ p+0(FP), SI
	MOVQ m+8(FP), DI

	VMOVDQU (SI), Y0
	VMOVDQU 32(SI), Y1
	VPBROADCASTB cQuote<>(SB), Y8
	VPBROADCASTB cBackslash<>(SB), Y9
	VPBROADCASTB cOpen<>(SB), Y10
	VPBROADCASTB cClose<>(SB), Y11
	VPBROADCASTB cFold<>(SB), Y12

	MASK64(Y8, Y4, AX, BX)
	MOVQ AX, 0(DI)
	MASK64(Y9, Y4, AX, BX)
	MOVQ AX, 8(DI)
	VPAND Y12, Y0, Y0
	VPAND Y12, Y1, Y1
	MASK64(Y10, Y4, AX, BX)
	MOVQ AX, 16(DI)
	MASK64(Y11, Y4, AX, BX)
	MOVQ AX, 24(DI)
	VZEROUPPER
	RET

DATA cQuote<>+0(SB)/1, $0x22
GLOBL cQuote<>(SB), RODATA|NOPTR, $1
DATA cBackslash<>+0(SB)/1, $0x5c
GLOBL cBackslash<>(SB), RODATA|NOPTR, $1
DATA cOpen<>+0(SB)/1, $0x5b
GLOBL cOpen<>(SB), RODATA|NOPTR, $1
DATA cClose<>+0(SB)/1, $0x5d
GLOBL cClose<>(SB), RODATA|NOPTR, $1
DATA cFold<>+0(SB)/1, $0xdf
GLOBL cFold<>(SB), RODATA|NOPTR, $1
