// Package main implements the gaston C-minus compiler.
// It reads a .cm source file and emits Plan 9 ARM64 assembly (.s).
package main

// TypeKind is the C-minus type system.
// Pointer types are NOT represented as flat TypeKind constants — they are all
// TypePtr, with the pointee described by a companion *CType on the Node.
type TypeKind int

const (
	TypeVoid         TypeKind = iota
	TypeInt                   // int scalar (4-byte LP64)
	TypeIntArray              // int[] — pointer when a param, inline storage when a local
	TypeChar                  // char scalar (1-byte integer)
	TypeCharPtr               // char* — pointer to char (used for string literals; legacy alias for TypePtr+TypeChar pointee)
	TypeUnsignedInt           // unsigned int (4-byte LP64, unsigned)
	TypeUnsignedChar          // unsigned char
	TypeShort                 // short (16-bit signed; stored as 64-bit in frame)
	TypeUnsignedShort         // unsigned short
	TypeFloat                 // float (32-bit IEEE 754; stored as 64-bit double internally)
	TypeDouble                // double (64-bit IEEE 754)
	TypeStruct                // struct (paired with Node.StructTag for struct name)
	TypeFuncPtr               // function pointer — void (*fp)(...); all func ptrs share this type
	TypePtr                   // generic pointer — pointee described by Node.Pointee *CType
	TypeLong                  // long / long long (8-byte LP64, signed)
	TypeUnsignedLong          // unsigned long / unsigned long long (8-byte LP64, unsigned)
	TypeTypeof                // sentinel: typeof(expr) — resolved to real type by semcheck
	TypeInt128  // __int128 / __int128_t (signed 128-bit integer, 16 bytes)
	TypeUint128 // __uint128_t / unsigned __int128 (unsigned 128-bit integer, 16 bytes)
)

// CType is the full parameterised representation of a pointer type.
// For a node with Type == TypePtr, CType describes what it points to:
//   - Kind == TypeInt / TypeChar / TypeFloat / ... for scalar pointees
//   - Kind == TypeStruct, Tag = struct name for struct pointees
//   - Kind == TypePtr, Pointee = next level for double/triple pointer chains
//   - Kind == TypeVoid for void*
type CType struct {
	Kind       TypeKind // pointee kind
	Tag        string   // struct/union name (non-empty when Kind == TypeStruct)
	Pointee    *CType   // non-nil when Kind == TypePtr (double/triple pointer chain)
	IsFuncType bool     // true for function-type typedefs (typedef int f(params)), not pointer-to-function
}

// leafCType builds a CType for a non-pointer (leaf) type.
func leafCType(kind TypeKind) *CType { return &CType{Kind: kind} }

// structCType builds a CType for a struct/union pointee.
func structCType(tag string) *CType { return &CType{Kind: TypeStruct, Tag: tag} }

// ptrCType builds a CType representing a pointer to the given pointee.
func ptrCType(pointee *CType) *CType { return &CType{Kind: TypePtr, Pointee: pointee} }

// funcTypeCType builds a CType for a function-type typedef (typedef int f(params)).
// IsFuncType=true distinguishes it from pointer-to-function typedefs.
func funcTypeCType() *CType { return &CType{Kind: TypeFuncPtr, IsFuncType: true} }

// ctypeStructTag returns the struct/union tag from a CType, or "" if not a struct.
func ctypeStructTag(ct *CType) string {
	if ct == nil {
		return ""
	}
	if ct.Kind == TypeStruct {
		return ct.Tag
	}
	return ""
}

// sizeofType returns the byte size of the type represented by ct,
// for use in const_int_expr array dimension evaluation.
// strLitSize returns sizeof(string_literal): the number of bytes in the
// string including the null terminator, matching what C sizeof("...") returns.
// tok is the raw token text including the surrounding double-quotes.
func strLitSize(tok string) int {
	// Strip surrounding quotes.
	if len(tok) < 2 || tok[0] != '"' {
		return 1 // degenerate: just the null terminator
	}
	s := tok[1 : len(tok)-1]
	count := 0
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'x':
				// \xHH — skip up to 2 hex digits
				i += 2
				for j := 0; j < 2 && i < len(s) && isHexDigit(s[i]); j++ {
					i++
				}
			case 'u':
				i += 2
				for j := 0; j < 4 && i < len(s) && isHexDigit(s[i]); j++ {
					i++
				}
			case 'U':
				i += 2
				for j := 0; j < 8 && i < len(s) && isHexDigit(s[i]); j++ {
					i++
				}
			case '0', '1', '2', '3', '4', '5', '6', '7':
				i += 2
				for j := 0; j < 2 && i < len(s) && s[i] >= '0' && s[i] <= '7'; j++ {
					i++
				}
			default:
				i += 2
			}
		} else {
			i++
		}
		count++
	}
	return count + 1 // +1 for null terminator
}

