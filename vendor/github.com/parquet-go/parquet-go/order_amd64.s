//go:build !purego

#include "textflag.h"

#define UNDEFINED 0
#define ASCENDING 1
#define DESCENDING -1

DATA shift1x32<>+0(SB)/4, $1
DATA shift1x32<>+4(SB)/4, $2
DATA shift1x32<>+8(SB)/4, $3
DATA shift1x32<>+12(SB)/4, $4
DATA shift1x32<>+16(SB)/4, $5
DATA shift1x32<>+20(SB)/4, $6
DATA shift1x32<>+24(SB)/4, $7
DATA shift1x32<>+28(SB)/4, $8
DATA shift1x32<>+32(SB)/4, $9
DATA shift1x32<>+36(SB)/4, $10
DATA shift1x32<>+40(SB)/4, $11
DATA shift1x32<>+44(SB)/4, $12
DATA shift1x32<>+48(SB)/4, $13
DATA shift1x32<>+52(SB)/4, $14
DATA shift1x32<>+56(SB)/4, $15
DATA shift1x32<>+60(SB)/4, $15
GLOBL shift1x32<>(SB), RODATA|NOPTR, $64

DATA shift1x64<>+0(SB)/4, $1
DATA shift1x64<>+8(SB)/4, $2
DATA shift1x64<>+16(SB)/4, $3
DATA shift1x64<>+24(SB)/4, $4
DATA shift1x64<>+32(SB)/4, $5
DATA shift1x64<>+40(SB)/4, $6
DATA shift1x64<>+48(SB)/4, $7
DATA shift1x64<>+56(SB)/4, $7
GLOBL shift1x64<>(SB), RODATA|NOPTR, $64

// func orderOfInt32(data []int32) int
TEXT ·orderOfInt32(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), R8
    MOVQ data_len+8(FP), R9
    XORQ SI, SI
    XORQ DI, DI

    CMPQ R9, $2
    JB undefined

    CMPB ·hasAVX512VL(SB), $0
    JE test

    CMPQ R9, $16
    JB test

    XORQ DX, DX
    MOVQ R9, AX
    SHRQ $4, AX
    SHLQ $4, AX
    MOVQ $15, CX
    IDIVQ CX
    IMULQ $15, AX
    DECQ R9

    VMOVDQU32 shift1x32<>(SB), Z2
    KXORW K2, K2, K2
testAscending15:
    VMOVDQU32 (R8)(SI*4), Z0
    VMOVDQU32 Z2, Z1
    VPERMI2D Z0, Z0, Z1
    VPCMPD $2, Z1, Z0, K1
    KORTESTW K2, K1
    JNC testDescending15
    ADDQ $15, SI
    CMPQ SI, AX
    JNE testAscending15
    VZEROUPPER
    JMP testAscending
testDescending15:
    VMOVDQU32 (R8)(DI*4), Z0
    VMOVDQU32 Z2, Z1
    VPERMI2D Z0, Z0, Z1
    VPCMPD $5, Z1, Z0, K1
    KORTESTW K2, K1
    JNC undefined15
    ADDQ $15, DI
    CMPQ DI, AX
    JNE testDescending15
    VZEROUPPER
    JMP testDescending

test:
    DECQ R9
testAscending:
    CMPQ SI, R9
    JAE ascending
    MOVL (R8)(SI*4), BX
    MOVL 4(R8)(SI*4), DX
    INCQ SI
    CMPL BX, DX
    JLE testAscending
    JMP testDescending
ascending:
    MOVQ $ASCENDING, ret+24(FP)
    RET
testDescending:
    CMPQ DI, R9
    JAE descending
    MOVL (R8)(DI*4), BX
    MOVL 4(R8)(DI*4), DX
    INCQ DI
    CMPL BX, DX
    JGE testDescending
    JMP undefined
descending:
    MOVQ $DESCENDING, ret+24(FP)
    RET
undefined15:
    VZEROUPPER
undefined:
    MOVQ $UNDEFINED, ret+24(FP)
    RET

