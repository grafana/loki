#include "textflag.h"

DATA prime_neon<>+0(SB)/4, $0x9e3779b1
DATA prime_neon<>+4(SB)/4, $0x9e3779b1
DATA prime_neon<>+8(SB)/4, $0x9e3779b1
DATA prime_neon<>+12(SB)/4, $0x9e3779b1
GLOBL prime_neon<>(SB), RODATA|NOPTR, $16

// XTN Vd.2S, Vn.2D - Narrow 64-bit to 32-bit (low part)
// Encoding: 0|0|0|01110|10|100001|001010|Rn|Rd = 0x0EA12800
#define XTN_2S_2D(Vd, Vn) WORD $(0x0EA12800 | ((Vn) << 5) | (Vd))

// SHRN #32, Vn.2D, Vd.2S - Shift right 32 and narrow (high part)
#define SHRN_32_2D_2S(Vd, Vn) WORD $(0x0F208400 | ((Vn) << 5) | (Vd))

// UMULL Vd.2D, Vn.2S, Vm.2S - Widening multiply 32x32→64
#define UMULL_2D_2S_2S(Vd, Vn, Vm) WORD $(0x2EA0C000 | ((Vm) << 16) | ((Vn) << 5) | (Vd))

// ROUND processes one 16-byte chunk: data XOR key, multiply, accumulate
// Uses V5 for data, V6 for key, V7/V8/V9 temps
// V_acc_num = numeric reg for macros (1-4), V_acc = symbolic (V1-V4)
#define ROUND(data_off, key_off, V_acc_num, V_acc) \
	ADD  $data_off, R1, R5          \
	VLD1 (R5), [V5.D2]              \
	ADD  $key_off, R2, R5           \
	VLD1 (R5), [V6.D2]              \
	VEOR V5.B16, V6.B16, V6.B16     \
	XTN_2S_2D(7, 6)                 \
	SHRN_32_2D_2S(8, 6)             \
	UMULL_2D_2S_2S(9, 7, 8)         \
	VEXT $8, V5.B16, V5.B16, V5.B16 \
	VADD V5.D2, V_acc.D2, V_acc.D2  \
	VADD V9.D2, V_acc.D2, V_acc.D2

// SCRAMBLE for one accumulator pair
#define SCRAMBLE(key_off, V_acc_num, V_acc) \
	VUSHR $47, V_acc.D2, V5.D2         \
	VEOR  V_acc.B16, V5.B16, V_acc.B16 \
	ADD   $key_off, R2, R5             \
	VLD1  (R5), [V5.D2]                \
	VEOR  V_acc.B16, V5.B16, V_acc.B16 \
	XTN_2S_2D(6, V_acc_num)            \
	SHRN_32_2D_2S(7, V_acc_num)        \
	UMULL_2D_2S_2S(V_acc_num, 6, 0)    \
	UMULL_2D_2S_2S(8, 7, 0)            \
	VSHL  $32, V8.D2, V8.D2            \
	VADD  V8.D2, V_acc.D2, V_acc.D2

// func accumNEON(acc *[8]uint64, data *byte, key *byte, len uint64)
TEXT ·accumNEON(SB), NOSPLIT, $0-32
	MOVD acc+0(FP), R0
	MOVD data+8(FP), R1
	MOVD key+16(FP), R2
	MOVD len+24(FP), R3

	// Load accumulators: V1=[acc0,acc1], V2=[acc2,acc3], V3=[acc4,acc5], V4=[acc6,acc7]
	VLD1 (R0), [V1.D2, V2.D2, V3.D2, V4.D2]

	// Load prime constant
	MOVD $prime_neon<>(SB), R4
	VLD1 (R4), [V0.D2]