func sizeofType(ct *CType) int {
	if ct == nil {
		return 8
	}
	switch ct.Kind {
	case TypeChar, TypeUnsignedChar:
		return 1
	case TypeShort, TypeUnsignedShort:
		return 2
	case TypeInt, TypeUnsignedInt, TypeFloat:
		return 4
	case TypeLong, TypeUnsignedLong, TypeDouble, TypePtr, TypeFuncPtr:
		return 8
	case TypeInt128, TypeUint128:
		return 16
	default:
		return 8 // structs and unknown: opaque 8-byte placeholder
	}
}

// alignofType returns the alignment in bytes for the type represented by ct.
func alignofType(ct *CType) int {
	if ct == nil {
		return 8
	}
	switch ct.Kind {
	case TypeChar, TypeUnsignedChar:
		return 1
	case TypeShort, TypeUnsignedShort:
		return 2
	case TypeInt, TypeUnsignedInt, TypeFloat:
		return 4
	default:
		return 8 // long, double, ptr, int128, struct → 8
	}
}

// ctypeEq reports whether two CTypes are equivalent.
// nil and nil are equal; nil and non-nil are not.
func ctypeEq(a, b *CType) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Kind != b.Kind || a.Tag != b.Tag {
		return false
	}
	return ctypeEq(a.Pointee, b.Pointee)
}

