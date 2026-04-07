// elfgen.go — direct Linux ARM64 ELF binary generator for gaston.
//
// ELF layout:
//   File offset 0x0000: ELF64 header (debug/elf.Header64, 64 bytes)
//   File offset 0x0040: PT_LOAD code phdr (debug/elf.Prog64, 56 bytes)
//   File offset 0x0078: PT_LOAD BSS  phdr (debug/elf.Prog64, 56 bytes)
//   File offset 0x00B0: literal pool  (numGlobals × 8 bytes, filled last)
//   File offset 0x00B0+poolBytes: _start, gaston_output, gaston_input, user functions
//
// Virtual addresses:
//   0x400000 + 0x00B0 = 0x4000B0  — codeBase (first instruction)
//   nextPage(codeBase + codeBytes) — bssBase  (zero-initialized globals)
//
// The literal pool is at the front of the code section; LDR literal instructions
// reference it with negative PC-relative offsets. Pool entries are patched with
// global variable virtual addresses after the BSS layout is determined.
//
// ELF structs (debug/elf.Header64, debug/elf.Prog64) are serialised with
// encoding/binary as required by the stdlib-only constraint.
package main

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

// ── ELF layout constants ───────────────────────────────────────────────────────

const (
	elfLoadBase  = uint64(0x400000) // standard Linux ARM64 load address
	elfHdrSize   = 64               // sizeof(elf.Header64)
	elfPhdrSize  = 56               // sizeof(elf.Prog64)
	elfNumPhdrs  = 3                // code + rodata + BSS
	elfHeaderEnd = elfHdrSize + elfNumPhdrs*elfPhdrSize // 0xC8
	codeBase     = elfLoadBase + elfHeaderEnd           // 0x4000C8
	pageSize     = uint64(4096)
)

func nextPage(addr uint64) uint64 { return (addr + pageSize - 1) &^ (pageSize - 1) }

// ── branch fixup ──────────────────────────────────────────────────────────────

type fixupKind int

const (
	fixB     fixupKind = iota // unconditional branch (26-bit imm)
	fixBL                     // branch-link (26-bit imm)
	fixBcond                  // conditional branch (19-bit imm in [23:5])
	fixCBZ                    // CBZ  (19-bit imm in [23:5])
	fixCBNZ                   // CBNZ (19-bit imm in [23:5])
	fixADR                    // ADR  (21-bit byte offset, immlo in [30:29] + immhi in [23:5])
)

type branchFixup struct {
	at    int       // instruction index holding the placeholder
	label string    // target label name
	kind  fixupKind
}

// ── codeBuilder ───────────────────────────────────────────────────────────────

// externBLRecord records a BL instruction that references an external symbol
// (used in ET_REL object file mode to generate CALL26 relocations).
type externBLRecord struct {
	at  int    // instruction index
	sym string // symbol name (e.g. "gaston_input")
}

// codeBuilder accumulates ARM64 machine-code words, label locations, and
// branch fixup requests. The literal pool occupies the first poolCount*2 slots
// of instrs (two uint32 words per 8-byte address). The pool holds both global
// variable VAs and string literal VAs.
type codeBuilder struct {
	instrs          []uint32
	labels          map[string]int // label name → instruction index
	fixups          []branchFixup
	poolIdx         map[string]int   // name/label → pool entry index (0-based)
	externBLs       []externBLRecord // extern BL calls (for ET_REL mode only)
	peepholeBarrier bool             // set by defineLabel; cleared by emit
}

func newCodeBuilder(globals []IRGlobal, strLits []IRStrLit, fconsts []IRFConst, funcRefs []string) *codeBuilder {
	cb := &codeBuilder{
		labels:  make(map[string]int),
		poolIdx: make(map[string]int),
	}
	i := 0
	for _, g := range globals {
		cb.poolIdx[g.Name] = i
		i++
	}
	for _, s := range strLits {
		cb.poolIdx[s.Label] = i
		i++
	}
	for _, fc := range fconsts {
		cb.poolIdx[fc.Label] = i
		i++
	}
	// Add pool entries for function address references (IRFuncAddr).
	for _, fn := range funcRefs {
		label := funcLabel(fn)
		if _, already := cb.poolIdx[label]; !already {
			cb.poolIdx[label] = i
			i++
		}
	}
	// Reserve space for the literal pool: 2 uint32 words per 8-byte value.
	for j := 0; j < i; j++ {
		cb.instrs = append(cb.instrs, 0, 0)
	}
	// Pre-fill FP constant slots with IEEE 754 bit patterns.
	// patchPool will not touch these (their labels are not in globalAddrs).
	for _, fc := range fconsts {
		k := cb.poolIdx[fc.Label]
		bits := math.Float64bits(fc.Value)
		cb.instrs[k*2] = uint32(bits)
		cb.instrs[k*2+1] = uint32(bits >> 32)
	}
	return cb
}

// emit appends one instruction word and clears the peephole barrier.
func (cb *codeBuilder) emit(w uint32) {
	cb.instrs = append(cb.instrs, w)
	cb.peepholeBarrier = false
}

// emitMOVimm emits the minimum MOVZ/MOVK sequence to load a 64-bit immediate.
func (cb *codeBuilder) emitMOVimm(rd int, val int64) {
	uval := uint64(val)
	first := true
	for sh := 0; sh < 64; sh += 16 {
		chunk := int((uval >> uint(sh)) & 0xFFFF)
		if chunk == 0 && !first {
			continue
		}
		if first {
			cb.emit(encMOVZ(rd, chunk, sh))
			first = false
		} else {
			cb.emit(encMOVK(rd, chunk, sh))
		}
	}
	if first {
		cb.emit(encMOVZ(rd, 0, 0)) // val == 0
	}
}

// ── peephole optimizer ────────────────────────────────────────────────────────

// peepholeElim is the store-then-load peephole optimizer.
// It is called before emitting any SP-relative LDR instruction. If the
// immediately preceding instruction is the STR counterpart of the LDR
// (same register, same SP-relative offset), the load is redundant — the
// value is already in the target register — and is dropped.
//
// STR and LDR to SP differ only in bit 22 (opc field):
//   STR: 0xF9000000 | …   (bit 22 = 0)
//   LDR: 0xF9400000 | …   (bit 22 = 1)
// So: the preceding STR encodes as ldrEnc ^ (1<<22).
//
// IMPORTANT: this optimization is only valid for straight-line code.
// peepholeBarrier is set by defineLabel to prevent elimination across
// a label definition (which may be a back-edge target for a loop).
func (cb *codeBuilder) peepholeElim(ldrEnc uint32) bool {
	if len(cb.instrs) == 0 || cb.peepholeBarrier {
		return false
	}
	prev := cb.instrs[len(cb.instrs)-1]
	return prev == ldrEnc^(1<<22)
}

// emitLDRsp emits LDR Xrt, [SP, #byteOff], subject to peephole elimination.
func (cb *codeBuilder) emitLDRsp(rt, byteOff int) {
	enc := encLDRuoff(rt, regSP, byteOff)
	if cb.peepholeElim(enc) {
		return
	}
	cb.emit(enc)
}

// emitLDRglobal emits a PC-relative LDR literal that loads the virtual address
// of a global variable into X9. Pool entry k is at word index 2*k; the signed
// word offset from the current instruction to that entry is 2*k − len(instrs).
func (cb *codeBuilder) emitLDRglobal(name string) {
	k := cb.poolIdx[name]
	imm19 := 2*k - len(cb.instrs)
	cb.emit(encLDRlit(regX9, imm19))
}

// emitLDRFPconst emits a PC-relative LDR literal that loads the 8-byte double
// value of a named FP constant from the pool directly into FP register fd.
func (cb *codeBuilder) emitLDRFPconst(label string, fd int) {
	k := cb.poolIdx[label]
	imm19 := 2*k - len(cb.instrs)
	cb.emit(encLDRDlit(fd, imm19))
}

// ── label / branch helpers ────────────────────────────────────────────────────

func (cb *codeBuilder) defineLabel(name string) {
	cb.labels[name] = len(cb.instrs)
	cb.peepholeBarrier = true
}

func (cb *codeBuilder) emitB(label string) {
	cb.fixups = append(cb.fixups, branchFixup{len(cb.instrs), label, fixB})
	cb.emit(0x14000000)
}

func (cb *codeBuilder) emitBL(label string) {
	cb.fixups = append(cb.fixups, branchFixup{len(cb.instrs), label, fixBL})
	cb.emit(0x94000000)
}

// emitBLextern emits a BL placeholder for an external symbol.
// Used in ET_REL mode; the linker fills in the final offset via R_AARCH64_CALL26.
func (cb *codeBuilder) emitBLextern(sym string) {
	cb.externBLs = append(cb.externBLs, externBLRecord{len(cb.instrs), sym})
	cb.emit(0x94000000)
}

func (cb *codeBuilder) emitBcond(cond int, label string) {
	cb.fixups = append(cb.fixups, branchFixup{len(cb.instrs), label, fixBcond})
	cb.emit(uint32(0x54000000 | cond))
}

func (cb *codeBuilder) emitCBZ(rt int, label string) {
	cb.fixups = append(cb.fixups, branchFixup{len(cb.instrs), label, fixCBZ})
	cb.emit(uint32(0xB4000000 | rt))
}

func (cb *codeBuilder) emitCBNZ(rt int, label string) {
	cb.fixups = append(cb.fixups, branchFixup{len(cb.instrs), label, fixCBNZ})
	cb.emit(uint32(0xB5000000 | rt))
}

// emitADR emits ADR Xd, <label> (PC-relative address load, ±1MB).
// The label must resolve to a word-aligned address in the same codeBuilder.
func (cb *codeBuilder) emitADR(rd int, label string) {
	cb.fixups = append(cb.fixups, branchFixup{len(cb.instrs), label, fixADR})
	cb.emit(0x10000000 | uint32(rd)) // ADR Xd, #0 placeholder
}

// applyFixups patches all recorded branch placeholders with correct offsets.
func (cb *codeBuilder) applyFixups() error {
	for _, fx := range cb.fixups {
		tgt, ok := cb.labels[fx.label]
		if !ok {
			return fmt.Errorf("undefined label %q", fx.label)
		}
		offset := tgt - fx.at
		old := cb.instrs[fx.at]
		switch fx.kind {
		case fixB, fixBL:
			if offset < -(1<<25) || offset >= (1<<25) {
				return fmt.Errorf("branch to %q: offset %d out of 26-bit range", fx.label, offset)
			}
			cb.instrs[fx.at] = (old & 0xFC000000) | uint32(offset)&0x3FFFFFF
		case fixBcond, fixCBZ, fixCBNZ:
			if offset < -(1<<18) || offset >= (1<<18) {
				return fmt.Errorf("branch to %q: offset %d out of 19-bit range", fx.label, offset)
			}
			cb.instrs[fx.at] = (old & 0xFF00001F) | (uint32(offset)&0x7FFFF)<<5
		case fixADR:
			// offset is in words; ADR encodes a *byte* offset.
			byteOff := offset * 4
			if byteOff < -(1<<20) || byteOff >= (1<<20) {
				return fmt.Errorf("ADR to %q: byte offset %d out of ±1MB range", fx.label, byteOff)
			}
			immlo := byteOff & 3
			immhi := (byteOff >> 2) & 0x7FFFF
			// Preserve opcode (bits 31, 28) and Rd (bits 4:0); patch immlo [30:29] + immhi [23:5].
			cb.instrs[fx.at] = (old & 0x9000001F) | uint32(immlo)<<29 | uint32(immhi)<<5
		}
	}
	return nil
}

// patchPool writes global virtual addresses into the literal pool.
func (cb *codeBuilder) patchPool(globalAddrs map[string]uint64) {
	for name, addr := range globalAddrs {
		k := cb.poolIdx[name]
		cb.instrs[k*2] = uint32(addr)
		cb.instrs[k*2+1] = uint32(addr >> 32)
	}
}

// ── ELF code generator ────────────────────────────────────────────────────────

// elfGen translates one IRProgram to binary ARM64 code.
type elfGen struct {
	cb            *codeBuilder
	pendingParams []paramArg         // params accumulate before each IRCall
	cmpN          int                // counter for synthetic comparison labels
	fn            *IRFunc
	fr            *frame
	isGlobalPtr   map[string]bool    // global variables that are pointers (TypeIntPtr/TypeCharPtr)
	funcRetType   map[string]TypeKind // function name → return type (for FP return detection)
	structDefs    map[string]*StructDef // struct type definitions
	// ET_REL object-file mode:
	isObjMode  bool
	localFuncs map[string]bool // functions defined in this compilation unit
}

// newLabel returns a unique internal label for comparison temporaries.
func (g *elfGen) newLabel() string {
	l := fmt.Sprintf("gaston_%s_cmp%d", g.fn.Name, g.cmpN)
	g.cmpN++
	return l
}

// funcLabel returns the global entry label for a C-minus function.
func funcLabel(name string) string { return "gaston_" + name }

// irLabel returns the label for an IR-level named label within a function.
func (g *elfGen) irLabel(extra string) string {
	return fmt.Sprintf("gaston_%s_%s", g.fn.Name, extra)
}

// ── load / store helpers ──────────────────────────────────────────────────────

// frameBase returns the frame-base register: regFP when the function has VLAs
// (SP may move), regSP otherwise.
func (g *elfGen) frameBase() int {
	if g.fr.hasVLA {
		return regFP
	}
	return regSP
}

// load emits instructions to load addr into register rd (0..7).
func (g *elfGen) load(addr IRAddr, rd int) {
	switch addr.Kind {
	case AddrConst:
		g.cb.emitMOVimm(rd, int64(addr.IVal))
	case AddrTemp, AddrLocal:
		off := g.fr.offsets[addr.Name]
		if g.fr.hasVLA {
			g.cb.emit(encLDRuoff(rd, regFP, off))
		} else {
			g.cb.emitLDRsp(rd, off)
		}
	case AddrGlobal:
		g.cb.emitLDRglobal(addr.Name)                    // X9 = &global
		g.cb.emit(encLDRuoff(rd, regX9, 0))              // rd = *X9
	}
}

// store emits instructions to store register rd into dst.
func (g *elfGen) store(rd int, dst IRAddr) {
	switch dst.Kind {
	case AddrTemp, AddrLocal:
		off := g.fr.offsets[dst.Name]
		g.cb.emit(encSTRuoff(rd, g.frameBase(), off))
	case AddrGlobal:
		g.cb.emitLDRglobal(dst.Name)                     // X9 = &global
		g.cb.emit(encSTRuoff(rd, regX9, 0))              // *X9 = rd
	}
}

