// linker.go — gaston simple ARM64 ELF linker.
//
// Reads one or more ET_REL (.o) files produced by objgen.go (or compatible
// ARM64 object files), links them with built-in gaston runtime helpers, and
// writes a Linux ARM64 ET_EXEC binary.
//
// Linking steps:
//  1. Parse each .o file: extract .text, .rodata, .data, .bss, symbols, rela.
//  2. Emit runtime helpers (_start, sbrk, POSIX syscalls) into a codeBuilder.
//  3. Append each file's .text as raw words to the codeBuilder.
//  4. Build a global symbol table (VA for every defined symbol).
//  5. Apply .rela.text relocations (ABS64 and CALL26).
//  6. Write the final ET_EXEC ELF.
package main

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"strings"
)

// resolveLib searches for libname.a in the given directories.
// Returns the full path to the first match, or an error if not found.
func resolveLib(name string, searchPaths []string) (string, error) {
	filename := "lib" + name + ".a"
	for _, dir := range searchPaths {
		path := dir + "/" + filename
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("cannot find -l%s (searched: %v)", name, searchPaths)
}

// linkerLoadBase is the virtual base address for the output ET_EXEC.
const linkerLoadBase = uint64(0x400000)

// objFile holds parsed data for one input .o file.
type objFile struct {
	path       string
	textData   []byte // raw .text bytes (pool + code)
	rodataData []byte // raw .rodata bytes
	dataData   []byte // raw .data bytes
	bssSize    uint64 // .bss size in bytes (SHT_NOBITS has no data)
	syms       []lnkSym
	relas      []lnkRela // .rela.text entries
	dataRelas  []lnkRela // .rela.data entries

	// Set during layout:
	textBaseWord int    // word offset of this file's .text in the merged codeBuilder
	rodataOff    uint64 // byte offset within merged rodata
	dataOff      uint64 // byte offset within merged data
	bssOff       uint64 // byte offset within merged bss

	// fromArchive marks lazily-pulled archive members. Duplicate strong
	// definitions are a hard error between explicit user objects, but only a
	// warning (last definition wins, matching historical behavior) when an
	// archive member is involved — shipped archives legitimately contain
	// duplicates (e.g. __isnand in both mathbuiltins and libm).
	fromArchive bool
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

// commonInfo carries the merged size/alignment of a COMMON (tentative) symbol.
type commonInfo struct{ size, align uint64 }

// isLinkableDef reports whether sym is a named definition that satisfies
// references and belongs in a symbol index. Both strong (STB_GLOBAL) and
// weak (STB_WEAK) definitions qualify: per ELF gABI a weak def is a real
// definition — it satisfies references and prevents archive pulls — even
// though strong defs and COMMONs win at precedence.
func isLinkableDef(sym lnkSym) bool {
	return (sym.binding == elf.STB_GLOBAL || sym.binding == elf.STB_WEAK) &&
		sym.secName != "" && sym.name != ""
}

// sectionVA computes the linked VA of a symbol defined in one of the four
// storage sections, from its object's placement in the merged layout.
// ok=false for any other section (COMMON, undefined).
func sectionVA(obj *objFile, sym lnkSym, codeBase, rodataBase, dataBase, bssBase uint64) (uint64, bool) {
	switch sym.secName {
	case ".text":
		return codeBase + uint64(obj.textBaseWord)*4 + sym.value, true
	case ".rodata":
		return rodataBase + obj.rodataOff + sym.value, true
	case ".data":
		return dataBase + obj.dataOff + sym.value, true
	case ".bss":
		return bssBase + obj.bssOff + sym.value, true
	}
	return 0, false
}

// weakInterposedVA implements gABI interposition for weak definitions: if
// sym is a weak def in obj but the chosen winner for its name lives
// elsewhere (a strong def, a COMMON, or an earlier weak def), the object's
// own references must resolve to the winner's VA, not the local dead copy.
// Returns va unchanged when sym is not a weak def or is itself the winner.
func weakInterposedVA(obj *objFile, sym lnkSym, weakDef map[string]*objFile, symVA map[string]uint64, va uint64) uint64 {
	if sym.binding != elf.STB_WEAK || sym.secName == "" || sym.name == "" {
		return va
	}
	if w, ok := weakDef[sym.name]; ok && w == obj {
		return va // this object holds the winning weak def
	}
	return symVA[sym.name] // loser (or suppressed by strong/COMMON): use the winner
}

// loadObjFile reads and parses an ET_REL file from disk.
func loadObjFile(path string) (*objFile, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("linker: open %s: %w", path, err)
	}
	defer f.Close()
	return parseObjELF(path, f)
}

// loadObjFromBytes parses an ET_REL object from an in-memory byte slice
// (used when extracting members from an ar archive).
func loadObjFromBytes(name string, data []byte) (*objFile, error) {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("linker: parse %s: %w", name, err)
	}
	defer f.Close()
	return parseObjELF(name, f)
}

