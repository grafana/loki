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

TEXT ·_arithmetic_binary_neon(SB), NOSPLIT|NOFRAME, $0-48
	MOVD typ+0(FP), R0
	MOVB op+8(FP), R1
	MOVD inLeft+16(FP), R2
	MOVD inRight+24(FP), R3
	MOVD out+32(FP), R4
	MOVD len+40(FP), R5

	CMP $6, R0
	BEQ LneonBinary32Int
	CMP $7, R0
	BEQ LneonBinary32Int
	CMP $8, R0
	BEQ LneonBinary64Int
	CMP $9, R0
	BEQ LneonBinary64Int
	CMP $11, R0
	BEQ LneonBinary32Float
	CMP $12, R0
	BEQ LneonBinary64Float
	RET

LneonBinary32Int:
	CMP $0, R1
	BEQ LneonBinary32IntAdd
	CMP $1, R1
	BEQ LneonBinary32IntSub
	CMP $2, R1
	BEQ LneonBinary32IntMul
	RET

LneonBinary32IntAdd:
	CMP $0, R5
	BLE LneonBinaryReturn
LneonBinary32IntAddVector:
	CMP $4, R5
	BLT LneonBinary32IntAddTail
	VLD1 (R2), [V0.S4]
	VLD1 (R3), [V1.S4]
	VADD V1.S4, V0.S4, V0.S4
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonBinary32IntAddVector
LneonBinary32IntAddTail:
	CMP $0, R5
	BEQ LneonBinaryReturn
	MOVW (R2), R6
	MOVW (R3), R7
	ADDW R7, R6, R6
	MOVW R6, (R4)
	ADD $4, R2, R2
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonBinary32IntAddTail

LneonBinary32IntSub:
	CMP $0, R5
	BLE LneonBinaryReturn
LneonBinary32IntSubVector:
	CMP $4, R5
	BLT LneonBinary32IntSubTail
	VLD1 (R2), [V0.S4]
	VLD1 (R3), [V1.S4]
	VSUB V1.S4, V0.S4, V0.S4
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonBinary32IntSubVector
LneonBinary32IntSubTail:
	CMP $0, R5
	BEQ LneonBinaryReturn
	MOVW (R2), R6
	MOVW (R3), R7
	SUBW R7, R6, R6
	MOVW R6, (R4)
	ADD $4, R2, R2
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonBinary32IntSubTail

LneonBinary32IntMul:
	CMP $0, R5
	BLE LneonBinaryReturn
LneonBinary32IntMulVector:
	CMP $4, R5
	BLT LneonBinary32IntMulTail
	VLD1 (R2), [V0.S4]
	VLD1 (R3), [V1.S4]
	WORD $0x4ea09c20 // mul v0.4s, v1.4s, v0.4s
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonBinary32IntMulVector
LneonBinary32IntMulTail:
	CMP $0, R5
	BEQ LneonBinaryReturn
	MOVW (R2), R6
	MOVW (R3), R7
	MULW R7, R6, R6
	MOVW R6, (R4)
	ADD $4, R2, R2
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonBinary32IntMulTail

LneonBinary64Int:
	CMP $0, R1
	BEQ LneonBinary64IntAdd
	CMP $1, R1
	BEQ LneonBinary64IntSub
	RET

LneonBinary64IntAdd:
	CMP $0, R5
	BLE LneonBinaryReturn
LneonBinary64IntAddVector:
	CMP $2, R5
	BLT LneonBinary64IntAddTail
	VLD1 (R2), [V0.D2]
	VLD1 (R3), [V1.D2]
	VADD V1.D2, V0.D2, V0.D2
	VST1 [V0.D2], (R4)
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonBinary64IntAddVector
LneonBinary64IntAddTail:
	CMP $0, R5
	BEQ LneonBinaryReturn
	MOVD (R2), R6
	MOVD (R3), R7
	ADD R7, R6, R6
	MOVD R6, (R4)
	ADD $8, R2, R2
	ADD $8, R3, R3
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonBinary64IntAddTail

LneonBinary64IntSub:
	CMP $0, R5
	BLE LneonBinaryReturn
LneonBinary64IntSubVector:
	CMP $2, R5
	BLT LneonBinary64IntSubTail
	VLD1 (R2), [V0.D2]
	VLD1 (R3), [V1.D2]
	VSUB V1.D2, V0.D2, V0.D2
	VST1 [V0.D2], (R4)
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonBinary64IntSubVector
LneonBinary64IntSubTail:
	CMP $0, R5
	BEQ LneonBinaryReturn
	MOVD (R2), R6
	MOVD (R3), R7
	SUB R7, R6, R6
	MOVD R6, (R4)
	ADD $8, R2, R2
	ADD $8, R3, R3
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonBinary64IntSubTail

LneonBinary32Float:
	CMP $0, R1
	BEQ LneonBinary32FloatAdd
	CMP $1, R1
	BEQ LneonBinary32FloatSub
	CMP $2, R1
	BEQ LneonBinary32FloatMul
	RET

LneonBinary32FloatAdd:
	CMP $0, R5
	BLE LneonBinaryReturn