// fpLoad emits instructions to load an FP value into D register fd (0..7).
// The stack slot layout is the same 8-byte slot as for integers; only the
// instruction differs (LDR Dt vs LDR Xt).
func (g *elfGen) fpLoad(addr IRAddr, fd int) {
	switch addr.Kind {
	case AddrFConst:
		g.cb.emitLDRFPconst(addr.Name, fd)
	case AddrTemp, AddrLocal:
		off := g.fr.offsets[addr.Name]
		g.cb.emit(encLDRDuoff(fd, g.frameBase(), off))
	case AddrGlobal:
		g.cb.emitLDRglobal(addr.Name)         // X9 = &global
		g.cb.emit(encLDRDuoff(fd, regX9, 0))  // Dfd = *X9
	}
}

// load128 loads a 128-bit value (lo, hi) from a 16-byte frame slot into (rdLo, rdHi).
// The slot at offsets[addr.Name] is the lo word; offsets[addr.Name]+8 is the hi word.
func (g *elfGen) load128(addr IRAddr, rdLo, rdHi int) {
	switch addr.Kind {
	case AddrTemp, AddrLocal:
		off := g.fr.offsets[addr.Name]
		base := g.frameBase()
		g.cb.emit(encLDRuoff(rdLo, base, off))
		g.cb.emit(encLDRuoff(rdHi, base, off+8))
	case AddrGlobal:
		g.cb.emitLDRglobal(addr.Name) // X9 = &global
		g.cb.emit(encLDRuoff(rdLo, regX9, 0))
		g.cb.emit(encLDRuoff(rdHi, regX9, 8))
	}
}

// store128 stores (rdLo, rdHi) into a 16-byte frame slot at dst.
func (g *elfGen) store128(rdLo, rdHi int, dst IRAddr) {
	switch dst.Kind {
	case AddrTemp, AddrLocal:
		off := g.fr.offsets[dst.Name]
		base := g.frameBase()
		g.cb.emit(encSTRuoff(rdLo, base, off))
		g.cb.emit(encSTRuoff(rdHi, base, off+8))
	case AddrGlobal:
		g.cb.emitLDRglobal(dst.Name) // X9 = &global
		g.cb.emit(encSTRuoff(rdLo, regX9, 0))
		g.cb.emit(encSTRuoff(rdHi, regX9, 8))
	}
}

// fpStore emits instructions to store D register fd into an FP stack/global slot.
func (g *elfGen) fpStore(fd int, dst IRAddr) {
	switch dst.Kind {
	case AddrTemp, AddrLocal:
		off := g.fr.offsets[dst.Name]
		g.cb.emit(encSTRDuoff(fd, g.frameBase(), off))
	case AddrGlobal:
		g.cb.emitLDRglobal(dst.Name)           // X9 = &global
		g.cb.emit(encSTRDuoff(fd, regX9, 0))   // *X9 = Dfd
	}
}

// emitFPCmpBool emits: Dst = (Src1 cond Src2) as integer 0 or 1.
// FCMP D0, D1 computes D0 − D1 and sets NZCV flags.
func (g *elfGen) emitFPCmpBool(q Quad, cond int) {
	g.fpLoad(q.Src1, 0)
	g.fpLoad(q.Src2, 1)
	g.cb.emit(encFCMPD(0, 1)) // FCMP D0, D1 → flags based on D0 − D1
	g.cb.emit(encCSET(regX0, cond))
	g.store(regX0, q.Dst)
}

// structBase loads the base address of a struct into rd, distinguishing two cases:
//  - isArrBase (struct local — storage is inline in the frame): ADD rd, SP/FP, #off
//  - everything else (temp / pointer slot that HOLDS the address): LDR rd, [SP/FP, #off]
// Used by IRStructCopy and emitStructReturn.
func (g *elfGen) structBase(addr IRAddr, rd int) {
	switch addr.Kind {
	case AddrGlobal:
		g.cb.emitLDRglobal(addr.Name)
		if g.isGlobalPtr[addr.Name] {
			g.cb.emit(encLDRuoff(rd, regX9, 0))
		} else {
			g.cb.emit(encMOVreg(rd, regX9))
		}
	case AddrLocal, AddrTemp:
		off := g.fr.offsets[addr.Name]
		base := g.frameBase()
		if g.fr.isArrBase[addr.Name] {
			g.cb.emit(encADDimm(rd, base, off)) // struct inline in frame → compute address
		} else {
			// slot holds a pointer value → load it
			if g.fr.hasVLA {
				g.cb.emit(encLDRuoff(rd, regFP, off))
			} else {
				g.cb.emitLDRsp(rd, off)
			}
		}
	}
}

// arrayBase emits instructions to load the base address of an array into rd.
func (g *elfGen) arrayBase(addr IRAddr, rd int) {
	switch addr.Kind {
	case AddrGlobal:
		g.cb.emitLDRglobal(addr.Name) // X9 = &global
		if g.isGlobalPtr[addr.Name] {
			// Pointer variable: the global slot holds an address; load it.
			g.cb.emit(encLDRuoff(rd, regX9, 0)) // rd = *X9 (pointer value)
		} else {
			// Array: &global IS the base address of the elements.
			g.cb.emit(encMOVreg(rd, regX9))
		}
	case AddrLocal, AddrTemp:
		off := g.fr.offsets[addr.Name]
		base := g.frameBase()
		if g.fr.isArrPtr[addr.Name] || g.fr.isPtrVar[addr.Name] {
			// Array param, pointer variable, or VLA: frame slot holds a pointer.
			if g.fr.hasVLA {
				g.cb.emit(encLDRuoff(rd, regFP, off))
			} else {
				g.cb.emitLDRsp(rd, off)
			}
		} else {
			// Local array: its elements are inline in the frame.
			g.cb.emit(encADDimm(rd, base, off))
		}
	}
}

// ── prologue / epilogue ───────────────────────────────────────────────────────

// emitSubSP subtracts imm from SP, handling values > 4095 that don't fit
// in a single SUB immediate instruction.
func (g *elfGen) emitSubSP(imm int) {
	for imm > 4095 {
		g.cb.emit(encSUBimm(regSP, regSP, 4095))
		imm -= 4095
	}
	if imm > 0 {
		g.cb.emit(encSUBimm(regSP, regSP, imm))
	}
}

// emitAddSP adds imm to SP, handling values > 4095 that don't fit
// in a single ADD immediate instruction.
func (g *elfGen) emitAddSP(imm int) {
	for imm > 4095 {
		g.cb.emit(encADDimm(regSP, regSP, 4095))
		imm -= 4095
	}
	if imm > 0 {
		g.cb.emit(encADDimm(regSP, regSP, imm))
	}
}

func (g *elfGen) emitPrologue(fn *IRFunc) {
	f := g.fr
	g.emitSubSP(f.frameSize)
	g.cb.emit(encSTP(regFP, regLR, regSP, 0))
	g.cb.emit(encADDimm(regFP, regSP, 0)) // FP = SP
	// AAPCS64: integer params arrive in X0–X7, FP params in D0–D7 (separate banks).
	iIdx, fIdx := 0, 0
	for i, name := range fn.Params {
		if i >= len(fn.ParamType) {
			break
		}
		pt := fn.ParamType[i]
		if pt == TypeStruct {
			off := f.offsets[name]
			tag := ""
			if i < len(fn.ParamStructTag) {
				tag = fn.ParamStructTag[i]
			}
			size := 0
			if sd, ok := g.structDefs[tag]; ok {
				size = sd.SizeBytes(g.structDefs)
			}
			switch {
			case size <= 8:
				g.cb.emit(encSTRuoff(iIdx, regSP, off))
				iIdx++
			case size <= 16:
				g.cb.emit(encSTRuoff(iIdx, regSP, off))
				g.cb.emit(encSTRuoff(iIdx+1, regSP, off+8))
				iIdx += 2
			default:
				numWords := (size + 7) / 8
				g.cb.emit(encMOVreg(regX9, iIdx)) // X9 = caller's pointer
				for w := 0; w < numWords; w++ {
					g.cb.emit(encLDRuoff(10, regX9, w*8))
					g.cb.emit(encSTRuoff(10, regSP, off+w*8))
				}
				iIdx++
			}
		} else if isFPType(pt) {
			if fIdx < 8 {
				g.cb.emit(encSTRDuoff(fIdx, regSP, f.offsets[name]))
				fIdx++
			}
		} else {
			if iIdx < 8 {
				g.cb.emit(encSTRuoff(iIdx, regSP, f.offsets[name]))
				iIdx++
			}
		}
	}
	// Variadic: save X1..X7 to frame slots SP+24..SP+72.
	if fn.IsVariadic {
		for i := 1; i <= 7; i++ {
			g.cb.emit(encSTRuoff(i, regSP, 16+8*i))
		}
	}
	// Struct return (>16 bytes): X8 holds the caller's destination pointer on entry.
	// Save it before any inner call can clobber it.
	if fn.ReturnStructTag != "" {
		if sd, ok := g.structDefs[fn.ReturnStructTag]; ok && sd.SizeBytes(g.structDefs) > 16 {
			if off, ok2 := f.offsets["__x8_save"]; ok2 {
				g.cb.emit(encSTRuoff(regX8, regSP, off))
			}
		}
	}
}

func (g *elfGen) emitEpilogue() {
	if g.fr.hasVLA {
		// SP may have moved below FP due to VLA allocation.
		// Restore SP = FP so the saved FP/LR are at [SP, #0].
		g.cb.emit(encADDimm(regSP, regFP, 0))
	}
	g.cb.emit(encLDP(regFP, regLR, regSP, 0))
	g.emitAddSP(g.fr.frameSize)
	g.cb.emit(encRET())
}

// emitStructReturn handles IRReturn when TypeHint == TypeStruct.
// q.Src1 names the local struct variable to return; q.StructTag names the type.
// AAPCS64 rules:
//   ≤  8 bytes → X0 = first 8-byte word
//   ≤ 16 bytes → X0 = first word, X1 = second word
//   > 16 bytes → caller put destination in X8; copy all words through it
func (g *elfGen) emitStructReturn(q Quad) {
	sd := g.structDefs[q.StructTag]
	size := 0
	if sd != nil {
		size = sd.SizeBytes(g.structDefs)
	}
	// X9 = base address of source struct
	g.structBase(q.Src1, regX9)
	switch {
	case size <= 8:
		g.cb.emit(encLDRuoff(regX0, regX9, 0))
	case size <= 16:
		g.cb.emit(encLDRuoff(regX0, regX9, 0))
		g.cb.emit(encLDRuoff(regX1, regX9, 8))
	default:
		// Reload the caller's destination pointer that was saved on entry.
		if off, ok := g.fr.offsets["__x8_save"]; ok {
			g.cb.emit(encLDRuoff(regX8, g.frameBase(), off))
		}
		numWords := (size + 7) / 8
		for i := 0; i < numWords; i++ {
			g.cb.emit(encLDRuoff(10, regX9, i*8)) // X10 as scratch
			g.cb.emit(encSTRuoff(10, regX8, i*8))
		}
	}
	g.emitEpilogue()
}

// ── IR quad emission ──────────────────────────────────────────────────────────