// ctypeCharCompatible reports whether two leaf CTypes are compatible for call-site
// pointer matching, allowing char ↔ unsigned char interchange (GCC -Wpointer-sign).
func ctypeCharCompatible(a, b *CType) bool {
	if ctypeEq(a, b) {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	isCharKind := func(k TypeKind) bool {
		return k == TypeChar || k == TypeUnsignedChar
	}
	return isCharKind(a.Kind) && isCharKind(b.Kind)
}

// ctypeIsVoidPtr reports whether a CType represents void* (a single pointer to void).
func ctypeIsVoidPtr(ct *CType) bool {
	return ct != nil && ct.Kind == TypeVoid
}

// isUnsignedType reports whether t is an unsigned integer type.
func isUnsignedType(t TypeKind) bool {
	return t == TypeUnsignedInt || t == TypeUnsignedChar || t == TypeUnsignedShort || t == TypeUnsignedLong || t == TypeUint128
}

// isFPType reports whether t is a floating-point type (float or double).
func isFPType(t TypeKind) bool {
	return t == TypeFloat || t == TypeDouble
}

// isPtrType reports whether t is a pointer type (holds an address).
// TypePtr is the generic pointer; TypeFuncPtr is opaque function-pointer;
// TypeCharPtr is retained as a legacy alias for char* string literals.
func isPtrType(t TypeKind) bool {
	return t == TypePtr || t == TypeFuncPtr || t == TypeCharPtr
}

// ptrType returns TypePtr (the single generic pointer kind).
// Used in grammar actions where we need to express "pointer to base type" as a TypeKind.
func ptrType(_ TypeKind) TypeKind { return TypePtr }

// NodeKind identifies the kind of an AST node.
type NodeKind int

const (
	// Top-level
	KindProgram NodeKind = iota

	// Declarations
	KindVarDecl // int x; or int x[N];
	KindFunDecl // type f(params) { body }
	KindParam   // int p or int p[]
	KindFNum    // floating-point literal: FVal = value

	// Statements
	KindCompound  // { decls... stmts... }
	KindExprStmt  // expr; or ;
	KindSelection // if (cond) then [else alt]
	KindIteration // while (cond) body
	KindFor       // for (init; cond; post) body — Children: [init|nil, cond|nil, post|nil, body]
	KindDoWhile   // do body while (cond); — Children: [body, cond]
	KindSwitch    // switch (expr) { cases } — Children: [expr, case1, case2, ...]
	KindCase      // case expr: stmts — Val=-1 for default; Children[0]=expr (if not default), rest=stmts
	KindReturn    // return [expr];
	KindBreak     // break;
	KindContinue  // continue;
	KindGoto         // goto label;     Name = label name
	KindIndirectGoto // goto *expr;     Children[0] = pointer expression (computed goto)
	KindLabel        // label: stmt     Name = label name; Children[0] = statement
	KindLabelAddr    // &&label (GCC):  Name = label name; Type = TypePtr (void*)

	// Expressions
	KindAssign         // var = expr
	KindCompoundAssign // var op= expr  (also x++/x--: Children[1]=nil, Val=1)
	KindBinOp          // expr op expr
	KindVar      // ID  (scalar or array-base reference)
	KindArrayVar  // ID[expr]
	KindIndexExpr // postfix_expr[expr]; Children[0]=base pointer expr, Children[1]=index
	KindCall     // ID(args...)
	KindNum      // integer literal
	KindUnary    // unary operator: Op = "-" or "!"
	KindCharLit  // character literal: Val = rune value (e.g. 'A' = 65)
	KindStrLit   // string literal: Name = string content (NUL not included)
	KindDeref       // *expr  (pointer dereference, as lvalue or rvalue); Children[0] = pointer expr
	KindAddrOf      // &var   (address-of scalar or array); Children[0] = KindVar
	KindFieldAccess // expr->field or expr.field; Children[0]=base; Name=field; Op="->" or "."
	KindStructDef   // struct TAG { fields }; Name=tag; Children=field KindVarDecl nodes; IsUnion=true for unions
	KindSizeof      // sizeof(type) or sizeof(expr); folded to KindNum during semcheck
	KindFuncPtrCall  // (*fp)(args) — call through a function pointer; Name=var, Children=args
	KindIndirectCall // expr(args) — call through arbitrary expression; Children[0]=callee expr, rest=args
	// KindVAArg: va_arg(ap, type) — read next variadic argument and advance ap.
	// Children[0] = ap expression (a va_list / long* local).
	// Type / Pointee / StructTag describe the requested result type (same semantics
	// as a VarDecl node: TypeInt for integer, TypeDouble for double, TypePtr+Pointee
	// for pointer types).  Irgen emits a deref-load + raw advance of 8 bytes.
	KindVAArg
	KindArray2D     // ID[expr][expr] — 2D array subscript; Children[0]=row, Children[1]=col
	KindPostInc     // x++: Children[0]=lvalue; evaluates to old value, then increments
	KindPostDec     // x--: Children[0]=lvalue; evaluates to old value, then decrements
	KindPreInc      // ++x: Children[0]=lvalue; increments, then evaluates to new value
	KindPreDec      // --x: Children[0]=lvalue; decrements, then evaluates to new value
	KindLogAnd      // a && b: short-circuit logical AND; yields 0 or 1 (TypeInt)
	KindLogOr       // a || b: short-circuit logical OR; yields 0 or 1 (TypeInt)
	KindCommaExpr   // (expr, expr) — C comma operator; Children[0]=left (side-effect), Children[1]=right (value)
	KindCast        // (type)expr — explicit type cast; Type/Pointee = target type; Children[0] = source expr
	KindTernary     // cond ? then : else — Children[0]=cond, [1]=then-expr, [2]=else-expr

	// Initializer lists (C99 §6.7.8 designated initializers).
	KindInitList  // { entry, entry, ... } — Children = KindInitEntry nodes in source order
	KindInitEntry // one initializer entry: Op="" plain, Op="." field designator, Op="[" index designator
	               // Name = field name when Op=="."; Val = index when Op=="["; Val = byte offset (set by semcheck)
	               // Children[0] = value expression or nested KindInitList

	// KindCompoundLit: (Type){ init_list } — anonymous temporary with init list (C99 §6.5.2.5).
	// Type / Pointee / StructTag = the declared type (same semantics as a KindVarDecl node:
	//   TypeStruct+StructTag, TypeIntArray+ElemType, TypePtr+Pointee, or a scalar TypeKind).
	// Val = array size (when Type == TypeIntArray).
	// ElemType = element type (when Type == TypeIntArray).
	// Children[0] = KindInitList node (may be empty).
	// Semcheck sets node.Type; irgen returns the IRAddr of the slot.
	KindCompoundLit

	// KindStmtExpr: ({ local_decls... stmts... }) — GCC statement expression.
	// Children = body contents in order (same layout as KindCompound).
	// Type/Pointee/StructTag set by semcheck to match the last expression's type.
	// If last statement is not KindExprStmt (or body is empty), Type = TypeVoid.
	KindStmtExpr

	// KindAlignof: _Alignof(type) or __alignof__(expr) — folded to KindNum during semcheck.
	// Same fields as KindSizeof: Type (for type-based), StructTag (for struct type),
	// Children[0] (for expression-based).
	KindAlignof

	// KindGeneric: C11 _Generic(ctrl-expr, type: expr, ..., default: expr).
	// Children[0] = controlling expression.
	// Children[1..n] = KindGenericAssoc nodes.
	KindGeneric

	// KindGenericAssoc: one association in a _Generic expression.
	// If Name == "default": the default association (Type == TypeVoid).
	// Otherwise: Type/StructTag = the type to match; Children[0] = the value expression.
	KindGenericAssoc

	// TODO: struct return by value (on the stack).
	// Currently all structs must be passed/returned by pointer (printf.cm works
	// around this). Need ABI rules: small structs (≤16 bytes) in X0/X1 registers
	// on AAPCS64; larger structs via hidden pointer argument (caller allocates on
	// stack, passes address in X8).  Irgen must detect TypeStruct return type and
	// emit the appropriate load/store sequence; elfgen must handle the hidden-ptr
	// calling convention.
)

// makeMultiDecl builds one KindVarDecl per name from an id_list of KindVar nodes.
// Used for scalar multi-declarations like "int a, b, c;".
func makeMultiDecl(ct *CType, names []*Node) []*Node {
	out := make([]*Node, len(names))
	for i, n := range names {
		if n.Kind == KindVarDecl {
			// Already a VarDecl (from id_list ',' ID '=' expression): just set the type.
			n.Type = ct.Kind
			n.StructTag = ct.Tag
			n.Pointee = ct.Pointee
			out[i] = n
		} else if n.Type == TypeIntArray {
			// Array element in id_list (e.g. "iq[20]" from "int jz, iq[20], i;")
			out[i] = &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: n.Name,
				Val: n.Val, ElemType: ct.Kind}
		} else {
			decl := &Node{Kind: KindVarDecl, Type: ct.Kind, StructTag: ct.Tag, Name: n.Name}
			if ct.Kind == TypePtr {
				decl.Pointee = ct.Pointee
			}
			out[i] = decl
		}
	}
	return out
}

