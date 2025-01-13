//go:build !purego

#include "textflag.h"

// This version of the probing algorithm for 32 bit keys takes advantage of
// the memory layout of table groups and SIMD instructions to accelerate the
// probing operations.
//
// The first 32 bytes of a table group contain the bit mask indicating which
// slots are in use, and the array of keys, which fits into a single vector
// register (YMM) and can be loaded and tested with a single instruction.
//
// A first version of the table group used the number of keys held in the
// group instead of a bit mask, which required the probing operation to
// reconstruct the bit mask during the lookup operation in order to identify
// which elements of the VPCMPEQD result should be retained. The extra CPU
// instructions used to reconstruct the bit mask had a measurable overhead.
// By holding the bit mask in the data structure, we can determine the number
// of keys in a group using the POPCNT instruction, and avoid recomputing the
// mask during lookups.
//
// func multiProbe32AVX2(table []table32Group, numKeys int, hashes []uintptr, keys sparse.Uint32Array, values []int32) int
TEXT ·multiProbe32AVX2(SB), NOSPLIT, $0-112
    MOVQ table_base+0(FP), AX
    MOVQ table_len+8(FP), BX
    MOVQ numKeys+24(FP), CX
    MOVQ hashes_base+32(FP), DX
    MOVQ hashes_len+40(FP), DI
    MOVQ keys_array_ptr+56(FP), R8
    MOVQ keys_array_off+72(FP), R15
    MOVQ values_base+80(FP), R9
    DECQ BX // modulo = len(table) - 1

    XORQ SI, SI
    JMP test
loop:
    MOVQ (DX)(SI*8), R10  // hash
    VPBROADCASTD (R8), Y0 // [key]
probe:
    MOVQ R10, R11
    ANDQ BX, R11 // hash & modulo
    SHLQ $6, R11 // x 64 (size of table32Group)
    LEAQ (AX)(R11*1), R12

    VMOVDQU (R12), Y1
    VPCMPEQD Y0, Y1, Y2
    VMOVMSKPS Y2, R11
    MOVL 56(R12), R13
    TESTL R11, R13
    JZ insert

    TZCNTL R11, R13
    MOVL 28(R12)(R13*4), R14
next:
    MOVL R14, (R9)(SI*4)
    INCQ SI
    ADDQ R15, R8
test:
    CMPQ SI, DI
    JNE loop
    MOVQ CX, ret+104(FP)
    VZEROUPPER
    RET
insert:
    CMPL R13, $0b1111111
    JE probeNextGroup

    MOVL R13, R11
    POPCNTL R13, R13
    MOVQ X0, R14 // key
    SHLL $1, R11
    ORL $1, R11
    MOVL R11, 56(R12)       // group.len = (group.len << 1) | 1
    MOVL R14, (R12)(R13*4)  // group.keys[i] = key
    MOVL CX, 28(R12)(R13*4) // group.values[i] = value
    MOVL CX, R14
    INCL CX
    JMP next
probeNextGroup:
    INCQ R10
    JMP probe

// func multiProbe64AVX2(table []table64Group, numKeys int, hashes []uintptr, keys sparse.Uint64Array, values []int32) int
TEXT ·multiProbe64AVX2(SB), NOSPLIT, $0-112
    MOVQ table_base+0(FP), AX
    MOVQ table_len+8(FP), BX
    MOVQ numKeys+24(FP), CX
    MOVQ hashes_base+32(FP), DX
    MOVQ hashes_len+40(FP), DI
    MOVQ keys_array_ptr+56(FP), R8
    MOVQ keys_array_off+72(FP), R15
    MOVQ values_base+80(FP), R9
    DECQ BX // modulo = len(table) - 1

    XORQ SI, SI
    JMP test
loop:
    MOVQ (DX)(SI*8), R10        // hash
    VPBROADCASTQ (R8), Y0 // [key]
probe:
    MOVQ R10, R11
    ANDQ BX, R11 // hash & modulo
    SHLQ $6, R11 // x 64 (size of table64Group)
    LEAQ (AX)(R11*1), R12

    VMOVDQU (R12), Y1
    VPCMPEQQ Y0, Y1, Y2
    VMOVMSKPD Y2, R11
    MOVL 48(R12), R13
    TESTL R11, R13
    JZ insert

    TZCNTL R11, R13
    MOVL 32(R12)(R13*4), R14
next:
    MOVL R14, (R9)(SI*4)
    INCQ SI
    ADDQ R15, R8
test:
    CMPQ SI, DI
    JNE loop
    MOVQ CX, ret+104(FP)
    VZEROUPPER
    RET
insert:
    CMPL R13, $0b1111
    JE probeNextGroup

    MOVL R13, R11
    POPCNTL R13, R13
    SHLL $1, R11
    ORL $1, R11
    MOVL R11, 48(R12)       // group.len = (group.len << 1) | 1
    MOVQ X0, (R12)(R13*8)   // group.keys[i] = key
    MOVL CX, 32(R12)(R13*4) // group.values[i] = value
    MOVL CX, R14
    INCL CX
    JMP next
probeNextGroup:
    INCQ R10
    JMP probe

// func multiProbe128SSE2(table []byte, tableCap, tableLen int, hashes []uintptr, keys sparse.Uint128Array, values []int32) int
TEXT ·multiProbe128SSE2(SB), NOSPLIT, $0-120
    MOVQ table_base+0(FP), AX
    MOVQ tableCap+24(FP), BX
    MOVQ tableLen+32(FP), CX
    MOVQ hashes_base+40(FP), DX
    MOVQ hashes_len+48(FP), DI
    MOVQ keys_array_ptr+64(FP), R8
    MOVQ keys_array_off+80(FP), R15
    MOVQ values_base+88(FP), R9

    MOVQ BX, R10
    SHLQ $4, R10
    LEAQ (AX)(R10*1), R10
    DECQ BX // modulo = tableCap - 1

    XORQ SI, SI
    JMP test
loop:
    MOVQ (DX)(SI*8), R11 // hash
    MOVOU (R8), X0       // key
probe:
    MOVQ R11, R12
    ANDQ BX, R12

    MOVL (R10)(R12*4), R14
    CMPL R14, $0
    JE insert

    SHLQ $4, R12
    MOVOU (AX)(R12*1), X1
    PCMPEQL X0, X1
    MOVMSKPS X1, R13
    CMPL R13, $0b1111
    JE next

    INCQ R11
    JMP probe
next:
    DECL R14
    MOVL R14, (R9)(SI*4)
    INCQ SI
    ADDQ R15, R8
test:
    CMPQ SI, DI
    JNE loop
    MOVQ CX, ret+112(FP)
    RET
insert:
    INCL CX
    MOVL CX, (R10)(R12*4)
    MOVL CX, R14
    SHLQ $4, R12
    MOVOU X0, (AX)(R12*1)
    JMP next
