package main

import (
	"fmt"
	"math"
	"strings"
)

// loopLabels holds the break/continue targets for one loop level.
type loopLabels struct {
	breakLabel    string
	continueLabel string
}

// irGen holds state for IR generation from a type-checked AST.
type irGen struct {
	prog            *IRProgram
	fn              *IRFunc   // current function being generated
	globals         map[string]*IRGlobal
	locals          map[string]localInfo
	funcNames       map[string]bool   // names of user-defined functions (for IRFuncAddr detection)
	variadicFuncs   map[string]bool   // names of variadic functions (for FP bitcast at call sites)
	structRetFuncs  map[string]string // function name → struct tag (for struct-returning functions)
	tempN           int
	labelN          int
	loopStack       []loopLabels
	currentFuncName string
	staticLocals    map[string]string // local name → mangled global name
	stmtExprN       int              // counter for unique statement expression scoping
}

// localInfo tracks what we know about a local name during IR gen.
type localInfo struct {
	isArray  bool
	isParam  bool   // if true, the stack slot holds a pointer, not inline storage
	arrSize  int    // >0 for local (non-param) arrays; -1 for array params
	innerDim int    // inner dimension for 2D arrays (0 for 1D)
	isStatic bool   // true for static locals (allocated as globals)
}

func newIRGen() *irGen {
	return &irGen{
		prog:           &IRProgram{StructDefs: make(map[string]*StructDef)},
		globals:        make(map[string]*IRGlobal),
		funcNames:      make(map[string]bool),
		variadicFuncs:  make(map[string]bool),
		structRetFuncs: make(map[string]string),
	}
}

func (g *irGen) newTemp() IRAddr {
	// Use "#t" prefix: '#' is not a valid C identifier character, so these
	// names can never collide with user-declared local variables (e.g. "t1").
	t := IRAddr{Kind: AddrTemp, Name: fmt.Sprintf("#t%d", g.tempN)}
	g.tempN++
	return t
}

func (g *irGen) newLabel() string {
	l := fmt.Sprintf("L%d", g.labelN)
	g.labelN++
	return l
}

func (g *irGen) emit(q Quad)      { g.fn.Quads = append(g.fn.Quads, q) }
func (g *irGen) emitLabel(l string) { g.emit(Quad{Op: IRLabel, Extra: l}) }
func (g *irGen) emitJump(l string)  { g.emit(Quad{Op: IRJump, Extra: l}) }

func (g *irGen) addrOf(name string) IRAddr {
	// Static locals are mapped to a mangled global name.
	if g.staticLocals != nil {
		if mangled, ok := g.staticLocals[name]; ok {
			return IRAddr{Kind: AddrGlobal, Name: mangled}
		}
	}
	if li, ok := g.locals[name]; ok {
		if li.isStatic {
			// Shouldn't reach here if staticLocals is set, but handle defensively.
			return IRAddr{Kind: AddrGlobal, Name: name}
		}
		return IRAddr{Kind: AddrLocal, Name: name}
	}
	return IRAddr{Kind: AddrGlobal, Name: name}
}

func (g *irGen) isArrayName(name string) bool {
	if li, ok := g.locals[name]; ok {
		return li.isArray
	}
	if gbl, ok := g.globals[name]; ok {
		return gbl.IsArr
	}
	return false
}

// genIR traverses a type-checked AST and returns the IR program.
func genIR(prog *Node) *IRProgram {
	g := newIRGen()

	// First pass: collect struct definitions.
	for _, decl := range prog.Children {
		if decl.Kind == KindStructDef {
			sd := buildStructDefIR(decl, g.prog.StructDefs)
			g.prog.StructDefs[sd.Name] = sd
		}
	}

	// Second pass: register global variable declarations.
	for _, decl := range prog.Children {
		if decl.Kind == KindVarDecl && !decl.IsConst {
			isArr := decl.Type == TypeIntArray
			isPtr := isPtrType(decl.Type)
			isStruct := decl.Type == TypeStruct
			sz := 1
			if isArr {
				sz = decl.Val
				if decl.Dim2 > 0 {
					sz = decl.Val * decl.Dim2
				}
			} else if isStruct {
				if sd, ok := g.prog.StructDefs[decl.StructTag]; ok {
					sz = (sd.SizeBytes(g.prog.StructDefs) + 7) / 8
				}
			}
			gbl := &IRGlobal{
				Name:      decl.Name,
				IsArr:     isArr,
				IsPtr:     isPtr,
				IsStruct:  isStruct,
				Pointee:   decl.Pointee,
				StructTag: decl.StructTag,
				IsExtern:  decl.IsExtern,
				Size:      sz,
				InnerDim:  decl.Dim2,
			}
			if !decl.IsExtern && len(decl.Children) > 0 {
				init := decl.Children[0]
				if init.Kind == KindNum {
					gbl.HasInitVal = true
					gbl.InitVal = init.Val
				} else if init.Kind == KindStrLit {
					// char array initialized from string literal.
					// Copy bytes into InitData; NUL-terminate only if there is space.
					content := []byte(init.Name)
					byteSize := sz * 8
					buf := make([]byte, byteSize)
					copy(buf, content)
					// NUL at len(content) is already zero (make zeroes)
					gbl.InitData = buf
				} else if init.Kind == KindInitList {
					// Build a byte-buffer from constant-valued init entries.
					byteSize := sz * 8
					if decl.StructTag != "" {
						if sd, ok := g.prog.StructDefs[decl.StructTag]; ok {
							elemSz := sd.SizeBytes(g.prog.StructDefs)
							if isArr {
								byteSize = decl.Val * elemSz
							} else {
								byteSize = elemSz
							}
						}
					}
					buf := make([]byte, byteSize)
					var relocs []InitReloc
					buildInitDataBuf(buf, init, decl, g.prog.StructDefs,
						0, g.prog, &relocs)
					gbl.InitData = buf
					gbl.InitRelocs = relocs
				}
			}
			g.prog.Globals = append(g.prog.Globals, *gbl)
			g.globals[decl.Name] = gbl
		}
	}

	// Third pass: collect function names, then generate IR for each function.
	// Also record which functions are variadic so call sites can bitcast FP args.
	knownVariadicLibFuncs := map[string]bool{
		"printf": true, "fprintf": true, "sprintf": true, "snprintf": true,
		"scanf": true, "fscanf": true, "sscanf": true,
	}
	for name := range knownVariadicLibFuncs {
		g.variadicFuncs[name] = true
	}
	for _, decl := range prog.Children {
		if decl.Kind == KindFunDecl {
			g.funcNames[decl.Name] = true
			for _, p := range decl.Children {
				if p.Kind == KindParam && p.Name == "..." {
					g.variadicFuncs[decl.Name] = true
					break
				}
			}
			// Track struct-returning functions (including extern) for call-site codegen.
			if decl.Type == TypeStruct {
				g.structRetFuncs[decl.Name] = decl.StructTag
			}
		}
	}
	for _, decl := range prog.Children {
		if decl.Kind == KindFunDecl && !decl.IsExtern {
			g.genFunc(decl)
		}
	}
	return g.prog
}

// buildStructDefIR converts a KindStructDef node to a StructDef using the same
// natural-alignment layout as buildStructDef in semcheck.go.
// structDefs holds previously registered structs and is used for nested struct sizing.
func buildStructDefIR(n *Node, structDefs map[string]*StructDef) *StructDef {
	sd := &StructDef{Name: n.Name, IsUnion: n.IsUnion}
	offset := 0
	bfWordOffset := -1
	bfBitsUsed := 0
	const bfWordBits = 64
	const bfWordSize = 8

	for _, child := range n.Children {
		// Inline struct/union definition node emitted as a sibling field (e.g. from
		// "struct { ... } tab[N];" in field grammar) — register it and skip.
		if child.Kind == KindStructDef {
			inner := buildStructDefIR(child, structDefs)
			if inner != nil {
				structDefs[child.Name] = inner
			}
			continue
		}

		// Anonymous or named-inline struct/union member.
		// Name=="" → anonymous member (fields promoted to outer scope).
		// Name!="" → named field whose type is defined inline (e.g. union { int a; } __value).
		if child.Type == TypeStruct && len(child.Children) > 0 {
			anonDef := &Node{Kind: KindStructDef, Name: child.StructTag,
				Children: child.Children, IsUnion: child.IsUnion}
			inner := buildStructDefIR(anonDef, structDefs)
			if inner != nil {
				structDefs[child.StructTag] = inner
			}
			sz, align := fieldSizeAlign(TypeStruct, child.StructTag, structDefs)
			if !sd.IsUnion {
				offset = (offset + align - 1) &^ (align - 1)
			}
			sd.Fields = append(sd.Fields, StructField{
				Name:      child.Name,
				Type:      TypeStruct,
				StructTag: child.StructTag,
				ByteOffset: offset,
			})
			if !sd.IsUnion {
				offset += sz
			}
			continue
		}

		isBF := child.BitWidth > 0
		isFlexArr := child.Type == TypeIntArray && child.Val == -1

		if isBF && !sd.IsUnion {
			if bfWordOffset == -1 || bfBitsUsed+child.BitWidth > bfWordBits {
				if bfWordOffset != -1 {
					offset = bfWordOffset + bfWordSize
				}
				offset = (offset + 7) &^ 7
				bfWordOffset = offset
				bfBitsUsed = 0
			}
			sd.Fields = append(sd.Fields, StructField{
				Name:       child.Name,
				Type:       child.Type,
				ByteOffset: bfWordOffset,
				IsBitField: true,
				BitOffset:  bfBitsUsed,
				BitWidth:   child.BitWidth,
			})
			bfBitsUsed += child.BitWidth
		} else {
			if bfWordOffset != -1 && !sd.IsUnion {
				offset = bfWordOffset + bfWordSize
				bfWordOffset = -1
				bfBitsUsed = 0
			}
			var sz, align int
			if child.Type == TypeIntArray && child.Val > 0 {
				sz, align = arrayFieldSizeAlign(child.ElemType, child.StructTag, child.Val, structDefs)
			} else {
				sz, align = fieldSizeAlign(child.Type, child.StructTag, structDefs)
			}
			if !sd.IsUnion {
				offset = (offset + align - 1) &^ (align - 1)
			}
			f := StructField{
				Name:        child.Name,
				Type:        child.Type,
				Pointee:     child.Pointee,
				ElemType:    child.ElemType,
				ElemPointee: child.ElemPointee,
				StructTag:   child.StructTag,
				ByteOffset:  offset,
				IsFlexArray: isFlexArr,
			}
			if child.Type == TypeIntArray && child.Val > 0 {
				f.ByteSize = sz
			}
			sd.Fields = append(sd.Fields, f)
			if !sd.IsUnion && !isFlexArr {
				offset += sz
			}
		}
	}
	return sd
}

func (g *irGen) genFunc(n *Node) {
	g.fn = &IRFunc{Name: n.Name, ReturnType: n.Type, ReturnPointee: n.Pointee,
		ReturnStructTag: n.StructTag}
	g.tempN = 0
	g.labelN = 0
	g.locals = make(map[string]localInfo)
	g.currentFuncName = n.Name
	g.staticLocals = make(map[string]string)

	// For struct-returning functions whose return struct exceeds 16 bytes, the caller
	// passes the destination address in X8 before the BL.  X8 is caller-saved and
	// will be clobbered by any inner call, so we save it on entry to the frame.
	if n.Type == TypeStruct {
		if sd, ok := g.prog.StructDefs[n.StructTag]; ok && sd.SizeBytes(g.prog.StructDefs) > 16 {
			g.fn.Locals = append(g.fn.Locals, IRLocal{Name: "__x8_save"})
			g.locals["__x8_save"] = localInfo{}
		}
	}

	nparams := len(n.Children) - 1
	realParams := 0
	for i := 0; i < nparams; i++ {
		p := n.Children[i]
		if p.Name == "..." {
			g.fn.IsVariadic = true
			continue
		}
		if p.Type == TypeStruct {
			// Struct-by-value param: storage goes in Locals (inline frame block),
			// not in the scalar param slot mechanism. buildFrame sees isArray=true
			// and allocates full SizeBytes via the Locals path (isArrBase).
			sz := 1
			if sd, ok := g.prog.StructDefs[p.StructTag]; ok {
				sz = (sd.SizeBytes(g.prog.StructDefs) + 7) / 8
			}
			g.fn.Locals = append(g.fn.Locals, IRLocal{
				Name: p.Name, IsStruct: true, StructTag: p.StructTag, ArrSize: sz,
			})
			g.locals[p.Name] = localInfo{isArray: true} // → isArrBase in buildFrame
		} else {
			isArr := p.Type == TypeIntArray
			g.locals[p.Name] = localInfo{isArray: isArr, isParam: true, arrSize: -1}
		}
		g.fn.Params = append(g.fn.Params, p.Name)
		g.fn.ParamType = append(g.fn.ParamType, p.Type)
		g.fn.ParamPointee = append(g.fn.ParamPointee, p.Pointee)
		g.fn.ParamStructTag = append(g.fn.ParamStructTag, p.StructTag)
		realParams++
	}

	g.emit(Quad{Op: IREnter, Extra: n.Name,
		Src1: IRAddr{Kind: AddrConst, IVal: realParams}})

	body := n.Children[len(n.Children)-1]
	g.genCompound(body)

	// Implicit void return at end of function.
	g.emit(Quad{Op: IRReturn})
	g.prog.Funcs = append(g.prog.Funcs, g.fn)
}

