package main

import (
	"fmt"
	"strings"
)

// FuncSig holds the type signature of a declared function.
type FuncSig struct {
	Name          string
	ReturnType    TypeKind
	ReturnPointee *CType    // non-nil when ReturnType == TypePtr
	Params        []TypeKind
	ParamPointees []*CType  // per-param pointee CType (nil for non-pointer params); len == len(Params)
	IsExtern      bool      // true for extern function declarations (no body)
	IsVariadic    bool      // true for variadic functions (last param is "...")
}

// symTable is a two-level symbol table: globals + per-function locals.
type symTable struct {
	globals    map[string]*symEntry
	locals     map[string]*symEntry // nil when not inside a function
	funcs      map[string]*FuncSig
	structDefs map[string]*StructDef // registered struct types
}

type symEntry struct {
	name          string
	typ           TypeKind
	pointee       *CType   // non-nil when typ == TypePtr: full pointee type
	structTag     string   // for TypeStruct: the struct name
	isParam       bool
	isGlobal      bool
	isConst       bool
	constVal      int
	arrSize       int      // for TypeIntArray locals/globals: number of elements (0 for params)
	innerDim      int      // for 2D arrays: inner dimension (columns)
	elemType      TypeKind // for TypeIntArray: element type (TypePtr when array-of-pointers)
	elemPointee   *CType   // for TypeIntArray with elemType==TypePtr: the pointer's pointee
	isConstTarget bool     // true for const T *p — cannot store through the pointer
}

// charPtee is the canonical CType for a char pointee (shared singleton).
var charPtee = leafCType(TypeChar)

// normalizePtrType converts TypeCharPtr to (TypePtr, charPtee) so that legacy
// string-literal char* nodes are interchangeable with TypePtr+TypeChar pointee
// variables in type-compatibility checks. TypeIntArray decays to (TypePtr, ptee)
// where ptee describes the element. All other types pass through unchanged.
func normalizePtrType(t TypeKind, ptee *CType) (TypeKind, *CType) {
	if t == TypeCharPtr {
		return TypePtr, charPtee
	}
	return t, ptee
}

func newSymTable() *symTable {
	voidPtee := leafCType(TypeVoid) // pointee for void*
	st := &symTable{
		globals:    make(map[string]*symEntry),
		funcs:      make(map[string]*FuncSig),
		structDefs: make(map[string]*StructDef),
	}
	// Built-in functions.
	st.funcs["input"] = &FuncSig{Name: "input", ReturnType: TypeInt}
	st.funcs["output"] = &FuncSig{Name: "output", ReturnType: TypeVoid,
		Params: []TypeKind{TypeInt}, ParamPointees: []*CType{nil}}
	st.funcs["print_char"] = &FuncSig{Name: "print_char", ReturnType: TypeVoid,
		Params: []TypeKind{TypeInt}, ParamPointees: []*CType{nil}}
	st.funcs["print_string"] = &FuncSig{Name: "print_string", ReturnType: TypeVoid,
		Params: []TypeKind{TypeCharPtr}, ParamPointees: []*CType{nil}}
	st.funcs["malloc"] = &FuncSig{Name: "malloc", ReturnType: TypePtr,
		ReturnPointee: voidPtee,
		Params: []TypeKind{TypeInt}, ParamPointees: []*CType{nil}}
	st.funcs["free"] = &FuncSig{Name: "free", ReturnType: TypeVoid,
		Params: []TypeKind{TypePtr}, ParamPointees: []*CType{voidPtee}}
	st.funcs["print_double"] = &FuncSig{Name: "print_double", ReturnType: TypeVoid,
		Params: []TypeKind{TypeDouble}, ParamPointees: []*CType{nil}}
	// Compiler built-in: returns a pointer to the variadic register save area.
	st.funcs["__va_start"] = &FuncSig{Name: "__va_start", ReturnType: TypePtr,
		ReturnPointee: leafCType(TypeInt)}
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
		// TypeInt, TypeUnsignedInt, TypeDouble, TypePtr, TypeFuncPtr, TypeCharPtr, TypeIntArray, TypeVoid
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
		structTag: n.StructTag, pointee: n.Pointee, isConstTarget: n.IsConstTarget}
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
		structTag: n.StructTag, pointee: n.Pointee, isConstTarget: n.IsConstTarget,
		elemType: n.ElemType, elemPointee: n.ElemPointee}
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
					st.globals[decl.Name].elemPointee = decl.ElemPointee
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
	// Close out trailing bit-field word.
	if bfWordOffset != -1 && !sd.IsUnion {
		// offset will be computed by SizeBytes via the field's ByteOffset+8
		_ = bfWordOffset
	}
	return sd
}

