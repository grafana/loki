//go:build !purego

#include "textflag.h"

#define salt0 0x47b6137b
#define salt1 0x44974d91
#define salt2 0x8824ad5b
#define salt3 0xa2b7289d
#define salt4 0x705495c7
#define salt5 0x2df1424b
#define salt6 0x9efc4947
#define salt7 0x5c6bfb31

DATA ones+0(SB)/4, $1
DATA ones+4(SB)/4, $1
DATA ones+8(SB)/4, $1
DATA ones+12(SB)/4, $1
DATA ones+16(SB)/4, $1
DATA ones+20(SB)/4, $1
DATA ones+24(SB)/4, $1
DATA ones+28(SB)/4, $1
GLOBL ones(SB), RODATA|NOPTR, $32

DATA salt+0(SB)/4, $salt0
DATA salt+4(SB)/4, $salt1
DATA salt+8(SB)/4, $salt2
DATA salt+12(SB)/4, $salt3
DATA salt+16(SB)/4, $salt4
DATA salt+20(SB)/4, $salt5
DATA salt+24(SB)/4, $salt6
DATA salt+28(SB)/4, $salt7
GLOBL salt(SB), RODATA|NOPTR, $32

// This initial block is a SIMD implementation of the mask function declared in
// block_default.go and block_optimized.go. For each of the 8 x 32 bits words of
// the bloom filter block, the operation performed is:
//
//      block[i] = 1 << ((x * salt[i]) >> 27)
//
// Arguments
// ---------
//
// * src is a memory location where the value to use when computing the mask is
//   located. The memory location is not modified.
//
// * tmp is a YMM register used as scratch space to hold intermediary results in
//   the algorithm.
//
// * dst is a YMM register where the final mask is written.
//
#define generateMask(src, tmp, dst) \
    VMOVDQA ones(SB), dst \
    VPBROADCASTD src, tmp \
    VPMULLD salt(SB), tmp, tmp \
    VPSRLD $27, tmp, tmp \
    VPSLLVD tmp, dst, dst

#define insert(salt, src, dst) \
    MOVL src, CX \
    IMULL salt, CX \
    SHRL $27, CX \
    MOVL $1, DX \
    SHLL CX, DX \
    ORL DX, dst

#define check(salt, b, x) \
    MOVL b, CX \
    MOVL x, DX \
    IMULL salt, DX \
    SHRL $27, DX \
    BTL DX, CX \
    JAE notfound

// func blockInsert(b *Block, x uint32)
TEXT ·blockInsert(SB), NOSPLIT, $0-16
    MOVQ b+0(FP), AX
    CMPB ·hasAVX2(SB), $0
    JE fallback
avx2:
    generateMask(x+8(FP), Y1, Y0)
    // Set all 1 bits of the mask in the bloom filter block.
    VPOR (AX), Y0, Y0
    VMOVDQU Y0, (AX)
    VZEROUPPER
    RET
fallback:
    MOVL x+8(FP), BX
    insert($salt0, BX, 0(AX))
    insert($salt1, BX, 4(AX))
    insert($salt2, BX, 8(AX))
    insert($salt3, BX, 12(AX))
    insert($salt4, BX, 16(AX))
    insert($salt5, BX, 20(AX))
    insert($salt6, BX, 24(AX))
    insert($salt7, BX, 28(AX))
    RET

// func blockCheck(b *Block, x uint32) bool
TEXT ·blockCheck(SB), NOSPLIT, $0-17
    MOVQ b+0(FP), AX
    CMPB ·hasAVX2(SB), $0
    JE fallback
avx2:
    generateMask(x+8(FP), Y1, Y0)
    // Compare the 1 bits of the mask with the bloom filter block, then compare
    // the result with the mask, expecting equality if the value `x` was present
    // in the block.
    VPAND (AX), Y0, Y1 // Y0 = block & mask
    VPTEST Y0, Y1      // if (Y0 & ^Y1) != 0 { CF = 1 }
    SETCS ret+16(FP)   // return CF == 1
    VZEROUPPER
    RET
fallback:
    MOVL x+8(FP), BX
    check($salt0, 0(AX), BX)
    check($salt1, 4(AX), BX)
    check($salt2, 8(AX), BX)
    check($salt3, 12(AX), BX)
    check($salt4, 16(AX), BX)
    check($salt5, 20(AX), BX)
    check($salt6, 24(AX), BX)
    check($salt7, 28(AX), BX)
    MOVB $1, CX
    JMP done
notfound:
    XORB CX, CX
done:
    MOVB CX, ret+16(FP)
    RET
