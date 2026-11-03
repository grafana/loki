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

// 32-bit packing weights: [1, 2, 4, 8].
DATA LCMPNEON_WEIGHTS<>+0(SB)/8, $0x0000000200000001
DATA LCMPNEON_WEIGHTS<>+8(SB)/8, $0x0000000800000004
// 64-bit variable shift counts: [0, 1].
DATA LCMPNEON_WEIGHTS<>+16(SB)/8, $0x0000000000000000
DATA LCMPNEON_WEIGHTS<>+24(SB)/8, $0x0000000000000001
GLOBL LCMPNEON_WEIGHTS<>(SB), (NOPTR+RODATA), $32

TEXT ·_comparison_neon(SB), NOSPLIT|NOFRAME, $0-56
	MOVD typ+0(FP), R0
	MOVD op+8(FP), R1
	MOVD shape+16(FP), R2
	MOVD left+24(FP), R3
	MOVD right+32(FP), R4
	MOVD out+40(FP), R5
	MOVD groups+48(FP), R6

	CMP $6, R0
	BEQ Lcomparison32Int
	CMP $7, R0
	BEQ Lcomparison32Int
	CMP $8, R0
	BEQ Lcomparison64Int
	CMP $9, R0
	BEQ Lcomparison64Int
	CMP $11, R0
	BEQ Lcomparison32Float
	CMP $12, R0
	BEQ Lcomparison64Float
	JMP LcomparisonReturn

Lcomparison32Int:
	MOVD $LCMPNEON_WEIGHTS<>(SB), R10
	VLD1 (R10), [V20.S4]
	CMP $0, R2
	BEQ Lcomparison32IntAASelect
	CMP $1, R2
	BEQ Lcomparison32IntASSelect
	CMP $2, R2
	BEQ Lcomparison32IntSASelect
	JMP LcomparisonReturn

Lcomparison32IntAASelect:
	CMP $0, R1
	BEQ Lcomparison32IntAAEQ
	CMP $1, R1
	BEQ Lcomparison32IntAANE
	CMP $2, R1
	BEQ Lcomparison32IntAAGTSelect
	CMP $3, R1
	BEQ Lcomparison32IntAAGESelect
	JMP LcomparisonReturn

Lcomparison32IntAAGTSelect:
	CMP $6, R0
	BEQ Lcomparison32IntAAUGT
	JMP Lcomparison32IntAASGT

Lcomparison32IntAAGESelect:
	CMP $6, R0
	BEQ Lcomparison32IntAAUGE
	JMP Lcomparison32IntAASGE

Lcomparison32IntASSelect:
	CMP $0, R1
	BEQ Lcomparison32IntASEQ
	CMP $1, R1
	BEQ Lcomparison32IntASNE
	CMP $2, R1
	BEQ Lcomparison32IntASGTSelect
	CMP $3, R1
	BEQ Lcomparison32IntASGESelect
	JMP LcomparisonReturn

Lcomparison32IntASGTSelect:
	CMP $6, R0
	BEQ Lcomparison32IntASUGT
	JMP Lcomparison32IntASSGT

Lcomparison32IntASGESelect:
	CMP $6, R0
	BEQ Lcomparison32IntASUGE
	JMP Lcomparison32IntASSGE

Lcomparison32IntSASelect:
	CMP $0, R1
	BEQ Lcomparison32IntSAEQ
	CMP $1, R1
	BEQ Lcomparison32IntSANE
	CMP $2, R1
	BEQ Lcomparison32IntSAGTSelect
	CMP $3, R1
	BEQ Lcomparison32IntSAGESelect
	JMP LcomparisonReturn

Lcomparison32IntSAGTSelect:
	CMP $6, R0
	BEQ Lcomparison32IntSAUGT
	JMP Lcomparison32IntSASGT

Lcomparison32IntSAGESelect:
	CMP $6, R0
	BEQ Lcomparison32IntSAUGE
	JMP Lcomparison32IntSASGE

Lcomparison32IntAAEQ:
Lcomparison32IntAAEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6ea18c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6ea18c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntAAEQLoop

Lcomparison32IntAANE:
Lcomparison32IntAANELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6ea18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6ea18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntAANELoop

Lcomparison32IntAASGT:
Lcomparison32IntAASGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x4ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x4ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntAASGTLoop

Lcomparison32IntAAUGT:
Lcomparison32IntAAUGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntAAUGTLoop

Lcomparison32IntAASGE:
Lcomparison32IntAASGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x4ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x4ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntAASGELoop

Lcomparison32IntAAUGE:
Lcomparison32IntAAUGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntAAUGELoop

Lcomparison32IntASEQ:
	VLD1R (R4), [V1.S4]
Lcomparison32IntASEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	WORD $0x6ea18c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	VLD1 (R3), [V0.S4]
	WORD $0x6ea18c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntASEQLoop

Lcomparison32IntASNE:
	VLD1R (R4), [V1.S4]
Lcomparison32IntASNELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	WORD $0x6ea18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	VLD1 (R3), [V0.S4]
	WORD $0x6ea18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntASNELoop

Lcomparison32IntASSGT:
	VLD1R (R4), [V1.S4]
Lcomparison32IntASSGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	WORD $0x4ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	VLD1 (R3), [V0.S4]
	WORD $0x4ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntASSGTLoop

Lcomparison32IntASUGT:
	VLD1R (R4), [V1.S4]
Lcomparison32IntASUGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	WORD $0x6ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	VLD1 (R3), [V0.S4]
	WORD $0x6ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntASUGTLoop

Lcomparison32IntASSGE:
	VLD1R (R4), [V1.S4]
Lcomparison32IntASSGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	WORD $0x4ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	VLD1 (R3), [V0.S4]
	WORD $0x4ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntASSGELoop

Lcomparison32IntASUGE:
	VLD1R (R4), [V1.S4]
Lcomparison32IntASUGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	WORD $0x6ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	VLD1 (R3), [V0.S4]
	WORD $0x6ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntASUGELoop

Lcomparison32IntSAEQ:
	VLD1R (R3), [V0.S4]
Lcomparison32IntSAEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.S4]
	WORD $0x6ea18c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R4, R4
	VLD1 (R4), [V1.S4]
	WORD $0x6ea18c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntSAEQLoop

Lcomparison32IntSANE:
	VLD1R (R3), [V0.S4]
Lcomparison32IntSANELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.S4]
	WORD $0x6ea18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R4, R4
	VLD1 (R4), [V1.S4]
	WORD $0x6ea18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntSANELoop

Lcomparison32IntSASGT:
	VLD1R (R3), [V0.S4]
Lcomparison32IntSASGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.S4]
	WORD $0x4ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R4, R4
	VLD1 (R4), [V1.S4]
	WORD $0x4ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntSASGTLoop

Lcomparison32IntSAUGT:
	VLD1R (R3), [V0.S4]
Lcomparison32IntSAUGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.S4]
	WORD $0x6ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R4, R4
	VLD1 (R4), [V1.S4]
	WORD $0x6ea13402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntSAUGTLoop

Lcomparison32IntSASGE:
	VLD1R (R3), [V0.S4]
Lcomparison32IntSASGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.S4]
	WORD $0x4ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R4, R4
	VLD1 (R4), [V1.S4]
	WORD $0x4ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntSASGELoop

Lcomparison32IntSAUGE:
	VLD1R (R3), [V0.S4]
Lcomparison32IntSAUGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.S4]
	WORD $0x6ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R4, R4
	VLD1 (R4), [V1.S4]
	WORD $0x6ea13c02
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32IntSAUGELoop

Lcomparison64Int:
	MOVD $LCMPNEON_WEIGHTS<>(SB), R10
	ADD $16, R10, R10
	VLD1 (R10), [V20.D2]
	CMP $0, R2
	BEQ Lcomparison64IntAASelect
	CMP $1, R2
	BEQ Lcomparison64IntASSelect
	CMP $2, R2
	BEQ Lcomparison64IntSASelect
	JMP LcomparisonReturn

Lcomparison64IntAASelect:
	CMP $0, R1
	BEQ Lcomparison64IntAAEQ
	CMP $1, R1
	BEQ Lcomparison64IntAANE
	CMP $2, R1
	BEQ Lcomparison64IntAAGTSelect
	CMP $3, R1
	BEQ Lcomparison64IntAAGESelect
	JMP LcomparisonReturn

Lcomparison64IntAAGTSelect:
	CMP $8, R0
	BEQ Lcomparison64IntAAUGT
	JMP Lcomparison64IntAASGT

Lcomparison64IntAAGESelect:
	CMP $8, R0
	BEQ Lcomparison64IntAAUGE
	JMP Lcomparison64IntAASGE

Lcomparison64IntASSelect:
	CMP $0, R1
	BEQ Lcomparison64IntASEQ
	CMP $1, R1
	BEQ Lcomparison64IntASNE
	CMP $2, R1
	BEQ Lcomparison64IntASGTSelect
	CMP $3, R1
	BEQ Lcomparison64IntASGESelect
	JMP LcomparisonReturn

