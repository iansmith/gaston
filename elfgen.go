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
	instrs    []uint32
	labels    map[string]int // label name → instruction index
	fixups    []branchFixup
	poolIdx   map[string]int  // name/label → pool entry index (0-based)
	externBLs []externBLRecord // extern BL calls (for ET_REL mode only)
}

func newCodeBuilder(globals []IRGlobal, strLits []IRStrLit) *codeBuilder {
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
	// Reserve space for the literal pool: 2 uint32 words per 8-byte address.
	for j := 0; j < i; j++ {
		cb.instrs = append(cb.instrs, 0, 0)
	}
	return cb
}

// emit appends one instruction word.
func (cb *codeBuilder) emit(w uint32) {
	cb.instrs = append(cb.instrs, w)
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
func (cb *codeBuilder) peepholeElim(ldrEnc uint32) bool {
	if len(cb.instrs) == 0 {
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

// ── label / branch helpers ────────────────────────────────────────────────────

func (cb *codeBuilder) defineLabel(name string) {
	cb.labels[name] = len(cb.instrs)
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
	pendingParams []IRAddr
	cmpN          int // counter for synthetic comparison labels
	fn            *IRFunc
	fr            *frame
	isGlobalPtr   map[string]bool // global variables that are pointers (TypeIntPtr/TypeCharPtr)
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

// load emits instructions to load addr into register rd (0..7).
func (g *elfGen) load(addr IRAddr, rd int) {
	switch addr.Kind {
	case AddrConst:
		g.cb.emitMOVimm(rd, int64(addr.IVal))
	case AddrTemp, AddrLocal:
		off := g.fr.offsets[addr.Name]
		g.cb.emitLDRsp(rd, off)
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
		g.cb.emit(encSTRuoff(rd, regSP, off))
	case AddrGlobal:
		g.cb.emitLDRglobal(dst.Name)                     // X9 = &global
		g.cb.emit(encSTRuoff(rd, regX9, 0))              // *X9 = rd
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
		if g.fr.isArrPtr[addr.Name] || g.fr.isPtrVar[addr.Name] {
			// Array param or pointer variable: frame slot holds a pointer.
			g.cb.emitLDRsp(rd, off)
		} else {
			// Local array: its elements are inline in the frame.
			g.cb.emit(encADDimm(rd, regSP, off))
		}
	}
}

// ── prologue / epilogue ───────────────────────────────────────────────────────

func (g *elfGen) emitPrologue(fn *IRFunc) {
	f := g.fr
	g.cb.emit(encSUBimm(regSP, regSP, f.frameSize))
	g.cb.emit(encSTP(regFP, regLR, regSP, 0))
	g.cb.emit(encADDimm(regFP, regSP, 0)) // FP = SP
	for i, name := range fn.Params {
		if i >= 8 {
			break
		}
		g.cb.emit(encSTRuoff(i, regSP, f.offsets[name]))
	}
}

func (g *elfGen) emitEpilogue() {
	g.cb.emit(encLDP(regFP, regLR, regSP, 0))
	g.cb.emit(encADDimm(regSP, regSP, g.fr.frameSize))
	g.cb.emit(encRET())
}

// ── IR quad emission ──────────────────────────────────────────────────────────

func (g *elfGen) genFunc(fn *IRFunc) {
	g.fn = fn
	g.fr = buildFrame(fn)
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
		case IRGetAddr:
			g.arrayBase(q.Src1, regX0)
			g.store(regX0, q.Dst)

		case IRStrAddr:
			// Load the address of a string literal via the pool.
			g.cb.emitLDRglobal(q.Extra)              // X9 = VA of string literal
			g.cb.emit(encMOVreg(regX0, regX9))       // X0 = X9
			g.store(regX0, q.Dst)

		case IRDerefLoad:
			// Dst = *Src1 (load 8 bytes via pointer in Src1)
			g.load(q.Src1, regX0)                           // X0 = pointer
			g.cb.emit(encLDRuoff(regX0, regX0, 0))         // X0 = *X0
			g.store(regX0, q.Dst)

		case IRDerefStore:
			// *Dst = Src1 (store 8 bytes via pointer in Dst)
			g.load(q.Dst, regX0)                            // X0 = pointer
			g.load(q.Src1, regX1)                           // X1 = value
			g.cb.emit(encSTRuoff(regX1, regX0, 0))         // *X0 = X1

		case IRParam:
			g.pendingParams = append(g.pendingParams, q.Src1)

		case IRCall:
			g.emitCall(q)

		case IRReturn:
			if q.Src1.Kind != AddrNone {
				g.load(q.Src1, regX0)
			}
			g.emitEpilogue()
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

func (g *elfGen) emitArrayLoad(q Quad) {
	g.arrayBase(q.Src1, regX0)              // X0 = base address
	g.load(q.Src2, regX1)                   // X1 = index
	g.cb.emit(encLSLimm(regX1, regX1, 3))  // X1 = index * 8
	g.cb.emit(encADDreg(regX0, regX0, regX1))
	g.cb.emit(encLDRuoff(regX0, regX0, 0)) // X0 = *X0
	g.store(regX0, q.Dst)
}

func (g *elfGen) emitArrayStore(q Quad) {
	g.arrayBase(q.Dst, regX0)               // X0 = base address
	g.load(q.Src1, regX1)                   // X1 = index
	g.cb.emit(encLSLimm(regX1, regX1, 3))  // X1 = index * 8
	g.cb.emit(encADDreg(regX0, regX0, regX1))
	g.load(q.Src2, regX2)                   // X2 = value
	g.cb.emit(encSTRuoff(regX2, regX0, 0)) // *X0 = X2
}

func (g *elfGen) emitCall(q Quad) {
	for i, p := range g.pendingParams {
		if i >= 8 {
			break
		}
		g.load(p, i) // load into Xi
	}
	g.pendingParams = g.pendingParams[:0]

	if g.isObjMode {
		// In object-file mode every call to a non-locally-defined function
		// becomes an extern BL (resolved by the linker via CALL26 relocation).
		switch q.Extra {
		case "input":
			g.cb.emitBLextern("gaston_input")
		case "output":
			g.cb.emitBLextern("gaston_output")
		case "print_char":
			g.cb.emitBLextern("gaston_print_char")
		case "print_string":
			g.cb.emitBLextern("gaston_print_string")
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
		case "malloc":
			g.cb.emitBL("gaston_malloc")
		case "free":
			g.cb.emitBL("gaston_free")
		default:
			g.cb.emitBL(funcLabel(q.Extra))
		}
	}

	if q.Dst.Kind != AddrNone {
		g.store(regX0, q.Dst)
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
		if !gbl.HasInitVal || gbl.IsArr {
			continue
		}
		g.cb.emitMOVimm(regX0, int64(gbl.InitVal)) // value → X0
		g.cb.emitLDRglobal(gbl.Name)               // X9 = &global
		g.cb.emit(encSTRuoff(regX0, regX9, 0))     // *X9 = value
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

	cb := newCodeBuilder(allPoolGlobals, irp.StrLits)
	gen := &elfGen{cb: cb, pendingParams: make([]IRAddr, 0, 8), isGlobalPtr: isGlobalPtr}

	gen.emitStart(definedGlobals)
	gen.emitOutputFn()
	gen.emitInputFn()
	gen.emitPrintCharFn()
	gen.emitPrintStringFn()
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

	poolAddrs := make(map[string]uint64, len(bssList)+len(rodataList))
	for _, gi := range bssList {
		poolAddrs[gi.name] = bssBase + gi.offset
	}
	for _, ri := range rodataList {
		poolAddrs[ri.label] = rodataBase + ri.offset
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
