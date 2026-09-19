// +build gc
// +build !noasm

#include "textflag.h"

// Register allocation. h and v1 deliberately share a register: v1 is dead
// by the time h is computed, and the tail loops reuse the register file.
#define p	R0
#define n	R1
#define h	R2
#define v1	R2	// Alias for h.
#define v2	R3
#define v3	R4
#define v4	R5
#define x1	R6
#define x2	R7
#define x3	R8
#define x4	R9
#define pend	R10
#define prime1r	R11
#define prime2r	R12
#define prime3r	R13
#define prime4r	R14
#define prime5r	R15
#define total	R16
#define saved	R17

// 16-byte round. Matches the multiplier-pipe-bound schedule gcc emits for
// the reference C: two LDPW pairs, then MADD/ROR/MUL per lane, one pointer
// increment, and nothing else -- the byte-count bookkeeping the Go compiler
// adds to the portable version costs ~15% on Cortex-A72.
#define round16 \
	LDPW  (p), (x1, x2)	\
	LDPW  8(p), (x3, x4)	\
	ADD   $16, p	\
	MADDW prime2r, v1, x1, v1	\
	MADDW prime2r, v2, x2, v2	\
	MADDW prime2r, v3, x3, v3	\
	MADDW prime2r, v4, x4, v4	\
	RORW  $19, v1, v1	\
	RORW  $19, v2, v2	\
	RORW  $19, v3, v3	\
	RORW  $19, v4, v4	\
	MULW  prime1r, v1, v1	\
	MULW  prime1r, v2, v2	\
	MULW  prime1r, v3, v3	\
	MULW  prime1r, v4, v4

// func ChecksumZero(input []byte) uint32
TEXT ·ChecksumZero(SB), NOSPLIT, $0-28
	MOVD input_base+0(FP), p
	MOVD input_len+8(FP), n

	MOVD $2654435761, prime1r
	MOVD $2246822519, prime2r
	MOVD $3266489917, prime3r
	MOVD $668265263, prime4r
	MOVD $374761393, prime5r

	MOVD n, total
	CMP  $16, n
	BLT  small

	// Accumulator init: prime1plus2, prime2, 0, prime1minus.
	MOVD $606290984, v1
	MOVD $2246822519, v2
	MOVD $0, v3
	MOVD $1640531535, v4

	AND $-16, n, x1
	ADD x1, p, pend
	AND $15, n, n

loop16:
	round16
	CMP pend, p
	BLO loop16

	// h = uint32(total) + rol1(v1) + rol7(v2) + rol12(v3) + rol18(v4).
	RORW $31, v1, v1
	RORW $25, v2, v2
	RORW $20, v3, v3
	RORW $14, v4, v4
	ADDW v2, v1
	ADDW v3, v1
	ADDW v4, v1
	ADDW total, h	// v1 is h.
	B    tail4

small:
	ADDW prime5r, total, h

tail4:
	CMP $4, n
	BLT tail1
	MOVWU.P 4(p), x1
	MADDW   prime3r, h, x1, h
	RORW    $15, h, h
	MULW    prime4r, h, h
	SUB     $4, n
	B       tail4

tail1:
	CBZ n, avalanche
	MOVBU.P 1(p), x1
	MADDW   prime5r, h, x1, h
	RORW    $21, h, h
	MULW    prime1r, h, h
	SUB     $1, n
	B       tail1

avalanche:
	EORW h>>15, h, h
	MULW prime2r, h, h
	EORW h>>13, h, h
	MULW prime3r, h, h
	EORW h>>16, h, h
	MOVW h, ret+24(FP)
	RET

// func update(v *[4]uint32, buf *[16]byte, input []byte)
//
// Processes buf (when non-nil) and then every full 16-byte block of input;
// the caller retains the trailing partial block, matching updateGo.
TEXT ·update(SB), NOSPLIT, $0-40
	MOVD v+0(FP), x1
	MOVD buf+8(FP), saved
	MOVD input_base+16(FP), p
	MOVD input_len+24(FP), n

	MOVD $2654435761, prime1r
	MOVD $2246822519, prime2r

	LDPW (x1), (v1, v2)
	LDPW 8(x1), (v3, v4)

	CBZ saved, blocks

	// One round over *buf, preserving the input cursor.
	MOVD p, total
	MOVD saved, p
	round16
	MOVD total, p

blocks:
	AND $-16, n, x1
	ADD x1, p, pend
	CMP pend, p
	BHS store

loop:
	round16
	CMP pend, p
	BLO loop

store:
	MOVD v+0(FP), x1
	STPW (v1, v2), (x1)
	STPW (v3, v4), 8(x1)
	RET
