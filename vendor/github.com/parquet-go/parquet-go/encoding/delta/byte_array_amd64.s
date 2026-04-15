//go:build !purego

#include "funcdata.h"
#include "textflag.h"

// func validatePrefixAndSuffixLengthValuesAVX2(prefix, suffix []int32, maxLength int) (totalPrefixLength, totalSuffixLength int, ok bool)
TEXT ·validatePrefixAndSuffixLengthValuesAVX2(SB), NOSPLIT, $0-73
    MOVQ prefix_base+0(FP), AX
    MOVQ suffix_base+24(FP), BX
    MOVQ suffix_len+32(FP), CX
    MOVQ maxLength+48(FP), DX

    XORQ SI, SI
    XORQ DI, DI // lastValueLength
    XORQ R8, R8
    XORQ R9, R9
    XORQ R10, R10 // totalPrefixLength
    XORQ R11, R11 // totalSuffixLength
    XORQ R12, R12 // ok

    CMPQ CX, $8
    JB test

    MOVQ CX, R13
    SHRQ $3, R13
    SHLQ $3, R13

    VPXOR X0, X0, X0 // lastValueLengths
    VPXOR X1, X1, X1 // totalPrefixLengths
    VPXOR X2, X2, X2 // totalSuffixLengths
    VPXOR X3, X3, X3 // negative prefix length sentinels
    VPXOR X4, X4, X4 // negative suffix length sentinels
    VPXOR X5, X5, X5 // prefix length overflow sentinels
    VMOVDQU ·rotateLeft32(SB), Y6

loopAVX2:
    VMOVDQU (AX)(SI*4), Y7 // p
    VMOVDQU (BX)(SI*4), Y8 // n

    VPADDD Y7, Y1, Y1
    VPADDD Y8, Y2, Y2

    VPOR Y7, Y3, Y3
    VPOR Y8, Y4, Y4

    VPADDD Y7, Y8, Y9 // p + n
    VPERMD Y0, Y6, Y10
    VPBLENDD $1, Y10, Y9, Y10
    VPCMPGTD Y10, Y7, Y10
    VPOR Y10, Y5, Y5

    VMOVDQU Y9, Y0
    ADDQ $8, SI
    CMPQ SI, R13
    JNE loopAVX2

    // If any of the sentinel values has its most significant bit set then one
    // of the values was negative or one of the prefixes was greater than the
    // length of the previous value, return false.
    VPOR Y4, Y3, Y3
    VPOR Y5, Y3, Y3
    VMOVMSKPS Y3, R13
    CMPQ R13, $0
    JNE done

    // We computed 8 sums in parallel for the prefix and suffix arrays, they
    // need to be accumulated into single values, which is what these reduction
    // steps do.
    VPSRLDQ $4, Y1, Y5
    VPSRLDQ $8, Y1, Y6
    VPSRLDQ $12, Y1, Y7
    VPADDD Y5, Y1, Y1
    VPADDD Y6, Y1, Y1
    VPADDD Y7, Y1, Y1
    VPERM2I128 $1, Y1, Y1, Y0
    VPADDD Y0, Y1, Y1
    MOVQ X1, R10
    ANDQ $0x7FFFFFFF, R10

    VPSRLDQ $4, Y2, Y5
    VPSRLDQ $8, Y2, Y6
    VPSRLDQ $12, Y2, Y7
    VPADDD Y5, Y2, Y2
    VPADDD Y6, Y2, Y2
    VPADDD Y7, Y2, Y2
    VPERM2I128 $1, Y2, Y2, Y0
    VPADDD Y0, Y2, Y2
    MOVQ X2, R11
    ANDQ $0x7FFFFFFF, R11

    JMP test
loop:
    MOVLQSX (AX)(SI*4), R8
    MOVLQSX (BX)(SI*4), R9

    CMPQ R8, $0 // p < 0 ?
    JL done

    CMPQ R9, $0 // n < 0 ?
    JL done

    CMPQ R8, DI // p > lastValueLength ?
    JG done

    ADDQ R8, R10
    ADDQ R9, R11
    ADDQ R8, DI
    ADDQ R9, DI

    INCQ SI
test:
    CMPQ SI, CX
    JNE loop

    CMPQ R11, DX // totalSuffixLength > maxLength ?
    JG done

    MOVB $1, R12
