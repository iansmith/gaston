package main

import (
	"fmt"
	"strings"
)

// FuncSig holds the type signature of a declared function.
type FuncSig struct {
	Name            string
	ReturnType      TypeKind
	ReturnPointee   *CType   // non-nil when ReturnType == TypePtr
	ReturnStructTag string   // non-empty when ReturnType == TypeStruct: the struct type name
	Params          []TypeKind
	ParamPointees   []*CType // per-param pointee CType (nil for non-pointer params); len == len(Params)
	IsExtern        bool     // true for extern function declarations (no body)
	IsVariadic      bool     // true for variadic functions (last param is "...")
}

// symTable is a two-level symbol table: globals + per-function locals.
type symTable struct {
	globals     map[string]*symEntry
	locals      map[string]*symEntry // nil when not inside a function
	funcs       map[string]*FuncSig
	structDefs  map[string]*StructDef // registered struct types
	currentFunc *Node                 // set during checkFunDecl; used by KindStmtExpr to check return stmts
	libraryMode bool                  // when true, unknown function calls are implicit extern (C89 style)
}

type symEntry struct {
	name          string
	typ           TypeKind
	pointee       *CType   // non-nil when typ == TypePtr: full pointee type
	structTag     string   // for TypeStruct: the struct name
	isParam       bool
	isGlobal      bool
	isExtern      bool     // true for extern forward declarations (may be overridden by a definition)
	isTentative   bool     // true for non-extern declarations without an initializer (C tentative definitions)
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
// isConstantScalar reports whether n is a compile-time constant suitable as a
// global variable initializer: a numeric/float literal, or unary minus thereof.
func isConstantScalar(n *Node) bool {
	switch n.Kind {
	case KindNum, KindFNum, KindCharLit:
		return true
	case KindStrLit:
		return true
	case KindUnary:
		return n.Op == "-" && len(n.Children) == 1 && isConstantScalar(n.Children[0])
	case KindCast:
		// (size_t)-1, (int)0, etc.
		return len(n.Children) == 1 && isConstantScalar(n.Children[0])
	case KindBinOp:
		// Constant arithmetic: 128L * 1024L, 1 << 16, etc.
		return len(n.Children) == 2 && isConstantScalar(n.Children[0]) && isConstantScalar(n.Children[1])
	case KindAddrOf:
		// &staticvar or &staticvar.field.field... is a link-time constant
		// and valid as a global variable initializer (C99 §6.6).
		return true
	}
	return false
}

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
	// Standard C library functions — provided by libgastonc.a at link time.
	st.funcs["input"] = &FuncSig{Name: "input", ReturnType: TypeInt, IsExtern: true}
	st.funcs["output"] = &FuncSig{Name: "output", ReturnType: TypeVoid, IsExtern: true,
		Params: []TypeKind{TypeInt}, ParamPointees: []*CType{nil}}
	st.funcs["print_char"] = &FuncSig{Name: "print_char", ReturnType: TypeVoid, IsExtern: true,
		Params: []TypeKind{TypeInt}, ParamPointees: []*CType{nil}}
	st.funcs["print_string"] = &FuncSig{Name: "print_string", ReturnType: TypeVoid, IsExtern: true,
		Params: []TypeKind{TypeCharPtr}, ParamPointees: []*CType{nil}}
	st.funcs["malloc"] = &FuncSig{Name: "malloc", ReturnType: TypePtr,
		ReturnPointee: voidPtee, IsExtern: true,
		Params: []TypeKind{TypeInt}, ParamPointees: []*CType{nil}}
	st.funcs["free"] = &FuncSig{Name: "free", ReturnType: TypeVoid, IsExtern: true,
		Params: []TypeKind{TypePtr}, ParamPointees: []*CType{voidPtee}}
	st.funcs["realloc"] = &FuncSig{Name: "realloc", ReturnType: TypePtr,
		ReturnPointee: voidPtee, IsExtern: true,
		Params: []TypeKind{TypePtr, TypeInt}, ParamPointees: []*CType{voidPtee, nil}}
	st.funcs["calloc"] = &FuncSig{Name: "calloc", ReturnType: TypePtr,
		ReturnPointee: voidPtee, IsExtern: true,
		Params: []TypeKind{TypeInt, TypeInt}, ParamPointees: []*CType{nil, nil}}
	st.funcs["print_double"] = &FuncSig{Name: "print_double", ReturnType: TypeVoid, IsExtern: true,
		Params: []TypeKind{TypeDouble}, ParamPointees: []*CType{nil}}
	st.funcs["fflush"] = &FuncSig{Name: "fflush", ReturnType: TypeInt, IsExtern: true,
		Params: []TypeKind{TypePtr}, ParamPointees: []*CType{voidPtee}}
	// Compiler built-in: returns a pointer to the variadic register save area.
	st.funcs["__va_start"] = &FuncSig{Name: "__va_start", ReturnType: TypePtr,
		ReturnPointee: leafCType(TypeLong)}
	// __builtin_expect(expr, hint) — returns expr; used for branch prediction hints.
	st.funcs["__builtin_expect"] = &FuncSig{Name: "__builtin_expect", ReturnType: TypeLong,
		Params: []TypeKind{TypeLong, TypeLong}, ParamPointees: []*CType{nil, nil}}
	// __builtin_signbit family — takes a floating-point arg, returns int.
	// Marked IsExtern so libc can provide actual implementations.
	for _, name := range []string{
		"__builtin_signbit", "__builtin_signbitf", "__builtin_signbitl",
	} {
		st.funcs[name] = &FuncSig{
			Name:          name,
			IsExtern:      true,
			ReturnType:    TypeInt,
			Params:        []TypeKind{TypeDouble},
			ParamPointees: []*CType{nil},
		}
	}
	// __builtin_unreachable() — no-op marker; tells compiler a path is never reached.
	st.funcs["__builtin_unreachable"] = &FuncSig{Name: "__builtin_unreachable", ReturnType: TypeVoid}
	// __builtin_bswap16/32/64: byte-swap intrinsics.
	st.funcs["__builtin_bswap16"] = &FuncSig{Name: "__builtin_bswap16", ReturnType: TypeUnsignedInt,
		Params: []TypeKind{TypeUnsignedInt}, ParamPointees: []*CType{nil}}
	st.funcs["__builtin_bswap32"] = &FuncSig{Name: "__builtin_bswap32", ReturnType: TypeUnsignedInt,
		Params: []TypeKind{TypeUnsignedInt}, ParamPointees: []*CType{nil}}
	st.funcs["__builtin_bswap64"] = &FuncSig{Name: "__builtin_bswap64", ReturnType: TypeUnsignedLong,
		Params: []TypeKind{TypeUnsignedLong}, ParamPointees: []*CType{nil}}
	// alloca(size_t): allocate bytes on the stack; returns void*.
	st.funcs["alloca"] = &FuncSig{Name: "alloca", ReturnType: TypePtr,
		ReturnPointee: leafCType(TypeVoid),
		Params: []TypeKind{TypeUnsignedLong}, ParamPointees: []*CType{nil}}
	// __builtin_add_overflow / __builtin_sub_overflow / __builtin_mul_overflow.
	st.funcs["__builtin_add_overflow"] = &FuncSig{Name: "__builtin_add_overflow", ReturnType: TypeInt,
		Params: []TypeKind{TypeLong, TypeLong, TypePtr},
		ParamPointees: []*CType{nil, nil, leafCType(TypeLong)}}
	st.funcs["__builtin_sub_overflow"] = &FuncSig{Name: "__builtin_sub_overflow", ReturnType: TypeInt,
		Params: []TypeKind{TypeLong, TypeLong, TypePtr},
		ParamPointees: []*CType{nil, nil, leafCType(TypeLong)}}
	st.funcs["__builtin_mul_overflow"] = &FuncSig{Name: "__builtin_mul_overflow", ReturnType: TypeInt,
		Params: []TypeKind{TypeLong, TypeLong, TypePtr},
		ParamPointees: []*CType{nil, nil, leafCType(TypeLong)}}
	// __builtin_frame_address(N) — returns frame pointer of Nth calling frame as void*.
	// Restrict to N=0 (current frame's FP) for simplicity.
	st.funcs["__builtin_frame_address"] = &FuncSig{Name: "__builtin_frame_address", ReturnType: TypePtr,
		ReturnPointee: leafCType(TypeVoid), Params: []TypeKind{TypeInt}, ParamPointees: []*CType{nil}}
	// GCC bit-manipulation intrinsics.
	for _, name := range []string{
		"__builtin_clz", "__builtin_clzl", "__builtin_clzll",
		"__builtin_ctz", "__builtin_ctzl", "__builtin_ctzll",
		"__builtin_popcount", "__builtin_popcountl", "__builtin_popcountll",
	} {
		paramType := TypeUnsignedInt
		if strings.HasSuffix(name, "ll") || strings.HasSuffix(name, "l") {
			paramType = TypeUnsignedLong
		}
		st.funcs[name] = &FuncSig{
			Name:          name,
			ReturnType:    TypeInt,
			Params:        []TypeKind{paramType},
			ParamPointees: []*CType{nil},
		}
	}
	return st
}