func (g *irGen) genCompound(n *Node) {
	for _, child := range n.Children {
		if child.Kind == KindStructDef {
			// Inline struct/union type defined inside a function body.
			sd := buildStructDefIR(child, g.prog.StructDefs)
			g.prog.StructDefs[sd.Name] = sd
			continue
		}
		if child.Kind == KindFunDecl {
			// Local function prototype — no codegen needed.
			continue
		}
		if child.Kind == KindVarDecl && child.IsConst {
			continue // const locals are folded away by semcheck; no storage needed
		}
		if child.Kind == KindVarDecl {
			// Static locals are allocated as globals with a mangled name.
			if child.IsStatic {
				mangledName := fmt.Sprintf("__static_%s_%s", g.currentFuncName, child.Name)
				g.staticLocals[child.Name] = mangledName
				g.locals[child.Name] = localInfo{isArray: child.Type == TypeIntArray, isStatic: true}
				isArrS := child.Type == TypeIntArray
				szS := 1
				if isArrS {
					szS = child.Val
					if child.Dim2 > 0 {
						szS = child.Val * child.Dim2
					}
				}
				gblS := IRGlobal{
					Name:     mangledName,
					IsArr:    isArrS,
					IsPtr:    isPtrType(child.Type),
					Pointee:  child.Pointee,
					Size:     szS,
					InnerDim: child.Dim2,
				}
				if len(child.Children) > 0 && child.Children[0].Kind == KindNum {
					gblS.HasInitVal = true
					gblS.InitVal = child.Children[0].Val
				}
				g.prog.Globals = append(g.prog.Globals, gblS)
				g.globals[mangledName] = &g.prog.Globals[len(g.prog.Globals)-1]
				continue
			}
			// 128-bit integer locals: allocate as 2-slot array with Is128 flag.
			if child.Type == TypeInt128 || child.Type == TypeUint128 {
				g.fn.Locals = append(g.fn.Locals, IRLocal{
					Name: child.Name, ArrSize: 2, Is128: true, IsArray: true,
				})
				g.locals[child.Name] = localInfo{isArray: true, arrSize: 2}
				if len(child.Children) > 0 {
					initAddr := g.genExpr(child.Children[0])
					// widen if the initializer is not already 128-bit
					initAddr = g.widen128(initAddr, child.Children[0].Type, child.Type == TypeUint128)
					g.emit(Quad{Op: IR128Copy, Dst: g.addrOf(child.Name), Src1: initAddr})
				}
				continue
			}
			isArr := child.Type == TypeIntArray
			isPtr := isPtrType(child.Type)
			isStruct := child.Type == TypeStruct
			sz := 1
			if isArr && !child.IsVLA {
				sz = child.Val
				if child.Dim2 > 0 {
					sz = child.Val * child.Dim2
				}
			} else if isStruct {
				if sd, ok := g.prog.StructDefs[child.StructTag]; ok {
					sz = (sd.SizeBytes(g.prog.StructDefs) + 7) / 8
				}
			}
			if child.IsVLA {
				// VLA: allocate a pointer-sized slot; emit IRVLAAlloc at runtime.
				g.fn.HasVLA = true
				g.locals[child.Name] = localInfo{isArray: true, arrSize: 0}
				g.fn.Locals = append(g.fn.Locals, IRLocal{
					Name:    child.Name,
					IsArray: true,
					IsVLA:   true,
					ArrSize: 0,
				})
				sizeVal := g.genExpr(child.Children[0])
				dst := g.addrOf(child.Name)
				g.emit(Quad{Op: IRVLAAlloc, Dst: dst, Src1: sizeVal})
			} else {
				g.locals[child.Name] = localInfo{isArray: isArr || isStruct, arrSize: sz, innerDim: child.Dim2}
				g.fn.Locals = append(g.fn.Locals, IRLocal{
					Name:      child.Name,
					IsArray:   isArr,
					IsPtr:     isPtr,
					IsStruct:  isStruct,
					Pointee:   child.Pointee,
					StructTag: child.StructTag,
					ArrSize:   sz,
					ElemType:  child.ElemType,
				})
				if len(child.Children) > 0 {
					if child.Children[0].Kind == KindInitList {
						// Brace-initializer: zero-fill then apply entries.
						baseAddr := g.addrOf(child.Name)
						ptrTmp := g.newTemp()
						g.emit(Quad{Op: IRGetAddr, Dst: ptrTmp, Src1: baseAddr})
						g.genInitList(child.Children[0], ptrTmp, sz*8, child)
					} else if !isArr && !isStruct {
						initVal := g.genExpr(child.Children[0])
						dst := g.addrOf(child.Name)
						if isFPType(child.Type) {
							initVal = g.coerceToFP(initVal, child.Children[0].Type)
							g.emit(Quad{Op: IRFCopy, Dst: dst, Src1: initVal})
						} else {
							g.emit(Quad{Op: IRCopy, Dst: dst, Src1: initVal})
						}
					}
				}
			}
		} else {
			g.genStmt(child)
		}
	}
}

// genInitList zero-fills the allocation and then applies each init entry.
// basePtr is an IRAddr holding the base pointer (address value in a temp or global).
// totalBytes is the total byte size of the allocation.
// decl is the variable's declaration node (for type/tag info).
func (g *irGen) genInitList(list *Node, basePtr IRAddr, totalBytes int, decl *Node) {
	// Step 1: zero-fill using 8-byte IRFieldStore (TypeHint=TypeLong → MOVD = 8-byte store).
	// IRFieldStore correctly loads the pointer value from basePtr, then stores at basePtr+offset.
	zero := IRAddr{Kind: AddrConst, IVal: 0}
	for off := 0; off+8 <= totalBytes; off += 8 {
		offAddr := IRAddr{Kind: AddrConst, IVal: off}
		g.emit(Quad{Op: IRFieldStore, Dst: basePtr, Src1: zero, Src2: offAddr, TypeHint: TypeLong})
	}

	// Step 2: store each designated entry over the zeros.
	for _, entry := range list.Children {
		g.genInitEntry(entry, basePtr, decl)
	}
}

// genInitEntry stores one initializer entry at its designated position.
// basePtr holds the base address (value loaded from a temp or global slot).
func (g *irGen) genInitEntry(entry *Node, basePtr IRAddr, decl *Node) {
	valNode := entry.Children[0]

	if decl.Type == TypeIntArray {
		// Array: entry.Val is element index; elements are stored at index*8 (LP64 stride).
		byteOff := entry.Val * 8
		offAddr := IRAddr{Kind: AddrConst, IVal: byteOff}

		if valNode.Kind == KindInitList {
			return // nested array-of-struct: not yet supported
		}
		val := g.genExpr(valNode)
		if isFPType(decl.ElemType) {
			val = g.coerceToFP(val, valNode.Type)
			g.emit(Quad{Op: IRFFieldStore, Dst: basePtr, Src1: val, Src2: offAddr, TypeHint: decl.ElemType})
		} else {
			// Store as 8-byte word (matching normal array element stride).
			g.emit(Quad{Op: IRFieldStore, Dst: basePtr, Src1: val, Src2: offAddr, TypeHint: TypeLong})
		}
		return
	}

	// Struct: entry.Val is byte offset (set by semcheck).
	offAddr := IRAddr{Kind: AddrConst, IVal: entry.Val}

	if valNode.Kind == KindInitList {
		// Nested struct: compute innerPtr = basePtr + offset, then recurse.
		innerPtr := g.newTemp()
		// Load basePtr value into innerPtr, then add offset.
		// Use IRFieldStore's Dst loading: we need to do pointer arithmetic.
		// Emit: innerPtr = *basePtr-slot + offset via add.
		// IRAdd does: Dst = Src1 + Src2 (integer add on register values).
		// But we need to add to the pointer VALUE, not a frame offset.
		// Emit a temp to hold the base address first, then add.
		baseCopy := g.newTemp()
		g.emit(Quad{Op: IRCopy, Dst: baseCopy, Src1: basePtr})
		g.emit(Quad{Op: IRAdd, Dst: innerPtr,
			Src1: baseCopy,
			Src2: IRAddr{Kind: AddrConst, IVal: entry.Val}})
		nestedSz := 8
		if sd, ok := g.prog.StructDefs[entry.StructTag]; ok {
			nestedSz = (sd.SizeBytes(g.prog.StructDefs) + 7) &^ 7
			if nestedSz == 0 {
				nestedSz = 8
			}
		}
		synthDecl := &Node{Kind: KindVarDecl, Type: entry.Type, StructTag: entry.StructTag}
		g.genInitList(valNode, innerPtr, nestedSz, synthDecl)
		return
	}

	val := g.genExpr(valNode)
	if isFPType(entry.Type) {
		val = g.coerceToFP(val, valNode.Type)
		g.emit(Quad{Op: IRFFieldStore, Dst: basePtr, Src1: val, Src2: offAddr, TypeHint: entry.Type})
	} else {
		g.emit(Quad{Op: IRFieldStore, Dst: basePtr, Src1: val, Src2: offAddr, TypeHint: entry.Type})
	}
}

// buildInitDataBuf fills buf with the constant values from a KindInitList node.
// Used for global variable initializers where all values must be compile-time constants.
// baseOff is the byte offset of buf[0] within the top-level global (used for relocs).
// String literal pointers are recorded as InitRelocs and the string content is
// appended to prog.StrLits.
func buildInitDataBuf(buf []byte, list *Node, decl *Node, structDefs map[string]*StructDef,
	baseOff int, prog *IRProgram, relocs *[]InitReloc) {
	for _, entry := range list.Children {
		valNode := entry.Children[0]
		byteOff := 0

		if decl.Type == TypeIntArray {
			// Array: entry.Val is element index.
			elemSz := 8 // default LP64 element size
			if entry.StructTag != "" {
				if sd, ok := structDefs[entry.StructTag]; ok {
					elemSz = sd.SizeBytes(structDefs)
				}
			}
			byteOff = entry.Val * elemSz
		} else {
			// Struct: entry.Val is already byte offset (set by semcheck).
			byteOff = entry.Val
		}

		if valNode.Kind == KindInitList {
			// Nested struct: recurse with a synthetic decl.
			synthDecl := &Node{Kind: KindVarDecl, Type: entry.Type, StructTag: entry.StructTag}
			if byteOff < len(buf) {
				subBuf := buf[byteOff:]
				buildInitDataBuf(subBuf, valNode, synthDecl, structDefs,
					baseOff+byteOff, prog, relocs)
			}
			continue
		}

		// Extract constant value.
		var ival int64
		var fval float64
		isFP := false
		isStr := false
		switch valNode.Kind {
		case KindNum:
			ival = int64(valNode.Val)
		case KindFNum:
			fval = valNode.FVal
			isFP = true
		case KindUnary:
			if valNode.Op == "-" && valNode.Children[0].Kind == KindNum {
				ival = -int64(valNode.Children[0].Val)
			} else if valNode.Op == "-" && valNode.Children[0].Kind == KindFNum {
				fval = -valNode.Children[0].FVal
				isFP = true
			}
		case KindCharLit:
			ival = int64(valNode.Val)
		case KindStrLit:
			// String literal pointer: create a string constant and record a reloc.
			label := fmt.Sprintf("str%d", len(prog.StrLits))
			prog.StrLits = append(prog.StrLits, IRStrLit{Label: label, Content: valNode.Name})
			*relocs = append(*relocs, InitReloc{ByteOff: baseOff + byteOff, Label: label})
			isStr = true
		default:
			continue // non-constant; semcheck should have caught this
		}

		if isStr {
			continue // address stored via reloc, not in byte buffer
		}

		// Determine store size from field type.
		storeSz := 8
		if decl.Type != TypeIntArray {
			storeSz, _ = fieldSizeAlign(entry.Type, entry.StructTag, structDefs)
		}

		if byteOff+storeSz > len(buf) {
			continue // out of bounds (shouldn't happen after semcheck)
		}

		if isFP {
			// float or double: write IEEE bits.
			if storeSz == 4 {
				bits := math.Float32bits(float32(fval))
				buf[byteOff+0] = byte(bits)
				buf[byteOff+1] = byte(bits >> 8)
				buf[byteOff+2] = byte(bits >> 16)
				buf[byteOff+3] = byte(bits >> 24)
			} else {
				bits := math.Float64bits(fval)
				for i := 0; i < 8; i++ {
					buf[byteOff+i] = byte(bits >> (uint(i) * 8))
				}
			}
		} else {
			// Integer: write little-endian.
			for i := 0; i < storeSz; i++ {
				buf[byteOff+i] = byte(ival >> (uint(i) * 8))
			}
		}
	}
}

