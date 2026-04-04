package main

import (
	"fmt"
	"strings"
)

// FuncSig holds the type signature of a declared function.
type FuncSig struct {
	Name       string
	ReturnType TypeKind
	Params     []TypeKind
	IsExtern   bool // true for extern function declarations (no body)
	IsVariadic bool // true for variadic functions (last param is "...")
}

// symTable is a two-level symbol table: globals + per-function locals.
type symTable struct {
	globals    map[string]*symEntry
	locals     map[string]*symEntry // nil when not inside a function
	funcs      map[string]*FuncSig
	structDefs map[string]*StructDef // registered struct types
}

type symEntry struct {
	name      string
	typ       TypeKind
	structTag string // for TypeStruct or TypeIntPtr-to-struct: the struct name
	isParam   bool
	isGlobal  bool
	isConst   bool
	constVal  int
}

func newSymTable() *symTable {
	st := &symTable{
		globals:    make(map[string]*symEntry),
		funcs:      make(map[string]*FuncSig),
		structDefs: make(map[string]*StructDef),
	}
	// Built-in functions.
	st.funcs["input"] = &FuncSig{Name: "input", ReturnType: TypeInt}
	st.funcs["output"] = &FuncSig{Name: "output", ReturnType: TypeVoid, Params: []TypeKind{TypeInt}}
	st.funcs["print_char"] = &FuncSig{Name: "print_char", ReturnType: TypeVoid, Params: []TypeKind{TypeInt}}
	st.funcs["print_string"] = &FuncSig{Name: "print_string", ReturnType: TypeVoid, Params: []TypeKind{TypeInt}}
	st.funcs["malloc"] = &FuncSig{Name: "malloc", ReturnType: TypeIntPtr, Params: []TypeKind{TypeInt}}
	st.funcs["free"] = &FuncSig{Name: "free", ReturnType: TypeVoid, Params: []TypeKind{TypeIntPtr}}
	st.funcs["print_double"] = &FuncSig{Name: "print_double", ReturnType: TypeVoid, Params: []TypeKind{TypeDouble}}
	// Compiler built-in: returns a pointer to the variadic register save area.
	st.funcs["__va_start"] = &FuncSig{Name: "__va_start", ReturnType: TypeIntPtr}
	return st
}

func (st *symTable) enterFunc() { st.locals = make(map[string]*symEntry) }
func (st *symTable) leaveFunc() { st.locals = nil }

func (st *symTable) declareGlobal(name string, typ TypeKind, structTag ...string) error {
	if _, ok := st.globals[name]; ok {
		return fmt.Errorf("redeclaration of global '%s'", name)
	}
	e := &symEntry{name: name, typ: typ, isGlobal: true}
	if len(structTag) > 0 {
		e.structTag = structTag[0]
	}
	st.globals[name] = e
	return nil
}

func (st *symTable) declareConst(name string, typ TypeKind, val int, isGlobal bool) error {
	if isGlobal {
		if _, ok := st.globals[name]; ok {
			return fmt.Errorf("redeclaration of global '%s'", name)
		}
		st.globals[name] = &symEntry{name: name, typ: typ, isGlobal: true, isConst: true, constVal: val}
	} else {
		if _, ok := st.locals[name]; ok {
			return fmt.Errorf("redeclaration of '%s'", name)
		}
		st.locals[name] = &symEntry{name: name, typ: typ, isConst: true, constVal: val}
	}
	return nil
}

func (st *symTable) declareLocal(name string, typ TypeKind, isParam bool, structTag ...string) error {
	if _, ok := st.locals[name]; ok {
		return fmt.Errorf("redeclaration of '%s'", name)
	}
	e := &symEntry{name: name, typ: typ, isParam: isParam}
	if len(structTag) > 0 {
		e.structTag = structTag[0]
	}
	st.locals[name] = e
	return nil
}

func (st *symTable) declareFunc(sig *FuncSig) error {
	if existing, ok := st.funcs[sig.Name]; ok {
		// Allow replacing an extern declaration with a full definition.
		if existing.IsExtern && !sig.IsExtern {
			st.funcs[sig.Name] = sig
			return nil
		}
		if !existing.IsExtern {
			return fmt.Errorf("redeclaration of function '%s'", sig.Name)
		}
	}
	st.funcs[sig.Name] = sig
	return nil
}

