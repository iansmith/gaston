// setjmp_arm64.s — setjmp/longjmp for ARM64 (Plan 9 assembly)
//
// Compatible with the picolibc AArch64 jmp_buf definition:
//   #define _JBLEN  22
//   #define _JBTYPE long long
//
// Layout (8-byte slots within jmp_buf[22]):
//   slot  0 (offset   0): X19     slot  1 (offset   8): X20
//   slot  2 (offset  16): X21     slot  3 (offset  24): X22
//   slot  4 (offset  32): X23     slot  5 (offset  40): X24
//   slot  6 (offset  48): X25     slot  7 (offset  56): X26
//   slot  8 (offset  64): X27     slot  9 (offset  72): X28
//   slot 10 (offset  80): X29/FP  slot 11 (offset  88): X30/LR
//   slot 12 (offset  96): SP
//   slots 13–21: reserved (d8–d15 callee-saved FP regs, not saved here)
//
// Note: gaston-compiled code does not keep live values in D8–D15 across
// function calls, so omitting FP callee-saved registers is safe.
//
// To assemble to a Linux ARM64 ELF object for inclusion in libgastonc.a:
//   GOOS=linux GOARCH=arm64 go tool asm \
//       -p setjmp -I $(go env GOROOT)/src/runtime \
//       -o setjmp.o setjmp_arm64.s

#include "textflag.h"

// int setjmp(jmp_buf env)
//
// Saves all callee-saved general-purpose registers, the frame pointer (X29),
// the link register (X30, which is the return address back to the caller of
// setjmp), and the stack pointer to env[].  Returns 0.
//
// When longjmp restores this state the CPU "returns" to the instruction after
// the setjmp call with the value passed to longjmp (never 0).
//
// ARM64 calling convention: R0 = first argument (pointer to jmp_buf).
TEXT setjmp(SB),NOSPLIT,$0
	STP	(R19, R20), 0(R0)
	STP	(R21, R22), 16(R0)
	STP	(R23, R24), 32(R0)
	STP	(R25, R26), 48(R0)
	STP	(R27, R28), 64(R0)
	STP	(R29, R30), 80(R0)	// X29 = FP, X30 = LR (caller's return address)
	MOVD	RSP, R1
	MOVD	R1, 96(R0)		// save SP
	MOVD	$0, R0			// return 0
	RET

// void longjmp(jmp_buf env, int val)
//
// Restores the state saved by a previous setjmp call and transfers control
// back to the instruction following that setjmp.  The setjmp call appears to
// return val; if val is 0 it is replaced by 1 (setjmp must not return 0 on
// the longjmp path).
//
// ARM64 calling convention: R0 = env pointer, R1 = val.
TEXT longjmp(SB),NOSPLIT,$0
	MOVD	R0, R3			// save env ptr — R0 is clobbered by LDP below
	MOVD	R1, R2			// save val — R1 is clobbered by SP restore
	LDP	0(R3), (R19, R20)
	LDP	16(R3), (R21, R22)
	LDP	32(R3), (R23, R24)
	LDP	48(R3), (R25, R26)
	LDP	64(R3), (R27, R28)
	LDP	80(R3), (R29, R30)	// restore FP and LR (= saved return address)
	MOVD	96(R3), R1
	MOVD	R1, RSP			// restore SP — must happen after LDP restores
	CBNZ	R2, setjmp_done		// if val != 0, use it as-is
	MOVD	$1, R2			// val == 0: substitute 1
setjmp_done:
	MOVD	R2, R0			// return val
	RET