func (g *irGen) genStmt(n *Node) {
	if n == nil {
		return
	}
	switch n.Kind {
	case KindExprStmt:
		if len(n.Children) > 0 {
			g.genExpr(n.Children[0])
		}
	case KindCompound:
		g.genCompound(n)
	case KindSelection:
		g.genIf(n)
	case KindIteration:
		g.genWhile(n)
	case KindFor:
		g.genFor(n)
	case KindDoWhile:
		g.genDoWhile(n)
	case KindSwitch:
		g.genSwitch(n)
	case KindBreak:
		if len(g.loopStack) == 0 {
			panic("break outside loop")
		}
		g.emitJump(g.loopStack[len(g.loopStack)-1].breakLabel)
	case KindContinue:
		if len(g.loopStack) == 0 {
			panic("continue outside loop")
		}
		g.emitJump(g.loopStack[len(g.loopStack)-1].continueLabel)
	case KindGoto:
		g.emitJump("user_" + n.Name)
	case KindLabel:
		g.emitLabel("user_" + n.Name)
		g.genStmt(n.Children[0])
	case KindReturn:
		if len(n.Children) > 0 {
			// Struct-by-value return: emit IRReturn with TypeHint=TypeStruct.
			if g.fn.ReturnStructTag != "" {
				child := n.Children[0]
				var src IRAddr
				if child.Kind == KindVar {
					src = g.addrOf(child.Name)
				} else if child.Kind == KindCall {
					if tag, ok := g.structRetFuncs[child.Name]; ok {
						slot := g.allocStructSlot(tag)
						g.genStructCallInto(child, slot, tag)
						src = slot
					} else {
						src = g.genExpr(child)
					}
				} else {
					src = g.genExpr(child)
				}
				g.emit(Quad{Op: IRReturn, TypeHint: TypeStruct,
					Src1: src, StructTag: g.fn.ReturnStructTag})
				return
			}
			v := g.genExpr(n.Children[0])
			if isFPType(g.fn.ReturnType) {
				v = g.coerceToFP(v, n.Children[0].Type)
			} else if isFPType(n.Children[0].Type) {
				// Returning FP value from non-FP function: truncate to int.
				tmp := g.newTemp()
				g.emit(Quad{Op: IRDoubleToInt, Dst: tmp, Src1: v})
				v = tmp
			}
			g.emit(Quad{Op: IRReturn, Src1: v})
		} else {
			g.emit(Quad{Op: IRReturn})
		}
	case KindVarDecl, KindStructDef, KindFunDecl:
		// C99 declarations in statement position (e.g. inside switch cases).
		// Wrap in a synthetic compound and delegate to genCompound.
		synth := &Node{Kind: KindCompound, Children: []*Node{n}}
		g.genCompound(synth)
	}
}

func (g *irGen) genIf(n *Node) {
	elseLabel := g.newLabel()
	endLabel := g.newLabel()

	cond := g.genExpr(n.Children[0])
	g.emit(Quad{Op: IRJumpF, Src1: cond, Extra: elseLabel})
	g.genStmt(n.Children[1])
	if len(n.Children) > 2 {
		g.emitJump(endLabel)
		g.emitLabel(elseLabel)
		g.genStmt(n.Children[2])
		g.emitLabel(endLabel)
	} else {
		g.emitLabel(elseLabel)
	}
}

func (g *irGen) genWhile(n *Node) {
	startLabel := g.newLabel()
	endLabel := g.newLabel()

	g.loopStack = append(g.loopStack, loopLabels{breakLabel: endLabel, continueLabel: startLabel})
	g.emitLabel(startLabel)
	cond := g.genExpr(n.Children[0])
	g.emit(Quad{Op: IRJumpF, Src1: cond, Extra: endLabel})
	g.genStmt(n.Children[1])
	g.emitJump(startLabel)
	g.emitLabel(endLabel)
	g.loopStack = g.loopStack[:len(g.loopStack)-1]
}

func (g *irGen) genFor(n *Node) {
	// Children: [init|nil, cond|nil, post|nil, body]
	condLabel := g.newLabel()
	postLabel := g.newLabel()
	endLabel := g.newLabel()

	// init — may be a compound (comma-init) or a plain expression
	if n.Children[0] != nil {
		if n.Children[0].Kind == KindCompound {
			g.genStmt(n.Children[0])
		} else {
			g.genExpr(n.Children[0])
		}
	}

	g.loopStack = append(g.loopStack, loopLabels{breakLabel: endLabel, continueLabel: postLabel})
	g.emitLabel(condLabel)

	// cond (nil = loop forever)
	if n.Children[1] != nil {
		cond := g.genExpr(n.Children[1])
		g.emit(Quad{Op: IRJumpF, Src1: cond, Extra: endLabel})
	}

	// body
	g.genStmt(n.Children[3])

	// post — may be a compound (comma-post) or a plain expression
	g.emitLabel(postLabel)
	if n.Children[2] != nil {
		if n.Children[2].Kind == KindCompound {
			g.genStmt(n.Children[2])
		} else {
			g.genExpr(n.Children[2])
		}
	}

	g.emitJump(condLabel)
	g.emitLabel(endLabel)
	g.loopStack = g.loopStack[:len(g.loopStack)-1]
}

func (g *irGen) genDoWhile(n *Node) {
	// Children: [body, cond]
	startLabel := g.newLabel()
	contLabel := g.newLabel()
	endLabel := g.newLabel()

	g.loopStack = append(g.loopStack, loopLabels{breakLabel: endLabel, continueLabel: contLabel})
	g.emitLabel(startLabel)
	g.genStmt(n.Children[0]) // body
	g.emitLabel(contLabel)
	cond := g.genExpr(n.Children[1])
	g.emit(Quad{Op: IRJumpT, Src1: cond, Extra: startLabel})
	g.emitLabel(endLabel)
	g.loopStack = g.loopStack[:len(g.loopStack)-1]
}

func (g *irGen) genSwitch(n *Node) {
	// Children: [switchExpr, case1, case2, ...]
	// KindCase: Val==-1 for default; Children[0]=expr (non-default), rest=stmts
	endLabel := g.newLabel()
	g.loopStack = append(g.loopStack, loopLabels{breakLabel: endLabel, continueLabel: endLabel})

	val := g.genExpr(n.Children[0])

	// Collect per-case labels and find the default case.
	cases := n.Children[1:]
	caseLabels := make([]string, len(cases))
	defaultLabel := endLabel
	for i, c := range cases {
		caseLabels[i] = g.newLabel()
		if c.Val == -1 {
			defaultLabel = caseLabels[i]
		}
	}

	// Emit comparison chain: for each non-default case, test val == caseExpr.
	for i, c := range cases {
		if c.Val == -1 {
			continue // default — handled by fallthrough
		}
		caseVal := g.genExpr(c.Children[0])
		cmp := g.newTemp()
		g.emit(Quad{Op: IREq, Dst: cmp, Src1: val, Src2: caseVal})
		g.emit(Quad{Op: IRJumpT, Src1: cmp, Extra: caseLabels[i]})
	}
	g.emitJump(defaultLabel)

	// Emit case bodies.
	for i, c := range cases {
		g.emitLabel(caseLabels[i])
		start := 0
		if c.Val != -1 {
			start = 1 // skip the case expression node
		}
		for _, s := range c.Children[start:] {
			g.genStmt(s)
		}
		// No implicit fall-through jump — break is handled by break statement.
		// If no break, execution falls through to the next case label naturally.
	}

	g.emitLabel(endLabel)
	g.loopStack = g.loopStack[:len(g.loopStack)-1]
}

