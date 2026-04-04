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
	name           string
	typ            TypeKind
	structTag      string // for TypeStruct or TypeIntPtr-to-struct: the struct name
	isParam        bool
	isGlobal       bool
	isConst        bool
	constVal       int
	arrSize        int      // for TypeIntArray locals/globals: number of elements (0 for params, which decay to pointer)
	innerDim       int      // for 2D arrays: inner dimension (columns)
	elemType       TypeKind // for TypeIntArray: element type (e.g. TypeDouble); 0/TypeInt if int array
	isConstTarget  bool     // true for const T *p — cannot store through the pointer
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
	st.funcs["malloc"] = &FuncSig{Name: "malloc", ReturnType: TypeVoidPtr, Params: []TypeKind{TypeInt}}
	st.funcs["free"] = &FuncSig{Name: "free", ReturnType: TypeVoid, Params: []TypeKind{TypeVoidPtr}}
	st.funcs["print_double"] = &FuncSig{Name: "print_double", ReturnType: TypeVoid, Params: []TypeKind{TypeDouble}}
	// Compiler built-in: returns a pointer to the variadic register save area.
	st.funcs["__va_start"] = &FuncSig{Name: "__va_start", ReturnType: TypeIntPtr}
	return st
}

// sizeofKind returns the byte size for the given TypeKind.
// All pointer types and int/long/double occupy 8 bytes; char is 1; short is 2; float is 4.
// For TypeStruct, structTag and structDefs are used to sum the field sizes.
func sizeofKind(t TypeKind, structTag string, structDefs map[string]*StructDef) int {
	switch t {
	case TypeChar, TypeUnsignedChar:
		return 1
	case TypeShort, TypeUnsignedShort:
		return 2
	case TypeFloat:
		return 4
	case TypeStruct:
		if structTag != "" {
			if sd, ok := structDefs[structTag]; ok {
				return sd.SizeBytes(structDefs)
			}
		}
		return 8
	default:
		// TypeInt, TypeUnsignedInt, TypeDouble, pointers, TypeFuncPtr, TypeIntArray, TypeVoid
		return 8
	}
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

func (st *symTable) declareGlobalFull(n *Node) error {
	if _, ok := st.globals[n.Name]; ok {
		return fmt.Errorf("redeclaration of global '%s'", n.Name)
	}
	e := &symEntry{name: n.Name, typ: n.Type, isGlobal: true,
		structTag: n.StructTag, isConstTarget: n.IsConstTarget}
	st.globals[n.Name] = e
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

func (st *symTable) declareLocalFull(n *Node, isParam bool) error {
	if _, ok := st.locals[n.Name]; ok {
		return fmt.Errorf("redeclaration of '%s'", n.Name)
	}
	e := &symEntry{name: n.Name, typ: n.Type, isParam: isParam,
		structTag: n.StructTag, isConstTarget: n.IsConstTarget}
	st.locals[n.Name] = e
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
			sd := buildStructDef(decl, &errs, st.structDefs)
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
				if err := st.declareGlobalFull(decl); err != nil {
					errs = append(errs, err.Error())
				} else if decl.Type == TypeIntArray {
					st.globals[decl.Name].arrSize = decl.Val
					st.globals[decl.Name].elemType = decl.ElemType
					st.globals[decl.Name].innerDim = decl.Dim2
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
// Field byte offsets follow the System V ARM64 ABI natural-alignment rule:
// each field is placed at the smallest offset satisfying its alignment.
// Bit-fields are packed into 8-byte storage words.
// Flex arrays (Val==-1) are last and have no storage size.
// structDefs is used to look up sizes of nested struct fields.
func buildStructDef(n *Node, errs *[]string, structDefs map[string]*StructDef) *StructDef {
	sd := &StructDef{Name: n.Name, IsUnion: n.IsUnion}
	offset := 0
	bfWordOffset := -1  // byte offset of current bit-field storage word (-1 = none active)
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
			// Close out any open bit-field word.
			if bfWordOffset != -1 && !sd.IsUnion {
				offset = bfWordOffset + bfWordSize
				bfWordOffset = -1
				bfBitsUsed = 0
			}
			sz, align := fieldSizeAlign(child.Type, child.StructTag, structDefs)
			if !sd.IsUnion {
				// Struct: advance offset with natural alignment.
				offset = (offset + align - 1) &^ (align - 1)
			}
			// Union: all fields start at offset 0 (offset stays 0 throughout).
			sd.Fields = append(sd.Fields, StructField{
				Name:        child.Name,
				Type:        child.Type,
				ElemType:    child.ElemType,
				StructTag:   child.StructTag,
				ByteOffset:  offset,
				IsFlexArray: isFlexArr,
			})
			if !sd.IsUnion && !isFlexArr {
				offset += sz
			}
		}
	}
	// Close out trailing bit-field word.
	if bfWordOffset != -1 && !sd.IsUnion {
		// offset will be computed by SizeBytes via the field's ByteOffset+8
		_ = bfWordOffset
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
			} else if child.IsStatic {
				// Static local: treat as global for type-checking purposes.
				// Register in globals so lookups work; irgen will handle allocation.
				e := &symEntry{name: child.Name, typ: child.Type, isGlobal: true,
					structTag: child.StructTag, isConstTarget: child.IsConstTarget}
				if child.Type == TypeIntArray {
					e.arrSize = child.Val
					e.elemType = child.ElemType
					e.innerDim = child.Dim2
				}
				// Add to locals (shadows any global of same name within this function).
				if st.locals != nil {
					st.locals[child.Name] = e
				}
				if len(child.Children) > 0 {
					checkExpr(child.Children[0], st, errs)
				}
			} else {
				if child.Type == TypeStruct {
					if _, ok := st.structDefs[child.StructTag]; !ok {
						*errs = append(*errs, fmt.Sprintf("undefined struct '%s'", child.StructTag))
					}
				}
				if err := st.declareLocalFull(child, false); err != nil {
					*errs = append(*errs, err.Error())
				} else if child.Type == TypeIntArray {
					st.locals[child.Name].arrSize = child.Val
					st.locals[child.Name].elemType = child.ElemType
					st.locals[child.Name].innerDim = child.Dim2
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
		case TypeCharPtr:
			n.Type = TypeCharPtrPtr
		case TypeIntPtr, TypeVoidPtr:
			n.Type = TypeIntPtrPtr
		case TypeFloat:
			n.Type = TypeFloatPtr
		case TypeDouble:
			n.Type = TypeDoublePtr
		case TypeFloatPtr:
			n.Type = TypeFloatPtrPtr
		case TypeDoublePtr:
			n.Type = TypeDoublePtrPtr
		case TypeIntArray:
			// Array-to-pointer decay: use element type to produce a typed pointer.
			et := TypeInt
			if child := n.Children[0]; child.Kind == KindVar {
				if e := st.lookup(child.Name); e != nil && e.elemType != 0 {
					et = e.elemType
				}
			}
			n.Type = ptrType(et)
		default:
			n.Type = TypeIntPtr
		}
		return n.Type

	case KindDeref:
		t := checkExpr(n.Children[0], st, errs)
		if t == TypeVoidPtr {
			*errs = append(*errs, "cannot dereference void pointer")
			n.Type = TypeInt
			return TypeInt
		}
		n.Type = derefPtrType(t)
		return n.Type

	case KindVar:
		e := st.lookup(n.Name)
		if e == nil {
			// Not a variable — check if it's a function name used as a value (func ptr assignment).
			if sig, ok := st.funcs[n.Name]; ok {
				_ = sig
				n.Type = TypeFuncPtr
				return TypeFuncPtr
			}
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
		if e.typ != TypeIntArray && !isPtrType(e.typ) {
			*errs = append(*errs, fmt.Sprintf("'%s' is not an array or pointer", n.Name))
		}
		checkExpr(n.Children[0], st, errs)
		// Element type from subscripting the pointer/array.
		switch e.typ {
		case TypeCharPtr:
			n.Type = TypeChar
		case TypeFloatPtr:
			n.Type = TypeFloat
		case TypeDoublePtr:
			n.Type = TypeDouble
		default:
			if isPtrPtrType(e.typ) {
				n.Type = derefPtrType(e.typ) // int** → int*, char** → char*, double** → double*
			} else if e.typ == TypeIntArray && e.elemType != 0 && e.elemType != TypeInt {
				n.Type = e.elemType // typed array: use the declared element type
			} else {
				n.Type = TypeInt // TypeIntPtr, TypeVoidPtr, TypeIntArray → int element
			}
		}
		return n.Type

	case KindArray2D:
		e := st.lookup(n.Name)
		if e == nil {
			*errs = append(*errs, fmt.Sprintf("undefined variable '%s'", n.Name))
			n.Type = TypeInt
			return TypeInt
		}
		checkExpr(n.Children[0], st, errs)
		checkExpr(n.Children[1], st, errs)
		// Element type of the 2D array
		if e.elemType != 0 {
			n.Type = e.elemType
		} else {
			n.Type = TypeInt
		}
		return n.Type

	case KindAssign:
		rhs := n.Children[1]
		rhsType := checkExpr(rhs, st, errs)
		lhsType := checkExpr(n.Children[0], st, errs)
		// Reject assignment through a const pointer target.
		if n.Children[0].Kind == KindDeref {
			if inner := n.Children[0].Children[0]; inner.Kind == KindVar {
				if e := st.lookup(inner.Name); e != nil && e.isConstTarget {
					*errs = append(*errs, "assignment to const-qualified pointer target")
				}
			}
		}
		// Pointer assignment compatibility check.
		if isPtrType(lhsType) {
			// TypeIntArray used as a value decays to int* (e.g. p = arr_name).
			effectiveRhs := rhsType
			if effectiveRhs == TypeIntArray {
				effectiveRhs = TypeIntPtr
			}
			if isPtrType(effectiveRhs) {
				// Both pointers: must be same type or one side is void*.
				if lhsType != effectiveRhs && lhsType != TypeVoidPtr && effectiveRhs != TypeVoidPtr {
					*errs = append(*errs, "assignment of incompatible pointer types")
				}
			} else {
				// Non-pointer rhs to pointer lhs: only the null pointer constant (literal 0) is valid.
				if rhs.Kind != KindNum || rhs.Val != 0 {
					*errs = append(*errs, "assignment of non-pointer to pointer type")
				}
			}
		}
		n.Type = rhsType
		return rhsType

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
			// Pointer comparison type checking.
			if isPtrType(lt) && isPtrType(rt) {
				if lt != rt && lt != TypeVoidPtr && rt != TypeVoidPtr {
					*errs = append(*errs, "comparison of incompatible pointer types")
				}
			}
			n.Type = TypeInt
		default:
			// Pointer arithmetic: pointer ± integer → same pointer type.
			if isPtrType(lt) && !isPtrType(rt) && (n.Op == "+" || n.Op == "-") {
				n.Type = lt
			} else if isPtrType(rt) && !isPtrType(lt) && n.Op == "+" {
				// integer + pointer (commutative) → pointer type
				n.Type = rt
			} else if isFPType(lt) || isFPType(rt) {
				// FP "infects" — if either operand is FP, result is double.
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
		if f.IsFlexArray {
			// Flex array member decays to a pointer to its element type.
			et := f.ElemType
			if et == 0 {
				et = TypeInt
			}
			n.Type = ptrType(et)
		} else {
			n.Type = f.Type
		}
		n.StructTag = f.StructTag // propagate inner struct tag (for chained access)
		return n.Type

	case KindSizeof:
		var size int
		if len(n.Children) > 0 {
			// sizeof(expr) — type-check expression to determine its type.
			child := n.Children[0]
			t := checkExpr(child, st, errs)
			if t == TypeIntArray && child.Kind == KindVar {
				// Array variable: sizeof = element count × element size (8 bytes for int[]).
				// Array params decay to a pointer, so sizeof(param_arr) = 8.
				if e := st.lookup(child.Name); e != nil && e.arrSize > 0 {
					size = e.arrSize * 8
				} else {
					size = 8 // param or unknown → pointer size
				}
			} else {
				size = sizeofKind(t, child.StructTag, st.structDefs)
			}
		} else if n.StructTag != "" {
			// sizeof(struct Tag)
			sd := st.structDefs[n.StructTag]
			if sd == nil {
				*errs = append(*errs, fmt.Sprintf("sizeof: unknown struct '%s'", n.StructTag))
			} else {
				size = sd.SizeBytes(st.structDefs)
			}
		} else {
			// sizeof(type_specifier)
			size = sizeofKind(n.Type, "", st.structDefs)
		}
		// Fold to integer literal — irgen never sees KindSizeof.
		n.Kind = KindNum
		n.Val = size
		n.Type = TypeInt
		n.Children = nil
		n.StructTag = ""
		return TypeInt

	case KindCall:
		// Check if the callee name is a TypeFuncPtr variable (not a direct function).
		if e := st.lookup(n.Name); e != nil && e.typ == TypeFuncPtr {
			// Rewrite as function-pointer call.
			n.Kind = KindFuncPtrCall
			for _, arg := range n.Children {
				checkExpr(arg, st, errs)
			}
			n.Type = TypeInt // opaque: assume int return
			return TypeInt
		}
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

	case KindFuncPtrCall:
		for _, arg := range n.Children {
			checkExpr(arg, st, errs)
		}
		n.Type = TypeInt // opaque: assume int return
		return TypeInt
	}
	return TypeVoid
}
