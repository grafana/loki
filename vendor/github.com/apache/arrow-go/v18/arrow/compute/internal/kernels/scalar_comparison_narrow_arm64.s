// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build go1.18 && arm64 && !noasm && !appengine

#include "textflag.h"

DATA LCMPNEON_NARROW_WEIGHTS<>+0(SB)/8, $0x0706050403020100
DATA LCMPNEON_NARROW_WEIGHTS<>+8(SB)/8, $0x0003000200010000
DATA LCMPNEON_NARROW_WEIGHTS<>+16(SB)/8, $0x0007000600050004
GLOBL LCMPNEON_NARROW_WEIGHTS<>(SB), (NOPTR+RODATA), $24

TEXT ·_comparison_narrow_neon(SB), NOSPLIT|NOFRAME, $0-56
	MOVD typ+0(FP), R0
	MOVD op+8(FP), R1
	MOVD shape+16(FP), R2
	MOVD left+24(FP), R3
	MOVD right+32(FP), R4
	MOVD out+40(FP), R5
	MOVD groups+48(FP), R6

	CMP $2, R0
	BEQ Lnarrow8Unsigned
	CMP $3, R0
	BEQ Lnarrow8Signed
	CMP $4, R0
	BEQ Lnarrow16Unsigned
	CMP $5, R0
	BEQ Lnarrow16Signed
	JMP LnarrowReturn

Lnarrow8Signed:
	MOVD $LCMPNEON_NARROW_WEIGHTS<>(SB), R10
	VLD1 (R10), [V20.B8]
	CMP $0, R2
	BEQ Lnarrow8SignedAASelect
	CMP $1, R2
	BEQ Lnarrow8SignedASSelect
	CMP $2, R2
	BEQ Lnarrow8SignedSASelect
	JMP LnarrowReturn

Lnarrow8SignedAASelect:
	CMP $0, R1
	BEQ Lnarrow8SignedAAEQ
	CMP $1, R1
	BEQ Lnarrow8SignedAANE
	CMP $2, R1
	BEQ Lnarrow8SignedAAGT
	CMP $3, R1
	BEQ Lnarrow8SignedAAGE
	JMP LnarrowReturn

Lnarrow8SignedASSelect:
	CMP $0, R1
	BEQ Lnarrow8SignedASEQ
	CMP $1, R1
	BEQ Lnarrow8SignedASNE
	CMP $2, R1
	BEQ Lnarrow8SignedASGT
	CMP $3, R1
	BEQ Lnarrow8SignedASGE
	JMP LnarrowReturn

Lnarrow8SignedSASelect:
	CMP $0, R1
	BEQ Lnarrow8SignedSAEQ
	CMP $1, R1
	BEQ Lnarrow8SignedSANE
	CMP $2, R1
	BEQ Lnarrow8SignedSAGT
	CMP $3, R1
	BEQ Lnarrow8SignedSAGE
	JMP LnarrowReturn

Lnarrow8SignedAAEQ:
Lnarrow8SignedAAEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	VLD1 (R4), [V1.B8]
	WORD $0x2e218c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedAAEQLoop

Lnarrow8SignedAANE:
Lnarrow8SignedAANELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	VLD1 (R4), [V1.B8]
	WORD $0x2e218c02
	WORD $0x2e205842
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedAANELoop

Lnarrow8SignedAAGT:
Lnarrow8SignedAAGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	VLD1 (R4), [V1.B8]
	WORD $0x0e213402
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedAAGTLoop

Lnarrow8SignedAAGE:
Lnarrow8SignedAAGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	VLD1 (R4), [V1.B8]
	WORD $0x0e213c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedAAGELoop

Lnarrow8SignedASEQ:
VLD1R (R4), [V1.B8]
Lnarrow8SignedASEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	WORD $0x2e218c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedASEQLoop

Lnarrow8SignedASNE:
VLD1R (R4), [V1.B8]
Lnarrow8SignedASNELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	WORD $0x2e218c02
	WORD $0x2e205842
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedASNELoop

Lnarrow8SignedASGT:
VLD1R (R4), [V1.B8]
Lnarrow8SignedASGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	WORD $0x0e213402
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedASGTLoop

Lnarrow8SignedASGE:
VLD1R (R4), [V1.B8]
Lnarrow8SignedASGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	WORD $0x0e213c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedASGELoop

Lnarrow8SignedSAEQ:
VLD1R (R3), [V0.B8]
Lnarrow8SignedSAEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.B8]
	WORD $0x2e218c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedSAEQLoop

Lnarrow8SignedSANE:
VLD1R (R3), [V0.B8]
Lnarrow8SignedSANELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.B8]
	WORD $0x2e218c02
	WORD $0x2e205842
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedSANELoop

Lnarrow8SignedSAGT:
VLD1R (R3), [V0.B8]
Lnarrow8SignedSAGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.B8]
	WORD $0x0e213402
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedSAGTLoop

Lnarrow8SignedSAGE:
VLD1R (R3), [V0.B8]
Lnarrow8SignedSAGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.B8]
	WORD $0x0e213c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8SignedSAGELoop

Lnarrow8Unsigned:
	MOVD $LCMPNEON_NARROW_WEIGHTS<>(SB), R10
	VLD1 (R10), [V20.B8]
	CMP $0, R2
	BEQ Lnarrow8UnsignedAASelect
	CMP $1, R2
	BEQ Lnarrow8UnsignedASSelect
	CMP $2, R2
	BEQ Lnarrow8UnsignedSASelect
	JMP LnarrowReturn

Lnarrow8UnsignedAASelect:
	CMP $0, R1
	BEQ Lnarrow8UnsignedAAEQ
	CMP $1, R1
	BEQ Lnarrow8UnsignedAANE
	CMP $2, R1
	BEQ Lnarrow8UnsignedAAGT
	CMP $3, R1
	BEQ Lnarrow8UnsignedAAGE
	JMP LnarrowReturn

Lnarrow8UnsignedASSelect:
	CMP $0, R1
	BEQ Lnarrow8UnsignedASEQ
	CMP $1, R1
	BEQ Lnarrow8UnsignedASNE
	CMP $2, R1
	BEQ Lnarrow8UnsignedASGT
	CMP $3, R1
	BEQ Lnarrow8UnsignedASGE
	JMP LnarrowReturn

Lnarrow8UnsignedSASelect:
	CMP $0, R1
	BEQ Lnarrow8UnsignedSAEQ
	CMP $1, R1
	BEQ Lnarrow8UnsignedSANE
	CMP $2, R1
	BEQ Lnarrow8UnsignedSAGT
	CMP $3, R1
	BEQ Lnarrow8UnsignedSAGE
	JMP LnarrowReturn

Lnarrow8UnsignedAAEQ:
Lnarrow8UnsignedAAEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	VLD1 (R4), [V1.B8]
	WORD $0x2e218c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedAAEQLoop

Lnarrow8UnsignedAANE:
Lnarrow8UnsignedAANELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	VLD1 (R4), [V1.B8]
	WORD $0x2e218c02
	WORD $0x2e205842
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedAANELoop

Lnarrow8UnsignedAAGT:
Lnarrow8UnsignedAAGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	VLD1 (R4), [V1.B8]
	WORD $0x2e213402
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedAAGTLoop

Lnarrow8UnsignedAAGE:
Lnarrow8UnsignedAAGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	VLD1 (R4), [V1.B8]
	WORD $0x2e213c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedAAGELoop

Lnarrow8UnsignedASEQ:
VLD1R (R4), [V1.B8]
Lnarrow8UnsignedASEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	WORD $0x2e218c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedASEQLoop

Lnarrow8UnsignedASNE:
VLD1R (R4), [V1.B8]
Lnarrow8UnsignedASNELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	WORD $0x2e218c02
	WORD $0x2e205842
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedASNELoop

Lnarrow8UnsignedASGT:
VLD1R (R4), [V1.B8]
Lnarrow8UnsignedASGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	WORD $0x2e213402
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedASGTLoop

Lnarrow8UnsignedASGE:
VLD1R (R4), [V1.B8]
Lnarrow8UnsignedASGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.B8]
	WORD $0x2e213c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedASGELoop

Lnarrow8UnsignedSAEQ:
VLD1R (R3), [V0.B8]
Lnarrow8UnsignedSAEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.B8]
	WORD $0x2e218c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedSAEQLoop

Lnarrow8UnsignedSANE:
VLD1R (R3), [V0.B8]
Lnarrow8UnsignedSANELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.B8]
	WORD $0x2e218c02
	WORD $0x2e205842
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedSANELoop

Lnarrow8UnsignedSAGT:
VLD1R (R3), [V0.B8]
Lnarrow8UnsignedSAGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.B8]
	WORD $0x2e213402
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedSAGTLoop

Lnarrow8UnsignedSAGE:
VLD1R (R3), [V0.B8]
Lnarrow8UnsignedSAGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.B8]
	WORD $0x2e213c02
	VUSHR $7, V2.B8, V2.B8
	WORD $0x2e344442
	VADDV V2.B8, V2
	VMOV V2.B[0], R7
	MOVB R7, (R5)
	ADD $8, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow8UnsignedSAGELoop

Lnarrow16Signed:
	MOVD $LCMPNEON_NARROW_WEIGHTS<>(SB), R10
	ADD $8, R10, R10
	VLD1 (R10), [V20.H8]
	CMP $0, R2
	BEQ Lnarrow16SignedAASelect
	CMP $1, R2
	BEQ Lnarrow16SignedASSelect
	CMP $2, R2
	BEQ Lnarrow16SignedSASelect
	JMP LnarrowReturn

Lnarrow16SignedAASelect:
	CMP $0, R1
	BEQ Lnarrow16SignedAAEQ
	CMP $1, R1
	BEQ Lnarrow16SignedAANE
	CMP $2, R1
	BEQ Lnarrow16SignedAAGT
	CMP $3, R1
	BEQ Lnarrow16SignedAAGE
	JMP LnarrowReturn

Lnarrow16SignedASSelect:
	CMP $0, R1
	BEQ Lnarrow16SignedASEQ
	CMP $1, R1
	BEQ Lnarrow16SignedASNE
	CMP $2, R1
	BEQ Lnarrow16SignedASGT
	CMP $3, R1
	BEQ Lnarrow16SignedASGE
	JMP LnarrowReturn

Lnarrow16SignedSASelect:
	CMP $0, R1
	BEQ Lnarrow16SignedSAEQ
	CMP $1, R1
	BEQ Lnarrow16SignedSANE
	CMP $2, R1
	BEQ Lnarrow16SignedSAGT
	CMP $3, R1
	BEQ Lnarrow16SignedSAGE
	JMP LnarrowReturn

Lnarrow16SignedAAEQ:
Lnarrow16SignedAAEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	VLD1 (R4), [V1.H8]
	WORD $0x6e618c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedAAEQLoop

Lnarrow16SignedAANE:
Lnarrow16SignedAANELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	VLD1 (R4), [V1.H8]
	WORD $0x6e618c02
	WORD $0x6e205842
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedAANELoop

Lnarrow16SignedAAGT:
Lnarrow16SignedAAGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	VLD1 (R4), [V1.H8]
	WORD $0x4e613402
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedAAGTLoop

Lnarrow16SignedAAGE:
Lnarrow16SignedAAGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	VLD1 (R4), [V1.H8]
	WORD $0x4e613c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedAAGELoop

Lnarrow16SignedASEQ:
VLD1R (R4), [V1.H8]
Lnarrow16SignedASEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	WORD $0x6e618c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedASEQLoop

Lnarrow16SignedASNE:
VLD1R (R4), [V1.H8]
Lnarrow16SignedASNELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	WORD $0x6e618c02
	WORD $0x6e205842
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedASNELoop

Lnarrow16SignedASGT:
VLD1R (R4), [V1.H8]
Lnarrow16SignedASGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	WORD $0x4e613402
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedASGTLoop

Lnarrow16SignedASGE:
VLD1R (R4), [V1.H8]
Lnarrow16SignedASGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	WORD $0x4e613c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedASGELoop

Lnarrow16SignedSAEQ:
VLD1R (R3), [V0.H8]
Lnarrow16SignedSAEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.H8]
	WORD $0x6e618c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedSAEQLoop

Lnarrow16SignedSANE:
VLD1R (R3), [V0.H8]
Lnarrow16SignedSANELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.H8]
	WORD $0x6e618c02
	WORD $0x6e205842
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedSANELoop

Lnarrow16SignedSAGT:
VLD1R (R3), [V0.H8]
Lnarrow16SignedSAGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.H8]
	WORD $0x4e613402
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedSAGTLoop

Lnarrow16SignedSAGE:
VLD1R (R3), [V0.H8]
Lnarrow16SignedSAGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.H8]
	WORD $0x4e613c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16SignedSAGELoop

