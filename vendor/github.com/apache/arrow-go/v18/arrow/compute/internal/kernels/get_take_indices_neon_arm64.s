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
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

//go:build go1.18 && arm64 && !noasm && !appengine

#include "textflag.h"

// func _getTakeIndicesUint32NEON(filter, output, positions, counts unsafe.Pointer, nbytes, tailMask int64)
TEXT ·_getTakeIndicesUint32NEON(SB), NOSPLIT|NOFRAME, $0-48
	MOVD filter+0(FP), R0
	MOVD output+8(FP), R1
	MOVD positions+16(FP), R2
	MOVD counts+24(FP), R3
	MOVD nbytes+32(FP), R4
	MOVD tailMask+40(FP), R5
	MOVD $0, R6 // input bit position

byte_loop:
	CBZ R4, done
	MOVBU (R0), R7
	SUB $1, R4, R8
	CBNZ R8, byte_not_tail
	AND R5, R7, R7

byte_not_tail:
	AND $15, R7, R8
	MOVBU (R3)(R8), R9
	CBZ R9, high_nibble
	LSL $4, R8, R10
	ADD R2, R10, R10
	VLD1 (R10), [V0.S4]
	VDUP R6, V1.S4
	VADD V1.S4, V0.S4, V0.S4
	CMP $4, R9
	BNE low_scalar
	VST1 [V0.S4], (R1)
	ADD $16, R1, R1
	JMP high_nibble

low_scalar:
	VMOV V0.S[0], R10
	MOVW R10, (R1)
	ADD $4, R1, R1
	CMP $1, R9
	BEQ high_nibble
	VMOV V0.S[1], R10
	MOVW R10, (R1)
	ADD $4, R1, R1
	CMP $2, R9
	BEQ high_nibble
	VMOV V0.S[2], R10
	MOVW R10, (R1)
	ADD $4, R1, R1

high_nibble:
	LSR $4, R7, R8
	MOVBU (R3)(R8), R9
	CBZ R9, next_byte
	LSL $4, R8, R10
	ADD R2, R10, R10
	VLD1 (R10), [V0.S4]
	ADD $4, R6, R10
	VDUP R10, V1.S4
	VADD V1.S4, V0.S4, V0.S4
	CMP $4, R9
	BNE high_scalar
	VST1 [V0.S4], (R1)
	ADD $16, R1, R1
	JMP next_byte

high_scalar:
	VMOV V0.S[0], R10
	MOVW R10, (R1)
	ADD $4, R1, R1
	CMP $1, R9
	BEQ next_byte
	VMOV V0.S[1], R10
	MOVW R10, (R1)
	ADD $4, R1, R1
	CMP $2, R9
	BEQ next_byte
	VMOV V0.S[2], R10
	MOVW R10, (R1)
	ADD $4, R1, R1

next_byte:
	ADD $8, R6, R6
	ADD $1, R0, R0
	SUB $1, R4, R4
	JMP byte_loop

done:
	RET
