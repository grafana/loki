//go:build !purego

#include "textflag.h"

// func maxInt32(data []int32) int32
TEXT ·maxInt32(SB), NOSPLIT, $-28
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ BX, BX

    CMPQ CX, $0
    JE done
    XORQ SI, SI
    MOVLQZX (AX), BX

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $32
    JB loop

    MOVQ CX, DI
    SHRQ $5, DI
    SHLQ $5, DI
    VPBROADCASTD (AX), Z0
loop32:
    VMOVDQU32 (AX)(SI*4), Z1
    VMOVDQU32 64(AX)(SI*4), Z2
    VPMAXSD Z1, Z0, Z0
    VPMAXSD Z2, Z0, Z0
    ADDQ $32, SI
    CMPQ SI, DI
    JNE loop32

    VMOVDQU32 swap32+0(SB), Z1
    VPERMI2D Z0, Z0, Z1
    VPMAXSD Y1, Y0, Y0

    VMOVDQU32 swap32+32(SB), Y1
    VPERMI2D Y0, Y0, Y1
    VPMAXSD X1, X0, X0

    VMOVDQU32 swap32+48(SB), X1
    VPERMI2D X0, X0, X1
    VPMAXSD X1, X0, X0
    VZEROUPPER

    MOVQ X0, DX
    MOVL DX, BX
    SHRQ $32, DX
    CMPL DX, BX
    CMOVLGT DX, BX

    CMPQ SI, CX
    JE done
loop:
    MOVLQZX (AX)(SI*4), DX
    CMPL DX, BX
    CMOVLGT DX, BX
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVL BX, ret+24(FP)
    RET