// func orderOfInt64(data []int64) int
TEXT ·orderOfInt64(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), R8
    MOVQ data_len+8(FP), R9
    XORQ SI, SI
    XORQ DI, DI

    CMPQ R9, $2
    JB undefined

    CMPB ·hasAVX512VL(SB), $0
    JE test

    CMPQ R9, $8
    JB test

    XORQ DX, DX
    MOVQ R9, AX
    SHRQ $3, AX
    SHLQ $3, AX
    MOVQ $7, CX
    IDIVQ CX
    IMULQ $7, AX
    DECQ R9

    VMOVDQU64 shift1x64<>(SB), Z2
    KXORB K2, K2, K2
testAscending7:
    VMOVDQU64 (R8)(SI*8), Z0
    VMOVDQU64 Z2, Z1
    VPERMI2Q Z0, Z0, Z1
    VPCMPQ $2, Z1, Z0, K1
    KORTESTB K2, K1
    JNC testDescending7
    ADDQ $7, SI
    CMPQ SI, AX
    JNE testAscending7
    VZEROUPPER
    JMP testAscending
testDescending7:
    VMOVDQU64 (R8)(DI*8), Z0
    VMOVDQU64 Z2, Z1
    VPERMI2Q Z0, Z0, Z1
    VPCMPQ $5, Z1, Z0, K1
    KORTESTB K2, K1
    JNC undefined7
    ADDQ $7, DI
    CMPQ DI, AX
    JNE testDescending7
    VZEROUPPER
    JMP testDescending

test:
    DECQ R9
testAscending:
    CMPQ SI, R9
    JAE ascending
    MOVQ (R8)(SI*8), BX
    MOVQ 8(R8)(SI*8), DX
    INCQ SI
    CMPQ BX, DX
    JLE testAscending
    JMP testDescending
ascending:
    MOVQ $ASCENDING, ret+24(FP)
    RET
testDescending:
    CMPQ DI, R9
    JAE descending
    MOVQ (R8)(DI*8), BX
    MOVQ 8(R8)(DI*8), DX
    INCQ DI
    CMPQ BX, DX
    JGE testDescending
    JMP undefined
descending:
    MOVQ $DESCENDING, ret+24(FP)
    RET
undefined7:
    VZEROUPPER
undefined:
    MOVQ $UNDEFINED, ret+24(FP)
    RET

// func orderOfUint32(data []uint32) int
TEXT ·orderOfUint32(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), R8
    MOVQ data_len+8(FP), R9
    XORQ SI, SI
    XORQ DI, DI

    CMPQ R9, $2
    JB undefined

    CMPB ·hasAVX512VL(SB), $0
    JE test

    CMPQ R9, $16
    JB test

    XORQ DX, DX
    MOVQ R9, AX
    SHRQ $4, AX
    SHLQ $4, AX
    MOVQ $15, CX
    IDIVQ CX
    IMULQ $15, AX
    DECQ R9

    VMOVDQU32 shift1x32<>(SB), Z2
    KXORW K2, K2, K2
testAscending15:
    VMOVDQU32 (R8)(SI*4), Z0
    VMOVDQU32 Z2, Z1
    VPERMI2D Z0, Z0, Z1
    VPCMPUD $2, Z1, Z0, K1
    KORTESTW K2, K1
    JNC testDescending15
    ADDQ $15, SI
    CMPQ SI, AX
    JNE testAscending15
    VZEROUPPER
    JMP testAscending
testDescending15:
    VMOVDQU32 (R8)(DI*4), Z0
    VMOVDQU32 Z2, Z1
    VPERMI2D Z0, Z0, Z1
    VPCMPUD $5, Z1, Z0, K1
    KORTESTW K2, K1
    JNC undefined15
    ADDQ $15, DI
    CMPQ DI, AX
    JNE testDescending15
    VZEROUPPER
    JMP testDescending

test:
    DECQ R9
testAscending:
    CMPQ SI, R9
    JAE ascending
    MOVL (R8)(SI*4), BX
    MOVL 4(R8)(SI*4), DX
    INCQ SI
    CMPL BX, DX
    JBE testAscending
    JMP testDescending
