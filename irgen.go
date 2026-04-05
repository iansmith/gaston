package main

import "fmt"

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
	funcNames       map[string]bool // names of user-defined functions (for IRFuncAddr detection)
	variadicFuncs   map[string]bool // names of variadic functions (for FP bitcast at call sites)
	tempN           int
	labelN          int
	loopStack       []loopLabels
	currentFuncName string
	staticLocals    map[string]string // local name → mangled global name
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
		prog:          &IRProgram{StructDefs: make(map[string]*StructDef)},
		globals:       make(map[string]*IRGlobal),
		funcNames:     make(map[string]bool),
		variadicFuncs: make(map[string]bool),
	}
}

func (g *irGen) newTemp() IRAddr {
	t := IRAddr{Kind: AddrTemp, Name: fmt.Sprintf("t%d", g.tempN)}
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
			if !decl.IsExtern && len(decl.Children) > 0 && decl.Children[0].Kind == KindNum {
				gbl.HasInitVal = true
				gbl.InitVal = decl.Children[0].Val
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
		if decl.Kind == KindFunDecl && !decl.IsExtern {
			g.funcNames[decl.Name] = true
			// A function is variadic if any of its param children is "...".
			for _, p := range decl.Children {
				if p.Kind == KindParam && p.Name == "..." {
					g.variadicFuncs[decl.Name] = true
					break
				}
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
			sz, align := fieldSizeAlign(child.Type, child.StructTag, structDefs)
			if !sd.IsUnion {
				offset = (offset + align - 1) &^ (align - 1)
			}
			sd.Fields = append(sd.Fields, StructField{
				Name:        child.Name,
				Type:        child.Type,
				Pointee:     child.Pointee,
				ElemType:    child.ElemType,
				ElemPointee: child.ElemPointee,
				StructTag:   child.StructTag,
				ByteOffset:  offset,
				IsFlexArray: isFlexArr,
			})
			if !sd.IsUnion && !isFlexArr {
				offset += sz
			}
		}
	}
	return sd
}

func (g *irGen) genFunc(n *Node) {
	g.fn = &IRFunc{Name: n.Name, ReturnType: n.Type, ReturnPointee: n.Pointee}
	g.tempN = 0
	g.labelN = 0
	g.locals = make(map[string]localInfo)
	g.currentFuncName = n.Name
	g.staticLocals = make(map[string]string)

	nparams := len(n.Children) - 1
	realParams := 0
	for i := 0; i < nparams; i++ {
		p := n.Children[i]
		if p.Name == "..." {
			g.fn.IsVariadic = true
			continue
		}
		isArr := p.Type == TypeIntArray
		g.locals[p.Name] = localInfo{isArray: isArr, isParam: true, arrSize: -1}
		g.fn.Params = append(g.fn.Params, p.Name)
		g.fn.ParamType = append(g.fn.ParamType, p.Type)
		g.fn.ParamPointee = append(g.fn.ParamPointee, p.Pointee)
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
				})
				if len(child.Children) > 0 && !isArr && !isStruct {
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
		} else {
			g.genStmt(child)
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

	// init
	if n.Children[0] != nil {
		g.genExpr(n.Children[0])
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

	// post
	g.emitLabel(postLabel)
	if n.Children[2] != nil {
		g.genExpr(n.Children[2])
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
		// &var → get the storage address of the variable (never loads through a pointer slot)
		varNode := n.Children[0]
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
			g.emit(Quad{Op: IRDerefLoad, Dst: dst, Src1: ptr})
		}
		return dst

	case KindVar:
		// If the name is a function (not a variable), emit IRFuncAddr to get its address.
		if g.funcNames[n.Name] {
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
		rhs := g.genExpr(n.Children[1])
		lhsType := n.Children[0].Type
		rhsType := n.Children[1].Type

		if isFPType(lhsType) {
			rhs = g.coerceToFP(rhs, rhsType)
		}

		switch lhs.Kind {
		case KindVar:
			addr := g.addrOf(lhs.Name)
			if isFPType(lhsType) {
				g.emit(Quad{Op: IRFCopy, Dst: addr, Src1: rhs})
			} else if isFPType(rhsType) {
				tmp := g.newTemp()
				g.emit(Quad{Op: IRDoubleToInt, Dst: tmp, Src1: rhs})
				g.emit(Quad{Op: IRCopy, Dst: addr, Src1: tmp})
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
				g.emit(Quad{Op: IRDerefStore, Dst: ptr, Src1: tmp})
			} else {
				g.emit(Quad{Op: IRDerefStore, Dst: ptr, Src1: rhs})
			}
			return rhs
		case KindFieldAccess:
			ptr := g.fieldBasePtr(lhs)
			offset := g.fieldByteOffset(lhs)
			offAddr := IRAddr{Kind: AddrConst, IVal: offset}
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

		// Write new pointer value back into the ap local variable.
		apAddr := g.addrOf(n.Children[0].Name)
		g.emit(Quad{Op: IRCopy, Dst: apAddr, Src1: newAp})

		return result
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
func (g *irGen) lookupStructField(n *Node) *StructField {
	structTag := n.Children[0].StructTag
	if structTag == "" {
		return nil
	}
	sd := g.prog.StructDefs[structTag]
	if sd == nil {
		return nil
	}
	return sd.FindField(n.Name)
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

	case KindDeref:
		ptrVal := g.genExpr(lval.Children[0])
		old = g.newTemp()
		g.emit(Quad{Op: IRDerefLoad, Dst: old, Src1: ptrVal})
		new_ = g.newTemp()
		g.emit(Quad{Op: irOp, Dst: new_, Src1: old, Src2: step})
		g.emit(Quad{Op: IRDerefStore, Dst: ptrVal, Src1: new_})

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

func (g *irGen) genCall(n *Node) IRAddr {
	isVariadic := g.variadicFuncs[n.Name]
	for _, arg := range n.Children {
		var val IRAddr
		// Passing an array by name → emit IRAddr to get base pointer.
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
				// FMOV Xd, Dn (no value conversion — bit-for-bit copy).
				intBits := g.newTemp()
				g.emit(Quad{Op: IRFBitcastFI, Dst: intBits, Src1: val})
				g.emit(Quad{Op: IRParam, Src1: intBits})
			} else {
				g.emit(Quad{Op: IRFParam, Src1: val})
			}
		} else {
			g.emit(Quad{Op: IRParam, Src1: val})
		}
	}

	nargs := IRAddr{Kind: AddrConst, IVal: len(n.Children)}
	dst := g.newTemp()
	g.emit(Quad{Op: IRCall, Dst: dst, Src1: nargs, Extra: n.Name})
	return dst
}

// genFuncPtrCall generates IR for a function-pointer call: (*fp)(args...).
// n.Name is the function-pointer variable; n.Children are the arguments.
func (g *irGen) genFuncPtrCall(n *Node) IRAddr {
	// Load the function pointer value into a temporary.
	fpAddr := g.addrOf(n.Name)
	fpVal := g.newTemp()
	g.emit(Quad{Op: IRCopy, Dst: fpVal, Src1: fpAddr})

	// Push arguments exactly as in genCall.
	for _, arg := range n.Children {
		var val IRAddr
		if arg.Kind == KindVar && g.isArrayName(arg.Name) {
			tmp := g.newTemp()
			g.emit(Quad{Op: IRGetAddr, Dst: tmp, Src1: g.addrOf(arg.Name)})
			val = tmp
		} else {
			val = g.genExpr(arg)
		}
		if isFPType(arg.Type) {
			g.emit(Quad{Op: IRFParam, Src1: val})
		} else {
			g.emit(Quad{Op: IRParam, Src1: val})
		}
	}

	nargs := IRAddr{Kind: AddrConst, IVal: len(n.Children)}
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
