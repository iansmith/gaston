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
)

// isUnsignedType reports whether t is an unsigned integer type.
func isUnsignedType(t TypeKind) bool {
	return t == TypeUnsignedInt || t == TypeUnsignedChar || t == TypeUnsignedShort
}

// isPtrType reports whether t is a pointer type (holds an address).
func isPtrType(t TypeKind) bool {
	return t == TypeIntPtr || t == TypeCharPtr
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
	KindDeref    // *expr  (pointer dereference, as lvalue or rvalue); Children[0] = pointer expr
	KindAddrOf   // &var   (address-of scalar or array); Children[0] = KindVar
)

// ptrType returns the pointer type corresponding to a base type.
func ptrType(base TypeKind) TypeKind {
	switch base {
	case TypeChar, TypeUnsignedChar:
		return TypeCharPtr
	default: // TypeInt, TypeUnsignedInt, TypeShort, TypeUnsignedShort, TypeVoid → int*
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
	Kind     NodeKind
	Type     TypeKind // resolved type (filled by semcheck)
	Name     string   // identifier
	Val      int      // numeric literal or array size
	Op       string   // binary operator string: "+", "-", "*", "/", "<", "<=", ">", ">=", "==", "!="
	Children []*Node
	Line     int
	IsConst  bool // true for KindVarDecl declared with const
	IsExtern bool // true for extern declarations (var or fun)
}