// sizeofKind returns the byte size for the given TypeKind.
// Follows LP64: int/unsigned int are 4 bytes; long/double/pointers are 8 bytes.
// For TypeStruct, structTag and structDefs are used to sum the field sizes.
func sizeofKind(t TypeKind, structTag string, structDefs map[string]*StructDef) int {
	switch t {
	case TypeChar, TypeUnsignedChar:
		return 1
	case TypeShort, TypeUnsignedShort:
		return 2
	case TypeInt, TypeUnsignedInt:
		return 4
	case TypeFloat:
		return 4
	case TypeStruct:
		if structTag != "" {
			if sd, ok := structDefs[structTag]; ok {
				return sd.SizeBytes(structDefs)
			}
		}
		return 8
	case TypeInt128, TypeUint128:
		return 16
	default:
		// TypeDouble, TypePtr, TypeFuncPtr, TypeCharPtr, TypeIntArray, TypeVoid → 8 bytes
		return 8
	}
}

// alignofKind returns the natural alignment in bytes for the given TypeKind.
func alignofKind(t TypeKind) int {
	switch t {
	case TypeChar, TypeUnsignedChar:
		return 1
	case TypeShort, TypeUnsignedShort:
		return 2
	case TypeInt, TypeUnsignedInt, TypeFloat:
		return 4
	default:
		return 8 // long, double, pointer, int128, struct, etc.
	}
}

func (st *symTable) enterFunc() { st.locals = make(map[string]*symEntry) }
func (st *symTable) leaveFunc() { st.locals = nil }

func (st *symTable) declareGlobal(name string, typ TypeKind, structTag ...string) error {
	if existing, ok := st.globals[name]; ok {
		if !existing.isExtern {
			return fmt.Errorf("redeclaration of global '%s'", name)
		}
		// Extern forward declaration — allow a later definition to override.
		return nil
	}
	e := &symEntry{name: name, typ: typ, isGlobal: true, isExtern: true}
	if len(structTag) > 0 {
		e.structTag = structTag[0]
	}
	st.globals[name] = e
	return nil
}

