#include "textflag.h"

// MASK computes into Vd the mask of the bytes of Vx which may need an escape, using Vt as a temporary:
// the lane of such a byte is not zero. See nibbleTables: V8 is the table of the low nibbles, V9 the one of
// the high nibbles and V10 has 0x0f in every lane.
#define MASK(Vx, Vt, Vd) \
	VAND  V10.B16, Vx.B16, Vt.B16 \
	VUSHR $4, Vx.B16, Vd.B16 \
	VTBL  Vt.B16, [V8.B16], Vt.B16 \
	VTBL  Vd.B16, [V9.B16], Vd.B16 \
	VAND  Vt.B16, Vd.B16, Vd.B16

// ANY sets R8 to a value which is not zero if a lane of Vd is not zero.
#define ANY(Vd) \
	VUADDLV Vd.B16, V15 \
	VMOV    V15.H[0], R8

// func scanStringNEON(p unsafe.Pointer, n int, tables *nibbleTables) int
//
// It returns 1 if a byte of the n bytes at p may need an escape by the tables, or 0. n is 16 or more.
// The blocks of 64 bytes are looked at first, then the blocks of 16 bytes, and the last block overlaps the
// previous one.
TEXT ·scanStringNEON(SB), NOSPLIT, $0-32
	MOVD p+0(FP), R0
	MOVD n+8(FP), R1
	MOVD tables+16(FP), R2

	VLD1 (R2), [V8.B16, V9.B16]
	MOVD $0x0f, R4
	VMOV R4, V10.B16

	MOVD R0, R7                  // the address of the block
	SUB  $64, R1, R6
	ADD  R0, R6, R6              // the address of the last block of 64 bytes
	CMP  R6, R7
	BGT  small
loop64:
	VLD1.P 64(R7), [V0.B16, V1.B16, V2.B16, V3.B16]
	MASK(V0, V4, V5)
	MASK(V1, V4, V6)
	VORR   V6.B16, V5.B16, V5.B16
	MASK(V2, V4, V6)
	VORR   V6.B16, V5.B16, V5.B16
	MASK(V3, V4, V6)
	VORR   V6.B16, V5.B16, V5.B16
	ANY(V5)
	CBNZ   R8, found
	CMP    R6, R7
	BLE    loop64
small:
	SUB  $16, R1, R6
	ADD  R0, R6, R6              // the address of the last block of 16 bytes
	CMP  R6, R7
	BGT  last
loop16:
	VLD1.P 16(R7), [V0.B16]
	MASK(V0, V4, V5)
	ANY(V5)
	CBNZ   R8, found
	CMP    R6, R7
	BLE    loop16
last:
	// the last block, which overlaps the previous one unless the blocks ended exactly at n.
	ADD  R0, R1, R4
	CMP  R4, R7
	BEQ  none
	VLD1 (R6), [V0.B16]
	MASK(V0, V4, V5)
	ANY(V5)
	CBNZ R8, found
none:
	MOVD $0, R8
	MOVD R8, ret+24(FP)
	RET
found:
	MOVD $1, R8
	MOVD R8, ret+24(FP)
	RET