func (g *elfGen) genFunc(fn *IRFunc) {
	g.fn = fn
	g.fr = buildFrame(fn, g.structDefs)
	g.cmpN = 0
	g.pendingParams = g.pendingParams[:0]

	g.cb.defineLabel(funcLabel(fn.Name))
	g.emitPrologue(fn)

	for _, q := range fn.Quads {
		switch q.Op {
		case IREnter:
			// prologue already emitted

		case IRLabel:
			g.cb.defineLabel(g.irLabel(q.Extra))

		case IRJump:
			g.cb.emitB(g.irLabel(q.Extra))

		case IRJumpT:
			g.load(q.Src1, regX0)
			g.cb.emit(encCMPimm0(regX0))
			g.cb.emitBcond(condNE, g.irLabel(q.Extra))

		case IRJumpF:
			g.load(q.Src1, regX0)
			g.cb.emit(encCMPimm0(regX0))
			g.cb.emitBcond(condEQ, g.irLabel(q.Extra))

		case IRCopy:
			g.load(q.Src1, regX0)
			g.store(regX0, q.Dst)

		case IRAdd:
			g.emitArith(q, encADDreg)
		case IRSub:
			g.emitArith(q, encSUBreg)
		case IRMul:
			g.emitArith(q, encMUL)
		case IRDiv:
			g.emitArith(q, encSDIV)
		case IRMod:
			g.emitMod(q)
		case IRUDiv:
			g.emitArith(q, encUDIV)
		case IRUMod:
			g.emitUMod(q)
		case IRBitAnd:
			g.emitArith(q, encAND)
		case IRBitOr:
			g.emitArith(q, encORR)
		case IRBitXor:
			g.emitArith(q, encEOR)
		case IRShl:
			g.emitArith(q, encLSLV)
		case IRShr:
			g.emitArith(q, encASRV)
		case IRUShr:
			g.emitArith(q, encLSRV)
		case IRBitNot:
			g.load(q.Src1, regX0)
			g.cb.emit(encMVN(regX0, regX0))
			g.store(regX0, q.Dst)

		case IRLt:
			g.emitCmpBool(q, condLT)
		case IRLe:
			g.emitCmpBool(q, condLE)
		case IRGt:
			g.emitCmpBool(q, condGT)
		case IRGe:
			g.emitCmpBool(q, condGE)
		case IREq:
			g.emitCmpBool(q, condEQ)
		case IRNe:
			g.emitCmpBool(q, condNE)
		case IRULt:
			g.emitCmpBool(q, condLO)
		case IRULe:
			g.emitCmpBool(q, condLS)
		case IRUGt:
			g.emitCmpBool(q, condHI)
		case IRUGe:
			g.emitCmpBool(q, condHS)

		case IRLoad:
			g.emitArrayLoad(q)
		case IRStore:
			g.emitArrayStore(q)
		case IRCharLoad:
			g.emitCharArrayLoad(q)
		case IRCharStore:
			g.emitCharArrayStore(q)
		case IRGetAddr:
			g.arrayBase(q.Src1, regX0)
			g.store(regX0, q.Dst)

		case IRAddrOf:
			// Compute the storage address of Src1 — never loads through a pointer slot.
			switch q.Src1.Kind {
			case AddrLocal, AddrTemp:
				off := g.fr.offsets[q.Src1.Name]
				g.cb.emit(encADDimm(regX0, g.frameBase(), off))
			case AddrGlobal:
				g.cb.emitLDRglobal(q.Src1.Name) // X9 = VA of global slot
				g.cb.emit(encMOVreg(regX0, regX9))
			}
			g.store(regX0, q.Dst)

		case IRSignExtend:
			// Dst = sign_extend(Src1, bits) — integer promotion for char/short.
			g.load(q.Src1, regX0)
			if q.Src2.IVal == 8 {
				g.cb.emit(encSXTB(regX0, regX0))
			} else {
				g.cb.emit(encSXTH(regX0, regX0))
			}
			g.store(regX0, q.Dst)

		case IRZeroExtend:
			// Dst = zero_extend(Src1, bits) — integer promotion for unsigned char/short.
			g.load(q.Src1, regX0)
			if q.Src2.IVal == 8 {
				g.cb.emit(encUXTB(regX0, regX0))
			} else {
				g.cb.emit(encUXTH(regX0, regX0))
			}
			g.store(regX0, q.Dst)

		case IRCLZ:
			g.load(q.Src1, regX0)
			if q.TypeHint == TypeUnsignedInt {
				// 32-bit __builtin_clz: use 32-bit CLZ (counts from bit 31)
				g.cb.emit(encCLZ32(regX0, regX0))
			} else {
				g.cb.emit(encCLZ(regX0, regX0))
			}
			g.store(regX0, q.Dst)

		case IRCTZ:
			g.load(q.Src1, regX0)
			if q.TypeHint == TypeUnsignedInt {
				g.cb.emit(encRBIT32(regX0, regX0))
				g.cb.emit(encCLZ32(regX0, regX0))
			} else {
				g.cb.emit(encRBIT(regX0, regX0))
				g.cb.emit(encCLZ(regX0, regX0))
			}
			g.store(regX0, q.Dst)

		case IRPopcount:
			// FMOV D0, X0; CNT V0.8B, V0.8B; ADDV B0, V0.8B; UMOV W0, V0.B[0]
			g.load(q.Src1, regX0)
			g.cb.emit(encFMOVfromGP(0, regX0)) // FMOV D0, X0
			g.cb.emit(encCNT8B(0, 0))           // CNT  V0.8B, V0.8B
			g.cb.emit(encADDVb(0, 0))           // ADDV B0, V0.8B
			g.cb.emit(encUMOVb0(regX0, 0))      // UMOV W0, V0.B[0]
			g.store(regX0, q.Dst)

		case IR128Copy:
			g.load128(q.Src1, regX0, regX1)
			g.store128(regX0, regX1, q.Dst)

		case IR128Add:
			g.load128(q.Src1, regX0, regX1) // X0=lo1, X1=hi1
			g.load128(q.Src2, regX2, regX3) // X2=lo2, X3=hi2
			g.cb.emit(encADDS(regX0, regX0, regX2)) // X0 = lo1+lo2, flags
			g.cb.emit(encADC(regX1, regX1, regX3))  // X1 = hi1+hi2+carry
			g.store128(regX0, regX1, q.Dst)

		case IR128Sub:
			g.load128(q.Src1, regX0, regX1)
			g.load128(q.Src2, regX2, regX3)
			g.cb.emit(encSUBS(regX0, regX0, regX2)) // X0 = lo1-lo2, flags
			g.cb.emit(encSBC(regX1, regX1, regX3))  // X1 = hi1-hi2-borrow
			g.store128(regX0, regX1, q.Dst)

		case IR128Mul:
			// Full 64×64→128-bit multiply.
			// For unsigned: lo = MUL(a_lo, b_lo); hi = UMULH(a_lo, b_lo)
			// For signed:   lo = MUL(a_lo, b_lo); hi = SMULH(a_lo, b_lo)
			g.load128(q.Src1, regX0, regX1)
			g.load128(q.Src2, regX2, regX3)
			g.cb.emit(encMUL(regX4, regX0, regX2)) // X4 = lo(a*b)
			if q.TypeHint == TypeUint128 {
				g.cb.emit(encUMULH(regX5, regX0, regX2)) // X5 = hi(a*b) unsigned
			} else {
				g.cb.emit(encSMULH(regX5, regX0, regX2)) // X5 = hi(a*b) signed
			}
			g.store128(regX4, regX5, q.Dst)

		case IR128And:
			g.load128(q.Src1, regX0, regX1)
			g.load128(q.Src2, regX2, regX3)
			g.cb.emit(encAND(regX0, regX0, regX2))
			g.cb.emit(encAND(regX1, regX1, regX3))
			g.store128(regX0, regX1, q.Dst)

		case IR128Or:
			g.load128(q.Src1, regX0, regX1)
			g.load128(q.Src2, regX2, regX3)
			g.cb.emit(encORR(regX0, regX0, regX2))
			g.cb.emit(encORR(regX1, regX1, regX3))
			g.store128(regX0, regX1, q.Dst)

		case IR128Xor:
			g.load128(q.Src1, regX0, regX1)
			g.load128(q.Src2, regX2, regX3)
			g.cb.emit(encEOR(regX0, regX0, regX2))
			g.cb.emit(encEOR(regX1, regX1, regX3))
			g.store128(regX0, regX1, q.Dst)

		case IR128Neg:
			g.load128(q.Src1, regX0, regX1)
			g.cb.emit(encNEGS(regX0, regX0)) // X0 = -lo, flags
			g.cb.emit(encNGC(regX1, regX1))  // X1 = ~hi + carry
			g.store128(regX0, regX1, q.Dst)

		case IR128Shl:
			n := q.Src2.IVal
			g.load128(q.Src1, regX0, regX1) // X0=lo, X1=hi
			switch {
			case n == 0:
				g.store128(regX0, regX1, q.Dst)
			case n == 64:
				g.cb.emit(encMOVreg(regX1, regX0)) // hi = lo
				g.cb.emit(encMOVZ(regX0, 0, 0))    // lo = 0
				g.store128(regX0, regX1, q.Dst)
			case n > 0 && n < 64:
				g.emit128Shl(regX0, regX1, n)
				g.store128(regX0, regX1, q.Dst)
			case n > 64 && n < 128:
				g.cb.emit(encUBFM(regX1, regX0, 64-(n-64), 63-(n-64))) // hi = lo << (n-64)
				g.cb.emit(encMOVZ(regX0, 0, 0))                         // lo = 0
				g.store128(regX0, regX1, q.Dst)
			default: // n >= 128
				g.cb.emit(encMOVZ(regX0, 0, 0))
				g.cb.emit(encMOVZ(regX1, 0, 0))
				g.store128(regX0, regX1, q.Dst)
			}

		case IR128LShr:
			n := q.Src2.IVal
			g.load128(q.Src1, regX0, regX1) // X0=lo, X1=hi
			switch {
			case n == 0:
				g.store128(regX0, regX1, q.Dst)
			case n == 64:
				g.cb.emit(encMOVreg(regX0, regX1)) // lo = hi
				g.cb.emit(encMOVZ(regX1, 0, 0))    // hi = 0
				g.store128(regX0, regX1, q.Dst)
			case n > 0 && n < 64:
				g.emit128LShr(regX0, regX1, n)
				g.store128(regX0, regX1, q.Dst)
			case n > 64 && n < 128:
				g.cb.emit(encUBFM(regX0, regX1, n-64, 63)) // lo = hi >> (n-64)
				g.cb.emit(encMOVZ(regX1, 0, 0))             // hi = 0
				g.store128(regX0, regX1, q.Dst)
			default: // n >= 128
				g.cb.emit(encMOVZ(regX0, 0, 0))
				g.cb.emit(encMOVZ(regX1, 0, 0))
				g.store128(regX0, regX1, q.Dst)
			}

		case IR128AShr:
			// Arithmetic right shift: hi half is sign-extended.
			n := q.Src2.IVal
			g.load128(q.Src1, regX0, regX1) // X0=lo, X1=hi
			switch {
			case n == 0:
				g.store128(regX0, regX1, q.Dst)
			case n == 64:
				g.cb.emit(encMOVreg(regX0, regX1))   // lo = hi
				g.cb.emit(encASR(regX1, regX1, 63))  // hi = sign bit
				g.store128(regX0, regX1, q.Dst)
			case n > 0 && n < 64:
				// lo = (lo >> n) | (hi << (64-n))
				// hi = hi >> n  (arithmetic)
				g.cb.emit(encUBFM(regX0, regX0, n, 63))       // lo = lo >> n (logical)
				g.cb.emit(encUBFM(regX2, regX1, 64-n, 63-n))  // X2 = hi << (64-n)
				g.cb.emit(encORR(regX0, regX0, regX2))         // lo |= X2
				g.cb.emit(encASR(regX1, regX1, n))             // hi = hi >> n (arithmetic)
				g.store128(regX0, regX1, q.Dst)
			default: // n >= 64
				g.cb.emit(encASR(regX0, regX1, 63)) // lo = sign bit
				g.cb.emit(encASR(regX1, regX1, 63)) // hi = sign bit
				g.store128(regX0, regX1, q.Dst)
			}

		case IR128FromU64:
			// 128-bit zero-extension of a 64-bit value.
			g.load(q.Src1, regX0)
			g.cb.emit(encMOVZ(regX1, 0, 0)) // hi = 0
			g.store128(regX0, regX1, q.Dst)

		case IR128FromI64:
			// 128-bit sign-extension of a 64-bit signed value.
			g.load(q.Src1, regX0)
			g.cb.emit(encASR(regX1, regX0, 63)) // hi = sign bit replicated
			g.store128(regX0, regX1, q.Dst)

		case IR64From128:
			// Narrow 128→64: take the lo half.
			g.load128(q.Src1, regX0, regX1)
			g.store(regX0, q.Dst)

		case IR128Eq:
			// (a_lo^b_lo)|(a_hi^b_hi) == 0
			g.load128(q.Src1, regX0, regX1)
			g.load128(q.Src2, regX2, regX3)
			g.cb.emit(encEOR(regX0, regX0, regX2))
			g.cb.emit(encEOR(regX1, regX1, regX3))
			g.cb.emit(encORR(regX0, regX0, regX1))
			g.cb.emit(encCMPimm0(regX0))
			g.cb.emit(encCSET(regX0, condEQ))
			g.store(regX0, q.Dst)

		case IR128Ne:
			// (a_lo^b_lo)|(a_hi^b_hi) != 0
			g.load128(q.Src1, regX0, regX1)
			g.load128(q.Src2, regX2, regX3)
			g.cb.emit(encEOR(regX0, regX0, regX2))
			g.cb.emit(encEOR(regX1, regX1, regX3))
			g.cb.emit(encORR(regX0, regX0, regX1))
			g.cb.emit(encCMPimm0(regX0))
			g.cb.emit(encCSET(regX0, condNE))
			g.store(regX0, q.Dst)

		// 128-bit unsigned comparisons.
		// Pattern: SUBS lo; SBCS hi; CSET cond.
		// For < and >= : no operand swap (a - b), CSET LO/HS.
		// For > and <= : swap operands (b - a), CSET LO/HS.
		case IR128ULt:
			g.emit128Cmp(q, false, condLO) // a < b: a-b, LO (C=0)
		case IR128ULe:
			g.emit128Cmp(q, true, condHS)  // a <= b: b-a, HS (C=1 = b>=a)
		case IR128UGt:
			g.emit128Cmp(q, true, condLO)  // a > b: b-a, LO (C=0 = b<a)
		case IR128UGe:
			g.emit128Cmp(q, false, condHS) // a >= b: a-b, HS (C=1)

		// 128-bit signed comparisons. Same swap pattern but with signed conditions.
		case IR128SLt:
			g.emit128Cmp(q, false, condLT) // a < b: a-b, LT (N≠V)
		case IR128SLe:
			g.emit128Cmp(q, true, condGE)  // a <= b: b-a, GE (N=V = b>=a)
		case IR128SGt:
			g.emit128Cmp(q, true, condLT)  // a > b: b-a, LT (N≠V = b<a)
		case IR128SGe:
			g.emit128Cmp(q, false, condGE) // a >= b: a-b, GE (N=V)

		case IRStrAddr:
			// Load the address of a string literal via the pool.
			g.cb.emitLDRglobal(q.Extra)              // X9 = VA of string literal
			g.cb.emit(encMOVreg(regX0, regX9))       // X0 = X9
			g.store(regX0, q.Dst)

		case IRDerefLoad:
			// Dst = *Src1 (load via pointer in Src1; 1 byte for char, 8 bytes otherwise)
			g.load(q.Src1, regX0)
			if q.TypeHint == TypeChar || q.TypeHint == TypeUnsignedChar {
				g.cb.emit(encLDRBuoff(regX0, regX0, 0))
			} else {
				g.cb.emit(encLDRuoff(regX0, regX0, 0))
			}
			g.store(regX0, q.Dst)

		case IRDerefStore:
			// *Dst = Src1 (store via pointer in Dst; 1 byte for char, 8 bytes otherwise)
			g.load(q.Dst, regX0)
			g.load(q.Src1, regX1)
			if q.TypeHint == TypeChar || q.TypeHint == TypeUnsignedChar {
				g.cb.emit(encSTRBuoff(regX1, regX0, 0))
			} else {
				g.cb.emit(encSTRuoff(regX1, regX0, 0))
			}

		case IRFDerefLoad:
			// Dst = *Src1 (load 8-byte double via pointer; result stored as FP)
			g.load(q.Src1, regX0)                    // X0 = pointer value
			g.cb.emit(encLDRDuoff(0, regX0, 0))      // D0 = *(X0)
			g.fpStore(0, q.Dst)                       // slot[Dst] = D0

		case IRFDerefStore:
			// *Dst = Src1 (store 8-byte double via pointer; Src1 is FP)
			g.load(q.Dst, regX0)                      // X0 = pointer value
			g.fpLoad(q.Src1, 0)                       // D0 = Src1
			g.cb.emit(encSTRDuoff(0, regX0, 0))       // *(X0) = D0

		case IRDerefCharLoad:
			// Dst = *(byte*)Src1 — zero-extended 1-byte load via computed pointer
			g.load(q.Src1, regX0)                    // X0 = byte address
			g.cb.emit(encLDRBuoff(regX0, regX0, 0)) // X0 = zero-extended byte
			g.store(regX0, q.Dst)

		case IRDerefCharStore:
			// *(byte*)Dst = Src1 — 1-byte store via computed pointer
			g.load(q.Dst, regX0)                    // X0 = byte address
			g.load(q.Src1, regX1)                   // X1 = value
			g.cb.emit(encSTRBuoff(regX1, regX0, 0)) // *(X0) = X1 low byte

		case IRFieldLoad:
			// Dst = *(Src1 + Src2.IVal) — struct field load; width depends on TypeHint
			g.load(q.Src1, regX0) // X0 = base ptr
			switch q.TypeHint {
			case TypeChar:
				g.cb.emit(encLDRSBuoff(regX0, regX0, q.Src2.IVal)) // LDRSB: sign-ext byte → 64-bit
			case TypeUnsignedChar:
				g.cb.emit(encLDRBuoff(regX0, regX0, q.Src2.IVal)) // LDRB: zero-ext byte → 64-bit
			case TypeShort:
				g.cb.emit(encLDRSH(regX0, regX0, q.Src2.IVal)) // LDRSH: sign-ext halfword → 64-bit
			case TypeUnsignedShort:
				g.cb.emit(encLDRH(regX0, regX0, q.Src2.IVal)) // LDRH: zero-ext halfword → 64-bit
			case TypeInt:
				g.cb.emit(encLDRSWuoff(regX0, regX0, q.Src2.IVal)) // LDRSW: sign-ext 32 → 64-bit
			case TypeUnsignedInt:
				g.cb.emit(encLDRWuoff(regX0, regX0, q.Src2.IVal)) // LDR W: zero-ext 32 → 64-bit
			default:
				g.cb.emit(encLDRuoff(regX0, regX0, q.Src2.IVal)) // LDR: 8-byte load
			}
			g.store(regX0, q.Dst)

		case IRFieldStore:
			// *(Dst + Src2.IVal) = Src1 — struct field store; width depends on TypeHint
			g.load(q.Dst, regX0) // X0 = base ptr
			g.load(q.Src1, regX1) // X1 = value
			switch q.TypeHint {
			case TypeChar, TypeUnsignedChar:
				g.cb.emit(encSTRBuoff(regX1, regX0, q.Src2.IVal)) // STRB: store 1 byte
			case TypeShort, TypeUnsignedShort:
				g.cb.emit(encSTRH(regX1, regX0, q.Src2.IVal)) // STRH: store 2 bytes
			case TypeInt, TypeUnsignedInt:
				g.cb.emit(encSTRWuoff(regX1, regX0, q.Src2.IVal)) // STR W: store 4 bytes
			default:
				g.cb.emit(encSTRuoff(regX1, regX0, q.Src2.IVal)) // STR: store 8 bytes
			}

		case IRFFieldLoad:
			// Dst = *(Src1 + Src2.IVal) — struct FP field load; width from TypeHint
			g.load(q.Src1, regX0)                              // X0 = base ptr
			if q.TypeHint == TypeFloat {
				g.cb.emit(encLDRSuoff(0, regX0, q.Src2.IVal)) // LDR S0, [X0+off] (4-byte float)
				g.cb.emit(encFCVTSD(0, 0))                     // FCVT D0, S0 (single → double)
			} else {
				g.cb.emit(encLDRDuoff(0, regX0, q.Src2.IVal)) // LDR D0, [X0+off] (8-byte double)
			}
			g.fpStore(0, q.Dst)

		case IRFFieldStore:
			// *(Dst + Src2.IVal) = Src1 — struct FP field store; width from TypeHint
			g.load(q.Dst, regX0)                               // X0 = base ptr
			g.fpLoad(q.Src1, 0)                                // D0 = value (always 64-bit in frame)
			if q.TypeHint == TypeFloat {
				g.cb.emit(encFCVTDS(0, 0))                     // FCVT S0, D0 (double → single)
				g.cb.emit(encSTRSuoff(0, regX0, q.Src2.IVal)) // STR S0, [X0+off] (4-byte float)
			} else {
				g.cb.emit(encSTRDuoff(0, regX0, q.Src2.IVal)) // STR D0, [X0+off] (8-byte double)
			}

		// ── floating-point operations ────────────────────────────────────
		case IRFAdd:
			g.fpLoad(q.Src1, 0)
			g.fpLoad(q.Src2, 1)
			g.cb.emit(encFADDD(0, 0, 1))
			g.fpStore(0, q.Dst)
		case IRFSub:
			g.fpLoad(q.Src1, 0)
			g.fpLoad(q.Src2, 1)
			g.cb.emit(encFSUBD(0, 0, 1))
			g.fpStore(0, q.Dst)
		case IRFMul:
			g.fpLoad(q.Src1, 0)
			g.fpLoad(q.Src2, 1)
			g.cb.emit(encFMULD(0, 0, 1))
			g.fpStore(0, q.Dst)
		case IRFDiv:
			g.fpLoad(q.Src1, 0)
			g.fpLoad(q.Src2, 1)
			g.cb.emit(encFDIVD(0, 0, 1))
			g.fpStore(0, q.Dst)
		case IRFNeg:
			g.fpLoad(q.Src1, 0)
			g.cb.emit(encFNEGD(0, 0))
			g.fpStore(0, q.Dst)
		case IRFCopy:
			g.fpLoad(q.Src1, 0)
			g.fpStore(0, q.Dst)
		case IRFLt:
			g.emitFPCmpBool(q, condLT)
		case IRFLe:
			g.emitFPCmpBool(q, condLE)
		case IRFGt:
			g.emitFPCmpBool(q, condGT)
		case IRFGe:
			g.emitFPCmpBool(q, condGE)
		case IRFEq:
			g.emitFPCmpBool(q, condEQ)
		case IRFNe:
			g.emitFPCmpBool(q, condNE)
		case IRIntToDouble:
			g.load(q.Src1, regX0)
			g.cb.emit(encSCVTFD(0, regX0)) // SCVTF D0, X0
			g.fpStore(0, q.Dst)
		case IRDoubleToInt:
			g.fpLoad(q.Src1, 0)
			g.cb.emit(encFCVTZSD(regX0, 0)) // FCVTZS X0, D0
			g.store(regX0, q.Dst)
		case IRFBitcastFI:
			g.fpLoad(q.Src1, 0)
			g.cb.emit(encFMOVtoGP(regX0, 0)) // FMOV X0, D0 — copy bits, no conversion
			g.store(regX0, q.Dst)
		case IRFBitcastIF:
			g.load(q.Src1, regX0)
			g.cb.emit(encFMOVfromGP(0, regX0)) // FMOV D0, X0 — copy bits, no conversion
			g.fpStore(0, q.Dst)

		case IRStructCopy:
			// x = y: copy all 8-byte words from Src1 (src addr) to Dst (dst addr).
			sd := g.structDefs[q.StructTag]
			if sd != nil {
				size := sd.SizeBytes(g.structDefs)
				numWords := (size + 7) / 8
				g.structBase(q.Src1, regX9) // X9 = src base
				g.structBase(q.Dst, 10)      // X10 = dst base
				for i := 0; i < numWords; i++ {
					g.cb.emit(encLDRuoff(11, regX9, i*8)) // X11 = word
					g.cb.emit(encSTRuoff(11, 10, i*8))    // store word
				}
			}

		case IRParam:
			if q.TypeHint == TypeStruct {
				g.pendingParams = append(g.pendingParams, paramArg{
					addr: q.Src1, isStruct: true, structTag: q.StructTag,
				})
			} else {
				g.pendingParams = append(g.pendingParams, paramArg{addr: q.Src1, isFP: false})
			}

		case IRFParam:
			g.pendingParams = append(g.pendingParams, paramArg{addr: q.Src1, isFP: true})

		case IRCall:
			g.emitCall(q)

		case IRVLAAlloc:
			// Allocate Src1*8 bytes on the stack (16-byte aligned).
			// Store the resulting stack pointer (VLA base) in the Dst frame slot.
			// The function uses FP-relative addressing so static slots stay valid.
			g.load(q.Src1, regX0)
			g.cb.emit(encLSLimm(regX0, regX0, 3))         // X0 = n * 8
			g.cb.emit(encADDimm(regX0, regX0, 15))         // X0 += 15 (round-up bias)
			g.cb.emitMOVimm(regX1, -16)                    // X1 = 0xFFFF…FFF0
			g.cb.emit(encAND(regX0, regX0, regX1))         // X0 &= ~15 (align to 16)
			g.cb.emit(encSUBext(regSP, regSP, regX0))      // SP -= aligned_size
			g.cb.emit(encADDimm(regX0, regSP, 0))          // X0 = SP (VLA base)
			g.store(regX0, q.Dst)                          // save base in FP-relative slot

		case IRFuncAddr:
			// Load the virtual address of a named function from the literal pool into X0.
			// The pool entry holds the function's VA (patched in Phase 4).
			g.cb.emitLDRglobal(funcLabel(q.Extra)) // X9 = pool[funcLabel] = func VA
			g.cb.emit(encMOVreg(regX0, regX9))     // X0 = func VA
			g.store(regX0, q.Dst)

		case IRFuncPtrCall:
			// Call through a function pointer value in Src1.
			// Flush pending params into X0–X7 / D0–D7 first, then BLR X8.
			iIdx, fIdx := 0, 0
			for _, p := range g.pendingParams {
				if p.isFP {
					if fIdx < 8 {
						g.fpLoad(p.addr, fIdx)
						fIdx++
					}
				} else {
					if iIdx < 8 {
						g.load(p.addr, iIdx)
						iIdx++
					}
				}
			}
			g.pendingParams = g.pendingParams[:0]
			g.load(q.Src1, regX16) // func ptr value into X16 (IP0, intra-proc scratch)
			g.cb.emit(encBLR(regX16))
			if q.Dst.Kind != AddrNone {
				g.store(regX0, q.Dst)
			}

		case IRReturn:
			if q.TypeHint == TypeStruct && q.StructTag != "" {
				g.emitStructReturn(q)
			} else {
				if q.Src1.Kind != AddrNone {
					if isFPType(g.fn.ReturnType) {
						g.fpLoad(q.Src1, 0) // FP return value in D0
					} else {
						g.load(q.Src1, regX0)
					}
				}
				g.emitEpilogue()
			}
		}
	}
}

