// arm64enc.go — ARM64 instruction encoder for gaston's ELF code generator.
// Each function encodes one instruction form and returns the 32-bit word.
// Instructions are stored little-endian (encoding/binary handles the byte swap).
package main

// ARM64 register numbers.
const (
	regX0  = 0
	regX1  = 1
	regX2  = 2
	regX3  = 3
	regX4  = 4
	regX5  = 5
	regX8  = 8  // Linux syscall number register
	regX9  = 9  // scratch for global address pool loads
	regX19 = 19 // callee-saved (used inside helper functions)
	regX20 = 20
	regX21 = 21
	regFP  = 29 // X29 — frame pointer
	regLR  = 30 // X30 — link register
	regXZR = 31 // zero register (source / result-discard context)
	regSP  = 31 // stack pointer (load/store base context)
)

// ARM64 condition codes for B.cond and CSET.
const (
	condEQ = 0  // equal         (Z=1)
	condNE = 1  // not equal     (Z=0)
	condGE = 10 // signed ≥      (N=V)
	condLT = 11 // signed <      (N≠V)
	condGT = 12 // signed >      (Z=0, N=V)
	condLE = 13 // signed ≤      (Z=1 or N≠V)
)

// encMOVZ encodes MOVZ Xd, #imm16, LSL #shift.
// shift must be 0, 16, 32, or 48.
func encMOVZ(rd, imm16, shift int) uint32 {
	hw := shift / 16
	return 0xD2800000 | uint32(hw)<<21 | uint32(imm16&0xFFFF)<<5 | uint32(rd)
}

// encMOVK encodes MOVK Xd, #imm16, LSL #shift.
func encMOVK(rd, imm16, shift int) uint32 {
	hw := shift / 16
	return 0xF2800000 | uint32(hw)<<21 | uint32(imm16&0xFFFF)<<5 | uint32(rd)
}