// genExpr generates IR for an expression and returns the address of its result.
func (g *irGen) genExpr(n *Node) IRAddr {
	switch n.Kind {

	case KindNum:
		return IRAddr{Kind: AddrConst, IVal: n.Val}

	case KindCharLit:
		return IRAddr{Kind: AddrConst, IVal: n.Val}

	case KindFNum:
		label := fmt.Sprintf("fc%d", len(g.prog.FConsts))
		g.prog.FConsts = append(g.prog.FConsts, IRFConst{Label: label, Value: n.FVal})
		return IRAddr{Kind: AddrFConst, FVal: n.FVal, Name: label}

	case KindStrLit:
		label := fmt.Sprintf("str%d", len(g.prog.StrLits))
		g.prog.StrLits = append(g.prog.StrLits, IRStrLit{Label: label, Content: n.Name})
		dst := g.newTemp()
		g.emit(Quad{Op: IRStrAddr, Dst: dst, Extra: label})
		return dst

	case KindAddrOf:
		varNode := n.Children[0]
		if varNode.Kind == KindFieldAccess {
			// &expr.field or &expr->field: base pointer + field byte offset.
			// Handles the classic offsetof pattern &((T*)0)->field where base == 0.
			ptr := g.fieldBasePtr(varNode)
			offset := g.fieldByteOffset(varNode)
			if offset == 0 {
				return ptr
			}
			dst := g.newTemp()
			g.emit(Quad{Op: IRAdd, Dst: dst, Src1: ptr, Src2: IRAddr{Kind: AddrConst, IVal: offset}})
			return dst
		}
		// &var → get the storage address of the variable (never loads through a pointer slot)
		src := g.addrOf(varNode.Name)
		dst := g.newTemp()
		g.emit(Quad{Op: IRAddrOf, Dst: dst, Src1: src})
		return dst

	case KindDeref:
		// *ptr → load from the pointer value
		ptr := g.genExpr(n.Children[0])
		dst := g.newTemp()
		if isFPType(n.Type) {
			g.emit(Quad{Op: IRFDerefLoad, Dst: dst, Src1: ptr})
		} else {
			g.emit(Quad{Op: IRDerefLoad, Dst: dst, Src1: ptr, TypeHint: n.Type})
		}
		return dst

	case KindVar:
		// If the name is a function (not a variable), emit IRFuncAddr to get its address.
		// But local variables always shadow function names.
		if _, isLocal := g.locals[n.Name]; g.funcNames[n.Name] && !isLocal {
			dst := g.newTemp()
			// Register in FuncRefs if not already there.
			alreadyRef := false
			for _, ref := range g.prog.FuncRefs {
				if ref == n.Name {
					alreadyRef = true
					break
				}
			}
			if !alreadyRef {
				g.prog.FuncRefs = append(g.prog.FuncRefs, n.Name)
			}
			g.emit(Quad{Op: IRFuncAddr, Dst: dst, Extra: n.Name})
			return dst
		}
		addr := g.addrOf(n.Name)
		// Array-to-pointer decay: bare array name used as a value decays to
		// a pointer to the first element.  Skip 128-bit locals (2-slot arrays
		// representing a single wide integer, not a real C array).
		if li, ok := g.locals[n.Name]; ok && li.isArray && n.Type == TypeIntArray {
			tmp := g.newTemp()
			g.emit(Quad{Op: IRGetAddr, Dst: tmp, Src1: addr})
			return tmp
		}
		// Integer promotion: widen sub-word types to 64-bit before use in expressions.
		switch n.Type {
		case TypeChar:
			tmp := g.newTemp()
			g.emit(Quad{Op: IRSignExtend, Dst: tmp, Src1: addr,
				Src2: IRAddr{Kind: AddrConst, IVal: 8}})
			return tmp
		case TypeUnsignedChar:
			tmp := g.newTemp()
			g.emit(Quad{Op: IRZeroExtend, Dst: tmp, Src1: addr,
				Src2: IRAddr{Kind: AddrConst, IVal: 8}})
			return tmp
		case TypeShort:
			tmp := g.newTemp()
			g.emit(Quad{Op: IRSignExtend, Dst: tmp, Src1: addr,
				Src2: IRAddr{Kind: AddrConst, IVal: 16}})
			return tmp
		case TypeUnsignedShort:
			tmp := g.newTemp()
			g.emit(Quad{Op: IRZeroExtend, Dst: tmp, Src1: addr,
				Src2: IRAddr{Kind: AddrConst, IVal: 16}})
			return tmp
		}
		return addr

	case KindArrayVar:
		base := g.addrOf(n.Name)
		idx := g.genExpr(n.Children[0])
		dst := g.newTemp()
		if isFPType(n.Type) {
			g.emit(Quad{Op: IRLoad, Dst: dst, Src1: base, Src2: idx, TypeHint: n.Type})
		} else if n.Type == TypeChar {
			g.emit(Quad{Op: IRCharLoad, Dst: dst, Src1: base, Src2: idx})
		} else {
			g.emit(Quad{Op: IRLoad, Dst: dst, Src1: base, Src2: idx})
		}
		return dst

	case KindIndexExpr:
		// postfix_expr[index]: base is a pointer value, not a named variable.
		ptr := g.genExpr(n.Children[0])
		idx := g.genExpr(n.Children[1])
		dst := g.newTemp()
		if n.Type == TypeChar {
			addr := g.newTemp()
			g.emit(Quad{Op: IRAdd, Dst: addr, Src1: ptr, Src2: idx})
			g.emit(Quad{Op: IRDerefCharLoad, Dst: dst, Src1: addr})
		} else {
			scaled := g.newTemp()
			g.emit(Quad{Op: IRShl, Dst: scaled, Src1: idx, Src2: IRAddr{Kind: AddrConst, IVal: 3}})
			addr := g.newTemp()
			g.emit(Quad{Op: IRAdd, Dst: addr, Src1: ptr, Src2: scaled})
			if isFPType(n.Type) {
				g.emit(Quad{Op: IRFDerefLoad, Dst: dst, Src1: addr})
			} else {
				g.emit(Quad{Op: IRDerefLoad, Dst: dst, Src1: addr})
			}
		}
		return dst

	case KindArray2D:
		var innerDim int
		if li, ok := g.locals[n.Name]; ok {
			innerDim = li.innerDim
		} else if gbl, ok := g.globals[n.Name]; ok {
			innerDim = gbl.InnerDim
		}
		base := g.addrOf(n.Name)
		rowIdx := g.genExpr(n.Children[0])
		colIdx := g.genExpr(n.Children[1])
		// flat_idx = rowIdx * innerDim + colIdx
		scaled := g.newTemp()
		g.emit(Quad{Op: IRMul, Dst: scaled, Src1: rowIdx,
			Src2: IRAddr{Kind: AddrConst, IVal: innerDim}})
		flatIdx := g.newTemp()
		g.emit(Quad{Op: IRAdd, Dst: flatIdx, Src1: scaled, Src2: colIdx})
		dst := g.newTemp()
		if isFPType(n.Type) {
			g.emit(Quad{Op: IRLoad, Dst: dst, Src1: base, Src2: flatIdx, TypeHint: n.Type})
		} else if n.Type == TypeChar {
			g.emit(Quad{Op: IRCharLoad, Dst: dst, Src1: base, Src2: flatIdx})
		} else {
			g.emit(Quad{Op: IRLoad, Dst: dst, Src1: base, Src2: flatIdx})
		}
		return dst

	case KindAssign:
		lhs := n.Children[0]
		rhsNode := n.Children[1]
		// Fast path: struct-returning call directly into the lhs variable.
		if lhs.Kind == KindVar && rhsNode.Kind == KindCall {
			if tag, ok := g.structRetFuncs[rhsNode.Name]; ok {
				dstAddr := g.addrOf(lhs.Name)
				g.genStructCallInto(rhsNode, dstAddr, tag)
				return dstAddr
			}
		}
		rhs := g.genExpr(rhsNode)
		lhsType := n.Children[0].Type
		rhsType := n.Children[1].Type

		if isFPType(lhsType) {
			rhs = g.coerceToFP(rhs, rhsType)
		}

		switch lhs.Kind {
		case KindVar:
			addr := g.addrOf(lhs.Name)
			if lhsType == TypeStruct {
				// Struct-to-struct copy (x = y).  rhs is already the address
				// of the source struct (genExpr(KindVar of TypeStruct) = addrOf).
				tag := lhs.StructTag
				if tag == "" {
					tag = rhsNode.StructTag
				}
				g.emit(Quad{Op: IRStructCopy, Dst: addr, Src1: rhs, StructTag: tag})
			} else if isFPType(lhsType) {
				g.emit(Quad{Op: IRFCopy, Dst: addr, Src1: rhs})
			} else if isFPType(rhsType) {
				tmp := g.newTemp()
				g.emit(Quad{Op: IRDoubleToInt, Dst: tmp, Src1: rhs})
				g.emit(Quad{Op: IRCopy, Dst: addr, Src1: tmp})
			} else if lhsType == TypeInt128 || lhsType == TypeUint128 {
				// 128-bit assignment: copy both lo and hi halves.
				rhsWide := g.widen128(rhs, rhsType, lhsType == TypeUint128)
				g.emit(Quad{Op: IR128Copy, Dst: addr, Src1: rhsWide})
			} else {
				g.emit(Quad{Op: IRCopy, Dst: addr, Src1: rhs})
			}
			return addr
		case KindArrayVar:
			base := g.addrOf(lhs.Name)
			idx := g.genExpr(lhs.Children[0])
			if isFPType(lhsType) {
				g.emit(Quad{Op: IRStore, Dst: base, Src1: idx, Src2: rhs, TypeHint: lhsType})
			} else if lhsType == TypeChar {
				g.emit(Quad{Op: IRCharStore, Dst: base, Src1: idx, Src2: rhs})
			} else {
				g.emit(Quad{Op: IRStore, Dst: base, Src1: idx, Src2: rhs})
			}
			return rhs
		case KindIndexExpr:
			ptr := g.genExpr(lhs.Children[0])
			idx := g.genExpr(lhs.Children[1])
			if lhsType == TypeChar {
				addr := g.newTemp()
				g.emit(Quad{Op: IRAdd, Dst: addr, Src1: ptr, Src2: idx})
				g.emit(Quad{Op: IRDerefCharStore, Dst: addr, Src1: rhs})
			} else {
				scaled := g.newTemp()
				g.emit(Quad{Op: IRShl, Dst: scaled, Src1: idx, Src2: IRAddr{Kind: AddrConst, IVal: 3}})
				addr := g.newTemp()
				g.emit(Quad{Op: IRAdd, Dst: addr, Src1: ptr, Src2: scaled})
				if isFPType(lhsType) {
					g.emit(Quad{Op: IRFDerefStore, Dst: addr, Src1: rhs})
				} else {
					g.emit(Quad{Op: IRDerefStore, Dst: addr, Src1: rhs})
				}
			}
			return rhs
		case KindArray2D:
			var innerDim int
			if li, ok := g.locals[lhs.Name]; ok {
				innerDim = li.innerDim
			} else if gbl, ok := g.globals[lhs.Name]; ok {
				innerDim = gbl.InnerDim
			}
			base2d := g.addrOf(lhs.Name)
			rowIdx := g.genExpr(lhs.Children[0])
			colIdx := g.genExpr(lhs.Children[1])
			scaled2d := g.newTemp()
			g.emit(Quad{Op: IRMul, Dst: scaled2d, Src1: rowIdx,
				Src2: IRAddr{Kind: AddrConst, IVal: innerDim}})
			flatIdx2d := g.newTemp()
			g.emit(Quad{Op: IRAdd, Dst: flatIdx2d, Src1: scaled2d, Src2: colIdx})
			if isFPType(lhsType) {
				g.emit(Quad{Op: IRStore, Dst: base2d, Src1: flatIdx2d, Src2: rhs, TypeHint: lhsType})
			} else if lhsType == TypeChar {
				g.emit(Quad{Op: IRCharStore, Dst: base2d, Src1: flatIdx2d, Src2: rhs})
			} else {
				g.emit(Quad{Op: IRStore, Dst: base2d, Src1: flatIdx2d, Src2: rhs})
			}
			return rhs
		case KindDeref:
			ptr := g.genExpr(lhs.Children[0])
			if isFPType(lhsType) {
				g.emit(Quad{Op: IRFDerefStore, Dst: ptr, Src1: rhs})
			} else if isFPType(rhsType) {
				// Implicit double→int truncation (e.g. *long_ptr = double_var).
				tmp := g.newTemp()
				g.emit(Quad{Op: IRDoubleToInt, Dst: tmp, Src1: rhs})
				g.emit(Quad{Op: IRDerefStore, Dst: ptr, Src1: tmp, TypeHint: lhsType})
			} else {
				g.emit(Quad{Op: IRDerefStore, Dst: ptr, Src1: rhs, TypeHint: lhsType})
			}
			return rhs
		case KindFieldAccess:
			ptr := g.fieldBasePtr(lhs)
			offset := g.fieldByteOffset(lhs)
			offAddr := IRAddr{Kind: AddrConst, IVal: offset}
			if lhsType == TypeStruct {
				// outer.inner = some_struct: compute &(outer.inner) and struct-copy.
				// rhs is already the address of the source struct (genExpr returns
				// address for TypeStruct expressions).
				dstAddr := ptr
				if offset > 0 {
					dstAddr = g.newTemp()
					g.emit(Quad{Op: IRAdd, Dst: dstAddr, Src1: ptr, Src2: offAddr})
				}
				g.emit(Quad{Op: IRStructCopy, Dst: dstAddr, Src1: rhs, StructTag: lhs.StructTag})
				return rhs
			}
			sf := g.lookupStructField(lhs)
			if sf != nil && sf.IsBitField {
				// Read-modify-write for bit-field store.
				word := g.newTemp()
				g.emit(Quad{Op: IRFieldLoad, Dst: word, Src1: ptr, Src2: offAddr, TypeHint: TypeInt})
				mask := (1 << sf.BitWidth) - 1
				clearMask := ^(mask << sf.BitOffset)
				cleared := g.newTemp()
				g.emit(Quad{Op: IRBitAnd, Dst: cleared, Src1: word,
					Src2: IRAddr{Kind: AddrConst, IVal: clearMask}})
				valMasked := g.newTemp()
				g.emit(Quad{Op: IRBitAnd, Dst: valMasked, Src1: rhs,
					Src2: IRAddr{Kind: AddrConst, IVal: mask}})
				shifted := valMasked
				if sf.BitOffset > 0 {
					shifted = g.newTemp()
					g.emit(Quad{Op: IRShl, Dst: shifted, Src1: valMasked,
						Src2: IRAddr{Kind: AddrConst, IVal: sf.BitOffset}})
				}
				merged := g.newTemp()
				g.emit(Quad{Op: IRBitOr, Dst: merged, Src1: cleared, Src2: shifted})
				g.emit(Quad{Op: IRFieldStore, Dst: ptr, Src1: merged, Src2: offAddr, TypeHint: TypeInt})
				return rhs
			}
			if isFPType(lhsType) {
				g.emit(Quad{Op: IRFFieldStore, Dst: ptr, Src1: rhs, Src2: offAddr, TypeHint: lhsType})
			} else if isFPType(rhsType) {
				// Implicit double→int truncation (e.g. struct.long_field = double_var).
				tmp := g.newTemp()
				g.emit(Quad{Op: IRDoubleToInt, Dst: tmp, Src1: rhs})
				g.emit(Quad{Op: IRFieldStore, Dst: ptr, Src1: tmp, Src2: offAddr, TypeHint: lhsType})
			} else {
				g.emit(Quad{Op: IRFieldStore, Dst: ptr, Src1: rhs, Src2: offAddr, TypeHint: lhsType})
			}
			return rhs
		}

	case KindCompoundAssign:
		// Desugar: lhs op= rhs  →  lhs = lhs op rhs
		lhs := n.Children[0]
		lhsType := lhs.Type
		var rhsAddr IRAddr
		if n.Children[1] != nil {
			rhsAddr = g.genExpr(n.Children[1])
		} else {
			rhsAddr = IRAddr{Kind: AddrConst, IVal: n.Val}
		}
		irOp := binOpToIRTyped(n.Op, lhsType)
		tmp := g.newTemp()

		// Pointer arithmetic: scale the step value by element size.
		if isPtrType(lhsType) && (n.Op == "+" || n.Op == "-") {
			sz := elemSize(lhs.Pointee)
			if sz > 1 {
				scaled := g.newTemp()
				g.emit(Quad{Op: IRMul, Dst: scaled, Src1: rhsAddr, Src2: IRAddr{Kind: AddrConst, IVal: sz}})
				rhsAddr = scaled
			}
			irOp = IRAdd
			if n.Op == "-" {
				irOp = IRSub
			}
		}

		switch lhs.Kind {
		case KindVar:
			lhsAddr := g.addrOf(lhs.Name)
			// Use genExpr so sub-word types are promoted before the operation.
			currentVal := g.genExpr(lhs)
			g.emit(Quad{Op: irOp, Dst: tmp, Src1: currentVal, Src2: rhsAddr})
			g.emit(Quad{Op: IRCopy, Dst: lhsAddr, Src1: tmp})
			return lhsAddr
		case KindArrayVar:
			base := g.addrOf(lhs.Name)
			idx := g.genExpr(lhs.Children[0])
			elem := g.newTemp()
			g.emit(Quad{Op: IRLoad, Dst: elem, Src1: base, Src2: idx})
			g.emit(Quad{Op: irOp, Dst: tmp, Src1: elem, Src2: rhsAddr})
			g.emit(Quad{Op: IRStore, Dst: base, Src1: idx, Src2: tmp})
			return tmp
		case KindIndexExpr:
			ptr := g.genExpr(lhs.Children[0])
			idx := g.genExpr(lhs.Children[1])
			elem := g.newTemp()
			if lhsType == TypeChar {
				addr := g.newTemp()
				g.emit(Quad{Op: IRAdd, Dst: addr, Src1: ptr, Src2: idx})
				g.emit(Quad{Op: IRDerefCharLoad, Dst: elem, Src1: addr})
				g.emit(Quad{Op: irOp, Dst: tmp, Src1: elem, Src2: rhsAddr})
				g.emit(Quad{Op: IRDerefCharStore, Dst: addr, Src1: tmp})
			} else {
				scaled := g.newTemp()
				g.emit(Quad{Op: IRShl, Dst: scaled, Src1: idx, Src2: IRAddr{Kind: AddrConst, IVal: 3}})
				addr := g.newTemp()
				g.emit(Quad{Op: IRAdd, Dst: addr, Src1: ptr, Src2: scaled})
				if isFPType(lhsType) {
					g.emit(Quad{Op: IRFDerefLoad, Dst: elem, Src1: addr})
					g.emit(Quad{Op: irOp, Dst: tmp, Src1: elem, Src2: rhsAddr})
					g.emit(Quad{Op: IRFDerefStore, Dst: addr, Src1: tmp})
				} else {
					g.emit(Quad{Op: IRDerefLoad, Dst: elem, Src1: addr})
					g.emit(Quad{Op: irOp, Dst: tmp, Src1: elem, Src2: rhsAddr})
					g.emit(Quad{Op: IRDerefStore, Dst: addr, Src1: tmp})
				}
			}
			return tmp
		}


	case KindPostInc:
		return g.genIncDec(n.Children[0], true, true)
	case KindPostDec:
		return g.genIncDec(n.Children[0], false, true)
	case KindPreInc:
		return g.genIncDec(n.Children[0], true, false)
	case KindPreDec:
		return g.genIncDec(n.Children[0], false, false)

	case KindBinOp:
		if isFPType(n.Type) || isFPType(n.Children[0].Type) || isFPType(n.Children[1].Type) {
			return g.genFPBinOp(n)
		}
		// 128-bit arithmetic dispatch.
		if n.Type == TypeInt128 || n.Type == TypeUint128 ||
			n.Children[0].Type == TypeInt128 || n.Children[0].Type == TypeUint128 ||
			n.Children[1].Type == TypeInt128 || n.Children[1].Type == TypeUint128 {
			return g.gen128BinOp(n)
		}
		// Pointer arithmetic: scale the integer operand by element size.
		leftType := n.Children[0].Type
		rightType := n.Children[1].Type
		if isPtrType(leftType) && !isPtrType(rightType) && (n.Op == "+" || n.Op == "-") {
			return g.genPtrArith(n.Children[0], n.Children[1], n.Children[0].Pointee, n.Op)
		}
		if isPtrType(rightType) && !isPtrType(leftType) && n.Op == "+" {
			return g.genPtrArith(n.Children[1], n.Children[0], n.Children[1].Pointee, "+")
		}
		left := g.genExpr(n.Children[0])
		right := g.genExpr(n.Children[1])
		dst := g.newTemp()
		op := binOpToIRTyped(n.Op, n.Children[0].Type)
		g.emit(Quad{Op: op, Dst: dst, Src1: left, Src2: right})
		return dst

	case KindLogAnd:
		dst := g.newTemp()
		doneL := g.newLabel()
		g.emit(Quad{Op: IRCopy, Dst: dst, Src1: IRAddr{Kind: AddrConst, IVal: 0}})
		left := g.genExpr(n.Children[0])
		g.emit(Quad{Op: IRJumpF, Src1: left, Extra: doneL})
		right := g.genExpr(n.Children[1])
		g.emit(Quad{Op: IRJumpF, Src1: right, Extra: doneL})
		g.emit(Quad{Op: IRCopy, Dst: dst, Src1: IRAddr{Kind: AddrConst, IVal: 1}})
		g.emitLabel(doneL)
		return dst

	case KindLogOr:
		dst := g.newTemp()
		doneL := g.newLabel()
		g.emit(Quad{Op: IRCopy, Dst: dst, Src1: IRAddr{Kind: AddrConst, IVal: 1}})
		left := g.genExpr(n.Children[0])
		g.emit(Quad{Op: IRJumpT, Src1: left, Extra: doneL})
		right := g.genExpr(n.Children[1])
		g.emit(Quad{Op: IRJumpT, Src1: right, Extra: doneL})
		g.emit(Quad{Op: IRCopy, Dst: dst, Src1: IRAddr{Kind: AddrConst, IVal: 0}})
		g.emitLabel(doneL)
		return dst

	case KindCommaExpr:
		// C comma operator: evaluate left for side effects, return value of right.
		g.genExpr(n.Children[0])
		return g.genExpr(n.Children[1])

	case KindTernary:
		condVal := g.genExpr(n.Children[0])
		thenLbl := g.newLabel()
		elseLbl := g.newLabel()
		endLbl := g.newLabel()
		result := g.newTemp()
		g.emit(Quad{Op: IRJumpT, Src1: condVal, Extra: thenLbl})
		g.emitJump(elseLbl)
		g.emitLabel(thenLbl)
		thenVal := g.genExpr(n.Children[1])
		if isFPType(n.Type) {
			thenVal = g.coerceToFP(thenVal, n.Children[1].Type)
			g.emit(Quad{Op: IRFCopy, Dst: result, Src1: thenVal})
		} else {
			g.emit(Quad{Op: IRCopy, Dst: result, Src1: thenVal})
		}
		g.emitJump(endLbl)
		g.emitLabel(elseLbl)
		elseVal := g.genExpr(n.Children[2])
		if isFPType(n.Type) {
			elseVal = g.coerceToFP(elseVal, n.Children[2].Type)
			g.emit(Quad{Op: IRFCopy, Dst: result, Src1: elseVal})
		} else {
			g.emit(Quad{Op: IRCopy, Dst: result, Src1: elseVal})
		}
		g.emitLabel(endLbl)
		return result

	case KindUnary:
		operand := g.genExpr(n.Children[0])
		dst := g.newTemp()
		zero := IRAddr{Kind: AddrConst, IVal: 0}
		if isFPType(n.Children[0].Type) {
			switch n.Op {
			case "-":
				g.emit(Quad{Op: IRFNeg, Dst: dst, Src1: operand})
			case "!":
				// !fp: fp == 0.0
				zeroFP := g.fpZero()
				g.emit(Quad{Op: IRFEq, Dst: dst, Src1: operand, Src2: zeroFP})
			}
			return dst
		}
		switch n.Op {
		case "-": // 0 - operand
			g.emit(Quad{Op: IRSub, Dst: dst, Src1: zero, Src2: operand})
		case "!": // operand == 0
			g.emit(Quad{Op: IREq, Dst: dst, Src1: operand, Src2: zero})
		case "~": // bitwise NOT
			g.emit(Quad{Op: IRBitNot, Dst: dst, Src1: operand})
		}
		return dst

	case KindFieldAccess:
		ptr := g.fieldBasePtr(n)
		sf := g.lookupStructField(n)
		if sf != nil && sf.IsFlexArray {
			// Flex array member: return pointer = base + ByteOffset
			dst := g.newTemp()
			if sf.ByteOffset == 0 {
				return ptr
			}
			g.emit(Quad{Op: IRAdd, Dst: dst, Src1: ptr,
				Src2: IRAddr{Kind: AddrConst, IVal: sf.ByteOffset}})
			return dst
		}
		offset := g.fieldByteOffset(n)
		offAddr := IRAddr{Kind: AddrConst, IVal: offset}
		dst := g.newTemp()
		if sf != nil && sf.IsBitField {
			// Load full 8-byte word, then extract bit field.
			word := g.newTemp()
			g.emit(Quad{Op: IRFieldLoad, Dst: word, Src1: ptr, Src2: offAddr, TypeHint: TypeInt})
			if sf.BitOffset > 0 {
				shifted := g.newTemp()
				g.emit(Quad{Op: IRShr, Dst: shifted, Src1: word,
					Src2: IRAddr{Kind: AddrConst, IVal: sf.BitOffset}})
				word = shifted
			}
			mask := (1 << sf.BitWidth) - 1
			g.emit(Quad{Op: IRBitAnd, Dst: dst, Src1: word,
				Src2: IRAddr{Kind: AddrConst, IVal: mask}})
			return dst
		}
		if n.Type == TypeStruct || n.Type == TypeIntArray {
			// Return the address of the nested struct/array field rather than
			// loading its first word.  Arrays decay to pointers; structs need
			// address for copy or chained access.
			if offset == 0 {
				return ptr
			}
			g.emit(Quad{Op: IRAdd, Dst: dst, Src1: ptr, Src2: offAddr})
			return dst
		}
		if isFPType(n.Type) {
			g.emit(Quad{Op: IRFFieldLoad, Dst: dst, Src1: ptr, Src2: offAddr, TypeHint: n.Type})
		} else {
			g.emit(Quad{Op: IRFieldLoad, Dst: dst, Src1: ptr, Src2: offAddr, TypeHint: n.Type})
		}
		return dst

	case KindCall:
		return g.genCall(n)

	case KindFuncPtrCall:
		return g.genFuncPtrCall(n)

	case KindIndirectCall:
		return g.genIndirectCall(n)

	case KindVAArg:
		// va_arg(ap, T): load *ap (8 bytes), advance ap by 8, return the value.
		// Children[0] must be a KindVar naming the va_list local (long*).
		apVal := g.genExpr(n.Children[0]) // current pointer value stored in ap

		// Load 8 bytes from *ap as the correct type.
		// FP slots were stored as integer bit patterns (via IRFBitcastFI at call site),
		// so read as int64 then bitcast back to double.
		result := g.newTemp()
		if isFPType(n.Type) {
			intBits := g.newTemp()
			g.emit(Quad{Op: IRDerefLoad, Dst: intBits, Src1: apVal})
			g.emit(Quad{Op: IRFBitcastIF, Dst: result, Src1: intBits})
		} else {
			g.emit(Quad{Op: IRDerefLoad, Dst: result, Src1: apVal})
		}

		// Advance ap by 8 bytes (one variadic slot) — raw byte add, no pointer scaling.
		newAp := g.newTemp()
		g.emit(Quad{Op: IRAdd, Dst: newAp, Src1: apVal, Src2: IRAddr{Kind: AddrConst, IVal: 8}})

		// Write new pointer value back into the ap lvalue.
		apNode := n.Children[0]
		switch apNode.Kind {
		case KindVar:
			apAddr := g.addrOf(apNode.Name)
			g.emit(Quad{Op: IRCopy, Dst: apAddr, Src1: newAp})
		case KindFieldAccess:
			ptr := g.fieldBasePtr(apNode)
			offset := g.fieldByteOffset(apNode)
			offAddr := IRAddr{Kind: AddrConst, IVal: offset}
			g.emit(Quad{Op: IRFieldStore, Dst: ptr, Src1: newAp, Src2: offAddr})
		case KindDeref:
			ptr := g.genExpr(apNode.Children[0])
			g.emit(Quad{Op: IRDerefStore, Dst: ptr, Src1: newAp})
		default:
			// Fallback: try addrOf (covers simple names).
			apAddr := g.addrOf(apNode.Name)
			g.emit(Quad{Op: IRCopy, Dst: apAddr, Src1: newAp})
		}

		return result

	case KindCast:
		src := g.genExpr(n.Children[0])
		srcType := n.Children[0].Type
		dstType := n.Type
		// Same type → identity.
		if srcType == dstType {
			return src
		}
		// → 128-bit widening cast.
		if dstType == TypeInt128 || dstType == TypeUint128 {
			return g.widen128(src, srcType, dstType == TypeUint128)
		}
		// Narrowing from 128-bit to 64-bit (or smaller).
		if srcType == TypeInt128 || srcType == TypeUint128 {
			dst := g.newTemp()
			g.emit(Quad{Op: IR64From128, Dst: dst, Src1: src})
			src = dst
			srcType = TypeLong // treat as 64-bit for further narrowing below
		}
		// → double/float: promote via IRIntToDouble.
		if isFPType(dstType) {
			return g.coerceToFP(src, srcType)
		}
		// double/float → int or ptr: truncate.
		if isFPType(srcType) {
			dst := g.newTemp()
			g.emit(Quad{Op: IRDoubleToInt, Dst: dst, Src1: src})
			return dst
		}
		// → char (8-bit truncation + sign-extend).
		if dstType == TypeChar {
			dst := g.newTemp()
			g.emit(Quad{Op: IRSignExtend, Dst: dst, Src1: src,
				Src2: IRAddr{Kind: AddrConst, IVal: 8}})
			return dst
		}
		// → unsigned char (8-bit truncation + zero-extend).
		if dstType == TypeUnsignedChar {
			dst := g.newTemp()
			g.emit(Quad{Op: IRZeroExtend, Dst: dst, Src1: src,
				Src2: IRAddr{Kind: AddrConst, IVal: 8}})
			return dst
		}
		// → short (16-bit truncation + sign-extend).
		if dstType == TypeShort {
			dst := g.newTemp()
			g.emit(Quad{Op: IRSignExtend, Dst: dst, Src1: src,
				Src2: IRAddr{Kind: AddrConst, IVal: 16}})
			return dst
		}
		// → unsigned short (16-bit truncation + zero-extend).
		if dstType == TypeUnsignedShort {
			dst := g.newTemp()
			g.emit(Quad{Op: IRZeroExtend, Dst: dst, Src1: src,
				Src2: IRAddr{Kind: AddrConst, IVal: 16}})
			return dst
		}
		// All other casts (int↔unsigned, ptr↔int, ptr↔ptr): identity (all 64-bit).
		return src

	case KindCompoundLit:
		return g.genCompoundLit(n)

	case KindStmtExpr:
		return g.genStmtExpr(n)
	}
	return IRAddr{Kind: AddrNone}
}