// makePtrFields creates pointer VarDecl nodes from a ptr_id_list.
// Each name in the list becomes a pointer field with the given pointee type.
func makePtrFields(pointee *CType, names []*Node) []*Node {
	out := make([]*Node, len(names))
	for i, n := range names {
		out[i] = &Node{Kind: KindVarDecl, Type: TypePtr, Name: n.Name}
		out[i].Pointee = pointee
	}
	return out
}

// castNode builds a KindCast node that converts child to the type described by ct.
func castNode(ct *CType, child *Node) *Node {
	n := &Node{Kind: KindCast, Type: ct.Kind, Children: []*Node{child}}
	if ct.Kind == TypePtr {
		n.Pointee = ct.Pointee
	} else {
		n.StructTag = ct.Tag
	}
	return n
}

// constAddrOf returns a positive integer representing the "address" of a named
// global in a compile-time constant expression context (e.g. static asserts).
// Gaston cannot know actual link-time addresses, so this uses a deterministic
// hash of the name.  Two distinct names are extremely unlikely to collide.
// Guarantees: non-zero, and equal iff names are equal.
func constAddrOf(name string) int {
	h := 0x811c9dc5 // FNV-1a basis
	for _, c := range name {
		h ^= int(c)
		h *= 0x01000193 // FNV-1a prime
	}
	if h == 0 {
		h = 1 // never return 0 (null address would be invalid)
	}
	return h
}

// arrayElemPtee returns the ElemPointee to store for an array whose element type
// is described by ct (the base type from a type_specifier).
//   - TypePtr element (e.g. mp_obj_t = void*): returns ct.Pointee so that
//     indexing yields TypePtr with the correct inner pointee.
//   - TypeStruct element: returns ct itself (the full struct CType).
//   - All others: returns nil.
func arrayElemPtee(ct *CType) *CType {
	if ct.Kind == TypePtr {
		return ct.Pointee
	}
	if ct.Kind == TypeStruct {
		return ct
	}
	return nil
}

// ctNode creates a node of the given kind with Type/Pointee/StructTag properly
// initialised from a *CType returned by type_specifier.
// For pointer types (ct.Kind == TypePtr): sets Type=TypePtr, Pointee=ct.Pointee.
// For struct types: sets Type=TypeStruct, StructTag=ct.Tag.
// For all others: sets Type=ct.Kind.
func ctNode(kind NodeKind, ct *CType, name string) *Node {
	n := &Node{Kind: kind, Name: name, Type: ct.Kind}
	if ct.Kind == TypePtr {
		n.Pointee = ct.Pointee
	} else {
		n.StructTag = ct.Tag
	}
	return n
}