func (st *symTable) declareGlobalFull(n *Node) error {
	hasInit := len(n.Children) > 0
	if existing, ok := st.globals[n.Name]; ok {
		if !existing.isExtern && !existing.isTentative {
			return fmt.Errorf("redeclaration of global '%s'", n.Name)
		}
		// Extern forward declaration or tentative definition — allow a definition to override it.
	}
	e := &symEntry{name: n.Name, typ: n.Type, isGlobal: true, isExtern: n.IsExtern,
		isTentative:   !n.IsExtern && !hasInit,
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
		// Allow re-declaration in inner blocks (C99 block scope); just update.
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
		// Allow re-declaration in inner blocks (C99 block scope); just update the entry.
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
		// Allow extern re-declaration of an already-defined function.
		if sig.IsExtern {
			return nil
		}
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
// requireMain: when false (library / -c compile), skip the 'main' requirement.
// Returns a multi-line error string if any errors are found.
func semCheck(prog *Node, requireMain bool) error {
	st := newSymTable()
	st.libraryMode = !requireMain
	var errs []string

	for _, decl := range prog.Children {
		switch decl.Kind {
		case KindStructDef:
			sd := buildStructDef(decl, &errs, st.structDefs)
			if sd != nil {
				st.structDefs[sd.Name] = sd
			}
		case KindVarDecl:
			// Resolve typeof(expr) at global scope (rare but possible).
			if decl.Type == TypeTypeof && decl.TypeofExpr != nil {
				resolvedType := checkExpr(decl.TypeofExpr, st, &errs)
				decl.Type = resolvedType
				decl.Pointee = decl.TypeofExpr.Pointee
				decl.StructTag = decl.TypeofExpr.StructTag
				decl.TypeofExpr = nil
			}
			if decl.IsExtern {
				if err := st.declareGlobalFull(decl); err != nil {
					errs = append(errs, err.Error())
				} else if decl.Type == TypeIntArray {
					st.globals[decl.Name].elemType = decl.ElemType
					st.globals[decl.Name].elemPointee = decl.ElemPointee
					st.globals[decl.Name].arrSize = decl.Val
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
					// Infer array size from initializer when Val==0.
					if decl.Val == 0 && len(decl.Children) > 0 {
						switch decl.Children[0].Kind {
						case KindInitList:
							decl.Val = len(decl.Children[0].Children)
						case KindStrLit:
							decl.Val = len(decl.Children[0].Name) + 1 // include NUL
						}
					}
					st.globals[decl.Name].arrSize = decl.Val
					st.globals[decl.Name].elemType = decl.ElemType
					st.globals[decl.Name].elemPointee = decl.ElemPointee
					st.globals[decl.Name].innerDim = decl.Dim2
				}
				if len(decl.Children) > 0 {
					init := decl.Children[0]
					if init.Kind == KindInitList {
						checkInitList(init, decl, st, &errs)
					} else if !isConstantScalar(init) {
						errs = append(errs, fmt.Sprintf("global '%s': initializer must be a constant", decl.Name))
					} else if decl.Type == TypeIntArray && init.Kind != KindStrLit {
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
	sd := &StructDef{Name: n.Name, IsUnion: n.IsUnion, IsPacked: n.IsPacked}
	offset := 0
	bfWordOffset := -1  // byte offset of current bit-field storage word (-1 = none active)
	bfBitsUsed := 0
	const bfWordBits = 64
	const bfWordSize = 8

	for _, child := range n.Children {
		// Inline struct/union definition node emitted as a sibling field (e.g. from
		// "struct { ... } tab[N];" in field grammar) — register it and skip.
		if child.Kind == KindStructDef {
			inner := buildStructDef(child, errs, structDefs)
			if inner != nil {
				structDefs[child.Name] = inner
			}
			continue
		}

		// Anonymous or named-inline struct/union member: child.Children holds the
		// inline field list.  Register the type, then add the field.
		// Name=="" → anonymous (fields promoted); Name!="" → named inline-type field.
		if child.Type == TypeStruct && len(child.Children) > 0 {
			anonDef := &Node{Kind: KindStructDef, Name: child.StructTag,
				Children: child.Children, IsUnion: child.IsUnion}
			inner := buildStructDef(anonDef, errs, structDefs)
			if inner != nil {
				structDefs[child.StructTag] = inner
			}
			sz, align := fieldSizeAlign(TypeStruct, child.StructTag, structDefs)
			if !sd.IsUnion {
				offset = (offset + align - 1) &^ (align - 1)
			}
			sd.Fields = append(sd.Fields, StructField{
				Name:       child.Name,
				Type:       TypeStruct,
				StructTag:  child.StructTag,
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
			// Close out any open bit-field word.
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
			if !sd.IsUnion && !sd.IsPacked {
				// Struct: advance offset with natural alignment.
				offset = (offset + align - 1) &^ (align - 1)
			}
			// Union: all fields start at offset 0 (offset stays 0 throughout).
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
	// Close out trailing bit-field word.
	if bfWordOffset != -1 && !sd.IsUnion {
		// offset will be computed by SizeBytes via the field's ByteOffset+8
		_ = bfWordOffset
	}
	return sd
}

// arrayParamToPtr converts a TypeIntArray parameter to its effective TypePtr form
// so that array-typed parameters (e.g. char[26]) are compatible with pointer arguments.
func arrayParamToPtr(p *Node) (TypeKind, *CType) {
	if p.Type != TypeIntArray {
		return p.Type, p.Pointee
	}
	if p.ElemType == TypePtr {
		// Array of pointers (e.g. char *argv[]) decays to pointer-to-pointer (char **).
		return TypePtr, ptrCType(p.ElemPointee)
	}
	if p.ElemType != 0 {
		return TypePtr, leafCType(p.ElemType)
	}
	return TypePtr, leafCType(TypeInt)
}

func checkFunDecl(n *Node, st *symTable, errs *[]string) {
	if n.IsExtern {
		sig := &FuncSig{Name: n.Name, ReturnType: n.Type, ReturnPointee: n.Pointee, IsExtern: true, ReturnStructTag: n.StructTag}
		for _, p := range n.Children {
			if p.Name == "..." {
				sig.IsVariadic = true
				continue
			}
			pt, pp := arrayParamToPtr(p)
			sig.Params = append(sig.Params, pt)
			sig.ParamPointees = append(sig.ParamPointees, pp)
		}
		if err := st.declareFunc(sig); err != nil {
			*errs = append(*errs, err.Error())
		}
		return
	}

	nparams := len(n.Children) - 1 // last child is the body
	sig := &FuncSig{Name: n.Name, ReturnType: n.Type, ReturnPointee: n.Pointee, ReturnStructTag: n.StructTag}
	for i := 0; i < nparams; i++ {
		p := n.Children[i]
		if p.Name == "..." {
			sig.IsVariadic = true
			continue
		}
		pt, pp := arrayParamToPtr(p)
		sig.Params = append(sig.Params, pt)
		sig.ParamPointees = append(sig.ParamPointees, pp)
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
	st.currentFunc = n
	defer func() { st.currentFunc = nil }()
	checkCompound(body, st, n, errs)
}

func checkCompound(n *Node, st *symTable, fn *Node, errs *[]string) {
	for _, child := range n.Children {
		switch child.Kind {
		case KindStructDef:
			// Inline struct/union defined inside a function body (e.g. "union { float f; uint32_t i; } u;")
			sd := buildStructDef(child, errs, st.structDefs)
			if sd != nil {
				st.structDefs[sd.Name] = sd
			}
		case KindFunDecl:
			// Local function prototype (e.g. "void f(int, void *);") — register as extern.
			checkFunDecl(child, st, errs)
		case KindVarDecl:
			// Resolve typeof(expr) before processing the declaration.
			if child.Type == TypeTypeof && child.TypeofExpr != nil {
				resolvedType := checkExpr(child.TypeofExpr, st, errs)
				child.Type = resolvedType
				child.Pointee = child.TypeofExpr.Pointee
				child.StructTag = child.TypeofExpr.StructTag
				child.TypeofExpr = nil // resolved
			}
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
					init := child.Children[0]
					if init.Kind == KindInitList {
						checkInitList(init, child, st, errs)
					} else {
						// For VLA: Children[0] is the size variable expression.
						// For non-VLA: Children[0] is the initializer expression.
						checkExpr(init, st, errs)
					}
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
			if n.Children[0].Kind == KindCompound {
				checkStmt(n.Children[0], st, fn, errs)
			} else {
				checkExpr(n.Children[0], st, errs)
			}
		}
		if n.Children[1] != nil {
			checkExpr(n.Children[1], st, errs)
		}
		if n.Children[2] != nil {
			if n.Children[2].Kind == KindCompound {
				checkStmt(n.Children[2], st, fn, errs)
			} else {
				checkExpr(n.Children[2], st, errs)
			}
		}
		checkStmt(n.Children[3], st, fn, errs)
	case KindDoWhile:
		checkStmt(n.Children[0], st, fn, errs)
		checkExpr(n.Children[1], st, errs)
	case KindSwitch:
		// Children: [expr, case1, case2, ...]
		checkExpr(n.Children[0], st, errs)
		for _, c := range n.Children[1:] {
			checkStmt(c, st, fn, errs)
		}
	case KindCase:
		// Val == -1 for default; Children[0] = case expr, rest = stmts
		start := 0
		if n.Val != -1 {
			checkExpr(n.Children[0], st, errs)
			start = 1
		}
		for _, s := range n.Children[start:] {
			checkStmt(s, st, fn, errs)
		}
	case KindBreak, KindContinue:
		// valid anywhere inside a loop; runtime check in irgen
	case KindGoto:
		// target label is function-scoped; validated at assembly time
	case KindIndirectGoto:
		// goto *expr; validate the expression
		checkExpr(n.Children[0], st, errs)
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
	case KindVarDecl:
		// C99 declaration inside a switch case (or other statement position).
		if n.Type == TypeTypeof && n.TypeofExpr != nil {
			resolvedType := checkExpr(n.TypeofExpr, st, errs)
			n.Type = resolvedType
			n.Pointee = n.TypeofExpr.Pointee
			n.StructTag = n.TypeofExpr.StructTag
			n.TypeofExpr = nil
		}
		if n.IsStatic {
			e := &symEntry{name: n.Name, typ: n.Type, isGlobal: true,
				structTag: n.StructTag, pointee: n.Pointee, isConstTarget: n.IsConstTarget}
			if n.Type == TypeIntArray {
				e.arrSize = n.Val
				e.elemType = n.ElemType
				e.elemPointee = n.ElemPointee
				e.innerDim = n.Dim2
			}
			if st.locals != nil {
				st.locals[n.Name] = e
			}
		} else {
			if err := st.declareLocalFull(n, false); err != nil {
				*errs = append(*errs, err.Error())
			} else if n.Type == TypeIntArray {
				st.locals[n.Name].arrSize = n.Val
				st.locals[n.Name].elemType = n.ElemType
				st.locals[n.Name].elemPointee = n.ElemPointee
				st.locals[n.Name].innerDim = n.Dim2
			}
		}
		if len(n.Children) > 0 {
			init := n.Children[0]
			if init.Kind == KindInitList {
				checkInitList(init, n, st, errs)
			} else {
				checkExpr(init, st, errs)
			}
		}
	case KindFunDecl:
		checkFunDecl(n, st, errs)
	case KindStructDef:
		sd := buildStructDef(n, errs, st.structDefs)
		if sd != nil {
			st.structDefs[sd.Name] = sd
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

	case KindLabelAddr:
		// &&label (GCC computed goto) — address of a user label; type is void*.
		n.Type = TypePtr
		n.Pointee = leafCType(TypeVoid)
		return TypePtr

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
		if t == TypeIntArray {
			// Array-to-pointer decay: *arr is equivalent to arr[0].
			child := n.Children[0]
			if child.ElemType == TypeStruct && child.StructTag != "" {
				n.Type = TypeStruct
				n.StructTag = child.StructTag
				return TypeStruct
			}
			elemType := child.ElemType
			if elemType == 0 {
				elemType = TypeInt
			}
			n.Type = elemType
			n.Pointee = child.ElemPointee
			return elemType
		}
		if t == TypeFuncPtr {
			// Dereferencing a function pointer is a no-op in C: (*fnptr)(args) == fnptr(args).
			n.Type = TypeFuncPtr
			return TypeFuncPtr
		}
		if t != TypePtr && t != TypeCharPtr {
			// Allow deref on opaque types from struct field access or array subscripts —
			// the actual type may be a function pointer that semcheck can't track.
			child := n.Children[0]
			if child.Kind != KindFieldAccess && child.Kind != KindIndexExpr {
				*errs = append(*errs, "cannot dereference non-pointer")
			}
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
			// GNU C allows void pointer dereference (treats result as char-sized int).
			// MicroPython and other real-world C code rely on this.
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
		n.ElemType = e.elemType
		n.ElemPointee = e.elemPointee
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
				n.StructTag = e.structTag // propagate element struct tag
			} else {
				n.Type = TypeInt
			}
		} else {
			n.Type = TypeInt // TypeVoidPtr fallback
		}
		return n.Type

	case KindIndexExpr:
		// postfix_expr[index]: base is a pointer expression, not a named variable.
		checkExpr(n.Children[0], st, errs)
		checkExpr(n.Children[1], st, errs)
		base := n.Children[0]
		switch base.Type {
		case TypePtr:
			ptee := base.Pointee
			if ptee == nil {
				n.Type = TypeInt
			} else {
				n.Type = ptee.Kind
				n.StructTag = ptee.Tag
				n.Pointee = ptee.Pointee
			}
		case TypeCharPtr:
			n.Type = TypeChar
		case TypeIntArray:
			// Array indexing via field access or array expression: element type is base.ElemType.
			et := base.ElemType
			if et == 0 {
				et = TypeInt
			}
			n.Type = et
			n.StructTag = base.StructTag
			if et == TypePtr {
				n.Pointee = base.ElemPointee
			}
		default:
			n.Type = TypeInt
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
		// Element type of the 2D array (or array-of-pointers: arr[i][j] = (arr[i])[j]).
		if e.elemType == TypePtr {
			// Array of pointers: second subscript dereferences the pointer element.
			if e.elemPointee != nil {
				n.Type = e.elemPointee.Kind
				n.StructTag = e.elemPointee.Tag
				n.Pointee = e.elemPointee.Pointee
			} else {
				n.Type = TypeInt
			}
		} else if e.elemType != 0 {
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
			// TypeIntArray used as a value decays to pointer to its element type.
			effectiveRhs := rhsType
			effectiveRhsPtee := rhs.Pointee
			if effectiveRhs == TypeIntArray {
				effectiveRhs = TypePtr
				if rhs.ElemType != TypeInt && rhs.ElemType != 0 {
					ct := leafCType(rhs.ElemType)
					if rhs.ElemType == TypeStruct && rhs.StructTag != "" {
						ct.Tag = rhs.StructTag
					}
					effectiveRhsPtee = ct
				} else {
					effectiveRhsPtee = leafCType(TypeInt)
				}
			}
			if isPtrType(effectiveRhs) {
				// Both pointers: must have compatible pointees, or one side is void*.
				// Normalize TypeCharPtr (legacy string-literal type) to TypePtr(TypeChar).
				// Also treat raw string literals (TypeCharPtr before normalization) as void*:
				// L"..." wide literals are lexed as char* but assigned to wchar_t* — suppress.
				rawRhsIsStringLit := effectiveRhs == TypeCharPtr
				normLhs, lhsPtee := normalizePtrType(lhsType, n.Children[0].Pointee)
				normRhs, rhsPtee := normalizePtrType(effectiveRhs, effectiveRhsPtee)
				isLhsVoid := normLhs == TypePtr && ctypeIsVoidPtr(lhsPtee)
				isRhsVoid := normRhs == TypePtr && (ctypeIsVoidPtr(rhsPtee) || rawRhsIsStringLit)
				if !isLhsVoid && !isRhsVoid {
					if normLhs != normRhs || !ctypeEq(lhsPtee, rhsPtee) {
						// Allow struct pointer assignment when tags match (ignore const qualifiers),
						// or when one side lost struct tag info through array decay.
						lTag := ctypeStructTag(lhsPtee)
						rTag := ctypeStructTag(rhsPtee)
						bothStruct := lTag != "" && rTag != "" && lTag == rTag
						arrayDecayLostTag := (lTag != "" && rhsPtee != nil && rhsPtee.Kind == TypeStruct && rTag == "") ||
							(rTag != "" && lhsPtee != nil && lhsPtee.Kind == TypeStruct && lTag == "")
						if !bothStruct && !arrayDecayLostTag {
							lhsName := ""
							if n.Children[0].Kind == KindVar {
								lhsName = n.Children[0].Name
							}
								rhsDesc := fmt.Sprintf("nodeKind=%v", n.Children[1].Kind)
							if n.Children[1].Kind == KindVar { rhsDesc += " name=" + n.Children[1].Name }
							if n.Children[1].Kind == KindCall { rhsDesc += " call=" + n.Children[1].Name }
							if n.Children[1].Kind == KindBinOp {
								rhsDesc += " op=" + n.Children[1].Op
								lc := n.Children[1].Children[0]
								rhsDesc += fmt.Sprintf(" lchild(kind=%v", lc.Kind)
								if lc.Kind == KindVar { rhsDesc += " name=" + lc.Name }
								if lc.Kind == KindBinOp { rhsDesc += " op=" + lc.Op }
								rhsDesc += fmt.Sprintf(" type=%v ptee=%v", lc.Type, lc.Pointee)
								if lc.Kind == KindAddrOf && len(lc.Children) > 0 {
									gc := lc.Children[0]
									rhsDesc += fmt.Sprintf(" addrof-child(kind=%v name=%q op=%q type=%v ptee=%v)", gc.Kind, gc.Name, gc.Op, gc.Type, gc.Pointee)
								}
								rhsDesc += ")"
							}
							lhsNodeKind := n.Children[0].Kind
								if gc2 := func() *Node { if n.Children[1].Kind == KindBinOp && len(n.Children[1].Children) > 0 { lc2 := n.Children[1].Children[0]; if lc2.Kind == KindAddrOf && len(lc2.Children) > 0 { gc := lc2.Children[0]; if gc.Kind == KindIndexExpr && len(gc.Children) > 0 { return gc.Children[0] } } }; return nil }(); gc2 != nil {
									rhsDesc += fmt.Sprintf(" idx-base(kind=%v name=%q type=%v ptee=%v elemType=%v elemPtee=%v)", gc2.Kind, gc2.Name, gc2.Type, gc2.Pointee, gc2.ElemType, gc2.ElemPointee)
								}
								*errs = append(*errs, fmt.Sprintf("line %d: assignment of incompatible pointer types: lhsKind=%v lhs=%q(kind=%v ptee=%v), rhs(%s kind=%v ptee=%v)", n.Line, lhsNodeKind, lhsName, normLhs, lhsPtee, rhsDesc, normRhs, rhsPtee))
						}
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
		// Assignment expression has the type of the LHS (C standard).
		// Propagate pointer metadata so callers (e.g. f(a = b)) see the correct pointee.
		n.Type = lhsType
		if isPtrType(lhsType) {
			n.Pointee = n.Children[0].Pointee
			n.StructTag = n.Children[0].StructTag
		}
		return lhsType

	case KindCompoundAssign:
		lt := checkExpr(n.Children[0], st, errs)
		if n.Children[1] != nil {
			checkExpr(n.Children[1], st, errs)
		}
		n.Type = lt // result type matches lhs (e.g. unsigned int x += y stays unsigned)
		if isPtrType(lt) {
			n.Pointee = n.Children[0].Pointee
		}
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

	case KindCommaExpr:
		checkExpr(n.Children[0], st, errs)
		t := checkExpr(n.Children[1], st, errs)
		n.Type = t
		return t

	case KindTernary:
		checkExpr(n.Children[0], st, errs) // condition
		thenType := checkExpr(n.Children[1], st, errs)
		elseType := checkExpr(n.Children[2], st, errs)
		switch {
		case isFPType(thenType) || isFPType(elseType):
			n.Type = TypeDouble
		case thenType == TypeUnsignedLong || elseType == TypeUnsignedLong:
			n.Type = TypeUnsignedLong
		case thenType == TypeLong || elseType == TypeLong:
			n.Type = TypeLong
		case isPtrType(thenType):
			n.Type = thenType
			n.Pointee = n.Children[1].Pointee
		case isPtrType(elseType):
			n.Type = elseType
			n.Pointee = n.Children[2].Pointee
		case isUnsignedType(thenType) || isUnsignedType(elseType):
			n.Type = TypeUnsignedInt
		default:
			n.Type = TypeInt
		}
		if thenType == TypeStruct && elseType == TypeStruct &&
			n.Children[1].StructTag == n.Children[2].StructTag {
			n.StructTag = n.Children[1].StructTag
		}
		return n.Type

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
				n.StructTag = n.Children[0].StructTag
			} else if isPtrType(rt) && !isPtrType(lt) && n.Op == "+" {
				n.Type = rt
				n.Pointee = n.Children[1].Pointee
				n.StructTag = n.Children[1].StructTag
			} else if lt == TypeIntArray && !isPtrType(rt) && (n.Op == "+" || n.Op == "-") {
				// Array + integer: array decays to pointer, result is pointer.
				n.Type = TypePtr
				lc := n.Children[0]
				if lc.ElemType == TypePtr {
					n.Pointee = ptrCType(lc.ElemPointee)
				} else if lc.ElemType != 0 {
					n.Pointee = leafCType(lc.ElemType)
					if lc.ElemType == TypeStruct {
						n.StructTag = lc.StructTag
						if n.Pointee != nil {
							n.Pointee.Tag = lc.StructTag
						}
					}
				}
			} else if isFPType(lt) || isFPType(rt) {
				n.Type = TypeDouble
			} else if lt == TypeUint128 || rt == TypeUint128 {
				n.Type = TypeUint128
			} else if lt == TypeInt128 || rt == TypeInt128 {
				n.Type = TypeInt128
			} else if lt == TypeUnsignedLong || rt == TypeUnsignedLong {
				n.Type = TypeUnsignedLong
			} else if lt == TypeLong || rt == TypeLong {
				n.Type = TypeLong
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
			// Write back so irgen can find it via n.Children[0].StructTag.
			base.StructTag = baseTag
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
		f := sd.FindFieldDeep(n.Name, st.structDefs)
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
				// Array of pointers: e.g. "const char *qstrs[]" decays to "const char **".
				// The pointee is itself a pointer (TypePtr → ElemPointee).
				n.Pointee = &CType{Kind: TypePtr, Pointee: f.ElemPointee}
			} else {
				n.Pointee = leafCType(et)
			}
		} else {
			n.Type = f.Type
			n.Pointee = f.Pointee
			n.ElemType = f.ElemType
			n.ElemPointee = f.ElemPointee
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

	case KindAlignof:
		var align int
		if len(n.Children) > 0 {
			t := checkExpr(n.Children[0], st, errs)
			align = alignofKind(t)
		} else if n.StructTag != "" {
			align = 8 // struct alignment = 8 (max alignment)
		} else {
			align = alignofKind(n.Type)
		}
		n.Kind = KindNum
		n.Val = align
		n.Type = TypeInt
		n.Children = nil
		n.StructTag = ""
		return TypeInt

	case KindGeneric:
		if len(n.Children) == 0 {
			return TypeInt
		}
		ctrlType := checkExpr(n.Children[0], st, errs)
		// Find the first matching association, then the default.
		var matched *Node
		var deflt *Node
		for _, assoc := range n.Children[1:] {
			if assoc.Name == "default" {
				deflt = assoc.Children[0]
			} else if assoc.Type == ctrlType {
				if matched == nil {
					matched = assoc.Children[0]
				}
			}
		}
		if matched == nil {
			matched = deflt
		}
		if matched == nil {
			*errs = append(*errs, "_Generic: no matching association for controlling expression")
			return TypeInt
		}
		matchedType := checkExpr(matched, st, errs)
		*n = *matched
		return matchedType

	case KindVAArg:
		// va_arg(ap, type) — type-check the ap expression; result type is already
		// recorded in n.Type / n.Pointee by the grammar action.  No further
		// checking needed: ap is a va_list (long*) and we trust the caller.
		checkExpr(n.Children[0], st, errs)
		// n.Type and n.Pointee are already set by the grammar rule; just return.
		return n.Type

	case KindCast:
		// (type)expr — target type already set by the grammar action.
		checkExpr(n.Children[0], st, errs)
		return n.Type

	case KindCompoundLit:
		// (Type){ init_list } — type already annotated by grammar action.
		// Reuse checkInitList exactly as for a named local variable.
		// For &(struct T){...}: n.Type==TypePtr, n.Pointee.Kind==TypeStruct.
		// Build the synthDecl using the inner struct type for init-list checking.
		synthType := n.Type
		synthTag := n.StructTag
		synthElem := n.ElemType
		if n.Type == TypePtr && n.Pointee != nil && n.Pointee.Kind == TypeStruct {
			synthType = TypeStruct
			synthTag = n.Pointee.Tag
		}
		// Infer array size from init list when not explicit.
		if n.Type == TypeIntArray && n.Val == 0 {
			if len(n.Children) > 0 && n.Children[0].Kind == KindInitList {
				n.Val = len(n.Children[0].Children)
			}
			if n.Val == 0 {
				n.Val = 1
			}
		}
		synthDecl := &Node{
			Kind:      KindVarDecl,
			Type:      synthType,
			Pointee:   n.Pointee,
			StructTag: synthTag,
			Val:       n.Val,
			ElemType:  synthElem,
		}
		if len(n.Children) > 0 && n.Children[0].Kind == KindInitList {
			checkInitList(n.Children[0], synthDecl, st, errs)
		}
		return n.Type

	case KindStmtExpr:
		// ({ local_decls... stmts... }) — GCC statement expression.
		if st.locals == nil {
			*errs = append(*errs, "statement expression not allowed at file scope")
			return TypeVoid
		}
		fn := st.currentFunc
		// Step 1: declare and type-check all KindVarDecl children.
		for _, child := range n.Children {
			if child.Kind != KindVarDecl {
				break
			}
			// Resolve TypeTypeof if present.
			if child.Type == TypeTypeof && child.TypeofExpr != nil {
				resolvedType := checkExpr(child.TypeofExpr, st, errs)
				child.Type = resolvedType
				child.Pointee = child.TypeofExpr.Pointee
				child.StructTag = child.TypeofExpr.StructTag
				child.TypeofExpr = nil
			}
			if err := st.declareLocalFull(child, false); err != nil {
				*errs = append(*errs, err.Error())
			} else if child.Type == TypeIntArray {
				st.locals[child.Name].arrSize = child.Val
				st.locals[child.Name].elemType = child.ElemType
				st.locals[child.Name].elemPointee = child.ElemPointee
				st.locals[child.Name].innerDim = child.Dim2
			}
			if len(child.Children) > 0 && child.Children[0].Kind != KindInitList {
				checkExpr(child.Children[0], st, errs)
			}
		}
		// Step 2: type-check all statement children, noting the last one.
		lastType := TypeVoid
		var lastPointee *CType
		var lastStructTag string
		for _, child := range n.Children {
			if child.Kind == KindVarDecl {
				continue
			}
			if child.Kind == KindExprStmt && len(child.Children) > 0 {
				lastType = checkExpr(child.Children[0], st, errs)
				lastPointee = child.Children[0].Pointee
				lastStructTag = child.Children[0].StructTag
			} else {
				if fn != nil {
					checkStmt(child, st, fn, errs)
				}
			}
		}
		// Step 3: annotate the KindStmtExpr node with the result type.
		n.Type = lastType
		n.Pointee = lastPointee
		n.StructTag = lastStructTag
		return lastType

	case KindCall:
		// Check if the callee name is a TypeFuncPtr variable (not a direct function).
		// Also handle pointer-to-function-type (e.g. cmp_t *cmp where cmp_t is a function typedef).
		if e := st.lookup(n.Name); e != nil && (e.typ == TypeFuncPtr ||
			(e.typ == TypePtr && e.pointee != nil && e.pointee.Kind == TypeFuncPtr)) {
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
			if st.libraryMode {
				// Implicit extern declaration (C89 style): assume int return, variadic.
				sig = &FuncSig{Name: n.Name, ReturnType: TypeInt, IsExtern: true, IsVariadic: true}
				st.funcs[n.Name] = sig
			} else {
				*errs = append(*errs, fmt.Sprintf("undefined function '%s'", n.Name))
				n.Type = TypeInt
				return TypeInt
			}
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
					// Prefer ElemType/ElemPointee on the node itself (set by field-access
					// and index-expression type-checking), fall back to symtable lookup.
					if arg.ElemType == TypePtr {
						// Array of pointers (e.g. char *arr[N]) decays to char** (pointer to pointer).
						if arg.ElemPointee != nil {
							effectiveArgPtee = ptrCType(arg.ElemPointee)
						} else {
							effectiveArgPtee = leafCType(TypePtr)
						}
					} else if arg.ElemType == TypeStruct && arg.StructTag != "" {
						effectiveArgPtee = structCType(arg.StructTag)
					} else if arg.ElemType != 0 {
						effectiveArgPtee = leafCType(arg.ElemType)
					} else if e := st.lookup(arg.Name); e != nil {
						if e.elemType == TypePtr {
							if e.elemPointee != nil {
								effectiveArgPtee = ptrCType(e.elemPointee)
							} else {
								effectiveArgPtee = leafCType(TypePtr)
							}
						} else if e.elemType == TypeStruct && e.structTag != "" {
							effectiveArgPtee = structCType(e.structTag)
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
				// Also remember if the raw arg was a string literal — L"..." wide string
				// literals are lexed as char* but used where wchar_t* is expected; treat
				// string literals as void* for pointer-compatibility purposes.
				rawArgIsStringLit := effectiveArgType == TypeCharPtr
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
					// String literals (TypeCharPtr before normalization) are treated as
					// void* for this check: L"..." wide literals are lexed as char* but
					// passed to wchar_t* params — suppress the false mismatch.
					isArgVoid := ctypeIsVoidPtr(effectiveArgPtee) || rawArgIsStringLit
					isParamVoid := ctypeIsVoidPtr(paramPtee)
					if !isArgVoid && !isParamVoid && !ctypeCharCompatible(effectiveArgPtee, paramPtee) {
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
		n.StructTag = sig.ReturnStructTag
		return sig.ReturnType

	case KindFuncPtrCall:
		for _, arg := range n.Children {
			checkExpr(arg, st, errs)
		}
		n.Type = TypeInt // opaque: assume int return
		return TypeInt

	case KindIndirectCall:
		// Children[0] is the callee expression; rest are arguments.
		if len(n.Children) > 0 {
			checkExpr(n.Children[0], st, errs)
			for _, arg := range n.Children[1:] {
				checkExpr(arg, st, errs)
			}
		}
		n.Type = TypeInt // opaque: assume int return
		return TypeInt
	}
	return TypeVoid
}

// checkInitList type-checks a KindInitList node used to initialise decl.
// For struct/union: resolves .field designators → sets entry.Val to byte offset.
// For array: resolves [index] designators → sets entry.Val to element index.
// Plain entries (Op=="") are assigned positions left-to-right.
func checkInitList(list *Node, decl *Node, st *symTable, errs *[]string) {
	switch decl.Type {
	case TypeStruct:
		sd, ok := st.structDefs[decl.StructTag]
		if !ok {
			*errs = append(*errs, fmt.Sprintf("struct '%s' not defined", decl.StructTag))
			return
		}
		checkStructInitList(list, sd, st, errs)
	case TypeIntArray:
		checkArrayInitList(list, decl, st, errs)
	default:
		// Scalar with braces: {expr} — accept, check single plain entry.
		if len(list.Children) == 1 && list.Children[0].Op == "" {
			checkExpr(list.Children[0].Children[0], st, errs)
			list.Children[0].Val = 0
			list.Children[0].Type = decl.Type
		} else {
			ops := ""
			for _, c := range list.Children { ops += "|" + c.Op + ":" + c.Name }
			*errs = append(*errs, fmt.Sprintf("scalar initializer must have exactly one element (decl.Type=%v decl.Tag=%s entries=%d %s)", decl.Type, decl.StructTag, len(list.Children), ops))
		}
	}
}

// checkStructInitList resolves field designators and type-checks entries for a struct init list.
func checkStructInitList(list *Node, sd *StructDef, st *symTable, errs *[]string) {
	// Build a slice of "addressable" fields (skip anonymous wrapper members).
	nonAnonFields := nonAnonFieldList(sd, st.structDefs)
	cursor := 0

	for _, entry := range list.Children {
		switch entry.Op {
		case ".":
			f := sd.FindFieldDeep(entry.Name, st.structDefs)
			if f == nil {
				*errs = append(*errs, fmt.Sprintf("struct has no field '%s'", entry.Name))
				entry.Val = 0
				entry.Type = TypeInt
			} else {
				entry.Val = f.ByteOffset
				entry.Type = f.Type
				entry.StructTag = f.StructTag
				// advance cursor past this field
				for i, nf := range nonAnonFields {
					if nf.Name == f.Name && nf.ByteOffset == f.ByteOffset {
						cursor = i + 1
						break
					}
				}
			}
		case "":
			if cursor < len(nonAnonFields) {
				f := nonAnonFields[cursor]
				entry.Val = f.ByteOffset
				entry.Type = f.Type
				entry.StructTag = f.StructTag
				cursor++
			} else {
				*errs = append(*errs, "too many initializer elements for struct")
			}
		default:
			*errs = append(*errs, fmt.Sprintf("index designator '[%d]' used in struct initializer", entry.Val))
		}
		// Type-check the value.
		if len(entry.Children) > 0 {
			child := entry.Children[0]
			if child.Kind == KindInitList {
				// Nested struct init: synthesize a decl node.
				synthDecl := &Node{Kind: KindVarDecl, Type: entry.Type, StructTag: entry.StructTag}
				checkInitList(child, synthDecl, st, errs)
			} else {
				checkExpr(child, st, errs)
			}
		}
	}
}

// nonAnonFieldList returns the named (non-anonymous) fields of a struct, in order,
// including fields promoted from anonymous sub-structs (flattened).
func nonAnonFieldList(sd *StructDef, structDefs map[string]*StructDef) []StructField {
	var result []StructField
	for _, f := range sd.Fields {
		if f.Name != "" {
			result = append(result, f)
		} else if f.Type == TypeStruct && f.StructTag != "" && structDefs != nil {
			// Anonymous member: flatten its fields (with absolute offsets).
			if inner, ok := structDefs[f.StructTag]; ok {
				for _, innerF := range nonAnonFieldList(inner, structDefs) {
					copy := innerF
					copy.ByteOffset = f.ByteOffset + innerF.ByteOffset
					result = append(result, copy)
				}
			}
		}
	}
	return result
}

// checkArrayInitList resolves index designators and type-checks entries for an array init list.
func checkArrayInitList(list *Node, decl *Node, st *symTable, errs *[]string) {
	arraySize := decl.Val
	nextIdx := 0
	for _, entry := range list.Children {
		switch entry.Op {
		case "[":
			nextIdx = entry.Val
		case "":
			entry.Val = nextIdx
		default:
			*errs = append(*errs, fmt.Sprintf("field designator '.%s' used in array initializer", entry.Name))
		}
		if arraySize > 0 && nextIdx >= arraySize {
			*errs = append(*errs, fmt.Sprintf("array index %d out of bounds (size %d)", nextIdx, arraySize))
		}
		entry.Val = nextIdx
		entry.StructTag = decl.StructTag
		nextIdx++
		if len(entry.Children) > 0 {
			child := entry.Children[0]
			if child.Kind == KindInitList && decl.StructTag != "" {
				// Array of structs: recurse into struct init.
				synthDecl := &Node{Kind: KindVarDecl, Type: TypeStruct, StructTag: decl.StructTag}
				checkInitList(child, synthDecl, st, errs)
			} else {
				checkExpr(child, st, errs)
			}
		}
	}
}
