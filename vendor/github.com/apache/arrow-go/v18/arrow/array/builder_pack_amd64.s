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

//go:build amd64 && !noasm && !appengine
// +build amd64,!noasm,!appengine

#include "textflag.h"

// func _packBoolsAVX2(values, dst unsafe.Pointer, length int)
TEXT ·_packBoolsAVX2(SB), NOSPLIT, $0-24
	MOVQ values+0(FP), DI
	MOVQ dst+8(FP), SI
	MOVQ length+16(FP), CX

	// Y1 is zero. The input is a []bool, so comparing it with zero
	// and inverting the result produces one all-ones byte per true value.
	VPXOR Y1, Y1, Y1

loop:
	CMPQ CX, $32
	JB done

	VMOVDQU (DI), Y0
	VPCMPEQB Y1, Y0, Y2
	VPCMPEQB Y1, Y1, Y3
	VPXOR Y2, Y3, Y2
	VPMOVMSKB Y2, DX
	MOVL DX, (SI)

	ADDQ $32, DI
	ADDQ $4, SI
	SUBQ $32, CX
	JMP loop

done:
	VZEROUPPER
	RET