ascending:
    MOVQ $ASCENDING, ret+24(FP)
    RET
testDescending:
    CMPQ DI, R9
    JAE descending
    MOVL (R8)(DI*4), BX
    MOVL 4(R8)(DI*4), DX
    INCQ DI
    CMPL BX, DX
    JAE testDescending
    JMP undefined
descending:
    MOVQ $DESCENDING, ret+24(FP)
    RET
undefined15:
    VZEROUPPER
undefined:
    MOVQ $UNDEFINED, ret+24(FP)
    RET

// func orderOfUint64(data []uint64) int
TEXT ·orderOfUint64(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), R8
    MOVQ data_len+8(FP), R9
    XORQ SI, SI
    XORQ DI, DI

    CMPQ R9, $2
    JB undefined

    CMPB ·hasAVX512VL(SB), $0
    JE test

    CMPQ R9, $8
    JB test

    XORQ DX, DX
    MOVQ R9, AX
    SHRQ $3, AX
    SHLQ $3, AX
    MOVQ $7, CX
    IDIVQ CX
    IMULQ $7, AX
    DECQ R9

    VMOVDQU64 shift1x64<>(SB), Z2
    KXORB K2, K2, K2
testAscending7:
    VMOVDQU64 (R8)(SI*8), Z0
    VMOVDQU64 Z2, Z1
    VPERMI2Q Z0, Z0, Z1
    VPCMPUQ $2, Z1, Z0, K1
    KORTESTB K2, K1
    JNC testDescending7
    ADDQ $7, SI
    CMPQ SI, AX
    JNE testAscending7
    VZEROUPPER
    JMP testAscending
testDescending7:
    VMOVDQU64 (R8)(DI*8), Z0
    VMOVDQU64 Z2, Z1
    VPERMI2Q Z0, Z0, Z1
    VPCMPUQ $5, Z1, Z0, K1
    KORTESTB K2, K1
    JNC undefined7
    ADDQ $7, DI
    CMPQ DI, AX
    JNE testDescending7
    VZEROUPPER
    JMP testDescending

test:
    DECQ R9
testAscending:
    CMPQ SI, R9
    JAE ascending
    MOVQ (R8)(SI*8), BX
    MOVQ 8(R8)(SI*8), DX
    INCQ SI
    CMPQ BX, DX
    JBE testAscending
    JMP testDescending
ascending:
    MOVQ $ASCENDING, ret+24(FP)
    RET
testDescending:
    CMPQ DI, R9
    JAE descending
    MOVQ (R8)(DI*8), BX
    MOVQ 8(R8)(DI*8), DX
    INCQ DI
    CMPQ BX, DX
    JAE testDescending
    JMP undefined
descending:
    MOVQ $DESCENDING, ret+24(FP)
    RET
undefined7:
    VZEROUPPER
undefined:
    MOVQ $UNDEFINED, ret+24(FP)
    RET

// func orderOfFloat32(data []float32) int
TEXT ·orderOfFloat32(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), R8
    MOVQ data_len+8(FP), R9
    XORQ SI, SI
    XORQ DI, DI

    CMPQ R9, $2
    JB undefined

    CMPB ·hasAVX512VL(SB), $0
    JE test

    CMPQ R9, $16
    JB test

    XORQ DX, DX
    MOVQ R9, AX
    SHRQ $4, AX
    SHLQ $4, AX
    MOVQ $15, CX
    IDIVQ CX
    IMULQ $15, AX
    DECQ R9

    VMOVDQU32 shift1x32<>(SB), Z2
    KXORW K2, K2, K2
testAscending15:
    VMOVDQU32 (R8)(SI*4), Z0
    VMOVDQU32 Z2, Z1
    VPERMI2D Z0, Z0, Z1
    VCMPPS $2, Z1, Z0, K1
    KORTESTW K2, K1
    JNC testDescending15
    ADDQ $15, SI
    CMPQ SI, AX
    JNE testAscending15
    VZEROUPPER
    JMP testAscending
