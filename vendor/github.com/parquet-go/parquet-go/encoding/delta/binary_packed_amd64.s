//go:build !purego

#include "textflag.h"

#define blockSize 128
#define numMiniBlocks 4
#define miniBlockSize 32

// -----------------------------------------------------------------------------
// 32 bits
// -----------------------------------------------------------------------------

#define deltaInt32AVX2x8(baseAddr) \
    VMOVDQU baseAddr, Y1    \ // [0,1,2,3,4,5,6,7]
    VPERMD Y1, Y3, Y2       \ // [7,0,1,2,3,4,5,6]
    VPBLENDD $1, Y0, Y2, Y2 \ // [x,0,1,2,3,4,5,6]
    VPSUBD Y2, Y1, Y2       \ // [0,1,2,...] - [x,0,1,...]
    VMOVDQU Y2, baseAddr    \
    VPERMD Y1, Y3, Y0

// func blockDeltaInt32AVX2(block *[blockSize]int32, lastValue int32) int32
TEXT ·blockDeltaInt32AVX2(SB), NOSPLIT, $0-20
    MOVQ block+0(FP), AX
    MOVL 4*blockSize-4(AX), CX
    MOVL CX, ret+16(FP)

    VPBROADCASTD lastValue+8(FP), Y0
    VMOVDQU ·rotateLeft32(SB), Y3

    XORQ SI, SI
loop:
    deltaInt32AVX2x8(0(AX)(SI*4))
    deltaInt32AVX2x8(32(AX)(SI*4))
    deltaInt32AVX2x8(64(AX)(SI*4))
    deltaInt32AVX2x8(96(AX)(SI*4))
    ADDQ $32, SI
    CMPQ SI, $blockSize
    JNE loop
    VZEROUPPER
    RET

// func blockMinInt32AVX2(block *[blockSize]int32) int32
TEXT ·blockMinInt32AVX2(SB), NOSPLIT, $0-12
    MOVQ block+0(FP), AX
    VPBROADCASTD (AX), Y15

    VPMINSD 0(AX), Y15, Y0
    VPMINSD 32(AX), Y15, Y1
    VPMINSD 64(AX), Y15, Y2
    VPMINSD 96(AX), Y15, Y3
    VPMINSD 128(AX), Y15, Y4
    VPMINSD 160(AX), Y15, Y5
    VPMINSD 192(AX), Y15, Y6
    VPMINSD 224(AX), Y15, Y7
    VPMINSD 256(AX), Y15, Y8
    VPMINSD 288(AX), Y15, Y9
    VPMINSD 320(AX), Y15, Y10
    VPMINSD 352(AX), Y15, Y11
    VPMINSD 384(AX), Y15, Y12
    VPMINSD 416(AX), Y15, Y13
    VPMINSD 448(AX), Y15, Y14
    VPMINSD 480(AX), Y15, Y15

    VPMINSD Y1, Y0, Y0
    VPMINSD Y3, Y2, Y2
    VPMINSD Y5, Y4, Y4
    VPMINSD Y7, Y6, Y6
    VPMINSD Y9, Y8, Y8
    VPMINSD Y11, Y10, Y10
    VPMINSD Y13, Y12, Y12
    VPMINSD Y15, Y14, Y14

    VPMINSD Y2, Y0, Y0
    VPMINSD Y6, Y4, Y4
    VPMINSD Y10, Y8, Y8
    VPMINSD Y14, Y12, Y12

    VPMINSD Y4, Y0, Y0
    VPMINSD Y12, Y8, Y8

    VPMINSD Y8, Y0, Y0

    VPERM2I128 $1, Y0, Y0, Y1
    VPMINSD Y1, Y0, Y0

    VPSHUFD $0b00011011, Y0, Y1
    VPMINSD Y1, Y0, Y0
    VZEROUPPER

    MOVQ X0, CX
    MOVL CX, BX
    SHRQ $32, CX
    CMPL CX, BX
    CMOVLLT CX, BX
    MOVL BX, ret+8(FP)
    RET

#define subInt32AVX2x32(baseAddr, offset) \
    VMOVDQU offset+0(baseAddr), Y1      \
    VMOVDQU offset+32(baseAddr), Y2     \
    VMOVDQU offset+64(baseAddr), Y3     \
    VMOVDQU offset+96(baseAddr), Y4     \
    VPSUBD Y0, Y1, Y1                   \
    VPSUBD Y0, Y2, Y2                   \
    VPSUBD Y0, Y3, Y3                   \
    VPSUBD Y0, Y4, Y4                   \
    VMOVDQU Y1, offset+0(baseAddr)      \
    VMOVDQU Y2, offset+32(baseAddr)     \
    VMOVDQU Y3, offset+64(baseAddr)     \
    VMOVDQU Y4, offset+96(baseAddr)

