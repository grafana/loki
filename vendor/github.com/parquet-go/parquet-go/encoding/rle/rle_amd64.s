//go:build !purego

#include "textflag.h"

GLOBL bitMasks<>(SB), RODATA|NOPTR, $64
DATA bitMasks<>+0(SB)/8,  $0b0000000100000001000000010000000100000001000000010000000100000001
DATA bitMasks<>+8(SB)/8,  $0b0000001100000011000000110000001100000011000000110000001100000011
DATA bitMasks<>+16(SB)/8, $0b0000011100000111000001110000011100000111000001110000011100000111
DATA bitMasks<>+24(SB)/8, $0b0000111100001111000011110000111100001111000011110000111100001111
DATA bitMasks<>+32(SB)/8, $0b0001111100011111000111110001111100011111000111110001111100011111
DATA bitMasks<>+40(SB)/8, $0b0011111100111111001111110011111100111111001111110011111100111111
DATA bitMasks<>+48(SB)/8, $0b0111111101111111011111110111111101111111011111110111111101111111
DATA bitMasks<>+56(SB)/8, $0b1111111111111111111111111111111111111111111111111111111111111111

// func decodeBytesBitpackBMI2(dst, src []byte, count, bitWidth uint)
TEXT ·decodeBytesBitpackBMI2(SB), NOSPLIT, $0-64
    MOVQ dst_base+0(FP), AX
    MOVQ src_base+24(FP), BX
    MOVQ count+48(FP), CX
    MOVQ bitWidth+56(FP), DX
    LEAQ bitMasks<>(SB), DI
    MOVQ -8(DI)(DX*8), DI
    XORQ SI, SI
    SHRQ $3, CX
    JMP test
loop:
    MOVQ (BX), R8
    PDEPQ DI, R8, R8
    MOVQ R8, (AX)(SI*8)
    ADDQ DX, BX
    INCQ SI
test:
    CMPQ SI, CX
    JNE loop
    RET

// func encodeBytesBitpackBMI2(dst []byte, src []uint64, bitWidth uint) int
TEXT ·encodeBytesBitpackBMI2(SB), NOSPLIT, $0-64
    MOVQ dst_base+0(FP), AX
    MOVQ src_base+24(FP), BX
    MOVQ src_len+32(FP), CX
    MOVQ bitWidth+48(FP), DX
    LEAQ bitMasks<>(SB), DI
    MOVQ -8(DI)(DX*8), DI
    XORQ SI, SI
    JMP test
loop:
    MOVQ (BX)(SI*8), R8
    PEXTQ DI, R8, R8
    MOVQ R8, (AX)
    ADDQ DX, AX
    INCQ SI
test:
    CMPQ SI, CX
    JNE loop
done:
    SUBQ dst+0(FP), AX
    MOVQ AX, ret+56(FP)
    RET

// func encodeInt32IndexEqual8ContiguousAVX2(words [][8]int32) int
TEXT ·encodeInt32IndexEqual8ContiguousAVX2(SB), NOSPLIT, $0-32
    MOVQ words_base+0(FP), AX
    MOVQ words_len+8(FP), BX
    XORQ SI, SI
    SHLQ $5, BX
    JMP test
loop:
    VMOVDQU (AX)(SI*1), Y0
    VPSHUFD $0, Y0, Y1
    VPCMPEQD Y1, Y0, Y0
    VMOVMSKPS Y0, CX
    CMPL CX, $0xFF
    JE done
    ADDQ $32, SI
test:
    CMPQ SI, BX
    JNE loop
done:
    VZEROUPPER
    SHRQ $5, SI
    MOVQ SI, ret+24(FP)
    RET

// func encodeInt32IndexEqual8ContiguousSSE(words [][8]int32) int
TEXT ·encodeInt32IndexEqual8ContiguousSSE(SB), NOSPLIT, $0-32
    MOVQ words_base+0(FP), AX
    MOVQ words_len+8(FP), BX
    XORQ SI, SI
    SHLQ $5, BX
    JMP test
loop:
    MOVOU (AX)(SI*1), X0
    MOVOU 16(AX)(SI*1), X1
    PSHUFD $0, X0, X2
    PCMPEQL X2, X0
    PCMPEQL X2, X1
    MOVMSKPS X0, CX
    MOVMSKPS X1, DX
    ANDL DX, CX
    CMPL CX, $0xF
    JE done
    ADDQ $32, SI
test:
    CMPQ SI, BX
    JNE loop
done:
    SHRQ $5, SI
    MOVQ SI, ret+24(FP)
    RET

// func encodeInt32Bitpack1to16bitsAVX2(dst []byte, src [][8]int32, bitWidth uint) int
TEXT ·encodeInt32Bitpack1to16bitsAVX2(SB), NOSPLIT, $0-64
    MOVQ dst_base+0(FP), AX
    MOVQ src_base+24(FP), BX
    MOVQ src_len+32(FP), CX
    MOVQ bitWidth+48(FP), DX

    MOVQ DX, X0
    VPBROADCASTQ X0, Y6 // [1*bitWidth...]
    VPSLLQ $1, Y6, Y7   // [2*bitWidth...]
    VPADDQ Y6, Y7, Y8   // [3*bitWidth...]
    VPSLLQ $2, Y6, Y9   // [4*bitWidth...]

    MOVQ $64, DI
    MOVQ DI, X1
    VPBROADCASTQ X1, Y10
    VPSUBQ Y6, Y10, Y11 // [64-1*bitWidth...]
    VPSUBQ Y9, Y10, Y12 // [64-4*bitWidth...]
    VPCMPEQQ Y4, Y4, Y4
    VPSRLVQ Y11, Y4, Y4

    VPXOR Y5, Y5, Y5
    XORQ SI, SI
    SHLQ $5, CX
    JMP test
loop:
    VMOVDQU (BX)(SI*1), Y0
    VPSHUFD $0b01010101, Y0, Y1
    VPSHUFD $0b10101010, Y0, Y2
    VPSHUFD $0b11111111, Y0, Y3

    VPAND Y4, Y0, Y0
    VPAND Y4, Y1, Y1
    VPAND Y4, Y2, Y2
    VPAND Y4, Y3, Y3

    VPSLLVQ Y6, Y1, Y1
    VPSLLVQ Y7, Y2, Y2
    VPSLLVQ Y8, Y3, Y3

    VPOR Y1, Y0, Y0
    VPOR Y3, Y2, Y2
    VPOR Y2, Y0, Y0

    VPERMQ $0b00001010, Y0, Y1

    VPSLLVQ X9, X1, X2
    VPSRLQ X12, X1, X3
    VBLENDPD $0b10, X3, X2, X1
    VBLENDPD $0b10, X5, X0, X0
    VPOR X1, X0, X0

    VMOVDQU X0, (AX)

    ADDQ DX, AX
    ADDQ $32, SI
test:
    CMPQ SI, CX
    JNE loop
    VZEROUPPER
    SUBQ dst+0(FP), AX
    MOVQ AX, ret+56(FP)
    RET