// fieldBasePtr returns the IR address holding the base pointer for a field access node.
// For "->": evaluates the pointer expression.
// For ".": emits IRGetAddr to get the struct's base address, handling chained dot access
// (e.g. outer.inner_val.field) by recursively computing the address of the intermediate field.
func (g *irGen) fieldBasePtr(n *Node) IRAddr {
	if n.Op == "->" {
		return g.genExpr(n.Children[0])
	}
	// "." — get address of the struct variable (possibly a chained field access)
	base := n.Children[0]
	if base.Kind == KindArrayVar {
		// Array element field access: arr[i].field
		// Compute base address = &arr + i * structSize.
		idxNode := base.Children[0]
		arrAddr := g.newTemp()
		src := g.addrOf(base.Name)
		g.emit(Quad{Op: IRGetAddr, Dst: arrAddr, Src1: src})
		idx := g.genExpr(idxNode)
		// Determine struct element size.
		elemSz := 8 // default
		if base.StructTag != "" {
			if sd, ok := g.prog.StructDefs[base.StructTag]; ok {
				elemSz = sd.SizeBytes(g.prog.StructDefs)
			}
		}
		byteIdx := g.newTemp()
		g.emit(Quad{Op: IRMul, Dst: byteIdx, Src1: idx,
			Src2: IRAddr{Kind: AddrConst, IVal: elemSz}})
		result := g.newTemp()
		g.emit(Quad{Op: IRAdd, Dst: result, Src1: arrAddr, Src2: byteIdx})
		return result
	}
	if base.Kind == KindIndexExpr {
		// Expression-based array element: expr[i].field
		arrNode := base.Children[0]
		idxNode := base.Children[1]
		arrAddr := g.genExpr(arrNode) // evaluate base expression to get pointer
		idx := g.genExpr(idxNode)
		elemSz := 8
		if base.StructTag != "" {
			if sd, ok := g.prog.StructDefs[base.StructTag]; ok {
				elemSz = sd.SizeBytes(g.prog.StructDefs)
			}
		}
		byteIdx := g.newTemp()
		g.emit(Quad{Op: IRMul, Dst: byteIdx, Src1: idx,
			Src2: IRAddr{Kind: AddrConst, IVal: elemSz}})
		result := g.newTemp()
		g.emit(Quad{Op: IRAdd, Dst: result, Src1: arrAddr, Src2: byteIdx})
		return result
	}
	if base.Kind == KindFieldAccess {
		// Chained field access: base is either "a.b" or "a->b".
		// In both cases, fieldBasePtr(base) gives a pointer to the base struct,
		// and fieldByteOffset(base) gives the byte offset of the inner field.
		// Compute address = outerBase + innerFieldOffset.
		outerBase := g.fieldBasePtr(base)
		innerOffset := g.fieldByteOffset(base)
		if innerOffset == 0 {
			return outerBase
		}
		tmp := g.newTemp()
		g.emit(Quad{Op: IRAdd, Dst: tmp, Src1: outerBase,
			Src2: IRAddr{Kind: AddrConst, IVal: innerOffset}})
		return tmp
	}
	// Simple case: base is a plain variable.
	src := g.addrOf(base.Name)
	tmp := g.newTemp()
	g.emit(Quad{Op: IRGetAddr, Dst: tmp, Src1: src})
	return tmp
}