// func blockSubInt32AVX2(block *[blockSize]int32, value int32)
TEXT ·blockSubInt32AVX2(SB), NOSPLIT, $0-12
    MOVQ block+0(FP), AX
    VPBROADCASTD value+8(FP), Y0
    subInt32AVX2x32(AX, 0)
    subInt32AVX2x32(AX, 128)
    subInt32AVX2x32(AX, 256)
    subInt32AVX2x32(AX, 384)
    VZEROUPPER
    RET

// func blockBitWidthsInt32AVX2(bitWidths *[numMiniBlocks]byte, block *[blockSize]int32)
TEXT ·blockBitWidthsInt32AVX2(SB), NOSPLIT, $0-16
    MOVQ bitWidths+0(FP), AX
    MOVQ block+8(FP), BX

    // AVX2 only has signed comparisons (and min/max), we emulate working on
    // unsigned values by adding -2^31 to the values. Y5 is a vector of -2^31
    // used to offset 8 packed 32 bits integers in other YMM registers where
    // the block data are loaded.
    VPCMPEQD Y5, Y5, Y5
    VPSLLD $31, Y5, Y5

    XORQ DI, DI
loop:
    VPBROADCASTD (BX), Y0 // max
    VPADDD Y5, Y0, Y0

    VMOVDQU (BX), Y1
    VMOVDQU 32(BX), Y2
    VMOVDQU 64(BX), Y3
    VMOVDQU 96(BX), Y4

    VPADDD Y5, Y1, Y1
    VPADDD Y5, Y2, Y2
    VPADDD Y5, Y3, Y3
    VPADDD Y5, Y4, Y4

    VPMAXSD Y2, Y1, Y1
    VPMAXSD Y4, Y3, Y3
    VPMAXSD Y3, Y1, Y1
    VPMAXSD Y1, Y0, Y0

    VPERM2I128 $1, Y0, Y0, Y1
    VPMAXSD Y1, Y0, Y0

    VPSHUFD $0b00011011, Y0, Y1
    VPMAXSD Y1, Y0, Y0
    VPSUBD Y5, Y0, Y0

    MOVQ X0, CX
    MOVL CX, DX
    SHRQ $32, CX
    CMPL CX, DX
    CMOVLHI CX, DX

    LZCNTL DX, DX
    NEGL DX
    ADDL $32, DX
    MOVB DX, (AX)(DI*1)

    ADDQ $128, BX
    INCQ DI
    CMPQ DI, $numMiniBlocks
    JNE loop
    VZEROUPPER
    RET

// encodeMiniBlockInt32Default is the generic implementation of the algorithm to
// pack 32 bit integers into values of a given bit width (<=32).
//
// This algorithm is much slower than the vectorized versions, but is useful
// as a reference implementation to run the tests against, and as fallback when
// the code runs on a CPU which does not support the AVX2 instruction set.
//
// func encodeMiniBlockInt32Default(dst *byte, src *[miniBlockSize]int32, bitWidth uint)
TEXT ·encodeMiniBlockInt32Default(SB), NOSPLIT, $0-24
    MOVQ dst+0(FP), AX
    MOVQ src+8(FP), BX
    MOVQ bitWidth+16(FP), R9

    XORQ DI, DI // bitOffset
    XORQ SI, SI
loop:
    MOVQ DI, CX
    MOVQ DI, DX

    ANDQ $0b11111, CX // bitOffset % 32
    SHRQ $5, DX       // bitOffset / 32

    MOVLQZX (BX)(SI*4), R8
    SHLQ CX, R8
    ORQ R8, (AX)(DX*4)

    ADDQ R9, DI
    INCQ SI
    CMPQ SI, $miniBlockSize
    JNE loop
    RET