LneonBinary32FloatAddVector:
	CMP $4, R5
	BLT LneonBinary32FloatAddTail
	VLD1 (R2), [V0.S4]
	VLD1 (R3), [V1.S4]
	WORD $0x4e21d400 // fadd v0.4s, v0.4s, v1.4s
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonBinary32FloatAddVector
LneonBinary32FloatAddTail:
	CMP $0, R5
	BEQ LneonBinaryReturn
	FMOVS (R2), F0
	FMOVS (R3), F1
	FADDS F1, F0, F0
	FMOVS F0, (R4)
	ADD $4, R2, R2
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonBinary32FloatAddTail

LneonBinary32FloatSub:
	CMP $0, R5
	BLE LneonBinaryReturn
LneonBinary32FloatSubVector:
	CMP $4, R5
	BLT LneonBinary32FloatSubTail
	VLD1 (R2), [V0.S4]
	VLD1 (R3), [V1.S4]
	WORD $0x4ea1d400 // fsub v0.4s, v0.4s, v1.4s
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonBinary32FloatSubVector
LneonBinary32FloatSubTail:
	CMP $0, R5
	BEQ LneonBinaryReturn
	FMOVS (R2), F0
	FMOVS (R3), F1
	FSUBS F1, F0, F0
	FMOVS F0, (R4)
	ADD $4, R2, R2
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonBinary32FloatSubTail

LneonBinary32FloatMul:
	CMP $0, R5
	BLE LneonBinaryReturn
LneonBinary32FloatMulVector:
	CMP $4, R5
	BLT LneonBinary32FloatMulTail
	VLD1 (R2), [V0.S4]
	VLD1 (R3), [V1.S4]
	WORD $0x6e21dc00 // fmul v0.4s, v0.4s, v1.4s
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonBinary32FloatMulVector
LneonBinary32FloatMulTail:
	CMP $0, R5
	BEQ LneonBinaryReturn
	FMOVS (R2), F0
	FMOVS (R3), F1
	FMULS F1, F0, F0
	FMOVS F0, (R4)
	ADD $4, R2, R2
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonBinary32FloatMulTail

LneonBinary64Float:
	CMP $0, R1
	BEQ LneonBinary64FloatAdd
	CMP $1, R1
	BEQ LneonBinary64FloatSub
	CMP $2, R1
	BEQ LneonBinary64FloatMul
	RET

LneonBinary64FloatAdd:
	CMP $0, R5
	BLE LneonBinaryReturn
LneonBinary64FloatAddVector:
	CMP $2, R5
	BLT LneonBinary64FloatAddTail
	VLD1 (R2), [V0.D2]
	VLD1 (R3), [V1.D2]
	WORD $0x4e61d400 // fadd v0.2d, v0.2d, v1.2d
	VST1 [V0.D2], (R4)
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonBinary64FloatAddVector
LneonBinary64FloatAddTail:
	CMP $0, R5
	BEQ LneonBinaryReturn
	FMOVD (R2), F0
	FMOVD (R3), F1
	FADDD F1, F0, F0
	FMOVD F0, (R4)
	ADD $8, R2, R2
	ADD $8, R3, R3
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonBinary64FloatAddTail

LneonBinary64FloatSub:
	CMP $0, R5
	BLE LneonBinaryReturn
LneonBinary64FloatSubVector:
	CMP $2, R5
	BLT LneonBinary64FloatSubTail
	VLD1 (R2), [V0.D2]
	VLD1 (R3), [V1.D2]
	WORD $0x4ee1d400 // fsub v0.2d, v0.2d, v1.2d
	VST1 [V0.D2], (R4)
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonBinary64FloatSubVector
LneonBinary64FloatSubTail:
	CMP $0, R5
	BEQ LneonBinaryReturn
	FMOVD (R2), F0
	FMOVD (R3), F1
	FSUBD F1, F0, F0
	FMOVD F0, (R4)
	ADD $8, R2, R2
	ADD $8, R3, R3
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonBinary64FloatSubTail

LneonBinary64FloatMul:
	CMP $0, R5
	BLE LneonBinaryReturn
LneonBinary64FloatMulVector:
	CMP $2, R5
	BLT LneonBinary64FloatMulTail
	VLD1 (R2), [V0.D2]
	VLD1 (R3), [V1.D2]
	WORD $0x6e61dc00 // fmul v0.2d, v0.2d, v1.2d
	VST1 [V0.D2], (R4)
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonBinary64FloatMulVector
LneonBinary64FloatMulTail:
	CMP $0, R5
	BEQ LneonBinaryReturn
	FMOVD (R2), F0
	FMOVD (R3), F1
	FMULD F1, F0, F0
	FMOVD F0, (R4)
	ADD $8, R2, R2
	ADD $8, R3, R3
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonBinary64FloatMulTail

LneonBinaryReturn:
	RET