testDescending15:
    VMOVDQU32 (R8)(DI*4), Z0
    VMOVDQU32 Z2, Z1
    VPERMI2D Z0, Z0, Z1
    VCMPPS $5, Z1, Z0, K1
    KORTESTW K2, K1
    JNC undefined15
    ADDQ $15, DI
    CMPQ DI, AX
    JNE testDescending15
    VZEROUPPER
    JMP testDescending

test:
    DECQ R9
testAscending:
    CMPQ SI, R9
    JAE ascending
    MOVLQZX (R8)(SI*4), BX
    MOVLQZX 4(R8)(SI*4), DX
    INCQ SI
    MOVQ BX, X0
    MOVQ DX, X1
    UCOMISS X1, X0
    JBE testAscending
    JMP testDescending
ascending:
    MOVQ $ASCENDING, ret+24(FP)
    RET
testDescending:
    CMPQ DI, R9
    JAE descending
    MOVLQZX (R8)(DI*4), BX
    MOVLQZX 4(R8)(DI*4), DX
    INCQ DI
    MOVQ BX, X0
    MOVQ DX, X1
    UCOMISS X1, X0
    JAE testDescending
    JMP undefined
descending:
    MOVQ $DESCENDING, ret+24(FP)
    RET
undefined15:
    VZEROUPPER
undefined:
    MOVQ $UNDEFINED, ret+24(FP)
    RET

// func orderOfFloat64(data []uint64) int
TEXT ·orderOfFloat64(SB), NOSPLIT, $-32
    MOVQ data_base+0(FP), R8
    MOVQ data_len+8(FP), R9
    XORQ SI, SI
    XORQ DI, DI

    CMPQ R9, $2
    JB undefined

    CMPB ·hasAVX512VL(SB), $0
    JE test

    CMPQ R9, $8
    JB test

    XORQ DX, DX
    MOVQ R9, AX
    SHRQ $3, AX
    SHLQ $3, AX
    MOVQ $7, CX
    IDIVQ CX
    IMULQ $7, AX
    DECQ R9

    VMOVDQU64 shift1x64<>(SB), Z2
    KXORB K2, K2, K2
testAscending7:
    VMOVDQU64 (R8)(SI*8), Z0
    VMOVDQU64 Z2, Z1
    VPERMI2Q Z0, Z0, Z1
    VCMPPD $2, Z1, Z0, K1
    KORTESTB K2, K1
    JNC testDescending7
    ADDQ $7, SI
    CMPQ SI, AX
    JNE testAscending7
    VZEROUPPER
    JMP testAscending
testDescending7:
    VMOVDQU64 (R8)(DI*8), Z0
    VMOVDQU64 Z2, Z1
    VPERMI2Q Z0, Z0, Z1
    VCMPPD $5, Z1, Z0, K1
    KORTESTB K2, K1
    JNC undefined7
    ADDQ $7, DI
    CMPQ DI, AX
    JNE testDescending7
    VZEROUPPER
    JMP testDescending

test:
    DECQ R9
testAscending:
    CMPQ SI, R9
    JAE ascending
    MOVQ (R8)(SI*8), BX
    MOVQ 8(R8)(SI*8), DX
    INCQ SI
    MOVQ BX, X0
    MOVQ DX, X1
    UCOMISD X1, X0
    JBE testAscending
    JMP testDescending
ascending:
    MOVQ $ASCENDING, ret+24(FP)
    RET
testDescending:
    CMPQ DI, R9
    JAE descending
    MOVQ (R8)(DI*8), BX
    MOVQ 8(R8)(DI*8), DX
    INCQ DI
    MOVQ BX, X0
    MOVQ DX, X1
    UCOMISD X1, X0
    JAE testDescending
    JMP undefined
descending:
    MOVQ $DESCENDING, ret+24(FP)
    RET
undefined7:
    VZEROUPPER
undefined:
    MOVQ $UNDEFINED, ret+24(FP)
    RET