func (g *elfGen) emitArith(q Quad, enc func(rd, rn, rm int) uint32) {
	g.load(q.Src1, regX0)
	g.load(q.Src2, regX1)
	g.cb.emit(enc(regX0, regX0, regX1))
	g.store(regX0, q.Dst)
}

// emitMod emits: Dst = Src1 % Src2 using SDIV + MSUB.
func (g *elfGen) emitMod(q Quad) {
	g.load(q.Src1, regX0)                                 // X0 = a
	g.load(q.Src2, regX1)                                 // X1 = b
	g.cb.emit(encSDIV(regX2, regX0, regX1))              // X2 = a / b (signed)
	g.cb.emit(encMSUB(regX0, regX2, regX1, regX0))       // X0 = a - X2*b
	g.store(regX0, q.Dst)
}

// emitUMod emits: Dst = Src1 % Src2 (unsigned) using UDIV + MSUB.
func (g *elfGen) emitUMod(q Quad) {
	g.load(q.Src1, regX0)
	g.load(q.Src2, regX1)
	g.cb.emit(encUDIV(regX2, regX0, regX1))             // X2 = a / b (unsigned)
	g.cb.emit(encMSUB(regX0, regX2, regX1, regX0))      // X0 = a - X2*b
	g.store(regX0, q.Dst)
}

func (g *elfGen) emitCmpBool(q Quad, cond int) {
	g.load(q.Src1, regX0)
	g.load(q.Src2, regX1)
	g.cb.emit(encCMPreg(regX0, regX1))
	g.cb.emit(encCSET(regX0, cond))
	g.store(regX0, q.Dst)
}

// emit128Shl emits a 128-bit logical left shift by constant n (0 < n < 64).
// On entry: lo=regX0, hi=regX1. On exit: regX0=new lo, regX1=new hi.
// Uses regX2 as scratch.
func (g *elfGen) emit128Shl(lo, hi, n int) {
	// hi = (hi << n) | (lo >> (64-n))
	// lo = lo << n
	// LSL hi, hi, #n  = UBFM hi, hi, #(64-n), #(63-n)
	// LSR X2, lo, #(64-n) = UBFM X2, lo, #(64-n), #63
	// ORR hi, hi, X2
	// LSL lo, lo, #n  = UBFM lo, lo, #(64-n), #(63-n)
	g.cb.emit(encUBFM(hi, hi, 64-n, 63-n))           // hi = hi << n
	g.cb.emit(encUBFM(regX2, lo, 64-n, 63))          // X2 = lo >> (64-n)
	g.cb.emit(encORR(hi, hi, regX2))                  // hi |= X2
	g.cb.emit(encUBFM(lo, lo, 64-n, 63-n))           // lo = lo << n
}

// emit128LShr emits a 128-bit logical right shift by constant n (0 < n < 64).
// On entry: lo=regX0, hi=regX1. On exit: regX0=new lo, regX1=new hi.
// Uses regX2 as scratch.
func (g *elfGen) emit128LShr(lo, hi, n int) {
	// lo = (lo >> n) | (hi << (64-n))
	// hi = hi >> n
	// LSR lo, lo, #n  = UBFM lo, lo, #n, #63
	// LSL X2, hi, #(64-n) = UBFM X2, hi, #(64-n), #(63-n)
	// ORR lo, lo, X2
	// LSR hi, hi, #n  = UBFM hi, hi, #n, #63
	g.cb.emit(encUBFM(lo, lo, n, 63))                 // lo = lo >> n
	g.cb.emit(encUBFM(regX2, hi, 64-n, 63-n))        // X2 = hi << (64-n)
	g.cb.emit(encORR(lo, lo, regX2))                  // lo |= X2
	g.cb.emit(encUBFM(hi, hi, n, 63))                 // hi = hi >> n
}

// emit128Cmp emits a branchless 128-bit comparison using SUBS + SBCS.
//
// For LT/LE (a < b, a <= b): compute a - b via SUBS a_lo, b_lo; SBCS a_hi, b_hi
//   Then CSET with LO/LS (unsigned) or LT/LE (signed).
//
// For GT/GE (a > b, a >= b): equivalent to b < a / b <= a, so swap operands:
//   SUBS b_lo, a_lo; SBCS b_hi, a_hi; CSET LO/LS (unsigned) or LT/LE (signed).
//
// swapOps: true for GT/GE (swap Src1 and Src2 before subtraction).
// finalCond: the ARM64 condition for the desired relation after the swap.
func (g *elfGen) emit128Cmp(q Quad, swapOps bool, finalCond int) {
	g.load128(q.Src1, regX0, regX1) // X0=a_lo, X1=a_hi
	g.load128(q.Src2, regX2, regX3) // X2=b_lo, X3=b_hi
	if swapOps {
		// Compute b - a: SUBS b_lo, a_lo; SBCS b_hi, a_hi
		g.cb.emit(encSUBS(regXZR, regX2, regX0))
		g.cb.emit(encSBCS(regXZR, regX3, regX1))
	} else {
		// Compute a - b: SUBS a_lo, b_lo; SBCS a_hi, b_hi
		g.cb.emit(encSUBS(regXZR, regX0, regX2))
		g.cb.emit(encSBCS(regXZR, regX1, regX3))
	}
	g.cb.emit(encCSET(regX0, finalCond))
	g.store(regX0, q.Dst)
}

