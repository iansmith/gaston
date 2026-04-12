// objgen.go — Linux ARM64 ELF relocatable object file (ET_REL) generator.
//
// Generates a .o file suitable for linking with the gaston linker.
//
// Fixed section layout (indices 0–9):
//   [0] NULL
//   [1] .text      — literal pool (zeros + RELA) + machine code (default functions)
//   [2] .rodata    — NUL-terminated string literals (default)
//   [3] .data      — initialized global scalars (default)
//   [4] .bss       — uninitialized globals (SHT_NOBITS, default)
//   [5] .symtab    — symbol table
//   [6] .strtab    — symbol name strings
//   [7] .rela.text — relocations for .text
//   [8] .rela.data — relocations for .data
//   [9] .shstrtab  — section name strings
//
// Dynamic custom sections (indices 10+):
//   Each unique custom exec section gets two indices: the section itself,
//   then its .rela.<name> companion.
//   Each unique custom data/rodata section gets one index.
//
// Symbol naming:
//   Functions   → "<name>" (STB_GLOBAL, STT_FUNC, in their section)
//   Global vars → "<name>" (STB_GLOBAL, STT_OBJECT, in their section)
//   Str lits    → ".str<n>" (STB_LOCAL,  STT_OBJECT, in .rodata)
//
// Relocations:
//   Pool entry k (byte offset k*8): R_AARCH64_ABS64 for the symbol
//   Extern BL at instruction i:     R_AARCH64_CALL26 for the callee symbol
package main

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// ARM64 relocation type constants (AAELF64 spec).
const (
	rAArch64Abs64  = 257 // R_AARCH64_ABS64: absolute 64-bit (S + A)
	rAArch64Call26 = 283 // R_AARCH64_CALL26: BL target (S + A - P) >> 2
)

// objShstrtab is the fixed section-name string table content and offsets.
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