TEXT ·_arithmetic_arr_scalar_neon(SB), NOSPLIT|NOFRAME, $0-48
	MOVD typ+0(FP), R0
	MOVB op+8(FP), R1
	MOVD inLeft+16(FP), R2
	MOVD inRight+24(FP), R3
	MOVD out+32(FP), R4
	MOVD len+40(FP), R5

	CMP $6, R0
	BEQ LneonArrScalar32Int
	CMP $7, R0
	BEQ LneonArrScalar32Int
	CMP $8, R0
	BEQ LneonArrScalar64Int
	CMP $9, R0
	BEQ LneonArrScalar64Int
	CMP $11, R0
	BEQ LneonArrScalar32Float
	CMP $12, R0
	BEQ LneonArrScalar64Float
	RET

LneonArrScalar32Int:
	CMP $0, R1
	BEQ LneonArrScalar32IntAdd
	CMP $1, R1
	BEQ LneonArrScalar32IntSub
	CMP $2, R1
	BEQ LneonArrScalar32IntMul
	RET

LneonArrScalar32IntAdd:
	CMP $0, R5
	BLE LneonArrScalarReturn
	VLD1R (R3), [V1.S4]
LneonArrScalar32IntAddVector:
	CMP $4, R5
	BLT LneonArrScalar32IntAddTail
	VLD1 (R2), [V0.S4]
	VADD V1.S4, V0.S4, V0.S4
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonArrScalar32IntAddVector
LneonArrScalar32IntAddTail:
	CMP $0, R5
	BEQ LneonArrScalarReturn
	MOVW (R2), R6
	MOVW (R3), R7
	ADDW R7, R6, R6
	MOVW R6, (R4)
	ADD $4, R2, R2
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonArrScalar32IntAddTail

LneonArrScalar32IntSub:
	CMP $0, R5
	BLE LneonArrScalarReturn
	VLD1R (R3), [V1.S4]
LneonArrScalar32IntSubVector:
	CMP $4, R5
	BLT LneonArrScalar32IntSubTail
	VLD1 (R2), [V0.S4]
	VSUB V1.S4, V0.S4, V0.S4
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonArrScalar32IntSubVector
LneonArrScalar32IntSubTail:
	CMP $0, R5
	BEQ LneonArrScalarReturn
	MOVW (R2), R6
	MOVW (R3), R7
	SUBW R7, R6, R6
	MOVW R6, (R4)
	ADD $4, R2, R2
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonArrScalar32IntSubTail

LneonArrScalar32IntMul:
	CMP $0, R5
	BLE LneonArrScalarReturn
	VLD1R (R3), [V1.S4]
LneonArrScalar32IntMulVector:
	CMP $4, R5
	BLT LneonArrScalar32IntMulTail
	VLD1 (R2), [V0.S4]
	WORD $0x4ea09c20 // mul v0.4s, v1.4s, v0.4s
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonArrScalar32IntMulVector
LneonArrScalar32IntMulTail:
	CMP $0, R5
	BEQ LneonArrScalarReturn
	MOVW (R2), R6
	MOVW (R3), R7
	MULW R7, R6, R6
	MOVW R6, (R4)
	ADD $4, R2, R2
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonArrScalar32IntMulTail

LneonArrScalar64Int:
	CMP $0, R1
	BEQ LneonArrScalar64IntAdd
	CMP $1, R1
	BEQ LneonArrScalar64IntSub
	RET

LneonArrScalar64IntAdd:
	CMP $0, R5
	BLE LneonArrScalarReturn
	VLD1R (R3), [V1.D2]
LneonArrScalar64IntAddVector:
	CMP $2, R5
	BLT LneonArrScalar64IntAddTail
	VLD1 (R2), [V0.D2]
	VADD V1.D2, V0.D2, V0.D2
	VST1 [V0.D2], (R4)
	ADD $16, R2, R2
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonArrScalar64IntAddVector
LneonArrScalar64IntAddTail:
	CMP $0, R5
	BEQ LneonArrScalarReturn
	MOVD (R2), R6
	MOVD (R3), R7
	ADD R7, R6, R6
	MOVD R6, (R4)
	ADD $8, R2, R2
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonArrScalar64IntAddTail

LneonArrScalar64IntSub:
	CMP $0, R5
	BLE LneonArrScalarReturn
	VLD1R (R3), [V1.D2]
LneonArrScalar64IntSubVector:
	CMP $2, R5
	BLT LneonArrScalar64IntSubTail
	VLD1 (R2), [V0.D2]
	VSUB V1.D2, V0.D2, V0.D2
	VST1 [V0.D2], (R4)
	ADD $16, R2, R2
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonArrScalar64IntSubVector
LneonArrScalar64IntSubTail:
	CMP $0, R5
	BEQ LneonArrScalarReturn
	MOVD (R2), R6
	MOVD (R3), R7
	SUB R7, R6, R6
	MOVD R6, (R4)
	ADD $8, R2, R2
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonArrScalar64IntSubTail

LneonArrScalar32Float:
	CMP $0, R1
	BEQ LneonArrScalar32FloatAdd
	CMP $1, R1
	BEQ LneonArrScalar32FloatSub
	CMP $2, R1
	BEQ LneonArrScalar32FloatMul
	RET