func (st *symTable) lookup(name string) *symEntry {
	if st.locals != nil {
		if e, ok := st.locals[name]; ok {
			return e
		}
	}
	return st.globals[name]
}

// semCheck type-checks the AST rooted at prog.
// requireMain: when false (library / -c compile), skip the 'main' requirement.
// Returns a multi-line error string if any errors are found.
func semCheck(prog *Node, requireMain bool) error {
	st := newSymTable()
	var errs []string

	for _, decl := range prog.Children {
		switch decl.Kind {
		case KindStructDef:
			sd := buildStructDef(decl, &errs)
			if sd != nil {
				st.structDefs[sd.Name] = sd
			}
		case KindVarDecl:
			if decl.IsExtern {
				if err := st.declareGlobal(decl.Name, decl.Type, decl.StructTag); err != nil {
					errs = append(errs, err.Error())
				}
			} else if decl.IsConst {
				if err := st.declareConst(decl.Name, decl.Type, decl.Val, true); err != nil {
					errs = append(errs, err.Error())
				}
			} else {
				if decl.Type == TypeStruct {
					if _, ok := st.structDefs[decl.StructTag]; !ok {
						errs = append(errs, fmt.Sprintf("undefined struct '%s'", decl.StructTag))
					}
				}
				if err := st.declareGlobal(decl.Name, decl.Type, decl.StructTag); err != nil {
					errs = append(errs, err.Error())
				}
				if len(decl.Children) > 0 {
					if decl.Children[0].Kind != KindNum {
						errs = append(errs, fmt.Sprintf("global '%s': initializer must be a constant", decl.Name))
					} else if decl.Type == TypeIntArray {
						errs = append(errs, fmt.Sprintf("global array '%s' cannot have a scalar initializer", decl.Name))
					}
				}
			}
		case KindFunDecl:
			checkFunDecl(decl, st, &errs)
		}
	}
	if requireMain && st.funcs["main"] == nil {
		errs = append(errs, "no 'main' function defined")
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return nil
}

// buildStructDef constructs a StructDef from a KindStructDef AST node.
func buildStructDef(n *Node, errs *[]string) *StructDef {
	sd := &StructDef{Name: n.Name}
	for i, child := range n.Children {
		sf := StructField{
			Name:       child.Name,
			Type:       child.Type,
			StructTag:  child.StructTag,
			ByteOffset: i * 8,
		}
		sd.Fields = append(sd.Fields, sf)
	}
	return sd
}

func checkFunDecl(n *Node, st *symTable, errs *[]string) {
	if n.IsExtern {
		sig := &FuncSig{Name: n.Name, ReturnType: n.Type, IsExtern: true}
		for _, p := range n.Children {
			if p.Name == "..." {
				sig.IsVariadic = true
				continue
			}
			sig.Params = append(sig.Params, p.Type)
		}
		if err := st.declareFunc(sig); err != nil {
			*errs = append(*errs, err.Error())
		}
		return
	}

	nparams := len(n.Children) - 1 // last child is the body
	sig := &FuncSig{Name: n.Name, ReturnType: n.Type}
	for i := 0; i < nparams; i++ {
		p := n.Children[i]
		if p.Name == "..." {
			sig.IsVariadic = true
			continue
		}
		sig.Params = append(sig.Params, p.Type)
	}
	if err := st.declareFunc(sig); err != nil {
		*errs = append(*errs, err.Error())
		return
	}

	st.enterFunc()
	defer st.leaveFunc()

	for i := 0; i < nparams; i++ {
		p := n.Children[i]
		if p.Name == "..." {
			continue
		}
		if err := st.declareLocal(p.Name, p.Type, true, p.StructTag); err != nil {
			*errs = append(*errs, err.Error())
		}
	}
	body := n.Children[len(n.Children)-1]
	checkCompound(body, st, n, errs)
}

func checkCompound(n *Node, st *symTable, fn *Node, errs *[]string) {
	for _, child := range n.Children {
		switch child.Kind {
		case KindVarDecl:
			if child.IsConst {
				if err := st.declareConst(child.Name, child.Type, child.Val, false); err != nil {
					*errs = append(*errs, err.Error())
				}
			} else {
				if child.Type == TypeStruct {
					if _, ok := st.structDefs[child.StructTag]; !ok {
						*errs = append(*errs, fmt.Sprintf("undefined struct '%s'", child.StructTag))
					}
				}
				if err := st.declareLocal(child.Name, child.Type, false, child.StructTag); err != nil {
					*errs = append(*errs, err.Error())
				}
				if len(child.Children) > 0 {
					// For VLA: Children[0] is the size variable expression.
					// For non-VLA: Children[0] is the initializer expression.
					checkExpr(child.Children[0], st, errs)
				}
			}
		default:
			checkStmt(child, st, fn, errs)
		}
	}
}

func checkStmt(n *Node, st *symTable, fn *Node, errs *[]string) {
	if n == nil {
		return
	}
	switch n.Kind {
	case KindExprStmt:
		if len(n.Children) > 0 {
			checkExpr(n.Children[0], st, errs)
		}
	case KindCompound:
		checkCompound(n, st, fn, errs)
	case KindSelection:
		checkExpr(n.Children[0], st, errs)
		checkStmt(n.Children[1], st, fn, errs)
		if len(n.Children) > 2 {
			checkStmt(n.Children[2], st, fn, errs)
		}
	case KindIteration:
		checkExpr(n.Children[0], st, errs)
		checkStmt(n.Children[1], st, fn, errs)
	case KindFor:
		if n.Children[0] != nil {
			checkExpr(n.Children[0], st, errs)
		}
		if n.Children[1] != nil {
			checkExpr(n.Children[1], st, errs)
		}
		if n.Children[2] != nil {
			checkExpr(n.Children[2], st, errs)
		}
		checkStmt(n.Children[3], st, fn, errs)
	case KindDoWhile:
		checkStmt(n.Children[0], st, fn, errs)
		checkExpr(n.Children[1], st, errs)
	case KindBreak, KindContinue:
		// valid anywhere inside a loop; runtime check in irgen
	case KindGoto:
		// target label is function-scoped; validated at assembly time
	case KindLabel:
		checkStmt(n.Children[0], st, fn, errs)
	case KindReturn:
		if fn.Type == TypeVoid && len(n.Children) > 0 {
			*errs = append(*errs, fmt.Sprintf("void function '%s' cannot return a value", fn.Name))
		}
		if fn.Type != TypeVoid && len(n.Children) == 0 {
			*errs = append(*errs, fmt.Sprintf("non-void function '%s' missing return value", fn.Name))
		}
		if len(n.Children) > 0 {
			checkExpr(n.Children[0], st, errs)
		}
	}
}

func checkExpr(n *Node, st *symTable, errs *[]string) TypeKind {
	if n == nil {
		return TypeVoid
	}
	switch n.Kind {
	case KindNum:
		n.Type = TypeInt
		return TypeInt

	case KindFNum:
		n.Type = TypeDouble
		return TypeDouble

	case KindCharLit:
		n.Type = TypeInt // char literals are int-valued
		return TypeInt

	case KindStrLit:
		n.Type = TypeCharPtr
		return TypeCharPtr

	case KindAddrOf:
		t := checkExpr(n.Children[0], st, errs)
		switch t {
		case TypeChar:
			n.Type = TypeCharPtr
		default:
			n.Type = TypeIntPtr
		}
		return n.Type

	case KindDeref:
		t := checkExpr(n.Children[0], st, errs)
		switch t {
		case TypeCharPtr:
			n.Type = TypeChar
		default:
			n.Type = TypeInt
		}
		return n.Type

	case KindVar:
		e := st.lookup(n.Name)
		if e == nil {
			*errs = append(*errs, fmt.Sprintf("undefined variable '%s'", n.Name))
			n.Type = TypeInt
			return TypeInt
		}
		if e.isConst {
			n.Kind = KindNum
			n.Val = e.constVal
			n.Type = TypeInt
			return TypeInt
		}
		n.Type = e.typ
		n.StructTag = e.structTag
		return e.typ

	case KindArrayVar:
		e := st.lookup(n.Name)
		if e == nil {
			*errs = append(*errs, fmt.Sprintf("undefined variable '%s'", n.Name))
			n.Type = TypeInt
			return TypeInt
		}
		if e.typ != TypeIntArray && e.typ != TypeIntPtr && e.typ != TypeCharPtr {
			*errs = append(*errs, fmt.Sprintf("'%s' is not an array or pointer", n.Name))
		}
		checkExpr(n.Children[0], st, errs)
		if e.typ == TypeCharPtr {
			n.Type = TypeChar
			return TypeChar
		}
		n.Type = TypeInt
		return TypeInt

	case KindAssign:
		t := checkExpr(n.Children[1], st, errs)
		checkExpr(n.Children[0], st, errs) // lhs: KindVar, KindArrayVar, or KindDeref
		n.Type = t
		return t

	case KindCompoundAssign:
		lt := checkExpr(n.Children[0], st, errs)
		if n.Children[1] != nil {
			checkExpr(n.Children[1], st, errs)
		}
		n.Type = lt // result type matches lhs (e.g. unsigned int x += y stays unsigned)
		return lt

	case KindBinOp:
		lt := checkExpr(n.Children[0], st, errs)
		rt := checkExpr(n.Children[1], st, errs)
		// Comparison operators always yield int (0 or 1); the operand types
		// determine signed vs unsigned comparison in irgen via Children[0].Type.
		switch n.Op {
		case "<", "<=", ">", ">=", "==", "!=":
			n.Type = TypeInt
		default:
			// FP "infects" — if either operand is FP, result is double.
			if isFPType(lt) || isFPType(rt) {
				n.Type = TypeDouble
			} else if isUnsignedType(lt) || isUnsignedType(rt) {
				// Arithmetic: unsigned "infects" — if either operand is unsigned, result is unsigned.
				n.Type = TypeUnsignedInt
			} else {
				n.Type = TypeInt
			}
		}
		return n.Type

	case KindUnary:
		t := checkExpr(n.Children[0], st, errs)
		switch n.Op {
		case "!":
			n.Type = TypeInt // logical-not always yields int
		case "~":
			n.Type = t // preserve type
		default: // "-"
			if isFPType(t) {
				n.Type = TypeDouble
			} else {
				n.Type = t // preserve signedness
			}
		}
		return n.Type

	case KindFieldAccess:
		checkExpr(n.Children[0], st, errs)
		baseTag := n.Children[0].StructTag
		if baseTag == "" {
			*errs = append(*errs, fmt.Sprintf("field access on non-struct expression"))
			n.Type = TypeInt
			return TypeInt
		}
		sd := st.structDefs[baseTag]
		if sd == nil {
			*errs = append(*errs, fmt.Sprintf("unknown struct '%s'", baseTag))
			n.Type = TypeInt
			return TypeInt
		}
		f := sd.FindField(n.Name)
		if f == nil {
			*errs = append(*errs, fmt.Sprintf("struct '%s' has no field '%s'", baseTag, n.Name))
			n.Type = TypeInt
			return TypeInt
		}
		n.Type = f.Type
		n.StructTag = f.StructTag // propagate inner struct tag (for chained access)
		return n.Type

	case KindCall:
		sig := st.funcs[n.Name]
		if sig == nil {
			*errs = append(*errs, fmt.Sprintf("undefined function '%s'", n.Name))
			n.Type = TypeInt
			return TypeInt
		}
		// Variadic and built-in functions skip strict arity check.
		if !sig.IsVariadic && n.Name != "input" && n.Name != "output" &&
			n.Name != "print_char" && n.Name != "print_string" && n.Name != "print_double" {
			if len(n.Children) < len(sig.Params) {
				*errs = append(*errs, fmt.Sprintf("'%s' expects %d args, got %d",
					n.Name, len(sig.Params), len(n.Children)))
			}
		}
		for _, arg := range n.Children {
			checkExpr(arg, st, errs)
		}
		n.Type = sig.ReturnType
		return sig.ReturnType
	}
	return TypeVoid
}