// encodeMiniBlockInt32x1bitAVX2 packs 32 bit integers into 1 bit values in the
// the output buffer.
//
// The algorithm uses MOVMSKPS to extract the 8 relevant bits from the 8 values
// packed in YMM registers, then combines 4 of these into a 32 bit word which
// then gets written to the output. The result is 32 bits because each mini
// block has 32 values (the block size is 128 and there are 4 mini blocks per
// block).
//
// func encodeMiniBlockInt32x1bitAVX2(dst *byte, src *[miniBlockSize]int32)
TEXT ·encodeMiniBlockInt32x1bitAVX2(SB), NOSPLIT, $0-16
    MOVQ dst+0(FP), AX
    MOVQ src+8(FP), BX

    VMOVDQU 0(BX), Y0
    VMOVDQU 32(BX), Y1
    VMOVDQU 64(BX), Y2
    VMOVDQU 96(BX), Y3

    VPSLLD $31, Y0, Y0
    VPSLLD $31, Y1, Y1
    VPSLLD $31, Y2, Y2
    VPSLLD $31, Y3, Y3

    VMOVMSKPS Y0, R8
    VMOVMSKPS Y1, R9
    VMOVMSKPS Y2, R10
    VMOVMSKPS Y3, R11

    SHLL $8, R9
    SHLL $16, R10
    SHLL $24, R11

    ORL R9, R8
    ORL R10, R8
    ORL R11, R8
    MOVL R8, (AX)
    VZEROUPPER
    RET

// encodeMiniBlockInt32x2bitsAVX2 implements an algorithm for packing 32 bit
// integers into 2 bit values.
//
// The algorithm is derived from the one employed in encodeMiniBlockInt32x1bitAVX2
// but needs to perform a bit extra work since MOVMSKPS can only extract one bit
// per packed integer of each YMM vector. We run two passes to extract the two
// bits needed to compose each item of the result, and merge the values by
// interleaving the first and second bits with PDEP.
//
// func encodeMiniBlockInt32x2bitsAVX2(dst *byte, src *[miniBlockSize]int32)
TEXT ·encodeMiniBlockInt32x2bitsAVX2(SB), NOSPLIT, $0-16
    MOVQ dst+0(FP), AX
    MOVQ src+8(FP), BX

    VMOVDQU 0(BX), Y0
    VMOVDQU 32(BX), Y1
    VMOVDQU 64(BX), Y2
    VMOVDQU 96(BX), Y3

    VPSLLD $31, Y0, Y4
    VPSLLD $31, Y1, Y5
    VPSLLD $31, Y2, Y6
    VPSLLD $31, Y3, Y7

    VMOVMSKPS Y4, R8
    VMOVMSKPS Y5, R9
    VMOVMSKPS Y6, R10
    VMOVMSKPS Y7, R11

    SHLQ $8, R9
    SHLQ $16, R10
    SHLQ $24, R11
    ORQ R9, R8
    ORQ R10, R8
    ORQ R11, R8

    MOVQ $0x5555555555555555, DX // 0b010101...
    PDEPQ DX, R8, R8

    VPSLLD $30, Y0, Y8
    VPSLLD $30, Y1, Y9
    VPSLLD $30, Y2, Y10
    VPSLLD $30, Y3, Y11

    VMOVMSKPS Y8, R12
    VMOVMSKPS Y9, R13
    VMOVMSKPS Y10, R14
    VMOVMSKPS Y11, R15

    SHLQ $8, R13
    SHLQ $16, R14
    SHLQ $24, R15
    ORQ R13, R12
    ORQ R14, R12
    ORQ R15, R12

    MOVQ $0xAAAAAAAAAAAAAAAA, DI // 0b101010...
    PDEPQ DI, R12, R12

    ORQ R12, R8
    MOVQ R8, (AX)
    VZEROUPPER
    RET

// encodeMiniBlockInt32x32bitsAVX2 is a specialization of the bit packing logic
// for 32 bit integers when the output bit width is also 32, in which case a
// simple copy of the mini block to the output buffer produces the result.
//
// func encodeMiniBlockInt32x32bitsAVX2(dst *byte, src *[miniBlockSize]int32)
TEXT ·encodeMiniBlockInt32x32bitsAVX2(SB), NOSPLIT, $0-16
    MOVQ dst+0(FP), AX
    MOVQ src+8(FP), BX
    VMOVDQU 0(BX), Y0
    VMOVDQU 32(BX), Y1
    VMOVDQU 64(BX), Y2
    VMOVDQU 96(BX), Y3
    VMOVDQU Y0, 0(AX)
    VMOVDQU Y1, 32(AX)
    VMOVDQU Y2, 64(AX)
    VMOVDQU Y3, 96(AX)
    VZEROUPPER
    RET

