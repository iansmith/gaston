// objgen.go — Linux ARM64 ELF relocatable object file (ET_REL) generator.
//
// Generates a .o file suitable for linking with the gaston linker.
//
// Section layout:
//   [0] NULL
//   [1] .text    — literal pool (zeros + RELA) + machine code
//   [2] .rodata  — NUL-terminated string literals
//   [3] .data    — initialized global scalars (8 bytes each, LE)
//   [4] .bss     — uninitialized globals and arrays (SHT_NOBITS)
//   [5] .symtab  — symbol table
//   [6] .strtab  — symbol name strings
//   [7] .rela.text — relocations for .text
//   [8] .shstrtab — section name strings
//
// Symbol naming:
//   Functions   → "gaston_<name>" (STB_GLOBAL, STT_FUNC, in .text)
//   Global vars → "<name>"        (STB_GLOBAL, STT_OBJECT, in .bss or .data)
//   Str lits    → ".str<n>"       (STB_LOCAL,  STT_OBJECT, in .rodata)
//
// Relocations in .rela.text:
//   Pool entry k (byte offset k*8): R_AARCH64_ABS64 for the symbol
//   Extern BL at instruction i:     R_AARCH64_CALL26 for the callee symbol
package main

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
)

// ARM64 relocation type constants (AAELF64 spec).
const (
	rAArch64Abs64   = 257 // R_AARCH64_ABS64: absolute 64-bit (S + A)
	rAArch64Call26  = 283 // R_AARCH64_CALL26: BL target (S + A - P) >> 2
)

// shstrtab is the fixed section-name string table content and offsets.
var objShstrtab = []byte("\x00.shstrtab\x00.text\x00.rodata\x00.data\x00.bss\x00.symtab\x00.strtab\x00.rela.text\x00.rela.data\x00")

const (
	shstrShstrtab = 1
	shstrText     = 11
	shstrRodata   = 17
	shstrData     = 25
	shstrBss      = 31
	shstrSymtab   = 36
	shstrStrtab   = 44
	shstrRelaText = 52
	shstrRelaData = 63
)

// Section indices in the generated .o file.
const (
	objSecNull     = 0
	objSecText     = 1
	objSecRodata   = 2
	objSecData     = 3
	objSecBss      = 4
	objSecSymtab   = 5
	objSecStrtab   = 6
	objSecRelaText = 7
	objSecRelaData = 8
	objSecShstrtab = 9
	objNumSections = 10
)

// strtabBuilder accumulates a .strtab / .shstrtab byte slice.
type strtabBuilder struct {
	data []byte
}

func newStrtab() *strtabBuilder {
	return &strtabBuilder{data: []byte{0}} // index 0 = empty string
}

// add appends name + NUL and returns the byte index of name.
func (s *strtabBuilder) add(name string) uint32 {
	idx := uint32(len(s.data))
	s.data = append(s.data, []byte(name)...)
	s.data = append(s.data, 0)
	return idx
}