Lnarrow16Unsigned:
	MOVD $LCMPNEON_NARROW_WEIGHTS<>(SB), R10
	ADD $8, R10, R10
	VLD1 (R10), [V20.H8]
	CMP $0, R2
	BEQ Lnarrow16UnsignedAASelect
	CMP $1, R2
	BEQ Lnarrow16UnsignedASSelect
	CMP $2, R2
	BEQ Lnarrow16UnsignedSASelect
	JMP LnarrowReturn

Lnarrow16UnsignedAASelect:
	CMP $0, R1
	BEQ Lnarrow16UnsignedAAEQ
	CMP $1, R1
	BEQ Lnarrow16UnsignedAANE
	CMP $2, R1
	BEQ Lnarrow16UnsignedAAGT
	CMP $3, R1
	BEQ Lnarrow16UnsignedAAGE
	JMP LnarrowReturn

Lnarrow16UnsignedASSelect:
	CMP $0, R1
	BEQ Lnarrow16UnsignedASEQ
	CMP $1, R1
	BEQ Lnarrow16UnsignedASNE
	CMP $2, R1
	BEQ Lnarrow16UnsignedASGT
	CMP $3, R1
	BEQ Lnarrow16UnsignedASGE
	JMP LnarrowReturn

Lnarrow16UnsignedSASelect:
	CMP $0, R1
	BEQ Lnarrow16UnsignedSAEQ
	CMP $1, R1
	BEQ Lnarrow16UnsignedSANE
	CMP $2, R1
	BEQ Lnarrow16UnsignedSAGT
	CMP $3, R1
	BEQ Lnarrow16UnsignedSAGE
	JMP LnarrowReturn

Lnarrow16UnsignedAAEQ:
Lnarrow16UnsignedAAEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	VLD1 (R4), [V1.H8]
	WORD $0x6e618c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedAAEQLoop

Lnarrow16UnsignedAANE:
Lnarrow16UnsignedAANELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	VLD1 (R4), [V1.H8]
	WORD $0x6e618c02
	WORD $0x6e205842
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedAANELoop

Lnarrow16UnsignedAAGT:
Lnarrow16UnsignedAAGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	VLD1 (R4), [V1.H8]
	WORD $0x6e613402
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedAAGTLoop

Lnarrow16UnsignedAAGE:
Lnarrow16UnsignedAAGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	VLD1 (R4), [V1.H8]
	WORD $0x6e613c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedAAGELoop

Lnarrow16UnsignedASEQ:
VLD1R (R4), [V1.H8]
Lnarrow16UnsignedASEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	WORD $0x6e618c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedASEQLoop

Lnarrow16UnsignedASNE:
VLD1R (R4), [V1.H8]
Lnarrow16UnsignedASNELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	WORD $0x6e618c02
	WORD $0x6e205842
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedASNELoop

Lnarrow16UnsignedASGT:
VLD1R (R4), [V1.H8]
Lnarrow16UnsignedASGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	WORD $0x6e613402
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedASGTLoop

Lnarrow16UnsignedASGE:
VLD1R (R4), [V1.H8]
Lnarrow16UnsignedASGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R3), [V0.H8]
	WORD $0x6e613c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedASGELoop

Lnarrow16UnsignedSAEQ:
VLD1R (R3), [V0.H8]
Lnarrow16UnsignedSAEQLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.H8]
	WORD $0x6e618c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedSAEQLoop

Lnarrow16UnsignedSANE:
VLD1R (R3), [V0.H8]
Lnarrow16UnsignedSANELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.H8]
	WORD $0x6e618c02
	WORD $0x6e205842
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedSANELoop

Lnarrow16UnsignedSAGT:
VLD1R (R3), [V0.H8]
Lnarrow16UnsignedSAGTLoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.H8]
	WORD $0x6e613402
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedSAGTLoop

Lnarrow16UnsignedSAGE:
VLD1R (R3), [V0.H8]
Lnarrow16UnsignedSAGELoop:
	CMP $0, R6
	BEQ LnarrowReturn
	VLD1 (R4), [V1.H8]
	WORD $0x6e613c02
	VUSHR $15, V2.H8, V2.H8
	WORD $0x6e744442
	VADDV V2.H8, V2
	VMOV V2.H[0], R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lnarrow16UnsignedSAGELoop

LnarrowReturn:
	RET