LneonArrScalar32FloatAdd:
	CMP $0, R5
	BLE LneonArrScalarReturn
	VLD1R (R3), [V1.S4]
LneonArrScalar32FloatAddVector:
	CMP $4, R5
	BLT LneonArrScalar32FloatAddTail
	VLD1 (R2), [V0.S4]
	WORD $0x4e21d400 // fadd v0.4s, v0.4s, v1.4s
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonArrScalar32FloatAddVector
LneonArrScalar32FloatAddTail:
	CMP $0, R5
	BEQ LneonArrScalarReturn
	FMOVS (R2), F0
	FMOVS (R3), F1
	FADDS F1, F0, F0
	FMOVS F0, (R4)
	ADD $4, R2, R2
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonArrScalar32FloatAddTail

LneonArrScalar32FloatSub:
	CMP $0, R5
	BLE LneonArrScalarReturn
	VLD1R (R3), [V1.S4]
LneonArrScalar32FloatSubVector:
	CMP $4, R5
	BLT LneonArrScalar32FloatSubTail
	VLD1 (R2), [V0.S4]
	WORD $0x4ea1d400 // fsub v0.4s, v0.4s, v1.4s
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonArrScalar32FloatSubVector
LneonArrScalar32FloatSubTail:
	CMP $0, R5
	BEQ LneonArrScalarReturn
	FMOVS (R2), F0
	FMOVS (R3), F1
	FSUBS F1, F0, F0
	FMOVS F0, (R4)
	ADD $4, R2, R2
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonArrScalar32FloatSubTail

LneonArrScalar32FloatMul:
	CMP $0, R5
	BLE LneonArrScalarReturn
	VLD1R (R3), [V1.S4]
LneonArrScalar32FloatMulVector:
	CMP $4, R5
	BLT LneonArrScalar32FloatMulTail
	VLD1 (R2), [V0.S4]
	WORD $0x6e21dc00 // fmul v0.4s, v0.4s, v1.4s
	VST1 [V0.S4], (R4)
	ADD $16, R2, R2
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonArrScalar32FloatMulVector
LneonArrScalar32FloatMulTail:
	CMP $0, R5
	BEQ LneonArrScalarReturn
	FMOVS (R2), F0
	FMOVS (R3), F1
	FMULS F1, F0, F0
	FMOVS F0, (R4)
	ADD $4, R2, R2
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonArrScalar32FloatMulTail

LneonArrScalar64Float:
	CMP $0, R1
	BEQ LneonArrScalar64FloatAdd
	CMP $1, R1
	BEQ LneonArrScalar64FloatSub
	CMP $2, R1
	BEQ LneonArrScalar64FloatMul
	RET

LneonArrScalar64FloatAdd:
	CMP $0, R5
	BLE LneonArrScalarReturn
	VLD1R (R3), [V1.D2]
LneonArrScalar64FloatAddVector:
	CMP $2, R5
	BLT LneonArrScalar64FloatAddTail
	VLD1 (R2), [V0.D2]
	WORD $0x4e61d400 // fadd v0.2d, v0.2d, v1.2d
	VST1 [V0.D2], (R4)
	ADD $16, R2, R2
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonArrScalar64FloatAddVector
LneonArrScalar64FloatAddTail:
	CMP $0, R5
	BEQ LneonArrScalarReturn
	FMOVD (R2), F0
	FMOVD (R3), F1
	FADDD F1, F0, F0
	FMOVD F0, (R4)
	ADD $8, R2, R2
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonArrScalar64FloatAddTail

LneonArrScalar64FloatSub:
	CMP $0, R5
	BLE LneonArrScalarReturn
	VLD1R (R3), [V1.D2]
LneonArrScalar64FloatSubVector:
	CMP $2, R5
	BLT LneonArrScalar64FloatSubTail
	VLD1 (R2), [V0.D2]
	WORD $0x4ee1d400 // fsub v0.2d, v0.2d, v1.2d
	VST1 [V0.D2], (R4)
	ADD $16, R2, R2
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonArrScalar64FloatSubVector
LneonArrScalar64FloatSubTail:
	CMP $0, R5
	BEQ LneonArrScalarReturn
	FMOVD (R2), F0
	FMOVD (R3), F1
	FSUBD F1, F0, F0
	FMOVD F0, (R4)
	ADD $8, R2, R2
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonArrScalar64FloatSubTail

LneonArrScalar64FloatMul:
	CMP $0, R5
	BLE LneonArrScalarReturn
	VLD1R (R3), [V1.D2]
LneonArrScalar64FloatMulVector:
	CMP $2, R5
	BLT LneonArrScalar64FloatMulTail
	VLD1 (R2), [V0.D2]
	WORD $0x6e61dc00 // fmul v0.2d, v0.2d, v1.2d
	VST1 [V0.D2], (R4)
	ADD $16, R2, R2
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonArrScalar64FloatMulVector
LneonArrScalar64FloatMulTail:
	CMP $0, R5
	BEQ LneonArrScalarReturn
	FMOVD (R2), F0
	FMOVD (R3), F1
	FMULD F1, F0, F0
	FMOVD F0, (R4)
	ADD $8, R2, R2
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonArrScalar64FloatMulTail