// parseObjELF extracts sections, symbols, and relocations from an open ELF file.
func parseObjELF(path string, f *elf.File) (*objFile, error) {
	if f.Type != elf.ET_REL {
		return nil, fmt.Errorf("linker: %s is not an ET_REL object file", path)
	}

	obj := &objFile{path: path}
	var err error

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

	// ── fold custom allocatable sections into .text / .data / .bss ────────
	// gaston emits __attribute__((section("name"))) code and data into their
	// own sections. The linker's layout model knows only the four standard
	// sections, so each custom section is folded into the appropriate one at
	// parse time: its bytes are appended, and its symbols and relocations are
	// rebased below. Placement semantics (linker scripts) are out of scope.
	type foldInfo struct {
		target string // ".text", ".data", or ".bss"
		off    uint64 // byte offset of the folded section within the target
	}
	folds := make(map[uint16]foldInfo)
	for i, sec := range f.Sections {
		switch sec.Name {
		case "", ".text", ".rodata", ".data", ".bss":
			continue
		}
		if sec.Type != elf.SHT_PROGBITS && sec.Type != elf.SHT_NOBITS {
			continue
		}
		if sec.Flags&elf.SHF_ALLOC == 0 {
			continue
		}
		switch {
		case sec.Flags&elf.SHF_EXECINSTR != 0:
			for len(obj.textData)%4 != 0 {
				obj.textData = append(obj.textData, 0)
			}
			off := uint64(len(obj.textData))
			if sec.Type == elf.SHT_PROGBITS {
				d, err2 := sec.Data()
				if err2 != nil {
					return nil, fmt.Errorf("linker: %s %s: %w", path, sec.Name, err2)
				}
				obj.textData = append(obj.textData, d...)
			}
			folds[uint16(i)] = foldInfo{".text", off}
		case sec.Type == elf.SHT_NOBITS:
			off := (obj.bssSize + 15) &^ 15
			obj.bssSize = off + sec.Size
			folds[uint16(i)] = foldInfo{".bss", off}
		default:
			for len(obj.dataData)%16 != 0 {
				obj.dataData = append(obj.dataData, 0)
			}
			off := uint64(len(obj.dataData))
			d, err2 := sec.Data()
			if err2 != nil {
				return nil, fmt.Errorf("linker: %s %s: %w", path, sec.Name, err2)
			}
			obj.dataData = append(obj.dataData, d...)
			folds[uint16(i)] = foldInfo{".data", off}
		}
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
			} else if shndx == uint16(elf.SHN_COMMON) {
				// Tentative definition: value = alignment, size = byte size.
				secName = "COMMON"
			} else if fi, ok := folds[shndx]; ok {
				// Symbol in a folded custom section: rebase into the target.
				secName = fi.target
				value += fi.off
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

	// Parse relocation sections. Standard .rela.text/.rela.data feed the two
	// rela lists directly; relas of folded custom sections are rebased by the
	// fold offset and routed to the list matching the fold target.
	parseRelas := func(secName string, base uint64) ([]lnkRela, error) {
		relaSec := f.Section(secName)
		if relaSec == nil {
			return nil, nil
		}
		relaData, err2 := relaSec.Data()
		if err2 != nil {
			return nil, fmt.Errorf("linker: %s %s: %w", path, secName, err2)
		}
		var out []lnkRela
		numRelas := len(relaData) / 24
		for i := 0; i < numRelas; i++ {
			raw := relaData[i*24 : i*24+24]
			off := binary.LittleEndian.Uint64(raw[0:8])
			info := binary.LittleEndian.Uint64(raw[8:16])
			addend := int64(binary.LittleEndian.Uint64(raw[16:24]))
			out = append(out, lnkRela{
				offset: base + off,
				symIdx: uint32(info >> 32),
				rtype:  uint32(info),
				addend: addend,
			})
		}
		return out, nil
	}

	if rs, err2 := parseRelas(".rela.text", 0); err2 != nil {
		return nil, err2
	} else {
		obj.relas = append(obj.relas, rs...)
	}
	if rs, err2 := parseRelas(".rela.data", 0); err2 != nil {
		return nil, err2
	} else {
		obj.dataRelas = append(obj.dataRelas, rs...)
	}
	for i, sec := range f.Sections {
		fi, ok := folds[uint16(i)]
		if !ok {
			continue
		}
		rs, err2 := parseRelas(".rela"+sec.Name, fi.off)
		if err2 != nil {
			return nil, err2
		}
		switch fi.target {
		case ".text":
			obj.relas = append(obj.relas, rs...)
		case ".data":
			obj.dataRelas = append(obj.dataRelas, rs...)
		}
	}

	return obj, nil
}

// link reads the object and archive files at inputpaths, links them, and
// writes ET_EXEC to outpath.  Files ending in ".a" are treated as ar archives
// and their members are lazy-linked (only included when they resolve an
// otherwise-undefined symbol).
func link(outpath string, inputpaths []string) error {
	return linkWithObjs(outpath, nil, inputpaths)
}

// linkWithObjs links pre-loaded objects (preObjs) together with objects/archives
// loaded from inputpaths, and writes the final ET_EXEC to outpath.
func linkWithObjs(outpath string, preObjs []*objFile, inputpaths []string) error {
	// ── phase 1: load .o files; collect .a archives ────────────────────────
	objs := append([]*objFile(nil), preObjs...)
	type archiveState struct {
		members []arMember
		symMap  map[string]int // symbol name → member index
		used    []bool         // which members have been pulled in
	}
	var archives []archiveState

	for _, p := range inputpaths {
		if strings.HasSuffix(p, ".a") {
			members, symMap, err := archiveRead(p)
			if err != nil {
				return err
			}
			archives = append(archives, archiveState{
				members: members,
				symMap:  symMap,
				used:    make([]bool, len(members)),
			})
		} else {
			obj, err := loadObjFile(p)
			if err != nil {
				return err
			}
			objs = append(objs, obj)
		}
	}

	// ── phase 2: lazy-link archive members ────────────────────────────────
	// Force-pull symbols referenced by _start helper code that aren't in user objects.
	for _, forceSym := range []string{"__posix_stdio_init"} {
		for ai := range archives {
			mi, ok := archives[ai].symMap[forceSym]
			if !ok || archives[ai].used[mi] {
				continue
			}
			archives[ai].used[mi] = true
			m := archives[ai].members[mi]
			pulled, err := loadObjFromBytes(m.name, m.data)
			if err != nil {
				return fmt.Errorf("link: archive member %s: %w", m.name, err)
			}
			pulled.fromArchive = true
			objs = append(objs, pulled)
		}
	}

	// Repeatedly scan undefined symbols in already-loaded objects; pull in any
	// archive member that defines one.  Repeat until stable.
	for {
		defined := make(map[string]bool)
		for _, obj := range objs {
			for _, sym := range obj.syms {
				// A weak def in a user object PREVENTS pulling an archive
				// member with a strong def of the same name (see isLinkableDef).
				if isLinkableDef(sym) {
					defined[sym.name] = true
				}
			}
		}

		added := false
		for _, obj := range objs {
			for _, sym := range obj.syms {
				if sym.binding != elf.STB_GLOBAL || sym.secName != "" || sym.name == "" {
					continue
				}
				if defined[sym.name] {
					continue
				}
				// Undefined symbol — search archives; first resolver wins.
				for ai := range archives {
					mi, ok := archives[ai].symMap[sym.name]
					if !ok || archives[ai].used[mi] {
						continue
					}
					archives[ai].used[mi] = true
					m := archives[ai].members[mi]
					pulled, err := loadObjFromBytes(m.name, m.data)
					if err != nil {
						return fmt.Errorf("link: archive member %s: %w", m.name, err)
					}
					pulled.fromArchive = true
					objs = append(objs, pulled)
					added = true
					fmt.Fprintf(os.Stderr, "linker: pulled %s for %s\n", m.name, sym.name)
					break
				}
			}
		}
		if !added {
			break
		}
	}

	// ── emit runtime helper code ───────────────────────────────────────────
	// The helper codeBuilder has one pool entry: sbrk's current-break pointer
	// (sbrk_cur_brk), which lives in BSS.  Its address is patched after layout.
	sbrkSynth := IRGlobal{Name: sbrkGlobalName, Size: 1}
	cb := newCodeBuilder([]IRGlobal{sbrkSynth}, nil, nil, nil)
	gen := &elfGen{
		cb:            cb,
		pendingParams: make([]paramArg, 0, 8),
		isGlobalPtr:   make(map[string]bool),
		funcRetType:   make(map[string]TypeKind),
	}

	// _start: call __posix_stdio_init, then main, then exit(0).
	cb.defineLabel("_start")
	cb.emitBL("__posix_stdio_init")
	cb.emitBL("main")
	cb.emitMOVimm(regX0, 0)
	cb.emitMOVimm(regX8, 94)
	cb.emit(encSVC(0))

	gen.emitSbrkFn()
	gen.emitPosixSyscalls()
	gen.emitSetjmpFns()

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
		obj.textBaseWord = helperWords + int(uint64(obj.textBaseWord)-uint64(helperWords))
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

	align16 := func(v uint64) uint64 { return (v + 15) &^ 15 }
	for _, obj := range objs {
		obj.rodataOff = align16(totalRodata)
		totalRodata = obj.rodataOff + uint64(len(obj.rodataData))
		obj.dataOff = align16(totalData)
		totalData = obj.dataOff + uint64(len(obj.dataData))
		obj.bssOff = align16(totalBss)
		totalBss = obj.bssOff + obj.bssSize
	}

	// ── merge and allocate COMMON (tentative) symbols ─────────────────────
	// Classic COMMON semantics among LOADED objects: a real section
	// definition wins over any tentative one regardless of load order;
	// multiple tentative definitions merge into one BSS slot of the MAX size
	// and MAX alignment. Two real definitions of one global are an error
	// between user objects, tolerated (with a warning) when an archive
	// member is involved. Note: archive members are only loaded on demand —
	// a tentative def in an earlier member can satisfy a reference so a
	// later member's real def is never pulled (GNU ld behaves the same).
	//
	// WEAK DEFINITIONS (STB_WEAK): Per ELF gABI, precedence is:
	// strong (STB_GLOBAL def) > COMMON > weak (STB_WEAK def).
	// Among multiple weak defs with no higher-precedence def: first in link
	// order wins, no error. Strong+weak same name: no duplicate error, strong
	// wins. ALL references resolve to the single chosen winner.
	commons := make(map[string]commonInfo)
	realDef := make(map[string]*objFile) // name → defining object
	weakDef := make(map[string]*objFile) // name → first STB_WEAK def in link order
	for _, obj := range objs {
		for _, sym := range obj.syms {
			if sym.name == "" {
				continue
			}
			// Handle strong (STB_GLOBAL) definitions.
			if sym.binding == elf.STB_GLOBAL {
				switch sym.secName {
				case ".text", ".rodata", ".data", ".bss":
					if prev, dup := realDef[sym.name]; dup {
						if !prev.fromArchive && !obj.fromArchive {
							return fmt.Errorf("linker: duplicate definition of '%s' (in %s and %s)",
								sym.name, prev.path, obj.path)
						}
						// Archive member involved: tolerated (shipped archives contain
						// duplicates, e.g. __isnand in mathbuiltins and libm). An
						// explicit user object beats an archive member; between two
						// archive members the last pulled wins (historical behavior).
						winner := obj
						if !prev.fromArchive && obj.fromArchive {
							winner = prev
						}
						fmt.Fprintf(os.Stderr, "linker: warning: duplicate definition of '%s' (%s and %s); using %s\n",
							sym.name, prev.path, obj.path, winner.path)
						realDef[sym.name] = winner
						continue
					}
					realDef[sym.name] = obj
				case "COMMON":
					ci := commons[sym.name]
					if sym.size > ci.size {
						ci.size = sym.size
					}
					al := sym.value // ELF convention: st_value of a COMMON = alignment
					if al == 0 || al&(al-1) != 0 || al > 4096 {
						al = 8 // zero, non-power-of-two, or absurd alignment: use default
					}
					if al > ci.align {
						ci.align = al
					}
					commons[sym.name] = ci
				}
			} else if sym.binding == elf.STB_WEAK {
				// Weak defs do NOT enter realDef (no duplicate error involvement),
				// and they do NOT suppress COMMONs. First weak def per name in
				// link order is the candidate winner; precedence suppression
				// happens after the scan.
				switch sym.secName {
				case ".text", ".rodata", ".data", ".bss":
					if _, already := weakDef[sym.name]; !already {
						weakDef[sym.name] = obj
					}
				}
			}
		}
	}
	// A real definition anywhere supersedes the tentative ones.
	for name := range commons {
		if _, ok := realDef[name]; ok {
			delete(commons, name)
		}
	}

	// Weak definitions are suppressed (lose) when a real definition or a COMMON
	// of the same name exists. Precedence: strong > COMMON > weak.
	for name := range weakDef {
		if _, hasStrong := realDef[name]; hasStrong {
			delete(weakDef, name)
		} else if _, hasCommon := commons[name]; hasCommon {
			delete(weakDef, name)
		}
	}
	// Allocate surviving COMMONs at the end of merged BSS, in sorted order
	// for determinism. Must happen before sbrkCurBrkVA is computed so the
	// heap break lands after this storage.
	commonNames := make([]string, 0, len(commons))
	for name := range commons {
		commonNames = append(commonNames, name)
	}
	sort.Strings(commonNames)
	commonOff := make(map[string]uint64, len(commons))
	for _, name := range commonNames {
		ci := commons[name]
		totalBss = (totalBss + ci.align - 1) &^ (ci.align - 1)
		commonOff[name] = totalBss
		totalBss += ci.size
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

	// sbrk's current-break pointer sits at the end of BSS.
	sbrkCurBrkVA := bssBase + totalBss
	totalBss += 8
	cb.patchPool(map[string]uint64{sbrkGlobalName: sbrkCurBrkVA})

	// ── build global symbol table (name → VA) ─────────────────────────────
	symVA := make(map[string]uint64)

	// Built-in helper labels.
	for name, wordIdx := range cb.labels {
		symVA[name] = lnkCodeBase + uint64(wordIdx)*4
	}

	// User function and variable symbols from all .o files. For duplicated
	// names, realDef records the winning object — only its definition enters
	// symVA, so link order cannot silently change which copy wins.
	// For weak defs, only the surviving weak defs (those in weakDef after
	// precedence suppression) enter symVA.
	for _, obj := range objs {
		for _, sym := range obj.syms {
			if sym.name == "" {
				continue
			}
			if sym.binding == elf.STB_GLOBAL {
				if w, ok := realDef[sym.name]; ok && w != obj {
					continue // a different object owns this name
				}
				if va, ok := sectionVA(obj, sym, lnkCodeBase, rodataBase, dataBase, bssBase); ok {
					symVA[sym.name] = va
				}
			} else if sym.binding == elf.STB_WEAK {
				// Include only the winning weak def: the one recorded in weakDef
				// with no realDef or common of that name (already deleted above if
				// either exists).
				if w, ok := weakDef[sym.name]; ok && w == obj {
					if va, ok := sectionVA(obj, sym, lnkCodeBase, rodataBase, dataBase, bssBase); ok {
						symVA[sym.name] = va
					}
				}
			}
		}
	}

	// Merged COMMON symbols live at the end of BSS. (commonOff only holds
	// names with no real definition, so this never shadows one.)
	for name, off := range commonOff {
		symVA[name] = bssBase + off
	}

	// Add main and __posix_stdio_init labels to cb.labels so applyFixups() can
	// resolve _start's BL instructions to archive-defined functions.
	if va, ok := symVA["main"]; ok {
		wordIdx := int((va - lnkCodeBase) / 4)
		cb.labels["main"] = wordIdx
	} else {
		return fmt.Errorf("linker: undefined symbol 'main' (no main function)")
	}
	if va, ok := symVA["__posix_stdio_init"]; ok {
		wordIdx := int((va - lnkCodeBase) / 4)
		cb.labels["__posix_stdio_init"] = wordIdx
	}

	// Resolve internal branch fixups (helper code internal labels).
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
			case "COMMON":
				// Tentative definition: resolve to the merged BSS slot (or a
				// real definition elsewhere) via the global table. Only GLOBAL
				// COMMONs are merged; anything else must not alias a global.
				if sym.binding == elf.STB_GLOBAL {
					va = symVA[sym.name]
				}
			case "":
				// Undefined: look up in global table.
				if sym.binding == elf.STB_GLOBAL && sym.name != "" {
					va = symVA[sym.name]
				}
			}
			va = weakInterposedVA(obj, sym, weakDef, symVA, va)
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
			if symVAval == 0 && rela.symIdx < uint32(len(obj.syms)) {
				sym := obj.syms[rela.symIdx]
				if sym.name != "" {
					fmt.Fprintf(os.Stderr, "linker: warning: unresolved symbol '%s' (rela.text) in %s\n", sym.name, obj.path)
				}
			}

			switch rela.rtype {
			case rAArch64Abs64:
				// Write 64-bit absolute address into the pool entry (2 uint32 words).
				addr := symVAval + uint64(rela.addend)
				cb.instrs[wordIdx] = uint32(addr)
				cb.instrs[wordIdx+1] = uint32(addr >> 32)
				if addr == 0 || (rela.symIdx < uint32(len(obj.syms)) && obj.syms[rela.symIdx].name == "__malloc_av_") {
					symName := ""
					if rela.symIdx < uint32(len(obj.syms)) {
						symName = obj.syms[rela.symIdx].name
					}
					fmt.Fprintf(os.Stderr, "linker: ABS64 %s pool[%d] wordIdx=%d → %#x (sym=%s)\n",
						obj.path, rela.offset/8, wordIdx, addr, symName)
				}

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

	// ── apply .rela.data relocations ─────────────────────────────────────
	for _, obj := range objs {
		// Build per-file symIdx → VA map (reuse same logic as .rela.text).
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
			case "COMMON":
				if sym.binding == elf.STB_GLOBAL {
					va = symVA[sym.name]
				}
			case "":
				if sym.binding == elf.STB_GLOBAL && sym.name != "" {
					va = symVA[sym.name]
				}
			}
			va = weakInterposedVA(obj, sym, weakDef, symVA, va)
			fileSymVA[uint32(i)] = va
		}

		for _, rela := range obj.dataRelas {
			symVAval := fileSymVA[rela.symIdx]
			if symVAval == 0 && rela.symIdx < uint32(len(obj.syms)) {
				symVAval = symVA[obj.syms[rela.symIdx].name]
			}
			if symVAval == 0 && rela.symIdx < uint32(len(obj.syms)) {
				sym := obj.syms[rela.symIdx]
				if sym.name != "" {
					fmt.Fprintf(os.Stderr, "linker: warning: unresolved symbol '%s' (rela.data) in %s\n", sym.name, obj.path)
				}
			}

			if rela.rtype == rAArch64Abs64 {
				addr := symVAval + uint64(rela.addend)
				byteOff := obj.dataOff + rela.offset
				if byteOff+8 <= uint64(len(dataBytes)) {
					binary.LittleEndian.PutUint64(dataBytes[byteOff:], addr)
				}
			}
		}
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
