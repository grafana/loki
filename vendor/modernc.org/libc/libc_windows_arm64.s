#include "textflag.h"

// func callStrtod(fn uintptr, s uintptr, p uintptr) float64
TEXT ·callStrtod(SB), NOSPLIT, $0
	// 1. Load arguments from Go stack
	// Go passes args on stack for ABI0 (assembly functions).
	MOVD	fn+0(FP), R2	// Function address (use R2 as temp)
	MOVD	s+8(FP), R0	// Arg 1: str -> x0 (R0 in Go ASM)
	MOVD	p+16(FP), R1	// Arg 2: endptr -> x1 (R1 in Go ASM)

	// 2. Call the C function
	// BL (Branch with Link) to the address in R2
	CALL	R2

	// 3. Handle Return Value
	// C returns double in d0 (F0 in Go ASM).
	// Go expects return value at ret+24(FP).
	FMOVD	F0, ret+24(FP)
	
	RET