// Node is a generic AST node.  Not every field is used by every kind;
// see the comment on each kind in the const block above.
//
//	KindProgram:    Children = declarations
//	KindVarDecl:    Type, Name; Val = array size (0 for scalar); Pointee != nil when Type == TypePtr
//	KindFunDecl:    Type (return type), Name; Children = params... + compound
//	KindParam:      Type (TypeInt or TypeIntArray), Name; Pointee != nil when Type == TypePtr
//	KindCompound:   Children = var_decls... + statements...
//	KindExprStmt:   Children[0] = expr (may be nil)
//	KindSelection:  Children[0]=cond [1]=then [2]=else (optional)
//	KindIteration:  Children[0]=cond [1]=body
//	KindReturn:     Children[0]=expr (optional)
//	KindAssign:     Children[0]=lvar [1]=rexpr
//	KindBinOp:      Op, Children[0]=left [1]=right
//	KindVar:        Name
//	KindArrayVar:   Name, Children[0]=index
//	KindCall:       Name, Children = args
//	KindNum:        Val
type Node struct {
	Kind      NodeKind
	Type      TypeKind // resolved type (filled by semcheck)
	Pointee   *CType   // non-nil when Type == TypePtr: describes the pointee type
	Name      string   // identifier
	Val       int      // numeric literal or array size
	FVal      float64  // floating-point literal value (KindFNum)
	Op        string   // binary operator string: "+", "-", "*", "/", "<", "<=", ">", ">=", "==", "!="
	StructTag string   // for TypeStruct: the struct type name; for TypePtr-to-struct nodes: propagated from Pointee.Tag
	Children  []*Node
	Line      int
	IsConst        bool     // true for KindVarDecl declared with const
	IsExtern       bool     // true for extern declarations (var or fun)
	IsVLA          bool     // true for variable-length array: type ID '[' ID ']'
	IsUnion        bool     // true for KindStructDef that is a union (all fields at offset 0)
	IsPacked       bool     // true for KindStructDef with __attribute__((packed))
	IsConstTarget  bool     // true for pointer declared as const T *p (cannot store through)
	IsStatic       bool     // true for static storage class (local: persistent, global: internal linkage)
	IsWeak         bool     // true for __attribute__((weak)) functions/globals
	AliasTarget    string   // non-empty for __attribute__((alias("target"))): the target symbol name
	SectionName    string   // non-empty for __attribute__((section("name"))): the target ELF section
	Align          int      // non-zero for _Alignas(N): required alignment in bytes
	ElemType       TypeKind // for TypeIntArray: element type; TypePtr when array-of-pointers
	ElemPointee    *CType   // for TypeIntArray with ElemType==TypePtr: the pointer's pointee CType
	Dim2           int      // inner dimension for 2D arrays (e.g. for int a[M][N]: Dim2=N)
	BitWidth       int      // bit width for struct bit-field members (0 for normal fields)
	TypeofExpr     *Node    // non-nil when Type==TypeTypeof: the expression whose type to infer
}

// StructField is one field in a struct definition (or union).
type StructField struct {
	Name        string
	Type        TypeKind
	Pointee     *CType   // non-nil when Type == TypePtr: describes the pointee type
	ElemType    TypeKind // for flex/array members: element type (TypePtr for array-of-pointer fields)
	ElemPointee *CType   // for array fields with ElemType==TypePtr: the pointer's pointee CType
	StructTag   string   // non-empty when Type == TypeStruct (nested struct field name)
	ByteOffset  int      // byte offset within the struct (natural alignment per fieldSizeAlign)
	ByteSize    int      // byte size of this field (set for array fields; 0 means use fieldSizeAlign)
	IsBitField  bool     // true for struct bit-field members
	BitOffset   int      // bit offset within the 8-byte storage word
	BitWidth    int      // bit width (0 for normal fields)
	IsFlexArray bool     // true for flexible array members (last field, no size)
}

// StructDef describes one named struct or union type and its fields.
type StructDef struct {
	Name     string
	Fields   []StructField
	IsUnion  bool // true when this is a union (all fields at offset 0)
	IsPacked bool // true when declared with __attribute__((packed)): no alignment padding
}

// fieldSizeAlign returns the byte size and natural alignment for a field type.
// For TypeStruct fields, structTag and structDefs are used to compute the recursive size.
func fieldSizeAlign(t TypeKind, structTag string, structDefs map[string]*StructDef) (size, align int) {
	switch t {
	case TypeChar, TypeUnsignedChar:
		return 1, 1
	case TypeShort, TypeUnsignedShort:
		return 2, 2
	case TypeInt:
		return 4, 4
	case TypeUnsignedInt:
		return 4, 4
	case TypeFloat:
		return 4, 4
	case TypeStruct:
		if structTag != "" && structDefs != nil {
			if sd, ok := structDefs[structTag]; ok {
				sz := sd.SizeBytes(structDefs)
				if sz > 0 {
					return sz, 8
				}
			}
		}
		return 8, 8
	case TypeTypeof:
		panic("TypeTypeof not resolved before fieldSizeAlign")
	case TypeInt128, TypeUint128:
		return 16, 16 // 16-byte size, 16-byte alignment
	default: // double, TypePtr, TypeFuncPtr, TypeCharPtr, TypeIntArray → 8 bytes
		return 8, 8
	}
}

// arrayFieldSizeAlign computes the (size, align) for an array field inside a struct.
// elemType is the element type, elemStructTag is used for struct element arrays,
// and count is the number of elements.
func arrayFieldSizeAlign(elemType TypeKind, elemStructTag string, count int, structDefs map[string]*StructDef) (size, align int) {
	elemSz, elemAlign := fieldSizeAlign(elemType, elemStructTag, structDefs)
	return elemSz * count, elemAlign
}

