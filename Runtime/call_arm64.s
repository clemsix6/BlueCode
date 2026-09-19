#include "textflag.h"

// func call(entry, args, results, trace, instance, gas, stack uintptr) int64
//
// The Go stack pointer is kept in R20, one of the registers the C convention makes the callee
// preserve (x19-x28). R28 holds g and survives the call for the same reason. The assembler
// saves LR for us since the function is not a leaf.
TEXT ·call(SB), NOSPLIT, $0-64
	MOVD entry+0(FP), R9
	MOVD args+8(FP), R0
	MOVD results+16(FP), R1
	MOVD trace+24(FP), R2
	MOVD instance+32(FP), R3
	MOVD gas+40(FP), R4
	MOVD stack+48(FP), R10
	MOVD RSP, R20
	MOVD R10, RSP
	BL   (R9)
	MOVD R20, RSP
	MOVD R0, ret+56(FP)
	RET

// func flushInstructionCache(start, end uintptr)
//
// The sequence the architecture prescribes: clean every data cache line to the point of
// unification, then invalidate every instruction cache line, with barriers. The step is the
// smallest line size the architecture allows, 16 bytes, since CTR_EL0 is not readable from
// user space on every OS; larger lines are merely visited more than once.
TEXT ·flushInstructionCache(SB), NOSPLIT, $0-16
	MOVD start+0(FP), R0
	MOVD end+8(FP), R2
	AND  $~15, R0, R1
clean:
	WORD $0xd50b7b21           // DC CVAU, R1
	ADD  $16, R1, R1
	CMP  R2, R1
	BLT  clean
	WORD $0xd5033b9f           // DSB ISH
	AND  $~15, R0, R1
invalidate:
	WORD $0xd50b7521           // IC IVAU, R1
	ADD  $16, R1, R1
	CMP  R2, R1
	BLT  invalidate
	WORD $0xd5033b9f           // DSB ISH
	WORD $0xd5033fdf           // ISB
	RET
