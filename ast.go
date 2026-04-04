// Package main implements the gaston C-minus compiler.
// It reads a .cm source file and emits Plan 9 ARM64 assembly (.s).
package main

// TypeKind is the C-minus type system (int, void, int[]).
type TypeKind int

const (
	TypeVoid         TypeKind = iota
	TypeInt                   // int / long / long long scalar (all 64-bit on ARM64)
	TypeIntArray              // int[] — pointer when a param, inline storage when a local
	TypeChar                  // char scalar (1-byte integer)
	TypeCharPtr               // char* — pointer to char (used for string literals)
	TypeIntPtr                // int*  — pointer to int
	TypeUnsignedInt           // unsigned int / unsigned long (64-bit, unsigned)
	TypeUnsignedChar          // unsigned char
	TypeShort                 // short (16-bit signed; stored as 64-bit in frame)
	TypeUnsignedShort         // unsigned short
	TypeFloat                 // float (32-bit IEEE 754; stored as 64-bit double internally)
	TypeDouble                // double (64-bit IEEE 754)
	TypeStruct                // struct (paired with Node.StructTag for struct name)
	TypeVoidPtr               // void* — untyped pointer (accepts any pointer type in assignment)
	TypeIntPtrPtr             // int** — pointer to pointer to int
	TypeCharPtrPtr            // char** — pointer to pointer to char
	TypeFloatPtr              // float*  — pointer to float
	TypeDoublePtr             // double* — pointer to double
	TypeFuncPtr               // function pointer — void (*fp)(...); all func ptrs share this type
	TypeDoublePtrPtr          // double** — pointer to pointer to double
	TypeFloatPtrPtr           // float**  — pointer to pointer to float
)

// isUnsignedType reports whether t is an unsigned integer type.
func isUnsignedType(t TypeKind) bool {
	return t == TypeUnsignedInt || t == TypeUnsignedChar || t == TypeUnsignedShort
}

// isFPType reports whether t is a floating-point type (float or double).
func isFPType(t TypeKind) bool {
	return t == TypeFloat || t == TypeDouble
}

// isPtrType reports whether t is a pointer type (holds an address).
func isPtrType(t TypeKind) bool {
	return t == TypeIntPtr || t == TypeCharPtr || t == TypeVoidPtr ||
		t == TypeIntPtrPtr || t == TypeCharPtrPtr ||
		t == TypeFloatPtr || t == TypeDoublePtr || t == TypeFuncPtr ||
		t == TypeDoublePtrPtr || t == TypeFloatPtrPtr
}

// isPtrPtrType reports whether t is a double-pointer type.
func isPtrPtrType(t TypeKind) bool {
	return t == TypeIntPtrPtr || t == TypeCharPtrPtr ||
		t == TypeDoublePtrPtr || t == TypeFloatPtrPtr
}

// ptrPtrType returns the double-pointer type for a given single-pointer type.
func ptrPtrType(base TypeKind) TypeKind {
	switch base {
	case TypeCharPtr:
		return TypeCharPtrPtr
	case TypeDoublePtr:
		return TypeDoublePtrPtr
	case TypeFloatPtr:
		return TypeFloatPtrPtr
	default:
		return TypeIntPtrPtr
	}
}

// derefPtrType returns the type obtained by dereferencing a pointer type once.
func derefPtrType(t TypeKind) TypeKind {
	switch t {
	case TypeIntPtrPtr:
		return TypeIntPtr
	case TypeCharPtrPtr:
		return TypeCharPtr
	case TypeDoublePtrPtr:
		return TypeDoublePtr
	case TypeFloatPtrPtr:
		return TypeFloatPtr
	case TypeCharPtr:
		return TypeChar
	case TypeFloatPtr:
		return TypeFloat
	case TypeDoublePtr:
		return TypeDouble
	default: // TypeIntPtr, TypeVoidPtr, etc.
		return TypeInt
	}
}

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
	KindArrayVar // ID[expr]
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
	KindArray2D     // ID[expr][expr] — 2D array subscript; Children[0]=row, Children[1]=col
)

// ptrType returns the pointer type corresponding to a base type.
func ptrType(base TypeKind) TypeKind {
	switch base {
	case TypeChar, TypeUnsignedChar:
		return TypeCharPtr
	case TypeVoid:
		return TypeVoidPtr
	case TypeFloat:
		return TypeFloatPtr
	case TypeDouble:
		return TypeDoublePtr
	default: // TypeInt, TypeUnsignedInt, TypeShort, TypeUnsignedShort → int*
		return TypeIntPtr
	}
}

// makeMultiDecl builds one KindVarDecl per name from an id_list of KindVar nodes.
func makeMultiDecl(typ TypeKind, names []*Node) []*Node {
	out := make([]*Node, len(names))
	for i, n := range names {
		out[i] = &Node{Kind: KindVarDecl, Type: typ, Name: n.Name}
	}
	return out
}

// Node is a generic AST node.  Not every field is used by every kind;
// see the comment on each kind in the const block above.
//
//	KindProgram:    Children = declarations
//	KindVarDecl:    Type, Name; Val = array size (0 for scalar)
//	KindFunDecl:    Type (return type), Name; Children = params... + compound
//	KindParam:      Type (TypeInt or TypeIntArray), Name
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
	Name      string   // identifier
	Val       int      // numeric literal or array size
	FVal      float64  // floating-point literal value (KindFNum)
	Op        string   // binary operator string: "+", "-", "*", "/", "<", "<=", ">", ">=", "==", "!="
	StructTag string   // for TypeStruct/TypeIntPtr-to-struct: the struct type name
	Children  []*Node
	Line      int
	IsConst        bool     // true for KindVarDecl declared with const
	IsExtern       bool     // true for extern declarations (var or fun)
	IsVLA          bool     // true for variable-length array: type ID '[' ID ']'
	IsUnion        bool     // true for KindStructDef that is a union (all fields at offset 0)
	IsConstTarget  bool     // true for pointer declared as const T *p (cannot store through)
	IsStatic       bool     // true for static storage class (local: persistent, global: internal linkage)
	ElemType       TypeKind // for TypeIntArray declarations: element type (e.g. TypeDouble for double arr[N])
	Dim2           int      // inner dimension for 2D arrays (e.g. for int a[M][N]: Dim2=N)
	BitWidth       int      // bit width for struct bit-field members (0 for normal fields)
}

// StructField is one field in a struct definition (or union).
type StructField struct {
	Name        string
	Type        TypeKind
	ElemType    TypeKind // for flex array members: element type
	StructTag   string   // non-empty when Type == TypeStruct or TypeIntPtr-to-struct
	ByteOffset  int      // byte offset within the struct (all fields are 8 bytes)
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
	default: // int, unsigned int, double, all pointer types, TypeFuncPtr → 8 bytes
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