// fieldByteOffset looks up the byte offset of the named field in the struct.
func (g *irGen) fieldByteOffset(n *Node) int {
	f := g.lookupStructField(n)
	if f == nil {
		panic("irgen: struct has no field '" + n.Name + "'")
	}
	return f.ByteOffset
}

// lookupStructField returns the StructField for a KindFieldAccess node, or nil.
// Searches anonymous (nameless) embedded members via FindFieldDeep.
func (g *irGen) lookupStructField(n *Node) *StructField {
	structTag := n.Children[0].StructTag
	if structTag == "" {
		return nil
	}
	sd := g.prog.StructDefs[structTag]
	if sd == nil {
		return nil
	}
	return sd.FindFieldDeep(n.Name, g.prog.StructDefs)
}

// genIncDec emits a load-modify-store sequence for x++/x--/++x/--x.
//   lval        — the lvalue node (KindVar, KindArrayVar, KindDeref, KindFieldAccess)
//   isIncrement — true for ++, false for --
//   post        — true to return old value (postfix), false to return new value (prefix)
func (g *irGen) genIncDec(lval *Node, isIncrement bool, post bool) IRAddr {
	lvalType := lval.Type

	// Choose add or subtract.
	irOp := IRAdd
	if !isIncrement {
		irOp = IRSub
	}

	// For pointer types, scale the step by the pointee element size.
	step := IRAddr{Kind: AddrConst, IVal: 1}
	if isPtrType(lvalType) {
		sz := elemSize(lval.Pointee)
		if sz > 1 {
			scaled := g.newTemp()
			g.emit(Quad{Op: IRMul, Dst: scaled,
				Src1: IRAddr{Kind: AddrConst, IVal: 1},
				Src2: IRAddr{Kind: AddrConst, IVal: sz}})
			step = scaled
		}
	}

	var old, new_ IRAddr

	switch lval.Kind {
	case KindVar:
		addr := g.addrOf(lval.Name)
		// Explicitly load into a fresh temp to snapshot the current value.
		// (g.genExpr(lval) returns addr itself — an AddrLocal — which, if returned
		// as "old" for postfix, would resolve to the *new* value after the store.)
		old = g.newTemp()
		g.emit(Quad{Op: IRCopy, Dst: old, Src1: addr})
		new_ = g.newTemp()
		g.emit(Quad{Op: irOp, Dst: new_, Src1: old, Src2: step})
		g.emit(Quad{Op: IRCopy, Dst: addr, Src1: new_})

	case KindArrayVar:
		base := g.addrOf(lval.Name)
		idx := g.genExpr(lval.Children[0])
		old = g.newTemp()
		g.emit(Quad{Op: IRLoad, Dst: old, Src1: base, Src2: idx})
		new_ = g.newTemp()
		g.emit(Quad{Op: irOp, Dst: new_, Src1: old, Src2: step})
		g.emit(Quad{Op: IRStore, Dst: base, Src1: idx, Src2: new_})

	case KindIndexExpr:
		ptr := g.genExpr(lval.Children[0])
		idx := g.genExpr(lval.Children[1])
		old = g.newTemp()
		new_ = g.newTemp()
		if lvalType == TypeChar {
			addr := g.newTemp()
			g.emit(Quad{Op: IRAdd, Dst: addr, Src1: ptr, Src2: idx})
			g.emit(Quad{Op: IRDerefCharLoad, Dst: old, Src1: addr})
			g.emit(Quad{Op: irOp, Dst: new_, Src1: old, Src2: step})
			g.emit(Quad{Op: IRDerefCharStore, Dst: addr, Src1: new_})
		} else {
			scaled := g.newTemp()
			g.emit(Quad{Op: IRShl, Dst: scaled, Src1: idx, Src2: IRAddr{Kind: AddrConst, IVal: 3}})
			addr := g.newTemp()
			g.emit(Quad{Op: IRAdd, Dst: addr, Src1: ptr, Src2: scaled})
			g.emit(Quad{Op: IRDerefLoad, Dst: old, Src1: addr})
			g.emit(Quad{Op: irOp, Dst: new_, Src1: old, Src2: step})
			g.emit(Quad{Op: IRDerefStore, Dst: addr, Src1: new_})
		}

	case KindDeref:
		ptrVal := g.genExpr(lval.Children[0])
		old = g.newTemp()
		g.emit(Quad{Op: IRDerefLoad, Dst: old, Src1: ptrVal, TypeHint: lvalType})
		new_ = g.newTemp()
		g.emit(Quad{Op: irOp, Dst: new_, Src1: old, Src2: step})
		g.emit(Quad{Op: IRDerefStore, Dst: ptrVal, Src1: new_, TypeHint: lvalType})

	case KindFieldAccess:
		ptr := g.fieldBasePtr(lval)
		offset := g.fieldByteOffset(lval)
		offAddr := IRAddr{Kind: AddrConst, IVal: offset}
		old = g.newTemp()
		g.emit(Quad{Op: IRFieldLoad, Dst: old, Src1: ptr, Src2: offAddr, TypeHint: lvalType})
		new_ = g.newTemp()
		g.emit(Quad{Op: irOp, Dst: new_, Src1: old, Src2: step})
		g.emit(Quad{Op: IRFieldStore, Dst: ptr, Src1: new_, Src2: offAddr, TypeHint: lvalType})

	default:
		panic(fmt.Sprintf("irgen: unsupported lvalue kind %v for inc/dec", lval.Kind))
	}

	if post {
		return old
	}
	return new_
}

