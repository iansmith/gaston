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
	regX16 = 16 // IP0 — intra-procedure scratch (used for indirect calls)
	regX19 = 19 // callee-saved (used inside helper functions)
	regX20 = 20
	regX21 = 21
	regX22 = 22
	regX23 = 23
	regX24 = 24
	regFP  = 29 // X29 — frame pointer
	regLR  = 30 // X30 — link register
	regXZR = 31 // zero register (source / result-discard context)
	regSP  = 31 // stack pointer (load/store base context)
)

// ARM64 condition codes for B.cond and CSET.
const (
	condEQ = 0  // equal              (Z=1)
	condNE = 1  // not equal          (Z=0)
	condHS = 2  // unsigned ≥  (CS)   (C=1)
	condLO = 3  // unsigned <  (CC)   (C=0)
	condVS = 6  // overflow set (V=1)  — unordered after FCMP (NaN)
	condVC = 7  // overflow clear (V=0) — ordered after FCMP (not NaN)
	condHI = 8  // unsigned >          (C=1, Z=0)
	condLS = 9  // unsigned ≤          (C=0 or Z=1)
	condGE = 10 // signed ≥           (N=V)
	condLT = 11 // signed <            (N≠V)
	condGT = 12 // signed >            (Z=0, N=V)
	condLE = 13 // signed ≤            (Z=1 or N≠V)
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
// NOTE: when Rd=31, this encodes XZR (not SP). Do NOT use for SP arithmetic.
func encSUBreg(rd, rn, rm int) uint32 {
	return 0xCB000000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encSUBext encodes SUB Xd, Xn, Xm, UXTX (extended-register form).
// Unlike encSUBreg, Rd=31 targets SP (not XZR), making this correct for SP arithmetic.
func encSUBext(rd, rn, rm int) uint32 {
	return 0xCB200000 | uint32(rm)<<16 | (3 << 13) | uint32(rn)<<5 | uint32(rd)
}

// encMUL encodes MUL Xd, Xn, Xm (= MADD Xd, Xn, Xm, XZR).
func encMUL(rd, rn, rm int) uint32 {
	return 0x9B000000 | uint32(rm)<<16 | uint32(regXZR)<<10 | uint32(rn)<<5 | uint32(rd)
}

// encMSUB encodes MSUB Xd, Xn, Xm, Xa (Xd = Xa - Xn*Xm).
func encMSUB(rd, rn, rm, ra int) uint32 {
	return 0x9B008000 | uint32(rm)<<16 | uint32(ra)<<10 | uint32(rn)<<5 | uint32(rd)
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

// encBLR encodes BLR Xn (branch with link to register). rn is the register index (0–30).
func encBLR(rn int) uint32 {
	return 0xD63F0000 | uint32(rn)<<5
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

// encLDRBuoff encodes LDRB Wt, [Xn, #byteOff] (unsigned 12-bit byte offset; zero-extends to 64 bits).
func encLDRBuoff(rt, rn, byteOff int) uint32 {
	return 0x39400000 | uint32(byteOff)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encLDRSBuoff encodes LDRSB Xt, [Xn, #byteOff] (unsigned 12-bit byte offset; sign-extends to 64 bits).
func encLDRSBuoff(rt, rn, byteOff int) uint32 {
	return 0x39800000 | uint32(byteOff)<<10 | uint32(rn)<<5 | uint32(rt)
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

// encAND encodes AND Xd, Xn, Xm (shifted register, LSL #0).
func encAND(rd, rn, rm int) uint32 {
	return 0x8A000000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encORR encodes ORR Xd, Xn, Xm (shifted register, LSL #0).
func encORR(rd, rn, rm int) uint32 {
	return 0xAA000000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encEOR encodes EOR Xd, Xn, Xm (shifted register, LSL #0).
func encEOR(rd, rn, rm int) uint32 {
	return 0xCA000000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encMVN encodes MVN Xd, Xm (= ORN Xd, XZR, Xm; bitwise NOT).
func encMVN(rd, rm int) uint32 {
	return 0xAA2003E0 | uint32(rm)<<16 | uint32(rd)
}

// encLSLV encodes LSL Xd, Xn, Xm (variable left shift).
func encLSLV(rd, rn, rm int) uint32 {
	return 0x9AC02000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encASRV encodes ASR Xd, Xn, Xm (arithmetic right shift, variable).
func encASRV(rd, rn, rm int) uint32 {
	return 0x9AC02800 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encMOVN encodes MOVN Xd, #imm16, LSL #shift (move NOT of immediate).
// Result = ~(imm16 << (hw*16)). MOVN Xd, #0 → Xd = -1.
func encMOVN(rd, imm16, shift int) uint32 {
	hw := shift / 16
	return 0x92800000 | uint32(hw)<<21 | uint32(imm16&0xFFFF)<<5 | uint32(rd)
}

// encADR encodes ADR Xd, #byteOff (PC-relative address, ±1 MB).
// byteOff is the signed byte offset from the ADR instruction to the target.
// The target must be 4-byte aligned (so byteOff is always a multiple of 4
// when pointing at instruction-stream labels, making immlo always 0).
func encADR(rd, byteOff int) uint32 {
	immlo := byteOff & 3
	immhi := (byteOff >> 2) & 0x7FFFF
	return 0x10000000 | uint32(immlo)<<29 | uint32(immhi)<<5 | uint32(rd)
}

// encLSRimm encodes LSR Xd, Xn, #shift (= UBFM Xd, Xn, #shift, #63).
func encLSRimm(rd, rn, shift int) uint32 {
	return 0xD3400000 | uint32(shift)<<16 | uint32(63)<<10 | uint32(rn)<<5 | uint32(rd)
}

// encLSRV encodes LSR Xd, Xn, Xm (logical shift right, variable; unsigned >>).
func encLSRV(rd, rn, rm int) uint32 {
	return 0x9AC02400 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encSTRWuoff encodes STR Wt, [Xn, #byteOff] (unsigned 12-bit word offset; stores 32 bits).
func encSTRWuoff(rt, rn, byteOff int) uint32 {
	return 0xB9000000 | uint32(byteOff/4)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encLDRWuoff encodes LDR Wt, [Xn, #byteOff] (unsigned 12-bit word offset; zero-extends 32→64).
func encLDRWuoff(rt, rn, byteOff int) uint32 {
	return 0xB9400000 | uint32(byteOff/4)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encLDRSWuoff encodes LDRSW Xt, [Xn, #byteOff] (unsigned 12-bit word offset; sign-extends 32→64).
func encLDRSWuoff(rt, rn, byteOff int) uint32 {
	return 0xB9800000 | uint32(byteOff/4)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encSTRH encodes STRH Wt, [Xn, #byteOff] (unsigned 12-bit halfword offset).
func encSTRH(rt, rn, byteOff int) uint32 {
	return 0x79000000 | uint32(byteOff/2)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encLDRH encodes LDRH Wt, [Xn, #byteOff] (unsigned 12-bit halfword offset; zero-extends).
func encLDRH(rt, rn, byteOff int) uint32 {
	return 0x79400000 | uint32(byteOff/2)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encLDRSH encodes LDRSH Xt, [Xn, #byteOff] (unsigned 12-bit halfword offset; sign-extends to 64 bits).
func encLDRSH(rt, rn, byteOff int) uint32 {
	return 0x79800000 | uint32(byteOff/2)<<10 | uint32(rn)<<5 | uint32(rt)
}

// ── floating-point instruction encoders (double precision, scalar) ─────────────

// encFADDD encodes FADD Dd, Dn, Dm (64-bit double precision addition).
func encFADDD(rd, rn, rm int) uint32 {
	return 0x1E602800 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encFSUBD encodes FSUB Dd, Dn, Dm.
func encFSUBD(rd, rn, rm int) uint32 {
	return 0x1E603800 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encFMULD encodes FMUL Dd, Dn, Dm.
func encFMULD(rd, rn, rm int) uint32 {
	return 0x1E600800 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encFDIVD encodes FDIV Dd, Dn, Dm.
func encFDIVD(rd, rn, rm int) uint32 {
	return 0x1E601800 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encFNEGD encodes FNEG Dd, Dn (negate).
func encFNEGD(rd, rn int) uint32 {
	return 0x1E614000 | uint32(rn)<<5 | uint32(rd)
}

// encFABSD encodes FABS Dd, Dn (absolute value).
func encFABSD(rd, rn int) uint32 {
	return 0x1E60C000 | uint32(rn)<<5 | uint32(rd)
}

// encFMOVDD encodes FMOV Dd, Dn (FP register copy).
func encFMOVDD(rd, rn int) uint32 {
	return 0x1E604000 | uint32(rn)<<5 | uint32(rd)
}

// encFCMPD encodes FCMP Dn, Dm (sets flags; V=1 if either is NaN).
func encFCMPD(rn, rm int) uint32 {
	return 0x1E602000 | uint32(rm)<<16 | uint32(rn)<<5
}

// encFCMPDzero encodes FCMP Dn, #0.0.
func encFCMPDzero(rn int) uint32 {
	return 0x1E602008 | uint32(rn)<<5
}

// encSCVTFD encodes SCVTF Dd, Xn (signed int64 → double).
func encSCVTFD(rd, rn int) uint32 {
	return 0x9E620000 | uint32(rn)<<5 | uint32(rd)
}

// encFCVTZSD encodes FCVTZS Xd, Dn (double → signed int64, truncate toward zero).
func encFCVTZSD(rd, rn int) uint32 {
	return 0x9E780000 | uint32(rn)<<5 | uint32(rd)
}

// encFMOVtoGP encodes FMOV Xd, Dn (move from FP register to GP register).
func encFMOVtoGP(rd, rn int) uint32 {
	return 0x9E660000 | uint32(rn)<<5 | uint32(rd)
}

// encFMOVfromGP encodes FMOV Dd, Xn (move from GP register to FP register).
func encFMOVfromGP(rd, rn int) uint32 {
	return 0x9E670000 | uint32(rn)<<5 | uint32(rd)
}

// encLDRDuoff encodes LDR Dt, [Xn, #byteOff] (double, unsigned 12-bit scaled offset).
// byteOff must be 0..32760 and divisible by 8.
func encLDRDuoff(rt, rn, byteOff int) uint32 {
	return 0xFD400000 | uint32(byteOff/8)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encSTRDuoff encodes STR Dt, [Xn, #byteOff] (double, unsigned 12-bit scaled offset).
func encSTRDuoff(rt, rn, byteOff int) uint32 {
	return 0xFD000000 | uint32(byteOff/8)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encLDRSuoff encodes LDR St, [Xn, #byteOff] (single-precision float, unsigned 12-bit scaled offset).
// byteOff must be 0..16380 and divisible by 4.
func encLDRSuoff(rt, rn, byteOff int) uint32 {
	return 0xBD400000 | uint32(byteOff/4)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encSTRSuoff encodes STR St, [Xn, #byteOff] (single-precision float, unsigned 12-bit scaled offset).
func encSTRSuoff(rt, rn, byteOff int) uint32 {
	return 0xBD000000 | uint32(byteOff/4)<<10 | uint32(rn)<<5 | uint32(rt)
}

// encFCVTDS encodes FCVT Sd, Dn (double → single precision conversion).
func encFCVTDS(rd, rn int) uint32 {
	return 0x1E624000 | uint32(rn)<<5 | uint32(rd)
}

// encFCVTSD encodes FCVT Dd, Sn (single → double precision conversion).
func encFCVTSD(rd, rn int) uint32 {
	return 0x1E22C000 | uint32(rn)<<5 | uint32(rd)
}

// encLDRDlit encodes LDR Dt, [PC, #(imm19*4)] (PC-relative literal load, 64-bit double).
// imm19 is the signed word offset from the instruction to the 8-byte-aligned literal.
func encLDRDlit(rt, imm19 int) uint32 {
	return 0x5C000000 | uint32(imm19&0x7FFFF)<<5 | uint32(rt)
}

// encSXTB encodes SXTB Xd, Xn (sign-extend byte to 64-bit = SBFM Xd, Xn, #0, #7).
func encSXTB(rd, rn int) uint32 { return 0x93401C00 | uint32(rn)<<5 | uint32(rd) }

// encSXTH encodes SXTH Xd, Xn (sign-extend halfword to 64-bit = SBFM Xd, Xn, #0, #15).
func encSXTH(rd, rn int) uint32 { return 0x93403C00 | uint32(rn)<<5 | uint32(rd) }

// encUXTB encodes UXTB Xd, Xn (zero-extend byte to 64-bit = UBFM Xd, Xn, #0, #7).
func encUXTB(rd, rn int) uint32 { return 0xD3401C00 | uint32(rn)<<5 | uint32(rd) }

// encUXTH encodes UXTH Xd, Xn (zero-extend halfword to 64-bit = UBFM Xd, Xn, #0, #15).
func encUXTH(rd, rn int) uint32 { return 0xD3403C00 | uint32(rn)<<5 | uint32(rd) }

// ── bit-manipulation instruction encoders ────────────────────────────────────

// encCLZ encodes CLZ Xd, Xn (64-bit count leading zeros).
// Data Processing (1 source): sf=1, S=0, opcode2=00000, opcode=000100(4)
// 1_1_0_11010110_00000_000100_Rn_Rd = 0xDAC01000 | rn<<5 | rd
func encCLZ(rd, rn int) uint32 {
	return 0xDAC01000 | uint32(rn)<<5 | uint32(rd)
}

// encCLZ32 encodes CLZ Wd, Wn (32-bit count leading zeros).
// sf=0: 0x5AC01000 | rn<<5 | rd
func encCLZ32(rd, rn int) uint32 {
	return 0x5AC01000 | uint32(rn)<<5 | uint32(rd)
}

// encRBIT encodes RBIT Xd, Xn (64-bit reverse bits).
// opcode=000000 → 0xDAC00000 | rn<<5 | rd
func encRBIT(rd, rn int) uint32 {
	return 0xDAC00000 | uint32(rn)<<5 | uint32(rd)
}

// encRBIT32 encodes RBIT Wd, Wn (32-bit reverse bits).
func encRBIT32(rd, rn int) uint32 {
	return 0x5AC00000 | uint32(rn)<<5 | uint32(rd)
}

// encCNT8B encodes CNT V<d>.8B, V<n>.8B (count set bits per byte, 8 bytes).
// Advanced SIMD two-register misc: Q=0, U=0, size=00, opcode=00101
// 0_0_0_01110_00_10000_00101_10_Rn_Rd = 0x0E205800 | vn<<5 | vd
func encCNT8B(vd, vn int) uint32 {
	return 0x0E205800 | uint32(vn)<<5 | uint32(vd)
}

// encADDVb encodes ADDV Bd, V<n>.8B (across-lane add, byte).
// Advanced SIMD across lanes: Q=0, U=0, size=00, opcode=11011
// 0_0_0_01110_00_11000_11011_10_Rn_Rd = 0x0E31B800 | vn<<5 | vd
func encADDVb(vd, vn int) uint32 {
	return 0x0E31B800 | uint32(vn)<<5 | uint32(vd)
}

// encUMOVb0 encodes UMOV Wd, V<n>.B[0] (extract byte lane 0 to 32-bit GP reg).
// Advanced SIMD copy: Q=0, op=0, imm5=00001 (byte lane 0), imm4=0111
// Verified encoding: 0x0E013C00 | vn<<5 | rd
func encUMOVb0(rd, vn int) uint32 {
	return 0x0E013C00 | uint32(vn)<<5 | uint32(rd)
}

// ── 128-bit arithmetic encoders ───────────────────────────────────────────────

// encADDS encodes ADDS Xd, Xn, Xm (add, set flags — for 128-bit lo half).
func encADDS(rd, rn, rm int) uint32 {
	return 0xAB000000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encADC encodes ADC Xd, Xn, Xm (add with carry, no flag update — for hi half).
func encADC(rd, rn, rm int) uint32 {
	return 0x9A000000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encSUBS encodes SUBS Xd, Xn, Xm (subtract, set flags — for lo half).
func encSUBS(rd, rn, rm int) uint32 {
	return 0xEB000000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encSBC encodes SBC Xd, Xn, Xm (subtract with carry, no flag update — for hi half).
func encSBC(rd, rn, rm int) uint32 {
	return 0xDA000000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encUMULH encodes UMULH Xd, Xn, Xm (unsigned multiply high — hi 64 bits of 64×64).
func encUMULH(rd, rn, rm int) uint32 {
	return 0x9BC07C00 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encSMULH encodes SMULH Xd, Xn, Xm (signed multiply high — hi 64 bits of 64×64).
func encSMULH(rd, rn, rm int) uint32 {
	return 0x9B407C00 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encNEGS encodes NEGS Xd, Xm (negate and set flags): SUBS Xd, XZR, Xm.
func encNEGS(rd, rm int) uint32 {
	return 0xEB0003E0 | uint32(rm)<<16 | uint32(rd)
}

// encNGC encodes NGC Xd, Xm (negate with carry): SBC Xd, XZR, Xm.
func encNGC(rd, rm int) uint32 {
	return 0xDA0003E0 | uint32(rm)<<16 | uint32(rd)
}

// encUBFM encodes UBFM Xd, Xn, #immr, #imms (unsigned bitfield move).
// LSL Xd, Xn, #n = UBFM Xd, Xn, #(64-n), #(63-n)
// LSR Xd, Xn, #n = UBFM Xd, Xn, #n, #63
func encUBFM(rd, rn, immr, imms int) uint32 {
	return 0xD3400000 | uint32(immr)<<16 | uint32(imms)<<10 | uint32(rn)<<5 | uint32(rd)
}

// encASR encodes ASR Xd, Xn, #n (arithmetic shift right by immediate).
// ASR Xd, Xn, #n = SBFM Xd, Xn, #n, #63
func encASR(rd, rn, n int) uint32 {
	return 0x9340FC00 | uint32(n)<<16 | uint32(rn)<<5 | uint32(rd)
}

// encCCMP encodes CCMP Xn, Xm, #nzcv, cond (conditional compare registers).
// If cond is true: flags = Xn - Xm; else flags = nzcv.
func encCCMP(rn, rm, nzcv, cond int) uint32 {
	return 0xFA400000 | uint32(rm)<<16 | uint32(cond)<<12 | uint32(rn)<<5 | uint32(nzcv)
}

// encSBCS encodes SBCS Xd, Xn, Xm (subtract with carry, set flags).
// 1 1 1 11010 000 Rm 000000 Rn Rd → 0xFA000000 | rm<<16 | rn<<5 | rd
func encSBCS(rd, rn, rm int) uint32 {
	return 0xFA000000 | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
}
