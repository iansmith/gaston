package main

import "fmt"

// loopLabels holds the break/continue targets for one loop level.
type loopLabels struct {
	breakLabel    string
	continueLabel string
}

// irGen holds state for IR generation from a type-checked AST.
type irGen struct {
	prog      *IRProgram
	fn        *IRFunc   // current function being generated
	globals   map[string]*IRGlobal
	locals    map[string]localInfo
	tempN     int
	labelN    int
	loopStack []loopLabels
}

// localInfo tracks what we know about a local name during IR gen.
type localInfo struct {
	isArray bool
	isParam bool   // if true, the stack slot holds a pointer, not inline storage
	arrSize int    // >0 for local (non-param) arrays; -1 for array params
}

func newIRGen() *irGen {
	return &irGen{
		prog:    &IRProgram{},
		globals: make(map[string]*IRGlobal),
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
	if _, ok := g.locals[name]; ok {
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

	// First pass: register global variable declarations.
	for _, decl := range prog.Children {
		if decl.Kind == KindVarDecl && !decl.IsConst {
			isArr := decl.Type == TypeIntArray
			isPtr := decl.Type == TypeIntPtr || decl.Type == TypeCharPtr
			sz := 1
			if isArr {
				sz = decl.Val
			}
			gbl := &IRGlobal{
				Name:     decl.Name,
				IsArr:    isArr,
				IsPtr:    isPtr,
				IsExtern: decl.IsExtern,
				Size:     sz,
			}
			if !decl.IsExtern && len(decl.Children) > 0 && decl.Children[0].Kind == KindNum {
				gbl.HasInitVal = true
				gbl.InitVal = decl.Children[0].Val
			}
			g.prog.Globals = append(g.prog.Globals, *gbl)
			g.globals[decl.Name] = gbl
		}
	}

	// Second pass: generate IR for each function.
	for _, decl := range prog.Children {
		if decl.Kind == KindFunDecl && !decl.IsExtern {
			g.genFunc(decl)
		}
	}
	return g.prog
}

func (g *irGen) genFunc(n *Node) {
	g.fn = &IRFunc{Name: n.Name}
	g.tempN = 0
	g.labelN = 0
	g.locals = make(map[string]localInfo)

	nparams := len(n.Children) - 1
	for i := 0; i < nparams; i++ {
		p := n.Children[i]
		isArr := p.Type == TypeIntArray
		g.locals[p.Name] = localInfo{isArray: isArr, isParam: true, arrSize: -1}
		g.fn.Params = append(g.fn.Params, p.Name)
		g.fn.ParamType = append(g.fn.ParamType, p.Type)
	}

	g.emit(Quad{Op: IREnter, Extra: n.Name,
		Src1: IRAddr{Kind: AddrConst, IVal: nparams}})

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
			isArr := child.Type == TypeIntArray
			isPtr := child.Type == TypeIntPtr || child.Type == TypeCharPtr
			sz := 1
			if isArr {
				sz = child.Val
			}
			g.locals[child.Name] = localInfo{isArray: isArr, arrSize: sz}
			g.fn.Locals = append(g.fn.Locals, IRLocal{
				Name:    child.Name,
				IsArray: isArr,
				IsPtr:   isPtr,
				ArrSize: sz,
			})
			if len(child.Children) > 0 && !isArr {
				initVal := g.genExpr(child.Children[0])
				dst := g.addrOf(child.Name)
				g.emit(Quad{Op: IRCopy, Dst: dst, Src1: initVal})
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
	case KindReturn:
		if len(n.Children) > 0 {
			v := g.genExpr(n.Children[0])
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

	case KindStrLit:
		label := fmt.Sprintf("str%d", len(g.prog.StrLits))
		g.prog.StrLits = append(g.prog.StrLits, IRStrLit{Label: label, Content: n.Name})
		dst := g.newTemp()
		g.emit(Quad{Op: IRStrAddr, Dst: dst, Extra: label})
		return dst

	case KindAddrOf:
		// &var → get the address of the variable
		varNode := n.Children[0]
		src := g.addrOf(varNode.Name)
		dst := g.newTemp()
		g.emit(Quad{Op: IRGetAddr, Dst: dst, Src1: src})
		return dst

	case KindDeref:
		// *ptr → load from the pointer value
		ptr := g.genExpr(n.Children[0])
		dst := g.newTemp()
		g.emit(Quad{Op: IRDerefLoad, Dst: dst, Src1: ptr})
		return dst

	case KindVar:
		return g.addrOf(n.Name)

	case KindArrayVar:
		base := g.addrOf(n.Name)
		idx := g.genExpr(n.Children[0])
		dst := g.newTemp()
		g.emit(Quad{Op: IRLoad, Dst: dst, Src1: base, Src2: idx})
		return dst

	case KindAssign:
		lhs := n.Children[0]
		rhs := g.genExpr(n.Children[1])

		switch lhs.Kind {
		case KindVar:
			addr := g.addrOf(lhs.Name)
			g.emit(Quad{Op: IRCopy, Dst: addr, Src1: rhs})
			return addr
		case KindArrayVar:
			base := g.addrOf(lhs.Name)
			idx := g.genExpr(lhs.Children[0])
			g.emit(Quad{Op: IRStore, Dst: base, Src1: idx, Src2: rhs})
			return rhs
		case KindDeref:
			// *ptr = rhs
			ptr := g.genExpr(lhs.Children[0])
			g.emit(Quad{Op: IRDerefStore, Dst: ptr, Src1: rhs})
			return rhs
		}

	case KindCompoundAssign:
		// Desugar: lhs op= rhs  →  lhs = lhs op rhs
		lhs := n.Children[0]
		var rhsAddr IRAddr
		if n.Children[1] != nil {
			rhsAddr = g.genExpr(n.Children[1])
		} else {
			rhsAddr = IRAddr{Kind: AddrConst, IVal: n.Val}
		}
		irOp := binOpToIRTyped(n.Op, n.Children[0].Type)
		tmp := g.newTemp()

		switch lhs.Kind {
		case KindVar:
			lhsAddr := g.addrOf(lhs.Name)
			g.emit(Quad{Op: irOp, Dst: tmp, Src1: lhsAddr, Src2: rhsAddr})
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


	case KindBinOp:
		left := g.genExpr(n.Children[0])
		right := g.genExpr(n.Children[1])
		dst := g.newTemp()
		op := binOpToIRTyped(n.Op, n.Children[0].Type)
		g.emit(Quad{Op: op, Dst: dst, Src1: left, Src2: right})
		return dst

	case KindUnary:
		operand := g.genExpr(n.Children[0])
		dst := g.newTemp()
		zero := IRAddr{Kind: AddrConst, IVal: 0}
		switch n.Op {
		case "-": // 0 - operand
			g.emit(Quad{Op: IRSub, Dst: dst, Src1: zero, Src2: operand})
		case "!": // operand == 0
			g.emit(Quad{Op: IREq, Dst: dst, Src1: operand, Src2: zero})
		case "~": // bitwise NOT
			g.emit(Quad{Op: IRBitNot, Dst: dst, Src1: operand})
		}
		return dst

	case KindCall:
		return g.genCall(n)
	}
	return IRAddr{Kind: AddrNone}
}

func (g *irGen) genCall(n *Node) IRAddr {
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
		g.emit(Quad{Op: IRParam, Src1: val})
	}

	nargs := IRAddr{Kind: AddrConst, IVal: len(n.Children)}
	dst := g.newTemp()
	g.emit(Quad{Op: IRCall, Dst: dst, Src1: nargs, Extra: n.Name})
	return dst
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