LneonArrScalarReturn:
	RET

TEXT ·_arithmetic_scalar_arr_neon(SB), NOSPLIT|NOFRAME, $0-48
	MOVD typ+0(FP), R0
	MOVB op+8(FP), R1
	MOVD inLeft+16(FP), R2
	MOVD inRight+24(FP), R3
	MOVD out+32(FP), R4
	MOVD len+40(FP), R5

	CMP $6, R0
	BEQ LneonScalarArr32Int
	CMP $7, R0
	BEQ LneonScalarArr32Int
	CMP $8, R0
	BEQ LneonScalarArr64Int
	CMP $9, R0
	BEQ LneonScalarArr64Int
	CMP $11, R0
	BEQ LneonScalarArr32Float
	CMP $12, R0
	BEQ LneonScalarArr64Float
	RET

LneonScalarArr32Int:
	CMP $0, R1
	BEQ LneonScalarArr32IntAdd
	CMP $1, R1
	BEQ LneonScalarArr32IntSub
	CMP $2, R1
	BEQ LneonScalarArr32IntMul
	RET

LneonScalarArr32IntAdd:
	CMP $0, R5
	BLE LneonScalarArrReturn
	VLD1R (R2), [V0.S4]
LneonScalarArr32IntAddVector:
	CMP $4, R5
	BLT LneonScalarArr32IntAddTail
	VLD1 (R3), [V1.S4]
	VADD V1.S4, V0.S4, V2.S4
	VST1 [V2.S4], (R4)
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonScalarArr32IntAddVector
LneonScalarArr32IntAddTail:
	CMP $0, R5
	BEQ LneonScalarArrReturn
	MOVW (R2), R6
	MOVW (R3), R7
	ADDW R7, R6, R6
	MOVW R6, (R4)
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonScalarArr32IntAddTail

LneonScalarArr32IntSub:
	CMP $0, R5
	BLE LneonScalarArrReturn
	VLD1R (R2), [V0.S4]
LneonScalarArr32IntSubVector:
	CMP $4, R5
	BLT LneonScalarArr32IntSubTail
	VLD1 (R3), [V1.S4]
	VSUB V1.S4, V0.S4, V2.S4
	VST1 [V2.S4], (R4)
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonScalarArr32IntSubVector
LneonScalarArr32IntSubTail:
	CMP $0, R5
	BEQ LneonScalarArrReturn
	MOVW (R2), R6
	MOVW (R3), R7
	SUBW R7, R6, R6
	MOVW R6, (R4)
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonScalarArr32IntSubTail

LneonScalarArr32IntMul:
	CMP $0, R5
	BLE LneonScalarArrReturn
	VLD1R (R2), [V0.S4]
LneonScalarArr32IntMulVector:
	CMP $4, R5
	BLT LneonScalarArr32IntMulTail
	VLD1 (R3), [V1.S4]
	WORD $0x4ea09c22 // mul v2.4s, v1.4s, v0.4s
	VST1 [V2.S4], (R4)
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonScalarArr32IntMulVector
LneonScalarArr32IntMulTail:
	CMP $0, R5
	BEQ LneonScalarArrReturn
	MOVW (R2), R6
	MOVW (R3), R7
	MULW R7, R6, R6
	MOVW R6, (R4)
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonScalarArr32IntMulTail

LneonScalarArr64Int:
	CMP $0, R1
	BEQ LneonScalarArr64IntAdd
	CMP $1, R1
	BEQ LneonScalarArr64IntSub
	RET

LneonScalarArr64IntAdd:
	CMP $0, R5
	BLE LneonScalarArrReturn
	VLD1R (R2), [V0.D2]
LneonScalarArr64IntAddVector:
	CMP $2, R5
	BLT LneonScalarArr64IntAddTail
	VLD1 (R3), [V1.D2]
	VADD V1.D2, V0.D2, V2.D2
	VST1 [V2.D2], (R4)
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonScalarArr64IntAddVector
LneonScalarArr64IntAddTail:
	CMP $0, R5
	BEQ LneonScalarArrReturn
	MOVD (R2), R6
	MOVD (R3), R7
	ADD R7, R6, R6
	MOVD R6, (R4)
	ADD $8, R3, R3
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonScalarArr64IntAddTail

LneonScalarArr64IntSub:
	CMP $0, R5
	BLE LneonScalarArrReturn
	VLD1R (R2), [V0.D2]
LneonScalarArr64IntSubVector:
	CMP $2, R5
	BLT LneonScalarArr64IntSubTail
	VLD1 (R3), [V1.D2]
	VSUB V1.D2, V0.D2, V2.D2
	VST1 [V2.D2], (R4)
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonScalarArr64IntSubVector
LneonScalarArr64IntSubTail:
	CMP $0, R5
	BEQ LneonScalarArrReturn
	MOVD (R2), R6
	MOVD (R3), R7
	SUB R7, R6, R6
	MOVD R6, (R4)
	ADD $8, R3, R3
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonScalarArr64IntSubTail