// encodeMiniBlockInt32x3to16bitsAVX2 is the algorithm used to bit-pack 32 bit
// integers into values of width 3 to 16 bits.
//
// This function is a small overhead due to having to initialize registers with
// values that depend on the bit width. We measured this cost at ~10% throughput
// in synthetic benchmarks compared to generating constant shifts and offsets
// using a macro. Using a single function rather than generating one for each
// bit width has the benefit of reducing the code size, which in practice can
// also yield benefits like reducing CPU cache misses. Not using a macro also
// has other advantages like providing accurate line number of stack traces and
// enabling the use of breakpoints when debugging. Overall, this approach seemed
// to be the right trade off between performance and maintainability.
//
// The algorithm treats chunks of 8 values in 4 iterations to process all 32
// values of the mini block. Writes to the output buffer are aligned on 128 bits
// since we may write up to 128 bits (8 x 16 bits). Padding is therefore
// required in the output buffer to avoid triggering a segfault.
// The encodeInt32AVX2 method adds enough padding when sizing the output buffer
// to account for this requirement.
//
// We leverage the two lanes of YMM registers to work on two sets of 4 values
// (in the sequence of VMOVDQU/VPSHUFD, VPAND, VPSLLQ, VPOR), resulting in having
// two sets of bit-packed values in the lower 64 bits of each YMM lane.
// The upper lane is then permuted into a lower lane to merge the two results,
// which may not be aligned on byte boundaries so we shift the lower and upper
// bits and compose two sets of 128 bits sequences (VPSLLQ, VPSRLQ, VBLENDPD),
// merge them and write the 16 bytes result to the output buffer.
TEXT ·encodeMiniBlockInt32x3to16bitsAVX2(SB), NOSPLIT, $0-24
    MOVQ dst+0(FP), AX
    MOVQ src+8(FP), BX
    MOVQ bitWidth+16(FP), CX

    VPBROADCASTQ bitWidth+16(FP), Y6 // [1*bitWidth...]
    VPSLLQ $1, Y6, Y7                // [2*bitWidth...]
    VPADDQ Y6, Y7, Y8                // [3*bitWidth...]
    VPSLLQ $2, Y6, Y9                // [4*bitWidth...]

    VPBROADCASTQ sixtyfour<>(SB), Y10
    VPSUBQ Y6, Y10, Y11 // [64-1*bitWidth...]
    VPSUBQ Y9, Y10, Y12 // [64-4*bitWidth...]
    VPCMPEQQ Y4, Y4, Y4
    VPSRLVQ Y11, Y4, Y4

    VPXOR Y5, Y5, Y5
    XORQ SI, SI
loop:
    VMOVDQU (BX)(SI*4), Y0
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

    ADDQ CX, AX
    ADDQ $8, SI
    CMPQ SI, $miniBlockSize
    JNE loop
    VZEROUPPER
    RET

GLOBL sixtyfour<>(SB), RODATA|NOPTR, $32
DATA sixtyfour<>+0(SB)/8, $64
DATA sixtyfour<>+8(SB)/8, $64
DATA sixtyfour<>+16(SB)/8, $64
DATA sixtyfour<>+24(SB)/8, $64

// func decodeBlockInt32Default(dst []int32, minDelta, lastValue int32) int32
TEXT ·decodeBlockInt32Default(SB), NOSPLIT, $0-36
    MOVQ dst_base+0(FP), AX
    MOVQ dst_len+8(FP), BX
    MOVLQZX minDelta+24(FP), CX
    MOVLQZX lastValue+28(FP), DX
    XORQ SI, SI
    JMP test
loop:
    MOVL (AX)(SI*4), DI
    ADDL CX, DI
    ADDL DI, DX
    MOVL DX, (AX)(SI*4)
    INCQ SI
test:
    CMPQ SI, BX
    JNE loop
done:
    MOVL DX, ret+32(FP)
    RET

// func decodeBlockInt32AVX2(dst []int32, minDelta, lastValue int32) int32
TEXT ·decodeBlockInt32AVX2(SB), NOSPLIT, $0-36
    MOVQ dst_base+0(FP), AX
    MOVQ dst_len+8(FP), BX
    MOVLQZX minDelta+24(FP), CX
    MOVLQZX lastValue+28(FP), DX
    XORQ SI, SI

    CMPQ BX, $8
    JB test

    MOVQ BX, DI
    SHRQ $3, DI
    SHLQ $3, DI

    VPXOR X1, X1, X1
    MOVQ CX, X0
    MOVQ DX, X1
    VPBROADCASTD X0, Y0