func (g *elfGen) emitArrayLoad(q Quad) {
	g.arrayBase(q.Src1, regX0)              // X0 = base address
	g.load(q.Src2, regX1)                   // X1 = index
	g.cb.emit(encLSLimm(regX1, regX1, 3))  // X1 = index * 8
	g.cb.emit(encADDreg(regX0, regX0, regX1))
	if isFPType(q.TypeHint) {
		g.cb.emit(encLDRDuoff(0, regX0, 0)) // D0 = *X0 (FP load)
		g.fpStore(0, q.Dst)
	} else {
		g.cb.emit(encLDRuoff(regX0, regX0, 0)) // X0 = *X0
		g.store(regX0, q.Dst)
	}
}

func (g *elfGen) emitArrayStore(q Quad) {
	g.arrayBase(q.Dst, regX0)               // X0 = base address
	g.load(q.Src1, regX1)                   // X1 = index
	g.cb.emit(encLSLimm(regX1, regX1, 3))  // X1 = index * 8
	g.cb.emit(encADDreg(regX0, regX0, regX1))
	if isFPType(q.TypeHint) {
		g.fpLoad(q.Src2, 0)                  // D0 = value
		g.cb.emit(encSTRDuoff(0, regX0, 0)) // *X0 = D0 (FP store)
	} else {
		g.load(q.Src2, regX2)                   // X2 = value
		g.cb.emit(encSTRuoff(regX2, regX0, 0)) // *X0 = X2
	}
}

// emitCharArrayLoad emits: Dst = Src1[Src2]  (char* byte-level load, no stride scaling)
func (g *elfGen) emitCharArrayLoad(q Quad) {
	g.arrayBase(q.Src1, regX0)               // X0 = base address
	g.load(q.Src2, regX1)                    // X1 = byte index (no scaling)
	g.cb.emit(encADDreg(regX0, regX0, regX1))
	g.cb.emit(encLDRBuoff(regX0, regX0, 0)) // X0 = byte (zero-extended)
	g.store(regX0, q.Dst)
}

// emitCharArrayStore emits: Dst[Src1] = Src2  (char* byte-level store, no stride scaling)
func (g *elfGen) emitCharArrayStore(q Quad) {
	g.arrayBase(q.Dst, regX0)                // X0 = base address
	g.load(q.Src1, regX1)                    // X1 = byte index (no scaling)
	g.cb.emit(encADDreg(regX0, regX0, regX1))
	g.load(q.Src2, regX2)                    // X2 = value (low byte stored)
	g.cb.emit(encSTRBuoff(regX2, regX0, 0)) // store byte
}

func (g *elfGen) emitCall(q Quad) {
	// __va_start builtin: returns SP + (16 + 8*nRegSlots) without a real call.
	// nRegSlots counts actual integer registers consumed by named params.
	if q.Extra == "__va_start" {
		g.pendingParams = g.pendingParams[:0]
		fn := g.fn
		regSlots := 0
		for i := range fn.Params {
			if i < len(fn.ParamType) && fn.ParamType[i] == TypeStruct {
				tag := ""
				if i < len(fn.ParamStructTag) {
					tag = fn.ParamStructTag[i]
				}
				size := 0
				if sd, ok := g.structDefs[tag]; ok {
					size = sd.SizeBytes(g.structDefs)
				}
				if size > 8 && size <= 16 {
					regSlots += 2
				} else {
					regSlots++
				}
			} else if i < len(fn.ParamType) && !isFPType(fn.ParamType[i]) {
				regSlots++
			}
		}
		offset := 16 + 8*regSlots
		g.cb.emit(encADDimm(regX0, regSP, offset))
		if q.Dst.Kind != AddrNone {
			g.store(regX0, q.Dst)
		}
		return
	}

	// Route integer params to X0–X7 and FP params to D0–D7 (separate counters,
	// per AAPCS64: each register class has its own next-register index).
	iIdx, fIdx := 0, 0
	for _, p := range g.pendingParams {
		if p.isStruct {
			sd := g.structDefs[p.structTag]
			size := 0
			if sd != nil {
				size = sd.SizeBytes(g.structDefs)
			}
			switch {
			case size <= 8:
				g.structBase(p.addr, regX9)
				g.cb.emit(encLDRuoff(iIdx, regX9, 0))
				iIdx++
			case size <= 16:
				g.structBase(p.addr, regX9)
				g.cb.emit(encLDRuoff(iIdx, regX9, 0))
				g.cb.emit(encLDRuoff(iIdx+1, regX9, 8))
				iIdx += 2
			default:
				// p.addr is inline frame storage; its address IS the caller-owned copy
				g.structBase(p.addr, iIdx)
				iIdx++
			}
		} else if p.isFP {
			if fIdx < 8 {
				g.fpLoad(p.addr, fIdx)
				fIdx++
			}
		} else {
			if iIdx < 8 {
				g.load(p.addr, iIdx)
				iIdx++
			}
		}
	}
	g.pendingParams = g.pendingParams[:0]

	// Struct-returning call: set X8 (>16 bytes) or unpack X0/X1 after BL (≤16 bytes).
	if q.TypeHint == TypeStruct && q.StructTag != "" {
		sd := g.structDefs[q.StructTag]
		size := 0
		if sd != nil {
			size = sd.SizeBytes(g.structDefs)
		}
		if size > 16 {
			// X8 = destination address; callee writes through it.
			g.arrayBase(q.Src2, regX8)
		}
		label := funcLabel(q.Extra)
		if g.isObjMode {
			if g.localFuncs[q.Extra] {
				g.cb.emitBL(label)
			} else {
				g.cb.emitBLextern(label)
			}
		} else {
			g.cb.emitBL(label)
		}
		if size <= 8 {
			// Recompute dest address into X9 (X8–X17 may be clobbered by callee).
			g.arrayBase(q.Src2, regX9)
			g.cb.emit(encSTRuoff(regX0, regX9, 0))
		} else if size <= 16 {
			g.arrayBase(q.Src2, regX9)
			g.cb.emit(encSTRuoff(regX0, regX9, 0))
			g.cb.emit(encSTRuoff(regX1, regX9, 8))
		}
		// size > 16: callee wrote through X8 already; nothing to do.
		return
	}

	if g.isObjMode {
		switch q.Extra {
		case "input":
			g.cb.emitBLextern("gaston_input")
		case "output":
			g.cb.emitBLextern("gaston_output")
		case "print_char":
			g.cb.emitBLextern("gaston_print_char")
		case "print_string":
			g.cb.emitBLextern("gaston_print_string")
		case "print_double":
			g.cb.emitBLextern("gaston_print_double")
		case "fflush":
			g.cb.emitBLextern("gaston_fflush")
		case "malloc":
			g.cb.emitBLextern("gaston_malloc")
		case "free":
			g.cb.emitBLextern("gaston_free")
		default:
			label := funcLabel(q.Extra)
			if g.localFuncs[q.Extra] {
				g.cb.emitBL(label)
			} else {
				g.cb.emitBLextern(label)
			}
		}
	} else {
		switch q.Extra {
		case "input":
			g.cb.emitBL("gaston_input")
		case "output":
			g.cb.emitBL("gaston_output")
		case "print_char":
			g.cb.emitBL("gaston_print_char")
		case "print_string":
			g.cb.emitBL("gaston_print_string")
		case "print_double":
			g.cb.emitBL("gaston_print_double")
		case "fflush":
			g.cb.emitBL("gaston_fflush")
		case "malloc":
			g.cb.emitBL("gaston_malloc")
		case "free":
			g.cb.emitBL("gaston_free")
		default:
			g.cb.emitBL(funcLabel(q.Extra))
		}
	}

	if q.Dst.Kind != AddrNone {
		if isFPType(g.funcRetType[q.Extra]) {
			g.fpStore(0, q.Dst) // FP return value from D0
		} else {
			g.store(regX0, q.Dst)
		}
	}
}

// ── runtime helper functions ──────────────────────────────────────────────────

// emitStart emits the _start entry point:
//   [global initializer stores]
//   BL gaston_main
//   MOV X0, #0 / MOV X8, #94 / SVC #0  (exit_group(0))
func (g *elfGen) emitStart(globals []IRGlobal) {
	g.cb.defineLabel("_start")
	// Initialize globals that have a constant initializer.
	for _, gbl := range globals {
		if gbl.IsArr {
			continue
		}
		if gbl.HasInitVal {
			g.cb.emitMOVimm(regX0, int64(gbl.InitVal)) // value → X0
			g.cb.emitLDRglobal(gbl.Name)               // X9 = &global
			g.cb.emit(encSTRuoff(regX0, regX9, 0))     // *X9 = value
		} else if len(gbl.InitData) > 0 {
			g.cb.emitLDRglobal(gbl.Name) // X9 = &global
			data := gbl.InitData
			off := 0
			for off+8 <= len(data) {
				var word uint64
				for i := 0; i < 8; i++ {
					word |= uint64(data[off+i]) << (uint(i) * 8)
				}
				if word != 0 {
					g.cb.emitMOVimm(regX0, int64(word))
					g.cb.emit(encSTRuoff(regX0, regX9, off))
				}
				off += 8
			}
			// Handle sub-8-byte tail (shouldn't be needed for aligned structs).
			for ; off < len(data); off++ {
				if data[off] != 0 {
					g.cb.emitMOVimm(regX0, int64(data[off]))
					g.cb.emit(encSTRBuoff(regX0, regX9, off))
				}
			}
		}
		// Pointer relocations: store string literal addresses at specified offsets.
		for _, rel := range gbl.InitRelocs {
			g.cb.emitLDRglobal(gbl.Name) // X9 = &global
			g.cb.emitLDRglobal(rel.Label) // X9 = &string (clobbers X9)
			g.cb.emit(encMOVreg(regX0, regX9)) // X0 = string addr
			g.cb.emitLDRglobal(gbl.Name) // X9 = &global again
			g.cb.emit(encSTRuoff(regX0, regX9, rel.ByteOff)) // global[off] = string addr
		}
	}
	g.cb.emitBL(funcLabel("main"))
	g.cb.emitMOVimm(regX0, 0)  // exit code 0
	g.cb.emitMOVimm(regX8, 94) // exit_group
	g.cb.emit(encSVC(0))
}

// emitOutputFn emits gaston_output(X0 = int64 value).
//
// Frame layout (64 bytes):
//   SP+0:  FP, SP+8: LR
//   SP+16: X19 (abs(value) — decremented in loop)
//   SP+24: X20 (sign flag: 0=positive, 1=negative)
//   SP+32: X21 (write pointer into buffer)
//   SP+40..SP+63: 24-byte char buffer (max "-9223372036854775808\n" = 22 chars)
func (g *elfGen) emitOutputFn() {
	cb := g.cb
	cb.defineLabel("gaston_output")

	// Prologue — save callee-saved registers.
	cb.emit(encSUBimm(regSP, regSP, 64))
	cb.emit(encSTP(regFP, regLR, regSP, 0))
	cb.emit(encSTP(regX19, regX20, regSP, 16))
	cb.emit(encSTRuoff(regX21, regSP, 32))
	cb.emit(encADDimm(regFP, regSP, 0))

	// Set up: X19 = abs(value), X20 = sign flag, X21 = &buf[23]
	cb.emit(encMOVreg(regX19, regX0))                // X19 = value
	cb.emitMOVimm(regX20, 0)                         // X20 = 0 (positive)
	cb.emit(encCMPimm0(regX19))
	cb.emitBcond(condGE, "out_pos")
	cb.emit(encNEG(regX19, regX19))                  // X19 = abs(value)
	cb.emitMOVimm(regX20, 1)                         // X20 = 1 (negative)
	cb.defineLabel("out_pos")
	cb.emit(encADDimm(regX21, regSP, 63))            // X21 = &buf[23]

	// Write '\n' at buf[23], then back up.
	cb.emitMOVimm(regX0, '\n')
	cb.emit(encSTRBuoff(regX0, regX21, 0))
	cb.emit(encSUBimm(regX21, regX21, 1))

	// Special case: value == 0.
	cb.emitCBNZ(regX19, "out_nonzero")
	cb.emitMOVimm(regX0, '0')
	cb.emit(encSTRBuoff(regX0, regX21, 0))
	cb.emit(encSUBimm(regX21, regX21, 1))
	cb.emitB("out_done_digits")

	// Digit-extraction loop: divide by 10 until X19 == 0.
	cb.defineLabel("out_nonzero")
	cb.emitMOVimm(regX0, 10) // X0 = divisor (constant in loop)
	cb.defineLabel("out_loop")
	cb.emit(encUDIV(regX2, regX19, regX0))           // X2 = X19 / 10
	cb.emit(encMUL(regX3, regX2, regX0))             // X3 = X2 * 10
	cb.emit(encSUBreg(regX3, regX19, regX3))         // X3 = X19 mod 10
	cb.emit(encADDimm(regX3, regX3, '0'))            // X3 = ASCII digit
	cb.emit(encSTRBuoff(regX3, regX21, 0))           // *X21 = digit
	cb.emit(encSUBimm(regX21, regX21, 1))            // X21--
	cb.emit(encMOVreg(regX19, regX2))                // X19 = X19 / 10
	cb.emitCBNZ(regX19, "out_loop")

	cb.defineLabel("out_done_digits")
	cb.emit(encADDimm(regX21, regX21, 1)) // X21 = first digit position

	// Prepend '-' if negative.
	cb.emitCBZ(regX20, "out_write")
	cb.emitMOVimm(regX0, '-')
	cb.emit(encSUBimm(regX21, regX21, 1))
	cb.emit(encSTRBuoff(regX0, regX21, 0))

	// write(1, X21, SP+64−X21).
	cb.defineLabel("out_write")
	cb.emit(encMOVreg(regX1, regX21))               // X1 = buf start
	cb.emit(encADDimm(regX2, regSP, 64))            // X2 = SP+64 (past-end)
	cb.emit(encSUBreg(regX2, regX2, regX1))         // X2 = length
	cb.emitMOVimm(regX0, 1)                         // fd = stdout
	cb.emitMOVimm(regX8, 64)                        // write syscall
	cb.emit(encSVC(0))

	// Epilogue.
	cb.emit(encLDP(regX19, regX20, regSP, 16))
	cb.emit(encLDRuoff(regX21, regSP, 32))
	cb.emit(encLDP(regFP, regLR, regSP, 0))
	cb.emit(encADDimm(regSP, regSP, 64))
	cb.emit(encRET())
}