// callArgSlot holds a fully-materialized argument ready for IRParam emission.
// Separating materialization from emission prevents inner calls (which drain
// pendingParams) from consuming params that belong to the outer call.
type callArgSlot struct {
	addr      IRAddr
	isStruct  bool
	isFP      bool
	structTag string
}

// collectCallArgs evaluates every argument expression (possibly triggering
// inner calls that drain pendingParams), then returns a slice of ready slots.
// IRParam quads must be emitted only AFTER all args are collected.
func (g *irGen) collectCallArgs(children []*Node, isVariadic bool) []callArgSlot {
	slots := make([]callArgSlot, 0, len(children))
	for _, arg := range children {
		if arg.Type == TypeStruct {
			srcAddr := g.materializeStructArg(arg)
			slots = append(slots, callArgSlot{addr: srcAddr, isStruct: true, structTag: arg.StructTag})
			continue
		}
		var val IRAddr
		if arg.Kind == KindVar && g.isArrayName(arg.Name) {
			tmp := g.newTemp()
			g.emit(Quad{Op: IRGetAddr, Dst: tmp, Src1: g.addrOf(arg.Name)})
			val = tmp
		} else {
			val = g.genExpr(arg)
		}
		if isFPType(arg.Type) {
			if isVariadic {
				// At a variadic call site, pass FP args through integer registers so
				// the callee's register-save area (X1-X7) captures the bit pattern.
				intBits := g.newTemp()
				g.emit(Quad{Op: IRFBitcastFI, Dst: intBits, Src1: val})
				slots = append(slots, callArgSlot{addr: intBits})
			} else {
				slots = append(slots, callArgSlot{addr: val, isFP: true})
			}
		} else {
			slots = append(slots, callArgSlot{addr: val})
		}
	}
	return slots
}

// emitCallArgParams emits IRParam / IRFParam quads for the given slots.
func (g *irGen) emitCallArgParams(slots []callArgSlot) {
	for _, s := range slots {
		if s.isStruct {
			g.emit(Quad{Op: IRParam, Src1: s.addr, TypeHint: TypeStruct, StructTag: s.structTag})
		} else if s.isFP {
			g.emit(Quad{Op: IRFParam, Src1: s.addr})
		} else {
			g.emit(Quad{Op: IRParam, Src1: s.addr})
		}
	}
}

func (g *irGen) genCall(n *Node) IRAddr {
	// Intercept GCC bit-manipulation intrinsics — emit inline IR, no call.
	switch n.Name {
	case "__builtin_clz", "__builtin_clzl", "__builtin_clzll",
		"__builtin_ctz", "__builtin_ctzl", "__builtin_ctzll",
		"__builtin_popcount", "__builtin_popcountl", "__builtin_popcountll":
		return g.genBuiltinBitop(n)
	case "__builtin_expect":
		// __builtin_expect(expr, hint) — just return expr, ignore hint.
		if len(n.Children) >= 1 {
			return g.genExpr(n.Children[0])
		}
		return g.newTemp()
	}
	isVariadic := g.variadicFuncs[n.Name]
	// Phase 1: materialize all args (inner calls complete here, draining pendingParams).
	slots := g.collectCallArgs(n.Children, isVariadic)
	// Phase 2: emit IRParams for THIS call only.
	g.emitCallArgParams(slots)
	nargs := IRAddr{Kind: AddrConst, IVal: len(n.Children)}
	dst := g.newTemp()
	hint := TypeInt
	if isFPType(n.Type) {
		hint = TypeDouble
	}
	g.emit(Quad{Op: IRCall, Dst: dst, Src1: nargs, Extra: n.Name, TypeHint: hint})
	return dst
}

func (g *irGen) genBuiltinBitop(n *Node) IRAddr {
	arg := g.genExpr(n.Children[0])
	dst := g.newTemp()
	var op IROpCode
	switch {
	case strings.HasPrefix(n.Name, "__builtin_clz"):
		op = IRCLZ
	case strings.HasPrefix(n.Name, "__builtin_ctz"):
		op = IRCTZ
	default:
		op = IRPopcount
	}
	hint := TypeUnsignedLong
	if n.Name == "__builtin_clz" || n.Name == "__builtin_popcount" || n.Name == "__builtin_ctz" {
		hint = TypeUnsignedInt // 32-bit variant: zero-extend before operating
	}
	g.emit(Quad{Op: op, Dst: dst, Src1: arg, TypeHint: hint})
	return dst
}

// genStructCallInto emits IR for a struct-returning call, writing the result
// directly into dstAddr (the frame address of the destination struct variable).
// The backend dispatches on struct size: sets X8 before BL (>16 bytes) or
// extracts X0/X1 after BL (≤8 or ≤16 bytes).
func (g *irGen) genStructCallInto(n *Node, dstAddr IRAddr, tag string) {
	isVariadic := g.variadicFuncs[n.Name]
	// Phase 1: materialize all args (inner calls complete here, draining pendingParams).
	slots := g.collectCallArgs(n.Children, isVariadic)
	// Phase 2: emit IRParams for THIS call only.
	g.emitCallArgParams(slots)
	nargs := IRAddr{Kind: AddrConst, IVal: len(n.Children)}
	g.emit(Quad{Op: IRCall, TypeHint: TypeStruct, StructTag: tag,
		Src1: nargs, Src2: dstAddr, Extra: n.Name})
}

// materializeStructArg ensures a TypeStruct argument expression is backed by
// caller-owned inline frame storage, and returns its IRAddr.
//
//   KindVar                    → direct AddrLocal of existing isArrBase storage
//   KindCall (struct return)   → materialize via genStructCallInto into a new slot
//   anything else (field accs) → genExpr returns a pointer-valued temp;
//                                copy into a new slot via IRStructCopy
func (g *irGen) materializeStructArg(arg *Node) IRAddr {
	if arg.Kind == KindVar {
		return g.addrOf(arg.Name)
	}
	slot := g.allocStructSlot(arg.StructTag)
	if arg.Kind == KindCall {
		// Struct-returning call: write result directly into slot.
		g.genStructCallInto(arg, slot, arg.StructTag)
	} else {
		// Field access or other expression returning a pointer to struct storage.
		ptrVal := g.genExpr(arg)
		g.emit(Quad{Op: IRStructCopy, Dst: slot, Src1: ptrVal, StructTag: arg.StructTag})
	}
	return slot
}

// alloc128Slot allocates an anonymous 128-bit frame slot (2 × 8-byte) and returns its IRAddr.
func (g *irGen) alloc128Slot() IRAddr {
	name := fmt.Sprintf("#w%d", g.tempN)
	g.tempN++
	g.fn.Locals = append(g.fn.Locals, IRLocal{Name: name, ArrSize: 2, Is128: true, IsArray: true})
	g.locals[name] = localInfo{isArray: true, arrSize: 2}
	return IRAddr{Kind: AddrLocal, Name: name}
}

// widen128 zero-extends or sign-extends a 64-bit value to a 128-bit slot.
// If srcType is already TypeInt128/TypeUint128, addr is returned unchanged.
func (g *irGen) widen128(addr IRAddr, srcType TypeKind, unsigned bool) IRAddr {
	if srcType == TypeInt128 || srcType == TypeUint128 {
		return addr
	}
	slot := g.alloc128Slot()
	op := IR128FromI64
	if unsigned || isUnsignedType(srcType) {
		op = IR128FromU64
	}
	g.emit(Quad{Op: op, Dst: slot, Src1: addr})
	return slot
}

// gen128BinOp emits IR for a binary operation on 128-bit operands.
func (g *irGen) gen128BinOp(n *Node) IRAddr {
	left := g.genExpr(n.Children[0])
	right := g.genExpr(n.Children[1])
	unsigned := n.Type == TypeUint128 ||
		n.Children[0].Type == TypeUint128 || n.Children[1].Type == TypeUint128

	// Widen 64-bit inputs to 128-bit if needed.
	left = g.widen128(left, n.Children[0].Type, unsigned)
	right = g.widen128(right, n.Children[1].Type, unsigned)

	hint := TypeInt128
	if unsigned {
		hint = TypeUint128
	}

	switch n.Op {
	case "==":
		dst := g.newTemp()
		g.emit(Quad{Op: IR128Eq, Dst: dst, Src1: left, Src2: right})
		return dst
	case "!=":
		dst := g.newTemp()
		g.emit(Quad{Op: IR128Ne, Dst: dst, Src1: left, Src2: right})
		return dst
	case "<":
		dst := g.newTemp()
		cmpOp := IR128SLt
		if unsigned {
			cmpOp = IR128ULt
		}
		g.emit(Quad{Op: cmpOp, Dst: dst, Src1: left, Src2: right})
		return dst
	case "<=":
		dst := g.newTemp()
		cmpOp := IR128SLe
		if unsigned {
			cmpOp = IR128ULe
		}
		g.emit(Quad{Op: cmpOp, Dst: dst, Src1: left, Src2: right})
		return dst
	case ">":
		dst := g.newTemp()
		cmpOp := IR128SGt
		if unsigned {
			cmpOp = IR128UGt
		}
		g.emit(Quad{Op: cmpOp, Dst: dst, Src1: left, Src2: right})
		return dst
	case ">=":
		dst := g.newTemp()
		cmpOp := IR128SGe
		if unsigned {
			cmpOp = IR128UGe
		}
		g.emit(Quad{Op: cmpOp, Dst: dst, Src1: left, Src2: right})
		return dst
	}

	dst := g.alloc128Slot()
	var op IROpCode
	switch n.Op {
	case "+":
		op = IR128Add
	case "-":
		op = IR128Sub
	case "*":
		op = IR128Mul
	case "&":
		op = IR128And
	case "|":
		op = IR128Or
	case "^":
		op = IR128Xor
	case "<<":
		// Shift count must be a constant for now.
		shiftVal := g.genExpr(n.Children[1])
		g.emit(Quad{Op: IR128Shl, Dst: dst, Src1: left, Src2: shiftVal, TypeHint: hint})
		return dst
	case ">>":
		shiftVal := g.genExpr(n.Children[1])
		if unsigned {
			g.emit(Quad{Op: IR128LShr, Dst: dst, Src1: left, Src2: shiftVal, TypeHint: hint})
		} else {
			g.emit(Quad{Op: IR128AShr, Dst: dst, Src1: left, Src2: shiftVal, TypeHint: hint})
		}
		return dst
	default:
		op = IR128Add // fallback
	}
	g.emit(Quad{Op: op, Dst: dst, Src1: left, Src2: right, TypeHint: hint})
	return dst
}

// allocStructSlot allocates an anonymous frame slot for a temporary struct value
// and returns its IR address.  Used when a struct-returning call result is needed
// in a context where the final destination is not yet known (e.g. return expr).
func (g *irGen) allocStructSlot(tag string) IRAddr {
	name := fmt.Sprintf("#sr%d", g.tempN)
	g.tempN++
	sz := 1
	if sd, ok := g.prog.StructDefs[tag]; ok {
		sz = (sd.SizeBytes(g.prog.StructDefs) + 7) / 8
	}
	g.fn.Locals = append(g.fn.Locals, IRLocal{
		Name: name, IsStruct: true, StructTag: tag, ArrSize: sz,
	})
	g.locals[name] = localInfo{isArray: true, arrSize: sz}
	return IRAddr{Kind: AddrLocal, Name: name}
}