loopAVX2:
    VMOVDQU (AX)(SI*4), Y2
    VPADDD Y0, Y2, Y2 // Y2[:] += minDelta
    VPADDD Y1, Y2, Y2 // Y2[0] += lastValue

    VPSLLDQ $4, Y2, Y3
    VPADDD Y3, Y2, Y2

    VPSLLDQ $8, Y2, Y3
    VPADDD Y3, Y2, Y2

    VPSHUFD $0xFF, X2, X1
    VPERM2I128 $1, Y2, Y2, Y3
    VPADDD X1, X3, X3

    VMOVDQU X2, (AX)(SI*4)
    VMOVDQU X3, 16(AX)(SI*4)
    VPSRLDQ $12, X3, X1 // lastValue

    ADDQ $8, SI
    CMPQ SI, DI
    JNE loopAVX2
    VZEROUPPER
    MOVQ X1, DX
    JMP test
loop:
    MOVL (AX)(SI*4), DI
    ADDL CX, DI
    ADDL DI, DX
    MOVL DX, (AX)(SI*4)
    INCQ SI
test:
    CMPQ SI, BX
    JNE loop
done:
    MOVL DX, ret+32(FP)
    RET

// -----------------------------------------------------------------------------
// 64 bits
// -----------------------------------------------------------------------------

#define deltaInt64AVX2x4(baseAddr)  \
    VMOVDQU baseAddr, Y1            \ // [0,1,2,3]
    VPERMQ $0b10010011, Y1, Y2      \ // [3,0,1,2]
    VPBLENDD $3, Y0, Y2, Y2         \ // [x,0,1,2]
    VPSUBQ Y2, Y1, Y2               \ // [0,1,2,3] - [x,0,1,2]
    VMOVDQU Y2, baseAddr            \
    VPERMQ $0b10010011, Y1, Y0

// func blockDeltaInt64AVX2(block *[blockSize]int64, lastValue int64) int64
TEXT ·blockDeltaInt64AVX2(SB), NOSPLIT, $0-24
    MOVQ block+0(FP), AX
    MOVQ 8*blockSize-8(AX), CX
    MOVQ CX, ret+16(FP)

    VPBROADCASTQ lastValue+8(FP), Y0
    XORQ SI, SI
loop:
    deltaInt64AVX2x4((AX)(SI*8))
    deltaInt64AVX2x4(32(AX)(SI*8))
    deltaInt64AVX2x4(64(AX)(SI*8))
    deltaInt64AVX2x4(96(AX)(SI*8))
    ADDQ $16, SI
    CMPQ SI, $blockSize
    JNE loop
    VZEROUPPER
    RET

// vpminsq is an emulation of the AVX-512 VPMINSQ instruction with AVX2.
#define vpminsq(ones, tmp, arg2, arg1, ret) \
    VPCMPGTQ arg1, arg2, tmp \
    VPBLENDVB tmp, arg1, arg2, ret

// func blockMinInt64AVX2(block *[blockSize]int64) int64
TEXT ·blockMinInt64AVX2(SB), NOSPLIT, $0-16
    MOVQ block+0(FP), AX
    XORQ SI, SI
    VPCMPEQQ Y9, Y9, Y9 // ones
    VPBROADCASTQ (AX), Y0
loop:
    VMOVDQU 0(AX)(SI*8), Y1
    VMOVDQU 32(AX)(SI*8), Y2
    VMOVDQU 64(AX)(SI*8), Y3
    VMOVDQU 96(AX)(SI*8), Y4
    VMOVDQU 128(AX)(SI*8), Y5
    VMOVDQU 160(AX)(SI*8), Y6
    VMOVDQU 192(AX)(SI*8), Y7
    VMOVDQU 224(AX)(SI*8), Y8

    vpminsq(Y9, Y10, Y0, Y1, Y1)
    vpminsq(Y9, Y11, Y0, Y2, Y2)
    vpminsq(Y9, Y12, Y0, Y3, Y3)
    vpminsq(Y9, Y13, Y0, Y4, Y4)
    vpminsq(Y9, Y14, Y0, Y5, Y5)
    vpminsq(Y9, Y15, Y0, Y6, Y6)
    vpminsq(Y9, Y10, Y0, Y7, Y7)
    vpminsq(Y9, Y11, Y0, Y8, Y8)

    vpminsq(Y9, Y12, Y2, Y1, Y1)
    vpminsq(Y9, Y13, Y4, Y3, Y3)
    vpminsq(Y9, Y14, Y6, Y5, Y5)
    vpminsq(Y9, Y15, Y8, Y7, Y7)

    vpminsq(Y9, Y10, Y3, Y1, Y1)
    vpminsq(Y9, Y11, Y7, Y5, Y5)
    vpminsq(Y9, Y12, Y5, Y1, Y0)

    ADDQ $32, SI
    CMPQ SI, $blockSize
    JNE loop

    VPERM2I128 $1, Y0, Y0, Y1
    vpminsq(Y9, Y10, Y1, Y0, Y0)

    MOVQ X0, CX
    VPEXTRQ $1, X0, BX
    CMPQ CX, BX
    CMOVQLT CX, BX
    MOVQ BX, ret+8(FP)
    VZEROUPPER
    RET