// emitInputFn emits gaston_input() → X0 = int64 read from stdin.
//
// Reads one byte at a time so that sequential calls to gaston_input()
// each consume exactly the bytes they use, leaving the file offset
// positioned correctly for the next call.
//
// Frame layout (64 bytes):
//   SP+0: FP, SP+8: LR
//   SP+16: X19 (result accumulator)
//   SP+24: X20 (sign flag: 0=positive, 1=negative)
//   SP+32: X21 (address of the single-byte read buffer at SP+40)
//   SP+40: 1-byte read buffer
//   SP+41..SP+63: padding
func (g *elfGen) emitInputFn() {
	cb := g.cb
	cb.defineLabel("gaston_input")

	// Prologue.
	cb.emit(encSUBimm(regSP, regSP, 64))
	cb.emit(encSTP(regFP, regLR, regSP, 0))
	cb.emit(encSTP(regX19, regX20, regSP, 16))
	cb.emit(encSTRuoff(regX21, regSP, 32))
	cb.emit(encADDimm(regFP, regSP, 0))

	// Initialise state.
	cb.emitMOVimm(regX19, 0)              // result = 0
	cb.emitMOVimm(regX20, 0)              // sign = positive
	cb.emit(encADDimm(regX21, regSP, 40)) // X21 = &buf[0]  (1-byte buffer)

	// Skip non-digit, non-sign characters (whitespace, newlines, etc.).
	// Each iteration reads exactly one byte from stdin.
	cb.defineLabel("in_scan")
	cb.emitMOVimm(regX0, 0)              // fd = stdin
	cb.emit(encMOVreg(regX1, regX21))    // buf = &buf[0]
	cb.emitMOVimm(regX2, 1)             // count = 1
	cb.emitMOVimm(regX8, 63)            // sys_read
	cb.emit(encSVC(0))
	cb.emitCBZ(regX0, "in_done")         // 0 bytes read = EOF

	cb.emit(encLDRBuoff(regX2, regX21, 0)) // X2 = byte read
	// Check for '-'
	cb.emitMOVimm(regX3, '-')
	cb.emit(encCMPreg(regX2, regX3))
	cb.emitBcond(condEQ, "in_minus")
	// Check if >= '0' (potential digit)
	cb.emitMOVimm(regX3, '0')
	cb.emit(encCMPreg(regX2, regX3))
	cb.emitBcond(condGE, "in_digit") // W2 >= '0': try digit
	// Not digit, not '-': skip and read next byte.
	cb.emitB("in_scan")

	// Found '-': set sign flag, continue scanning for first digit.
	cb.defineLabel("in_minus")
	cb.emitMOVimm(regX20, 1)
	cb.emitB("in_scan")

	// in_digit: X2 holds current char, already checked >= '0'.
	// Validate <= '9' then accumulate; then read the next byte and loop.
	cb.defineLabel("in_digit")
	cb.emitMOVimm(regX3, '9')
	cb.emit(encCMPreg(regX2, regX3))
	cb.emitBcond(condGT, "in_done")      // > '9': stop (not a real digit)
	cb.emit(encSUBimm(regX2, regX2, '0')) // digit value
	cb.emitMOVimm(regX3, 10)
	cb.emit(encMUL(regX19, regX19, regX3))  // result *= 10
	cb.emit(encADDreg(regX19, regX19, regX2)) // result += digit

	// Read the next byte for the next iteration.
	cb.emitMOVimm(regX0, 0)
	cb.emit(encMOVreg(regX1, regX21))
	cb.emitMOVimm(regX2, 1)
	cb.emitMOVimm(regX8, 63)
	cb.emit(encSVC(0))
	cb.emitCBZ(regX0, "in_done") // EOF: stop

	cb.emit(encLDRBuoff(regX2, regX21, 0)) // X2 = next byte
	cb.emitMOVimm(regX3, '0')
	cb.emit(encCMPreg(regX2, regX3))
	cb.emitBcond(condLT, "in_done") // < '0': stop
	cb.emitMOVimm(regX3, '9')
	cb.emit(encCMPreg(regX2, regX3))
	cb.emitBcond(condGT, "in_done") // > '9': stop
	cb.emitB("in_digit")             // continue accumulating

	cb.defineLabel("in_done")
	cb.emitCBZ(regX20, "in_return")
	cb.emit(encNEG(regX19, regX19))

	cb.defineLabel("in_return")
	cb.emit(encMOVreg(regX0, regX19))

	// Epilogue.
	cb.emit(encLDP(regX19, regX20, regSP, 16))
	cb.emit(encLDRuoff(regX21, regSP, 32))
	cb.emit(encLDP(regFP, regLR, regSP, 0))
	cb.emit(encADDimm(regSP, regSP, 64))
	cb.emit(encRET())
}

// emitFflushFn emits gaston_fflush(X0 = FILE*, ignored).
//
// Calls fdatasync(1) to flush the Linux shepherd's line buffer for stdout.
// Returns 0 on success, -1 on error (from fdatasync return value).
// Frame layout (32 bytes): SP+0: FP, SP+8: LR, SP+16..31: pad.
func (g *elfGen) emitFflushFn() {
	cb := g.cb
	cb.defineLabel("gaston_fflush")

	cb.emit(encSUBimm(regSP, regSP, 32))
	cb.emit(encSTP(regFP, regLR, regSP, 0))
	cb.emit(encADDimm(regFP, regSP, 0))

	// fdatasync(1)
	cb.emitMOVimm(regX0, 1)  // fd = stdout
	cb.emitMOVimm(regX8, 83) // sys_fdatasync (ARM64)
	cb.emit(encSVC(0))

	cb.emit(encLDP(regFP, regLR, regSP, 0))
	cb.emit(encADDimm(regSP, regSP, 32))
	cb.emit(encRET())
}

// emitPrintCharFn emits gaston_print_char(X0 = character as int64).
//
// Writes the low byte of X0 to stdout via write(1, &buf, 1).
// Frame layout (32 bytes): SP+0: FP, SP+8: LR, SP+16: 1-byte buf, SP+17..31: pad.
func (g *elfGen) emitPrintCharFn() {
	cb := g.cb
	cb.defineLabel("gaston_print_char")

	cb.emit(encSUBimm(regSP, regSP, 32))
	cb.emit(encSTP(regFP, regLR, regSP, 0))
	cb.emit(encADDimm(regFP, regSP, 0))

	// Store low byte of X0 into buf at SP+16.
	cb.emit(encSTRBuoff(regX0, regSP, 16))

	// write(1, SP+16, 1)
	cb.emitMOVimm(regX0, 1)                   // fd = stdout
	cb.emit(encADDimm(regX1, regSP, 16))      // buf = &SP[16]
	cb.emitMOVimm(regX2, 1)                   // count = 1
	cb.emitMOVimm(regX8, 64)                  // sys_write
	cb.emit(encSVC(0))

	cb.emit(encLDP(regFP, regLR, regSP, 0))
	cb.emit(encADDimm(regSP, regSP, 32))
	cb.emit(encRET())
}

// emitPrintStringFn emits gaston_print_string(X0 = pointer to null-terminated string).
//
// Scans forward from X0 to find the null terminator, then calls write(1, X0, len).
// Frame layout (48 bytes): SP+0: FP, SP+8: LR, SP+16: X19 (base ptr), SP+24: X20 (scan ptr).
func (g *elfGen) emitPrintStringFn() {
	cb := g.cb
	cb.defineLabel("gaston_print_string")

	cb.emit(encSUBimm(regSP, regSP, 48))
	cb.emit(encSTP(regFP, regLR, regSP, 0))
	cb.emit(encSTP(regX19, regX20, regSP, 16))
	cb.emit(encADDimm(regFP, regSP, 0))

	// X19 = base (start of string), X20 = scan pointer.
	cb.emit(encMOVreg(regX19, regX0))
	cb.emit(encMOVreg(regX20, regX0))

	// Scan loop: load byte at X20, if zero stop, else X20++.
	cb.defineLabel("ps_scan")
	cb.emit(encLDRBuoff(regX0, regX20, 0))    // X0 = *X20
	cb.emitCBZ(regX0, "ps_write")             // if zero → done
	cb.emit(encADDimm(regX20, regX20, 1))     // X20++
	cb.emitB("ps_scan")

	// write(1, X19, X20-X19).
	cb.defineLabel("ps_write")
	cb.emit(encSUBreg(regX2, regX20, regX19)) // X2 = length
	cb.emitCBZ(regX2, "ps_ret")               // skip write if empty
	cb.emitMOVimm(regX0, 1)                   // fd = stdout
	cb.emit(encMOVreg(regX1, regX19))         // buf = base
	cb.emitMOVimm(regX8, 64)                  // sys_write
	cb.emit(encSVC(0))

	cb.defineLabel("ps_ret")
	cb.emit(encLDP(regX19, regX20, regSP, 16))
	cb.emit(encLDP(regFP, regLR, regSP, 0))
	cb.emit(encADDimm(regSP, regSP, 48))
	cb.emit(encRET())
}

// emitPrintDoubleFn emits gaston_print_double(D0 = double value).
//
// Prints the value as "[-]integer.fraction\n" with 6 decimal places.
//
// Frame layout (80 bytes):
//   SP+0:  FP       SP+8:  LR
//   SP+16: X19 (integer part of |D0|)
//   SP+24: X20 (fractional part scaled to 6 digits)
//   SP+32: X21 (digit buffer pointer)
//   SP+40: X22 (digit loop counter)
//   SP+48..SP+79: 32-byte scratch buffer
//     SP+48:       1-byte scratch (sign char, '.', '\n')
//     SP+49..SP+79: 31-byte integer digit buffer (built right-to-left)
//     SP+48..SP+53: 6-byte fractional digit buffer (reused after int write)
func (g *elfGen) emitPrintDoubleFn() {
	cb := g.cb
	cb.defineLabel("gaston_print_double")

	// Prologue.
	cb.emit(encSUBimm(regSP, regSP, 80))
	cb.emit(encSTP(regFP, regLR, regSP, 0))
	cb.emit(encSTP(regX19, regX20, regSP, 16))
	cb.emit(encSTP(regX21, 22, regSP, 32)) // X21, X22
	cb.emit(encADDimm(regFP, regSP, 0))

	// Step 1: handle sign — FCMP D0, #0.0; if ≥ 0 skip sign printing.
	cb.emit(encFCMPDzero(0))
	cb.emitBcond(condGE, "pd_pos")
	cb.emitMOVimm(regX0, '-')
	cb.emit(encSTRBuoff(regX0, regSP, 48))
	cb.emitMOVimm(regX0, 1)
	cb.emit(encADDimm(regX1, regSP, 48))
	cb.emitMOVimm(regX2, 1)
	cb.emitMOVimm(regX8, 64)
	cb.emit(encSVC(0))
	cb.emit(encFNEGD(0, 0)) // D0 = |D0|
	cb.defineLabel("pd_pos")

	// Step 2: extract integer part into X19.
	cb.emit(encFCVTZSD(regX19, 0)) // X19 = (int64)D0

	// Step 3: compute fractional part in D1 = D0 − (double)X19.
	cb.emit(encSCVTFD(1, regX19)) // D1 = (double)X19
	cb.emit(encFSUBD(1, 0, 1))   // D1 = D0 − D1

	// Step 4: scale fractional to 6 digits in X20.
	cb.emitMOVimm(regX0, 1000000)
	cb.emit(encSCVTFD(2, regX0))   // D2 = 1000000.0
	cb.emit(encFMULD(1, 1, 2))     // D1 = D1 * 1000000.0
	cb.emit(encFCVTZSD(regX20, 1)) // X20 = 6-digit fractional

	// Step 5: print integer part (X19) — build digits backward into SP+49..SP+79.
	cb.emit(encADDimm(regX21, regSP, 79)) // X21 = &buf[31]
	cb.emitCBNZ(regX19, "pd_int_nonzero")
	// X19 == 0: emit single '0'.
	cb.emitMOVimm(regX0, '0')
	cb.emit(encSTRBuoff(regX0, regX21, 0))
	cb.emit(encSUBimm(regX21, regX21, 1))
	cb.emitB("pd_int_done")
	// X19 != 0: digit-extract loop.
	cb.defineLabel("pd_int_nonzero")
	cb.emitMOVimm(regX0, 10) // divisor in X0 (constant through loop)
	cb.defineLabel("pd_int_loop")
	cb.emit(encUDIV(regX2, regX19, regX0))    // X2  = X19 / 10
	cb.emit(encMUL(3, regX2, regX0))          // X3  = quotient * 10
	cb.emit(encSUBreg(3, regX19, 3))          // X3  = X19 mod 10
	cb.emit(encADDimm(3, 3, '0'))             // X3  = ASCII digit
	cb.emit(encSTRBuoff(3, regX21, 0))        // *X21 = digit
	cb.emit(encSUBimm(regX21, regX21, 1))     // X21--
	cb.emit(encMOVreg(regX19, regX2))         // X19 = X19 / 10
	cb.emitCBNZ(regX19, "pd_int_loop")
	cb.defineLabel("pd_int_done")
	cb.emit(encADDimm(regX21, regX21, 1)) // X21 = first digit
	// write(1, X21, SP+80 − X21)
	cb.emit(encMOVreg(regX1, regX21))
	cb.emit(encADDimm(regX2, regSP, 80))
	cb.emit(encSUBreg(regX2, regX2, regX1))
	cb.emitMOVimm(regX0, 1)
	cb.emitMOVimm(regX8, 64)
	cb.emit(encSVC(0))

	// Step 6: print '.'.
	cb.emitMOVimm(regX0, '.')
	cb.emit(encSTRBuoff(regX0, regSP, 48))
	cb.emitMOVimm(regX0, 1)
	cb.emit(encADDimm(regX1, regSP, 48))
	cb.emitMOVimm(regX2, 1)
	cb.emitMOVimm(regX8, 64)
	cb.emit(encSVC(0))

	// Step 7: print 6 fractional digits (zero-padded) from X20.
	// Build backward into SP+48..SP+53 (least-significant digit at SP+53).
	cb.emit(encADDimm(regX21, regSP, 53)) // X21 = SP+53 (last slot)
	cb.emitMOVimm(22, 6)                  // X22 = 6 (counter)
	cb.emitMOVimm(regX0, 10)
	cb.defineLabel("pd_frac_loop")
	cb.emit(encUDIV(regX2, regX20, regX0))  // X2  = X20 / 10
	cb.emit(encMUL(3, regX2, regX0))        // X3  = quotient * 10
	cb.emit(encSUBreg(3, regX20, 3))        // X3  = X20 mod 10
	cb.emit(encADDimm(3, 3, '0'))           // X3  = ASCII digit
	cb.emit(encSTRBuoff(3, regX21, 0))      // *X21 = digit
	cb.emit(encSUBimm(regX21, regX21, 1))   // X21--
	cb.emit(encMOVreg(regX20, regX2))       // X20 = X20 / 10
	cb.emit(encSUBimm(22, 22, 1))           // X22--
	cb.emitCBNZ(22, "pd_frac_loop")
	// write(1, SP+48, 6)
	cb.emitMOVimm(regX0, 1)
	cb.emit(encADDimm(regX1, regSP, 48))
	cb.emitMOVimm(regX2, 6)
	cb.emitMOVimm(regX8, 64)
	cb.emit(encSVC(0))

	// Step 8: print '\n'.
	cb.emitMOVimm(regX0, '\n')
	cb.emit(encSTRBuoff(regX0, regSP, 48))
	cb.emitMOVimm(regX0, 1)
	cb.emit(encADDimm(regX1, regSP, 48))
	cb.emitMOVimm(regX2, 1)
	cb.emitMOVimm(regX8, 64)
	cb.emit(encSVC(0))

	// Epilogue.
	cb.emit(encLDP(regX21, 22, regSP, 32))
	cb.emit(encLDP(regX19, regX20, regSP, 16))
	cb.emit(encLDP(regFP, regLR, regSP, 0))
	cb.emit(encADDimm(regSP, regSP, 80))
	cb.emit(encRET())
}