Lcomparison64IntASGTSelect:
	CMP $8, R0
	BEQ Lcomparison64IntASUGT
	JMP Lcomparison64IntASSGT

Lcomparison64IntASGESelect:
	CMP $8, R0
	BEQ Lcomparison64IntASUGE
	JMP Lcomparison64IntASSGE

Lcomparison64IntSASelect:
	CMP $0, R1
	BEQ Lcomparison64IntSAEQ
	CMP $1, R1
	BEQ Lcomparison64IntSANE
	CMP $2, R1
	BEQ Lcomparison64IntSAGTSelect
	CMP $3, R1
	BEQ Lcomparison64IntSAGESelect
	JMP LcomparisonReturn

Lcomparison64IntSAGTSelect:
	CMP $8, R0
	BEQ Lcomparison64IntSAUGT
	JMP Lcomparison64IntSASGT

Lcomparison64IntSAGESelect:
	CMP $8, R0
	BEQ Lcomparison64IntSAUGE
	JMP Lcomparison64IntSASGE

Lcomparison64IntAAEQ:
Lcomparison64IntAAEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntAAEQLoop

Lcomparison64IntAANE:
Lcomparison64IntAANELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntAANELoop

Lcomparison64IntAASGT:
Lcomparison64IntAASGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntAASGTLoop

Lcomparison64IntAAUGT:
Lcomparison64IntAAUGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntAAUGTLoop

Lcomparison64IntAASGE:
Lcomparison64IntAASGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntAASGELoop

Lcomparison64IntAAUGE:
Lcomparison64IntAAUGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntAAUGELoop

Lcomparison64IntASEQ:
	VLD1R (R4), [V1.D2]
Lcomparison64IntASEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntASEQLoop

Lcomparison64IntASNE:
	VLD1R (R4), [V1.D2]
Lcomparison64IntASNELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntASNELoop

Lcomparison64IntASSGT:
	VLD1R (R4), [V1.D2]
Lcomparison64IntASSGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntASSGTLoop

Lcomparison64IntASUGT:
	VLD1R (R4), [V1.D2]
Lcomparison64IntASUGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntASUGTLoop

Lcomparison64IntASSGE:
	VLD1R (R4), [V1.D2]
Lcomparison64IntASSGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntASSGELoop

Lcomparison64IntASUGE:
	VLD1R (R4), [V1.D2]
Lcomparison64IntASUGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntASUGELoop

Lcomparison64IntSAEQ:
	VLD1R (R3), [V0.D2]
Lcomparison64IntSAEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntSAEQLoop

Lcomparison64IntSANE:
	VLD1R (R3), [V0.D2]
Lcomparison64IntSANELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee18c02
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntSANELoop

Lcomparison64IntSASGT:
	VLD1R (R3), [V0.D2]
Lcomparison64IntSASGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntSASGTLoop

Lcomparison64IntSAUGT:
	VLD1R (R3), [V0.D2]
Lcomparison64IntSAUGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntSAUGTLoop

Lcomparison64IntSASGE:
	VLD1R (R3), [V0.D2]
Lcomparison64IntSASGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntSASGELoop

Lcomparison64IntSAUGE:
	VLD1R (R3), [V0.D2]
Lcomparison64IntSAUGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee13c02
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64IntSAUGELoop

Lcomparison32Float:
	MOVD $LCMPNEON_WEIGHTS<>(SB), R10
	VLD1 (R10), [V20.S4]
	CMP $0, R2
	BEQ Lcomparison32FloatAASelect
	CMP $1, R2
	BEQ Lcomparison32FloatASSelect
	CMP $2, R2
	BEQ Lcomparison32FloatSASelect
	JMP LcomparisonReturn

Lcomparison32FloatAASelect:
	CMP $0, R1
	BEQ Lcomparison32FloatAAEQ
	CMP $1, R1
	BEQ Lcomparison32FloatAANE
	CMP $2, R1
	BEQ Lcomparison32FloatAAGTSelect
	CMP $3, R1
	BEQ Lcomparison32FloatAAGESelect
	JMP LcomparisonReturn

Lcomparison32FloatAAGTSelect:
	JMP Lcomparison32FloatAAFGT

Lcomparison32FloatAAGESelect:
	JMP Lcomparison32FloatAAFGE