LneonScalarArr32Float:
	CMP $0, R1
	BEQ LneonScalarArr32FloatAdd
	CMP $1, R1
	BEQ LneonScalarArr32FloatSub
	CMP $2, R1
	BEQ LneonScalarArr32FloatMul
	RET

LneonScalarArr32FloatAdd:
	CMP $0, R5
	BLE LneonScalarArrReturn
	VLD1R (R2), [V0.S4]
LneonScalarArr32FloatAddVector:
	CMP $4, R5
	BLT LneonScalarArr32FloatAddTail
	VLD1 (R3), [V1.S4]
	WORD $0x4e21d402 // fadd v2.4s, v0.4s, v1.4s
	VST1 [V2.S4], (R4)
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonScalarArr32FloatAddVector
LneonScalarArr32FloatAddTail:
	CMP $0, R5
	BEQ LneonScalarArrReturn
	FMOVS (R2), F0
	FMOVS (R3), F1
	FADDS F1, F0, F0
	FMOVS F0, (R4)
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonScalarArr32FloatAddTail

LneonScalarArr32FloatSub:
	CMP $0, R5
	BLE LneonScalarArrReturn
	VLD1R (R2), [V0.S4]
LneonScalarArr32FloatSubVector:
	CMP $4, R5
	BLT LneonScalarArr32FloatSubTail
	VLD1 (R3), [V1.S4]
	WORD $0x4ea1d402 // fsub v2.4s, v0.4s, v1.4s
	VST1 [V2.S4], (R4)
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonScalarArr32FloatSubVector
LneonScalarArr32FloatSubTail:
	CMP $0, R5
	BEQ LneonScalarArrReturn
	FMOVS (R2), F0
	FMOVS (R3), F1
	FSUBS F1, F0, F0
	FMOVS F0, (R4)
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonScalarArr32FloatSubTail

LneonScalarArr32FloatMul:
	CMP $0, R5
	BLE LneonScalarArrReturn
	VLD1R (R2), [V0.S4]
LneonScalarArr32FloatMulVector:
	CMP $4, R5
	BLT LneonScalarArr32FloatMulTail
	VLD1 (R3), [V1.S4]
	WORD $0x6e21dc02 // fmul v2.4s, v0.4s, v1.4s
	VST1 [V2.S4], (R4)
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	JMP LneonScalarArr32FloatMulVector
LneonScalarArr32FloatMulTail:
	CMP $0, R5
	BEQ LneonScalarArrReturn
	FMOVS (R2), F0
	FMOVS (R3), F1
	FMULS F1, F0, F0
	FMOVS F0, (R4)
	ADD $4, R3, R3
	ADD $4, R4, R4
	SUB $1, R5, R5
	JMP LneonScalarArr32FloatMulTail

LneonScalarArr64Float:
	CMP $0, R1
	BEQ LneonScalarArr64FloatAdd
	CMP $1, R1
	BEQ LneonScalarArr64FloatSub
	CMP $2, R1
	BEQ LneonScalarArr64FloatMul
	RET

LneonScalarArr64FloatAdd:
	CMP $0, R5
	BLE LneonScalarArrReturn
	VLD1R (R2), [V0.D2]
LneonScalarArr64FloatAddVector:
	CMP $2, R5
	BLT LneonScalarArr64FloatAddTail
	VLD1 (R3), [V1.D2]
	WORD $0x4e61d402 // fadd v2.2d, v0.2d, v1.2d
	VST1 [V2.D2], (R4)
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonScalarArr64FloatAddVector
LneonScalarArr64FloatAddTail:
	CMP $0, R5
	BEQ LneonScalarArrReturn
	FMOVD (R2), F0
	FMOVD (R3), F1
	FADDD F1, F0, F0
	FMOVD F0, (R4)
	ADD $8, R3, R3
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonScalarArr64FloatAddTail

LneonScalarArr64FloatSub:
	CMP $0, R5
	BLE LneonScalarArrReturn
	VLD1R (R2), [V0.D2]
LneonScalarArr64FloatSubVector:
	CMP $2, R5
	BLT LneonScalarArr64FloatSubTail
	VLD1 (R3), [V1.D2]
	WORD $0x4ee1d402 // fsub v2.2d, v0.2d, v1.2d
	VST1 [V2.D2], (R4)
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonScalarArr64FloatSubVector
LneonScalarArr64FloatSubTail:
	CMP $0, R5
	BEQ LneonScalarArrReturn
	FMOVD (R2), F0
	FMOVD (R3), F1
	FSUBD F1, F0, F0
	FMOVD F0, (R4)
	ADD $8, R3, R3
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonScalarArr64FloatSubTail

LneonScalarArr64FloatMul:
	CMP $0, R5
	BLE LneonScalarArrReturn
	VLD1R (R2), [V0.D2]
LneonScalarArr64FloatMulVector:
	CMP $2, R5
	BLT LneonScalarArr64FloatMulTail
	VLD1 (R3), [V1.D2]
	WORD $0x6e61dc02 // fmul v2.2d, v0.2d, v1.2d
	VST1 [V2.D2], (R4)
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $2, R5, R5
	JMP LneonScalarArr64FloatMulVector