#define subInt64AVX2x32(baseAddr, offset) \
    VMOVDQU offset+0(baseAddr), Y1      \
    VMOVDQU offset+32(baseAddr), Y2     \
    VMOVDQU offset+64(baseAddr), Y3     \
    VMOVDQU offset+96(baseAddr), Y4     \
    VMOVDQU offset+128(baseAddr), Y5    \
    VMOVDQU offset+160(baseAddr), Y6    \
    VMOVDQU offset+192(baseAddr), Y7    \
    VMOVDQU offset+224(baseAddr), Y8    \
    VPSUBQ Y0, Y1, Y1                   \
    VPSUBQ Y0, Y2, Y2                   \
    VPSUBQ Y0, Y3, Y3                   \
    VPSUBQ Y0, Y4, Y4                   \
    VPSUBQ Y0, Y5, Y5                   \
    VPSUBQ Y0, Y6, Y6                   \
    VPSUBQ Y0, Y7, Y7                   \
    VPSUBQ Y0, Y8, Y8                   \
    VMOVDQU Y1, offset+0(baseAddr)      \
    VMOVDQU Y2, offset+32(baseAddr)     \
    VMOVDQU Y3, offset+64(baseAddr)     \
    VMOVDQU Y4, offset+96(baseAddr)     \
    VMOVDQU Y5, offset+128(baseAddr)    \
    VMOVDQU Y6, offset+160(baseAddr)    \
    VMOVDQU Y7, offset+192(baseAddr)    \
    VMOVDQU Y8, offset+224(baseAddr)

// func blockSubInt64AVX2(block *[blockSize]int64, value int64)
TEXT ·blockSubInt64AVX2(SB), NOSPLIT, $0-16
    MOVQ block+0(FP), AX
    VPBROADCASTQ value+8(FP), Y0
    subInt64AVX2x32(AX, 0)
    subInt64AVX2x32(AX, 256)
    subInt64AVX2x32(AX, 512)
    subInt64AVX2x32(AX, 768)
    VZEROUPPER
    RET

// vpmaxsq is an emulation of the AVX-512 VPMAXSQ instruction with AVX2.
#define vpmaxsq(tmp, arg2, arg1, ret) \
    VPCMPGTQ arg2, arg1, tmp \
    VPBLENDVB tmp, arg1, arg2, ret

// func blockBitWidthsInt64AVX2(bitWidths *[numMiniBlocks]byte, block *[blockSize]int64)
TEXT ·blockBitWidthsInt64AVX2(SB), NOSPLIT, $0-16
    MOVQ bitWidths+0(FP), AX
    MOVQ block+8(FP), BX

    // AVX2 only has signed comparisons (and min/max), we emulate working on
    // unsigned values by adding -2^64 to the values. Y9 is a vector of -2^64
    // used to offset 4 packed 64 bits integers in other YMM registers where
    // the block data are loaded.
    VPCMPEQQ Y9, Y9, Y9
    VPSLLQ $63, Y9, Y9

    XORQ DI, DI
loop:
    VPBROADCASTQ (BX), Y0 // max
    VPADDQ Y9, Y0, Y0

    VMOVDQU (BX), Y1
    VMOVDQU 32(BX), Y2
    VMOVDQU 64(BX), Y3
    VMOVDQU 96(BX), Y4
    VMOVDQU 128(BX), Y5
    VMOVDQU 160(BX), Y6
    VMOVDQU 192(BX), Y7
    VMOVDQU 224(BX), Y8

    VPADDQ Y9, Y1, Y1
    VPADDQ Y9, Y2, Y2
    VPADDQ Y9, Y3, Y3
    VPADDQ Y9, Y4, Y4
    VPADDQ Y9, Y5, Y5
    VPADDQ Y9, Y6, Y6
    VPADDQ Y9, Y7, Y7
    VPADDQ Y9, Y8, Y8

    vpmaxsq(Y10, Y2, Y1, Y1)
    vpmaxsq(Y11, Y4, Y3, Y3)
    vpmaxsq(Y12, Y6, Y5, Y5)
    vpmaxsq(Y13, Y8, Y7, Y7)

    vpmaxsq(Y10, Y3, Y1, Y1)
    vpmaxsq(Y11, Y7, Y5, Y5)
    vpmaxsq(Y12, Y5, Y1, Y1)
    vpmaxsq(Y13, Y1, Y0, Y0)

    VPERM2I128 $1, Y0, Y0, Y1
    vpmaxsq(Y10, Y1, Y0, Y0)
    VPSUBQ Y9, Y0, Y0

    MOVQ X0, CX
    VPEXTRQ $1, X0, DX
    CMPQ CX, DX
    CMOVQHI CX, DX

    LZCNTQ DX, DX
    NEGQ DX
    ADDQ $64, DX
    MOVB DX, (AX)(DI*1)

    ADDQ $256, BX
    INCQ DI
    CMPQ DI, $numMiniBlocks
    JNE loop
    VZEROUPPER
    RET