Lcomparison32FloatASSelect:
	CMP $0, R1
	BEQ Lcomparison32FloatASEQ
	CMP $1, R1
	BEQ Lcomparison32FloatASNE
	CMP $2, R1
	BEQ Lcomparison32FloatASGTSelect
	CMP $3, R1
	BEQ Lcomparison32FloatASGESelect
	JMP LcomparisonReturn

Lcomparison32FloatASGTSelect:
	JMP Lcomparison32FloatASFGT

Lcomparison32FloatASGESelect:
	JMP Lcomparison32FloatASFGE

Lcomparison32FloatSASelect:
	CMP $0, R1
	BEQ Lcomparison32FloatSAEQ
	CMP $1, R1
	BEQ Lcomparison32FloatSANE
	CMP $2, R1
	BEQ Lcomparison32FloatSAGTSelect
	CMP $3, R1
	BEQ Lcomparison32FloatSAGESelect
	JMP LcomparisonReturn

Lcomparison32FloatSAGTSelect:
	JMP Lcomparison32FloatSAFGT

Lcomparison32FloatSAGESelect:
	JMP Lcomparison32FloatSAFGE

Lcomparison32FloatAAEQ:
Lcomparison32FloatAAEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x4e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x4e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatAAEQLoop

Lcomparison32FloatAANE:
Lcomparison32FloatAANELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x4e21e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x4e21e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatAANELoop

Lcomparison32FloatAAFGT:
Lcomparison32FloatAAFGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6ea1e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6ea1e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatAAFGTLoop

Lcomparison32FloatAAFGE:
Lcomparison32FloatAAFGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.S4]
	VLD1 (R4), [V1.S4]
	WORD $0x6e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatAAFGELoop

Lcomparison32FloatASEQ:
	VLD1R (R4), [V1.S4]
Lcomparison32FloatASEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	WORD $0x4e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	VLD1 (R3), [V0.S4]
	WORD $0x4e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatASEQLoop

Lcomparison32FloatASNE:
	VLD1R (R4), [V1.S4]
Lcomparison32FloatASNELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	WORD $0x4e21e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	VLD1 (R3), [V0.S4]
	WORD $0x4e21e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatASNELoop

Lcomparison32FloatASFGT:
	VLD1R (R4), [V1.S4]
Lcomparison32FloatASFGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	WORD $0x6ea1e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	VLD1 (R3), [V0.S4]
	WORD $0x6ea1e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatASFGTLoop

Lcomparison32FloatASFGE:
	VLD1R (R4), [V1.S4]
Lcomparison32FloatASFGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.S4]
	WORD $0x6e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R3, R3
	VLD1 (R3), [V0.S4]
	WORD $0x6e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatASFGELoop

Lcomparison32FloatSAEQ:
	VLD1R (R3), [V0.S4]
Lcomparison32FloatSAEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.S4]
	WORD $0x4e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R4, R4
	VLD1 (R4), [V1.S4]
	WORD $0x4e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatSAEQLoop

Lcomparison32FloatSANE:
	VLD1R (R3), [V0.S4]
Lcomparison32FloatSANELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.S4]
	WORD $0x4e21e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R4, R4
	VLD1 (R4), [V1.S4]
	WORD $0x4e21e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatSANELoop

Lcomparison32FloatSAFGT:
	VLD1R (R3), [V0.S4]
Lcomparison32FloatSAFGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.S4]
	WORD $0x6ea1e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R4, R4
	VLD1 (R4), [V1.S4]
	WORD $0x6ea1e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatSAFGTLoop

Lcomparison32FloatSAFGE:
	VLD1R (R3), [V0.S4]
Lcomparison32FloatSAFGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.S4]
	WORD $0x6e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c47 // umov result
	ADD $16, R4, R4
	VLD1 (R4), [V1.S4]
	WORD $0x6e21e402
	VUSHR $31, V2.S4, V2.S4
	WORD $0x4eb49c42 // mul v2.4s, v2.4s, v20.4s
	VADDV V2.S4, V2
	WORD $0x0e043c48 // umov result
	LSL $4, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison32FloatSAFGELoop

Lcomparison64Float:
	MOVD $LCMPNEON_WEIGHTS<>(SB), R10
	ADD $16, R10, R10
	VLD1 (R10), [V20.D2]
	CMP $0, R2
	BEQ Lcomparison64FloatAASelect
	CMP $1, R2
	BEQ Lcomparison64FloatASSelect
	CMP $2, R2
	BEQ Lcomparison64FloatSASelect
	JMP LcomparisonReturn

