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
	elfNumPhdrs  = 2
	elfHeaderEnd = elfHdrSize + elfNumPhdrs*elfPhdrSize // 0xB0
	codeBase     = elfLoadBase + elfHeaderEnd           // 0x4000B0
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
)

type branchFixup struct {
	at    int       // instruction index holding the placeholder
	label string    // target label name
	kind  fixupKind
}

// ── codeBuilder ───────────────────────────────────────────────────────────────

// codeBuilder accumulates ARM64 machine-code words, label locations, and
// branch fixup requests. The literal pool for global addresses occupies the
// first poolCount*2 slots of instrs (two uint32 words per 8-byte address).
type codeBuilder struct {
	instrs   []uint32
	labels   map[string]int // label name → instruction index
	fixups   []branchFixup
	poolIdx  map[string]int // global name → pool entry index (0-based)
}

func newCodeBuilder(globals []IRGlobal) *codeBuilder {
	cb := &codeBuilder{
		labels:  make(map[string]int),
		poolIdx: make(map[string]int),
	}
	for i, g := range globals {
		cb.poolIdx[g.Name] = i
	}
	// Reserve space for the literal pool: 2 uint32 words per 8-byte address.
	for range globals {
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
		g.cb.emitLDRglobal(addr.Name) // X9 = &global (IS the base address)
		g.cb.emit(encMOVreg(rd, regX9))
	case AddrLocal, AddrTemp:
		off := g.fr.offsets[addr.Name]
		if g.fr.isArrPtr[addr.Name] {
			// Array parameter: the frame slot holds a pointer.
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

		case IRLoad:
			g.emitArrayLoad(q)
		case IRStore:
			g.emitArrayStore(q)
		case IRGetAddr:
			g.arrayBase(q.Src1, regX0)
			g.store(regX0, q.Dst)

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

	switch q.Extra {
	case "input":
		g.cb.emitBL("gaston_input")
	case "output":
		g.cb.emitBL("gaston_output")
	default:
		g.cb.emitBL(funcLabel(q.Extra))
	}

	if q.Dst.Kind != AddrNone {
		g.store(regX0, q.Dst)
	}
}

// ── runtime helper functions ──────────────────────────────────────────────────

// emitStart emits the _start entry point:
//   BL gaston_main
//   MOV X0, #0 / MOV X8, #94 / SVC #0  (exit_group(0))
func (g *elfGen) emitStart() {
	g.cb.defineLabel("_start")
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
// Frame layout (64 bytes):
//   SP+0: FP, SP+8: LR
//   SP+16: X19 (result accumulator)
//   SP+24: X20 (sign flag)
//   SP+32: X21 (char pointer into buffer)
//   SP+40..SP+63: 24-byte read buffer
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
	cb.emitMOVimm(regX19, 0)                         // result = 0
	cb.emitMOVimm(regX20, 0)                         // sign = positive
	cb.emit(encADDimm(regX21, regSP, 40))            // X21 = &buf[0]

	// read(0, buf, 23).
	cb.emitMOVimm(regX0, 0)                          // fd = stdin
	cb.emit(encMOVreg(regX1, regX21))               // buf
	cb.emitMOVimm(regX2, 23)                        // max bytes
	cb.emitMOVimm(regX8, 63)                        // read syscall
	cb.emit(encSVC(0))

	// Scan past non-digit, non-sign characters (whitespace etc.).
	cb.defineLabel("in_scan")
	cb.emit(encLDRBuoff(regX2, regX21, 0))          // W2 = *X21
	cb.emit(encCMPimm0(regX2))
	cb.emitBcond(condEQ, "in_done")                  // null byte → done
	// Check for '-'
	cb.emitMOVimm(regX3, '-')
	cb.emit(encCMPreg(regX2, regX3))
	cb.emitBcond(condEQ, "in_minus")
	// Check if digit.
	cb.emitMOVimm(regX3, '0')
	cb.emit(encCMPreg(regX2, regX3))
	cb.emitBcond(condGE, "in_digit_loop") // W2 >= '0': start parsing
	// Not a digit or '-': advance.
	cb.emit(encADDimm(regX21, regX21, 1))
	cb.emitB("in_scan")

	// Found '-': set sign flag, advance.
	cb.defineLabel("in_minus")
	cb.emitMOVimm(regX20, 1)
	cb.emit(encADDimm(regX21, regX21, 1))

	// Digit-parse loop (re-reads W2 at top, including first digit).
	cb.defineLabel("in_digit_loop")
	cb.emit(encLDRBuoff(regX2, regX21, 0))          // W2 = *X21
	cb.emitMOVimm(regX3, '0')
	cb.emit(encCMPreg(regX2, regX3))
	cb.emitBcond(condLT, "in_done")                  // < '0' → stop
	cb.emitMOVimm(regX3, '9')
	cb.emit(encCMPreg(regX2, regX3))
	cb.emitBcond(condGT, "in_done")                  // > '9' → stop
	cb.emit(encSUBimm(regX2, regX2, '0'))           // digit value
	cb.emitMOVimm(regX3, 10)
	cb.emit(encMUL(regX19, regX19, regX3))          // result *= 10
	cb.emit(encADDreg(regX19, regX19, regX2))       // result += digit
	cb.emit(encADDimm(regX21, regX21, 1))
	cb.emitB("in_digit_loop")

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
		sz := uint64(gbl.Size) * 8
		bssOffset[gbl.Name] = bssTotal
		bssList = append(bssList, globalInfo{gbl.Name, bssTotal})
		bssTotal += sz
	}

	// --- Phase 2: emit machine code ----------------------------------------
	cb := newCodeBuilder(irp.Globals)
	gen := &elfGen{cb: cb, pendingParams: make([]IRAddr, 0, 8)}

	gen.emitStart()
	gen.emitOutputFn()
	gen.emitInputFn()
	for _, fn := range irp.Funcs {
		gen.genFunc(fn)
	}

	// --- Phase 3: patch branch offsets -------------------------------------
	if err := cb.applyFixups(); err != nil {
		return fmt.Errorf("genELF: %w", err)
	}

	// --- Phase 4: compute virtual addresses and patch pool -----------------
	codeBytes := uint64(len(cb.instrs)) * 4
	bssBase := nextPage(codeBase + codeBytes)

	globalAddrs := make(map[string]uint64, len(bssList))
	for _, gi := range bssList {
		globalAddrs[gi.name] = bssBase + gi.offset
	}
	cb.patchPool(globalAddrs)

	// --- Phase 5: write ELF ------------------------------------------------
	fileSize := elfHeaderEnd + codeBytes // header + code; BSS is memory-only
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

	// PT_LOAD — code (maps entire file: ELF header + code).
	codePhdr := elf.Prog64{
		Type:   uint32(elf.PT_LOAD),
		Flags:  uint32(elf.PF_R | elf.PF_X),
		Off:    0,
		Vaddr:  elfLoadBase,
		Paddr:  elfLoadBase,
		Filesz: fileSize,
		Memsz:  fileSize,
		Align:  pageSize,
	}
	if err := binary.Write(f, binary.LittleEndian, codePhdr); err != nil {
		return fmt.Errorf("genELF: write code phdr: %w", err)
	}

	// PT_LOAD — BSS (memory-only; filesz=0 so offset is irrelevant).
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

	return nil
}