// encodeMiniBlockInt64Default is the generic implementation of the algorithm to
// pack 64 bit integers into values of a given bit width (<=64).
//
// This algorithm is much slower than the vectorized versions, but is useful
// as a reference implementation to run the tests against, and as fallback when
// the code runs on a CPU which does not support the AVX2 instruction set.
//
// func encodeMiniBlockInt64Default(dst *byte, src *[miniBlockSize]int64, bitWidth uint)
TEXT ·encodeMiniBlockInt64Default(SB), NOSPLIT, $0-24
    MOVQ dst+0(FP), AX
    MOVQ src+8(FP), BX
    MOVQ bitWidth+16(FP), R10

    XORQ R11, R11 // zero
    XORQ DI, DI // bitOffset
    XORQ SI, SI
loop:
    MOVQ DI, CX
    MOVQ DI, DX

    ANDQ $0b111111, CX // bitOffset % 64
    SHRQ $6, DX        // bitOffset / 64

    MOVQ (BX)(SI*8), R8
    MOVQ R8, R9
    SHLQ CX, R8
    NEGQ CX
    ADDQ $64, CX
    SHRQ CX, R9
    CMPQ CX, $64
    CMOVQEQ R11, R9 // needed because shifting by more than 63 is undefined

    ORQ R8, 0(AX)(DX*8)
    ORQ R9, 8(AX)(DX*8)

    ADDQ R10, DI
    INCQ SI
    CMPQ SI, $miniBlockSize
    JNE loop
    RET

// func encodeMiniBlockInt64x1bitAVX2(dst *byte, src *[miniBlockSize]int64)
TEXT ·encodeMiniBlockInt64x1bitAVX2(SB), NOSPLIT, $0-16
    MOVQ dst+0(FP), AX
    MOVQ src+8(FP), BX

    VMOVDQU 0(BX), Y0
    VMOVDQU 32(BX), Y1
    VMOVDQU 64(BX), Y2
    VMOVDQU 96(BX), Y3
    VMOVDQU 128(BX), Y4
    VMOVDQU 160(BX), Y5
    VMOVDQU 192(BX), Y6
    VMOVDQU 224(BX), Y7

    VPSLLQ $63, Y0, Y0
    VPSLLQ $63, Y1, Y1
    VPSLLQ $63, Y2, Y2
    VPSLLQ $63, Y3, Y3
    VPSLLQ $63, Y4, Y4
    VPSLLQ $63, Y5, Y5
    VPSLLQ $63, Y6, Y6
    VPSLLQ $63, Y7, Y7

    VMOVMSKPD Y0, R8
    VMOVMSKPD Y1, R9
    VMOVMSKPD Y2, R10
    VMOVMSKPD Y3, R11
    VMOVMSKPD Y4, R12
    VMOVMSKPD Y5, R13
    VMOVMSKPD Y6, R14
    VMOVMSKPD Y7, R15

    SHLL $4, R9
    SHLL $8, R10
    SHLL $12, R11
    SHLL $16, R12
    SHLL $20, R13
    SHLL $24, R14
    SHLL $28, R15

    ORL R9, R8
    ORL R11, R10
    ORL R13, R12
    ORL R15, R14
    ORL R10, R8
    ORL R14, R12
    ORL R12, R8

    MOVL R8, (AX)
    VZEROUPPER
    RET