accum_large:
	CMP $1024, R3
	BLE accum

	// Process 1024 bytes = 16 stripes of 64 bytes each
	// Stripe 0: data[0:64], key[0:64]
	ROUND(0, 0, 1, V1)
	ROUND(16, 16, 2, V2)
	ROUND(32, 32, 3, V3)
	ROUND(48, 48, 4, V4)

	// Stripe 1: data[64:128], key[8:72]
	ROUND(64, 8, 1, V1)
	ROUND(80, 24, 2, V2)
	ROUND(96, 40, 3, V3)
	ROUND(112, 56, 4, V4)

	// Stripe 2: data[128:192], key[16:80]
	ROUND(128, 16, 1, V1)
	ROUND(144, 32, 2, V2)
	ROUND(160, 48, 3, V3)
	ROUND(176, 64, 4, V4)

	// Stripe 3: data[192:256], key[24:88]
	ROUND(192, 24, 1, V1)
	ROUND(208, 40, 2, V2)
	ROUND(224, 56, 3, V3)
	ROUND(240, 72, 4, V4)

	// Stripe 4: data[256:320], key[32:96]
	ROUND(256, 32, 1, V1)
	ROUND(272, 48, 2, V2)
	ROUND(288, 64, 3, V3)
	ROUND(304, 80, 4, V4)

	// Stripe 5: data[320:384], key[40:104]
	ROUND(320, 40, 1, V1)
	ROUND(336, 56, 2, V2)
	ROUND(352, 72, 3, V3)
	ROUND(368, 88, 4, V4)

	// Stripe 6: data[384:448], key[48:112]
	ROUND(384, 48, 1, V1)
	ROUND(400, 64, 2, V2)
	ROUND(416, 80, 3, V3)
	ROUND(432, 96, 4, V4)

	// Stripe 7: data[448:512], key[56:120]
	ROUND(448, 56, 1, V1)
	ROUND(464, 72, 2, V2)
	ROUND(480, 88, 3, V3)
	ROUND(496, 104, 4, V4)

	// Stripe 8: data[512:576], key[64:128]
	ROUND(512, 64, 1, V1)
	ROUND(528, 80, 2, V2)
	ROUND(544, 96, 3, V3)
	ROUND(560, 112, 4, V4)

	// Stripe 9: data[576:640], key[72:136] -> but key is 192 bytes, key[72]=ok
	ROUND(576, 72, 1, V1)
	ROUND(592, 88, 2, V2)
	ROUND(608, 104, 3, V3)
	ROUND(624, 120, 4, V4)

	// Stripe 10: data[640:704], key[80:144]
	ROUND(640, 80, 1, V1)
	ROUND(656, 96, 2, V2)
	ROUND(672, 112, 3, V3)
	ROUND(688, 128, 4, V4)

	// Stripe 11: data[704:768], key[88:152]
	ROUND(704, 88, 1, V1)
	ROUND(720, 104, 2, V2)
	ROUND(736, 120, 3, V3)
	ROUND(752, 136, 4, V4)

	// Stripe 12: data[768:832], key[96:160]
	ROUND(768, 96, 1, V1)
	ROUND(784, 112, 2, V2)
	ROUND(800, 128, 3, V3)
	ROUND(816, 144, 4, V4)

	// Stripe 13: data[832:896], key[104:168]
	ROUND(832, 104, 1, V1)
	ROUND(848, 120, 2, V2)
	ROUND(864, 136, 3, V3)
	ROUND(880, 152, 4, V4)

	// Stripe 14: data[896:960], key[112:176]
	ROUND(896, 112, 1, V1)
	ROUND(912, 128, 2, V2)
	ROUND(928, 144, 3, V3)
	ROUND(944, 160, 4, V4)

	// Stripe 15: data[960:1024], key[120:184]
	ROUND(960, 120, 1, V1)
	ROUND(976, 136, 2, V2)
	ROUND(992, 152, 3, V3)
	ROUND(1008, 168, 4, V4)

	// Scramble with key[128:]
	SCRAMBLE(128, 1, V1)
	SCRAMBLE(144, 2, V2)
	SCRAMBLE(160, 3, V3)
	SCRAMBLE(176, 4, V4)

	ADD $1024, R1, R1
	SUB $1024, R3, R3
	B   accum_large

accum:
	// If no remaining bytes, we're done
	CBZ R3, done

	// Compute number of full stripes: R6 = (R3-1) / 64
	SUB $1, R3, R6
	LSR $6, R6, R6
	CBZ R6, finalize

	MOVD $0, R4 // key offset