// genObjectFile compiles irp to a Linux ARM64 ET_REL object file at outpath.
func genObjectFile(irp *IRProgram, outpath string) error {
	// ── build isGlobalPtr map ──────────────────────────────────────────────
	isGlobalPtr := make(map[string]bool)
	for _, gbl := range irp.Globals {
		if gbl.IsPtr {
			isGlobalPtr[gbl.Name] = true
		}
	}

	// ── partition globals ──────────────────────────────────────────────────
	// Only locally-defined (non-extern) globals get storage.
	type globalSlot struct {
		gbl      IRGlobal
		inData   bool   // initialized scalar → .data; else → .bss
		dataOff  uint64 // byte offset within .data
		bssOff   uint64 // byte offset within .bss
	}
	var slots []globalSlot
	var dataTotal, bssTotal uint64
	for _, gbl := range irp.Globals {
		if gbl.IsExtern {
			continue
		}
		sl := globalSlot{gbl: gbl}
		if gbl.HasInitVal && !gbl.IsArr {
			sl.inData = true
			sl.dataOff = dataTotal
			dataTotal += 8
		} else if len(gbl.InitData) > 0 {
			// Struct/array with init data → .data section.
			sz := uint64(len(gbl.InitData))
			// Align to 8 bytes.
			if sz%8 != 0 {
				sz = (sz + 7) &^ 7
			}
			sl.inData = true
			sl.dataOff = dataTotal
			dataTotal += sz
		} else {
			sl.bssOff = bssTotal
			bssTotal += uint64(gbl.Size) * 8
		}
		slots = append(slots, sl)
	}

	// ── .data section bytes ────────────────────────────────────────────────
	dataBytes := make([]byte, dataTotal)
	// Track .data relocations for pointer fields (string literals, etc.)
	type dataReloc struct {
		offset uint64 // byte offset within .data
		symbol string // symbol name to relocate against
	}
	var dataRelocs []dataReloc
	for _, sl := range slots {
		if sl.inData {
			if sl.gbl.HasInitVal && len(sl.gbl.InitData) == 0 {
				binary.LittleEndian.PutUint64(dataBytes[sl.dataOff:], uint64(sl.gbl.InitVal))
			} else if len(sl.gbl.InitData) > 0 {
				copy(dataBytes[sl.dataOff:], sl.gbl.InitData)
				for _, rel := range sl.gbl.InitRelocs {
					dataRelocs = append(dataRelocs, dataReloc{
						offset: sl.dataOff + uint64(rel.ByteOff),
						symbol: rel.Label,
					})
				}
			}
		}
	}

	// ── .rodata layout ────────────────────────────────────────────────────
	type rodataEntry struct {
		label  string
		bytes  []byte
		offset uint64
	}
	var rodataList []rodataEntry
	var rodataTotal uint64
	for _, sl := range irp.StrLits {
		b := append([]byte(sl.Content), 0)
		rodataList = append(rodataList, rodataEntry{sl.Label, b, rodataTotal})
		rodataTotal += uint64(len(b))
	}
	rodataBytes := make([]byte, rodataTotal)
	for _, re := range rodataList {
		copy(rodataBytes[re.offset:], re.bytes)
	}

	// ── set of locally-defined function names ──────────────────────────────
	localFuncs := make(map[string]bool)
	for _, fn := range irp.Funcs {
		localFuncs[fn.Name] = true
	}

	// ── code generation ────────────────────────────────────────────────────
	// Build a pool for ALL globals (including extern) + string literals.
	// Extern globals get pool slots so poolIdx maps them correctly; the linker
	// fills those slots via R_AARCH64_ABS64 relocations against UNDEF symbols.
	// Only locally-defined globals get pool slots for defined symbols.
	allGlobalsForPool := irp.Globals

	funcRetType := make(map[string]TypeKind, len(irp.Funcs))
	for _, fn := range irp.Funcs {
		funcRetType[fn.Name] = fn.ReturnType
	}

	cb := newCodeBuilder(allGlobalsForPool, irp.StrLits, irp.FConsts, irp.FuncRefs)
	gen := &elfGen{
		cb:            cb,
		pendingParams: make([]paramArg, 0, 8),
		isGlobalPtr:   isGlobalPtr,
		isObjMode:     true,
		localFuncs:    localFuncs,
		funcRetType:   funcRetType,
		structDefs:    irp.StructDefs,
	}

	// Emit code for each locally-defined function (no _start, no helpers).
	for _, fn := range irp.Funcs {
		gen.genFunc(fn)
	}

	if err := cb.applyFixups(); err != nil {
		return fmt.Errorf("genObjectFile: %w", err)
	}

	textBytes := make([]byte, len(cb.instrs)*4)
	for i, w := range cb.instrs {
		binary.LittleEndian.PutUint32(textBytes[i*4:], w)
	}

	// ── build .symtab and .strtab ─────────────────────────────────────────
	strtab := newStrtab()

	type symRec struct {
		nameIdx uint32
		info    uint8
		other   uint8
		shndx   uint16
		value   uint64
		size    uint64
	}

	var syms []symRec

	// [0]: null symbol
	syms = append(syms, symRec{})

	// Section symbols (STB_LOCAL, STT_SECTION).
	sectionSym := func(shndx uint16) symRec {
		return symRec{info: (0 << 4) | 3, shndx: shndx} // STB_LOCAL|STT_SECTION
	}
	syms = append(syms, sectionSym(objSecText))   // [1]
	syms = append(syms, sectionSym(objSecRodata)) // [2]
	syms = append(syms, sectionSym(objSecData))   // [3]
	syms = append(syms, sectionSym(objSecBss))    // [4]

	// String literal local symbols (STB_LOCAL, STT_OBJECT, in .rodata).
	strSymIdx := make(map[string]int) // label → symtab index
	for _, re := range rodataList {
		idx := len(syms)
		strSymIdx[re.label] = idx
		nameIdx := strtab.add(re.label)
		syms = append(syms, symRec{
			nameIdx: nameIdx,
			info:    (0 << 4) | 1, // STB_LOCAL | STT_OBJECT
			shndx:   objSecRodata,
			value:   re.offset,
			size:    uint64(len(re.bytes)),
		})
	}

	// First global symbol index.
	firstGlobal := len(syms)

	// Global symbols for locally-defined functions (STB_GLOBAL, STT_FUNC).
	funcSymIdx := make(map[string]int) // C-minus name → symtab index
	for _, fn := range irp.Funcs {
		label := funcLabel(fn.Name) // "gaston_<name>"
		wordIdx, ok := cb.labels[label]
		if !ok {
			return fmt.Errorf("genObjectFile: function label %q not found", label)
		}
		idx := len(syms)
		funcSymIdx[fn.Name] = idx
		nameIdx := strtab.add(label)
		syms = append(syms, symRec{
			nameIdx: nameIdx,
			info:    (1 << 4) | 2, // STB_GLOBAL | STT_FUNC
			shndx:   objSecText,
			value:   uint64(wordIdx) * 4,
		})
	}

	// Global symbols for locally-defined variables.
	globalSymIdx := make(map[string]int) // C-minus name → symtab index
	for _, sl := range slots {
		idx := len(syms)
		globalSymIdx[sl.gbl.Name] = idx
		nameIdx := strtab.add(sl.gbl.Name)
		sz := uint64(sl.gbl.Size) * 8
		if sl.inData {
			syms = append(syms, symRec{
				nameIdx: nameIdx,
				info:    (1 << 4) | 1, // STB_GLOBAL | STT_OBJECT
				shndx:   objSecData,
				value:   sl.dataOff,
				size:    sz,
			})
		} else {
			syms = append(syms, symRec{
				nameIdx: nameIdx,
				info:    (1 << 4) | 1, // STB_GLOBAL | STT_OBJECT
				shndx:   objSecBss,
				value:   sl.bssOff,
				size:    sz,
			})
		}
	}

	// Collect all extern symbol names referenced by pool entries or extern BLs.
	externSymNames := make(map[string]int) // sym name → symtab index

	// Pool entries for extern globals.
	for _, gbl := range irp.Globals {
		if gbl.IsExtern {
			if _, ok := externSymNames[gbl.Name]; !ok {
				idx := len(syms)
				externSymNames[gbl.Name] = idx
				nameIdx := strtab.add(gbl.Name)
				syms = append(syms, symRec{
					nameIdx: nameIdx,
					info:    (1 << 4) | 0, // STB_GLOBAL | STT_NOTYPE
					shndx:   uint16(elf.SHN_UNDEF),
				})
			}
		}
	}

	// Extern BL targets.
	for _, xbl := range cb.externBLs {
		if _, ok := externSymNames[xbl.sym]; !ok {
			idx := len(syms)
			externSymNames[xbl.sym] = idx
			nameIdx := strtab.add(xbl.sym)
			syms = append(syms, symRec{
				nameIdx: nameIdx,
				info:    (1 << 4) | 0, // STB_GLOBAL | STT_NOTYPE
				shndx:   uint16(elf.SHN_UNDEF),
			})
		}
	}

	// ── build .rela.text ──────────────────────────────────────────────────
	type relaRec struct {
		off    uint64
		info   uint64
		addend int64
	}
	var relas []relaRec

	mkRela := func(off uint64, symIdx int, typ uint32, addend int64) relaRec {
		return relaRec{
			off:    off,
			info:   uint64(symIdx)<<32 | uint64(typ),
			addend: addend,
		}
	}

	// ABS64 relocations for each pool entry.
	poolCount := len(allGlobalsForPool) + len(irp.StrLits)
	for k := 0; k < poolCount; k++ {
		byteOff := uint64(k * 8)
		var symIdx int

		// Determine which symbol this pool entry refers to.
		// Pool is ordered: globals first, then string literals.
		if k < len(allGlobalsForPool) {
			name := allGlobalsForPool[k].Name
			if i, ok := globalSymIdx[name]; ok {
				symIdx = i
			} else if i, ok := externSymNames[name]; ok {
				symIdx = i
			} else {
				return fmt.Errorf("genObjectFile: no symbol for pool global %q", name)
			}
		} else {
			litIdx := k - len(allGlobalsForPool)
			symIdx = strSymIdx[rodataList[litIdx].label]
		}

		relas = append(relas, mkRela(byteOff, symIdx, rAArch64Abs64, 0))
	}

	// ABS64 relocations for function address references (IRFuncAddr).
	// Pool order: globals, string lits, FP constants, func refs.
	// FP constants don't need relocs; func refs do.
	for _, fnName := range irp.FuncRefs {
		label := funcLabel(fnName)
		k, ok := cb.poolIdx[label]
		if !ok {
			return fmt.Errorf("genObjectFile: no pool entry for func ref %q", label)
		}
		byteOff := uint64(k * 8)
		var symIdx int
		if i, ok2 := funcSymIdx[fnName]; ok2 {
			symIdx = i
		} else if i, ok2 := externSymNames[label]; ok2 {
			symIdx = i
		} else {
			// Function not in this unit — add as extern UNDEF.
			idx := len(syms)
			externSymNames[label] = idx
			nameIdx := strtab.add(label)
			syms = append(syms, symRec{
				nameIdx: nameIdx,
				info:    (1 << 4) | 2, // STB_GLOBAL | STT_FUNC
				shndx:   uint16(elf.SHN_UNDEF),
			})
			symIdx = idx
		}
		relas = append(relas, mkRela(byteOff, symIdx, rAArch64Abs64, 0))
	}

	// CALL26 relocations for extern BL calls.
	for _, xbl := range cb.externBLs {
		byteOff := uint64(xbl.at) * 4
		symIdx := externSymNames[xbl.sym]
		relas = append(relas, mkRela(byteOff, symIdx, rAArch64Call26, 0))
	}
	// Also CALL26 for local function calls that cross compilation-unit boundaries
	// (currently none; local calls are pre-patched by applyFixups).

	// ── serialise .symtab ─────────────────────────────────────────────────
	symtabBytes := make([]byte, len(syms)*24)
	for i, s := range syms {
		off := i * 24
		binary.LittleEndian.PutUint32(symtabBytes[off:], s.nameIdx)
		symtabBytes[off+4] = s.info
		symtabBytes[off+5] = s.other
		binary.LittleEndian.PutUint16(symtabBytes[off+6:], s.shndx)
		binary.LittleEndian.PutUint64(symtabBytes[off+8:], s.value)
		binary.LittleEndian.PutUint64(symtabBytes[off+16:], s.size)
	}

	// ── serialise .rela.text ──────────────────────────────────────────────
	relaBytes := make([]byte, len(relas)*24)
	for i, r := range relas {
		off := i * 24
		binary.LittleEndian.PutUint64(relaBytes[off:], r.off)
		binary.LittleEndian.PutUint64(relaBytes[off+8:], r.info)
		binary.LittleEndian.PutUint64(relaBytes[off+16:], uint64(r.addend))
	}

	// ── serialise .rela.data ──────────────────────────────────────────────
	var relaDataRecs []relaRec
	for _, dr := range dataRelocs {
		// Find the symbol index for this string literal label.
		symIdx, ok := strSymIdx[dr.symbol]
		if !ok {
			return fmt.Errorf("genObjectFile: no symbol for data reloc %q", dr.symbol)
		}
		relaDataRecs = append(relaDataRecs, mkRela(dr.offset, symIdx, rAArch64Abs64, 0))
	}
	relaDataBytes := make([]byte, len(relaDataRecs)*24)
	for i, r := range relaDataRecs {
		off := i * 24
		binary.LittleEndian.PutUint64(relaDataBytes[off:], r.off)
		binary.LittleEndian.PutUint64(relaDataBytes[off+8:], r.info)
		binary.LittleEndian.PutUint64(relaDataBytes[off+16:], uint64(r.addend))
	}

	// ── compute file layout ───────────────────────────────────────────────
	// File layout (in order):
	//   ELF header (64 bytes)
	//   .text data
	//   .rodata data
	//   .data data
	//   .symtab data
	//   .strtab data
	//   .rela.text data
	//   .rela.data data
	//   .shstrtab data
	//   Section header table (10 × 64 bytes)

	const elfHdr = 64
	const shdrSize = 64
	const shdrTableSize = objNumSections * shdrSize

	off := uint64(elfHdr)
	textOff := off
	off += uint64(len(textBytes))

	rodataOff := off
	off += rodataTotal

	dataOff := off
	off += dataTotal

	symtabOff := off
	off += uint64(len(symtabBytes))

	strtabOff := off
	off += uint64(len(strtab.data))

	relaOff := off
	off += uint64(len(relaBytes))

	relaDataOff := off
	off += uint64(len(relaDataBytes))

	shstrtabOff := off
	off += uint64(len(objShstrtab))

	// Align section header table to 8 bytes.
	for off%8 != 0 {
		off++
	}
	shdrOff := off

	// ── write file ────────────────────────────────────────────────────────
	f, err := os.OpenFile(outpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("genObjectFile: %w", err)
	}
	defer f.Close()

	write := func(data interface{}) {
		if err != nil {
			return
		}
		err = binary.Write(f, binary.LittleEndian, data)
	}
	writeBytes := func(b []byte) {
		if err != nil {
			return
		}
		_, err = f.Write(b)
	}

	// ELF header.
	var ident [elf.EI_NIDENT]byte
	copy(ident[:], elf.ELFMAG)
	ident[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	ident[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	ident[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	ident[elf.EI_OSABI] = byte(elf.ELFOSABI_NONE)

	ehdr := elf.Header64{
		Ident:     ident,
		Type:      uint16(elf.ET_REL),
		Machine:   uint16(elf.EM_AARCH64),
		Version:   uint32(elf.EV_CURRENT),
		Entry:     0,
		Phoff:     0,
		Shoff:     shdrOff,
		Flags:     0,
		Ehsize:    uint16(elfHdr),
		Phentsize: 0,
		Phnum:     0,
		Shentsize: uint16(shdrSize),
		Shnum:     uint16(objNumSections),
		Shstrndx:  uint16(objSecShstrtab),
	}
	write(ehdr)

	// Section data.
	writeBytes(textBytes)
	writeBytes(rodataBytes)
	writeBytes(dataBytes)
	writeBytes(symtabBytes)
	writeBytes(strtab.data)
	writeBytes(relaBytes)
	writeBytes(relaDataBytes)
	writeBytes(objShstrtab)

	// Padding before section header table.
	curOff := uint64(elfHdr) + uint64(len(textBytes)) + rodataTotal + dataTotal +
		uint64(len(symtabBytes)) + uint64(len(strtab.data)) + uint64(len(relaBytes)) +
		uint64(len(relaDataBytes)) + uint64(len(objShstrtab))
	for curOff < shdrOff {
		writeBytes([]byte{0})
		curOff++
	}

	// Section header table.
	mkShdr := func(name uint32, typ uint32, flags uint64, addr, off2, size uint64, link, info uint32, align, entsize uint64) elf.Section64 {
		return elf.Section64{
			Name:      name,
			Type:      typ,
			Flags:     flags,
			Addr:      addr,
			Off:       off2,
			Size:      size,
			Link:      link,
			Info:      info,
			Addralign: align,
			Entsize:   entsize,
		}
	}

	shdrs := [objNumSections]elf.Section64{
		// [0] NULL
		mkShdr(0, uint32(elf.SHT_NULL), 0, 0, 0, 0, 0, 0, 0, 0),
		// [1] .text
		mkShdr(shstrText, uint32(elf.SHT_PROGBITS),
			uint64(elf.SHF_ALLOC|elf.SHF_EXECINSTR),
			0, textOff, uint64(len(textBytes)), 0, 0, 4, 0),
		// [2] .rodata
		mkShdr(shstrRodata, uint32(elf.SHT_PROGBITS),
			uint64(elf.SHF_ALLOC),
			0, rodataOff, rodataTotal, 0, 0, 1, 0),
		// [3] .data
		mkShdr(shstrData, uint32(elf.SHT_PROGBITS),
			uint64(elf.SHF_ALLOC|elf.SHF_WRITE),
			0, dataOff, dataTotal, 0, 0, 8, 0),
		// [4] .bss (SHT_NOBITS — no file data)
		mkShdr(shstrBss, uint32(elf.SHT_NOBITS),
			uint64(elf.SHF_ALLOC|elf.SHF_WRITE),
			0, dataOff+dataTotal, bssTotal, 0, 0, 8, 0),
		// [5] .symtab (link=.strtab, info=firstGlobal)
		mkShdr(shstrSymtab, uint32(elf.SHT_SYMTAB),
			0, 0, symtabOff, uint64(len(symtabBytes)),
			uint32(objSecStrtab), uint32(firstGlobal), 8, 24),
		// [6] .strtab
		mkShdr(shstrStrtab, uint32(elf.SHT_STRTAB),
			0, 0, strtabOff, uint64(len(strtab.data)), 0, 0, 1, 0),
		// [7] .rela.text (link=.symtab, info=.text)
		mkShdr(shstrRelaText, uint32(elf.SHT_RELA),
			0, 0, relaOff, uint64(len(relaBytes)),
			uint32(objSecSymtab), uint32(objSecText), 8, 24),
		// [8] .rela.data (link=.symtab, info=.data)
		mkShdr(shstrRelaData, uint32(elf.SHT_RELA),
			0, 0, relaDataOff, uint64(len(relaDataBytes)),
			uint32(objSecSymtab), uint32(objSecData), 8, 24),
		// [9] .shstrtab
		mkShdr(shstrShstrtab, uint32(elf.SHT_STRTAB),
			0, 0, shstrtabOff, uint64(len(objShstrtab)), 0, 0, 1, 0),
	}
	for _, sh := range shdrs {
		write(sh)
	}

	if err != nil {
		return fmt.Errorf("genObjectFile: write: %w", err)
	}
	_ = funcSymIdx
	_ = poolCount
	return nil
}
