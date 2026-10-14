//go:build gc && !noasm
// +build gc,!noasm

#include "textflag.h"

// The Go compiler's code for these loops is already bound by multiply
// throughput, so this keeps its lane-by-lane schedule (load, multiply, add
// and rotate each lane, then the four final multiplies) and only fixes what
// the compiler cannot: the loops' alignment. The compiled loop in updateGo
// is 133 bytes at wherever the function lands; on Zen 3 it ran 1.7x slower
// when an unrelated change moved it by 32 bytes. Here each loop starts on a
// 64-byte boundary (PCALIGN $64 also raises the function's alignment).

#define prime1 $-1640531535 // 2654435761
#define prime2 $-2048144777 // 2246822519
#define prime3 $-1028477379 // 3266489917
#define prime4 $668265263
#define prime5 $374761393

// v1..v4 in DX, R8, R9, R10; scratch R11..R14.
#define ROUND(p) \
	MOVL   0(p), R11           \
	IMUL3L prime2, R11, R11    \
	ADDL   DX, R11             \
	ROLL   $13, R11            \
	MOVL   4(p), R12           \
	IMUL3L prime2, R12, R12    \
	ADDL   R8, R12             \
	ROLL   $13, R12            \
	MOVL   8(p), R13           \
	IMUL3L prime2, R13, R13    \
	ADDL   R9, R13             \
	ROLL   $13, R13            \
	MOVL   12(p), R14          \
	IMUL3L prime2, R14, R14    \
	ADDL   R10, R14            \
	ROLL   $13, R14            \
	IMUL3L prime1, R11, DX     \
	IMUL3L prime1, R12, R8     \
	IMUL3L prime1, R13, R9     \
	IMUL3L prime1, R14, R10

// func ChecksumZero(input []byte) uint32
TEXT ·ChecksumZero(SB), NOSPLIT, $0-28
	MOVQ input_base+0(FP), SI
	MOVQ input_len+8(FP), CX
	MOVL CX, AX // h32 = uint32(n)
	CMPQ CX, $16
	JB   small

	MOVL $606290984, DX  // prime1 + prime2
	MOVL $2246822519, R8 // prime2
	XORL R9, R9
	MOVL $1640531535, R10 // -prime1
	MOVQ CX, BX
	ANDQ $-16, BX
	ADDQ SI, BX // end of the full 16-byte blocks
	ANDQ $15, CX

	PCALIGN $64
loop16:
	ROUND(SI)
	ADDQ $16, SI
	CMPQ SI, BX
	JB   loop16

	// h32 += rol1(v1) + rol7(v2) + rol12(v3) + rol18(v4)
	ROLL $1, DX
	ROLL $7, R8
	ROLL $12, R9
	ROLL $18, R10
	ADDL R8, DX
	ADDL R9, DX
	ADDL R10, DX
	ADDL DX, AX
	JMP  tail4

small:
	ADDL prime5, AX

tail4:
	CMPQ   CX, $4
	JB     tail1
	MOVL   (SI), R11
	IMUL3L prime3, R11, R11
	ADDL   R11, AX
	ROLL   $17, AX
	IMUL3L prime4, AX, AX
	ADDQ   $4, SI
	SUBQ   $4, CX
	JMP    tail4

tail1:
	TESTQ  CX, CX
	JZ     avalanche
	MOVBLZX (SI), R11
	IMUL3L prime5, R11, R11
	ADDL   R11, AX
	ROLL   $11, AX
	IMUL3L prime1, AX, AX
	INCQ   SI
	DECQ   CX
	JMP    tail1

avalanche:
	MOVL   AX, R11
	SHRL   $15, R11
	XORL   R11, AX
	IMUL3L prime2, AX, AX
	MOVL   AX, R11
	SHRL   $13, R11
	XORL   R11, AX
	IMUL3L prime3, AX, AX
	MOVL   AX, R11
	SHRL   $16, R11
	XORL   R11, AX
	MOVL   AX, ret+24(FP)
	RET

// func update(v *[4]uint32, buf *[16]byte, input []byte)
//
// Processes buf (when non-nil) and then every full 16-byte block of input;
// the caller retains the trailing partial block, matching updateGo.
TEXT ·update(SB), NOSPLIT, $0-40
	MOVQ v+0(FP), AX
	MOVQ buf+8(FP), BX
	MOVQ input_base+16(FP), SI
	MOVQ input_len+24(FP), CX

	MOVL 0(AX), DX
	MOVL 4(AX), R8
	MOVL 8(AX), R9
	MOVL 12(AX), R10

	TESTQ BX, BX
	JZ    blocks
	ROUND(BX)

blocks:
	ANDQ $-16, CX
	JZ   store
	ADDQ SI, CX // end of the full 16-byte blocks

	PCALIGN $64
loop:
	ROUND(SI)
	ADDQ $16, SI
	CMPQ SI, CX
	JB   loop

store:
	MOVL DX, 0(AX)
	MOVL R8, 4(AX)
	MOVL R9, 8(AX)
	MOVL R10, 12(AX)
	RET