accum_loop:
	// Process one 64-byte stripe
	ADD  R4, R2, R5
	VLD1 (R1), [V5.D2]
	VLD1 (R5), [V6.D2]
	VEOR V5.B16, V6.B16, V6.B16
	XTN_2S_2D(7, 6)
	SHRN_32_2D_2S(8, 6)
	UMULL_2D_2S_2S(9, 7, 8)
	VEXT $8, V5.B16, V5.B16, V5.B16
	VADD V5.D2, V1.D2, V1.D2
	VADD V9.D2, V1.D2, V1.D2

	ADD  $16, R1, R7
	ADD  $16, R5, R5
	VLD1 (R7), [V5.D2]
	VLD1 (R5), [V6.D2]
	VEOR V5.B16, V6.B16, V6.B16
	XTN_2S_2D(7, 6)
	SHRN_32_2D_2S(8, 6)
	UMULL_2D_2S_2S(9, 7, 8)
	VEXT $8, V5.B16, V5.B16, V5.B16
	VADD V5.D2, V2.D2, V2.D2
	VADD V9.D2, V2.D2, V2.D2

	ADD  $32, R1, R7
	ADD  $16, R5, R5
	VLD1 (R7), [V5.D2]
	VLD1 (R5), [V6.D2]
	VEOR V5.B16, V6.B16, V6.B16
	XTN_2S_2D(7, 6)
	SHRN_32_2D_2S(8, 6)
	UMULL_2D_2S_2S(9, 7, 8)
	VEXT $8, V5.B16, V5.B16, V5.B16
	VADD V5.D2, V3.D2, V3.D2
	VADD V9.D2, V3.D2, V3.D2

	ADD  $48, R1, R7
	ADD  $16, R5, R5
	VLD1 (R7), [V5.D2]
	VLD1 (R5), [V6.D2]
	VEOR V5.B16, V6.B16, V6.B16
	XTN_2S_2D(7, 6)
	SHRN_32_2D_2S(8, 6)
	UMULL_2D_2S_2S(9, 7, 8)
	VEXT $8, V5.B16, V5.B16, V5.B16
	VADD V5.D2, V4.D2, V4.D2
	VADD V9.D2, V4.D2, V4.D2

	ADD  $64, R1, R1
	SUB  $64, R3, R3
	ADD  $8, R4, R4
	AND  $127, R4, R4
	SUBS $1, R6, R6
	BNE  accum_loop

finalize:
	// Always process final stripe if there's remaining data (R3 > 0)
	CBZ R3, done

	// Adjust data pointer to last 64 bytes: R1 = R1 + R3 - 64
	ADD R3, R1, R6
	SUB $64, R6, R6

	// Final key offset is 121
	ADD  $121, R2, R5
	VLD1 (R6), [V5.D2]
	VLD1 (R5), [V6.D2]
	VEOR V5.B16, V6.B16, V6.B16
	XTN_2S_2D(7, 6)
	SHRN_32_2D_2S(8, 6)
	UMULL_2D_2S_2S(9, 7, 8)
	VEXT $8, V5.B16, V5.B16, V5.B16
	VADD V5.D2, V1.D2, V1.D2
	VADD V9.D2, V1.D2, V1.D2

	ADD  $16, R6, R6
	ADD  $16, R5, R5
	VLD1 (R6), [V5.D2]
	VLD1 (R5), [V6.D2]
	VEOR V5.B16, V6.B16, V6.B16
	XTN_2S_2D(7, 6)
	SHRN_32_2D_2S(8, 6)
	UMULL_2D_2S_2S(9, 7, 8)
	VEXT $8, V5.B16, V5.B16, V5.B16
	VADD V5.D2, V2.D2, V2.D2
	VADD V9.D2, V2.D2, V2.D2

	ADD  $16, R6, R6
	ADD  $16, R5, R5
	VLD1 (R6), [V5.D2]
	VLD1 (R5), [V6.D2]
	VEOR V5.B16, V6.B16, V6.B16
	XTN_2S_2D(7, 6)
	SHRN_32_2D_2S(8, 6)
	UMULL_2D_2S_2S(9, 7, 8)
	VEXT $8, V5.B16, V5.B16, V5.B16
	VADD V5.D2, V3.D2, V3.D2
	VADD V9.D2, V3.D2, V3.D2

	ADD  $16, R6, R6
	ADD  $16, R5, R5
	VLD1 (R6), [V5.D2]
	VLD1 (R5), [V6.D2]
	VEOR V5.B16, V6.B16, V6.B16
	XTN_2S_2D(7, 6)
	SHRN_32_2D_2S(8, 6)
	UMULL_2D_2S_2S(9, 7, 8)
	VEXT $8, V5.B16, V5.B16, V5.B16
	VADD V5.D2, V4.D2, V4.D2
	VADD V9.D2, V4.D2, V4.D2

