// Package main implements the gaston C-minus compiler.
// It reads a .cm source file and emits Plan 9 ARM64 assembly (.s).
package main

// TypeKind is the C-minus type system.
// Pointer types are NOT represented as flat TypeKind constants — they are all
// TypePtr, with the pointee described by a companion *CType on the Node.
type TypeKind int

const (
	TypeVoid         TypeKind = iota
	TypeInt                   // int scalar (4-byte LP64); long / long long are still 8-byte
	TypeIntArray              // int[] — pointer when a param, inline storage when a local
	TypeChar                  // char scalar (1-byte integer)
	TypeCharPtr               // char* — pointer to char (used for string literals; legacy alias for TypePtr+TypeChar pointee)
	TypeUnsignedInt           // unsigned int / unsigned long (64-bit, unsigned)
	TypeUnsignedChar          // unsigned char
	TypeShort                 // short (16-bit signed; stored as 64-bit in frame)
	TypeUnsignedShort         // unsigned short
	TypeFloat                 // float (32-bit IEEE 754; stored as 64-bit double internally)
	TypeDouble                // double (64-bit IEEE 754)
	TypeStruct                // struct (paired with Node.StructTag for struct name)
	TypeFuncPtr               // function pointer — void (*fp)(...); all func ptrs share this type
	TypePtr                   // generic pointer — pointee described by Node.Pointee *CType
)

// CType is the full parameterised representation of a pointer type.
// For a node with Type == TypePtr, CType describes what it points to:
//   - Kind == TypeInt / TypeChar / TypeFloat / ... for scalar pointees
//   - Kind == TypeStruct, Tag = struct name for struct pointees
//   - Kind == TypePtr, Pointee = next level for double/triple pointer chains
//   - Kind == TypeVoid for void*
type CType struct {
	Kind    TypeKind // pointee kind
	Tag     string   // struct/union name (non-empty when Kind == TypeStruct)
	Pointee *CType   // non-nil when Kind == TypePtr (double/triple pointer chain)
}

// leafCType builds a CType for a non-pointer (leaf) type.
func leafCType(kind TypeKind) *CType { return &CType{Kind: kind} }

// structCType builds a CType for a struct/union pointee.
func structCType(tag string) *CType { return &CType{Kind: TypeStruct, Tag: tag} }

// ptrCType builds a CType representing a pointer to the given pointee.
func ptrCType(pointee *CType) *CType { return &CType{Kind: TypePtr, Pointee: pointee} }

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

// ctypeIsVoidPtr reports whether a CType represents void* (a single pointer to void).
func ctypeIsVoidPtr(ct *CType) bool {
	return ct != nil && ct.Kind == TypeVoid
}

// isUnsignedType reports whether t is an unsigned integer type.
func isUnsignedType(t TypeKind) bool {
	return t == TypeUnsignedInt || t == TypeUnsignedChar || t == TypeUnsignedShort
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
	KindReturn    // return [expr];
	KindBreak     // break;
	KindContinue  // continue;
	KindGoto      // goto label;   Name = label name
	KindLabel     // label: stmt   Name = label name; Children[0] = statement

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
	KindFuncPtrCall // (*fp)(args) — call through a function pointer; Name=var, Children=args
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
	KindCast        // (type)expr — explicit type cast; Type/Pointee = target type; Children[0] = source expr

	// TODO: KindTernary — cond ? then : else expression.
	// Parser change: add QUESTION/COLON tokens, new grammar rule in expr.
	// Irgen: emit cond, branch to two blocks, phi-join into result temp.

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
		decl := &Node{Kind: KindVarDecl, Type: ct.Kind, StructTag: ct.Tag, Name: n.Name}
		if ct.Kind == TypePtr {
			decl.Pointee = ct.Pointee
		}
		out[i] = decl
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
	IsConstTarget  bool     // true for pointer declared as const T *p (cannot store through)
	IsStatic       bool     // true for static storage class (local: persistent, global: internal linkage)
	ElemType       TypeKind // for TypeIntArray: element type; TypePtr when array-of-pointers
	ElemPointee    *CType   // for TypeIntArray with ElemType==TypePtr: the pointer's pointee CType
	Dim2           int      // inner dimension for 2D arrays (e.g. for int a[M][N]: Dim2=N)
	BitWidth       int      // bit width for struct bit-field members (0 for normal fields)
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
	IsBitField  bool     // true for struct bit-field members
	BitOffset   int      // bit offset within the 8-byte storage word
	BitWidth    int      // bit width (0 for normal fields)
	IsFlexArray bool     // true for flexible array members (last field, no size)
}

// StructDef describes one named struct or union type and its fields.
type StructDef struct {
	Name    string
	Fields  []StructField
	IsUnion bool // true when this is a union (all fields at offset 0)
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
	default: // double, TypePtr, TypeFuncPtr, TypeCharPtr → 8 bytes
		return 8, 8
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
			sz, a := fieldSizeAlign(f.Type, f.StructTag, structDefs)
			if a > maxAlign {
				maxAlign = a
			}
			end = f.ByteOffset + sz
		}
		if end > rawEnd {
			rawEnd = end
		}
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
