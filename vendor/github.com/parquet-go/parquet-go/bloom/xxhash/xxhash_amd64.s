//go:build !purego

#include "textflag.h"

#define PRIME1 0x9E3779B185EBCA87
#define PRIME2 0xC2B2AE3D27D4EB4F
#define PRIME3 0x165667B19E3779F9
#define PRIME4 0x85EBCA77C2B2AE63
#define PRIME5 0x27D4EB2F165667C5

DATA prime3<>+0(SB)/8, $PRIME3
GLOBL prime3<>(SB), RODATA|NOPTR, $8

DATA prime5<>+0(SB)/8, $PRIME5
GLOBL prime5<>(SB), RODATA|NOPTR, $8

// Register allocation:
// AX	h
// SI	pointer to advance through b
// DX	n
// BX	loop end
// R8	v1, k1
// R9	v2
// R10	v3
// R11	v4
// R12	tmp
// R13	PRIME1
// R14	PRIME2
// DI	PRIME4

// round reads from and advances the buffer pointer in SI.
// It assumes that R13 has PRIME1 and R14 has PRIME2.
#define round(r) \
	MOVQ  (SI), R12 \
	ADDQ  $8, SI    \
	IMULQ R14, R12  \
	ADDQ  R12, r    \
	ROLQ  $31, r    \
	IMULQ R13, r

// mergeRound applies a merge round on the two registers acc and val.
// It assumes that R13 has PRIME1, R14 has PRIME2, and DI has PRIME4.
#define mergeRound(acc, val) \
	IMULQ R14, val \
	ROLQ  $31, val \
	IMULQ R13, val \
	XORQ  val, acc \
	IMULQ R13, acc \
	ADDQ  DI, acc

// func Sum64(b []byte) uint64
TEXT ·Sum64(SB), NOSPLIT, $0-32
	// Load fixed primes.
	MOVQ $PRIME1, R13
	MOVQ $PRIME2, R14
	MOVQ $PRIME4, DI

	// Load slice.
	MOVQ b_base+0(FP), SI
	MOVQ b_len+8(FP), DX
	LEAQ (SI)(DX*1), BX

	// The first loop limit will be len(b)-32.
	SUBQ $32, BX

	// Check whether we have at least one block.
	CMPQ DX, $32
	JLT  noBlocks

	// Set up initial state (v1, v2, v3, v4).
	MOVQ R13, R8
	ADDQ R14, R8
	MOVQ R14, R9
	XORQ R10, R10
	XORQ R11, R11
	SUBQ R13, R11

	// Loop until SI > BX.
blockLoop:
	round(R8)
	round(R9)
	round(R10)
	round(R11)

	CMPQ SI, BX
	JLE  blockLoop

	MOVQ R8, AX
	ROLQ $1, AX
	MOVQ R9, R12
	ROLQ $7, R12
	ADDQ R12, AX
	MOVQ R10, R12
	ROLQ $12, R12
	ADDQ R12, AX
	MOVQ R11, R12
	ROLQ $18, R12
	ADDQ R12, AX

	mergeRound(AX, R8)
	mergeRound(AX, R9)
	mergeRound(AX, R10)
	mergeRound(AX, R11)

	JMP afterBlocks

noBlocks:
	MOVQ $PRIME5, AX

afterBlocks:
	ADDQ DX, AX

	// Right now BX has len(b)-32, and we want to loop until SI > len(b)-8.
	ADDQ $24, BX

	CMPQ SI, BX
	JG   fourByte

wordLoop:
	// Calculate k1.
	MOVQ  (SI), R8
	ADDQ  $8, SI
	IMULQ R14, R8
	ROLQ  $31, R8
	IMULQ R13, R8

	XORQ  R8, AX
	ROLQ  $27, AX
	IMULQ R13, AX
	ADDQ  DI, AX

	CMPQ SI, BX
	JLE  wordLoop

fourByte:
	ADDQ $4, BX
	CMPQ SI, BX
	JG   singles

	MOVL  (SI), R8
	ADDQ  $4, SI
	IMULQ R13, R8
	XORQ  R8, AX

	ROLQ  $23, AX
	IMULQ R14, AX
	ADDQ  prime3<>(SB), AX

singles:
	ADDQ $4, BX
	CMPQ SI, BX
	JGE  finalize

singlesLoop:
	MOVBQZX (SI), R12
	ADDQ    $1, SI
	IMULQ   prime5<>(SB), R12
	XORQ    R12, AX

	ROLQ  $11, AX
	IMULQ R13, AX

	CMPQ SI, BX
	JL   singlesLoop

finalize:
	MOVQ  AX, R12
	SHRQ  $33, R12
	XORQ  R12, AX
	IMULQ R14, AX
	MOVQ  AX, R12
	SHRQ  $29, R12
	XORQ  R12, AX
	IMULQ prime3<>(SB), AX
	MOVQ  AX, R12
	SHRQ  $32, R12
	XORQ  R12, AX

	MOVQ AX, ret+24(FP)
	RET