done:
    MOVQ R10, totalPrefixLength+56(FP)
    MOVQ R11, totalSuffixLength+64(FP)
    MOVB R12, ok+72(FP)
    RET

// func decodeByteArrayOffsets(offsets []uint32, prefix, suffix []int32)
TEXT ·decodeByteArrayOffsets(SB), NOSPLIT, $0-72
    MOVQ offsets_base+0(FP), AX
    MOVQ prefix_base+24(FP), BX
    MOVQ suffix_base+48(FP), CX
    MOVQ suffix_len+56(FP), DX

    XORQ SI, SI
    XORQ R10, R10
    JMP test
loop:
    MOVL (BX)(SI*4), R8
    MOVL (CX)(SI*4), R9
    MOVL R10, (AX)(SI*4)
    ADDL R8, R10
    ADDL R9, R10
    INCQ SI
test:
    CMPQ SI, DX
    JNE loop
    MOVL R10, (AX)(SI*4)
    RET

// func decodeByteArrayAVX2(dst, src []byte, prefix, suffix []int32) int
TEXT ·decodeByteArrayAVX2(SB), NOSPLIT, $0-104
    MOVQ dst_base+0(FP), AX
    MOVQ src_base+24(FP), BX
    MOVQ prefix_base+48(FP), CX
    MOVQ suffix_base+72(FP), DX
    MOVQ suffix_len+80(FP), DI

    XORQ SI, SI
    XORQ R8, R8
    XORQ R9, R9
    MOVQ AX, R10 // last value

    JMP test
loop:
    MOVLQZX (CX)(SI*4), R8 // prefix length
    MOVLQZX (DX)(SI*4), R9 // suffix length
prefix:
    VMOVDQU (R10), Y0
    VMOVDQU Y0, (AX)
    CMPQ R8, $32
    JA copyPrefix
suffix:
    VMOVDQU (BX), Y1
    VMOVDQU Y1, (AX)(R8*1)
    CMPQ R9, $32
    JA copySuffix
next:
    MOVQ AX, R10
    ADDQ R9, R8
    LEAQ (AX)(R8*1), AX
    LEAQ (BX)(R9*1), BX
    INCQ SI
test:
    CMPQ SI, DI
    JNE loop
    MOVQ dst_base+0(FP), BX
    SUBQ BX, AX
    MOVQ AX, ret+96(FP)
    VZEROUPPER
    RET
copyPrefix:
    MOVQ $32, R12
copyPrefixLoop:
    VMOVDQU (R10)(R12*1), Y0
    VMOVDQU Y0, (AX)(R12*1)
    ADDQ $32, R12
    CMPQ R12, R8
    JB copyPrefixLoop
    JMP suffix
copySuffix:
    MOVQ $32, R12
    LEAQ (AX)(R8*1), R13
copySuffixLoop:
    VMOVDQU (BX)(R12*1), Y1
    VMOVDQU Y1, (R13)(R12*1)
    ADDQ $32, R12
    CMPQ R12, R9
    JB copySuffixLoop
    JMP next

// func decodeByteArrayAVX2x128bits(dst, src []byte, prefix, suffix []int32) int
TEXT ·decodeByteArrayAVX2x128bits(SB), NOSPLIT, $0-104
    MOVQ dst_base+0(FP), AX
    MOVQ src_base+24(FP), BX
    MOVQ prefix_base+48(FP), CX
    MOVQ suffix_base+72(FP), DX
    MOVQ suffix_len+80(FP), DI

    XORQ SI, SI
    XORQ R8, R8
    XORQ R9, R9
    VPXOR X0, X0, X0

    JMP test
loop:
    MOVLQZX (CX)(SI*4), R8 // prefix length
    MOVLQZX (DX)(SI*4), R9 // suffix length

    VMOVDQU (BX), X1
    VMOVDQU X0, (AX)
    VMOVDQU X1, (AX)(R8*1)
    VMOVDQU (AX), X0

    ADDQ R9, R8
    LEAQ (AX)(R8*1), AX
    LEAQ (BX)(R9*1), BX
    INCQ SI
test:
    CMPQ SI, DI
    JNE loop
    MOVQ dst_base+0(FP), BX
    SUBQ BX, AX
    MOVQ AX, ret+96(FP)
    VZEROUPPER
    RET