// genObjectBytes compiles irp to an in-memory ET_REL ELF byte slice.
func genObjectBytes(irp *IRProgram) ([]byte, error) {
	var buf bytes.Buffer
	if err := genObjectTo(irp, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// genObjectFile compiles irp to a Linux ARM64 ET_REL object file at outpath.
func genObjectFile(irp *IRProgram, outpath string) error {
	f, err := os.OpenFile(outpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("genObjectFile: %w", err)
	}
	defer f.Close()
	return genObjectTo(irp, f)
}

// isExecSection reports whether a section name looks like a code section.
// Names starting with ".text" are treated as executable.
func isExecSection(name string) bool {
	if len(name) >= 5 && name[:5] == ".text" {
		return true
	}
	return false
}

// genObjectTo compiles irp to a Linux ARM64 ET_REL ELF, writing to w.
func genObjectTo(irp *IRProgram, w io.Writer) error {
	// ── build isGlobalPtr map ──────────────────────────────────────────────
	isGlobalPtr := make(map[string]bool)
	for _, gbl := range irp.Globals {
		if gbl.IsPtr {
			isGlobalPtr[gbl.Name] = true
		}
	}

	// ── partition globals ──────────────────────────────────────────────────
	// Only locally-defined (non-extern) globals get storage.
	// Globals with a custom SectionName go into their own section, not .data/.bss.
	type globalSlot struct {
		gbl        IRGlobal
		inData     bool   // initialized scalar → .data; else → .bss
		isCommon   bool   // true for non-static uninitialized globals (SHN_COMMON; linker merges)
		dataOff    uint64 // byte offset within .data
		bssOff     uint64 // byte offset within .bss
		customOff  uint64 // byte offset within the custom section data
	}
	var slots []globalSlot        // default-section globals
	var customSlots []globalSlot  // custom-section globals
	var dataTotal, bssTotal uint64
	// customSecData: section name → accumulated bytes for that custom section
	customSecData := make(map[string][]byte)
	customSecOffset := make(map[string]uint64) // track current offset per custom section

	for _, gbl := range irp.Globals {
		if gbl.IsExtern {
			continue
		}
		sl := globalSlot{gbl: gbl}
		if gbl.SectionName != "" {
			// Custom-section global — all go into their named section as data.
			secName := gbl.SectionName
			sl.customOff = customSecOffset[secName]
			var sz uint64
			if gbl.HasInitVal && !gbl.IsArr {
				// Initialized scalar — write 8 bytes LE.
				buf := make([]byte, 8)
				binary.LittleEndian.PutUint64(buf, uint64(gbl.InitVal))
				customSecData[secName] = append(customSecData[secName], buf...)
				sz = 8
			} else if len(gbl.InitData) > 0 {
				data := make([]byte, len(gbl.InitData))
				copy(data, gbl.InitData)
				// Pad to 8-byte boundary.
				if len(data)%8 != 0 {
					pad := 8 - len(data)%8
					data = append(data, make([]byte, pad)...)
				}
				customSecData[secName] = append(customSecData[secName], data...)
				sz = uint64(len(data))
			} else {
				// Zero-initialized — still needs space.
				sz = uint64(gbl.Size) * 8
				customSecData[secName] = append(customSecData[secName], make([]byte, sz)...)
			}
			customSecOffset[secName] = sl.customOff + sz
			sl.inData = true // custom section is always treated as data-like
			customSlots = append(customSlots, sl)
			continue
		}
		if gbl.HasInitVal && !gbl.IsArr {
			sl.inData = true
			if gbl.Align > 0 {
				dataTotal = (dataTotal + uint64(gbl.Align) - 1) &^ (uint64(gbl.Align) - 1)
			}
			sl.dataOff = dataTotal
			dataTotal += 8
		} else if len(gbl.InitData) > 0 {
			// Struct/array with init data → .data section.
			sz := uint64(len(gbl.InitData))
			// Align to 8 bytes (or requested alignment if larger).
			alignTo := uint64(8)
			if gbl.Align > 0 && uint64(gbl.Align) > alignTo {
				alignTo = uint64(gbl.Align)
			}
			if sz%alignTo != 0 {
				sz = (sz + alignTo - 1) &^ (alignTo - 1)
			}
			sl.inData = true
			if gbl.Align > 0 {
				dataTotal = (dataTotal + uint64(gbl.Align) - 1) &^ (uint64(gbl.Align) - 1)
			}
			sl.dataOff = dataTotal
			dataTotal += sz
		} else if !gbl.IsStatic && !gbl.IsWeak {
			// Non-static uninitialized global → SHN_COMMON (tentative definition).
			// The linker merges all COMMON symbols with the same name across TUs,
			// which is the correct C semantic for file-scope declarations without
			// initializers (e.g. `int x;` in a header included by multiple .c files).
			sl.isCommon = true
		} else {
			// Static or weak uninitialized global → .bss (strong/local definition).
			if gbl.Align > 0 {
				bssTotal = (bssTotal + uint64(gbl.Align) - 1) &^ (uint64(gbl.Align) - 1)
			}
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
		addend int64  // constant addend (e.g. for &arr[i])
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
						addend: rel.Addend,
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

	// ── set of locally-defined function names (per section) ──────────────────
	// localFuncsBySection[secName] = set of function names defined in that section.
	// "" means the default .text section.
	// Cross-section calls must use emitBLextern so the linker resolves them.
	localFuncsBySection := make(map[string]map[string]bool)
	localFuncsBySection[""] = make(map[string]bool)
	for _, fn := range irp.Funcs {
		sec := fn.SectionName
		if _, ok := localFuncsBySection[sec]; !ok {
			localFuncsBySection[sec] = make(map[string]bool)
		}
		localFuncsBySection[sec][fn.Name] = true
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

	// Collect unique custom section names (preserving order of first appearance).
	customSecOrder := []string{}
	customSecSeen := make(map[string]bool)

	// Partition functions: default vs custom-section.
	var defaultFuncs []*IRFunc
	// customFuncsBySection maps section name → list of IRFunc
	customFuncsBySection := make(map[string][]*IRFunc)
	for _, fn := range irp.Funcs {
		if fn.SectionName != "" {
			if !customSecSeen[fn.SectionName] {
				customSecSeen[fn.SectionName] = true
				customSecOrder = append(customSecOrder, fn.SectionName)
			}
			customFuncsBySection[fn.SectionName] = append(customFuncsBySection[fn.SectionName], fn)
		} else {
			defaultFuncs = append(defaultFuncs, fn)
		}
	}
	// Also collect custom section names from custom-section globals.
	for _, sl := range customSlots {
		name := sl.gbl.SectionName
		if !customSecSeen[name] {
			customSecSeen[name] = true
			customSecOrder = append(customSecOrder, name)
		}
	}

	// Sort custom section names for deterministic output within exec/data groups.
	// Actually preserve insertion order (first appearance) for predictability.
	// customSecOrder already has insertion-order.

	// Build the default codeBuilder and generate default functions.
	cb := newCodeBuilder(allGlobalsForPool, irp.StrLits, irp.FConsts, irp.FuncRefs)
	gen := &elfGen{
		cb:            cb,
		pendingParams: make([]paramArg, 0, 8),
		isGlobalPtr:   isGlobalPtr,
		isObjMode:     true,
		localFuncs:    localFuncsBySection[""],
		funcRetType:   funcRetType,
		structDefs:    irp.StructDefs,
	}

	for _, fn := range defaultFuncs {
		gen.genFunc(fn)
	}

	if err := cb.applyFixups(); err != nil {
		return fmt.Errorf("genObjectFile: %w", err)
	}

	textBytes := make([]byte, len(cb.instrs)*4)
	for i, w32 := range cb.instrs {
		binary.LittleEndian.PutUint32(textBytes[i*4:], w32)
	}

	// Generate custom-section functions, one codeBuilder per section.
	// customExecSections: section name → generated bytes + externBLs + labels
	type customExecSection struct {
		name      string
		secIdx    uint16   // assigned ELF section index
		relaIdx   uint16   // assigned ELF rela section index
		textBytes []byte
		cb        *codeBuilder
	}
	var customExecList []*customExecSection

	nextSecIdx := uint16(objNumSections)

	// Separate exec sections from data sections among custom sections.
	// Exec = starts with ".text"; everything else treated as data.
	customExecByName := make(map[string]*customExecSection)

	// Determine which custom sections are exec vs data.
	// We process exec sections first (for section index assignment).
	// Collect exec custom section names (those that have functions).
	execCustomNames := []string{}
	for _, name := range customSecOrder {
		if isExecSection(name) && len(customFuncsBySection[name]) > 0 {
			execCustomNames = append(execCustomNames, name)
		}
	}
	// Also include exec names that appear in customFuncsBySection but may not be in customSecOrder.
	// (They should be, since we added them above.)

	for _, secName := range execCustomNames {
		ces := &customExecSection{
			name:    secName,
			secIdx:  nextSecIdx,
			relaIdx: nextSecIdx + 1,
		}
		nextSecIdx += 2
		customExecByName[secName] = ces
		customExecList = append(customExecList, ces)

		// Generate code for all functions in this section.
		secCB := newCodeBuilder(allGlobalsForPool, irp.StrLits, irp.FConsts, irp.FuncRefs)
		// Swap codeBuilder and localFuncs in gen so cross-section calls use extern BL.
		gen.cb = secCB
		gen.localFuncs = localFuncsBySection[secName]
		gen.pendingParams = gen.pendingParams[:0]
		for _, fn := range customFuncsBySection[secName] {
			gen.genFunc(fn)
		}
		if err := secCB.applyFixups(); err != nil {
			return fmt.Errorf("genObjectFile custom section %q: %w", secName, err)
		}
		ces.cb = secCB
		bs := make([]byte, len(secCB.instrs)*4)
		for i, w32 := range secCB.instrs {
			binary.LittleEndian.PutUint32(bs[i*4:], w32)
		}
		ces.textBytes = bs
	}
	// Restore gen.cb and localFuncs to main defaults (for any future use).
	gen.cb = cb
	gen.localFuncs = localFuncsBySection[""]

	// Assign section indices for custom data sections.
	type customDataSection struct {
		name   string
		secIdx uint16
		data   []byte
	}
	var customDataList []*customDataSection
	customDataByName := make(map[string]*customDataSection)

	dataCustomNames := []string{}
	for _, name := range customSecOrder {
		if !isExecSection(name) || len(customFuncsBySection[name]) == 0 {
			// Data-like custom section.
			if _, hasExec := customExecByName[name]; !hasExec {
				// Hasn't been assigned yet.
				dataCustomNames = append(dataCustomNames, name)
			}
		}
	}
	// Also handle exec-named sections that only have globals (no functions).
	// Actually, a ".text.foo" section with no functions but with globals is odd,
	// but we handle it as a data section.

	// Build the set of all custom data section names from customSlots.
	customDataNames := make(map[string]bool)
	for _, sl := range customSlots {
		if _, isExec := customExecByName[sl.gbl.SectionName]; !isExec {
			customDataNames[sl.gbl.SectionName] = true
		}
	}
	// Create entries in customSecOrder order.
	for _, name := range customSecOrder {
		if customDataNames[name] {
			if _, already := customDataByName[name]; !already {
				cds := &customDataSection{
					name:   name,
					secIdx: nextSecIdx,
					data:   customSecData[name],
				}
				nextSecIdx++
				customDataByName[name] = cds
				customDataList = append(customDataList, cds)
			}
		}
	}
	_ = dataCustomNames // used implicitly via customDataNames

	totalSections := int(nextSecIdx)

	// ── build dynamic shstrtab ─────────────────────────────────────────────
	// Start with the fixed bytes, then append custom section names.
	dynShstrtab := make([]byte, len(objShstrtab))
	copy(dynShstrtab, objShstrtab)

	// customShstrOff[name] = offset of the section name in dynShstrtab.
	customShstrOff := make(map[string]uint32)

	// For exec sections: append ".text.foo\0" and ".rela.text.foo\0".
	for _, ces := range customExecList {
		// The section name itself.
		off := uint32(len(dynShstrtab))
		customShstrOff[ces.name] = off
		dynShstrtab = append(dynShstrtab, []byte(ces.name)...)
		dynShstrtab = append(dynShstrtab, 0)

		// The .rela.<name> string.
		relaName := ".rela" + ces.name
		relaOff := uint32(len(dynShstrtab))
		customShstrOff[relaName] = relaOff
		dynShstrtab = append(dynShstrtab, []byte(relaName)...)
		dynShstrtab = append(dynShstrtab, 0)
	}
	// For data sections: append just the name.
	for _, cds := range customDataList {
		if _, already := customShstrOff[cds.name]; !already {
			off := uint32(len(dynShstrtab))
			customShstrOff[cds.name] = off
			dynShstrtab = append(dynShstrtab, []byte(cds.name)...)
			dynShstrtab = append(dynShstrtab, 0)
		}
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

	// Section symbols for custom sections.
	for _, ces := range customExecList {
		syms = append(syms, sectionSym(ces.secIdx))
	}
	for _, cds := range customDataList {
		syms = append(syms, sectionSym(cds.secIdx))
	}

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

	// ELF requires STB_LOCAL symbols to appear before STB_GLOBAL/STB_WEAK symbols.
	// Pass 1: emit static (STB_LOCAL) function symbols.
	funcSymIdx := make(map[string]int) // C-minus name → symtab index
	for _, fn := range defaultFuncs {
		if !fn.IsStatic {
			continue
		}
		label := funcLabel(fn.Name)
		wordIdx, ok := cb.labels[label]
		if !ok {
			return fmt.Errorf("genObjectFile: function label %q not found", label)
		}
		idx := len(syms)
		funcSymIdx[fn.Name] = idx
		nameIdx := strtab.add(label)
		syms = append(syms, symRec{
			nameIdx: nameIdx,
			info:    (0 << 4) | 2, // STB_LOCAL | STT_FUNC
			shndx:   objSecText,
			value:   uint64(wordIdx) * 4,
		})
	}

	// Pass 1b: static variables as STB_LOCAL.
	globalSymIdx := make(map[string]int) // C-minus name → symtab index
	for _, sl := range slots {
		if !sl.gbl.IsStatic {
			continue
		}
		idx := len(syms)
		globalSymIdx[sl.gbl.Name] = idx
		nameIdx := strtab.add(sl.gbl.Name)
		sz := uint64(sl.gbl.Size) * 8
		if sl.inData {
			syms = append(syms, symRec{
				nameIdx: nameIdx,
				info:    (0 << 4) | 1, // STB_LOCAL | STT_OBJECT
				shndx:   objSecData,
				value:   sl.dataOff,
				size:    sz,
			})
		} else {
			syms = append(syms, symRec{
				nameIdx: nameIdx,
				info:    (0 << 4) | 1, // STB_LOCAL | STT_OBJECT
				shndx:   objSecBss,
				value:   sl.bssOff,
				size:    sz,
			})
		}
	}

	// First global symbol index (all STB_LOCAL symbols emitted above this point).
	firstGlobal := len(syms)

	// Pass 2: global/weak function symbols (STB_GLOBAL, STT_FUNC).
	for _, fn := range defaultFuncs {
		if fn.IsStatic {
			continue
		}
		label := funcLabel(fn.Name)
		wordIdx, ok := cb.labels[label]
		if !ok {
			return fmt.Errorf("genObjectFile: function label %q not found", label)
		}
		idx := len(syms)
		funcSymIdx[fn.Name] = idx
		nameIdx := strtab.add(label)
		binding := uint8(1) // STB_GLOBAL
		if fn.IsWeak {
			binding = 2 // STB_WEAK
		}
		syms = append(syms, symRec{
			nameIdx: nameIdx,
			info:    (binding << 4) | 2, // STB_{GLOBAL,WEAK} | STT_FUNC
			shndx:   objSecText,
			value:   uint64(wordIdx) * 4,
		})
	}

	// Global symbols for custom-section functions.
	for _, ces := range customExecList {
		for _, fn := range customFuncsBySection[ces.name] {
			label := funcLabel(fn.Name)
			wordIdx, ok := ces.cb.labels[label]
			if !ok {
				return fmt.Errorf("genObjectFile: custom function label %q not found", label)
			}
			idx := len(syms)
			funcSymIdx[fn.Name] = idx
			nameIdx := strtab.add(label)
			binding := uint8(1) // STB_GLOBAL
			if fn.IsWeak {
				binding = 2 // STB_WEAK
			}
			syms = append(syms, symRec{
				nameIdx: nameIdx,
				info:    (binding << 4) | 2, // STB_{GLOBAL,WEAK} | STT_FUNC
				shndx:   ces.secIdx,
				value:   uint64(wordIdx) * 4,
			})
		}
	}

	// Global symbols for locally-defined default variables (non-static only; static emitted above).
	for _, sl := range slots {
		if sl.gbl.IsStatic {
			continue // already emitted as STB_LOCAL before firstGlobal
		}
		idx := len(syms)
		globalSymIdx[sl.gbl.Name] = idx
		nameIdx := strtab.add(sl.gbl.Name)
		sz := uint64(sl.gbl.Size) * 8
		gblBinding := uint8(1) // STB_GLOBAL
		if sl.gbl.IsWeak {
			gblBinding = 2 // STB_WEAK
		}
		if sl.isCommon {
			// Tentative definition → SHN_COMMON.  ELF spec: value = alignment,
			// size = byte size.  The linker merges same-named COMMON symbols.
			alignment := uint64(8)
			if sl.gbl.Align > 0 {
				alignment = uint64(sl.gbl.Align)
			}
			syms = append(syms, symRec{
				nameIdx: nameIdx,
				info:    (gblBinding << 4) | 1, // STB_GLOBAL | STT_OBJECT
				shndx:   uint16(elf.SHN_COMMON),
				value:   alignment,
				size:    sz,
			})
		} else if sl.inData {
			syms = append(syms, symRec{
				nameIdx: nameIdx,
				info:    (gblBinding << 4) | 1, // STB_{GLOBAL,WEAK} | STT_OBJECT
				shndx:   objSecData,
				value:   sl.dataOff,
				size:    sz,
			})
		} else {
			syms = append(syms, symRec{
				nameIdx: nameIdx,
				info:    (gblBinding << 4) | 1, // STB_{GLOBAL,WEAK} | STT_OBJECT
				shndx:   objSecBss,
				value:   sl.bssOff,
				size:    sz,
			})
		}
	}

	// Global symbols for custom-section variables.
	for _, sl := range customSlots {
		secName := sl.gbl.SectionName
		var shndx uint16
		if ces, ok := customExecByName[secName]; ok {
			shndx = ces.secIdx
		} else if cds, ok := customDataByName[secName]; ok {
			shndx = cds.secIdx
		} else {
			return fmt.Errorf("genObjectFile: custom section %q not found for global %q", secName, sl.gbl.Name)
		}
		idx := len(syms)
		globalSymIdx[sl.gbl.Name] = idx
		nameIdx := strtab.add(sl.gbl.Name)
		sz := uint64(sl.gbl.Size) * 8
		gblBinding := uint8(1)
		if sl.gbl.IsWeak {
			gblBinding = 2
		} else if sl.gbl.IsStatic {
			gblBinding = 0 // STB_LOCAL
		}
		syms = append(syms, symRec{
			nameIdx: nameIdx,
			info:    (gblBinding << 4) | 1, // STB_{GLOBAL,WEAK} | STT_OBJECT
			shndx:   shndx,
			value:   sl.customOff,
			size:    sz,
		})
	}

	// Build alias name set for fast lookup — aliases must NOT appear as SHN_UNDEF.
	aliasTargets := make(map[string]string) // alias name → target name
	for _, a := range irp.Aliases {
		aliasTargets[a.Name] = a.Target
	}

	// Collect all extern symbol names referenced by pool entries or extern BLs.
	externSymNames := make(map[string]int) // sym name → symtab index

	// Pool entries for extern globals.
	for _, gbl := range irp.Globals {
		if gbl.IsExtern {
			if _, isAlias := aliasTargets[gbl.Name]; isAlias {
				continue // will be emitted as a defined alias symbol below
			}
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

	// Extern BL targets — from default cb and all custom cbs.
	// Skip symbols that are already defined locally (in any section of this TU),
	// since cross-section local calls still resolve within the same object.
	collectExternBLs := func(xbls []externBLRecord) {
		for _, xbl := range xbls {
			if _, isAlias := aliasTargets[xbl.sym]; isAlias {
				continue // will be emitted as a defined alias symbol below
			}
			// Skip if already defined as a local function (any section).
			if _, ok := funcSymIdx[xbl.sym]; ok {
				continue
			}
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
	}
	collectExternBLs(cb.externBLs)
	for _, ces := range customExecList {
		collectExternBLs(ces.cb.externBLs)
	}

	// Ensure data reloc symbols are in the symbol table.
	for _, dr := range dataRelocs {
		if _, ok := strSymIdx[dr.symbol]; ok {
			continue
		}
		if _, ok := globalSymIdx[dr.symbol]; ok {
			continue
		}
		if _, ok := funcSymIdx[dr.symbol]; ok {
			continue
		}
		if _, ok := externSymNames[dr.symbol]; ok {
			continue
		}
		// Add as extern.
		idx := len(syms)
		externSymNames[dr.symbol] = idx
		nameIdx := strtab.add(dr.symbol)
		syms = append(syms, symRec{
			nameIdx: nameIdx,
			info:    (1 << 4) | 0, // STB_GLOBAL | STT_NOTYPE
			shndx:   uint16(elf.SHN_UNDEF),
		})
	}

	// Build a map from alias name → alias target for chain resolution.
	aliasTargetMap := make(map[string]string, len(irp.Aliases))
	for _, a := range irp.Aliases {
		aliasTargetMap[a.Name] = a.Target
	}
	resolveAliasTarget := func(target string) string {
		seen := make(map[string]bool)
		for {
			next, ok := aliasTargetMap[target]
			if !ok || seen[target] {
				break
			}
			seen[target] = true
			target = next
		}
		return target
	}

	// Emit alias symbols: defined symbols with the same value+section as their target.
	aliasSymIdx := make(map[string]int) // alias name → symtab index
	for _, a := range irp.Aliases {
		idx := len(syms)
		aliasSymIdx[a.Name] = idx
		nameIdx := strtab.add(a.Name)
		if a.IsFunc {
			// Function alias: look up the target's code offset.
			// Follow alias chains (e.g. gammal → gamma → lgamma).
			targetLabel := funcLabel(resolveAliasTarget(a.Target))
			// Check default cb first, then custom cbs.
			var wordIdx int
			var shndx uint16
			var found bool
			if wi, ok := cb.labels[targetLabel]; ok {
				wordIdx = wi
				shndx = objSecText
				found = true
			}
			if !found {
				for _, ces := range customExecList {
					if wi, ok := ces.cb.labels[targetLabel]; ok {
						wordIdx = wi
						shndx = ces.secIdx
						found = true
						break
					}
				}
			}
			if !found {
				return fmt.Errorf("alias %q: target function %q not found in object", a.Name, a.Target)
			}
			syms = append(syms, symRec{
				nameIdx: nameIdx,
				info:    (1 << 4) | 2, // STB_GLOBAL | STT_FUNC
				shndx:   shndx,
				value:   uint64(wordIdx) * 4,
			})
		} else {
			// Variable alias: copy the target symbol's section and value.
			targetIdx, ok := globalSymIdx[a.Target]
			if ok {
				target := syms[targetIdx]
				syms = append(syms, symRec{
					nameIdx: nameIdx,
					info:    (1 << 4) | 1, // STB_GLOBAL | STT_OBJECT
					shndx:   target.shndx,
					value:   target.value,
					size:    target.size,
				})
			} else {
				// Fallback: the alias may point to a function whose type was not
				// resolved (e.g. __typeof(fn) aliases where TypeTypeof was not
				// resolved to TypeFuncPtr). Try the function code-label lookup.
				// Also follow alias chains.
				targetLabel := funcLabel(resolveAliasTarget(a.Target))
				var wordIdx int
				var shndx uint16
				var found bool
				if wi, ok2 := cb.labels[targetLabel]; ok2 {
					wordIdx = wi
					shndx = objSecText
					found = true
				}
				if !found {
					for _, ces := range customExecList {
						if wi, ok2 := ces.cb.labels[targetLabel]; ok2 {
							wordIdx = wi
							shndx = ces.secIdx
							found = true
							break
						}
					}
				}
				if !found {
					return fmt.Errorf("alias %q: target %q not found in object (tried variable and function)", a.Name, a.Target)
				}
				syms = append(syms, symRec{
					nameIdx: nameIdx,
					info:    (1 << 4) | 2, // STB_GLOBAL | STT_FUNC
					shndx:   shndx,
					value:   uint64(wordIdx) * 4,
				})
			}
		}
	}

	// ── helper: build rela records for a codeBuilder ──────────────────────
	type relaRec struct {
		off    uint64
		info   uint64
		addend int64
	}

	mkRela := func(off uint64, symIdx int, typ uint32, addend int64) relaRec {
		return relaRec{
			off:    off,
			info:   uint64(symIdx)<<32 | uint64(typ),
			addend: addend,
		}
	}

	buildCodeRelas := func(srcCB *codeBuilder) []relaRec {
		var relas []relaRec

		// ABS64 relocations for each pool entry.
		poolCount := len(allGlobalsForPool) + len(irp.StrLits)
		for k := 0; k < poolCount; k++ {
			byteOff := uint64(k * 8)
			var symIdx int

			if k < len(allGlobalsForPool) {
				name := allGlobalsForPool[k].Name
				if i, ok := globalSymIdx[name]; ok {
					symIdx = i
				} else if i, ok := externSymNames[name]; ok {
					symIdx = i
				} else if i, ok := aliasSymIdx[name]; ok {
					symIdx = i
				} else {
					// Return error inline — we'll check after.
					symIdx = -1
				}
			} else {
				litIdx := k - len(allGlobalsForPool)
				symIdx = strSymIdx[rodataList[litIdx].label]
			}

			if symIdx >= 0 {
				relas = append(relas, mkRela(byteOff, symIdx, rAArch64Abs64, 0))
			}
		}

		// ABS64 relocations for function address references (IRFuncAddr).
		for _, fnName := range irp.FuncRefs {
			label := funcLabel(fnName)
			k, ok := srcCB.poolIdx[label]
			if !ok {
				continue
			}
			byteOff := uint64(k * 8)
			var symIdx int
			if i, ok2 := funcSymIdx[fnName]; ok2 {
				symIdx = i
			} else if i, ok2 := externSymNames[label]; ok2 {
				symIdx = i
			} else {
				// Add as extern UNDEF.
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
		// The target may be a locally-defined function in another section (funcSymIdx)
		// or a truly external symbol (externSymNames).
		for _, xbl := range srcCB.externBLs {
			byteOff := uint64(xbl.at) * 4
			var symIdx int
			if i, ok := funcSymIdx[xbl.sym]; ok {
				symIdx = i
			} else {
				symIdx = externSymNames[xbl.sym]
			}
			relas = append(relas, mkRela(byteOff, symIdx, rAArch64Call26, 0))
		}

		return relas
	}

	// ── build .rela.text ──────────────────────────────────────────────────
	relas := buildCodeRelas(cb)

	// Check for missing pool global symbols (report first error).
	poolCount := len(allGlobalsForPool) + len(irp.StrLits)
	for k := 0; k < poolCount; k++ {
		if k < len(allGlobalsForPool) {
			name := allGlobalsForPool[k].Name
			_, inGlobal := globalSymIdx[name]
			_, inExtern := externSymNames[name]
			_, inAlias := aliasSymIdx[name]
			if !inGlobal && !inExtern && !inAlias {
				return fmt.Errorf("genObjectFile: no symbol for pool global %q", name)
			}
		}
	}

	// Serialise .rela.text.
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
		symIdx, ok := strSymIdx[dr.symbol]
		if !ok {
			symIdx, ok = globalSymIdx[dr.symbol]
		}
		if !ok {
			symIdx, ok = funcSymIdx[dr.symbol]
		}
		if !ok {
			symIdx, ok = externSymNames[dr.symbol]
		}
		if !ok {
			return fmt.Errorf("genObjectFile: no symbol for data reloc %q", dr.symbol)
		}
		relaDataRecs = append(relaDataRecs, mkRela(dr.offset, symIdx, rAArch64Abs64, dr.addend))
	}
	relaDataBytes := make([]byte, len(relaDataRecs)*24)
	for i, r := range relaDataRecs {
		off := i * 24
		binary.LittleEndian.PutUint64(relaDataBytes[off:], r.off)
		binary.LittleEndian.PutUint64(relaDataBytes[off+8:], r.info)
		binary.LittleEndian.PutUint64(relaDataBytes[off+16:], uint64(r.addend))
	}

	// ── build rela for each custom exec section ────────────────────────────
	type customRelaBytes struct {
		ces   *customExecSection
		bytes []byte
	}
	var customRelaList []customRelaBytes
	for _, ces := range customExecList {
		secRelas := buildCodeRelas(ces.cb)
		bs := make([]byte, len(secRelas)*24)
		for i, r := range secRelas {
			off := i * 24
			binary.LittleEndian.PutUint64(bs[off:], r.off)
			binary.LittleEndian.PutUint64(bs[off+8:], r.info)
			binary.LittleEndian.PutUint64(bs[off+16:], uint64(r.addend))
		}
		customRelaList = append(customRelaList, customRelaBytes{ces, bs})
	}

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
	//   custom exec section data (in order)
	//   custom rela data (in order, paired with exec sections)
	//   custom data section data (in order)
	//   .shstrtab data
	//   Section header table (totalSections × 64 bytes)

	const elfHdr = 64
	const shdrSize = 64
	shdrTableSize := totalSections * shdrSize

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

	// Custom exec sections and their relas.
	type customSecFileOff struct {
		textOff uint64
		relaOff uint64
	}
	customExecOffsets := make([]customSecFileOff, len(customExecList))
	for i, ces := range customExecList {
		customExecOffsets[i].textOff = off
		off += uint64(len(ces.textBytes))
		customExecOffsets[i].relaOff = off
		off += uint64(len(customRelaList[i].bytes))
	}

	// Custom data sections.
	customDataOffsets := make([]uint64, len(customDataList))
	for i, cds := range customDataList {
		customDataOffsets[i] = off
		off += uint64(len(cds.data))
	}

	shstrtabOff := off
	off += uint64(len(dynShstrtab))

	// Align section header table to 8 bytes.
	for off%8 != 0 {
		off++
	}
	shdrOff := off

	// ── write ELF ────────────────────────────────────────────────────────
	var werr error
	write := func(data interface{}) {
		if werr != nil {
			return
		}
		werr = binary.Write(w, binary.LittleEndian, data)
	}
	writeBytes := func(b []byte) {
		if werr != nil {
			return
		}
		_, werr = w.Write(b)
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
		Shnum:     uint16(totalSections),
		Shstrndx:  uint16(objSecShstrtab),
	}
	write(ehdr)

	// Section data (fixed sections).
	writeBytes(textBytes)
	writeBytes(rodataBytes)
	writeBytes(dataBytes)
	writeBytes(symtabBytes)
	writeBytes(strtab.data)
	writeBytes(relaBytes)
	writeBytes(relaDataBytes)

	// Custom exec sections and their rela sections.
	for i, ces := range customExecList {
		writeBytes(ces.textBytes)
		writeBytes(customRelaList[i].bytes)
	}

	// Custom data sections.
	for _, cds := range customDataList {
		writeBytes(cds.data)
	}

	// shstrtab.
	writeBytes(dynShstrtab)

	// Padding before section header table.
	curOff := shstrtabOff + uint64(len(dynShstrtab))
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

	// Write fixed section headers.
	write(mkShdr(0, uint32(elf.SHT_NULL), 0, 0, 0, 0, 0, 0, 0, 0))
	write(mkShdr(shstrText, uint32(elf.SHT_PROGBITS),
		uint64(elf.SHF_ALLOC|elf.SHF_EXECINSTR),
		0, textOff, uint64(len(textBytes)), 0, 0, 4, 0))
	write(mkShdr(shstrRodata, uint32(elf.SHT_PROGBITS),
		uint64(elf.SHF_ALLOC),
		0, rodataOff, rodataTotal, 0, 0, 1, 0))
	write(mkShdr(shstrData, uint32(elf.SHT_PROGBITS),
		uint64(elf.SHF_ALLOC|elf.SHF_WRITE),
		0, dataOff, dataTotal, 0, 0, 8, 0))
	write(mkShdr(shstrBss, uint32(elf.SHT_NOBITS),
		uint64(elf.SHF_ALLOC|elf.SHF_WRITE),
		0, dataOff+dataTotal, bssTotal, 0, 0, 8, 0))
	write(mkShdr(shstrSymtab, uint32(elf.SHT_SYMTAB),
		0, 0, symtabOff, uint64(len(symtabBytes)),
		uint32(objSecStrtab), uint32(firstGlobal), 8, 24))
	write(mkShdr(shstrStrtab, uint32(elf.SHT_STRTAB),
		0, 0, strtabOff, uint64(len(strtab.data)), 0, 0, 1, 0))
	write(mkShdr(shstrRelaText, uint32(elf.SHT_RELA),
		0, 0, relaOff, uint64(len(relaBytes)),
		uint32(objSecSymtab), uint32(objSecText), 8, 24))
	write(mkShdr(shstrRelaData, uint32(elf.SHT_RELA),
		0, 0, relaDataOff, uint64(len(relaDataBytes)),
		uint32(objSecSymtab), uint32(objSecData), 8, 24))
	write(mkShdr(shstrShstrtab, uint32(elf.SHT_STRTAB),
		0, 0, shstrtabOff, uint64(len(dynShstrtab)), 0, 0, 1, 0))

	// Custom exec section headers and their rela headers.
	for i, ces := range customExecList {
		secNameOff := customShstrOff[ces.name]
		relaNameStr := ".rela" + ces.name
		relaNameOff := customShstrOff[relaNameStr]

		write(mkShdr(secNameOff, uint32(elf.SHT_PROGBITS),
			uint64(elf.SHF_ALLOC|elf.SHF_EXECINSTR),
			0, customExecOffsets[i].textOff, uint64(len(ces.textBytes)), 0, 0, 4, 0))
		write(mkShdr(relaNameOff, uint32(elf.SHT_RELA),
			0, 0, customExecOffsets[i].relaOff, uint64(len(customRelaList[i].bytes)),
			uint32(objSecSymtab), uint32(ces.secIdx), 8, 24))
	}

	// Custom data section headers.
	for i, cds := range customDataList {
		secNameOff := customShstrOff[cds.name]
		// Determine flags: if the section name starts with ".rodata", it's read-only alloc.
		var flags uint64
		if len(cds.name) >= 7 && cds.name[:7] == ".rodata" {
			flags = uint64(elf.SHF_ALLOC)
		} else {
			flags = uint64(elf.SHF_ALLOC | elf.SHF_WRITE)
		}
		write(mkShdr(secNameOff, uint32(elf.SHT_PROGBITS),
			flags, 0, customDataOffsets[i], uint64(len(cds.data)), 0, 0, 8, 0))
	}

	if werr != nil {
		return fmt.Errorf("genObjectTo: write: %w", werr)
	}
	_ = shdrTableSize
	return nil
}