// genCompoundLit emits IR for a (Type){init_list} compound literal.
// It allocates an anonymous local slot, zero-fills it, applies the init entries,
// and returns the IRAddr of the slot.
// For structs/arrays the return value is a pointer temp (IRAddr of AddrTemp holding address).
// For scalars the return value is the AddrLocal of the slot.
func (g *irGen) genCompoundLit(n *Node) IRAddr {
	name := fmt.Sprintf("#clit%d", g.tempN)
	g.tempN++

	// For &(struct T){...}: n.Type==TypePtr, n.Pointee.Kind==TypeStruct.
	// Treat as a struct compound lit but return the pointer value.
	isPtrToStruct := n.Type == TypePtr && n.Pointee != nil && n.Pointee.Kind == TypeStruct

	isArr := n.Type == TypeIntArray
	isStruct := n.Type == TypeStruct || isPtrToStruct
	isPtr := isPtrType(n.Type) && !isPtrToStruct

	// Determine the effective struct tag and synthDecl for genInitList.
	structTag := n.StructTag
	if isPtrToStruct {
		structTag = n.Pointee.Tag
	}

	sz := 1
	if isStruct {
		if sd, ok := g.prog.StructDefs[structTag]; ok {
			sz = (sd.SizeBytes(g.prog.StructDefs) + 7) / 8
		}
	} else if isArr {
		sz = n.Val
		if sz == 0 {
			sz = 1
		}
	}

	// Register the anonymous local so buildFrame allocates stack space.
	g.fn.Locals = append(g.fn.Locals, IRLocal{
		Name:      name,
		IsArray:   isArr || isStruct,
		IsPtr:     isPtr,
		IsStruct:  isStruct && !isArr,
		StructTag: structTag,
		Pointee:   n.Pointee,
		ArrSize:   sz,
		ElemType:  n.ElemType,
	})
	g.locals[name] = localInfo{isArray: isArr || isStruct, arrSize: sz}

	// Obtain a pointer to the slot via IRGetAddr.
	baseAddr := g.addrOf(name) // AddrLocal
	ptrTmp := g.newTemp()
	g.emit(Quad{Op: IRGetAddr, Dst: ptrTmp, Src1: baseAddr})

	// Build a synthDecl for genInitList (uses struct tag from the inner type).
	synthDecl := n
	if isPtrToStruct {
		synthDecl = &Node{Kind: KindVarDecl, Type: TypeStruct, StructTag: structTag}
	}

	// Zero-fill + apply init entries.
	if len(n.Children) > 0 && n.Children[0].Kind == KindInitList {
		g.genInitList(n.Children[0], ptrTmp, sz*8, synthDecl)
	}

	if isStruct || isArr {
		return ptrTmp
	}
	return baseAddr
}

// genStmtExpr emits IR for a ({ ... }) statement expression.
// It processes the body exactly like genCompound — var decls are registered into the
// function frame, intermediate statements are emitted normally. The last statement
// must be a KindExprStmt; its expression's IRAddr is the return value.
// If the body is empty or ends in a non-expression statement, AddrConst{0} is returned.
//
// Local variables declared inside the statement expression are scoped: they get unique
// mangled names so that nested statement expressions (e.g. min(x, max(y,z))) don't
// clobber each other's locals that share the same source name (like _a, _b).
func (g *irGen) genStmtExpr(n *Node) IRAddr {
	children := n.Children

	// Mangle local var names declared in this statement expression to avoid
	// collisions with identically-named locals in enclosing or sibling scopes.
	// We rename each VarDecl to a unique name and update all references.
	g.stmtExprN++
	suffix := fmt.Sprintf("__se%d", g.stmtExprN)
	renames := make(map[string]string) // original name → mangled name
	for _, child := range children {
		if child.Kind == KindVarDecl {
			orig := child.Name
			mangled := orig + suffix
			renames[orig] = mangled
			child.Name = mangled
		}
	}
	// Rename references in all children (statements and expressions).
	if len(renames) > 0 {
		for _, child := range children {
			renameRefs(child, renames)
		}
	}

	// Find the index of the last non-VarDecl child (the final statement).
	lastStmtIdx := -1
	for i := len(children) - 1; i >= 0; i-- {
		if children[i].Kind != KindVarDecl {
			lastStmtIdx = i
			break
		}
	}

	if lastStmtIdx < 0 {
		// Body is all var decls (or empty) — no value.
		synth := &Node{Kind: KindCompound, Children: children}
		g.genCompound(synth)
		return IRAddr{Kind: AddrConst, IVal: 0}
	}

	// Process everything up to (but not including) the last statement via genCompound.
	// This registers var decls and emits intermediate statements.
	synth := &Node{Kind: KindCompound, Children: children[:lastStmtIdx]}
	g.genCompound(synth)

	// Emit the last statement and capture its value.
	last := children[lastStmtIdx]
	if last.Kind == KindExprStmt && len(last.Children) > 0 {
		return g.genExpr(last.Children[0])
	}
	// Last child is not a value-producing expression statement (e.g. an if/while).
	g.genStmt(last)
	return IRAddr{Kind: AddrConst, IVal: 0}
}

// renameRefs recursively renames all KindVar references matching the renames map.
// It stops at nested KindStmtExpr boundaries because each statement expression
// handles its own renaming independently.
func renameRefs(n *Node, renames map[string]string) {
	if n == nil {
		return
	}
	if n.Kind == KindVar {
		if mangled, ok := renames[n.Name]; ok {
			n.Name = mangled
		}
	}
	for _, child := range n.Children {
		if child != nil && child.Kind == KindStmtExpr {
			continue // nested stmt exprs handle their own renaming
		}
		renameRefs(child, renames)
	}
}

// genFuncPtrCall generates IR for a function-pointer call: (*fp)(args...).
// n.Name is the function-pointer variable; n.Children are the arguments.
func (g *irGen) genFuncPtrCall(n *Node) IRAddr {
	// Load the function pointer value into a temporary.
	fpAddr := g.addrOf(n.Name)
	fpVal := g.newTemp()
	g.emit(Quad{Op: IRCopy, Dst: fpVal, Src1: fpAddr})

	// Phase 1: materialize all args (inner calls complete here).
	slots := g.collectCallArgs(n.Children, false)
	// Phase 2: emit IRParams for THIS call only.
	g.emitCallArgParams(slots)

	nargs := IRAddr{Kind: AddrConst, IVal: len(n.Children)}
	dst := g.newTemp()
	g.emit(Quad{Op: IRFuncPtrCall, Dst: dst, Src1: fpVal, Src2: nargs})
	return dst
}

// genIndirectCall generates IR for an indirect call through an arbitrary expression.
// n.Children[0] is the callee expression; n.Children[1:] are the arguments.
func (g *irGen) genIndirectCall(n *Node) IRAddr {
	if len(n.Children) == 0 {
		dst := g.newTemp()
		return dst
	}
	// Evaluate callee expression to get the function pointer value.
	callee := n.Children[0]
	var fpVal IRAddr
	if callee.Kind == KindFieldAccess {
		// For struct->field or struct.field, load the function pointer value from the field.
		basePtr := g.fieldBasePtr(callee)
		fieldOff := g.fieldByteOffset(callee)
		if fieldOff != 0 {
			offAddr := IRAddr{Kind: AddrConst, IVal: fieldOff}
			tmp := g.newTemp()
			g.emit(Quad{Op: IRAdd, Dst: tmp, Src1: basePtr, Src2: offAddr})
			basePtr = tmp
		}
		fpVal = g.newTemp()
		g.emit(Quad{Op: IRDerefLoad, Dst: fpVal, Src1: basePtr})
	} else {
		fpVal = g.genExpr(callee)
	}

	args := n.Children[1:]
	slots := g.collectCallArgs(args, false)
	g.emitCallArgParams(slots)
	nargs := IRAddr{Kind: AddrConst, IVal: len(args)}
	dst := g.newTemp()
	g.emit(Quad{Op: IRFuncPtrCall, Dst: dst, Src1: fpVal, Src2: nargs})
	return dst
}

// coerceToFP ensures val is an FP IR address; emits IRIntToDouble if needed.
func (g *irGen) coerceToFP(val IRAddr, fromType TypeKind) IRAddr {
	if isFPType(fromType) || val.Kind == AddrFConst {
		return val
	}
	tmp := g.newTemp()
	g.emit(Quad{Op: IRIntToDouble, Dst: tmp, Src1: val})
	return tmp
}

// fpZero returns an IR address for the floating-point constant 0.0.
func (g *irGen) fpZero() IRAddr {
	label := fmt.Sprintf("fc%d", len(g.prog.FConsts))
	g.prog.FConsts = append(g.prog.FConsts, IRFConst{Label: label, Value: 0.0})
	return IRAddr{Kind: AddrFConst, FVal: 0.0, Name: label}
}

// genFPBinOp generates IR for a binary operation involving at least one FP operand.
func (g *irGen) genFPBinOp(n *Node) IRAddr {
	left := g.genExpr(n.Children[0])
	left = g.coerceToFP(left, n.Children[0].Type)
	right := g.genExpr(n.Children[1])
	right = g.coerceToFP(right, n.Children[1].Type)
	dst := g.newTemp()
	op := fpBinOpToIR(n.Op)
	g.emit(Quad{Op: op, Dst: dst, Src1: left, Src2: right})
	return dst
}

// genPtrArith emits IR for pointer ± integer with automatic element-size scaling.
// ptrExpr is the pointer operand, idxExpr is the integer operand.
// pointee is the CType that the pointer points to (used to compute element size).
func (g *irGen) genPtrArith(ptrExpr, idxExpr *Node, pointee *CType, op string) IRAddr {
	ptrVal := g.genExpr(ptrExpr)
	idx := g.genExpr(idxExpr)
	sz := elemSize(pointee)
	if sz > 1 {
		scaled := g.newTemp()
		g.emit(Quad{Op: IRMul, Dst: scaled, Src1: idx, Src2: IRAddr{Kind: AddrConst, IVal: sz}})
		idx = scaled
	}
	dst := g.newTemp()
	irOp := IRAdd
	if op == "-" {
		irOp = IRSub
	}
	g.emit(Quad{Op: irOp, Dst: dst, Src1: ptrVal, Src2: idx})
	return dst
}

// fpBinOpToIR maps a binary operator string to the corresponding FP IR opcode.
func fpBinOpToIR(op string) IROpCode {
	switch op {
	case "+":
		return IRFAdd
	case "-":
		return IRFSub
	case "*":
		return IRFMul
	case "/":
		return IRFDiv
	case "<":
		return IRFLt
	case "<=":
		return IRFLe
	case ">":
		return IRFGt
	case ">=":
		return IRFGe
	case "==":
		return IRFEq
	case "!=":
		return IRFNe
	}
	panic("unknown FP op: " + op)
}

// binOpToIRTyped maps a binary operator to the correct IR opcode,
// choosing signed vs unsigned variants based on the left-operand type.
func binOpToIRTyped(op string, leftType TypeKind) IROpCode {
	u := isUnsignedType(leftType)
	switch op {
	case "+":
		return IRAdd
	case "-":
		return IRSub
	case "*":
		return IRMul
	case "/":
		if u {
			return IRUDiv
		}
		return IRDiv
	case "%":
		if u {
			return IRUMod
		}
		return IRMod
	case "&":
		return IRBitAnd
	case "|":
		return IRBitOr
	case "^":
		return IRBitXor
	case "<<":
		return IRShl
	case ">>":
		if u {
			return IRUShr
		}
		return IRShr
	case "<":
		if u {
			return IRULt
		}
		return IRLt
	case "<=":
		if u {
			return IRULe
		}
		return IRLe
	case ">":
		if u {
			return IRUGt
		}
		return IRGt
	case ">=":
		if u {
			return IRUGe
		}
		return IRGe
	case "==":
		return IREq // sign-independent
	case "!=":
		return IRNe // sign-independent
	}
	panic("unknown op: " + op)
}

func binOpToIR(op string) IROpCode { return binOpToIRTyped(op, TypeInt) }

// elemSize returns the byte stride for pointer arithmetic given the pointee CType.
// ct is Node.Pointee (the type that the pointer points to), not the pointer type itself.
func elemSize(ct *CType) int {
	if ct == nil {
		return 8
	}
	switch ct.Kind {
	case TypeChar, TypeUnsignedChar, TypeVoid:
		return 1
	case TypeFloat:
		return 4
	default: // TypeInt, TypeDouble, TypePtr, TypeStruct, etc. → 8 bytes
		return 8
	}
}
