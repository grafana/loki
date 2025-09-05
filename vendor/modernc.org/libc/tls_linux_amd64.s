#include "funcdata.h"
#include "textflag.h"

TEXT ·TLSAlloc(SB),$24-24
	GO_ARGS
	NO_LOCAL_POINTERS
	MOVQ p0+0(FP), AX
	MOVQ AX, 0(SP)
	MOVQ p1+8(FP), AX
	MOVQ AX, 8(SP)
	CALL ·tlsAlloc(SB)
	MOVQ 16(SP), AX
	MOVQ AX, ret+16(FP)
	RET

TEXT ·TLSFree(SB),$16-16
	GO_ARGS
	NO_LOCAL_POINTERS
	MOVQ p0+0(FP), AX
	MOVQ AX, 0(SP)
	MOVQ p1+8(FP), AX
	MOVQ AX, 8(SP)
	CALL ·tlsFree(SB)
	RET
