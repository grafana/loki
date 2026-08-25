#include "textflag.h"

// func callStrtod(fn uintptr, s uintptr, p uintptr) float64
TEXT ·callStrtod(SB), NOSPLIT, $0
	// 1. Initialize FPU
	// This ensures the x87 stack is empty (Tag Word = FFFF).
	// Without this, garbage on the stack causes the result push to overflow -> NaN.
	FINIT

	// 2. Load arguments from Go stack
	MOVL	fn+0(FP), AX	// Function pointer
	MOVL	s+4(FP), CX	// String pointer
	MOVL	p+8(FP), DX	// Endptr pointer

	// 3. Setup C stack for __cdecl
	SUBL	$8, SP
	MOVL	DX, 4(SP)	// Push endptr
	MOVL	CX, 0(SP)	// Push str

	// 4. Call the C function
	CALL	AX

	// 5. Clean up stack
	ADDL	$8, SP

	// 6. Store FPU result (ST0) into Go return slot
	FMOVD	F0, ret+12(FP)
	
	RET	
