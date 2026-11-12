#include "textflag.h"

// SPECIAL computes into Yd the lanes of the bytes of Yx which end a run of the plain bytes of a string: a quote, a
// backslash or a control character ( 0x1f or less, which the unsigned minimum with 0x1f leaves unchanged ). Y8, Y9
// and Y10 have the quote, the backslash and 0x1f in every lane; Yt is a temporary.
//
// Only VEX instructions are used: a legacy SSE instruction mixed with them costs a transition.
#define SPECIAL(Yx, Yt, Yd) \
	VPCMPEQB Y8, Yx, Yd \
	VPCMPEQB Y9, Yx, Yt \
	VPOR     Yt, Yd, Yd \
	VPMINUB  Y10, Yx, Yt \
	VPCMPEQB Yx, Yt, Yt \
	VPOR     Yt, Yd, Yd

// func indexStringSpecialAVX2(p unsafe.Pointer, n int) (index int, high uint64)
//
// It returns the index of the first byte of the n bytes at p which is a quote, a backslash or a control
// character, or n if there is none, and high, which is not zero if a byte before it is 0x80 or more. n is 32
// or more. The blocks of 32 bytes are looked at in order, and the last block overlaps the previous one.
TEXT ·indexStringSpecialAVX2(SB), NOSPLIT, $0-32
	MOVQ p+0(FP), SI
	MOVQ n+8(FP), CX

	VPBROADCASTB cQuoteS<>(SB), Y8
	VPBROADCASTB cBackslashS<>(SB), Y9
	VPBROADCASTB c1f<>(SB), Y10

	XORQ R8, R8                  // the top bits of the bytes before the index
	MOVQ SI, DI                  // the address of the block
	LEAQ -32(SI)(CX*1), R11      // the address of the last block
loop:
	CMPQ DI, R11
	JGT  last
	VMOVDQU (DI), Y0
	SPECIAL(Y0, Y1, Y2)
	VPMOVMSKB Y2, DX
	VPMOVMSKB Y0, BX
	TESTL DX, DX
	JNE   found
	ORL   BX, R8
	ADDQ  $32, DI
	JMP   loop
last:
	// the last block, which overlaps the previous one unless the blocks ended exactly at n.
	LEAQ (SI)(CX*1), AX
	CMPQ DI, AX
	JEQ  none
	MOVQ R11, DI
	VMOVDQU (DI), Y0
	SPECIAL(Y0, Y1, Y2)
	VPMOVMSKB Y2, DX
	VPMOVMSKB Y0, BX
	TESTL DX, DX
	JNE   found
	ORL   BX, R8
none:
	VZEROUPPER
	MOVQ CX, index+16(FP)
	MOVQ R8, high+24(FP)
	RET
found:
	// the top bits of the bytes of the block before the special one: below the lowest bit of DX.
	MOVL  DX, AX
	NEGL  AX
	ANDL  DX, AX
	DECL  AX
	ANDL  AX, BX
	ORL   BX, R8
	BSFL  DX, DX
	SUBQ  SI, DI
	ADDQ  DX, DI
	VZEROUPPER
	MOVQ  DI, index+16(FP)
	MOVQ  R8, high+24(FP)
	RET

DATA cQuoteS<>+0(SB)/1, $0x22
GLOBL cQuoteS<>(SB), RODATA|NOPTR, $1
DATA cBackslashS<>+0(SB)/1, $0x5c
GLOBL cBackslashS<>(SB), RODATA|NOPTR, $1
DATA c1f<>+0(SB)/1, $0x1f
GLOBL c1f<>(SB), RODATA|NOPTR, $1