Lcomparison64FloatAASelect:
	CMP $0, R1
	BEQ Lcomparison64FloatAAEQ
	CMP $1, R1
	BEQ Lcomparison64FloatAANE
	CMP $2, R1
	BEQ Lcomparison64FloatAAGTSelect
	CMP $3, R1
	BEQ Lcomparison64FloatAAGESelect
	JMP LcomparisonReturn

Lcomparison64FloatAAGTSelect:
	JMP Lcomparison64FloatAAFGT

Lcomparison64FloatAAGESelect:
	JMP Lcomparison64FloatAAFGE

Lcomparison64FloatASSelect:
	CMP $0, R1
	BEQ Lcomparison64FloatASEQ
	CMP $1, R1
	BEQ Lcomparison64FloatASNE
	CMP $2, R1
	BEQ Lcomparison64FloatASGTSelect
	CMP $3, R1
	BEQ Lcomparison64FloatASGESelect
	JMP LcomparisonReturn

Lcomparison64FloatASGTSelect:
	JMP Lcomparison64FloatASFGT

Lcomparison64FloatASGESelect:
	JMP Lcomparison64FloatASFGE

Lcomparison64FloatSASelect:
	CMP $0, R1
	BEQ Lcomparison64FloatSAEQ
	CMP $1, R1
	BEQ Lcomparison64FloatSANE
	CMP $2, R1
	BEQ Lcomparison64FloatSAGTSelect
	CMP $3, R1
	BEQ Lcomparison64FloatSAGESelect
	JMP LcomparisonReturn

Lcomparison64FloatSAGTSelect:
	JMP Lcomparison64FloatSAFGT

Lcomparison64FloatSAGESelect:
	JMP Lcomparison64FloatSAFGE

Lcomparison64FloatAAEQ:
Lcomparison64FloatAAEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatAAEQLoop

Lcomparison64FloatAANE:
Lcomparison64FloatAANELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatAANELoop

Lcomparison64FloatAAFGT:
Lcomparison64FloatAAFGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatAAFGTLoop

Lcomparison64FloatAAFGE:
Lcomparison64FloatAAFGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	ADD $16, R4, R4
	VLD1 (R3), [V0.D2]
	VLD1 (R4), [V1.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatAAFGELoop

Lcomparison64FloatASEQ:
	VLD1R (R4), [V1.D2]
Lcomparison64FloatASEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatASEQLoop

Lcomparison64FloatASNE:
	VLD1R (R4), [V1.D2]
Lcomparison64FloatASNELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatASNELoop

Lcomparison64FloatASFGT:
	VLD1R (R4), [V1.D2]
Lcomparison64FloatASFGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatASFGTLoop

Lcomparison64FloatASFGE:
	VLD1R (R4), [V1.D2]
Lcomparison64FloatASFGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R3), [V0.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R3, R3
	VLD1 (R3), [V0.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R3, R3
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatASFGELoop

Lcomparison64FloatSAEQ:
	VLD1R (R3), [V0.D2]
Lcomparison64FloatSAEQLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatSAEQLoop

Lcomparison64FloatSANE:
	VLD1R (R3), [V0.D2]
Lcomparison64FloatSANELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x4e61e402
	WORD $0x6e205842 // mvn v2.16b, v2.16b
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatSANELoop

Lcomparison64FloatSAFGT:
	VLD1R (R3), [V0.D2]
Lcomparison64FloatSAFGTLoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6ee1e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatSAFGTLoop

Lcomparison64FloatSAFGE:
	VLD1R (R3), [V0.D2]
Lcomparison64FloatSAFGELoop:
	CMP $0, R6
	BEQ LcomparisonReturn
	VLD1 (R4), [V1.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c47 // umov x7, v2.d[0]
	WORD $0x4e183c48 // umov x8, v2.d[1]
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $2, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $4, R8, R8
	ORR R8, R7, R7
	ADD $16, R4, R4
	VLD1 (R4), [V1.D2]
	WORD $0x6e61e402
	VUSHR $63, V2.D2, V2.D2
	WORD $0x6ef44442 // ushl v2.2d, v2.2d, v20.2d
	WORD $0x4e083c48 // umov x8, v2.d[0]
	WORD $0x4e183c49 // umov x9, v2.d[1]
	ORR R9, R8, R8
	LSL $6, R8, R8
	ORR R8, R7, R7
	MOVB R7, (R5)
	ADD $16, R4, R4
	ADD $1, R5, R5
	SUB $1, R6, R6
	JMP Lcomparison64FloatSAFGELoop

LcomparisonReturn:
	RET