// freeListGlobalName is the synthetic BSS global used as the allocator's
// free-list head pointer.  It is registered in the literal pool so that
// emitMallocFn / emitFreeFn can load its address via emitLDRglobal, just
// like any user-declared global.  The slot is zero-initialised by the OS
// because it lives in the BSS segment.
const freeListGlobalName = "gaston_free_list_head"

// ── boundary-tag coalescing allocator ─────────────────────────────────────────
//
// Block layout (every block, free or allocated):
//
//	[+0]  header  8 bytes: (block_size | alloc_bit)
//	              block_size includes header + payload + footer, always ≥ 32,
//	              always a multiple of 8.  alloc_bit = bit 0: 1=allocated, 0=free.
//	[+8]  payload (or, when free: next_free ptr 8 bytes)
//	[+16] …       (or, when free: prev_free ptr 8 bytes)
//	…
//	[size-8]  footer 8 bytes: identical to header
//
// The user pointer returned by malloc points to [+8] (payload start).
//
// Each 1 MB slab is initialised with:
//   [+0..+15]   prologue  (allocated sentinel, size=16, alloc=1)
//   [+16..end-9] one big free block
//   [end-8..end-1] epilogue (allocated sentinel, size=0, alloc=1)
//
// The prologue prevents backward coalescing past the slab start; the
// epilogue (size=0) stops forward coalescing at the slab end.
//
// Register conventions inside malloc/free:
//   X19 = n_bytes (malloc) / block_ptr (free)
//   X20 = block_size_needed (malloc) / current coalesced size (free)
//   X21 = &gaston_free_list_slot  (constant throughout the function)
//   X22 = current/chosen block pointer
//   X23 = confirmed block size (malloc found_block section)

const allocSlabSize = 1 << 20 // 1 MB

// emitMallocFn emits gaston_malloc(X0 = n_bytes) → X0 = user_ptr.
//
// First-fit search of the explicit free list; splits large blocks; lazy-mmaps
// a new 1 MB slab when the list has no block large enough.
func (g *elfGen) emitMallocFn() {
	cb := g.cb
	cb.defineLabel("gaston_malloc")

	// ── prologue (64-byte frame, save X19-X23) ────────────────────────────
	cb.emit(encSUBimm(regSP, regSP, 64))
	cb.emit(encSTP(regFP, regLR, regSP, 0))
	cb.emit(encSTP(regX19, regX20, regSP, 16))
	cb.emit(encSTP(regX21, regX22, regSP, 32))
	cb.emit(encSTP(regX23, 24 /*X24*/, regSP, 48)) // save X24 as spare callee pair
	cb.emit(encADDimm(regFP, regSP, 0))

	cb.emit(encMOVreg(regX19, regX0)) // X19 = n_bytes

	// ── compute block_size_needed → X20 ──────────────────────────────────
	// payload  = max(roundup8(n_bytes), 16)
	// block_sz = payload + 16  (header + footer)
	cb.emitMOVimm(regX1, 16)
	cb.emit(encCMPreg(regX19, regX1))
	cb.emitBcond(condGE, "gaston_malloc_size_ok")
	cb.emit(encMOVreg(regX0, regX1)) // X0 = 16 (minimum payload)
	cb.emitB("gaston_malloc_size_done")
	cb.defineLabel("gaston_malloc_size_ok")
	cb.emit(encMOVreg(regX0, regX19)) // X0 = n_bytes
	cb.defineLabel("gaston_malloc_size_done")
	cb.emit(encADDimm(regX0, regX0, 7))   // X0 += 7
	cb.emit(encLSRimm(regX0, regX0, 3))   // X0 >>= 3
	cb.emit(encLSLimm(regX0, regX0, 3))   // X0 <<= 3  → roundup8(payload)
	cb.emit(encADDimm(regX20, regX0, 16)) // X20 = block_size_needed

	// ── load free-list head address ───────────────────────────────────────
	// gaston_free_list_head lives in the BSS segment (RW); load its VA via
	// the literal pool, then read the current head pointer.
	cb.emitLDRglobal(freeListGlobalName)        // X9 = &gaston_free_list_head
	cb.emit(encMOVreg(regX21, regX9))           // X21 = &gaston_free_list_head
	cb.emit(encLDRuoff(regX22, regX21, 0))      // X22 = *X21 = free_list_head

	// ── first-fit search ──────────────────────────────────────────────────
	cb.defineLabel("gaston_malloc_search")
	cb.emitCBZ(regX22, "gaston_malloc_new_slab")
	cb.emit(encLDRuoff(regX0, regX22, 0)) // X0 = header
	cb.emit(encLSRimm(regX1, regX0, 1))   // X1 = header >> 1
	cb.emit(encLSLimm(regX1, regX1, 1))   // X1 = block_size (alloc bit cleared)
	cb.emit(encCMPreg(regX1, regX20))
	cb.emitBcond(condGE, "gaston_malloc_found")
	cb.emit(encLDRuoff(regX22, regX22, 8)) // X22 = X22.next
	cb.emitB("gaston_malloc_search")

	// ── found a fitting block ─────────────────────────────────────────────
	// X22 = block ptr,  X1 = block_size
	cb.defineLabel("gaston_malloc_found")
	cb.emit(encMOVreg(regX23, regX1)) // X23 = block_size (save)

	// Check split condition: remainder = block_size - block_size_needed ≥ 32
	cb.emit(encSUBreg(regX2, regX23, regX20)) // X2 = remainder
	cb.emitMOVimm(regX0, 32)
	cb.emit(encCMPreg(regX2, regX0))
	cb.emitBcond(condLT, "gaston_malloc_use_whole")

	// ── split: carve X20-byte block from front, leave X2-byte block ──────
	cb.emit(encADDreg(regX3, regX22, regX20)) // X3 = new free block ptr

	// new free block header + footer
	cb.emit(encSTRuoff(regX2, regX3, 0)) // new.header = X2
	cb.emit(encADDreg(regX0, regX3, regX2))
	cb.emit(encSUBimm(regX0, regX0, 8))
	cb.emit(encSTRuoff(regX2, regX0, 0)) // new.footer = X2

	// new.next = X22.next;  new.prev = X22.prev
	cb.emit(encLDRuoff(regX0, regX22, 8))  // X0 = X22.next
	cb.emit(encLDRuoff(regX1, regX22, 16)) // X1 = X22.prev
	cb.emit(encSTRuoff(regX0, regX3, 8))   // new.next = X0
	cb.emit(encSTRuoff(regX1, regX3, 16))  // new.prev = X1

	// fix up prev.next → X3 (or free_list_head → X3)
	cb.emitCBZ(regX1, "gaston_malloc_split_fix_head")
	cb.emit(encSTRuoff(regX3, regX1, 8)) // prev.next = X3
	cb.emitB("gaston_malloc_split_fix_next")
	cb.defineLabel("gaston_malloc_split_fix_head")
	cb.emit(encSTRuoff(regX3, regX21, 0)) // free_list_head = X3
	cb.defineLabel("gaston_malloc_split_fix_next")
	// fix up next.prev → X3 (if next != 0)
	cb.emitCBZ(regX0, "gaston_malloc_split_alloc")
	cb.emit(encSTRuoff(regX3, regX0, 16)) // next.prev = X3

	// mark X22 as allocated (size = X20)
	cb.defineLabel("gaston_malloc_split_alloc")
	cb.emit(encADDimm(regX0, regX20, 1))        // X0 = X20 | 1
	cb.emit(encSTRuoff(regX0, regX22, 0))        // X22.header = X20|1
	cb.emit(encADDreg(regX1, regX22, regX20))
	cb.emit(encSUBimm(regX1, regX1, 8))
	cb.emit(encSTRuoff(regX0, regX1, 0)) // X22.footer = X20|1
	cb.emitB("gaston_malloc_ret")

	// ── use whole block (no split) ────────────────────────────────────────
	cb.defineLabel("gaston_malloc_use_whole")
	// mark as allocated (size = X23)
	cb.emit(encADDimm(regX0, regX23, 1))  // X0 = X23 | 1
	cb.emit(encSTRuoff(regX0, regX22, 0)) // header
	cb.emit(encADDreg(regX1, regX22, regX23))
	cb.emit(encSUBimm(regX1, regX1, 8))
	cb.emit(encSTRuoff(regX0, regX1, 0)) // footer

	// remove X22 from free list
	cb.emit(encLDRuoff(regX0, regX22, 8))  // X0 = next
	cb.emit(encLDRuoff(regX1, regX22, 16)) // X1 = prev
	cb.emitCBZ(regX1, "gaston_malloc_whole_fix_head")
	cb.emit(encSTRuoff(regX0, regX1, 8)) // prev.next = next
	cb.emitB("gaston_malloc_whole_fix_next")
	cb.defineLabel("gaston_malloc_whole_fix_head")
	cb.emit(encSTRuoff(regX0, regX21, 0)) // free_list_head = next
	cb.defineLabel("gaston_malloc_whole_fix_next")
	cb.emitCBZ(regX0, "gaston_malloc_ret")
	cb.emit(encSTRuoff(regX1, regX0, 16)) // next.prev = prev

	// ── return user pointer ───────────────────────────────────────────────
	cb.defineLabel("gaston_malloc_ret")
	cb.emit(encADDimm(regX0, regX22, 8)) // X0 = block + 8 (skip header)

	// ── epilogue ──────────────────────────────────────────────────────────
	cb.defineLabel("gaston_malloc_epi")
	cb.emit(encLDP(regX23, 24, regSP, 48))
	cb.emit(encLDP(regX21, regX22, regSP, 32))
	cb.emit(encLDP(regX19, regX20, regSP, 16))
	cb.emit(encLDP(regFP, regLR, regSP, 0))
	cb.emit(encADDimm(regSP, regSP, 64))
	cb.emit(encRET())

	// ── new slab: mmap 1 MB, initialise, prepend to free list ────────────
	cb.defineLabel("gaston_malloc_new_slab")
	cb.emitMOVimm(regX0, 0)
	cb.emitMOVimm(regX1, allocSlabSize)
	cb.emitMOVimm(regX2, 3)    // PROT_READ|PROT_WRITE
	cb.emitMOVimm(regX3, 0x22) // MAP_PRIVATE|MAP_ANONYMOUS
	cb.emit(encMOVN(regX4, 0, 0))
	cb.emitMOVimm(regX5, 0)
	cb.emitMOVimm(regX8, 222) // SYS_MMAP
	cb.emit(encSVC(0))
	// X0 = slab_base
	cb.emit(encMOVreg(regX23, regX0)) // X23 = slab_base

	// prologue: [+0]=16|1, [+8]=16|1
	cb.emitMOVimm(regX0, 17) // 16 | 1
	cb.emit(encSTRuoff(regX0, regX23, 0))
	cb.emit(encSTRuoff(regX0, regX23, 8))

	// first free block at slab_base+16, size = allocSlabSize−24
	cb.emit(encADDimm(regX3, regX23, 16)) // X3 = first free block ptr
	cb.emitMOVimm(regX0, allocSlabSize-24)
	cb.emit(encSTRuoff(regX0, regX3, 0)) // header = size
	// footer at X3 + size − 8
	cb.emit(encADDreg(regX1, regX3, regX0))
	cb.emit(encSUBimm(regX1, regX1, 8))
	cb.emit(encSTRuoff(regX0, regX1, 0)) // footer = size
	// next = old head; prev = 0
	cb.emit(encLDRuoff(regX1, regX21, 0))                   // X1 = old head
	cb.emit(encSTRuoff(regX1, regX3, 8))                    // new.next = old head
	cb.emit(encSTRuoff(regXZR, regX3, 16))                  // new.prev = 0
	cb.emitCBZ(regX1, "gaston_malloc_slab_fix_head")
	cb.emit(encSTRuoff(regX3, regX1, 16)) // old_head.prev = X3
	cb.defineLabel("gaston_malloc_slab_fix_head")
	cb.emit(encSTRuoff(regX3, regX21, 0)) // free_list_head = X3

	// epilogue sentinel at slab_base + allocSlabSize − 8
	cb.emitMOVimm(regX0, 1) // size=0, alloc=1
	cb.emitMOVimm(regX1, allocSlabSize-8)
	cb.emit(encADDreg(regX1, regX23, regX1))
	cb.emit(encSTRuoff(regX0, regX1, 0)) // epilogue header

	// retry search with X22 = new block
	cb.emit(encMOVreg(regX22, regX3))
	cb.emitB("gaston_malloc_search")
}

