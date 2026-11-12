#include "textflag.h"

// MASK64 computes into R the 64-bit mask of the bytes of the block in V0..V3 which are equal to the byte
// broadcast in Vc. Every lane of a compare is 0xff or 0: it is masked to the weight of the lane in its
// byte of the mask ( V20: 1, 2, 4 ... 128, repeated ), and three pairwise additions sum the weights of
// the eight lanes of every byte of the mask, in the order of the block.
#define MASK64(Vc, R) \
	VCMEQ Vc.B16, V0.B16, V4.B16 \
	VCMEQ Vc.B16, V1.B16, V5.B16 \
	VCMEQ Vc.B16, V2.B16, V6.B16 \
	VCMEQ Vc.B16, V3.B16, V7.B16 \
	VAND  V20.B16, V4.B16, V4.B16 \
	VAND  V20.B16, V5.B16, V5.B16 \
	VAND  V20.B16, V6.B16, V6.B16 \
	VAND  V20.B16, V7.B16, V7.B16 \
	VADDP V5.B16, V4.B16, V4.B16 \
	VADDP V7.B16, V6.B16, V6.B16 \
	VADDP V6.B16, V4.B16, V4.B16 \
	VADDP V4.B16, V4.B16, V4.B16 \
	VMOV  V4.D[0], R

// func scanBlockNEON(p unsafe.Pointer, m *scanMasks)
//
// It computes the masks of the 64 bytes at p: bit i of a mask is set if the byte i is the character.
// The brackets are folded by clearing the bit 5, which makes '{' a '[' and '}' a ']'.
TEXT ·scanBlockNEON(SB), NOSPLIT, $0-16
	MOVD p+0(FP), R0
	MOVD m+8(FP), R1

	VLD1 (R0), [V0.B16, V1.B16, V2.B16, V3.B16]
	MOVD $weights<>(SB), R2
	VLD1 (R2), [V20.B16]
	MOVD $0x22, R3
	VMOV R3, V16.B16
	MOVD $0x5c, R3
	VMOV R3, V17.B16
	MOVD $0x5b, R3
	VMOV R3, V18.B16
	MOVD $0x5d, R3
	VMOV R3, V19.B16
	MOVD $0xdf, R3
	VMOV R3, V21.B16

	MASK64(V16, R4)
	MOVD R4, 0(R1)
	MASK64(V17, R4)
	MOVD R4, 8(R1)
	VAND V21.B16, V0.B16, V0.B16
	VAND V21.B16, V1.B16, V1.B16
	VAND V21.B16, V2.B16, V2.B16
	VAND V21.B16, V3.B16, V3.B16
	MASK64(V18, R4)
	MOVD R4, 16(R1)
	MASK64(V19, R4)
	MOVD R4, 24(R1)
	RET

DATA weights<>+0(SB)/8, $0x8040201008040201
DATA weights<>+8(SB)/8, $0x8040201008040201
GLOBL weights<>(SB), RODATA|NOPTR, $16
