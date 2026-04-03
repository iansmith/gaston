// linker.go — gaston simple ARM64 ELF linker.
//
// Reads one or more ET_REL (.o) files produced by objgen.go (or compatible
// ARM64 object files), links them with built-in gaston runtime helpers, and
// writes a Linux ARM64 ET_EXEC binary.
//
// Linking steps:
//  1. Parse each .o file: extract .text, .rodata, .data, .bss, symbols, rela.
//  2. Emit runtime helpers (_start, gaston_output, …) into a codeBuilder.
//  3. Append each file's .text as raw words to the codeBuilder.
//  4. Build a global symbol table (VA for every defined symbol).
//  5. Apply .rela.text relocations (ABS64 and CALL26).
//  6. Write the final ET_EXEC ELF.
package main

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
)

// linkerLoadBase is the virtual base address for the output ET_EXEC.
const linkerLoadBase = uint64(0x400000)

// objFile holds parsed data for one input .o file.
type objFile struct {
	path       string
	textData   []byte  // raw .text bytes (pool + code)
	rodataData []byte  // raw .rodata bytes
	dataData   []byte  // raw .data bytes
	bssSize    uint64  // .bss size in bytes (SHT_NOBITS has no data)
	syms       []lnkSym
	relas      []lnkRela

	// Set during layout:
	textBaseWord int    // word offset of this file's .text in the merged codeBuilder
	rodataOff    uint64 // byte offset within merged rodata
	dataOff      uint64 // byte offset within merged data
	bssOff       uint64 // byte offset within merged bss
}

// lnkSym is one symbol table entry (simplified).
type lnkSym struct {
	name    string
	value   uint64 // byte offset within section (or 0 for SHN_UNDEF)
	size    uint64
	secName string // ".text", ".rodata", ".data", ".bss", or "" for undef
	binding elf.SymBind
	typ     elf.SymType
}

// lnkRela is one RELA record for .rela.text.
type lnkRela struct {
	offset uint64 // byte offset within this file's .text
	symIdx uint32 // index into this file's syms slice
	rtype  uint32 // R_AARCH64_ABS64 or R_AARCH64_CALL26
	addend int64
}

// loadObjFile reads and parses an ET_REL file.
func loadObjFile(path string) (*objFile, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("linker: open %s: %w", path, err)
	}
	defer f.Close()

	if f.Type != elf.ET_REL {
		return nil, fmt.Errorf("linker: %s is not an ET_REL object file", path)
	}

	obj := &objFile{path: path}

	// Read section data.
	readSec := func(name string) ([]byte, error) {
		sec := f.Section(name)
		if sec == nil {
			return nil, nil
		}
		if sec.Type == elf.SHT_NOBITS {
			return nil, nil // BSS: no file data
		}
		return sec.Data()
	}

	if obj.textData, err = readSec(".text"); err != nil {
		return nil, fmt.Errorf("linker: %s .text: %w", path, err)
	}
	if obj.rodataData, err = readSec(".rodata"); err != nil {
		return nil, fmt.Errorf("linker: %s .rodata: %w", path, err)
	}
	if obj.dataData, err = readSec(".data"); err != nil {
		return nil, fmt.Errorf("linker: %s .data: %w", path, err)
	}
	if sec := f.Section(".bss"); sec != nil {
		obj.bssSize = sec.Size
	}

	// Build section name → index map for symbol resolution.
	secNames := make([]string, len(f.Sections))
	for i, sec := range f.Sections {
		secNames[i] = sec.Name
	}

	// Parse raw .symtab (to avoid off-by-one issues with f.Symbols()).
	symtabSec := f.Section(".symtab")
	strtabSec := f.Section(".strtab")
	if symtabSec != nil && strtabSec != nil {
		symData, err2 := symtabSec.Data()
		if err2 != nil {
			return nil, fmt.Errorf("linker: %s .symtab: %w", path, err2)
		}
		strData, err2 := strtabSec.Data()
		if err2 != nil {
			return nil, fmt.Errorf("linker: %s .strtab: %w", path, err2)
		}
		numSyms := len(symData) / 24
		for i := 0; i < numSyms; i++ {
			raw := symData[i*24 : i*24+24]
			nameOff := binary.LittleEndian.Uint32(raw[0:4])
			info := raw[4]
			shndx := binary.LittleEndian.Uint16(raw[6:8])
			value := binary.LittleEndian.Uint64(raw[8:16])
			size := binary.LittleEndian.Uint64(raw[16:24])

			// Extract NUL-terminated name from strtab.
			name := ""
			if int(nameOff) < len(strData) {
				end := int(nameOff)
				for end < len(strData) && strData[end] != 0 {
					end++
				}
				name = string(strData[nameOff:end])
			}

			binding := elf.SymBind(info >> 4)
			typ := elf.SymType(info & 0xF)

			secName := ""
			if shndx == uint16(elf.SHN_UNDEF) {
				secName = ""
			} else if int(shndx) < len(secNames) {
				secName = secNames[shndx]
			}

			obj.syms = append(obj.syms, lnkSym{
				name:    name,
				value:   value,
				size:    size,
				secName: secName,
				binding: binding,
				typ:     typ,
			})
		}
	}

	// Parse .rela.text.
	relaSec := f.Section(".rela.text")
	if relaSec != nil {
		relaData, err2 := relaSec.Data()
		if err2 != nil {
			return nil, fmt.Errorf("linker: %s .rela.text: %w", path, err2)
		}
		numRelas := len(relaData) / 24
		for i := 0; i < numRelas; i++ {
			raw := relaData[i*24 : i*24+24]
			off := binary.LittleEndian.Uint64(raw[0:8])
			info := binary.LittleEndian.Uint64(raw[8:16])
			addend := int64(binary.LittleEndian.Uint64(raw[16:24]))
			symIdx := uint32(info >> 32)
			rtype := uint32(info)
			obj.relas = append(obj.relas, lnkRela{
				offset: off,
				symIdx: symIdx,
				rtype:  rtype,
				addend: addend,
			})
		}
	}

	return obj, nil
}