// maybeSetTypeofExpr sets n.TypeofExpr from the lexer scratch if the CType is TypeTypeof.
// Called from grammar actions to propagate typeof expressions into declaration nodes.
func maybeSetTypeofExpr(n *Node, ct *CType, l interface{}) {
	if ct.Kind == TypeTypeof {
		if lx, ok := l.(*lexer); ok {
			n.TypeofExpr = lx.typeofExpr
			lx.typeofExpr = nil
		}
	}
}

// SizeBytes returns the total byte size of the struct using natural alignment.
// Each field is placed at the smallest offset >= previous offset that satisfies
// the field's natural alignment (size == alignment for scalar types).
// The total is rounded up to the struct's natural alignment (= max field alignment).
// structDefs is used to recursively resolve the size of nested struct fields.
func (sd *StructDef) SizeBytes(structDefs map[string]*StructDef) int {
	if len(sd.Fields) == 0 {
		return 0
	}
	maxAlign := 1
	rawEnd := 0
	for _, f := range sd.Fields {
		if f.IsFlexArray {
			continue // flexible array member has no size in the struct
		}
		var end int
		if f.IsBitField {
			end = f.ByteOffset + 8 // storage word is always 8 bytes
			if 8 > maxAlign {
				maxAlign = 8
			}
		} else {
			var sz, a int
			if f.ByteSize > 0 {
				sz = f.ByteSize
				_, a = fieldSizeAlign(f.Type, f.StructTag, structDefs)
			} else {
				sz, a = fieldSizeAlign(f.Type, f.StructTag, structDefs)
			}
			if a > maxAlign {
				maxAlign = a
			}
			end = f.ByteOffset + sz
		}
		if end > rawEnd {
			rawEnd = end
		}
	}
	if sd.IsPacked {
		return rawEnd // no trailing alignment padding for packed structs
	}
	return (rawEnd + maxAlign - 1) &^ (maxAlign - 1)
}

// FindField returns the field with the given name, or nil if not found.
func (sd *StructDef) FindField(name string) *StructField {
	for i := range sd.Fields {
		if sd.Fields[i].Name == name {
			return &sd.Fields[i]
		}
	}
	return nil
}

// FindFieldDeep searches for a field by name, looking inside anonymous
// (nameless) embedded struct/union members.  Returns the field with its
// absolute byte offset within the outer struct (anonymous base offset +
// inner field offset), or nil if not found.
// applyTypeToDecls fills in the CType fields on a list of partial VarDecl nodes
// produced by the multi_init_id_list grammar rule, and sets IsStatic/IsConst.
// Nodes already marked TypeIntArray (array items in the list) get ElemType from ct.
func applyTypeToDecls(ct *CType, decls []*Node, isStatic, isConst bool) []*Node {
	for _, d := range decls {
		if d.Type == TypeIntArray {
			// Array item: element type comes from ct; Type stays TypeIntArray.
			d.ElemType = ct.Kind
			d.StructTag = ct.Tag // propagate struct tag so array-of-struct subscript resolves fields
		} else {
			d.Type = ct.Kind
			d.StructTag = ct.Tag
			d.Pointee = ct.Pointee
		}
		d.IsStatic = isStatic
		d.IsConst = isConst
	}
	return decls
}

// ─── DeclSpec / Declarator ─────────────────────────────────────────────────
// These types support the factored C standard grammar:
//   declaration: declaration_specifiers init_declarator_list ';'
// DeclSpec accumulates storage class + qualifiers + base type.
// Declarator describes name + pointer chain + array dims + initializer.

// DeclSpec holds the accumulated declaration specifiers
// (storage class + qualifiers + base type).
type DeclSpec struct {
	BaseType      *CType // resolved base type from type keywords
	IsStatic      bool
	IsExtern      bool
	IsInline      bool   // true for inline / __inline__ / __inline function specifier
	IsConst       bool   // leading const (qualifier on type)
	IsConstTarget bool   // const T *p pattern (pointer target is const)
	IsTypedef     bool
	IsUnion       bool   // true when BaseType came from UNION
	StructDef     *Node  // non-nil if inline struct/union definition
	TypeofExpr    *Node  // non-nil for typeof(expr) — deferred resolution
}

// Declarator describes a single declarator: name, pointer chain, array dims, etc.
type Declarator struct {
	Name       string // "" for abstract declarators (unnamed params)
	PtrChain   *CType // pointer chain (nil=no pointers, ptrCType(nil)=single *, etc.)
	IsConstPtr bool   // T * const p — the pointer itself is const
	IsArray    bool
	ArraySize  int  // dimension; -1 for unsized [], 0 for VLA
	Dim2       int  // inner dimension for 2D arrays
	IsVLA      bool
	VLAExpr    *Node // VLA size expression (non-nil when IsVLA)
	IsFuncPtr  bool  // (*name)(params) function pointer declarator
	Init       *Node // initializer: expression, KindInitList, or KindStrLit
}

