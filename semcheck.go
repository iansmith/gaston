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
}

// symTable is a two-level symbol table: globals + per-function locals.
type symTable struct {
	globals map[string]*symEntry
	locals  map[string]*symEntry // nil when not inside a function
	funcs   map[string]*FuncSig
}

type symEntry struct {
	name     string
	typ      TypeKind
	isParam  bool
	isGlobal bool
}

func newSymTable() *symTable {
	st := &symTable{
		globals: make(map[string]*symEntry),
		funcs:   make(map[string]*FuncSig),
	}
	// Built-in functions.
	st.funcs["input"] = &FuncSig{Name: "input", ReturnType: TypeInt}
	st.funcs["output"] = &FuncSig{Name: "output", ReturnType: TypeVoid, Params: []TypeKind{TypeInt}}
	return st
}

func (st *symTable) enterFunc() { st.locals = make(map[string]*symEntry) }
func (st *symTable) leaveFunc() { st.locals = nil }

func (st *symTable) declareGlobal(name string, typ TypeKind) error {
	if _, ok := st.globals[name]; ok {
		return fmt.Errorf("redeclaration of global '%s'", name)
	}
	st.globals[name] = &symEntry{name: name, typ: typ, isGlobal: true}
	return nil
}

func (st *symTable) declareLocal(name string, typ TypeKind, isParam bool) error {
	if _, ok := st.locals[name]; ok {
		return fmt.Errorf("redeclaration of '%s'", name)
	}
	st.locals[name] = &symEntry{name: name, typ: typ, isParam: isParam}
	return nil
}

func (st *symTable) declareFunc(sig *FuncSig) error {
	if _, ok := st.funcs[sig.Name]; ok {
		return fmt.Errorf("redeclaration of function '%s'", sig.Name)
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
// Returns a multi-line error string if any errors are found.
func semCheck(prog *Node) error {
	st := newSymTable()
	var errs []string

	for _, decl := range prog.Children {
		switch decl.Kind {
		case KindVarDecl:
			if err := st.declareGlobal(decl.Name, decl.Type); err != nil {
				errs = append(errs, err.Error())
			}
			if len(decl.Children) > 0 {
				if decl.Children[0].Kind != KindNum {
					errs = append(errs, fmt.Sprintf("global '%s': initializer must be a constant", decl.Name))
				} else if decl.Type == TypeIntArray {
					errs = append(errs, fmt.Sprintf("global array '%s' cannot have a scalar initializer", decl.Name))
				}
			}
		case KindFunDecl:
			checkFunDecl(decl, st, &errs)
		}
	}
	if st.funcs["main"] == nil {
		errs = append(errs, "no 'main' function defined")
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return nil
}

func checkFunDecl(n *Node, st *symTable, errs *[]string) {
	nparams := len(n.Children) - 1 // last child is the body
	sig := &FuncSig{Name: n.Name, ReturnType: n.Type}
	for i := 0; i < nparams; i++ {
		sig.Params = append(sig.Params, n.Children[i].Type)
	}
	if err := st.declareFunc(sig); err != nil {
		*errs = append(*errs, err.Error())
		return
	}

	st.enterFunc()
	defer st.leaveFunc()

	for i := 0; i < nparams; i++ {
		p := n.Children[i]
		if err := st.declareLocal(p.Name, p.Type, true); err != nil {
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
			if err := st.declareLocal(child.Name, child.Type, false); err != nil {
				*errs = append(*errs, err.Error())
			}
			if len(child.Children) > 0 {
				checkExpr(child.Children[0], st, errs)
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

	case KindVar:
		e := st.lookup(n.Name)
		if e == nil {
			*errs = append(*errs, fmt.Sprintf("undefined variable '%s'", n.Name))
			n.Type = TypeInt
			return TypeInt
		}
		n.Type = e.typ
		return e.typ

	case KindArrayVar:
		e := st.lookup(n.Name)
		if e == nil {
			*errs = append(*errs, fmt.Sprintf("undefined variable '%s'", n.Name))
			n.Type = TypeInt
			return TypeInt
		}
		if e.typ != TypeIntArray {
			*errs = append(*errs, fmt.Sprintf("'%s' is not an array", n.Name))
		}
		checkExpr(n.Children[0], st, errs)
		n.Type = TypeInt
		return TypeInt

	case KindAssign:
		t := checkExpr(n.Children[1], st, errs)
		checkExpr(n.Children[0], st, errs)
		n.Type = t
		return t

	case KindCompoundAssign:
		checkExpr(n.Children[0], st, errs)
		if n.Children[1] != nil {
			checkExpr(n.Children[1], st, errs)
		}
		n.Type = TypeInt
		return TypeInt

	case KindBinOp:
		checkExpr(n.Children[0], st, errs)
		checkExpr(n.Children[1], st, errs)
		n.Type = TypeInt
		return TypeInt

	case KindUnary:
		checkExpr(n.Children[0], st, errs)
		n.Type = TypeInt
		return TypeInt

	case KindCall:
		sig := st.funcs[n.Name]
		if sig == nil {
			*errs = append(*errs, fmt.Sprintf("undefined function '%s'", n.Name))
			n.Type = TypeInt
			return TypeInt
		}
		// Variadic built-ins skip arity check.
		if n.Name != "input" && n.Name != "output" {
			if len(n.Children) != len(sig.Params) {
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