// func encodeMiniBlockInt64x2bitsAVX2(dst *byte, src *[miniBlockSize]int64)
TEXT ·encodeMiniBlockInt64x2bitsAVX2(SB), NOSPLIT, $0-16
    MOVQ dst+0(FP), AX
    MOVQ src+8(FP), BX

    VMOVDQU 0(BX), Y8
    VMOVDQU 32(BX), Y9
    VMOVDQU 64(BX), Y10
    VMOVDQU 96(BX), Y11
    VMOVDQU 128(BX), Y12
    VMOVDQU 160(BX), Y13
    VMOVDQU 192(BX), Y14
    VMOVDQU 224(BX), Y15

    VPSLLQ $63, Y8, Y0
    VPSLLQ $63, Y9, Y1
    VPSLLQ $63, Y10, Y2
    VPSLLQ $63, Y11, Y3
    VPSLLQ $63, Y12, Y4
    VPSLLQ $63, Y13, Y5
    VPSLLQ $63, Y14, Y6
    VPSLLQ $63, Y15, Y7

    VMOVMSKPD Y0, R8
    VMOVMSKPD Y1, R9
    VMOVMSKPD Y2, R10
    VMOVMSKPD Y3, R11
    VMOVMSKPD Y4, R12
    VMOVMSKPD Y5, R13
    VMOVMSKPD Y6, R14
    VMOVMSKPD Y7, R15

    SHLQ $4, R9
    SHLQ $8, R10
    SHLQ $12, R11
    SHLQ $16, R12
    SHLQ $20, R13
    SHLQ $24, R14
    SHLQ $28, R15

    ORQ R9, R8
    ORQ R11, R10
    ORQ R13, R12
    ORQ R15, R14
    ORQ R10, R8
    ORQ R14, R12
    ORQ R12, R8

    MOVQ $0x5555555555555555, CX // 0b010101...
    PDEPQ CX, R8, CX

    VPSLLQ $62, Y8, Y8
    VPSLLQ $62, Y9, Y9
    VPSLLQ $62, Y10, Y10
    VPSLLQ $62, Y11, Y11
    VPSLLQ $62, Y12, Y12
    VPSLLQ $62, Y13, Y13
    VPSLLQ $62, Y14, Y14
    VPSLLQ $62, Y15, Y15

    VMOVMSKPD Y8, R8
    VMOVMSKPD Y9, R9
    VMOVMSKPD Y10, R10
    VMOVMSKPD Y11, R11
    VMOVMSKPD Y12, R12
    VMOVMSKPD Y13, R13
    VMOVMSKPD Y14, R14
    VMOVMSKPD Y15, R15

    SHLQ $4, R9
    SHLQ $8, R10
    SHLQ $12, R11
    SHLQ $16, R12
    SHLQ $20, R13
    SHLQ $24, R14
    SHLQ $28, R15

    ORQ R9, R8
    ORQ R11, R10
    ORQ R13, R12
    ORQ R15, R14
    ORQ R10, R8
    ORQ R14, R12
    ORQ R12, R8

    MOVQ $0xAAAAAAAAAAAAAAAA, DX // 0b101010...
    PDEPQ DX, R8, DX
    ORQ DX, CX
    MOVQ CX, (AX)
    VZEROUPPER
    RET

// func encodeMiniBlockInt64x64bitsAVX2(dst *byte, src *[miniBlockSize]int64)
TEXT ·encodeMiniBlockInt64x64bitsAVX2(SB), NOSPLIT, $0-16
    MOVQ dst+0(FP), AX
    MOVQ src+8(FP), BX
    VMOVDQU 0(BX), Y0
    VMOVDQU 32(BX), Y1
    VMOVDQU 64(BX), Y2
    VMOVDQU 96(BX), Y3
    VMOVDQU 128(BX), Y4
    VMOVDQU 160(BX), Y5
    VMOVDQU 192(BX), Y6
    VMOVDQU 224(BX), Y7
    VMOVDQU Y0, 0(AX)
    VMOVDQU Y1, 32(AX)
    VMOVDQU Y2, 64(AX)
    VMOVDQU Y3, 96(AX)
    VMOVDQU Y4, 128(AX)
    VMOVDQU Y5, 160(AX)
    VMOVDQU Y6, 192(AX)
    VMOVDQU Y7, 224(AX)
    VZEROUPPER
    RET

// func decodeBlockInt64Default(dst []int64, minDelta, lastValue int64) int64
TEXT ·decodeBlockInt64Default(SB), NOSPLIT, $0-48
    MOVQ dst_base+0(FP), AX
    MOVQ dst_len+8(FP), BX
    MOVQ minDelta+24(FP), CX
    MOVQ lastValue+32(FP), DX
    XORQ SI, SI
    JMP test
loop:
    MOVQ (AX)(SI*8), DI
    ADDQ CX, DI
    ADDQ DI, DX
    MOVQ DX, (AX)(SI*8)
    INCQ SI
test:
    CMPQ SI, BX
    JNE loop
done:
    MOVQ DX, ret+40(FP)
    RET