// link reads the object files at objpaths, links them, and writes ET_EXEC to outpath.
func link(outpath string, objpaths []string) error {
	// ── load all .o files ─────────────────────────────────────────────────
	objs := make([]*objFile, len(objpaths))
	for i, p := range objpaths {
		obj, err := loadObjFile(p)
		if err != nil {
			return err
		}
		objs[i] = obj
	}

	// ── emit runtime helper code ───────────────────────────────────────────
	// The helper codeBuilder has one pool entry: the allocator's free-list head
	// pointer (gaston_free_list_head), which lives in BSS.  Its address is
	// patched into the pool after the BSS layout is determined below.
	freeListSynth := IRGlobal{Name: freeListGlobalName, Size: 1}
	cb := newCodeBuilder([]IRGlobal{freeListSynth}, nil, nil)
	gen := &elfGen{
		cb:            cb,
		pendingParams: make([]paramArg, 0, 8),
		isGlobalPtr:   make(map[string]bool),
		funcRetType:   make(map[string]TypeKind),
	}

	// _start: call gaston_main (extern → fixed-up after symbol layout), then exit.
	cb.defineLabel("_start")
	cb.emitBL("gaston_main") // fixup resolved after all user text is loaded
	cb.emitMOVimm(regX0, 0)
	cb.emitMOVimm(regX8, 94)
	cb.emit(encSVC(0))

	gen.emitOutputFn()
	gen.emitInputFn()
	gen.emitPrintCharFn()
	gen.emitPrintStringFn()
	gen.emitPrintDoubleFn()
	gen.emitMallocFn()
	gen.emitFreeFn()

	helperWords := len(cb.instrs) // number of words used by runtime helpers

	// ── compute layout for merged sections ───────────────────────────────
	var totalRodata, totalData, totalBss uint64
	for _, obj := range objs {
		obj.textBaseWord = helperWords + len(cb.instrs) - helperWords
		// Append this file's text as words.
		for i := 0; i+3 < len(obj.textData); i += 4 {
			w := binary.LittleEndian.Uint32(obj.textData[i:])
			cb.instrs = append(cb.instrs, w)
		}
		obj.textBaseWord = helperWords + int(uint64(obj.textBaseWord) - uint64(helperWords))
	}

	// Recompute: textBaseWord for file k = helperWords + sum of prev file text words.
	acc := helperWords
	for _, obj := range objs {
		obj.textBaseWord = acc
		acc += len(obj.textData) / 4
	}

	// Rebuild cb.instrs with correct per-file bases.
	cb.instrs = cb.instrs[:helperWords]
	for _, obj := range objs {
		for i := 0; i+3 < len(obj.textData); i += 4 {
			cb.instrs = append(cb.instrs, binary.LittleEndian.Uint32(obj.textData[i:]))
		}
	}

	for _, obj := range objs {
		obj.rodataOff = totalRodata
		totalRodata += uint64(len(obj.rodataData))
		obj.dataOff = totalData
		totalData += uint64(len(obj.dataData))
		obj.bssOff = totalBss
		totalBss += obj.bssSize
	}

	// ── compute virtual addresses ─────────────────────────────────────────
	// Header: 64 bytes ELF + 4 × 56 bytes phdrs = 288 = 0x120
	const lnkPhdrs = 4
	lnkHdrEnd := uint64(64 + lnkPhdrs*56)
	lnkCodeBase := linkerLoadBase + lnkHdrEnd

	totalCodeBytes := uint64(len(cb.instrs)) * 4
	rodataBase := nextPage(lnkCodeBase + totalCodeBytes)
	dataBase := nextPage(rodataBase + totalRodata)
	bssBase := nextPage(dataBase + totalData)

	// The allocator's free-list head sits at the end of the BSS segment
	// (after all object-file BSS).  Patch its address into the helper pool.
	freeListHeadVA := bssBase + totalBss
	totalBss += 8 // 8-byte pointer, zero-initialised by the OS
	cb.patchPool(map[string]uint64{freeListGlobalName: freeListHeadVA})

	// ── build global symbol table (name → VA) ─────────────────────────────
	symVA := make(map[string]uint64)

	// Built-in helper labels.
	for name, wordIdx := range cb.labels {
		symVA[name] = lnkCodeBase + uint64(wordIdx)*4
	}

	// User function and variable symbols from all .o files.
	for _, obj := range objs {
		for _, sym := range obj.syms {
			if sym.binding != elf.STB_GLOBAL || sym.name == "" {
				continue
			}
			switch sym.secName {
			case ".text":
				symVA[sym.name] = lnkCodeBase + uint64(obj.textBaseWord)*4 + sym.value
			case ".rodata":
				symVA[sym.name] = rodataBase + obj.rodataOff + sym.value
			case ".data":
				symVA[sym.name] = dataBase + obj.dataOff + sym.value
			case ".bss":
				symVA[sym.name] = bssBase + obj.bssOff + sym.value
			}
		}
	}

	// Add gaston_main label to cb.labels so applyFixups() can resolve _start's BL.
	if va, ok := symVA["gaston_main"]; ok {
		wordIdx := int((va - lnkCodeBase) / 4)
		cb.labels["gaston_main"] = wordIdx
	} else {
		return fmt.Errorf("linker: undefined symbol 'gaston_main' (no main function)")
	}

	// Resolve internal branch fixups (helper code internal labels + gaston_main).
	if err := cb.applyFixups(); err != nil {
		return fmt.Errorf("linker: fixups: %w", err)
	}

	// ── apply .rela.text relocations ──────────────────────────────────────
	for _, obj := range objs {
		// Build a per-file symIdx → VA map (includes locals for ABS64 of str lits).
		fileSymVA := make(map[uint32]uint64)
		for i, sym := range obj.syms {
			va := uint64(0)
			switch sym.secName {
			case ".text":
				va = lnkCodeBase + uint64(obj.textBaseWord)*4 + sym.value
			case ".rodata":
				va = rodataBase + obj.rodataOff + sym.value
			case ".data":
				va = dataBase + obj.dataOff + sym.value
			case ".bss":
				va = bssBase + obj.bssOff + sym.value
			case "":
				// Undefined: look up in global table.
				if sym.binding == elf.STB_GLOBAL && sym.name != "" {
					va = symVA[sym.name]
				}
			}
			fileSymVA[uint32(i)] = va
		}

		for _, rela := range obj.relas {
			// Word/byte index within merged cb.instrs.
			byteInFile := rela.offset
			wordIdx := obj.textBaseWord + int(byteInFile/4)

			symVAval := fileSymVA[rela.symIdx]
			if symVAval == 0 && rela.symIdx < uint32(len(obj.syms)) {
				// Try global table by name.
				symVAval = symVA[obj.syms[rela.symIdx].name]
			}

			switch rela.rtype {
			case rAArch64Abs64:
				// Write 64-bit absolute address into the pool entry (2 uint32 words).
				addr := symVAval + uint64(rela.addend)
				cb.instrs[wordIdx] = uint32(addr)
				cb.instrs[wordIdx+1] = uint32(addr >> 32)

			case rAArch64Call26:
				// Patch BL instruction: offset = (sym_va - instr_va) / 4
				instrVA := lnkCodeBase + uint64(wordIdx)*4
				offset := int64(symVAval) - int64(instrVA) + rela.addend
				wordOff := offset / 4
				old := cb.instrs[wordIdx]
				cb.instrs[wordIdx] = (old & 0xFC000000) | (uint32(wordOff) & 0x3FFFFFF)
			}
		}
	}

	// ── assemble merged rodata / data ────────────────────────────────────
	rodataBytes := make([]byte, totalRodata)
	for _, obj := range objs {
		copy(rodataBytes[obj.rodataOff:], obj.rodataData)
	}
	dataBytes := make([]byte, totalData)
	for _, obj := range objs {
		copy(dataBytes[obj.dataOff:], obj.dataData)
	}

	// ── write ET_EXEC ELF ─────────────────────────────────────────────────
	codeFileSz := lnkHdrEnd + totalCodeBytes
	rodataFileOff := nextPage(lnkHdrEnd + totalCodeBytes)
	rodataFileSz := totalRodata

	dataFileOff := nextPage(rodataFileOff + totalRodata)
	dataFileSz := totalData

	entryVA := uint64(0)
	if idx, ok := cb.labels["_start"]; ok {
		entryVA = lnkCodeBase + uint64(idx)*4
	}

	out, err := os.OpenFile(outpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("linker: %w", err)
	}
	defer out.Close()

	write := func(v interface{}) {
		if err != nil {
			return
		}
		err = binary.Write(out, binary.LittleEndian, v)
	}
	writeB := func(b []byte) {
		if err != nil || len(b) == 0 {
			return
		}
		_, err = out.Write(b)
	}
	pad := func(n uint64) {
		if n > 0 {
			writeB(make([]byte, n))
		}
	}

	// ELF header.
	var ident [elf.EI_NIDENT]byte
	copy(ident[:], elf.ELFMAG)
	ident[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	ident[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	ident[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	ident[elf.EI_OSABI] = byte(elf.ELFOSABI_NONE)
	write(elf.Header64{
		Ident:     ident,
		Type:      uint16(elf.ET_EXEC),
		Machine:   uint16(elf.EM_AARCH64),
		Version:   uint32(elf.EV_CURRENT),
		Entry:     entryVA,
		Phoff:     64,
		Shoff:     0,
		Ehsize:    64,
		Phentsize: 56,
		Phnum:     uint16(lnkPhdrs),
	})

	// PT_LOAD — code (RX).
	write(elf.Prog64{
		Type:   uint32(elf.PT_LOAD),
		Flags:  uint32(elf.PF_R | elf.PF_X),
		Off:    0,
		Vaddr:  linkerLoadBase,
		Paddr:  linkerLoadBase,
		Filesz: codeFileSz,
		Memsz:  codeFileSz,
		Align:  pageSize,
	})
	// PT_LOAD — rodata (R).
	write(elf.Prog64{
		Type:   uint32(elf.PT_LOAD),
		Flags:  uint32(elf.PF_R),
		Off:    rodataFileOff,
		Vaddr:  rodataBase,
		Paddr:  rodataBase,
		Filesz: rodataFileSz,
		Memsz:  rodataFileSz,
		Align:  pageSize,
	})
	// PT_LOAD — data (RW, initialized).
	write(elf.Prog64{
		Type:   uint32(elf.PT_LOAD),
		Flags:  uint32(elf.PF_R | elf.PF_W),
		Off:    dataFileOff,
		Vaddr:  dataBase,
		Paddr:  dataBase,
		Filesz: dataFileSz,
		Memsz:  dataFileSz,
		Align:  pageSize,
	})
	// PT_LOAD — bss (RW, zero-initialized).
	write(elf.Prog64{
		Type:   uint32(elf.PT_LOAD),
		Flags:  uint32(elf.PF_R | elf.PF_W),
		Off:    0,
		Vaddr:  bssBase,
		Paddr:  bssBase,
		Filesz: 0,
		Memsz:  totalBss,
		Align:  pageSize,
	})

	// Code section (helper + user).
	write(cb.instrs)

	// Rodata.
	codeEnd := lnkHdrEnd + totalCodeBytes
	pad(rodataFileOff - codeEnd)
	writeB(rodataBytes)

	// Data.
	dataEnd := rodataFileOff + totalRodata
	pad(dataFileOff - dataEnd)
	writeB(dataBytes)

	if err != nil {
		return fmt.Errorf("linker: write: %w", err)
	}
	return nil
}