done:
	VST1 [V1.D2, V2.D2, V3.D2, V4.D2], (R0)
	RET

// func accumBlockNEON(acc *[8]uint64, data *byte, key *byte)
// Processes exactly 1024 bytes (16 stripes) and scrambles
TEXT ·accumBlockNEON(SB), NOSPLIT, $0-24
	MOVD acc+0(FP), R0
	MOVD data+8(FP), R1
	MOVD key+16(FP), R2

	VLD1 (R0), [V1.D2, V2.D2, V3.D2, V4.D2]

	// Load prime constant for scramble
	MOVD $prime_neon<>(SB), R4
	VLD1 (R4), [V0.D2]

	// Stripe 0
	ROUND(0, 0, 1, V1)
	ROUND(16, 16, 2, V2)
	ROUND(32, 32, 3, V3)
	ROUND(48, 48, 4, V4)

	// Stripe 1
	ROUND(64, 8, 1, V1)
	ROUND(80, 24, 2, V2)
	ROUND(96, 40, 3, V3)
	ROUND(112, 56, 4, V4)

	// Stripe 2
	ROUND(128, 16, 1, V1)
	ROUND(144, 32, 2, V2)
	ROUND(160, 48, 3, V3)
	ROUND(176, 64, 4, V4)

	// Stripe 3
	ROUND(192, 24, 1, V1)
	ROUND(208, 40, 2, V2)
	ROUND(224, 56, 3, V3)
	ROUND(240, 72, 4, V4)

	// Stripe 4
	ROUND(256, 32, 1, V1)
	ROUND(272, 48, 2, V2)
	ROUND(288, 64, 3, V3)
	ROUND(304, 80, 4, V4)

	// Stripe 5
	ROUND(320, 40, 1, V1)
	ROUND(336, 56, 2, V2)
	ROUND(352, 72, 3, V3)
	ROUND(368, 88, 4, V4)

	// Stripe 6
	ROUND(384, 48, 1, V1)
	ROUND(400, 64, 2, V2)
	ROUND(416, 80, 3, V3)
	ROUND(432, 96, 4, V4)

	// Stripe 7
	ROUND(448, 56, 1, V1)
	ROUND(464, 72, 2, V2)
	ROUND(480, 88, 3, V3)
	ROUND(496, 104, 4, V4)

	// Stripe 8
	ROUND(512, 64, 1, V1)
	ROUND(528, 80, 2, V2)
	ROUND(544, 96, 3, V3)
	ROUND(560, 112, 4, V4)

	// Stripe 9
	ROUND(576, 72, 1, V1)
	ROUND(592, 88, 2, V2)
	ROUND(608, 104, 3, V3)
	ROUND(624, 120, 4, V4)

	// Stripe 10
	ROUND(640, 80, 1, V1)
	ROUND(656, 96, 2, V2)
	ROUND(672, 112, 3, V3)
	ROUND(688, 128, 4, V4)

	// Stripe 11
	ROUND(704, 88, 1, V1)
	ROUND(720, 104, 2, V2)
	ROUND(736, 120, 3, V3)
	ROUND(752, 136, 4, V4)

	// Stripe 12
	ROUND(768, 96, 1, V1)
	ROUND(784, 112, 2, V2)
	ROUND(800, 128, 3, V3)
	ROUND(816, 144, 4, V4)

	// Stripe 13
	ROUND(832, 104, 1, V1)
	ROUND(848, 120, 2, V2)
	ROUND(864, 136, 3, V3)
	ROUND(880, 152, 4, V4)

	// Stripe 14
	ROUND(896, 112, 1, V1)
	ROUND(912, 128, 2, V2)
	ROUND(928, 144, 3, V3)
	ROUND(944, 160, 4, V4)

	// Stripe 15
	ROUND(960, 120, 1, V1)
	ROUND(976, 136, 2, V2)
	ROUND(992, 152, 3, V3)
	ROUND(1008, 168, 4, V4)

	// Scramble
	SCRAMBLE(128, 1, V1)
	SCRAMBLE(144, 2, V2)
	SCRAMBLE(160, 3, V3)
	SCRAMBLE(176, 4, V4)

	VST1 [V1.D2, V2.D2, V3.D2, V4.D2], (R0)
	RET