// FunDeclarator describes a function declarator: name, optional pointer return,
// parameter list, and whether it's variadic.
type FunDeclarator struct {
	Name       string   // function name
	PtrChain   *CType   // pointer chain for return type (nil = non-pointer return)
	Params     []*Node  // parameter nodes (KindParam)
	IsParenName bool    // true for (name)(params) — parenthesized name to prevent macro expansion
}

// ptrDepth returns the pointer depth of the declarator's PtrChain.
func (d *Declarator) ptrDepth() int {
	n := 0
	for p := d.PtrChain; p != nil; p = p.Pointee {
		n++
	}
	return n
}

// buildPointee resolves the full pointee CType by combining the DeclSpec base type
// with the declarator's pointer chain.
// For single pointer (*): returns ds.BaseType directly.
// For double pointer (**): returns ptrCType(ds.BaseType).
// For triple pointer (***): returns ptrCType(ptrCType(ds.BaseType)).
func buildPointee(ds *DeclSpec, d *Declarator) *CType {
	depth := d.ptrDepth()
	if depth == 0 {
		return nil
	}
	// Start from the base type and wrap in (depth-1) pointer layers.
	result := ds.BaseType
	for i := 1; i < depth; i++ {
		result = ptrCType(result)
	}
	return result
}

// applyDeclToVarNode builds a KindVarDecl Node from a DeclSpec and Declarator.
// The resulting Node has exactly the same field layout as the old grammar actions produce.
func applyDeclToVarNode(ds *DeclSpec, d *Declarator) *Node {
	n := &Node{
		Kind:     KindVarDecl,
		Name:     d.Name,
		IsStatic: ds.IsStatic,
		IsExtern: ds.IsExtern,
	}

	hasPtr := d.PtrChain != nil

	switch {
	case d.IsFuncPtr:
		n.Type = TypeFuncPtr
		if d.Init != nil {
			n.Children = []*Node{d.Init}
		}

	case d.IsArray && hasPtr:
		// Array of pointers: T *name[N]
		n.Type = TypeIntArray
		arrSz := d.ArraySize
		if arrSz == -1 && d.Init != nil {
			arrSz = 0 // unsized with init: size inferred from initializer
		}
		n.Val = arrSz
		n.ElemType = TypePtr
		n.ElemPointee = ds.BaseType
		if d.Init != nil {
			n.Children = []*Node{d.Init}
		}

	case d.IsArray:
		// Plain array: T name[N] or T name[]
		n.Type = TypeIntArray
		arrSz2 := d.ArraySize
		if arrSz2 == -1 && d.Init != nil {
			arrSz2 = 0 // unsized with init: size inferred from initializer
		}
		n.Val = arrSz2
		n.Dim2 = d.Dim2
		n.ElemType = ds.BaseType.Kind
		n.StructTag = ds.BaseType.Tag
		if ds.BaseType.Kind == TypeStruct {
			n.ElemPointee = ds.BaseType
		} else if ds.BaseType.Kind == TypePtr {
			// Typedef'd pointer element (e.g. mp_obj_t = void*): preserve the pointee chain.
			n.ElemPointee = ds.BaseType.Pointee
		}
		if d.IsVLA {
			n.IsVLA = true
			n.Children = []*Node{d.VLAExpr}
		} else if d.Init != nil {
			n.Children = []*Node{d.Init}
		}

	case hasPtr:
		// Pointer: T *name or T **name
		n.Type = TypePtr
		n.Pointee = buildPointee(ds, d)
		if ds.IsConst && d.ptrDepth() == 1 {
			n.IsConstTarget = true
		}
		if d.Init != nil {
			n.Children = []*Node{d.Init}
		}

	default:
		// Plain scalar or struct
		n.Type = ds.BaseType.Kind
		n.StructTag = ds.BaseType.Tag
		if ds.BaseType.Kind == TypePtr {
			n.Pointee = ds.BaseType.Pointee
		}
		if d.Init != nil {
			n.Children = []*Node{d.Init}
		}
	}

	return n
}