LneonScalarArr64FloatMulTail:
	CMP $0, R5
	BEQ LneonScalarArrReturn
	FMOVD (R2), F0
	FMOVD (R3), F1
	FMULD F1, F0, F0
	FMOVD F0, (R4)
	ADD $8, R3, R3
	ADD $8, R4, R4
	SUB $1, R5, R5
	JMP LneonScalarArr64FloatMulTail

LneonScalarArrReturn:
	RET
TEXT ·_arithmetic_unary_same_types_neon(SB), NOSPLIT|NOFRAME, $0-40
	MOVD typ+0(FP), R0
	MOVB op+8(FP), R1
	MOVD input+16(FP), R2
	MOVD output+24(FP), R3
	MOVD len+32(FP), R5

	CMP $7, R0
	BEQ LneonUnary32SignedInt
	CMP $6, R0
	BEQ LneonUnary32UnsignedInt
	CMP $9, R0
	BEQ LneonUnary64SignedInt
	CMP $8, R0
	BEQ LneonUnary64UnsignedInt
	CMP $11, R0
	BEQ LneonUnary32Float
	CMP $12, R0
	BEQ LneonUnary64Float
	RET

LneonUnary32SignedInt:
	CMP $4, R1
	BEQ LneonUnary32SignedIntAbs
	CMP $5, R1
	BEQ LneonUnary32SignedIntNeg
	RET

LneonUnary32SignedIntAbs:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary32SignedIntAbsVector:
	CMP $4, R5
	BLT LneonUnary32SignedIntAbsTail
	VLD1 (R2), [V0.S4]
	WORD $0x4ea0b800 // abs v0.4s, v0.4s
	VST1 [V0.S4], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $4, R5, R5
	JMP LneonUnary32SignedIntAbsVector
LneonUnary32SignedIntAbsTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	MOVW (R2), R6
	CMPW $0, R6
	BGE LneonUnary32SignedIntAbsStore
	NEGW R6, R6
LneonUnary32SignedIntAbsStore:
	MOVW R6, (R3)
	ADD $4, R2, R2
	ADD $4, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary32SignedIntAbsTail

LneonUnary32SignedIntNeg:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary32SignedIntNegVector:
	CMP $4, R5
	BLT LneonUnary32SignedIntNegTail
	VLD1 (R2), [V0.S4]
	WORD $0x6ea0b800 // neg v0.4s, v0.4s
	VST1 [V0.S4], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $4, R5, R5
	JMP LneonUnary32SignedIntNegVector
LneonUnary32SignedIntNegTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	MOVW (R2), R6
	NEGW R6, R6
	MOVW R6, (R3)
	ADD $4, R2, R2
	ADD $4, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary32SignedIntNegTail

LneonUnary32UnsignedInt:
	CMP $4, R1
	BEQ LneonUnary32UnsignedIntAbs
	CMP $5, R1
	BEQ LneonUnary32UnsignedIntNeg
	RET

LneonUnary32UnsignedIntAbs:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary32UnsignedIntAbsVector:
	CMP $4, R5
	BLT LneonUnary32UnsignedIntAbsTail
	VLD1 (R2), [V0.S4]
	VST1 [V0.S4], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $4, R5, R5
	JMP LneonUnary32UnsignedIntAbsVector
LneonUnary32UnsignedIntAbsTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	MOVW (R2), R6
	MOVW R6, (R3)
	ADD $4, R2, R2
	ADD $4, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary32UnsignedIntAbsTail

LneonUnary32UnsignedIntNeg:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary32UnsignedIntNegVector:
	CMP $4, R5
	BLT LneonUnary32UnsignedIntNegTail
	VLD1 (R2), [V0.S4]
	WORD $0x6ea0b800 // neg v0.4s, v0.4s
	VST1 [V0.S4], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $4, R5, R5
	JMP LneonUnary32UnsignedIntNegVector
LneonUnary32UnsignedIntNegTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	MOVW (R2), R6
	NEGW R6, R6
	MOVW R6, (R3)
	ADD $4, R2, R2
	ADD $4, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary32UnsignedIntNegTail

LneonUnary64SignedInt:
	CMP $4, R1
	BEQ LneonUnary64SignedIntAbs
	CMP $5, R1
	BEQ LneonUnary64SignedIntNeg
	RET

LneonUnary64SignedIntAbs:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary64SignedIntAbsVector:
	CMP $2, R5
	BLT LneonUnary64SignedIntAbsTail
	VLD1 (R2), [V0.D2]
	WORD $0x4ee0b800 // abs v0.2d, v0.2d
	VST1 [V0.D2], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $2, R5, R5
	JMP LneonUnary64SignedIntAbsVector
LneonUnary64SignedIntAbsTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	MOVD (R2), R6
	CMP $0, R6
	BGE LneonUnary64SignedIntAbsStore
	NEG R6, R6
LneonUnary64SignedIntAbsStore:
	MOVD R6, (R3)
	ADD $8, R2, R2
	ADD $8, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary64SignedIntAbsTail

