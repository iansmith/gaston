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
	"fmt"
	"math"
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
	sym string // symbol name (e.g. "__gaston_input")
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

// funcLabel returns the global entry label for a C function.
// No prefix — symbols match standard C conventions so that user code
// and library code (libc, picolibc) link together naturally.
func funcLabel(name string) string { return name }

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

		case IRFrameAddr:
			// __builtin_frame_address(0) — return current frame pointer (X29/FP).
			g.cb.emit(encMOVreg(regX0, 29)) // MOV X0, X29 (FP register)
			g.store(regX0, q.Dst)

		case IRLabelAddr:
			// &&label — load PC-relative address of a user label.
			// Emits: ADR X0, gaston_fn_user_labelname
			g.cb.emitADR(regX0, g.irLabel(q.Extra))
			g.store(regX0, q.Dst)

		case IRIndirectJump:
			// goto *expr — load pointer from Src1 and branch to it.
			g.load(q.Src1, regX16) // X16 = IP0, intra-procedure scratch
			g.cb.emit(encBR(regX16))

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
		label := funcLabel(q.Extra)
		if g.localFuncs[q.Extra] {
			g.cb.emitBL(label)
		} else {
			g.cb.emitBLextern(label)
		}
	} else {
		g.cb.emitBL(funcLabel(q.Extra))
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


// sbrkGlobalName is the synthetic BSS global for sbrk's current program break.
const sbrkGlobalName = "sbrk_cur_brk"

// emitSbrkFn emits sbrk(X0 = increment) → X0 = old_break or (void*)-1.
//
// Uses the Linux brk syscall (214 on ARM64).  On the first call
// (sbrk_cur_brk == 0), discovers the current break via brk(0).
//
// Register plan:
//   X19 = increment (saved)
//   X20 = &sbrk_cur_brk
//   X21 = old_brk
func (g *elfGen) emitSbrkFn() {
	cb := g.cb
	cb.defineLabel("sbrk")

	// Prologue.
	cb.emit(encSUBimm(regSP, regSP, 48))
	cb.emit(encSTP(regFP, regLR, regSP, 0))
	cb.emit(encSTP(regX19, regX20, regSP, 16))
	cb.emit(encSTP(regX21, 22, regSP, 32))
	cb.emit(encADDimm(regFP, regSP, 0))

	cb.emit(encMOVreg(regX19, regX0)) // X19 = increment

	// X20 = &sbrk_cur_brk (from literal pool)
	cb.emitLDRglobal(sbrkGlobalName)
	cb.emit(encMOVreg(regX20, regX9))

	// X21 = *X20 = cur_brk
	cb.emit(encLDRuoff(regX21, regX20, 0))
	cb.emitCBNZ(regX21, "sbrk_have_brk")

	// First call: brk(0) → discover current break.
	cb.emit(encMOVZ(regX0, 0, 0))
	cb.emitMOVimm(regX8, 214)
	cb.emit(encSVC(0))
	cb.emitCBNZ(regX0, "sbrk_init_ok")
	// failure
	cb.emit(encMOVN(regX0, 0, 0)) // X0 = -1
	cb.emitB("sbrk_epilogue")

	cb.defineLabel("sbrk_init_ok")
	cb.emit(encMOVreg(regX21, regX0))       // X21 = discovered break
	cb.emit(encSTRuoff(regX21, regX20, 0))  // store it

	cb.defineLabel("sbrk_have_brk")
	// if (increment == 0) return cur_brk
	cb.emit(encMOVreg(regX0, regX21))       // X0 = cur_brk (return value)
	cb.emitCBZ(regX19, "sbrk_epilogue")

	// new_brk = cur_brk + increment
	cb.emit(encADDreg(regX0, regX21, regX19)) // X0 = new_brk (arg to brk)
	cb.emitMOVimm(regX8, 214)
	cb.emit(encSVC(0))
	// X0 = actual new break.  On Linux, if brk fails it returns the old break.
	// Success: X0 >= X21 + X19.
	cb.emit(encADDreg(regX1, regX21, regX19)) // X1 = requested new_brk
	cb.emit(encCMPreg(regX0, regX1))           // cmp X0, X1
	cb.emitBcond(condGE, "sbrk_ok")            // if actual >= requested, OK

	// failure: return -1
	cb.emit(encMOVN(regX0, 0, 0))
	cb.emitB("sbrk_epilogue")

	cb.defineLabel("sbrk_ok")
	cb.emit(encSTRuoff(regX0, regX20, 0))    // update sbrk_cur_brk = actual
	cb.emit(encMOVreg(regX0, regX21))        // return old_brk

	cb.defineLabel("sbrk_epilogue")
	cb.emit(encLDP(regX21, 22, regSP, 32))
	cb.emit(encLDP(regX19, regX20, regSP, 16))
	cb.emit(encLDP(regFP, regLR, regSP, 0))
	cb.emit(encADDimm(regSP, regSP, 48))
	cb.emit(encRET())
}

// emitPosixSyscalls emits thin wrappers for the POSIX syscalls that picolibc
// needs: read, write, open, close, lseek.  Each is a leaf function that loads
// the syscall number into X8 and executes SVC #0.  Arguments are already in
// X0-X2 per the AAPCS64 calling convention, matching the Linux syscall ABI.
func (g *elfGen) emitPosixSyscalls() {
	cb := g.cb

	// ssize_t read(int fd, void *buf, size_t count)  — syscall 63
	cb.defineLabel("read")
	cb.emitMOVimm(regX8, 63)
	cb.emit(encSVC(0))
	cb.emit(encRET())

	// ssize_t write(int fd, const void *buf, size_t count)  — syscall 64
	cb.defineLabel("write")
	cb.emitMOVimm(regX8, 64)
	cb.emit(encSVC(0))
	cb.emit(encRET())

	// int open(const char *pathname, int flags, int mode)  — syscall 56
	cb.defineLabel("open")
	// ARM64 Linux uses openat (56) with AT_FDCWD=-100 in X0, path in X1.
	// But glibc's open() wrapper uses openat internally.  For simplicity,
	// ARM64 Linux doesn't have a plain "open" syscall — it only has openat (56).
	// open(path, flags, mode) = openat(AT_FDCWD, path, flags, mode)
	cb.emit(encMOVreg(regX3, regX2))  // mode → X3
	cb.emit(encMOVreg(regX2, regX1))  // flags → X2
	cb.emit(encMOVreg(regX1, regX0))  // pathname → X1
	cb.emitMOVimm(regX0, 0)           // AT_FDCWD = -100
	cb.emit(encSUBimm(regX0, regX0, 100))
	cb.emitMOVimm(regX8, 56)          // openat
	cb.emit(encSVC(0))
	cb.emit(encRET())

	// int close(int fd)  — syscall 57
	cb.defineLabel("close")
	cb.emitMOVimm(regX8, 57)
	cb.emit(encSVC(0))
	cb.emit(encRET())

	// off_t lseek(int fd, off_t offset, int whence)  — syscall 62
	cb.defineLabel("lseek")
	cb.emitMOVimm(regX8, 62)
	cb.emit(encSVC(0))
	cb.emit(encRET())
}

// emitSetjmpFns emits setjmp and longjmp as inline ARM64 machine code.
//
// The jmp_buf layout matches picolibc's AArch64 definition (_JBLEN=22, _JBTYPE=long long,
// total 176 bytes).  Only integer callee-saved registers are saved; D8–D15 (slots 13–21)
// are reserved but not written because gaston-compiled code never keeps live values in
// those registers across function call boundaries.
//
// Offset layout (each slot is 8 bytes):
//   0: X19   8: X20   16: X21   24: X22   32: X23   40: X24
//  48: X25  56: X26   64: X27   72: X28   80: X29   88: X30/LR
//  96: SP
//
// See also: libc/setjmp_arm64.s — the canonical Plan 9 assembly source for this code.
func (g *elfGen) emitSetjmpFns() {
	cb := g.cb

	// setjmp(jmp_buf *env) → int
	// X0 = env pointer on entry; returns 0.
	cb.defineLabel("setjmp")
	cb.emit(encSTP(19, 20, regX0, 0))           // STP X19,X20,[X0,#0]
	cb.emit(encSTP(21, 22, regX0, 16))           // STP X21,X22,[X0,#16]
	cb.emit(encSTP(23, 24, regX0, 32))           // STP X23,X24,[X0,#32]
	cb.emit(encSTP(25, 26, regX0, 48))           // STP X25,X26,[X0,#48]
	cb.emit(encSTP(27, 28, regX0, 64))           // STP X27,X28,[X0,#64]
	cb.emit(encSTP(regFP, regLR, regX0, 80))     // STP X29,X30,[X0,#80] (FP, LR)
	cb.emit(encADDimm(regX1, regSP, 0))          // MOV X1, SP  (ADD X1, SP, #0)
	cb.emit(encSTRuoff(regX1, regX0, 96))        // STR X1,[X0,#96]  (save SP)
	cb.emitMOVimm(regX0, 0)                      // MOV X0, #0  (return 0)
	cb.emit(encRET())

	// longjmp(jmp_buf *env, int val)
	// X0 = env pointer, X1 = val; restores state and returns to setjmp caller.
	// If val == 0, returns 1 instead (setjmp must not return 0 on the longjmp path).
	cb.defineLabel("longjmp")
	cb.emit(encMOVreg(regX3, regX0))             // MOV X3, X0  (save env ptr)
	cb.emit(encMOVreg(regX2, regX1))             // MOV X2, X1  (save val)
	cb.emit(encLDP(19, 20, regX3, 0))            // LDP X19,X20,[X3,#0]
	cb.emit(encLDP(21, 22, regX3, 16))           // LDP X21,X22,[X3,#16]
	cb.emit(encLDP(23, 24, regX3, 32))           // LDP X23,X24,[X3,#32]
	cb.emit(encLDP(25, 26, regX3, 48))           // LDP X25,X26,[X3,#48]
	cb.emit(encLDP(27, 28, regX3, 64))           // LDP X27,X28,[X3,#64]
	cb.emit(encLDP(regFP, regLR, regX3, 80))     // LDP X29,X30,[X3,#80] (FP, LR)
	cb.emit(encLDRuoff(regX1, regX3, 96))        // LDR X1,[X3,#96]
	cb.emit(encADDimm(regSP, regX1, 0))          // MOV SP, X1  (ADD SP, X1, #0)
	// CBNZ X2, +2: if val != 0, skip the "use 1" instruction.
	// imm19=2 means jump 2 words forward (target is the MOV X0, X2 after the MOVZ).
	cb.emit(encCBNZ(regX2, 2))                   // CBNZ X2, done
	cb.emitMOVimm(regX2, 1)                      // MOV X2, #1  (val was 0; substitute 1)
	// done:
	cb.emit(encMOVreg(regX0, regX2))             // MOV X0, X2  (return val)
	cb.emit(encRET())
}