// encLDRuoff encodes LDR Xt, [Xn, #byteOff] (unsigned 12-bit scaled offset).
// byteOff must be 0..32760 and divisible by 8.
func encLDRuoff(rt, rn, byteOff int) uint32 {
	return 0xF9400000 | uint32(byteOff/8)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encSTRuoff encodes STR Xt, [Xn, #byteOff] (unsigned 12-bit scaled offset).
func encSTRuoff(rt, rn, byteOff int) uint32 {
	return 0xF9000000 | uint32(byteOff/8)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encLDRlit encodes LDR Xt, [PC, #(imm19*4)] (PC-relative literal load, 64-bit).
// imm19 is the signed word offset from the instruction to the 8-byte-aligned literal.
func encLDRlit(rt, imm19 int) uint32 {
	return 0x58000000 | uint32(imm19&0x7FFFF)<<5 | uint32(rt)
}

// encADDreg encodes ADD Xd, Xn, Xm (shifted register, LSL #0).
func encADDreg(rd, rn, rm int) uint32 {
	return 0x8B000000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encSUBreg encodes SUB Xd, Xn, Xm (shifted register, LSL #0).
func encSUBreg(rd, rn, rm int) uint32 {
	return 0xCB000000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encMUL encodes MUL Xd, Xn, Xm (= MADD Xd, Xn, Xm, XZR).
func encMUL(rd, rn, rm int) uint32 {
	return 0x9B000000 | uint32(rm)<<16 | uint32(regXZR)<<10 | uint32(rn)<<5 | uint32(rd)
}

// encSDIV encodes SDIV Xd, Xn, Xm (signed divide, Xd = Xn / Xm).
func encSDIV(rd, rn, rm int) uint32 {
	return 0x9AC00C00 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encUDIV encodes UDIV Xd, Xn, Xm (unsigned divide).
func encUDIV(rd, rn, rm int) uint32 {
	return 0x9AC00800 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encCMPreg encodes CMP Xn, Xm (= SUBS XZR, Xn, Xm; sets flags for Xn−Xm).
func encCMPreg(rn, rm int) uint32 {
	return 0xEB00001F | uint32(rm)<<16 | uint32(rn)<<5
}

// encCMPimm0 encodes CMP Xn, #0 (= SUBS XZR, Xn, #0).
func encCMPimm0(rn int) uint32 {
	return 0xF100001F | uint32(rn)<<5
}

// encLSLimm encodes LSL Xd, Xn, #shift (= UBFM Xd, Xn, #(64-shift), #(63-shift)).
func encLSLimm(rd, rn, shift int) uint32 {
	immr := (64 - shift) & 63
	imms := 63 - shift
	return 0xD3400000 | uint32(immr)<<16 | uint32(imms)<<10 | uint32(rn)<<5 | uint32(rd)
}

// encB encodes B label (unconditional branch). imm26 is signed word offset.
func encB(imm26 int) uint32 {
	return 0x14000000 | uint32(imm26)&0x3FFFFFF
}

// encBL encodes BL label (branch with link). imm26 is signed word offset.
func encBL(imm26 int) uint32 {
	return 0x94000000 | uint32(imm26)&0x3FFFFFF
}

// encBcond encodes B.cond label. imm19 is signed word offset, cond is a cond* constant.
func encBcond(cond, imm19 int) uint32 {
	return 0x54000000 | uint32(imm19&0x7FFFF)<<5 | uint32(cond)
}

// encRET encodes RET (return via LR = X30).
func encRET() uint32 {
	return 0xD65F03C0
}

// encCSET encodes CSET Xd, cond (= CSINC Xd, XZR, XZR, NOT(cond)).
// Xd = 1 if condition is true, else 0.
func encCSET(rd, cond int) uint32 {
	// For all ARM64 condition pairs used here, NOT(cond) = cond ^ 1.
	invCond := cond ^ 1
	return 0x9A9F07E0 | uint32(invCond)<<12 | uint32(rd)
}

// encSTP encodes STP Xt1, Xt2, [Xn, #byteOff] (signed 7-bit scaled offset, ±512).
// byteOff must be divisible by 8.
func encSTP(rt1, rt2, rn, byteOff int) uint32 {
	imm7 := (byteOff / 8) & 0x7F
	return 0xA9000000 | uint32(imm7)<<15 | uint32(rt2)<<10 | uint32(rn)<<5 | uint32(rt1)
}

// encLDP encodes LDP Xt1, Xt2, [Xn, #byteOff].
func encLDP(rt1, rt2, rn, byteOff int) uint32 {
	imm7 := (byteOff / 8) & 0x7F
	return 0xA9400000 | uint32(imm7)<<15 | uint32(rt2)<<10 | uint32(rn)<<5 | uint32(rt1)
}

// encSVC encodes SVC #imm16.
func encSVC(imm16 int) uint32 {
	return 0xD4000001 | uint32(imm16)<<5
}

// encSTRBuoff encodes STRB Wt, [Xn, #byteOff] (unsigned 12-bit byte offset).
func encSTRBuoff(rt, rn, byteOff int) uint32 {
	return 0x39000000 | uint32(byteOff)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encLDRBuoff encodes LDRB Wt, [Xn, #byteOff] (unsigned 12-bit byte offset).
func encLDRBuoff(rt, rn, byteOff int) uint32 {
	return 0x39400000 | uint32(byteOff)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encCBZ encodes CBZ Xt, label. imm19 is signed word offset.
func encCBZ(rt, imm19 int) uint32 {
	return 0xB4000000 | uint32(imm19&0x7FFFF)<<5 | uint32(rt)
}

// encCBNZ encodes CBNZ Xt, label. imm19 is signed word offset.
func encCBNZ(rt, imm19 int) uint32 {
	return 0xB5000000 | uint32(imm19&0x7FFFF)<<5 | uint32(rt)
}

// encNEG encodes NEG Xd, Xn (= SUB Xd, XZR, Xn).
func encNEG(rd, rn int) uint32 {
	return 0xCB0003E0 | uint32(rn)<<16 | uint32(rd)
}

// encMOVreg encodes MOV Xd, Xn (= ORR Xd, XZR, Xn, LSL #0).
func encMOVreg(rd, rn int) uint32 {
	return 0xAA0003E0 | uint32(rn)<<16 | uint32(rd)
}

// encADDimm encodes ADD Xd, Xn, #imm12 (no shift, imm12 ∈ [0, 4095]).
// When Xn = SP (31), this uses SP as source; when Xd = SP (31), sets SP.
func encADDimm(rd, rn, imm12 int) uint32 {
	return 0x91000000 | uint32(imm12)<<10 | uint32(rn)<<5 | uint32(rd)
}

// encSUBimm encodes SUB Xd, Xn, #imm12 (no shift).
func encSUBimm(rd, rn, imm12 int) uint32 {
	return 0xD1000000 | uint32(imm12)<<10 | uint32(rn)<<5 | uint32(rd)
}