LneonUnary64SignedIntNeg:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary64SignedIntNegVector:
	CMP $2, R5
	BLT LneonUnary64SignedIntNegTail
	VLD1 (R2), [V0.D2]
	WORD $0x6ee0b800 // neg v0.2d, v0.2d
	VST1 [V0.D2], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $2, R5, R5
	JMP LneonUnary64SignedIntNegVector
LneonUnary64SignedIntNegTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	MOVD (R2), R6
	NEG R6, R6
	MOVD R6, (R3)
	ADD $8, R2, R2
	ADD $8, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary64SignedIntNegTail

LneonUnary64UnsignedInt:
	CMP $4, R1
	BEQ LneonUnary64UnsignedIntAbs
	CMP $5, R1
	BEQ LneonUnary64UnsignedIntNeg
	RET

LneonUnary64UnsignedIntAbs:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary64UnsignedIntAbsVector:
	CMP $2, R5
	BLT LneonUnary64UnsignedIntAbsTail
	VLD1 (R2), [V0.D2]
	VST1 [V0.D2], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $2, R5, R5
	JMP LneonUnary64UnsignedIntAbsVector
LneonUnary64UnsignedIntAbsTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	MOVD (R2), R6
	MOVD R6, (R3)
	ADD $8, R2, R2
	ADD $8, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary64UnsignedIntAbsTail

LneonUnary64UnsignedIntNeg:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary64UnsignedIntNegVector:
	CMP $2, R5
	BLT LneonUnary64UnsignedIntNegTail
	VLD1 (R2), [V0.D2]
	WORD $0x6ee0b800 // neg v0.2d, v0.2d
	VST1 [V0.D2], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $2, R5, R5
	JMP LneonUnary64UnsignedIntNegVector
LneonUnary64UnsignedIntNegTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	MOVD (R2), R6
	NEG R6, R6
	MOVD R6, (R3)
	ADD $8, R2, R2
	ADD $8, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary64UnsignedIntNegTail

LneonUnary32Float:
	CMP $4, R1
	BEQ LneonUnary32FloatAbs
	CMP $5, R1
	BEQ LneonUnary32FloatNeg
	RET

LneonUnary32FloatAbs:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary32FloatAbsVector:
	CMP $4, R5
	BLT LneonUnary32FloatAbsTail
	VLD1 (R2), [V0.S4]
	WORD $0x4ea0f800 // fabs v0.4s, v0.4s
	VST1 [V0.S4], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $4, R5, R5
	JMP LneonUnary32FloatAbsVector
LneonUnary32FloatAbsTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	FMOVS (R2), F0
	FABSS F0, F0
	FMOVS F0, (R3)
	ADD $4, R2, R2
	ADD $4, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary32FloatAbsTail

LneonUnary32FloatNeg:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary32FloatNegVector:
	CMP $4, R5
	BLT LneonUnary32FloatNegTail
	VLD1 (R2), [V0.S4]
	WORD $0x6ea0f800 // fneg v0.4s, v0.4s
	VST1 [V0.S4], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $4, R5, R5
	JMP LneonUnary32FloatNegVector
LneonUnary32FloatNegTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	FMOVS (R2), F0
	FNEGS F0, F0
	FMOVS F0, (R3)
	ADD $4, R2, R2
	ADD $4, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary32FloatNegTail

LneonUnary64Float:
	CMP $4, R1
	BEQ LneonUnary64FloatAbs
	CMP $5, R1
	BEQ LneonUnary64FloatNeg
	RET

LneonUnary64FloatAbs:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary64FloatAbsVector:
	CMP $2, R5
	BLT LneonUnary64FloatAbsTail
	VLD1 (R2), [V0.D2]
	WORD $0x4ee0f800 // fabs v0.2d, v0.2d
	VST1 [V0.D2], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $2, R5, R5
	JMP LneonUnary64FloatAbsVector
LneonUnary64FloatAbsTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	FMOVD (R2), F0
	FABSD F0, F0
	FMOVD F0, (R3)
	ADD $8, R2, R2
	ADD $8, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary64FloatAbsTail

LneonUnary64FloatNeg:
	CMP $0, R5
	BLE LneonUnaryReturn
LneonUnary64FloatNegVector:
	CMP $2, R5
	BLT LneonUnary64FloatNegTail
	VLD1 (R2), [V0.D2]
	WORD $0x6ee0f800 // fneg v0.2d, v0.2d
	VST1 [V0.D2], (R3)
	ADD $16, R2, R2
	ADD $16, R3, R3
	SUB $2, R5, R5
	JMP LneonUnary64FloatNegVector
LneonUnary64FloatNegTail:
	CMP $0, R5
	BEQ LneonUnaryReturn
	FMOVD (R2), F0
	FNEGD F0, F0
	FMOVD F0, (R3)
	ADD $8, R2, R2
	ADD $8, R3, R3
	SUB $1, R5, R5
	JMP LneonUnary64FloatNegTail

LneonUnaryReturn:
	RET
