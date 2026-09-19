#include "textflag.h"

// func call(entry, args, results, trace, instance, gas, stack uintptr) int64
//
// System V convention: arguments in DI, SI, DX, CX, R8, result in AX. The Go stack pointer is
// kept in R12, which the callee preserves along with BX, BP and R13-R15; R14 holds g and
// survives the same way. The stack top is 16-byte aligned, so CALL leaves it as the callee expects.
TEXT ·call(SB), NOSPLIT, $0-64
	MOVQ entry+0(FP), AX
	MOVQ args+8(FP), DI
	MOVQ results+16(FP), SI
	MOVQ trace+24(FP), DX
	MOVQ instance+32(FP), CX
	MOVQ gas+40(FP), R8
	MOVQ stack+48(FP), R10
	MOVQ SP, R12
	MOVQ R10, SP
	CALL AX
	MOVQ R12, SP
	MOVQ AX, ret+56(FP)
	RET

// func flushInstructionCache(start, end uintptr)
//
// x86 keeps instruction fetch coherent with stores: nothing to do.
TEXT ·flushInstructionCache(SB), NOSPLIT, $0-16
	RET