func checkFunDecl(n *Node, st *symTable, errs *[]string) {
	if n.IsExtern {
		sig := &FuncSig{Name: n.Name, ReturnType: n.Type, ReturnPointee: n.Pointee, IsExtern: true}
		for _, p := range n.Children {
			if p.Name == "..." {
				sig.IsVariadic = true
				continue
			}
			sig.Params = append(sig.Params, p.Type)
			sig.ParamPointees = append(sig.ParamPointees, p.Pointee)
		}
		if err := st.declareFunc(sig); err != nil {
			*errs = append(*errs, err.Error())
		}
		return
	}

	nparams := len(n.Children) - 1 // last child is the body
	sig := &FuncSig{Name: n.Name, ReturnType: n.Type, ReturnPointee: n.Pointee}
	for i := 0; i < nparams; i++ {
		p := n.Children[i]
		if p.Name == "..." {
			sig.IsVariadic = true
			continue
		}
		sig.Params = append(sig.Params, p.Type)
		sig.ParamPointees = append(sig.ParamPointees, p.Pointee)
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
		if err := st.declareLocalFull(p, true); err != nil {
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
					structTag: child.StructTag, pointee: child.Pointee, isConstTarget: child.IsConstTarget}
				if child.Type == TypeIntArray {
					e.arrSize = child.Val
					e.elemType = child.ElemType
					e.elemPointee = child.ElemPointee
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
					st.locals[child.Name].elemPointee = child.ElemPointee
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
		_ = checkExpr(n.Children[0], st, errs)
		child := n.Children[0]
		n.Type = TypePtr
		switch child.Type {
		case TypeIntArray:
			// Array-to-pointer decay: use element type.
			et := TypeInt
			var ep *CType
			if child.Kind == KindVar {
				if e := st.lookup(child.Name); e != nil {
					if e.elemType != 0 {
						et = e.elemType
						ep = e.elemPointee
					}
				}
			}
			if et == TypePtr {
				n.Pointee = ptrCType(ep)
			} else {
				n.Pointee = leafCType(et)
			}
		case TypePtr:
			// &ptr — pointer to a pointer.
			n.Pointee = ptrCType(child.Pointee)
		case TypeStruct:
			n.Pointee = structCType(child.StructTag)
		default:
			n.Pointee = leafCType(child.Type)
		}
		return TypePtr

	case KindDeref:
		t := checkExpr(n.Children[0], st, errs)
		if t != TypePtr && t != TypeCharPtr {
			*errs = append(*errs, "cannot dereference non-pointer")
			n.Type = TypeInt
			return TypeInt
		}
		child := n.Children[0]
		ptee := child.Pointee
		if t == TypeCharPtr || (ptee != nil && ptee.Kind == TypeChar) {
			n.Type = TypeChar
			n.Pointee = nil
			return TypeChar
		}
		if ptee == nil {
			n.Type = TypeInt
			return TypeInt
		}
		if ptee.Kind == TypeVoid {
			*errs = append(*errs, "cannot dereference void pointer")
			n.Type = TypeInt
			return TypeInt
		}
		n.Type = ptee.Kind
		n.StructTag = ptee.Tag
		n.Pointee = ptee.Pointee
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
		n.Pointee = e.pointee
		n.StructTag = e.structTag
		// For TypePtr-to-struct: propagate the struct tag to StructTag for field-access lookup.
		if e.typ == TypePtr && e.pointee != nil && e.pointee.Kind == TypeStruct {
			n.StructTag = e.pointee.Tag
		}
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
		if e.typ == TypePtr {
			ptee := e.pointee
			if ptee == nil {
				n.Type = TypeInt
			} else {
				n.Type = ptee.Kind
				n.StructTag = ptee.Tag
				n.Pointee = ptee.Pointee
				// For TypePtr-to-struct: propagate struct tag.
				if ptee.Kind == TypeStruct {
					n.StructTag = ptee.Tag
				}
			}
		} else if e.typ == TypeCharPtr {
			n.Type = TypeChar
		} else if e.typ == TypeIntArray {
			if e.elemType == TypePtr {
				n.Type = TypePtr
				n.Pointee = e.elemPointee
			} else if e.elemType != 0 && e.elemType != TypeInt {
				n.Type = e.elemType // typed array: use the declared element type
			} else {
				n.Type = TypeInt
			}
		} else {
			n.Type = TypeInt // TypeVoidPtr fallback
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
			// TypeIntArray used as a value decays to pointer.
			effectiveRhs := rhsType
			effectiveRhsPtee := rhs.Pointee
			if effectiveRhs == TypeIntArray {
				effectiveRhs = TypePtr
				effectiveRhsPtee = leafCType(TypeInt)
			}
			if isPtrType(effectiveRhs) {
				// Both pointers: must have compatible pointees, or one side is void*.
				// Normalize TypeCharPtr (legacy string-literal type) to TypePtr(TypeChar).
				normLhs, lhsPtee := normalizePtrType(lhsType, n.Children[0].Pointee)
				normRhs, rhsPtee := normalizePtrType(effectiveRhs, effectiveRhsPtee)
				isLhsVoid := normLhs == TypePtr && ctypeIsVoidPtr(lhsPtee)
				isRhsVoid := normRhs == TypePtr && ctypeIsVoidPtr(rhsPtee)
				if !isLhsVoid && !isRhsVoid {
					if normLhs != normRhs || !ctypeEq(lhsPtee, rhsPtee) {
						*errs = append(*errs, "assignment of incompatible pointer types")
					}
				}
			} else {
				// Non-pointer rhs to pointer lhs: reject non-zero integer literals.
				// Other non-pointer expressions (e.g. deref of long* for variadic args)
				// are allowed — the programmer is responsible for the cast-free conversion.
				if rhs.Kind == KindNum && rhs.Val != 0 {
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

	case KindPostInc, KindPostDec, KindPreInc, KindPreDec:
		t := checkExpr(n.Children[0], st, errs)
		n.Type = t
		if isPtrType(t) {
			n.Pointee = n.Children[0].Pointee
		}
		return t

	case KindLogAnd, KindLogOr:
		checkExpr(n.Children[0], st, errs)
		checkExpr(n.Children[1], st, errs)
		n.Type = TypeInt
		return TypeInt

	case KindBinOp:
		lt := checkExpr(n.Children[0], st, errs)
		rt := checkExpr(n.Children[1], st, errs)
		// Comparison operators always yield int (0 or 1); the operand types
		// determine signed vs unsigned comparison in irgen via Children[0].Type.
		switch n.Op {
		case "<", "<=", ">", ">=", "==", "!=":
			// Pointer comparison type checking.
			if isPtrType(lt) && isPtrType(rt) {
				lPtee := n.Children[0].Pointee
				rPtee := n.Children[1].Pointee
				isLVoid := lt == TypePtr && ctypeIsVoidPtr(lPtee)
				isRVoid := rt == TypePtr && ctypeIsVoidPtr(rPtee)
				if !isLVoid && !isRVoid && !ctypeEq(lPtee, rPtee) {
					*errs = append(*errs, "comparison of incompatible pointer types")
				}
			}
			n.Type = TypeInt
		default:
			// Pointer arithmetic: pointer ± integer → same pointer type (preserve Pointee).
			if isPtrType(lt) && !isPtrType(rt) && (n.Op == "+" || n.Op == "-") {
				n.Type = lt
				n.Pointee = n.Children[0].Pointee
			} else if isPtrType(rt) && !isPtrType(lt) && n.Op == "+" {
				n.Type = rt
				n.Pointee = n.Children[1].Pointee
			} else if isFPType(lt) || isFPType(rt) {
				n.Type = TypeDouble
			} else if isUnsignedType(lt) || isUnsignedType(rt) {
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
		base := n.Children[0]
		baseTag := base.StructTag
		// For "->": if base is TypePtr pointing to a struct, extract tag from Pointee.
		if baseTag == "" && base.Type == TypePtr && base.Pointee != nil && base.Pointee.Kind == TypeStruct {
			baseTag = base.Pointee.Tag
		}
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
			n.Type = TypePtr
			if et == TypePtr {
				n.Pointee = f.ElemPointee
			} else {
				n.Pointee = leafCType(et)
			}
		} else {
			n.Type = f.Type
			n.Pointee = f.Pointee
			if f.Type == TypeStruct {
				n.StructTag = f.StructTag
			} else if f.Type == TypePtr && f.Pointee != nil && f.Pointee.Kind == TypeStruct {
				n.StructTag = f.Pointee.Tag
			} else {
				n.StructTag = f.StructTag
			}
		}
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

	case KindVAArg:
		// va_arg(ap, type) — type-check the ap expression; result type is already
		// recorded in n.Type / n.Pointee by the grammar action.  No further
		// checking needed: ap is a va_list (long*) and we trust the caller.
		checkExpr(n.Children[0], st, errs)
		// n.Type and n.Pointee are already set by the grammar rule; just return.
		return n.Type

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
		for i, arg := range n.Children {
			argType := checkExpr(arg, st, errs)
			// Per-param pointer type checking: skip variadic functions and
			// builtins whose signatures are simplified (accept both ptr and int).
			isBuiltin := n.Name == "input" || n.Name == "output" ||
				n.Name == "print_char" || n.Name == "print_string" || n.Name == "print_double"
			if i < len(sig.Params) && !sig.IsVariadic && !isBuiltin {
				paramType := sig.Params[i]
				paramPtee := (*CType)(nil)
				if i < len(sig.ParamPointees) {
					paramPtee = sig.ParamPointees[i]
				}
				// Array-to-pointer decay: TypeIntArray passed as arg decays to a pointer.
				// Treat it as TypePtr for the purpose of compatibility checking.
				effectiveArgType := argType
				effectiveArgPtee := arg.Pointee
				if argType == TypeIntArray {
					effectiveArgType = TypePtr
					// Determine element type for the decayed pointer.
					if e := st.lookup(arg.Name); e != nil {
						if e.elemType == TypePtr {
							effectiveArgPtee = e.elemPointee
						} else if e.elemType != 0 {
							effectiveArgPtee = leafCType(e.elemType)
						} else {
							effectiveArgPtee = leafCType(TypeInt)
						}
					} else {
						effectiveArgPtee = leafCType(TypeInt)
					}
				}
				// Array-to-pointer decay on the param side: int arr[] params behave as int*.
				effectiveParamType := paramType
				if effectiveParamType == TypeIntArray {
					effectiveParamType = TypePtr
					if paramPtee == nil {
						paramPtee = leafCType(TypeInt)
					}
				}
				// Normalize TypeCharPtr (legacy string-literal type) to TypePtr(TypeChar)
				// on both sides so that char* variables and string literals compare equal.
				effectiveArgType, effectiveArgPtee = normalizePtrType(effectiveArgType, effectiveArgPtee)
				effectiveParamType, paramPtee = normalizePtrType(effectiveParamType, paramPtee)
				// Flag: pointer passed where non-pointer expected, or vice versa.
				argIsPtr := isPtrType(effectiveArgType)
				paramIsPtr := isPtrType(effectiveParamType)
				if argIsPtr && !paramIsPtr && effectiveParamType != TypeVoid {
					*errs = append(*errs, fmt.Sprintf("'%s' arg %d: pointer passed where non-pointer expected",
						n.Name, i+1))
				} else if !argIsPtr && paramIsPtr && argType != TypeVoid &&
					!(arg.Kind == KindNum && arg.Val == 0) {
					// Null pointer constant (0) is always valid.
					*errs = append(*errs, fmt.Sprintf("'%s' arg %d: non-pointer passed where pointer expected",
						n.Name, i+1))
				} else if argIsPtr && paramIsPtr && effectiveParamType == TypePtr && effectiveArgType == TypePtr {
					// Both pointer: check compatibility (void* is universal).
					isArgVoid := ctypeIsVoidPtr(effectiveArgPtee)
					isParamVoid := ctypeIsVoidPtr(paramPtee)
					if !isArgVoid && !isParamVoid && !ctypeEq(effectiveArgPtee, paramPtee) {
						*errs = append(*errs, fmt.Sprintf("'%s' arg %d: incompatible pointer types",
							n.Name, i+1))
					}
				} else if isFPType(argType) && !isFPType(paramType) && !paramIsPtr {
					*errs = append(*errs, fmt.Sprintf("'%s' arg %d: floating-point passed where integer expected",
						n.Name, i+1))
				} else if !isFPType(argType) && isFPType(paramType) {
					*errs = append(*errs, fmt.Sprintf("'%s' arg %d: integer passed where floating-point expected",
						n.Name, i+1))
				}
			}
		}
		n.Type = sig.ReturnType
		n.Pointee = sig.ReturnPointee
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