// emitFreeFn emits gaston_free(X0 = user_ptr).
//
// Marks the block free, coalesces with adjacent free neighbours (O(1) via
// boundary tags), then prepends the result to the free list.
func (g *elfGen) emitFreeFn() {
	cb := g.cb
	cb.defineLabel("gaston_free")

	// ── prologue ──────────────────────────────────────────────────────────
	cb.emit(encSUBimm(regSP, regSP, 64))
	cb.emit(encSTP(regFP, regLR, regSP, 0))
	cb.emit(encSTP(regX19, regX20, regSP, 16))
	cb.emit(encSTP(regX21, regX22, regSP, 32))
	cb.emit(encSTP(regX23, 24 /*X24*/, regSP, 48))
	cb.emit(encADDimm(regFP, regSP, 0))

	cb.emitLDRglobal(freeListGlobalName)    // X9 = &gaston_free_list_head
	cb.emit(encMOVreg(regX21, regX9))      // X21 = &gaston_free_list_head
	cb.emit(encSUBimm(regX19, regX0, 8))        // X19 = block_ptr = user_ptr − 8

	// load header, extract size into X20
	cb.emit(encLDRuoff(regX0, regX19, 0))
	cb.emit(encLSRimm(regX20, regX0, 1))
	cb.emit(encLSLimm(regX20, regX20, 1)) // X20 = block_size

	// mark block free (write size with alloc=0 to header and footer)
	cb.emit(encSTRuoff(regX20, regX19, 0)) // header = size
	cb.emit(encADDreg(regX0, regX19, regX20))
	cb.emit(encSUBimm(regX0, regX0, 8))
	cb.emit(encSTRuoff(regX20, regX0, 0)) // footer = size

	// ── coalesce right ─────────────────────────────────────────────────────
	cb.emit(encADDreg(regX0, regX19, regX20)) // X0 = next_block
	cb.emit(encLDRuoff(regX1, regX0, 0))      // X1 = next.header
	cb.emit(encLSRimm(regX2, regX1, 1))
	cb.emit(encLSLimm(regX2, regX2, 1))   // X2 = next_size (alloc bit cleared)
	cb.emit(encCMPreg(regX1, regX2))
	cb.emitBcond(condNE, "gaston_free_cl") // next is allocated → skip
	cb.emitCBZ(regX2, "gaston_free_cl")   // epilogue sentinel (size=0) → skip

	// remove next_block (X0) from free list
	cb.emit(encLDRuoff(regX22, regX0, 8))  // X22 = next.next
	cb.emit(encLDRuoff(regX23, regX0, 16)) // X23 = next.prev
	cb.emitCBZ(regX23, "gaston_free_cr_head")
	cb.emit(encSTRuoff(regX22, regX23, 8)) // prev.next = next.next
	cb.emitB("gaston_free_cr_next")
	cb.defineLabel("gaston_free_cr_head")
	cb.emit(encSTRuoff(regX22, regX21, 0)) // free_list_head = next.next
	cb.defineLabel("gaston_free_cr_next")
	cb.emitCBZ(regX22, "gaston_free_cr_done")
	cb.emit(encSTRuoff(regX23, regX22, 16)) // next.next.prev = next.prev
	cb.defineLabel("gaston_free_cr_done")

	// extend current block
	cb.emit(encADDreg(regX20, regX20, regX2)) // X20 += next_size
	cb.emit(encSTRuoff(regX20, regX19, 0))    // update header
	cb.emit(encADDreg(regX0, regX19, regX20))
	cb.emit(encSUBimm(regX0, regX0, 8))
	cb.emit(encSTRuoff(regX20, regX0, 0)) // update footer

	// ── coalesce left ──────────────────────────────────────────────────────
	cb.defineLabel("gaston_free_cl")
	cb.emit(encSUBimm(regX0, regX19, 8))  // X0 = prev footer addr
	cb.emit(encLDRuoff(regX1, regX0, 0))  // X1 = prev.footer value
	cb.emit(encLSRimm(regX2, regX1, 1))
	cb.emit(encLSLimm(regX2, regX2, 1))  // X2 = prev_size (alloc bit cleared)
	cb.emit(encCMPreg(regX1, regX2))
	cb.emitBcond(condNE, "gaston_free_add") // prev is allocated → skip
	cb.emitCBZ(regX2, "gaston_free_add")   // prev_size=0? (shouldn't happen, safety)

	// prev_block = X19 − prev_size
	cb.emit(encSUBreg(regX0, regX19, regX2)) // X0 = prev_block ptr

	// remove prev_block (X0) from free list
	cb.emit(encLDRuoff(regX22, regX0, 8))  // X22 = prev.next
	cb.emit(encLDRuoff(regX23, regX0, 16)) // X23 = prev.prev
	cb.emitCBZ(regX23, "gaston_free_cl_head")
	cb.emit(encSTRuoff(regX22, regX23, 8)) // prev.prev.next = prev.next
	cb.emitB("gaston_free_cl_next")
	cb.defineLabel("gaston_free_cl_head")
	cb.emit(encSTRuoff(regX22, regX21, 0)) // free_list_head = prev.next
	cb.defineLabel("gaston_free_cl_next")
	cb.emitCBZ(regX22, "gaston_free_cl_done")
	cb.emit(encSTRuoff(regX23, regX22, 16)) // prev.next.prev = prev.prev
	cb.defineLabel("gaston_free_cl_done")

	// merge current block into prev
	cb.emit(encADDreg(regX20, regX20, regX2)) // X20 += prev_size
	cb.emit(encMOVreg(regX19, regX0))         // X19 = prev_block
	cb.emit(encSTRuoff(regX20, regX19, 0))    // update header
	cb.emit(encADDreg(regX0, regX19, regX20))
	cb.emit(encSUBimm(regX0, regX0, 8))
	cb.emit(encSTRuoff(regX20, regX0, 0)) // update footer

	// ── prepend to free list ───────────────────────────────────────────────
	cb.defineLabel("gaston_free_add")
	cb.emit(encLDRuoff(regX0, regX21, 0))     // X0 = old_head
	cb.emit(encSTRuoff(regX0, regX19, 8))     // new.next = old_head
	cb.emit(encSTRuoff(regXZR, regX19, 16))   // new.prev = 0
	cb.emitCBZ(regX0, "gaston_free_add_head")
	cb.emit(encSTRuoff(regX19, regX0, 16)) // old_head.prev = new_block
	cb.defineLabel("gaston_free_add_head")
	cb.emit(encSTRuoff(regX19, regX21, 0)) // free_list_head = new_block

	// ── epilogue ──────────────────────────────────────────────────────────
	cb.emit(encLDP(regX23, 24, regSP, 48))
	cb.emit(encLDP(regX21, regX22, regSP, 32))
	cb.emit(encLDP(regX19, regX20, regSP, 16))
	cb.emit(encLDP(regFP, regLR, regSP, 0))
	cb.emit(encADDimm(regSP, regSP, 64))
	cb.emit(encRET())
}

// ── genELF entry point ────────────────────────────────────────────────────────

// genELF compiles irp to a Linux ARM64 static ELF binary and writes it to
// outpath. ELF headers use debug/elf struct types serialised with
// encoding/binary (little-endian). The output file is made executable.
func genELF(irp *IRProgram, outpath string) error {
	// --- Phase 1: build BSS layout -----------------------------------------
	type globalInfo struct {
		name   string
		offset uint64 // byte offset in BSS
	}
	var bssList []globalInfo
	var bssTotal uint64
	bssOffset := make(map[string]uint64)
	for _, gbl := range irp.Globals {
		if gbl.IsExtern {
			continue // extern globals have no local storage
		}
		sz := uint64(gbl.Size) * 8
		bssOffset[gbl.Name] = bssTotal
		bssList = append(bssList, globalInfo{gbl.Name, bssTotal})
		bssTotal += sz
	}

	// --- Phase 1b: build rodata layout (string literals) ------------------
	type rodataEntry struct {
		label  string
		bytes  []byte // content + NUL
		offset uint64 // byte offset in rodata
	}
	var rodataList []rodataEntry
	var rodataTotal uint64
	for _, sl := range irp.StrLits {
		b := append([]byte(sl.Content), 0) // NUL-terminate
		rodataList = append(rodataList, rodataEntry{sl.Label, b, rodataTotal})
		rodataTotal += uint64(len(b))
	}

	// --- Phase 2: emit machine code ----------------------------------------
	// Build the isGlobalPtr map for pointer global variables.
	isGlobalPtr := make(map[string]bool)
	for _, gbl := range irp.Globals {
		if gbl.IsPtr {
			isGlobalPtr[gbl.Name] = true
		}
	}

	// Only include non-extern globals in the pool (extern globals are resolved
	// by the linker; in single-file ELF mode they would not exist anyway).
	// Also add a synthetic global for the allocator's free-list head (BSS, 8 bytes).
	definedGlobals := make([]IRGlobal, 0, len(irp.Globals))
	for _, gbl := range irp.Globals {
		if !gbl.IsExtern {
			definedGlobals = append(definedGlobals, gbl)
		}
	}
	// Synthetic global: malloc's free-list head pointer (lives in BSS, zero-init).
	freeListSynth := IRGlobal{Name: freeListGlobalName, Size: 1}
	bssOffset[freeListGlobalName] = bssTotal
	bssList = append(bssList, globalInfo{freeListGlobalName, bssTotal})
	bssTotal += 8
	allPoolGlobals := append(definedGlobals, freeListSynth)

	// Build function return-type map for FP return detection in emitCall.
	funcRetType := make(map[string]TypeKind, len(irp.Funcs))
	for _, fn := range irp.Funcs {
		funcRetType[fn.Name] = fn.ReturnType
	}

	cb := newCodeBuilder(allPoolGlobals, irp.StrLits, irp.FConsts, irp.FuncRefs)
	gen := &elfGen{
		cb:            cb,
		pendingParams: make([]paramArg, 0, 8),
		isGlobalPtr:   isGlobalPtr,
		funcRetType:   funcRetType,
		structDefs:    irp.StructDefs,
	}

	gen.emitStart(definedGlobals)
	gen.emitOutputFn()
	gen.emitInputFn()
	gen.emitFflushFn()
	gen.emitPrintCharFn()
	gen.emitPrintStringFn()
	gen.emitPrintDoubleFn()
	gen.emitMallocFn()
	gen.emitFreeFn()
	for _, fn := range irp.Funcs {
		gen.genFunc(fn)
	}

	// --- Phase 3: patch branch offsets -------------------------------------
	if err := cb.applyFixups(); err != nil {
		return fmt.Errorf("genELF: %w", err)
	}

	// --- Phase 4: compute virtual addresses and patch pool -----------------
	codeBytes := uint64(len(cb.instrs)) * 4
	// Linux requires (file_offset % page_size) == (vaddr % page_size) for
	// each PT_LOAD.  rodataBase is page-aligned (vaddr%page==0), so the file
	// offset must be too.  Pad the file between code and rodata with zeros.
	rodataFileOff := nextPage(elfHeaderEnd + codeBytes)
	rodataBase := nextPage(codeBase + codeBytes)
	bssBase := nextPage(rodataBase + rodataTotal)

	poolAddrs := make(map[string]uint64, len(bssList)+len(rodataList)+len(irp.FuncRefs))
	for _, gi := range bssList {
		poolAddrs[gi.name] = bssBase + gi.offset
	}
	for _, ri := range rodataList {
		poolAddrs[ri.label] = rodataBase + ri.offset
	}
	// Patch function address pool entries with the function's code VA.
	for _, fn := range irp.FuncRefs {
		label := funcLabel(fn)
		if idx, ok := cb.labels[label]; ok {
			poolAddrs[label] = codeBase + uint64(idx)*4
		}
	}
	cb.patchPool(poolAddrs)

	// --- Phase 5: write ELF ------------------------------------------------
	fileSize := rodataFileOff + rodataTotal // header + code + pad + rodata
	entryVaddr := uint64(0)
	if idx, ok := cb.labels["_start"]; ok {
		entryVaddr = codeBase + uint64(idx)*4
	}

	f, err := os.OpenFile(outpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("genELF: %w", err)
	}
	defer f.Close()

	// ELF header.
	var ident [elf.EI_NIDENT]byte
	copy(ident[:], elf.ELFMAG)              // bytes 0..3: 0x7F 'E' 'L' 'F'
	ident[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	ident[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	ident[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	ident[elf.EI_OSABI] = byte(elf.ELFOSABI_NONE)

	ehdr := elf.Header64{
		Ident:     ident,
		Type:      uint16(elf.ET_EXEC),
		Machine:   uint16(elf.EM_AARCH64),
		Version:   uint32(elf.EV_CURRENT),
		Entry:     entryVaddr,
		Phoff:     uint64(elfHdrSize),
		Shoff:     0,
		Flags:     0,
		Ehsize:    uint16(elfHdrSize),
		Phentsize: uint16(elfPhdrSize),
		Phnum:     uint16(elfNumPhdrs),
		Shentsize: 0,
		Shnum:     0,
		Shstrndx:  0,
	}
	if err := binary.Write(f, binary.LittleEndian, ehdr); err != nil {
		return fmt.Errorf("genELF: write header: %w", err)
	}

	// PT_LOAD — code (maps ELF header + code).
	codeFileSz := elfHeaderEnd + codeBytes
	codePhdr := elf.Prog64{
		Type:   uint32(elf.PT_LOAD),
		Flags:  uint32(elf.PF_R | elf.PF_X),
		Off:    0,
		Vaddr:  elfLoadBase,
		Paddr:  elfLoadBase,
		Filesz: codeFileSz,
		Memsz:  codeFileSz,
		Align:  pageSize,
	}
	if err := binary.Write(f, binary.LittleEndian, codePhdr); err != nil {
		return fmt.Errorf("genELF: write code phdr: %w", err)
	}

	// PT_LOAD — rodata (read-only; filesz = rodataTotal, or 0 if no strings).
	rodataPhdr := elf.Prog64{
		Type:   uint32(elf.PT_LOAD),
		Flags:  uint32(elf.PF_R),
		Off:    rodataFileOff,
		Vaddr:  rodataBase,
		Paddr:  rodataBase,
		Filesz: rodataTotal,
		Memsz:  rodataTotal,
		Align:  pageSize,
	}
	if err := binary.Write(f, binary.LittleEndian, rodataPhdr); err != nil {
		return fmt.Errorf("genELF: write rodata phdr: %w", err)
	}

	// PT_LOAD — BSS (memory-only; filesz=0).
	bssPhdr := elf.Prog64{
		Type:   uint32(elf.PT_LOAD),
		Flags:  uint32(elf.PF_R | elf.PF_W),
		Off:    0,
		Vaddr:  bssBase,
		Paddr:  bssBase,
		Filesz: 0,
		Memsz:  bssTotal,
		Align:  pageSize,
	}
	if err := binary.Write(f, binary.LittleEndian, bssPhdr); err != nil {
		return fmt.Errorf("genELF: write BSS phdr: %w", err)
	}

	// Code section (literal pool + instructions).
	if err := binary.Write(f, binary.LittleEndian, cb.instrs); err != nil {
		return fmt.Errorf("genELF: write code: %w", err)
	}

	// Padding: zero-fill from end of code to rodataFileOff.
	codeEnd := elfHeaderEnd + codeBytes
	if pad := rodataFileOff - codeEnd; pad > 0 {
		if _, err := f.Write(make([]byte, pad)); err != nil {
			return fmt.Errorf("genELF: write rodata padding: %w", err)
		}
	}

	// Rodata section (string literals, NUL-terminated).
	for _, ri := range rodataList {
		if _, err := f.Write(ri.bytes); err != nil {
			return fmt.Errorf("genELF: write rodata: %w", err)
		}
	}

	_ = fileSize
	return nil
}