// applyDeclToParamNode builds a KindParam Node from a DeclSpec and Declarator.
func applyDeclToParamNode(ds *DeclSpec, d *Declarator) *Node {
	n := &Node{
		Kind: KindParam,
		Name: d.Name,
	}

	hasPtr := d.PtrChain != nil

	switch {
	case d.IsFuncPtr:
		n.Type = TypeFuncPtr

	case d.IsArray && hasPtr:
		// Array of pointers param: T *name[]
		n.Type = TypeIntArray
		n.ElemType = TypePtr
		n.ElemPointee = ds.BaseType
		n.Val = d.ArraySize

	case d.IsArray:
		// Array param: T name[N] or T name[]
		n.Type = TypeIntArray
		n.ElemType = ds.BaseType.Kind
		if ds.BaseType.Kind == TypeStruct {
			n.ElemPointee = ds.BaseType
		}
		n.Val = d.ArraySize

	case hasPtr:
		// Pointer param: T *name or T **name
		n.Type = TypePtr
		n.Pointee = buildPointee(ds, d)
		if ds.IsConst && d.ptrDepth() == 1 {
			n.IsConstTarget = true
		}

	default:
		// Scalar or struct param
		n.Type = ds.BaseType.Kind
		n.StructTag = ds.BaseType.Tag
		if ds.BaseType.Kind == TypePtr {
			n.Pointee = ds.BaseType.Pointee
		}
	}

	return n
}

// applyDeclToFunNode builds a KindFunDecl Node from declaration specifiers,
// a function name, optional pointer return chain, params, and optional body.
// If body is nil, the declaration is treated as a prototype (IsExtern=true).
func applyDeclToFunNode(ds *DeclSpec, name string, retPtrChain *CType, params []*Node, body *Node) *Node {
	n := &Node{
		Kind:     KindFunDecl,
		Name:     name,
		IsStatic: ds.IsStatic,
		IsExtern: ds.IsExtern,
	}

	if retPtrChain != nil {
		// Function returns a pointer type.
		n.Type = TypePtr
		n.Pointee = buildPointee(ds, &Declarator{PtrChain: retPtrChain})
	} else {
		n.Type = ds.BaseType.Kind
		n.StructTag = ds.BaseType.Tag
		if ds.BaseType.Kind == TypePtr {
			n.Pointee = ds.BaseType.Pointee
		}
	}

	if body != nil {
		if ds.IsInline && ds.IsExtern {
			// extern inline function (e.g. gnu_inline pattern in headers): treat as
			// a prototype only. The body is discarded — a real definition in the
			// corresponding .c file will replace it via declareFunc.
			n.IsExtern = true
			n.Children = params
		} else {
			n.IsExtern = false // extern on a definition means external linkage, not prototype
			n.Children = append(params, body)
		}
	} else {
		n.IsExtern = true
		n.Children = params
	}

	return n
}

// buildDeclNodes builds one or more KindVarDecl Nodes from a DeclSpec and a list
// of Declarators (for multi-variable declarations like "int a, b=3, c[10];").
// If ds.StructDef is non-nil (inline struct definition), the StructDef node is
// prepended to the result.
func buildDeclNodes(ds *DeclSpec, decls []*Declarator, lex *lexer) []*Node {
	var result []*Node

	// Emit inline struct/union definition if present.
	if ds.StructDef != nil {
		result = append(result, ds.StructDef)
	}

	for _, d := range decls {
		// When a function-type typedef is used as a declaration (e.g. `wctomb_f __ascii_wctomb;`
		// where `typedef int wctomb_f(char *, wchar_t, mbstate_t *)`), the declaration is a
		// function prototype, not a variable. Emit an extern KindFunDecl so the function is
		// callable but no storage is allocated.
		if ds.BaseType != nil && ds.BaseType.IsFuncType && d.PtrChain == nil && !d.IsFuncPtr {
			n := &Node{
				Kind:     KindFunDecl,
				Name:     d.Name,
				Type:     TypeVoid, // return type unknown; sufficient for call resolution
				IsExtern: true,
			}
			result = append(result, n)
			continue
		}
		n := applyDeclToVarNode(ds, d)
		// Handle typeof expressions.
		if ds.TypeofExpr != nil {
			n.TypeofExpr = ds.TypeofExpr
		}
		result = append(result, n)
	}

	return result
}

// buildFieldNodes builds KindVarDecl Nodes for struct/union fields.
func buildFieldNodes(ds *DeclSpec, decls []*Declarator) []*Node {
	var result []*Node
	if ds.StructDef != nil {
		result = append(result, ds.StructDef)
	}
	for _, d := range decls {
		n := applyDeclToVarNode(ds, d)
		result = append(result, n)
	}
	return result
}

func (sd *StructDef) FindFieldDeep(name string, structDefs map[string]*StructDef) *StructField {
	for i := range sd.Fields {
		f := &sd.Fields[i]
		if f.Name == name {
			return f
		}
		// Anonymous member: recurse into it.
		if f.Name == "" && f.Type == TypeStruct && f.StructTag != "" && structDefs != nil {
			if anon, ok := structDefs[f.StructTag]; ok {
				if inner := anon.FindFieldDeep(name, structDefs); inner != nil {
					// Return a copy with the absolute offset.
					copy := *inner
					copy.ByteOffset = f.ByteOffset + inner.ByteOffset
					return &copy
				}
			}
		}
	}
	return nil
}