// func maxInt64(data []int64) int64
TEXT ·maxInt64(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ BX, BX

    CMPQ CX, $0
    JE done
    XORQ SI, SI
    MOVQ (AX), BX

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $32
    JB loop

    MOVQ CX, DI
    SHRQ $5, DI
    SHLQ $5, DI
    VPBROADCASTQ (AX), Z0
loop32:
    VMOVDQU64 (AX)(SI*8), Z1
    VMOVDQU64 64(AX)(SI*8), Z2
    VMOVDQU64 128(AX)(SI*8), Z3
    VMOVDQU64 192(AX)(SI*8), Z4
    VPMAXSQ Z1, Z2, Z5
    VPMAXSQ Z3, Z4, Z6
    VPMAXSQ Z5, Z6, Z1
    VPMAXSQ Z1, Z0, Z0
    ADDQ $32, SI
    CMPQ SI, DI
    JNE loop32

    VMOVDQU32 swap32+0(SB), Z1
    VPERMI2D Z0, Z0, Z1
    VPMAXSQ Y1, Y0, Y0

    VMOVDQU32 swap32+32(SB), Y1
    VPERMI2D Y0, Y0, Y1
    VPMAXSQ X1, X0, X0

    VMOVDQU32 swap32+48(SB), X1
    VPERMI2D X0, X0, X1
    VPMAXSQ X1, X0, X0
    VZEROUPPER

    MOVQ X0, BX
    CMPQ SI, CX
    JE done
loop:
    MOVQ (AX)(SI*8), DX
    CMPQ DX, BX
    CMOVQGT DX, BX
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVQ BX, ret+24(FP)
    RET

// func maxUint32(data []int32) int32
TEXT ·maxUint32(SB), NOSPLIT, $-28
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ BX, BX

    CMPQ CX, $0
    JE done
    XORQ SI, SI
    MOVLQZX (AX), BX

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $32
    JB loop

    MOVQ CX, DI
    SHRQ $5, DI
    SHLQ $5, DI
    VPBROADCASTD (AX), Z0
loop32:
    VMOVDQU32 (AX)(SI*4), Z1
    VMOVDQU32 64(AX)(SI*4), Z2
    VPMAXUD Z1, Z0, Z0
    VPMAXUD Z2, Z0, Z0
    ADDQ $32, SI
    CMPQ SI, DI
    JNE loop32

    VMOVDQU32 swap32+0(SB), Z1
    VPERMI2D Z0, Z0, Z1
    VPMAXUD Y1, Y0, Y0

    VMOVDQU32 swap32+32(SB), Y1
    VPERMI2D Y0, Y0, Y1
    VPMAXUD X1, X0, X0

    VMOVDQU32 swap32+48(SB), X1
    VPERMI2D X0, X0, X1
    VPMAXUD X1, X0, X0
    VZEROUPPER

    MOVQ X0, DX
    MOVL DX, BX
    SHRQ $32, DX
    CMPL DX, BX
    CMOVLHI DX, BX

    CMPQ SI, CX
    JE done
loop:
    MOVLQZX (AX)(SI*4), DX
    CMPL DX, BX
    CMOVLHI DX, BX
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVL BX, ret+24(FP)
    RET

// func maxUint64(data []uint64) uint64
TEXT ·maxUint64(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ BX, BX

    CMPQ CX, $0
    JE done
    XORQ SI, SI
    MOVQ (AX), BX

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $32
    JB loop

    MOVQ CX, DI
    SHRQ $5, DI
    SHLQ $5, DI
    VPBROADCASTQ (AX), Z0
loop32:
    VMOVDQU64 (AX)(SI*8), Z1
    VMOVDQU64 64(AX)(SI*8), Z2
    VMOVDQU64 128(AX)(SI*8), Z3
    VMOVDQU64 192(AX)(SI*8), Z4
    VPMAXUQ Z1, Z2, Z5
    VPMAXUQ Z3, Z4, Z6
    VPMAXUQ Z5, Z6, Z1
    VPMAXUQ Z1, Z0, Z0
    ADDQ $32, SI
    CMPQ SI, DI
    JNE loop32

    VMOVDQU32 swap32+0(SB), Z1
    VPERMI2D Z0, Z0, Z1
    VPMAXUQ Y1, Y0, Y0

    VMOVDQU32 swap32+32(SB), Y1
    VPERMI2D Y0, Y0, Y1
    VPMAXUQ X1, X0, X0

    VMOVDQU32 swap32+48(SB), X1
    VPERMI2D X0, X0, X1
    VPMAXUQ X1, X0, X0
    VZEROUPPER

    MOVQ X0, BX
    CMPQ SI, CX
    JE done
loop:
    MOVQ (AX)(SI*8), DX
    CMPQ DX, BX
    CMOVQHI DX, BX
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVQ BX, ret+24(FP)
    RET

// func maxFloat32(data []float32) float32
TEXT ·maxFloat32(SB), NOSPLIT, $-28
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ BX, BX

    CMPQ CX, $0
    JE done
    XORPS X0, X0
    XORPS X1, X1
    XORQ SI, SI
    MOVLQZX (AX), BX
    MOVQ BX, X0

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $64
    JB loop

    MOVQ CX, DI
    SHRQ $6, DI
    SHLQ $6, DI
    VPBROADCASTD (AX), Z0
loop64:
    VMOVDQU32 (AX)(SI*4), Z1
    VMOVDQU32 64(AX)(SI*4), Z2
    VMOVDQU32 128(AX)(SI*4), Z3
    VMOVDQU32 192(AX)(SI*4), Z4
    VMAXPS Z1, Z2, Z5
    VMAXPS Z3, Z4, Z6
    VMAXPS Z5, Z6, Z1
    VMAXPS Z1, Z0, Z0
    ADDQ $64, SI
    CMPQ SI, DI
    JNE loop64

    VMOVDQU32 swap32+0(SB), Z1
    VPERMI2D Z0, Z0, Z1
    VMAXPS Y1, Y0, Y0

    VMOVDQU32 swap32+32(SB), Y1
    VPERMI2D Y0, Y0, Y1
    VMAXPS X1, X0, X0

    VMOVDQU32 swap32+48(SB), X1
    VPERMI2D X0, X0, X1
    VMAXPS X1, X0, X0
    VZEROUPPER

    MOVAPS X0, X1
    PSRLQ $32, X1
    MOVQ X0, BX
    MOVQ X1, DX
    UCOMISS X0, X1
    CMOVLHI DX, BX

    CMPQ SI, CX
    JE done
    MOVQ BX, X0
loop:
    MOVLQZX (AX)(SI*4), DX
    MOVQ DX, X1
    UCOMISS X0, X1
    CMOVLHI DX, BX
    MOVQ BX, X0
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVL BX, ret+24(FP)
    RET

// func maxFloat64(data []float64) float64
TEXT ·maxFloat64(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    XORQ BX, BX

    CMPQ CX, $0
    JE done
    XORPD X0, X0
    XORPD X1, X1
    XORQ SI, SI
    MOVQ (AX), BX
    MOVQ BX, X0

    CMPB ·hasAVX512VL(SB), $0
    JE loop

    CMPQ CX, $32
    JB loop

    MOVQ CX, DI
    SHRQ $5, DI
    SHLQ $5, DI
    VPBROADCASTQ (AX), Z0
loop32:
    VMOVDQU64 (AX)(SI*8), Z1
    VMOVDQU64 64(AX)(SI*8), Z2
    VMOVDQU64 128(AX)(SI*8), Z3
    VMOVDQU64 192(AX)(SI*8), Z4
    VMAXPD Z1, Z2, Z5
    VMAXPD Z3, Z4, Z6
    VMAXPD Z5, Z6, Z1
    VMAXPD Z1, Z0, Z0
    ADDQ $32, SI
    CMPQ SI, DI
    JNE loop32

    VMOVDQU64 swap32+0(SB), Z1
    VPERMI2D Z0, Z0, Z1
    VMAXPD Y1, Y0, Y0

    VMOVDQU64 swap32+32(SB), Y1
    VPERMI2D Y0, Y0, Y1
    VMAXPD X1, X0, X0

    VMOVDQU64 swap32+48(SB), X1
    VPERMI2D X0, X0, X1
    VMAXPD X1, X0, X0
    VZEROUPPER

    MOVQ X0, BX
    CMPQ SI, CX
    JE done
loop:
    MOVQ (AX)(SI*8), DX
    MOVQ DX, X1
    UCOMISD X0, X1
    CMOVQHI DX, BX
    MOVQ BX, X0
    INCQ SI
    CMPQ SI, CX
    JNE loop
done:
    MOVQ BX, ret+24(FP)
    RET

// vpmaxu128 is a macro comparing unsigned 128 bits values held in the
// `srcValues` and `maxValues` vectors. The `srcIndexes` and `maxIndexes`
// vectors contain the indexes of elements in the value vectors. Remaining
// K and R arguments are mask and general purpose registers needed to hold
// temporary values during the computation. The last M argument is a mask
// generated by vpmaxu128mask.
//
// The routine uses AVX-512 instructions (VPCMPUQ, VPBLENDMQ) to implement
// the comparison of 128 bits values. The values are expected to be stored
// in the vectors as a little-endian pair of two consecutive quad words.
//
// The results are written to the `maxValues` and `maxIndexes` vectors,
// overwriting the inputs. `srcValues` and `srcIndexes` are read-only
// parameters.
//
// At a high level, for two pairs of quad words formaxg two 128 bits values
// A and B, the test implemented by this macro is:
//
//   A[1] > B[1] || (A[1] == B[1] && A[0] > B[0])
//
// Values in the source vector that evaluate to true on this expression are
// written to the vector of maximum values, and their indexes are written to
// the vector of indexes.
#define vpmaxu128(srcValues, srcIndexes, maxValues, maxIndexes, K1, K2, R1, R2, R3, M) \
    VPCMPUQ $0, maxValues, srcValues, K1 \
    VPCMPUQ $6, maxValues, srcValues, K2 \
    KMOVB K1, R1 \
    KMOVB K2, R2 \
    MOVB R2, R3 \
    SHLB $1, R3 \
    ANDB R3, R1 \
    ORB R2, R1 \
    ANDB M, R1 \
    MOVB R1, R2 \
    SHRB $1, R2 \
    ORB R2, R1 \
    KMOVB R1, K1 \
    VPBLENDMQ srcValues, maxValues, K1, maxValues \
    VPBLENDMQ srcIndexes, maxIndexes, K1, maxIndexes

// vpmaxu128mask is a macro used to initialize the mask passed as last argument
// to vpmaxu128. The argument M is intended to be a general purpose register.
//
// The bit mask is used to merge the results of the "greater than" and "equal"
// comparison that are performed on each lane of maximum vectors. The upper bits
// are used to compute results of the operation to determine which of the pairs
// of quad words representing the 128 bits elements are the maximums.
#define vpmaxu128mask(M) MOVB $0b10101010, M

// func maxBE128(data [][16]byte) []byte
TEXT ·maxBE128(SB), NOSPLIT, $-48
    MOVQ data_base+0(FP), AX
    MOVQ data_len+8(FP), CX
    CMPQ CX, $0
    JE null

    SHLQ $4, CX
    MOVQ CX, DX // len
    MOVQ AX, BX // max
    ADDQ AX, CX // end

    CMPQ DX, $256
    JB loop

    CMPB ·hasAVX512MinMaxBE128(SB), $0
    JE loop

    // Z19 holds a vector of the count by which we increment the vectors of
    // swap at each loop iteration.
    MOVQ $16, DI
    VPBROADCASTQ DI, Z19

    // Z31 holds the shuffle mask used to convert 128 bits elements from big to
    // little endian so we can apply vectorized comparison instructions.
    VMOVDQU64 bswap128(SB), Z31

    // These vectors hold four lanes of maximum values found in the input.
    VBROADCASTI64X2 (AX), Z0
    VPSHUFB Z31, Z0, Z0
    VMOVDQU64 Z0, Z5
    VMOVDQU64 Z0, Z10
    VMOVDQU64 Z0, Z15

    // These vectors hold four lanes of swap of maximum values.
    //
    // We initialize them at zero because we broadcast the first value of the
    // input in the vectors that track the maximums of each lane; in other
    // words, we assume the maximum value is at the first offset and work our
    // way up from there.
    VPXORQ Z2, Z2, Z2
    VPXORQ Z7, Z7, Z7
    VPXORQ Z12, Z12, Z12
    VPXORQ Z17, Z17, Z17

    // These vectors are used to compute the swap of maximum values held
    // in [Z1, Z5, Z10, Z15]. Each vector holds a contiguous sequence of
    // swap; for example, Z3 is initialized with [0, 1, 2, 3]. At each
    // loop iteration, the swap are incremented by the number of elements
    // consumed from the input (4x4=16).
    VMOVDQU64 indexes128(SB), Z3
    VPXORQ Z8, Z8, Z8
    VPXORQ Z13, Z13, Z13
    VPXORQ Z18, Z18, Z18
    MOVQ $4, DI
    VPBROADCASTQ DI, Z1
    VPADDQ Z1, Z3, Z8
    VPADDQ Z1, Z8, Z13
    VPADDQ Z1, Z13, Z18

    // This bit mask is used to merge the results of the "less than" and "equal"
    // comparison that we perform on each lane of maximum vectors. We use the
    // upper bits to compute four results of the operation which determines
    // which of the pair of quad words representing the 128 bits elements is the
    // maximum.
    vpmaxu128mask(DI)
    SHRQ $8, DX
    SHLQ $8, DX
    ADDQ AX, DX
loop16:
    // Compute 4x4 maximum values in vector registers, along with their swap
    // in the input array.
    VMOVDQU64 (AX), Z1
    VMOVDQU64 64(AX), Z6
    VMOVDQU64 128(AX), Z11
    VMOVDQU64 192(AX), Z16
    VPSHUFB Z31, Z1, Z1
    VPSHUFB Z31, Z6, Z6
    VPSHUFB Z31, Z11, Z11
    VPSHUFB Z31, Z16, Z16
    vpmaxu128(Z1, Z3, Z0, Z2, K1, K2, R8, R9, R10, DI)
    vpmaxu128(Z6, Z8, Z5, Z7, K3, K4, R11, R12, R13, DI)
    vpmaxu128(Z11, Z13, Z10, Z12, K1, K2, R8, R9, R10, DI)
    vpmaxu128(Z16, Z18, Z15, Z17, K3, K4, R11, R12, R13, DI)
    VPADDQ Z19, Z3, Z3
    VPADDQ Z19, Z8, Z8
    VPADDQ Z19, Z13, Z13
    VPADDQ Z19, Z18, Z18
    ADDQ $256, AX
    CMPQ AX, DX
    JB loop16

    // After the loop completed, we need to merge the lanes that each contain
    // 4 maximum values (so 16 total candidate at this stage). The results are
    // reduced into 4 candidates in Z0, with their swap in Z2.
    vpmaxu128(Z10, Z12, Z0, Z2, K1, K2, R8, R9, R10, DI)
    vpmaxu128(Z15, Z17, Z5, Z7, K3, K4, R11, R12, R13, DI)
    vpmaxu128(Z5, Z7, Z0, Z2, K1, K2, R8, R9, R10, DI)

    // Further reduce the results by swapping the upper and lower parts of the
    // vector registers, and comparing them to determaxe which values are the
    // smallest. We compare 2x2 values at this step, then 2x1 values at the next
    // to find the index of the maximum.
    VMOVDQU64 swap64+0(SB), Z1
    VMOVDQU64 swap64+0(SB), Z3
    VPERMI2Q Z0, Z0, Z1
    VPERMI2Q Z2, Z2, Z3
    vpmaxu128(Y1, Y3, Y0, Y2, K1, K2, R8, R9, R10, DI)

    VMOVDQU64 swap64+32(SB), Y1
    VMOVDQU64 swap64+32(SB), Y3
    VPERMI2Q Y0, Y0, Y1
    VPERMI2Q Y2, Y2, Y3
    vpmaxu128(X1, X3, X0, X2, K1, K2, R8, R9, R10, DI)
    VZEROUPPER

    // Extract the index of the maximum value computed in the lower 64 bits of
    // X2 and position the BX pointer at the index of the maximum value.
    MOVQ X2, DX
    SHLQ $4, DX
    ADDQ DX, BX
    CMPQ AX, CX
    JE done

    // Unless the input was aligned on 256 bytes, we need to perform a few more
    // iterations on the remaining elements.
    //
    // This loop is also taken if the CPU has no support for AVX-512.
loop:
    MOVQ (AX), R8
    MOVQ (BX), R9
    BSWAPQ R8
    BSWAPQ R9
    CMPQ R8, R9
    JA more
    JB next
    MOVQ 8(AX), R8
    MOVQ 8(BX), R9
    BSWAPQ R8
    BSWAPQ R9
    CMPQ R8, R9
    JBE next
more:
    MOVQ AX, BX
next:
    ADDQ $16, AX
    CMPQ AX, CX
    JB loop
done:
    MOVQ BX, ret_base+24(FP)
    MOVQ $16, ret_len+32(FP)
    MOVQ $16, ret_cap+40(FP)
    RET
null:
    XORQ BX, BX
    MOVQ BX, ret_base+24(FP)
    MOVQ BX, ret_len+32(FP)
    MOVQ BX, ret_cap+40(FP)
    RET

