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

//go:build arm64 && !noasm && !appengine
// +build arm64,!noasm,!appengine

// Each input byte is a Go bool (0 or 1). Masking nonzero values with the bit
// weights turns each true value into its output bit's weight. VADDV then
// reduces each group of eight weighted bytes to one Arrow bitmap byte.

#include "textflag.h"

DATA boolBitWeights<>+0(SB)/8, $0x8040201008040201
GLOBL boolBitWeights<>(SB), (NOPTR+RODATA), $8

// func _packBoolsNEON(values, dst unsafe.Pointer, length int)
TEXT ·_packBoolsNEON(SB), NOSPLIT, $0-24
	MOVD values+0(FP), R0
	MOVD dst+8(FP), R1
	MOVD length+16(FP), R2

	MOVD $boolBitWeights<>(SB), R3
	VLD1 (R3), [V4.B8]

loop:
	CMP $32, R2
	BLT done

	VLD1 (R0), [V0.B8]
	ADD $8, R0, R8
	VLD1 (R8), [V1.B8]
	ADD $16, R0, R8
	VLD1 (R8), [V2.B8]
	ADD $24, R0, R8
	VLD1 (R8), [V3.B8]

	VCMTST V0.B8, V0.B8, V0.B8
	VCMTST V1.B8, V1.B8, V1.B8
	VCMTST V2.B8, V2.B8, V2.B8
	VCMTST V3.B8, V3.B8, V3.B8
	VAND V4.B8, V0.B8, V0.B8
	VAND V4.B8, V1.B8, V1.B8
	VAND V4.B8, V2.B8, V2.B8
	VAND V4.B8, V3.B8, V3.B8
	VADDV V0.B8, V0
	VADDV V1.B8, V1
	VADDV V2.B8, V2
	VADDV V3.B8, V3

	VMOV V0.B[0], R4
	VMOV V1.B[0], R5
	VMOV V2.B[0], R6
	VMOV V3.B[0], R7
	MOVB R4, (R1)
	MOVB R5, 1(R1)
	MOVB R6, 2(R1)
	MOVB R7, 3(R1)

	ADD $32, R0
	ADD $4, R1
	SUB $32, R2
	B loop

done:
	RET
