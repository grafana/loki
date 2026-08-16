//go:build !purego && !goexperiment.simd

#include "textflag.h"

// func unpackInt64x8bitsAVX2(dst []int64, src []byte)
TEXT ·unpackInt64x8bitsAVX2(SB), NOSPLIT, $0-48
	MOVQ dst_base+0(FP), AX
	MOVQ dst_len+8(FP), DX
	MOVQ src_base+24(FP), BX
	SHLQ $3, DX
	ADDQ AX, DX
unpack_int64_8bit_avx2_loop:
	VPMOVZXBQ 0(BX), Y0
	VPMOVZXBQ 4(BX), Y1
	VMOVDQU Y0, 0(AX)
	VMOVDQU Y1, 32(AX)
	ADDQ $8, BX
	ADDQ $64, AX
	CMPQ AX, DX
	JB unpack_int64_8bit_avx2_loop
	VZEROUPPER
	RET

// func unpackInt64x16bitsAVX2(dst []int64, src []byte)
TEXT ·unpackInt64x16bitsAVX2(SB), NOSPLIT, $0-48
	MOVQ dst_base+0(FP), AX
	MOVQ dst_len+8(FP), DX
	MOVQ src_base+24(FP), BX
	SHLQ $3, DX
	ADDQ AX, DX
unpack_int64_16bit_avx2_loop:
	VPMOVZXWQ 0(BX), Y0
	VPMOVZXWQ 8(BX), Y1
	VMOVDQU Y0, 0(AX)
	VMOVDQU Y1, 32(AX)
	ADDQ $16, BX
	ADDQ $64, AX
	CMPQ AX, DX
	JB unpack_int64_16bit_avx2_loop
	VZEROUPPER
	RET

// func unpackInt64x32bitsAVX2(dst []int64, src []byte)
TEXT ·unpackInt64x32bitsAVX2(SB), NOSPLIT, $0-48
	MOVQ dst_base+0(FP), AX
	MOVQ dst_len+8(FP), DX
	MOVQ src_base+24(FP), BX
	SHLQ $3, DX
	ADDQ AX, DX
unpack_int64_32bit_avx2_loop:
	VPMOVZXDQ 0(BX), Y0
	VPMOVZXDQ 16(BX), Y1
	VMOVDQU Y0, 0(AX)
	VMOVDQU Y1, 32(AX)
	ADDQ $32, BX
	ADDQ $64, AX
	CMPQ AX, DX
	JB unpack_int64_32bit_avx2_loop
	VZEROUPPER
	RET

// unpackInt64x9to31bitsAVX2 decodes eight values per iteration. The caller
// handles widths outside 9-31 and uses a dedicated widening kernel for 16.
//
// func unpackInt64x9to31bitsAVX2(dst []int64, src []byte, bitWidth uint)
TEXT ·unpackInt64x9to31bitsAVX2(SB), NOSPLIT, $0-56
	MOVQ dst_base+0(FP), AX
	MOVQ dst_len+8(FP), DX
	MOVQ src_base+24(FP), BX
	MOVQ bitWidth+48(FP), CX

	SHLQ $3, DX
	ADDQ AX, DX

	MOVQ $1, R8
	SHLQ CX, R8
	DECQ R8
	MOVQ R8, X0
	VPBROADCASTQ X0, Y0

	MOVQ CX, R9
	SUBQ $9, R9
	SHLQ $7, R9
	LEAQ ·permuteInt64Table(SB), R10
	VMOVDQU 0(R10)(R9*1), Y7
	VMOVDQU 32(R10)(R9*1), Y8
	VMOVDQU 64(R10)(R9*1), Y5
	VMOVDQU 96(R10)(R9*1), Y6

unpack_int64_9to31_avx2_loop:
	VMOVDQU (BX), Y1
	VPERMD Y1, Y7, Y2
	VPERMD Y1, Y8, Y3
	VPSRLVQ Y5, Y2, Y2
	VPSRLVQ Y6, Y3, Y3
	VPAND Y0, Y2, Y2
	VPAND Y0, Y3, Y3
	VMOVDQU Y2, (AX)
	VMOVDQU Y3, 32(AX)
	ADDQ CX, BX
	ADDQ $64, AX
	CMPQ AX, DX
	JB unpack_int64_9to31_avx2_loop
	VZEROUPPER
	RET
