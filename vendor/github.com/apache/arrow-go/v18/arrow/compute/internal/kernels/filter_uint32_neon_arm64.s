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

// Each filter byte selects up to four uint32 values from each of two
// four-value halves. The table-driven VTBL compacts one half in-place. The
// count-specific stores write exactly the selected number of bytes, because
// the output allocation is sized to the filter's population count.

// func _filter_uint32_neon(values, filter, output, tables unsafe.Pointer, length int64)
TEXT ·_filter_uint32_neon(SB), NOSPLIT, $0-40
	MOVD values+0(FP), R0
	MOVD filter+8(FP), R1
	MOVD output+16(FP), R2
	MOVD tables+24(FP), R3
	MOVD length+32(FP), R4

	LSR $3, R4, R4

filter_loop:
	MOVBU (R1), R5
	ADD $1, R1
	CBZ R5, filter_next

	CMPW $255, R5
	BEQ filter_all

	VLD1 (R0), [V0.B16]
	ADD $16, R0, R8
	VLD1 (R8), [V1.B16]

	ANDW $15, R5, R6
	ADD $336, R3, R8
	MOVBU (R8)(R6<<0), R9
	CBZ R9, filter_high

	LSL $4, R6, R8
	ADD R3, R8, R8
	VLD1 (R8), [V2.B16]
	VTBL V2.B16, [V0.B16], V0.B16

	CMPW $1, R9
	BEQ filter_low_store1
	CMPW $2, R9
	BEQ filter_low_store2
	CMPW $3, R9
	BEQ filter_low_store3
	VST1 [V0.S4], (R2)
	ADD $16, R2
	B filter_high

filter_low_store1:
	VMOV V0.S[0], R8
	MOVW R8, (R2)
	ADD $4, R2
	B filter_high

filter_low_store2:
	VMOV V0.S[0], R8
	MOVW R8, (R2)
	VMOV V0.S[1], R8
	MOVW R8, 4(R2)
	ADD $8, R2
	B filter_high

filter_low_store3:
	VMOV V0.S[0], R8
	MOVW R8, (R2)
	VMOV V0.S[1], R8
	MOVW R8, 4(R2)
	VMOV V0.S[2], R8
	MOVW R8, 8(R2)
	ADD $12, R2

filter_high:
	LSR $4, R5, R6
	ADD $336, R3, R8
	MOVBU (R8)(R6<<0), R9
	CBZ R9, filter_next

	LSL $4, R6, R8
	ADD R3, R8, R8
	VLD1 (R8), [V2.B16]
	VTBL V2.B16, [V1.B16], V1.B16

	CMPW $1, R9
	BEQ filter_high_store1
	CMPW $2, R9
	BEQ filter_high_store2
	CMPW $3, R9
	BEQ filter_high_store3
	VST1 [V1.S4], (R2)
	ADD $16, R2
	B filter_next

filter_high_store1:
	VMOV V1.S[0], R8
	MOVW R8, (R2)
	ADD $4, R2
	B filter_next

filter_high_store2:
	VMOV V1.S[0], R8
	MOVW R8, (R2)
	VMOV V1.S[1], R8
	MOVW R8, 4(R2)
	ADD $8, R2
	B filter_next

filter_high_store3:
	VMOV V1.S[0], R8
	MOVW R8, (R2)
	VMOV V1.S[1], R8
	MOVW R8, 4(R2)
	VMOV V1.S[2], R8
	MOVW R8, 8(R2)
	ADD $12, R2
	B filter_next

filter_all:
	VLD1 (R0), [V0.B16]
	ADD $16, R0, R8
	VLD1 (R8), [V1.B16]
	VST1 [V0.B16], (R2)
	ADD $16, R2
	VST1 [V1.B16], (R2)
	ADD $16, R2

filter_next:
	ADD $32, R0
	SUBS $1, R4, R4
	BNE filter_loop
	RET
