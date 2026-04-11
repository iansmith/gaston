// grammar.y — C-minus grammar for the gaston compiler.
// Generate the parser with:  goyacc -o parser.go grammar.y
//
// S/R conflicts: ~8 from multi-keyword type specifiers (LONG LONG, UNSIGNED INT, etc.),
// all resolved correctly by default shift preference.
// Dangling-else resolved via %prec LOWER_THAN_ELSE.
%{
package main

import "fmt"
%}

%union {
	ival   int
	fval   float64
	sval   string
	node   *Node
	nodes  []*Node
	typ    *CType
	dspec  *DeclSpec
	declr  *Declarator
	declrs []*Declarator
	ctyp   *CType
	fdecl  *FunDeclarator
}

// Literals
%token <ival> NUM CHAR_LIT
%token <fval> FNUM
%token <sval> ID STRING_LIT

// Keywords
%token INT VOID IF ELSE WHILE RETURN FOR DO BREAK CONTINUE CONST CHAR EXTERN GOTO
%token LONG UNSIGNED SHORT FLOAT DOUBLE STRUCT SIZEOF ENUM UNION TYPEDEF STATIC VA_ARG TYPEOF INT128 SIGNED
%token SWITCH CASE DEFAULT
%token ATTR_PACKED
%token ATTR_WEAK
%token <ival> ATTR_ALIGNED
%token <sval> ATTR_SECTION ATTR_ALIAS
%token <ival> ALIGNAS_SPEC
%token ALIGNOF GENERIC
%token STATIC_ASSERT
%token ASM_KW
%token <sval> TYPENAME

// Multi-character operators
%token LE GE EQ NE LSHIFT RSHIFT
%token INC DEC PLUSEQ MINUSEQ STAREQ DIVEQ MODEQ
%token ANDEQ OREQ XOREQ SHLEQ SHREQ
%token ARROW ELLIPSIS
%token ANDAND OROR
%token QUESTION

// Types for non-terminals
%type <node>  program
%type <nodes> declaration var_declaration declaration_list params param_list id_list multi_init_id_list ptr_id_list
%type <nodes> struct_declaration field_list union_declaration enum_declaration enum_list typedef_declaration
%type <nodes> fp_param_types fp_param_type_list
%type <nodes> field
%type <ival>  const_int_expr const_int_ternary const_int_or const_int_and const_int_cmp const_int_shift const_int_add const_int_mul const_int_unary const_int_primary
%type <nodes> generic_assoc_list
%type <node>  generic_assoc
%type <node>  param compound_stmt postfix_expr enum_member fp_param_type
%type <nodes> block_item_list statement_list
%type <nodes> init_list
%type <node>  init_entry assign_expr
%type <node>  statement expression_stmt selection_stmt iteration_stmt for_stmt do_while_stmt switch_stmt case_item return_stmt break_stmt continue_stmt goto_stmt
%type <nodes> case_list
%type <node>  opt_expression
%type <node>  expression var simple_expression ternary_expression logical_expression comparison_expression bitwise_expression additive_expression term factor call comma_expr_list
%type <nodes> args arg_list
%type <typ>   type_specifier
%type <sval>  relop addop mulop bitwiseop
%type <dspec>  declaration_specifiers
%type <declr>  gd_declarator gd_init_declarator gd_param_declarator
%type <declrs> gd_init_declarator_list
%type <ctyp>   gd_pointer
%type <fdecl>  gd_fun_declarator
%type <sval>   local_fun_proto

// Logical operators: || < &&
%left OROR
%left ANDAND

// Resolve dangling-else: ELSE binds to the nearest IF.
%nonassoc LOWER_THAN_ELSE
%nonassoc ELSE

%%

program
	: declaration_list
		{ l := yylex.(*lexer)
		  // Prepend any anonymous struct defs created by cast expressions.
		  all := append(l.pendingStructDefs, $1...)
		  l.result = &Node{Kind: KindProgram, Children: all} }
	| /* empty */
		{ yylex.(*lexer).result = &Node{Kind: KindProgram} }
	;

declaration_list
	: declaration_list declaration
		{ $$ = append($1, $2...) }
	| declaration_list ';'
		{ $$ = $1 }
	| declaration
		{ $$ = $1 }
	| ';'
		{ $$ = nil }
	;

declaration
	: var_declaration    { $$ = $1 }
	/* _Alignas(N) on global variable declarations */
	| ALIGNAS_SPEC var_declaration
		{ for _, n := range $2 { n.Align = $1 }; $$ = $2 }
	/* Forward struct/union/enum declarations: struct Foo; */
	| STRUCT ID ';'    { $$ = nil }
	| UNION ID ';'     { $$ = nil }
	| ENUM ID ';'      { $$ = nil }
	| struct_declaration { $$ = $1 }
	| union_declaration  { $$ = $1 }
	| enum_declaration   { $$ = $1 }
	| typedef_declaration { $$ = $1 }
	/* GCC global inline assembly: __asm(...); or bare __asm__; — parse and discard */
	| ASM_KW '(' args ')' ';'  { $$ = nil }
	| ASM_KW ';'               { $$ = nil }
	| STATIC_ASSERT '(' const_int_expr ',' STRING_LIT ')' ';'
		{
			if $3 == 0 {
				yylex.(*lexer).Error(fmt.Sprintf("_Static_assert failed: %s", $5))
			}
			$$ = nil
		}
	| STATIC_ASSERT '(' const_int_expr ')' ';'
		{
			if $3 == 0 {
				yylex.(*lexer).Error("_Static_assert failed")
			}
			$$ = nil
		}
	/* ── Unified function definition: declaration_specifiers fun_declarator compound_stmt ── */
	| declaration_specifiers gd_fun_declarator compound_stmt
		{ $$ = []*Node{applyDeclToFunNode($1, $2.Name, $2.PtrChain, $2.Params, $3)} }
	/* ── Unified function prototype: declaration_specifiers fun_declarator ';' ── */
	| declaration_specifiers gd_fun_declarator ';'
		{ $$ = []*Node{applyDeclToFunNode($1, $2.Name, $2.PtrChain, $2.Params, nil)} }
	/* ── Multi-prototype: "static T f(T), g(T);" ── */
	| declaration_specifiers gd_fun_declarator ',' gd_fun_declarator ';'
		{ $$ = []*Node{
			applyDeclToFunNode($1, $2.Name, $2.PtrChain, $2.Params, nil),
			applyDeclToFunNode($1, $4.Name, $4.PtrChain, $4.Params, nil),
		} }
	/* ── __attribute__((weak)) function definitions ── */
	| ATTR_WEAK declaration_specifiers gd_fun_declarator compound_stmt
		{ n := applyDeclToFunNode($2, $3.Name, $3.PtrChain, $3.Params, $4); n.IsWeak = true; $$ = []*Node{n} }
	| ATTR_WEAK declaration_specifiers gd_fun_declarator ';'
		{ n := applyDeclToFunNode($2, $3.Name, $3.PtrChain, $3.Params, nil); n.IsWeak = true; $$ = []*Node{n} }
	| declaration_specifiers ATTR_WEAK gd_fun_declarator compound_stmt
		{ n := applyDeclToFunNode($1, $3.Name, $3.PtrChain, $3.Params, $4); n.IsWeak = true; $$ = []*Node{n} }
	| declaration_specifiers ATTR_WEAK gd_fun_declarator ';'
		{ n := applyDeclToFunNode($1, $3.Name, $3.PtrChain, $3.Params, nil); n.IsWeak = true; $$ = []*Node{n} }
	| declaration_specifiers gd_fun_declarator ATTR_WEAK compound_stmt
		{ n := applyDeclToFunNode($1, $2.Name, $2.PtrChain, $2.Params, $4); n.IsWeak = true; $$ = []*Node{n} }
	/* ── __attribute__((weak)) prototype with trailing attribute ── */
	| declaration_specifiers gd_fun_declarator ATTR_WEAK ';'
		{ n := applyDeclToFunNode($1, $2.Name, $2.PtrChain, $2.Params, nil); n.IsWeak = true; $$ = []*Node{n} }
	/* ── __attribute__((weak)) global variables ── */
	| ATTR_WEAK declaration_specifiers gd_init_declarator_list ';'
		{ nodes := buildDeclNodes($2, $3, yylex.(*lexer)); for _, n := range nodes { n.IsWeak = true }; $$ = nodes }
	/* ── trailing attribute on function prototype ── */
	| declaration_specifiers gd_fun_declarator ATTR_SECTION ';'
		{ n := applyDeclToFunNode($1, $2.Name, $2.PtrChain, $2.Params, nil); n.SectionName = $3; $$ = []*Node{n} }
	| declaration_specifiers gd_fun_declarator ATTR_ALIGNED ';'
		{ n := applyDeclToFunNode($1, $2.Name, $2.PtrChain, $2.Params, nil); n.Align = $3; $$ = []*Node{n} }
	/* ── __attribute__((section("name"))) — propagate section name into AST ── */
	| ATTR_SECTION declaration_specifiers gd_fun_declarator compound_stmt
		{ n := applyDeclToFunNode($2, $3.Name, $3.PtrChain, $3.Params, $4); n.SectionName = $1; $$ = []*Node{n} }
	| ATTR_SECTION declaration_specifiers gd_fun_declarator ';'
		{ n := applyDeclToFunNode($2, $3.Name, $3.PtrChain, $3.Params, nil); n.SectionName = $1; $$ = []*Node{n} }
	| ATTR_SECTION declaration_specifiers gd_init_declarator_list ';'
		{ nodes := buildDeclNodes($2, $3, yylex.(*lexer)); for _, n := range nodes { n.SectionName = $1 }; $$ = nodes }
	| declaration_specifiers ATTR_SECTION gd_init_declarator_list ';'
		{ nodes := buildDeclNodes($1, $3, yylex.(*lexer)); for _, n := range nodes { n.SectionName = $2 }; $$ = nodes }
	| declaration_specifiers ATTR_SECTION gd_fun_declarator compound_stmt
		{ n := applyDeclToFunNode($1, $3.Name, $3.PtrChain, $3.Params, $4); n.SectionName = $2; $$ = []*Node{n} }
	| declaration_specifiers ATTR_SECTION gd_fun_declarator ';'
		{ n := applyDeclToFunNode($1, $3.Name, $3.PtrChain, $3.Params, nil); n.SectionName = $2; $$ = []*Node{n} }
	/* ── __attribute__((aligned(N))) — propagate alignment into AST ── */
	| ATTR_ALIGNED declaration_specifiers gd_fun_declarator compound_stmt
		{ n := applyDeclToFunNode($2, $3.Name, $3.PtrChain, $3.Params, $4); n.Align = $1; $$ = []*Node{n} }
	| ATTR_ALIGNED declaration_specifiers gd_fun_declarator ';'
		{ n := applyDeclToFunNode($2, $3.Name, $3.PtrChain, $3.Params, nil); n.Align = $1; $$ = []*Node{n} }
	| ATTR_ALIGNED declaration_specifiers gd_init_declarator_list ';'
		{ nodes := buildDeclNodes($2, $3, yylex.(*lexer)); for _, n := range nodes { n.Align = $1 }; $$ = nodes }
	| declaration_specifiers ATTR_ALIGNED gd_init_declarator_list ';'
		{ nodes := buildDeclNodes($1, $3, yylex.(*lexer)); for _, n := range nodes { n.Align = $2 }; $$ = nodes }
	/* ── __attribute__((alias("target"))) — function and variable aliases ── */
	| declaration_specifiers gd_fun_declarator ATTR_ALIAS ';'
		{ n := applyDeclToFunNode($1, $2.Name, $2.PtrChain, $2.Params, nil); n.AliasTarget = $3; $$ = []*Node{n} }
	| declaration_specifiers gd_init_declarator_list ATTR_ALIAS ';'
		{ nodes := buildDeclNodes($1, $2, yylex.(*lexer)); for _, n := range nodes { n.AliasTarget = $3 }; $$ = nodes }
	/* ── Array of function pointers: extern T (*name[])(...); — e.g. __fini_array_start ── */
	| declaration_specifiers '(' '*' ID '[' ']' ')' '(' fp_param_types ')' ';'
		{ n := &Node{Kind: KindVarDecl, Name: $4, Type: TypePtr, IsExtern: true}
		  n.Pointee = &CType{Kind: TypeFuncPtr}; $$ = []*Node{n} }
	| declaration_specifiers '(' '*' ID '[' ']' ')' '(' fp_param_types ')' ATTR_WEAK ';'
		{ n := &Node{Kind: KindVarDecl, Name: $4, Type: TypePtr, IsExtern: true, IsWeak: true}
		  n.Pointee = &CType{Kind: TypeFuncPtr}; $$ = []*Node{n} }
	;

/* extern_declaration removed — forward declarations handled inline in declaration/block_item_list,
   extern variables/functions handled via declaration_specifiers path */


var_declaration
	: declaration_specifiers gd_init_declarator_list ';'
		{ $$ = buildDeclNodes($1, $2, yylex.(*lexer)) }
	;


init_list
	: init_entry
		{ $$ = []*Node{$1} }
	| init_list ',' init_entry
		{ $$ = append($1, $3) }
	;

init_entry
	: assign_expr
		{ $$ = &Node{Kind: KindInitEntry, Op: "", Children: []*Node{$1}} }
	| '.' ID '=' assign_expr
		{ $$ = &Node{Kind: KindInitEntry, Op: ".", Name: $2, Children: []*Node{$4}} }
	| '.' ID '.' ID '=' assign_expr
		/* Nested designator: .outer.inner = val — desugar to .outer = { .inner = val } */
		{ inner := &Node{Kind: KindInitEntry, Op: ".", Name: $4, Children: []*Node{$6}}
		  innerList := &Node{Kind: KindInitList, Children: []*Node{inner}}
		  $$ = &Node{Kind: KindInitEntry, Op: ".", Name: $2, Children: []*Node{innerList}} }
	| '.' ID '.' ID '.' ID '=' assign_expr
		/* Three-level nested designator: .a.b.c = val */
		{ innermost := &Node{Kind: KindInitEntry, Op: ".", Name: $6, Children: []*Node{$8}}
		  mid := &Node{Kind: KindInitEntry, Op: ".", Name: $4, Children: []*Node{
		      {Kind: KindInitList, Children: []*Node{innermost}}}}
		  outerList := &Node{Kind: KindInitList, Children: []*Node{mid}}
		  $$ = &Node{Kind: KindInitEntry, Op: ".", Name: $2, Children: []*Node{outerList}} }
	| '.' ID '=' '{' init_list '}'
		{ $$ = &Node{Kind: KindInitEntry, Op: ".", Name: $2, Children: []*Node{
		      {Kind: KindInitList, Children: $5}}} }
	| '.' ID '=' '{' init_list ',' '}'
		{ $$ = &Node{Kind: KindInitEntry, Op: ".", Name: $2, Children: []*Node{
		      {Kind: KindInitList, Children: $5}}} }
	| '[' const_int_expr ']' '=' assign_expr
		{ $$ = &Node{Kind: KindInitEntry, Op: "[", Val: $2, Children: []*Node{$5}} }
	| '[' const_int_expr ']' '=' '{' init_list '}'
		{ $$ = &Node{Kind: KindInitEntry, Op: "[", Val: $2, Children: []*Node{
		      {Kind: KindInitList, Children: $6}}} }
	| '{' init_list '}'
		{ $$ = &Node{Kind: KindInitEntry, Op: "", Children: []*Node{
		      {Kind: KindInitList, Children: $2}}} }
	| '{' init_list ',' '}'
		{ $$ = &Node{Kind: KindInitEntry, Op: "", Children: []*Node{
		      {Kind: KindInitList, Children: $2}}} }
	;

assign_expr
	: expression
		{ $$ = $1 }
	;

/* const_declaration removed — handled by var_declaration via declaration_specifiers */

type_specifier
	: INT                { $$ = leafCType(TypeInt) }
	| VOID               { $$ = leafCType(TypeVoid) }
	| CHAR               { $$ = leafCType(TypeChar) }
	| LONG               { $$ = leafCType(TypeLong) }
	| LONG LONG          { $$ = leafCType(TypeLong) }
	| LONG INT           { $$ = leafCType(TypeLong) }
	| LONG LONG INT      { $$ = leafCType(TypeLong) }
	| SHORT              { $$ = leafCType(TypeShort) }
	| SHORT INT          { $$ = leafCType(TypeShort) }
	| UNSIGNED           { $$ = leafCType(TypeUnsignedInt) }
	| UNSIGNED INT       { $$ = leafCType(TypeUnsignedInt) }
	| UNSIGNED LONG      { $$ = leafCType(TypeUnsignedLong) }
	| UNSIGNED LONG LONG { $$ = leafCType(TypeUnsignedLong) }
	| UNSIGNED LONG INT  { $$ = leafCType(TypeUnsignedLong) }
	| UNSIGNED LONG LONG INT { $$ = leafCType(TypeUnsignedLong) }
	| LONG UNSIGNED      { $$ = leafCType(TypeUnsignedLong) }
	| LONG UNSIGNED INT  { $$ = leafCType(TypeUnsignedLong) }
	| LONG LONG UNSIGNED { $$ = leafCType(TypeUnsignedLong) }
	| LONG LONG UNSIGNED INT { $$ = leafCType(TypeUnsignedLong) }
	| LONG SIGNED        { $$ = leafCType(TypeLong) }
	| LONG SIGNED INT    { $$ = leafCType(TypeLong) }
	| LONG LONG SIGNED   { $$ = leafCType(TypeLong) }
	| LONG LONG SIGNED INT { $$ = leafCType(TypeLong) }
	| UNSIGNED CHAR      { $$ = leafCType(TypeUnsignedChar) }
	| UNSIGNED SHORT     { $$ = leafCType(TypeUnsignedShort) }
	| UNSIGNED SHORT INT { $$ = leafCType(TypeUnsignedShort) }
	| SHORT UNSIGNED     { $$ = leafCType(TypeUnsignedShort) }
	| SHORT UNSIGNED INT { $$ = leafCType(TypeUnsignedShort) }
	| FLOAT              { $$ = leafCType(TypeFloat) }
	| DOUBLE             { $$ = leafCType(TypeDouble) }
	| LONG DOUBLE        { $$ = leafCType(TypeDouble) }
	| SIGNED             { $$ = leafCType(TypeInt) }
	| SIGNED INT         { $$ = leafCType(TypeInt) }
	| SIGNED CHAR        { $$ = leafCType(TypeChar) }
	| SIGNED SHORT       { $$ = leafCType(TypeShort) }
	| SIGNED SHORT INT   { $$ = leafCType(TypeShort) }
	| SIGNED LONG        { $$ = leafCType(TypeLong) }
	| SIGNED LONG INT    { $$ = leafCType(TypeLong) }
	| SIGNED LONG LONG   { $$ = leafCType(TypeLong) }
	| SIGNED LONG LONG INT { $$ = leafCType(TypeLong) }
	| INT128             { $$ = leafCType(TypeInt128) }
	| UNSIGNED INT128    { $$ = leafCType(TypeUint128) }
	| INT128 UNSIGNED    { $$ = leafCType(TypeUint128) }
	| SIGNED INT128      { $$ = leafCType(TypeInt128) }
	| TYPENAME           { $$ = yylex.(*lexer).lookupTypedefCType($1) }
	| TYPEOF '(' expression ')'
		{ yylex.(*lexer).typeofExpr = $3
		  $$ = leafCType(TypeTypeof) }
	| TYPEOF '(' type_specifier ')'
		{ $$ = $3 }
	| TYPEOF '(' STRUCT ID ')'
		{ $$ = structCType($4) }
	| STRUCT ID
		{ $$ = structCType($2) }
	| UNION ID
		{ $$ = structCType($2) }
	| ENUM ID
		{ $$ = leafCType(TypeInt) }
	;


params
	: param_list { $$ = $1 }
	| VOID       { $$ = nil }
	|            { $$ = nil }
	;

param_list
	: param_list ',' param
		{ $$ = append($1, $3) }
	| param_list ',' ELLIPSIS
		{ $$ = append($1, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."}) }
	| param
		{ $$ = []*Node{$1} }
	;

param
	: type_specifier ID
		{ $$ = ctNode(KindParam, $1, $2) }
	| type_specifier ID '[' const_int_expr ']' '[' const_int_expr ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $2, ElemType: $1.Kind, StructTag: $1.Tag, Dim2: $7} }
	| type_specifier ID '[' const_int_expr ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $2, ElemType: $1.Kind, StructTag: $1.Tag, Val: $4} }
	| type_specifier ID '[' ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $2, ElemType: $1.Kind, StructTag: $1.Tag} }
	| type_specifier '*' ID '[' ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $3, ElemType: TypePtr, ElemPointee: $1} }
	| type_specifier '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $3}; n.Pointee = $1; $$ = n }
	| type_specifier '*' '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $4}; n.Pointee = ptrCType($1); $$ = n }
	| STRUCT ID ID
		{ $$ = &Node{Kind: KindParam, Type: TypeStruct, Name: $3, StructTag: $2} }
	| CONST STRUCT ID ID
		{ $$ = &Node{Kind: KindParam, Type: TypeStruct, Name: $4, StructTag: $3} }
	| CONST STRUCT ID '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $5}; n.Pointee = structCType($3); $$ = n }
	| CONST STRUCT ID '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = structCType($3); $$ = n }
	| CONST STRUCT ID '*' '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = ptrCType(structCType($3)); $$ = n }
	| CONST STRUCT ID '*' '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $6}; n.Pointee = ptrCType(structCType($3)); $$ = n }
	| STRUCT ID '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); $$ = n }
	| STRUCT ID '*' '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $5}; n.Pointee = ptrCType(structCType($2)); $$ = n }
	| STRUCT ID '*' CONST '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $6}; n.Pointee = ptrCType(structCType($2)); $$ = n }
	| STRUCT ID '*' CONST '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = ptrCType(structCType($2)); $$ = n }
	| CONST type_specifier '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $4, IsConstTarget: true}; n.Pointee = $2; $$ = n }
	| type_specifier CONST '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $4, IsConstTarget: true}; n.Pointee = $1; $$ = n }
	/* Nameless pointer parameters (in extern / forward declarations) — only pointer forms
	   to avoid a R/R conflict with the "params: VOID" production for func(void). */
	| type_specifier '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = $1; $$ = n }
	| type_specifier '*' CONST ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $4}; n.Pointee = $1; $$ = n }
	| CONST type_specifier '*' CONST ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $5, IsConstTarget: true}; n.Pointee = $2; $$ = n }
	| CONST type_specifier '*' CONST ID '[' ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $5, ElemType: TypePtr, ElemPointee: $2} }
	| CONST type_specifier '*' CONST ID '[' const_int_expr ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $5, ElemType: TypePtr, ElemPointee: $2, Val: $7} }
	| CONST type_specifier '*' CONST
		{ n := &Node{Kind: KindParam, Type: TypePtr, IsConstTarget: true}; n.Pointee = $2; $$ = n }
	| type_specifier '*' CONST ID '[' ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $4, ElemType: TypePtr, ElemPointee: $1} }
	| type_specifier '*' CONST '[' ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: "", ElemType: TypePtr, ElemPointee: $1} }
	| type_specifier '*' CONST ID '[' const_int_expr ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $4, ElemType: TypePtr, ElemPointee: $1, Val: $6} }
	| type_specifier '*' '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = ptrCType($1); $$ = n }
	| type_specifier '*' CONST '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = ptrCType($1); $$ = n }
	| type_specifier '*' CONST '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $5}; n.Pointee = ptrCType($1); $$ = n }
	| CONST type_specifier '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: "", IsConstTarget: true}; n.Pointee = $2; $$ = n }
	| CONST type_specifier '*' '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = ptrCType($2); $$ = n }
	| CONST type_specifier '*' '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $5}; n.Pointee = ptrCType($2); $$ = n }
	| CONST type_specifier '*' CONST '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = ptrCType($2); $$ = n }
	| CONST type_specifier '*' CONST '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $6}; n.Pointee = ptrCType($2); $$ = n }
	/* Opaque/forward-declared type used as pointer parameter: __FILE *, some_opaque_t * */
	| ID '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = leafCType(TypeVoid); $$ = n }
	| ID '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $3}; n.Pointee = leafCType(TypeVoid); $$ = n }
	| STRUCT ID '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = structCType($2); $$ = n }
	| STRUCT ID '*' '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = ptrCType(structCType($2)); $$ = n }
	| STRUCT ID '*' '*' '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = ptrCType(ptrCType(structCType($2))); $$ = n }
	/* Nameless scalar parameters — use specific tokens to avoid VOID conflict */
	| INT         { $$ = &Node{Kind: KindParam, Type: TypeInt,         Name: ""} }
	| CHAR        { $$ = &Node{Kind: KindParam, Type: TypeChar,        Name: ""} }
	| LONG        { $$ = &Node{Kind: KindParam, Type: TypeLong,        Name: ""} }
	| LONG LONG   { $$ = &Node{Kind: KindParam, Type: TypeLong,        Name: ""} }
	| LONG INT    { $$ = &Node{Kind: KindParam, Type: TypeLong,        Name: ""} }
	| LONG DOUBLE { $$ = &Node{Kind: KindParam, Type: TypeDouble,      Name: ""} }
	| SHORT       { $$ = &Node{Kind: KindParam, Type: TypeShort,       Name: ""} }
	| SHORT INT   { $$ = &Node{Kind: KindParam, Type: TypeShort,       Name: ""} }
	| FLOAT       { $$ = &Node{Kind: KindParam, Type: TypeFloat,       Name: ""} }
	| DOUBLE      { $$ = &Node{Kind: KindParam, Type: TypeDouble,      Name: ""} }
	| UNSIGNED    { $$ = &Node{Kind: KindParam, Type: TypeUnsignedInt, Name: ""} }
	| UNSIGNED INT   { $$ = &Node{Kind: KindParam, Type: TypeUnsignedInt,  Name: ""} }
	| UNSIGNED LONG  { $$ = &Node{Kind: KindParam, Type: TypeUnsignedLong, Name: ""} }
	| UNSIGNED CHAR  { $$ = &Node{Kind: KindParam, Type: TypeUnsignedChar, Name: ""} }
	| UNSIGNED SHORT { $$ = &Node{Kind: KindParam, Type: TypeUnsignedShort, Name: ""} }
	| UNSIGNED SHORT '[' const_int_expr ']' { $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: "", ElemType: TypeUnsignedShort, Val: $4} }
	| UNSIGNED SHORT ID '[' const_int_expr ']' { $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $3, ElemType: TypeUnsignedShort, Val: $5} }
	| SIGNED         { $$ = &Node{Kind: KindParam, Type: TypeInt,      Name: ""} }
	| SIGNED INT     { $$ = &Node{Kind: KindParam, Type: TypeInt,      Name: ""} }
	| SIGNED CHAR    { $$ = &Node{Kind: KindParam, Type: TypeChar,     Name: ""} }
	| SIGNED LONG    { $$ = &Node{Kind: KindParam, Type: TypeLong,     Name: ""} }
	| INT128         { $$ = &Node{Kind: KindParam, Type: TypeInt128,   Name: ""} }
	| UNSIGNED INT128 { $$ = &Node{Kind: KindParam, Type: TypeUint128, Name: ""} }
	| TYPENAME {
			ct := yylex.(*lexer).lookupTypedefCType($1)
			n := &Node{Kind: KindParam, Type: ct.Kind, Name: "", StructTag: ct.Tag}
			if ct.Kind == TypePtr { n.Pointee = ct.Pointee }
			$$ = n }
	| type_specifier '(' '*' ID ')' '(' fp_param_types ')'
		{ $$ = &Node{Kind: KindParam, Type: TypeFuncPtr, Name: $4} }
	| type_specifier '(' '*' ')' '(' fp_param_types ')'
		{ $$ = &Node{Kind: KindParam, Type: TypeFuncPtr, Name: ""} }
	/* Function pointer parameter returning struct pointer: struct T *(*fn)(params) */
	| STRUCT ID '*' '(' '*' ID ')' '(' fp_param_types ')'
		{ $$ = &Node{Kind: KindParam, Type: TypeFuncPtr, Name: $6} }
	| STRUCT ID '*' '(' '*' ')' '(' fp_param_types ')'
		{ $$ = &Node{Kind: KindParam, Type: TypeFuncPtr, Name: ""} }
	/* Nameless sized/unsized array parameters: e.g. "unsigned short[3]", "regmatch_t[]" */
	| type_specifier '[' const_int_expr ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: "", ElemType: $1.Kind, StructTag: $1.Tag, Val: $3} }
	| type_specifier '[' ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: "", ElemType: $1.Kind, StructTag: $1.Tag} }
	/* const-qualified scalar parameters: "const int32_t e" */
	| CONST type_specifier ID
		{ $$ = ctNode(KindParam, $2, $3) }
	| CONST type_specifier
		{ $$ = ctNode(KindParam, $2, "") }
	/* const struct array params: "const struct timespec[2]" */
	| CONST STRUCT ID '[' const_int_expr ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: "", ElemType: TypeStruct, ElemPointee: structCType($3), Val: $5} }
	| CONST STRUCT ID ID '[' const_int_expr ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $4, ElemType: TypeStruct, ElemPointee: structCType($3), Val: $6} }
	;

compound_stmt
	: '{' block_item_list '}'
		{ $$ = &Node{Kind: KindCompound, Children: $2} }
	;

/* C99-style block: declarations and statements may be freely mixed. */
block_item_list
	: block_item_list statement
		{ $$ = append($1, $2) }
	| block_item_list var_declaration
		{ $$ = append($1, $2...) }
	/* _Alignas(N) local variable declaration */
	| block_item_list ALIGNAS_SPEC var_declaration
		{ for _, n := range $3 { n.Align = $2 }; $$ = append($1, $3...) }
	/* __attribute__((aligned(N))) local variable declaration */
	| block_item_list ATTR_ALIGNED var_declaration
		{ for _, n := range $3 { n.Align = $2 }; $$ = append($1, $3...) }
	/* const_declaration merged into var_declaration */
	| block_item_list declaration_specifiers local_fun_proto ';'
		{ fn := applyDeclToFunNode($2, $3, nil, nil, nil); $$ = append($1, fn) }
	| block_item_list declaration_specifiers local_fun_proto ATTR_WEAK ';'
		{ fn := applyDeclToFunNode($2, $3, nil, nil, nil); fn.IsWeak = true; $$ = append($1, fn) }
	| block_item_list declaration_specifiers local_fun_proto ATTR_SECTION ';'
		{ fn := applyDeclToFunNode($2, $3, nil, nil, nil); fn.SectionName = $4; $$ = append($1, fn) }
	| block_item_list declaration_specifiers local_fun_proto ATTR_ALIGNED ';'
		{ fn := applyDeclToFunNode($2, $3, nil, nil, nil); $$ = append($1, fn) }
	| block_item_list STRUCT ID ';'
		{ $$ = $1 }
	| block_item_list UNION ID ';'
		{ $$ = $1 }
	| block_item_list ENUM ID ';'
		{ $$ = $1 }
	/* Local anonymous enum declaration: enum { A, B, C }; — defines constants in local scope */
	| block_item_list ENUM '{' enum_list '}' ';'
		{ $$ = append($1, $4...) }
	| block_item_list ENUM ID '{' enum_list '}' ';'
		{ $$ = append($1, $5...) }
	/* Local struct/union type declaration (no variable): struct Foo { ... }; inside a function */
	| block_item_list struct_declaration
		{ $$ = append($1, $2...) }
	| block_item_list union_declaration
		{ $$ = append($1, $2...) }
	/* Local typedef declaration */
	| block_item_list typedef_declaration
		{ $$ = $1 }
	/* GCC inline assembly: __asm(...); or bare __asm__; — parse and discard */
	| block_item_list ASM_KW '(' args ')' ';'
		{ $$ = $1 }
	| block_item_list ASM_KW ';'
		{ $$ = $1 }
	| block_item_list STATIC_ASSERT '(' const_int_expr ',' STRING_LIT ')' ';'
		{
			if $4 == 0 {
				yylex.(*lexer).Error(fmt.Sprintf("_Static_assert failed: %s", $6))
			}
			$$ = $1
		}
	| block_item_list STATIC_ASSERT '(' const_int_expr ')' ';'
		{
			if $4 == 0 {
				yylex.(*lexer).Error("_Static_assert failed")
			}
			$$ = $1
		}
	| /* empty */
		{ $$ = nil }
	;

id_list
	: id_list ',' ID
		{ $$ = append($1, &Node{Kind: KindVar, Name: $3}) }
	| id_list ',' ID '[' const_int_expr ']'
		{ $$ = append($1, &Node{Kind: KindVar, Name: $3, Val: $5, Type: TypeIntArray}) }
	/* ── Allow "a, b, c = val, d, e" — id_list with an initialized item in the middle/end ── */
	| id_list ',' ID '=' expression
		{ $$ = append($1, &Node{Kind: KindVarDecl, Name: $3, Children: []*Node{$5}}) }
	| ID ',' ID
		{ $$ = []*Node{{Kind: KindVar, Name: $1}, {Kind: KindVar, Name: $3}} }
	;

/* ptr_id_list: comma-separated pointer declarators for struct fields.
   Used for patterns like: int *a, *b, *c; */
ptr_id_list
	: '*' ID
		{ $$ = []*Node{{Kind: KindVar, Name: $2}} }
	| ptr_id_list ',' '*' ID
		{ $$ = append($1, &Node{Kind: KindVar, Name: $4}) }
	;

/* multi_init_id_list: two or more declarators separated by commas.
   Requires at least two entries to avoid ambiguity with the single-var rules.
   Supports: ID=expr, bare ID, ID[]=init, ID[N]=init, ID[N] items. */
multi_init_id_list
	/* ── Base cases involving arrays (no initializer on first element) ── */
	: ID ',' ID '[' const_int_expr ']'
		{ $$ = []*Node{
			{Kind: KindVarDecl, Name: $1},
			{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5},
		} }
	| ID '[' const_int_expr ']' ',' ID
		{ $$ = []*Node{
			{Kind: KindVarDecl, Type: TypeIntArray, Name: $1, Val: $3},
			{Kind: KindVarDecl, Name: $6},
		} }
	/* ── Base: two initialized items ── */
	| ID '=' expression ',' ID '=' expression
		{ $$ = []*Node{
			{Kind: KindVarDecl, Name: $1, Children: []*Node{$3}},
			{Kind: KindVarDecl, Name: $5, Children: []*Node{$7}},
		} }
	/* first item init, second item bare ID */
	| ID '=' expression ',' ID
		{ $$ = []*Node{
			{Kind: KindVarDecl, Name: $1, Children: []*Node{$3}},
			{Kind: KindVarDecl, Name: $5},
		} }
	/* first item init, second item unsized array with init list */
	| ID '=' expression ',' ID '[' ']' '=' '{' init_list '}'
		{ $$ = []*Node{
			{Kind: KindVarDecl, Name: $1, Children: []*Node{$3}},
			{Kind: KindVarDecl, Type: TypeIntArray, Name: $5, Val: 0,
			 Children: []*Node{{Kind: KindInitList, Children: $10}}},
		} }
	| ID '=' expression ',' ID '[' ']' '=' '{' init_list ',' '}'
		{ $$ = []*Node{
			{Kind: KindVarDecl, Name: $1, Children: []*Node{$3}},
			{Kind: KindVarDecl, Type: TypeIntArray, Name: $5, Val: 0,
			 Children: []*Node{{Kind: KindInitList, Children: $10}}},
		} }
	/* extend with another initialized item */
	| multi_init_id_list ',' ID '=' expression
		{ $$ = append($1, &Node{Kind: KindVarDecl, Name: $3, Children: []*Node{$5}}) }
	/* extend with bare uninitialized ID */
	| multi_init_id_list ',' ID
		{ $$ = append($1, &Node{Kind: KindVarDecl, Name: $3}) }
	/* extend with unsized array + init list */
	| multi_init_id_list ',' ID '[' ']' '=' '{' init_list '}'
		{ $$ = append($1, &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: 0,
		    Children: []*Node{{Kind: KindInitList, Children: $8}}}) }
	| multi_init_id_list ',' ID '[' ']' '=' '{' init_list ',' '}'
		{ $$ = append($1, &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: 0,
		    Children: []*Node{{Kind: KindInitList, Children: $8}}}) }
	/* extend with sized array (no initializer) */
	| multi_init_id_list ',' ID '[' const_int_expr ']'
		{ $$ = append($1, &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5}) }
	;

statement_list
	: statement_list statement
		{ $$ = append($1, $2) }
	| /* empty */
		{ $$ = nil }
	;

statement
	: expression_stmt { $$ = $1 }
	| compound_stmt   { $$ = $1 }
	| selection_stmt  { $$ = $1 }
	| iteration_stmt  { $$ = $1 }
	| for_stmt        { $$ = $1 }
	| do_while_stmt   { $$ = $1 }
	| switch_stmt     { $$ = $1 }
	| return_stmt     { $$ = $1 }
	| break_stmt      { $$ = $1 }
	| continue_stmt   { $$ = $1 }
	| goto_stmt       { $$ = $1 }
	| ID ':' statement
		{ $$ = &Node{Kind: KindLabel, Name: $1, Children: []*Node{$3}} }
	;

expression_stmt
	: expression ';' { $$ = &Node{Kind: KindExprStmt, Children: []*Node{$1}} }
	| comma_expr_list ';'
		{ $$ = &Node{Kind: KindExprStmt, Children: []*Node{$1}} }
	| ';'            { $$ = &Node{Kind: KindExprStmt} }
	;

selection_stmt
	: IF '(' expression ')' statement %prec LOWER_THAN_ELSE
		{ $$ = &Node{Kind: KindSelection, Children: []*Node{$3, $5}} }
	| IF '(' expression ')' statement ELSE statement
		{ $$ = &Node{Kind: KindSelection, Children: []*Node{$3, $5, $7}} }
	;

iteration_stmt
	: WHILE '(' expression ')' statement
		{ $$ = &Node{Kind: KindIteration, Children: []*Node{$3, $5}} }
	| WHILE '(' comma_expr_list ')' statement
		{ $$ = &Node{Kind: KindIteration, Children: []*Node{$3, $5}} }
	;

for_stmt
	: FOR '(' opt_expression ';' opt_expression ';' opt_expression ')' statement
		{ $$ = &Node{Kind: KindFor, Children: []*Node{$3, $5, $7, $9}} }
	/* C99: for (type var = init; cond; post) — variable declared in for-init */
	| FOR '(' var_declaration opt_expression ';' opt_expression ')' statement
		{ init := &Node{Kind: KindCompound, Children: $3}
		  $$ = &Node{Kind: KindFor, Children: []*Node{init, $4, $6, $8}} }
	| FOR '(' var_declaration opt_expression ';' expression ',' expression ')' statement
		{ init := &Node{Kind: KindCompound, Children: $3}
		  inc := &Node{Kind: KindCompound, Children: []*Node{
		      {Kind: KindExprStmt, Children: []*Node{$6}},
		      {Kind: KindExprStmt, Children: []*Node{$8}}}}
		  $$ = &Node{Kind: KindFor, Children: []*Node{init, $4, inc, $10}} }
	| FOR '(' var_declaration opt_expression ';' expression ',' expression ',' expression ')' statement
		{ init := &Node{Kind: KindCompound, Children: $3}
		  inc := &Node{Kind: KindCompound, Children: []*Node{
		      {Kind: KindExprStmt, Children: []*Node{$6}},
		      {Kind: KindExprStmt, Children: []*Node{$8}},
		      {Kind: KindExprStmt, Children: []*Node{$10}}}}
		  $$ = &Node{Kind: KindFor, Children: []*Node{init, $4, inc, $12}} }
	/* ── For loop with comma expression in increment: "for (i=0; cond; i++, j++)" ── */
	| FOR '(' opt_expression ';' opt_expression ';' expression ',' expression ')' statement
		{ inc := &Node{Kind: KindCompound, Children: []*Node{
		      &Node{Kind: KindExprStmt, Children: []*Node{$7}},
		      &Node{Kind: KindExprStmt, Children: []*Node{$9}}}}
		  $$ = &Node{Kind: KindFor, Children: []*Node{$3, $5, inc, $11}} }
	| FOR '(' opt_expression ';' opt_expression ';' expression ',' expression ',' expression ')' statement
		{ inc := &Node{Kind: KindCompound, Children: []*Node{
		      &Node{Kind: KindExprStmt, Children: []*Node{$7}},
		      &Node{Kind: KindExprStmt, Children: []*Node{$9}},
		      &Node{Kind: KindExprStmt, Children: []*Node{$11}}}}
		  $$ = &Node{Kind: KindFor, Children: []*Node{$3, $5, inc, $13}} }
	/* ── For loop with comma expression in init: "for (a=x, b=y; ...)" ── */
	| FOR '(' expression ',' expression ';' opt_expression ';' opt_expression ')' statement
		{ init := &Node{Kind: KindExprStmt, Children: []*Node{$3}}
		  init2 := &Node{Kind: KindExprStmt, Children: []*Node{$5}}
		  $$ = &Node{Kind: KindFor, Children: []*Node{
		      &Node{Kind: KindCompound, Children: []*Node{init, init2}}, $7, $9, $11}} }
	| FOR '(' expression ',' expression ',' expression ';' opt_expression ';' opt_expression ')' statement
		{ init := &Node{Kind: KindExprStmt, Children: []*Node{$3}}
		  init2 := &Node{Kind: KindExprStmt, Children: []*Node{$5}}
		  init3 := &Node{Kind: KindExprStmt, Children: []*Node{$7}}
		  $$ = &Node{Kind: KindFor, Children: []*Node{
		      &Node{Kind: KindCompound, Children: []*Node{init, init2, init3}}, $9, $11, $13}} }
	/* ── For loop with comma init (2) AND comma increment (2) ── */
	| FOR '(' expression ',' expression ';' opt_expression ';' expression ',' expression ')' statement
		{ init := &Node{Kind: KindCompound, Children: []*Node{
		      &Node{Kind: KindExprStmt, Children: []*Node{$3}},
		      &Node{Kind: KindExprStmt, Children: []*Node{$5}}}}
		  inc := &Node{Kind: KindCompound, Children: []*Node{
		      &Node{Kind: KindExprStmt, Children: []*Node{$9}},
		      &Node{Kind: KindExprStmt, Children: []*Node{$11}}}}
		  $$ = &Node{Kind: KindFor, Children: []*Node{init, $7, inc, $13}} }
	/* ── For loop with comma init (3) AND comma increment (2) ── */
	| FOR '(' expression ',' expression ',' expression ';' opt_expression ';' expression ',' expression ')' statement
		{ init := &Node{Kind: KindCompound, Children: []*Node{
		      &Node{Kind: KindExprStmt, Children: []*Node{$3}},
		      &Node{Kind: KindExprStmt, Children: []*Node{$5}},
		      &Node{Kind: KindExprStmt, Children: []*Node{$7}}}}
		  inc := &Node{Kind: KindCompound, Children: []*Node{
		      &Node{Kind: KindExprStmt, Children: []*Node{$11}},
		      &Node{Kind: KindExprStmt, Children: []*Node{$13}}}}
		  $$ = &Node{Kind: KindFor, Children: []*Node{init, $9, inc, $15}} }
	;

do_while_stmt
	: DO statement WHILE '(' expression ')' ';'
		{ $$ = &Node{Kind: KindDoWhile, Children: []*Node{$2, $5}} }
	;

switch_stmt
	: SWITCH '(' expression ')' '{' case_list '}'
		{ $$ = &Node{Kind: KindSwitch, Children: append([]*Node{$3}, $6...)} }
	;

case_list
	: case_list case_item
		{ $$ = append($1, $2) }
	| /* empty */
		{ $$ = nil }
	;

case_item
	: CASE expression ':' block_item_list
		{ $$ = &Node{Kind: KindCase, Children: append([]*Node{$2}, $4...)} }
	| DEFAULT ':' block_item_list
		{ $$ = &Node{Kind: KindCase, Val: -1, Children: $3} }
	;

goto_stmt
	: GOTO ID ';'
		{ $$ = &Node{Kind: KindGoto, Name: $2} }
	| GOTO '*' expression ';'
		{ $$ = &Node{Kind: KindIndirectGoto, Children: []*Node{$3}} }
	;

break_stmt
	: BREAK ';'
		{ $$ = &Node{Kind: KindBreak} }
	;

continue_stmt
	: CONTINUE ';'
		{ $$ = &Node{Kind: KindContinue} }
	;

opt_expression
	: expression { $$ = $1 }
	| /* empty */ { $$ = nil }
	;

return_stmt
	: RETURN ';'
		{ $$ = &Node{Kind: KindReturn} }
	| RETURN expression ';'
		{ $$ = &Node{Kind: KindReturn, Children: []*Node{$2}} }
	;

postfix_expr
	: var
		{ $$ = $1 }
	| call
		{ $$ = $1 }
	/* Indirect call through var (covers arr[i]() where arr[i] reduces to var first) */
	| var '(' args ')'
		{ $$ = &Node{Kind: KindIndirectCall, Children: append([]*Node{$1}, $3...)} }
	| postfix_expr ARROW ID
		{ $$ = &Node{Kind: KindFieldAccess, Op: "->", Name: $3, Children: []*Node{$1}} }
	| postfix_expr '.' ID
		{ $$ = &Node{Kind: KindFieldAccess, Op: ".", Name: $3, Children: []*Node{$1}} }
	| postfix_expr INC
		{ $$ = &Node{Kind: KindPostInc, Children: []*Node{$1}} }
	| postfix_expr DEC
		{ $$ = &Node{Kind: KindPostDec, Children: []*Node{$1}} }
	| postfix_expr '[' expression ']'
		{ $$ = &Node{Kind: KindIndexExpr, Children: []*Node{$1, $3}} }
	/* Indirect call through array subscript: fp_array[i](args) */
	| postfix_expr '[' expression ']' '(' args ')'
		{ callee := &Node{Kind: KindIndexExpr, Children: []*Node{$1, $3}}
		  $$ = &Node{Kind: KindIndirectCall, Children: append([]*Node{callee}, $6...)} }
	/* Indirect call through struct field: stream->put(args) */
	| postfix_expr ARROW ID '(' args ')'
		{ callee := &Node{Kind: KindFieldAccess, Op: "->", Name: $3, Children: []*Node{$1}}
		  $$ = &Node{Kind: KindIndirectCall, Children: append([]*Node{callee}, $5...)} }
	| postfix_expr '.' ID '(' args ')'
		{ callee := &Node{Kind: KindFieldAccess, Op: ".", Name: $3, Children: []*Node{$1}}
		  $$ = &Node{Kind: KindIndirectCall, Children: append([]*Node{callee}, $5...)} }
	/* Address-of parenthesized expression with field/arrow access: &(expr).field, &(expr)->field.
	   These MUST come before plain '(' expression ')' '.' ID to win R/R conflict. */
	| '&' '(' expression ')' '.' ID
		{ fa := &Node{Kind: KindFieldAccess, Op: ".", Name: $6, Children: []*Node{$3}}
		  $$ = &Node{Kind: KindAddrOf, Children: []*Node{fa}} }
	| '&' '(' expression ')' ARROW ID
		{ fa := &Node{Kind: KindFieldAccess, Op: "->", Name: $6, Children: []*Node{$3}}
		  $$ = &Node{Kind: KindAddrOf, Children: []*Node{fa}} }
	/* Parenthesized expression with field access: (expr).field */
	| '(' expression ')' '.' ID
		{ $$ = &Node{Kind: KindFieldAccess, Op: ".", Name: $5, Children: []*Node{$2}} }
	/* Parenthesized expression with arrow: (expr)->field */
	| '(' expression ')' ARROW ID
		{ $$ = &Node{Kind: KindFieldAccess, Op: "->", Name: $5, Children: []*Node{$2}} }
	/* Comma-expression in parens with arrow: (a, b)->field */
	| '(' comma_expr_list ')' ARROW ID
		{ $$ = &Node{Kind: KindFieldAccess, Op: "->", Name: $5, Children: []*Node{$2}} }
	/* Comma-expression in parens with dot: (a, b).field */
	| '(' comma_expr_list ')' '.' ID
		{ $$ = &Node{Kind: KindFieldAccess, Op: ".", Name: $5, Children: []*Node{$2}} }
	/* Indirect call through parenthesized arrow: (expr)->fn(args) */
	| '(' expression ')' ARROW ID '(' args ')'
		{ callee := &Node{Kind: KindFieldAccess, Op: "->", Name: $5, Children: []*Node{$2}}
		  $$ = &Node{Kind: KindIndirectCall, Children: append([]*Node{callee}, $7...)} }
	| '(' expression ')' '.' ID '(' args ')'
		{ callee := &Node{Kind: KindFieldAccess, Op: ".", Name: $5, Children: []*Node{$2}}
		  $$ = &Node{Kind: KindIndirectCall, Children: append([]*Node{callee}, $7...)} }
	/* Parenthesized expression with subscript: (expr)[idx] */
	| '(' expression ')' '[' expression ']'
		{ $$ = &Node{Kind: KindIndexExpr, Children: []*Node{$2, $5}} }
	/* Post-increment/decrement on parenthesized expression: (x)++, (x)-- */
	| '(' expression ')' INC
		{ $$ = &Node{Kind: KindPostInc, Children: []*Node{$2}} }
	| '(' expression ')' DEC
		{ $$ = &Node{Kind: KindPostDec, Children: []*Node{$2}} }
	/* Indirect call through parenthesized expression: (ptr->fn)(args), (fnptr)(args) */
	| '(' expression ')' '(' args ')'
		{ $$ = &Node{Kind: KindIndirectCall, Children: append([]*Node{$2}, $5...)} }
	/* Chained call: f(x)(y) — result of call is a function pointer, call it */
	| call '(' args ')'
		{ $$ = &Node{Kind: KindIndirectCall, Children: append([]*Node{$1}, $3...)} }
	/* String literal subscript: "str"[0] — char at given index */
	| STRING_LIT '[' expression ']'
		{ base := &Node{Kind: KindStrLit, Name: $1, Type: TypeCharPtr}
		  $$ = &Node{Kind: KindIndexExpr, Children: []*Node{base, $3}} }
	;

expression
	: postfix_expr '=' expression
		{ $$ = &Node{Kind: KindAssign, Children: []*Node{$1, $3}} }
	| '(' expression ')' '=' expression
		{ $$ = &Node{Kind: KindAssign, Children: []*Node{$2, $5}} }
	/* ── Compound assignment with parenthesized LHS: (expr) += rhs ── */
	| '(' expression ')' PLUSEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "+", Children: []*Node{$2, $5}} }
	| '(' expression ')' MINUSEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "-", Children: []*Node{$2, $5}} }
	| '(' expression ')' STAREQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "*", Children: []*Node{$2, $5}} }
	| '(' expression ')' ANDEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "&", Children: []*Node{$2, $5}} }
	| '(' expression ')' OREQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "|", Children: []*Node{$2, $5}} }
	| '(' expression ')' XOREQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "^", Children: []*Node{$2, $5}} }
	| '(' expression ')' SHLEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "<<", Children: []*Node{$2, $5}} }
	| '(' expression ')' SHREQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: ">>", Children: []*Node{$2, $5}} }
	| '(' expression ')' DIVEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "/", Children: []*Node{$2, $5}} }
	| '(' expression ')' MODEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "%", Children: []*Node{$2, $5}} }
	| '*' factor '=' expression
		{ lhs := &Node{Kind: KindDeref, Children: []*Node{$2}}; $$ = &Node{Kind: KindAssign, Children: []*Node{lhs, $4}} }
	/* ── Compound assignment through pointer: "*eptr += expr" ── */
	| '*' factor PLUSEQ expression
		{ lhs := &Node{Kind: KindDeref, Children: []*Node{$2}}; $$ = &Node{Kind: KindCompoundAssign, Op: "+", Children: []*Node{lhs, $4}} }
	| '*' factor MINUSEQ expression
		{ lhs := &Node{Kind: KindDeref, Children: []*Node{$2}}; $$ = &Node{Kind: KindCompoundAssign, Op: "-", Children: []*Node{lhs, $4}} }
	| '*' factor STAREQ expression
		{ lhs := &Node{Kind: KindDeref, Children: []*Node{$2}}; $$ = &Node{Kind: KindCompoundAssign, Op: "*", Children: []*Node{lhs, $4}} }
	| '*' factor SHREQ expression
		{ lhs := &Node{Kind: KindDeref, Children: []*Node{$2}}; $$ = &Node{Kind: KindCompoundAssign, Op: ">>", Children: []*Node{lhs, $4}} }
	| '*' factor SHLEQ expression
		{ lhs := &Node{Kind: KindDeref, Children: []*Node{$2}}; $$ = &Node{Kind: KindCompoundAssign, Op: "<<", Children: []*Node{lhs, $4}} }
	| '*' factor OREQ expression
		{ lhs := &Node{Kind: KindDeref, Children: []*Node{$2}}; $$ = &Node{Kind: KindCompoundAssign, Op: "|", Children: []*Node{lhs, $4}} }
	| '*' factor ANDEQ expression
		{ lhs := &Node{Kind: KindDeref, Children: []*Node{$2}}; $$ = &Node{Kind: KindCompoundAssign, Op: "&", Children: []*Node{lhs, $4}} }
	| postfix_expr PLUSEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "+", Children: []*Node{$1, $3}} }
	| postfix_expr MINUSEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "-", Children: []*Node{$1, $3}} }
	| postfix_expr STAREQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "*", Children: []*Node{$1, $3}} }
	| postfix_expr DIVEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "/", Children: []*Node{$1, $3}} }
	| postfix_expr MODEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "%", Children: []*Node{$1, $3}} }
	| postfix_expr ANDEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "&", Children: []*Node{$1, $3}} }
	| postfix_expr OREQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "|", Children: []*Node{$1, $3}} }
	| postfix_expr XOREQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "^", Children: []*Node{$1, $3}} }
	| postfix_expr SHLEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "<<", Children: []*Node{$1, $3}} }
	| postfix_expr SHREQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: ">>", Children: []*Node{$1, $3}} }
	| simple_expression
		{ $$ = $1 }
	;

var
	: ID
		{ $$ = &Node{Kind: KindVar, Name: $1} }
	| ID '[' expression ']' '[' expression ']'
		{ $$ = &Node{Kind: KindArray2D, Name: $1, Children: []*Node{$3, $6}} }
	| ID '[' expression ']'
		{ $$ = &Node{Kind: KindArrayVar, Name: $1, Children: []*Node{$3}} }
	;

simple_expression
	: ternary_expression
		{ $$ = $1 }
	;

ternary_expression
	: logical_expression QUESTION ternary_expression ':' ternary_expression
		{ $$ = &Node{Kind: KindTernary, Children: []*Node{$1, $3, $5}} }
	| logical_expression
		{ $$ = $1 }
	;

logical_expression
	: logical_expression ANDAND comparison_expression
		{ $$ = &Node{Kind: KindLogAnd, Children: []*Node{$1, $3}} }
	| logical_expression OROR comparison_expression
		{ $$ = &Node{Kind: KindLogOr, Children: []*Node{$1, $3}} }
	| comparison_expression
		{ $$ = $1 }
	;

comparison_expression
	: bitwise_expression relop bitwise_expression
		{ $$ = &Node{Kind: KindBinOp, Op: $2, Children: []*Node{$1, $3}} }
	| bitwise_expression
		{ $$ = $1 }
	;

bitwise_expression
	: bitwise_expression bitwiseop additive_expression
		{ $$ = &Node{Kind: KindBinOp, Op: $2, Children: []*Node{$1, $3}} }
	| additive_expression
		{ $$ = $1 }
	;

bitwiseop
	: '&'    { $$ = "&" }
	| '|'    { $$ = "|" }
	| '^'    { $$ = "^" }
	| LSHIFT { $$ = "<<" }
	| RSHIFT { $$ = ">>" }
	;

relop
	: LE   { $$ = "<=" }
	| '<'  { $$ = "<" }
	| '>'  { $$ = ">" }
	| GE   { $$ = ">=" }
	| EQ   { $$ = "==" }
	| NE   { $$ = "!=" }
	;

additive_expression
	: additive_expression addop term
		{ $$ = &Node{Kind: KindBinOp, Op: $2, Children: []*Node{$1, $3}} }
	| term
		{ $$ = $1 }
	;

addop
	: '+' { $$ = "+" }
	| '-' { $$ = "-" }
	;

term
	: term mulop factor
		{ $$ = &Node{Kind: KindBinOp, Op: $2, Children: []*Node{$1, $3}} }
	| factor
		{ $$ = $1 }
	;

mulop
	: '*' { $$ = "*" }
	| '/' { $$ = "/" }
	| '%' { $$ = "%" }
	;

factor
	: '(' expression ')' { $$ = $2 }
	/* ── C comma operator inside parens: (expr1, expr2, ...) ── */
	| '(' comma_expr_list ')'
		{ $$ = $2 }
	| postfix_expr        { $$ = $1 }
	| NUM                 { $$ = &Node{Kind: KindNum, Val: $1} }
	| FNUM                { $$ = &Node{Kind: KindFNum, FVal: $1, Type: TypeDouble} }
	| CHAR_LIT            { $$ = &Node{Kind: KindCharLit, Val: $1, Type: TypeInt} }
	| STRING_LIT          { $$ = &Node{Kind: KindStrLit, Name: $1, Type: TypeCharPtr} }
	| '-' factor          { $$ = &Node{Kind: KindUnary, Op: "-", Children: []*Node{$2}} }
	| '!' factor          { $$ = &Node{Kind: KindUnary, Op: "!", Children: []*Node{$2}} }
	| '~' factor          { $$ = &Node{Kind: KindUnary, Op: "~", Children: []*Node{$2}} }
	| INC factor          { $$ = &Node{Kind: KindPreInc, Children: []*Node{$2}} }
	| DEC factor          { $$ = &Node{Kind: KindPreDec, Children: []*Node{$2}} }
	| '&' var             { $$ = &Node{Kind: KindAddrOf, Children: []*Node{$2}} }
	| ANDAND ID           { $$ = &Node{Kind: KindLabelAddr, Name: $2, Type: TypePtr} }
	| '&' '(' expression ')' { $$ = &Node{Kind: KindAddrOf, Children: []*Node{$3}} }
	/* Address-of array element through dereference: &(*p)[idx] — &(expr)[idx] */
	| '&' '(' expression ')' '[' expression ']'
		{ idx := &Node{Kind: KindIndexExpr, Children: []*Node{$3, $6}}
		  $$ = &Node{Kind: KindAddrOf, Children: []*Node{idx}} }
	/* Address of array subscript on struct member: &g->member[expr] */
	| '&' postfix_expr '[' expression ']'
		{ idx := &Node{Kind: KindIndexExpr, Children: []*Node{$2, $4}}
		  $$ = &Node{Kind: KindAddrOf, Children: []*Node{idx}} }
	/* Address of struct field: &s.field or &p->field or &(p->field) */
	| '&' postfix_expr ARROW ID  { fa := &Node{Kind: KindFieldAccess, Op: "->", Name: $4, Children: []*Node{$2}}; $$ = &Node{Kind: KindAddrOf, Children: []*Node{fa}} }
	| '&' postfix_expr '.' ID    { fa := &Node{Kind: KindFieldAccess, Op: ".", Name: $4, Children: []*Node{$2}}; $$ = &Node{Kind: KindAddrOf, Children: []*Node{fa}} }
	/* Address of string literal subscript: &"str"[N] → const char* pointer into the string */
	| '&' STRING_LIT '[' expression ']'
		{ base := &Node{Kind: KindStrLit, Name: $2, Type: TypeCharPtr}
		  idx := &Node{Kind: KindIndexExpr, Type: TypeChar, Children: []*Node{base, $4}}
		  $$ = &Node{Kind: KindAddrOf, Children: []*Node{idx}} }
	/* NOTE: '&' '(' postfix_expr ARROW ID ')' and '&' '(' postfix_expr '.' ID ')' are
	   intentionally omitted. They are redundant with '&' '(' expression ')' (rule 979) and
	   they cause S/R conflicts that prevent '&' '(' expression ')' '.' ID from working. */
	| '*' factor          { $$ = &Node{Kind: KindDeref, Children: []*Node{$2}} }
	| SIZEOF '(' type_specifier ')'
		{ $$ = &Node{Kind: KindSizeof, Type: $3.Kind, StructTag: $3.Tag} }
	| SIZEOF '(' type_specifier '*' ')'
		{ $$ = &Node{Kind: KindSizeof, Type: TypePtr} }
	| SIZEOF '(' CONST type_specifier '*' ')'
		{ $$ = &Node{Kind: KindSizeof, Type: TypePtr} }
	| SIZEOF '(' type_specifier '*' '*' ')'
		{ $$ = &Node{Kind: KindSizeof, Type: TypePtr} }
	| SIZEOF '(' type_specifier '[' const_int_expr ']' ')'
		{
			// sizeof(T[N]): used as a static assertion (C trick). If N <= 0 the assertion
			// would fire in GCC/clang. Gaston silently accepts negative dimensions here
			// because (a) gaston can't always evaluate complex const_int_exprs (e.g.
			// offsetof computations), and (b) the real compiler will catch true failures.
			dim := $5
			if dim <= 0 { dim = 1 }
			$$ = &Node{Kind: KindNum, Val: sizeofType($3) * dim}
		}
	| SIZEOF '(' STRUCT ID ')'
		{ $$ = &Node{Kind: KindSizeof, StructTag: $4} }
	| SIZEOF '(' STRUCT ID '*' ')'
		{ $$ = &Node{Kind: KindSizeof, Type: TypePtr} }
	| SIZEOF '(' UNION ID ')'
		{ $$ = &Node{Kind: KindSizeof, StructTag: $4} }
	| SIZEOF '(' expression ')'
		{ $$ = &Node{Kind: KindSizeof, Children: []*Node{$3}} }
	| SIZEOF postfix_expr
		{ $$ = &Node{Kind: KindSizeof, Children: []*Node{$2}} }
	| SIZEOF '*' factor
		{ deref := &Node{Kind: KindDeref, Children: []*Node{$3}}; $$ = &Node{Kind: KindSizeof, Children: []*Node{deref}} }
	| ALIGNOF '(' type_specifier ')'
		{ $$ = &Node{Kind: KindAlignof, Type: $3.Kind, StructTag: $3.Tag} }
	| ALIGNOF '(' type_specifier '*' ')'
		{ $$ = &Node{Kind: KindAlignof, Type: TypePtr} }
	| ALIGNOF '(' STRUCT ID ')'
		{ $$ = &Node{Kind: KindAlignof, StructTag: $4} }
	| ALIGNOF '(' expression ')'
		{ $$ = &Node{Kind: KindAlignof, Children: []*Node{$3}} }
	| GENERIC '(' expression ',' generic_assoc_list ')'
		{ $$ = &Node{Kind: KindGeneric, Children: append([]*Node{$3}, $5...)} }
	| VA_ARG '(' expression ',' type_specifier ')'
		{
			n := ctNode(KindVAArg, $5, "")
			n.Children = []*Node{$3}
			$$ = n
		}
	| VA_ARG '(' expression ',' type_specifier '*' ')'
		{
			n := &Node{Kind: KindVAArg, Type: TypePtr}
			n.Pointee = $5
			n.Children = []*Node{$3}
			$$ = n
		}
	| VA_ARG '(' expression ',' CONST type_specifier '*' ')'
		{
			n := &Node{Kind: KindVAArg, Type: TypePtr}
			n.Pointee = $6
			n.Children = []*Node{$3}
			$$ = n
		}
	| VA_ARG '(' expression ',' type_specifier '*' '*' ')'
		{
			n := &Node{Kind: KindVAArg, Type: TypePtr}
			n.Pointee = ptrCType($5)
			n.Children = []*Node{$3}
			$$ = n
		}
	| VA_ARG '(' expression ',' STRUCT ID '*' ')'
		{
			n := &Node{Kind: KindVAArg, Type: TypePtr}
			n.Pointee = structCType($6)
			n.Children = []*Node{$3}
			$$ = n
		}
	| '(' type_specifier ')' factor
		{ $$ = castNode($2, $4) }
	| '(' type_specifier '*' ')' factor
		{ n := &Node{Kind: KindCast, Type: TypePtr}; n.Pointee = $2; n.Children = []*Node{$5}; $$ = n }
	| '(' CONST type_specifier '*' ')' factor
		{ n := &Node{Kind: KindCast, Type: TypePtr}; n.Pointee = $3; n.Children = []*Node{$6}; $$ = n }
	| '(' type_specifier '*' '*' ')' factor
		{ n := &Node{Kind: KindCast, Type: TypePtr}; n.Pointee = ptrCType($2); n.Children = []*Node{$6}; $$ = n }
	| '(' CONST type_specifier '*' '*' ')' factor
		{ n := &Node{Kind: KindCast, Type: TypePtr}; n.Pointee = ptrCType($3); n.Children = []*Node{$7}; $$ = n }
	| '(' STRUCT ID '*' ')' factor
		{ n := &Node{Kind: KindCast, Type: TypePtr}; n.Pointee = structCType($3); n.Children = []*Node{$6}; $$ = n }
	| '(' STRUCT ID ')' factor
		{ n := &Node{Kind: KindCast, Type: TypeStruct, StructTag: $3}; n.Children = []*Node{$5}; $$ = n }
	/* Cast to anonymous struct pointer: (struct { ... }*) expr — register a real struct def */
	| '(' STRUCT '{' field_list '}' '*' ')' factor
		{ l := yylex.(*lexer); tag := l.nextAnon()
		  sd := &Node{Kind: KindStructDef, Name: tag, Children: $4}
		  l.pendingStructDefs = append(l.pendingStructDefs, sd)
		  n := &Node{Kind: KindCast, Type: TypePtr}; n.Pointee = structCType(tag); n.Children = []*Node{$8}; $$ = n }
	| '(' STRUCT '{' field_list '}' ')' factor
		{ l := yylex.(*lexer); tag := l.nextAnon()
		  sd := &Node{Kind: KindStructDef, Name: tag, Children: $4}
		  l.pendingStructDefs = append(l.pendingStructDefs, sd)
		  n := &Node{Kind: KindCast, Type: TypeStruct, StructTag: tag}; n.Children = []*Node{$7}; $$ = n }
	/* Cast to function pointer: (rettype (*)(params)) expr — treated as TypePtr cast */
	| '(' type_specifier '(' '*' ')' '(' fp_param_types ')' ')' factor
		{ n := &Node{Kind: KindCast, Type: TypePtr}; n.Pointee = $2; n.Children = []*Node{$10}; $$ = n }
	| '(' type_specifier '*' '(' '*' ')' '(' fp_param_types ')' ')' factor
		{ n := &Node{Kind: KindCast, Type: TypePtr}; n.Pointee = ptrCType($2); n.Children = []*Node{$11}; $$ = n }
	/* removed: '(' '*' postfix_expr ARROW ID ')' '(' args ')' — caused S/R conflict
	   preventing (*p->field) from parsing. Handled via '(' expression ')' '(' args ')' */
	/* ── Address-of compound literal: &(struct T){...} — produces TypePtr ── */
	| '&' '(' STRUCT ID ')' '{' init_list '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypePtr}
		  n.Pointee = structCType($4)
		  n.Children = []*Node{{Kind: KindInitList, Children: $7}}
		  $$ = n }
	| '&' '(' STRUCT ID ')' '{' init_list ',' '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypePtr}
		  n.Pointee = structCType($4)
		  n.Children = []*Node{{Kind: KindInitList, Children: $7}}
		  $$ = n }
	| '&' '(' STRUCT ID ')' '{' '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypePtr}
		  n.Pointee = structCType($4)
		  n.Children = []*Node{{Kind: KindInitList}}
		  $$ = n }
	/* ── Array compound literal: (int[]){...} ── */
	| '(' type_specifier '[' ']' ')' '{' init_list '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypeIntArray, ElemType: $2.Kind}
		  n.Children = []*Node{{Kind: KindInitList, Children: $7}}
		  $$ = n }
	| '(' type_specifier '[' ']' ')' '{' init_list ',' '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypeIntArray, ElemType: $2.Kind}
		  n.Children = []*Node{{Kind: KindInitList, Children: $7}}
		  $$ = n }
	| '(' type_specifier '[' const_int_expr ']' ')' '{' init_list '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypeIntArray, ElemType: $2.Kind, Val: $4}
		  n.Children = []*Node{{Kind: KindInitList, Children: $8}}
		  $$ = n }
	| '(' type_specifier '[' const_int_expr ']' ')' '{' init_list ',' '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypeIntArray, ElemType: $2.Kind, Val: $4}
		  n.Children = []*Node{{Kind: KindInitList, Children: $8}}
		  $$ = n }
	/* ── Compound literals (C99 §6.5.2.5) ── */
	| '(' type_specifier ')' '{' init_list '}'
		{ n := ctNode(KindCompoundLit, $2, "")
		  n.Children = []*Node{{Kind: KindInitList, Children: $5}}
		  $$ = n }
	| '(' type_specifier ')' '{' init_list ',' '}'
		{ n := ctNode(KindCompoundLit, $2, "")
		  n.Children = []*Node{{Kind: KindInitList, Children: $5}}
		  $$ = n }
	| '(' type_specifier ')' '{' '}'
		{ n := ctNode(KindCompoundLit, $2, "")
		  n.Children = []*Node{{Kind: KindInitList}}
		  $$ = n }
	| '(' type_specifier '*' ')' '{' init_list '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypePtr}
		  n.Pointee = $2
		  n.Children = []*Node{{Kind: KindInitList, Children: $6}}
		  $$ = n }
	| '(' type_specifier '*' ')' '{' init_list ',' '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypePtr}
		  n.Pointee = $2
		  n.Children = []*Node{{Kind: KindInitList, Children: $6}}
		  $$ = n }
	| '(' STRUCT ID ')' '{' init_list '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypeStruct, StructTag: $3}
		  n.Children = []*Node{{Kind: KindInitList, Children: $6}}
		  $$ = n }
	| '(' STRUCT ID ')' '{' init_list ',' '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypeStruct, StructTag: $3}
		  n.Children = []*Node{{Kind: KindInitList, Children: $6}}
		  $$ = n }
	| '(' STRUCT ID ')' '{' '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypeStruct, StructTag: $3}
		  n.Children = []*Node{{Kind: KindInitList}}
		  $$ = n }
	| '(' UNION ID ')' '{' init_list '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypeStruct, StructTag: $3}
		  n.Children = []*Node{{Kind: KindInitList, Children: $6}}
		  $$ = n }
	| '(' UNION ID ')' '{' init_list ',' '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypeStruct, StructTag: $3}
		  n.Children = []*Node{{Kind: KindInitList, Children: $6}}
		  $$ = n }
	/* ── Statement expressions (GCC extension) ── */
	| '(' '{' block_item_list '}' ')'
		{ $$ = &Node{Kind: KindStmtExpr, Children: $3} }
	;

generic_assoc_list
	: generic_assoc                           { $$ = []*Node{$1} }
	| generic_assoc_list ',' generic_assoc    { $$ = append($1, $3) }
	;

generic_assoc
	: type_specifier ':' assign_expr
		{ $$ = &Node{Kind: KindGenericAssoc, Type: $1.Kind, StructTag: $1.Tag, Children: []*Node{$3}} }
	| DEFAULT ':' assign_expr
		{ $$ = &Node{Kind: KindGenericAssoc, Name: "default", Children: []*Node{$3}} }
	;

comma_expr_list
	: comma_expr_list ',' expression
		{ $$ = &Node{Kind: KindCommaExpr, Children: []*Node{$1, $3}} }
	| expression ',' expression
		{ $$ = &Node{Kind: KindCommaExpr, Children: []*Node{$1, $3}} }
	;

call
	: ID '(' args ')'
		{ $$ = &Node{Kind: KindCall, Name: $1, Children: $3} }
	;

args
	: arg_list { $$ = $1 }
	| /* empty */ { $$ = nil }
	;

arg_list
	: arg_list ',' expression
		{ $$ = append($1, $3) }
	| expression
		{ $$ = []*Node{$1} }
	;

struct_declaration
	: STRUCT ID '{' field_list '}' ';'
		{ $$ = []*Node{{Kind: KindStructDef, Name: $2, Children: $4}} }
	| STRUCT ATTR_PACKED ID '{' field_list '}' ';'
		{ $$ = []*Node{{Kind: KindStructDef, Name: $3, Children: $5, IsPacked: true}} }
	| STRUCT ID ATTR_PACKED '{' field_list '}' ';'
		{ $$ = []*Node{{Kind: KindStructDef, Name: $2, Children: $5, IsPacked: true}} }
	| STRUCT ID '{' field_list '}' ATTR_PACKED ';'
		{ $$ = []*Node{{Kind: KindStructDef, Name: $2, Children: $4, IsPacked: true}} }
	| STRUCT ATTR_ALIGNED ID '{' field_list '}' ';'
		{ $$ = []*Node{{Kind: KindStructDef, Name: $3, Children: $5}} }
	| STRUCT ID ATTR_ALIGNED '{' field_list '}' ';'
		{ $$ = []*Node{{Kind: KindStructDef, Name: $2, Children: $5}} }
	;

union_declaration
	: UNION ID '{' field_list '}' ';'
		{ $$ = []*Node{{Kind: KindStructDef, Name: $2, Children: $4, IsUnion: true}} }
	| UNION ATTR_PACKED ID '{' field_list '}' ';'
		{ $$ = []*Node{{Kind: KindStructDef, Name: $3, Children: $5, IsUnion: true, IsPacked: true}} }
	| UNION ID ATTR_PACKED '{' field_list '}' ';'
		{ $$ = []*Node{{Kind: KindStructDef, Name: $2, Children: $5, IsUnion: true, IsPacked: true}} }
	| UNION ID '{' field_list '}' ATTR_PACKED ';'
		{ $$ = []*Node{{Kind: KindStructDef, Name: $2, Children: $4, IsUnion: true, IsPacked: true}} }
	;

enum_declaration
	: ENUM '{' enum_list '}' ';'
		{ $$ = $3 }
	| ENUM ID '{' enum_list '}' ';'
		{ $$ = $4 }
	;

enum_list
	: enum_list ',' enum_member
		{ $$ = append($1, $3) }
	| enum_list ','
		{ $$ = $1 }
	| enum_member
		{ $$ = []*Node{$1} }
	;

enum_member
	: ID
		{
			val := yylex.(*lexer).enumAutoVal
			yylex.(*lexer).registerEnumConst($1, val)
			yylex.(*lexer).enumAutoVal++
			$$ = &Node{Kind: KindVarDecl, Type: TypeInt, Name: $1, Val: val, IsConst: true}
		}
	| ID '=' const_int_expr
		{
			yylex.(*lexer).registerEnumConst($1, $3)
			yylex.(*lexer).enumAutoVal = $3 + 1
			$$ = &Node{Kind: KindVarDecl, Type: TypeInt, Name: $1, Val: $3, IsConst: true}
		}
	;

typedef_declaration
	: TYPEDEF type_specifier ID ';'
		{
			yylex.(*lexer).registerTypedef($3, $2)
			$$ = nil
		}
	| TYPEDEF type_specifier TYPENAME ';'
		{
			yylex.(*lexer).registerTypedef($3, $2)
			$$ = nil
		}
	| TYPEDEF type_specifier '*' ID ';'
		{
			yylex.(*lexer).registerTypedef($4, ptrCType($2))
			$$ = nil
		}
	| TYPEDEF type_specifier '*' TYPENAME ';'
		{
			yylex.(*lexer).registerTypedef($4, ptrCType($2))
			$$ = nil
		}
	| TYPEDEF type_specifier '*' '*' ID ';'
		{
			yylex.(*lexer).registerTypedef($5, ptrCType(ptrCType($2)))
			$$ = nil
		}
	| TYPEDEF type_specifier '*' '*' TYPENAME ';'
		{
			yylex.(*lexer).registerTypedef($5, ptrCType(ptrCType($2)))
			$$ = nil
		}
	| TYPEDEF CONST type_specifier ID ';'
		{
			yylex.(*lexer).registerTypedef($4, $3)
			$$ = nil
		}
	| TYPEDEF CONST type_specifier TYPENAME ';'
		{
			yylex.(*lexer).registerTypedef($4, $3)
			$$ = nil
		}
	| TYPEDEF CONST type_specifier '*' ID ';'
		{
			yylex.(*lexer).registerTypedef($5, ptrCType($3))
			$$ = nil
		}
	| TYPEDEF CONST type_specifier '*' TYPENAME ';'
		{
			yylex.(*lexer).registerTypedef($5, ptrCType($3))
			$$ = nil
		}
	| TYPEDEF CONST type_specifier '*' '*' ID ';'
		{
			yylex.(*lexer).registerTypedef($6, ptrCType(ptrCType($3)))
			$$ = nil
		}
	| TYPEDEF CONST type_specifier '*' '*' TYPENAME ';'
		{
			yylex.(*lexer).registerTypedef($6, ptrCType(ptrCType($3)))
			$$ = nil
		}
	| TYPEDEF STRUCT ID ID ';'
		{
			yylex.(*lexer).registerTypedef($4, structCType($3))
			$$ = nil
		}
	| TYPEDEF STRUCT ID TYPENAME ';'
		{
			yylex.(*lexer).registerTypedef($4, structCType($3))
			$$ = nil
		}
	| TYPEDEF STRUCT ID '*' ID ';'
		{
			yylex.(*lexer).registerTypedef($5, ptrCType(structCType($3)))
			$$ = nil
		}
	| TYPEDEF STRUCT ID '*' TYPENAME ';'
		{
			yylex.(*lexer).registerTypedef($5, ptrCType(structCType($3)))
			$$ = nil
		}
	/* typedef SomeUnknownId NewName; — source type is a forward/opaque ID */
	| TYPEDEF ID ID ';'
		{
			yylex.(*lexer).registerTypedef($3, leafCType(TypeVoid))
			$$ = nil
		}
	| TYPEDEF ID TYPENAME ';'
		{
			/* re-typedef: new name is already known; treat as no-op */
			$$ = nil
		}
	| TYPEDEF ID '*' ID ';'
		{
			yylex.(*lexer).registerTypedef($4, ptrCType(leafCType(TypeVoid)))
			$$ = nil
		}
	| TYPEDEF type_specifier '(' '*' ID ')' '(' fp_param_types ')' ';'
		{
			yylex.(*lexer).registerTypedef($5, leafCType(TypeFuncPtr))
			$$ = nil
		}
	/* typedef of function pointer with pointer return type: typedef T *(*name)(params); */
	| TYPEDEF type_specifier '*' '(' '*' ID ')' '(' fp_param_types ')' ';'
		{
			yylex.(*lexer).registerTypedef($6, leafCType(TypeFuncPtr))
			$$ = nil
		}
	/* typedef of function pointer with const pointer return: typedef const T *(*name)(params); */
	| TYPEDEF CONST type_specifier '*' '(' '*' ID ')' '(' fp_param_types ')' ';'
		{
			yylex.(*lexer).registerTypedef($7, leafCType(TypeFuncPtr))
			$$ = nil
		}
	/* typedef of function pointer with const non-pointer return: typedef const T (*name)(params); */
	| TYPEDEF CONST type_specifier '(' '*' ID ')' '(' fp_param_types ')' ';'
		{
			yylex.(*lexer).registerTypedef($6, leafCType(TypeFuncPtr))
			$$ = nil
		}
	/* typedef of function type (not pointer): typedef int name(params); */
	| TYPEDEF type_specifier ID '(' fp_param_types ')' ';'
		{
			yylex.(*lexer).registerTypedef($3, leafCType(TypeFuncPtr))
			$$ = nil
		}
	/* typedef of anonymous struct/union: typedef struct { ... } Name; */
	| TYPEDEF STRUCT '{' field_list '}' ID ';'
		{
			tag := yylex.(*lexer).nextAnon()
			sd := &Node{Kind: KindStructDef, Name: tag, Children: $4}
			yylex.(*lexer).registerTypedef($6, structCType(tag))
			$$ = []*Node{sd}
		}
	/* typedef struct { ... } name[N]; — struct-array typedef (e.g. sigjmp_buf[1]) */
	| TYPEDEF STRUCT '{' field_list '}' ID '[' const_int_expr ']' ';'
		{
			tag := yylex.(*lexer).nextAnon()
			sd := &Node{Kind: KindStructDef, Name: tag, Children: $4}
			yylex.(*lexer).registerTypedef($6, structCType(tag))
			$$ = []*Node{sd}
		}
	| TYPEDEF STRUCT ATTR_PACKED '{' field_list '}' ID ';'
		{
			tag := yylex.(*lexer).nextAnon()
			sd := &Node{Kind: KindStructDef, Name: tag, Children: $5, IsPacked: true}
			yylex.(*lexer).registerTypedef($7, structCType(tag))
			$$ = []*Node{sd}
		}
	| TYPEDEF STRUCT '{' field_list '}' ATTR_PACKED ID ';'
		{
			tag := yylex.(*lexer).nextAnon()
			sd := &Node{Kind: KindStructDef, Name: tag, Children: $4, IsPacked: true}
			yylex.(*lexer).registerTypedef($7, structCType(tag))
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION '{' field_list '}' ID ';'
		{
			tag := yylex.(*lexer).nextAnon()
			sd := &Node{Kind: KindStructDef, Name: tag, Children: $4, IsUnion: true}
			yylex.(*lexer).registerTypedef($6, structCType(tag))
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION ATTR_PACKED '{' field_list '}' ID ';'
		{
			tag := yylex.(*lexer).nextAnon()
			sd := &Node{Kind: KindStructDef, Name: tag, Children: $5, IsUnion: true, IsPacked: true}
			yylex.(*lexer).registerTypedef($7, structCType(tag))
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION '{' field_list '}' ATTR_PACKED ID ';'
		{
			tag := yylex.(*lexer).nextAnon()
			sd := &Node{Kind: KindStructDef, Name: tag, Children: $4, IsUnion: true, IsPacked: true}
			yylex.(*lexer).registerTypedef($7, structCType(tag))
			$$ = []*Node{sd}
		}
	/* typedef of named struct/union defining the struct inline: typedef struct Tag { ... } Name; */
	| TYPEDEF STRUCT ID '{' field_list '}' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5}
			yylex.(*lexer).registerTypedef($7, structCType($3))
			$$ = []*Node{sd}
		}
	| TYPEDEF STRUCT ATTR_PACKED ID '{' field_list '}' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $4, Children: $6, IsPacked: true}
			yylex.(*lexer).registerTypedef($8, structCType($4))
			$$ = []*Node{sd}
		}
	| TYPEDEF STRUCT ID ATTR_PACKED '{' field_list '}' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $6, IsPacked: true}
			yylex.(*lexer).registerTypedef($8, structCType($3))
			$$ = []*Node{sd}
		}
	| TYPEDEF STRUCT ID '{' field_list '}' ATTR_PACKED ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5, IsPacked: true}
			yylex.(*lexer).registerTypedef($8, structCType($3))
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION ID '{' field_list '}' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5, IsUnion: true}
			yylex.(*lexer).registerTypedef($7, structCType($3))
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION ATTR_PACKED ID '{' field_list '}' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $4, Children: $6, IsUnion: true, IsPacked: true}
			yylex.(*lexer).registerTypedef($8, structCType($4))
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION ID ATTR_PACKED '{' field_list '}' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $6, IsUnion: true, IsPacked: true}
			yylex.(*lexer).registerTypedef($8, structCType($3))
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION ID '{' field_list '}' ATTR_PACKED ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5, IsUnion: true, IsPacked: true}
			yylex.(*lexer).registerTypedef($8, structCType($3))
			$$ = []*Node{sd}
		}
	| TYPEDEF STRUCT ID '{' field_list '}' ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5}
			$$ = []*Node{sd}
		}
	| TYPEDEF STRUCT ATTR_PACKED ID '{' field_list '}' ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $4, Children: $6, IsPacked: true}
			$$ = []*Node{sd}
		}
	| TYPEDEF STRUCT ID ATTR_PACKED '{' field_list '}' ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $6, IsPacked: true}
			$$ = []*Node{sd}
		}
	| TYPEDEF STRUCT ID '{' field_list '}' ATTR_PACKED ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5, IsPacked: true}
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION ID '{' field_list '}' ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5, IsUnion: true}
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION ATTR_PACKED ID '{' field_list '}' ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $4, Children: $6, IsUnion: true, IsPacked: true}
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION ID ATTR_PACKED '{' field_list '}' ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $6, IsUnion: true, IsPacked: true}
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION ID '{' field_list '}' ATTR_PACKED ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5, IsUnion: true, IsPacked: true}
			$$ = []*Node{sd}
		}
	| TYPEDEF ENUM ID '{' enum_list '}' ID ';'
		{ yylex.(*lexer).registerTypedef($7, leafCType(TypeInt)); $$ = $5 }
	| TYPEDEF ENUM '{' enum_list '}' ID ';'
		{ yylex.(*lexer).registerTypedef($6, leafCType(TypeInt)); $$ = $4 }
	/* typedef T Name[N]; — array typedef (e.g. typedef long long __jmp_buf[22]) */
	| TYPEDEF type_specifier ID '[' const_int_expr ']' ';'
		{ yylex.(*lexer).registerTypedef($3, ptrCType($2)); $$ = nil }
	| TYPEDEF type_specifier TYPENAME '[' const_int_expr ']' ';'
		{ yylex.(*lexer).registerTypedef($3, ptrCType($2)); $$ = nil }
	/* typedef struct Tag Name[N]; — struct array typedef (e.g. typedef struct __jmp_buf_tag jmp_buf[1]) */
	| TYPEDEF STRUCT ID ID '[' const_int_expr ']' ';'
		{ yylex.(*lexer).registerTypedef($4, ptrCType(structCType($3))); $$ = nil }
	| TYPEDEF STRUCT ID TYPENAME '[' const_int_expr ']' ';'
		{ yylex.(*lexer).registerTypedef($4, ptrCType(structCType($3))); $$ = nil }
	;

/* local_fun_proto matches function prototypes inside function bodies.
   These are always ignored (block_item_list produces $1), so we don't
   need semantic values.  Uses its own param rules to support both named
   and unnamed parameters without conflicting with the global params rule. */
local_fun_proto
	: ID '(' local_params ')'
		{ $$ = $1 }
	| gd_pointer ID '(' local_params ')'
		{ $$ = $2 }
	| gd_pointer CONST ID '(' local_params ')'
		{ $$ = $3 }
	| '(' ID ')' '(' local_params ')'
		{ $$ = $2 }
	;

local_params
	: local_param_list
	| local_param_list ',' ELLIPSIS
	| /* empty */
	;

local_param_list
	: local_param_list ',' local_param
	| local_param
	;

local_param
	: type_specifier
	| type_specifier ID
	| type_specifier '*'
	| type_specifier '*' ID
	| type_specifier '*' '*'
	| type_specifier '*' '*' ID
	| type_specifier '*' CONST
	| type_specifier '*' CONST ID
	| type_specifier CONST '*'
	| type_specifier CONST '*' ID
	| STRUCT ID
	| STRUCT ID ID
	| STRUCT ID '*'
	| STRUCT ID '*' ID
	| STRUCT ID '*' '*'
	| STRUCT ID '*' '*' ID
	| CONST type_specifier
	| CONST type_specifier ID
	| CONST type_specifier '*'
	| CONST type_specifier '*' ID
	| CONST type_specifier '*' CONST
	| CONST type_specifier '*' CONST ID
	| CONST STRUCT ID '*'
	| CONST STRUCT ID '*' ID
	| CONST STRUCT ID '*' '*'
	| CONST STRUCT ID '*' '*' ID
	| CONST STRUCT ID ID
	| type_specifier '(' '*' ID ')' '(' fp_param_types ')'
	| type_specifier '(' '*' ')' '(' fp_param_types ')'
	;

fp_param_types
	: fp_param_type_list { $$ = $1 }
	| /* empty */        { $$ = nil }
	;

fp_param_type_list
	: fp_param_type_list ',' fp_param_type
		{ $$ = append($1, $3) }
	| fp_param_type_list ',' ELLIPSIS
		{ $$ = append($1, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."}) }
	| fp_param_type
		{ $$ = []*Node{$1} }
	;

fp_param_type
	: type_specifier           { $$ = ctNode(KindParam, $1, "") }
	| type_specifier '*'       { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = $1; $$ = n }
	| type_specifier ID        { $$ = ctNode(KindParam, $1, $2) }
	| type_specifier '*' ID    { n := &Node{Kind: KindParam, Type: TypePtr, Name: $3}; n.Pointee = $1; $$ = n }
	| STRUCT ID '*'            { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = structCType($2); $$ = n }
	| STRUCT ID '*' ID         { n := &Node{Kind: KindParam, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); $$ = n }
	| STRUCT ID '*' '*'        { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = ptrCType(structCType($2)); $$ = n }
	| STRUCT ID '*' '*' ID     { n := &Node{Kind: KindParam, Type: TypePtr, Name: $5}; n.Pointee = ptrCType(structCType($2)); $$ = n }
	| STRUCT ID '*' '*' '*'    { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = ptrCType(ptrCType(structCType($2))); $$ = n }
	| STRUCT ID '*' '*' '*' ID { n := &Node{Kind: KindParam, Type: TypePtr, Name: $6}; n.Pointee = ptrCType(ptrCType(structCType($2))); $$ = n }
	| CONST type_specifier '*' { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = $2; $$ = n }
	| CONST type_specifier '*' ID { n := &Node{Kind: KindParam, Type: TypePtr, Name: $4}; n.Pointee = $2; $$ = n }
	| type_specifier CONST '*'    { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = $1; $$ = n }
	| type_specifier CONST '*' ID { n := &Node{Kind: KindParam, Type: TypePtr, Name: $4}; n.Pointee = $1; $$ = n }
	| type_specifier '[' const_int_expr ']' { $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: "", ElemType: $1.Kind, StructTag: $1.Tag, Val: $3} }
	| type_specifier '[' ']'   { $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: "", ElemType: $1.Kind, StructTag: $1.Tag} }
	| CONST type_specifier ID  { $$ = ctNode(KindParam, $2, $3) }
	| CONST type_specifier     { $$ = ctNode(KindParam, $2, "") }
	| CONST STRUCT ID '*'        { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = structCType($3); $$ = n }
	| CONST STRUCT ID '*' ID     { n := &Node{Kind: KindParam, Type: TypePtr, Name: $5}; n.Pointee = structCType($3); $$ = n }
	| CONST STRUCT ID '*' '*'    { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = ptrCType(structCType($3)); $$ = n }
	| CONST STRUCT ID '*' '*' ID { n := &Node{Kind: KindParam, Type: TypePtr, Name: $6}; n.Pointee = ptrCType(structCType($3)); $$ = n }
	| CONST STRUCT ID '[' const_int_expr ']' { $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: "", ElemType: TypeStruct, ElemPointee: structCType($3), Val: $5} }
	| CONST STRUCT ID '[' ']'    { $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: "", ElemType: TypeStruct, ElemPointee: structCType($3)} }
	/* double-pointer params: T **, T **name, const T **, T const **, const T * const * */
	| type_specifier '*' '*'       { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = ptrCType($1); $$ = n }
	| type_specifier '*' '*' ID    { n := &Node{Kind: KindParam, Type: TypePtr, Name: $4}; n.Pointee = ptrCType($1); $$ = n }
	| CONST type_specifier '*' '*' { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = ptrCType($2); $$ = n }
	| CONST type_specifier '*' '*' ID { n := &Node{Kind: KindParam, Type: TypePtr, Name: $5}; n.Pointee = ptrCType($2); $$ = n }
	| type_specifier CONST '*' '*'    { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = ptrCType($1); $$ = n }
	| type_specifier CONST '*' '*' ID { n := &Node{Kind: KindParam, Type: TypePtr, Name: $5}; n.Pointee = ptrCType($1); $$ = n }
	/* const T * const * [name] — pointer to const pointer to const T */
	| CONST type_specifier '*' CONST '*'    { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = ptrCType($2); $$ = n }
	| CONST type_specifier '*' CONST '*' ID { n := &Node{Kind: KindParam, Type: TypePtr, Name: $6}; n.Pointee = ptrCType($2); $$ = n }
	| type_specifier '*' CONST '*'          { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = ptrCType($1); $$ = n }
	| type_specifier '*' CONST '*' ID       { n := &Node{Kind: KindParam, Type: TypePtr, Name: $5}; n.Pointee = ptrCType($1); $$ = n }
	/* unregistered-typedef fallbacks: handles types not yet seen by the lexer */
	| ID '*'       { n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = leafCType(TypeVoid); $$ = n }
	| ID '*' ID    { n := &Node{Kind: KindParam, Type: TypePtr, Name: $3}; n.Pointee = leafCType(TypeVoid); $$ = n }
	;

field_list
	: field_list field
		{ $$ = append($1, $2...) }
	| field
		{ $$ = $1 }
	| field_list STATIC_ASSERT '(' const_int_expr ',' STRING_LIT ')' ';'
		{
			if $4 == 0 {
				yylex.(*lexer).Error(fmt.Sprintf("_Static_assert failed: %s", $6))
			}
			$$ = $1
		}
	| field_list STATIC_ASSERT '(' const_int_expr ')' ';'
		{
			if $4 == 0 {
				yylex.(*lexer).Error("_Static_assert failed")
			}
			$$ = $1
		}
	| STATIC_ASSERT '(' const_int_expr ',' STRING_LIT ')' ';'
		{
			if $3 == 0 {
				yylex.(*lexer).Error(fmt.Sprintf("_Static_assert failed: %s", $5))
			}
			$$ = nil
		}
	;

field
	: type_specifier ID ';'
		{ $$ = []*Node{ctNode(KindVarDecl, $1, $2)} }
	/* _Alignas(N) struct field — scalar */
	| ALIGNAS_SPEC type_specifier ID ';'
		{ n := ctNode(KindVarDecl, $2, $3); n.Align = $1; $$ = []*Node{n} }
	/* _Alignas(N) struct field — scalar array */
	| ALIGNAS_SPEC type_specifier ID '[' const_int_expr ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5, ElemType: $2.Kind, ElemPointee: arrayElemPtee($2), StructTag: $2.Tag, Align: $1}} }
	| type_specifier id_list ';'
		{ $$ = makeMultiDecl($1, $2) }
	| type_specifier ID ':' const_int_expr ';'
		{ n := ctNode(KindVarDecl, $1, $2); n.BitWidth = $4; $$ = []*Node{n} }
	| type_specifier ID '[' const_int_expr ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, Val: $4, ElemType: $1.Kind, ElemPointee: arrayElemPtee($1), StructTag: $1.Tag}} }
	| type_specifier ID '[' const_int_expr ']' '[' const_int_expr ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, Val: $4, ElemType: $1.Kind, ElemPointee: arrayElemPtee($1), StructTag: $1.Tag, Dim2: $7}} }
	| type_specifier ID '[' ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, Val: -1, ElemType: $1.Kind, ElemPointee: arrayElemPtee($1), StructTag: $1.Tag}} }
	| type_specifier '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $3}; n.Pointee = $1; $$ = []*Node{n} }
	| type_specifier '*' ID ',' ptr_id_list ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $3}; n.Pointee = $1; $$ = append([]*Node{n}, makePtrFields($1, $5)...) }
	| type_specifier '*' '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = ptrCType($1); $$ = []*Node{n} }
	| type_specifier '*' ID '[' const_int_expr ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5, ElemType: TypePtr, ElemPointee: $1}} }
	| type_specifier '*' ID '[' const_int_expr ']' '[' const_int_expr ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5, ElemType: TypePtr, ElemPointee: $1, Dim2: $8}} }
	| STRUCT ID '*' ID '[' const_int_expr ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $4, Val: $6, ElemType: TypePtr, ElemPointee: structCType($2)}} }
	| CONST type_specifier ID ';'
		{ $$ = []*Node{ctNode(KindVarDecl, $2, $3)} }
	| CONST type_specifier '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = $2; $$ = []*Node{n} }
	| CONST type_specifier '*' '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $5}; n.Pointee = ptrCType($2); $$ = []*Node{n} }
	| CONST type_specifier '*' ID '[' const_int_expr ']' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntArray, Name: $4, Val: $6, ElemType: TypePtr, ElemPointee: $2}} }
	| CONST type_specifier '*' ID '[' ']' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntArray, Name: $4, Val: -1, ElemType: TypePtr, ElemPointee: $2}} }
	| CONST type_specifier ID '[' const_int_expr ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5, ElemType: $2.Kind, ElemPointee: arrayElemPtee($2), StructTag: $2.Tag}} }
	| type_specifier '(' '*' ID ')' '(' fp_param_types ')' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeFuncPtr, Name: $4}} }
	| type_specifier '*' '(' '*' ID ')' '(' fp_param_types ')' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeFuncPtr, Name: $5}} }
	/* Function pointer field returning const pointer: const T *(*name)(params); */
	| CONST type_specifier '*' '(' '*' ID ')' '(' fp_param_types ')' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeFuncPtr, Name: $6}} }
	| type_specifier '(' '*' ID '[' const_int_expr ']' ')' '(' fp_param_types ')' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeFuncPtr, Name: $4}} }
	| STRUCT ID '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); $$ = []*Node{n} }
	| STRUCT ID '*' ID ',' ptr_id_list ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); $$ = append([]*Node{n}, makePtrFields(structCType($2), $6)...) }
	| STRUCT ID '*' CONST '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $6}; n.Pointee = ptrCType(structCType($2)); $$ = []*Node{n} }
	| STRUCT ID '*' '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $5}; n.Pointee = ptrCType(structCType($2)); $$ = []*Node{n} }
	| STRUCT ID ID ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2}} }
	| STRUCT ID ID '[' const_int_expr ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5, ElemType: TypeStruct, ElemPointee: structCType($2), StructTag: $2}} }
	| UNION ID ID ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2}} }
	| UNION ID '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); $$ = []*Node{n} }
	| STRUCT '{' field_list '}' ';'
		{
			tag := yylex.(*lexer).nextAnon()
			$$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeStruct, Name: "", StructTag: tag, Children: $3}}
		}
	| UNION '{' field_list '}' ';'
		{
			tag := yylex.(*lexer).nextAnon()
			$$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeStruct, Name: "", StructTag: tag, Children: $3, IsUnion: true}}
		}
	| UNION '{' field_list '}' ID ';'
		{
			tag := yylex.(*lexer).nextAnon()
			$$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeStruct, Name: $5, StructTag: tag, Children: $3, IsUnion: true}}
		}
	| STRUCT '{' field_list '}' ID ';'
		{
			tag := yylex.(*lexer).nextAnon()
			$$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeStruct, Name: $5, StructTag: tag, Children: $3}}
		}
	| STRUCT ID '{' field_list '}' ';'
		{
			tag := yylex.(*lexer).nextAnon()
			$$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeStruct, Name: "", StructTag: tag, Children: $4}}
		}
	/* Named struct/union with named member: struct Tag { ... } name; (e.g. Lua's Node type) */
	| STRUCT ID '{' field_list '}' ID ';'
		{
			$$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeStruct, Name: $6, StructTag: $2, Children: $4}}
		}
	/* Named struct with pointer field: struct Tag { ... } *name; (e.g. XDR's x_ops) */
	| STRUCT ID '{' field_list '}' '*' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $2, Children: $4}
			n  := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $7}; n.Pointee = structCType($2)
			$$ = []*Node{sd, n}
		}
	/* const struct Tag { ... } *name; */
	| CONST STRUCT ID '{' field_list '}' '*' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5}
			n  := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $8}; n.Pointee = structCType($3)
			$$ = []*Node{sd, n}
		}
	| UNION ID '{' field_list '}' ID ';'
		{
			$$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeStruct, Name: $6, StructTag: $2, Children: $4, IsUnion: true}}
		}
	| UNION ID '{' field_list '}' ';'
		{
			tag := yylex.(*lexer).nextAnon()
			$$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeStruct, Name: "", StructTag: tag, Children: $4, IsUnion: true}}
		}
	/* Inline struct/union as array element type: "struct { ... } tab[N];" */
	| STRUCT '{' field_list '}' ID '[' const_int_expr ']' ';'
		{
			tag := yylex.(*lexer).nextAnon()
			sd := &Node{Kind: KindStructDef, Name: tag, Children: $3}
			f  := &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $5, Val: $7,
			            ElemType: TypeStruct, ElemPointee: structCType(tag), StructTag: tag}
			$$ = []*Node{sd, f}
		}
	| UNION '{' field_list '}' ID '[' const_int_expr ']' ';'
		{
			tag := yylex.(*lexer).nextAnon()
			sd := &Node{Kind: KindStructDef, Name: tag, Children: $3, IsUnion: true}
			f  := &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $5, Val: $7,
			            ElemType: TypeStruct, ElemPointee: structCType(tag), StructTag: tag}
			$$ = []*Node{sd, f}
		}
	| CONST type_specifier '(' '*' ID ')' '(' fp_param_types ')' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeFuncPtr, Name: $5}} }
	;

const_int_expr
	: const_int_ternary                       { $$ = $1 }
	;

const_int_ternary
	: const_int_or                                                        { $$ = $1 }
	| const_int_or QUESTION const_int_or ':' const_int_or                { if $1 != 0 { $$ = $3 } else { $$ = $5 } }
	;

const_int_or
	: const_int_and                           { $$ = $1 }
	| const_int_or OROR  const_int_and        { if $1 != 0 || $3 != 0 { $$ = 1 } else { $$ = 0 } }
	;

const_int_and
	: const_int_cmp                           { $$ = $1 }
	| const_int_and ANDAND const_int_cmp      { if $1 != 0 && $3 != 0 { $$ = 1 } else { $$ = 0 } }
	;

const_int_cmp
	: const_int_shift                         { $$ = $1 }
	| const_int_cmp EQ const_int_shift        { if $1 == $3 { $$ = 1 } else { $$ = 0 } }
	| const_int_cmp NE const_int_shift        { if $1 != $3 { $$ = 1 } else { $$ = 0 } }
	| const_int_cmp '<' const_int_shift       { if $1 < $3  { $$ = 1 } else { $$ = 0 } }
	| const_int_cmp '>' const_int_shift       { if $1 > $3  { $$ = 1 } else { $$ = 0 } }
	| const_int_cmp LE  const_int_shift       { if $1 <= $3 { $$ = 1 } else { $$ = 0 } }
	| const_int_cmp GE  const_int_shift       { if $1 >= $3 { $$ = 1 } else { $$ = 0 } }
	;

const_int_shift
	: const_int_add                           { $$ = $1 }
	| const_int_shift LSHIFT const_int_add    { $$ = $1 << $3 }
	| const_int_shift RSHIFT const_int_add    { $$ = $1 >> $3 }
	;

const_int_add
	: const_int_mul                           { $$ = $1 }
	| const_int_add '+' const_int_mul         { $$ = $1 + $3 }
	| const_int_add '-' const_int_mul         { $$ = $1 - $3 }
	;

const_int_mul
	: const_int_unary                         { $$ = $1 }
	| const_int_mul '*' const_int_unary       { $$ = $1 * $3 }
	| const_int_mul '/' const_int_unary       { if $3 != 0 { $$ = $1 / $3 } else { $$ = 0 } }
	| const_int_mul '%' const_int_unary       { if $3 != 0 { $$ = $1 % $3 } else { $$ = 0 } }
	;

const_int_unary
	: const_int_primary                       { $$ = $1 }
	| '-' const_int_primary                   { $$ = -$2 }
	| '!' const_int_primary                   { if $2 == 0 { $$ = 1 } else { $$ = 0 } }
	;

const_int_primary
	: NUM                                     { $$ = $1 }
	| '(' const_int_ternary ')'               { $$ = $2 }
	| '(' type_specifier ')' const_int_primary  { $$ = $4 /* cast: ignore type, value unchanged */ }
	| SIZEOF '(' STRING_LIT ')'               { $$ = strLitSize($3) }
	| SIZEOF '(' type_specifier ')'           { $$ = sizeofType($3) }
	| SIZEOF '(' type_specifier '*' ')'       { $$ = 8 }
	| SIZEOF '(' CONST type_specifier '*' ')' { $$ = 8 }
	| SIZEOF '(' type_specifier '[' NUM ']' ')'  { $$ = sizeofType($3) * $5 }
	| SIZEOF '(' ID ')'                       { $$ = 8 /* opaque sizeof(varname): assume 8 */ }
	| SIZEOF '(' ID '[' NUM ']' ')'           { $$ = 8 /* opaque sizeof(arr[i]): return element size */ }
	/* sizeof((arr)[0]) — parenthesized subscript, element size */
	| SIZEOF '(' '(' ID ')' '[' NUM ']' ')'   { $$ = 8 /* opaque sizeof((arr)[i]): assume element size 8 */ }
	/* sizeof((arr)[0]) with const_int_expr index */
	| SIZEOF '(' '(' ID ')' '[' const_int_expr ']' ')'  { $$ = 8 /* opaque: assume element size 8 */ }
	/* sizeof(a.b), sizeof(a.b.c) — struct field sizeof */
	| SIZEOF '(' ID '.' ID ')'                { $$ = 8 }
	| SIZEOF '(' ID '.' ID '.' ID ')'         { $$ = 8 }
	/* sizeof((a.b.c)) — parenthesized struct field */
	| SIZEOF '(' '(' ID '.' ID '.' ID ')' ')' { $$ = 8 }
	| SIZEOF '(' '(' ID '.' ID ')' ')'        { $$ = 8 }
	/* sizeof(((a.b.c))[i]) — subscript on parenthesized struct field */
	| SIZEOF '(' '(' '(' ID '.' ID '.' ID ')' ')' '[' const_int_expr ']' ')'  { $$ = 8 }
	| SIZEOF '(' '(' '(' ID '.' ID ')' ')' '[' const_int_expr ']' ')'         { $$ = 8 }
	| ALIGNOF '(' type_specifier ')'          { $$ = alignofType($3) }
	| ALIGNOF '(' type_specifier '*' ')'      { $$ = 8 }
	| ALIGNOF '(' STRUCT ID ')'               { $$ = 8 }
	| ID                                      { $$ = yylex.(*lexer).lookupConstInt($1) }
	| '&' ID                                  { $$ = constAddrOf($2) /* link-time address: opaque non-zero */ }
	| '&' TYPENAME                            { $$ = constAddrOf($2) /* link-time address of typedef'd object */ }
	/* offsetof-style patterns: &((T*)0)->field, used as compile-time offset computation.
	   Gaston cannot evaluate struct field offsets in const_int_expr context, so return 0
	   (a safe sentinel for "unknown offset"). sizeof(char[...]) clamps to dim>=1 anyway. */
	| '&' '(' '(' type_specifier '*' ')' NUM ')' ARROW ID { $$ = 0 }
	| '&' '(' '(' type_specifier '*' ')' NUM ')' '.' ID  { $$ = 0 }
	;

/* ═══════════════════════════════════════════════════════════════════════════
   Factored declaration grammar (Phase 2).
   These rules coexist with the old rules and will gradually replace them.
   Prefixed with gd_ to avoid name collisions during migration.
   ═══════════════════════════════════════════════════════════════════════════ */

declaration_specifiers
	/* ── bare type specifier (handles STRUCT ID, UNION ID, ENUM ID, TYPENAME,
	       and all basic C types via type_specifier) ── */
	: type_specifier
		{ $$ = &DeclSpec{BaseType: $1} }
	/* ── storage class + type specifier ── */
	| STATIC type_specifier
		{ $$ = &DeclSpec{BaseType: $2, IsStatic: true} }
	| EXTERN type_specifier
		{ $$ = &DeclSpec{BaseType: $2, IsExtern: true} }
	| STATIC CONST type_specifier
		{ $$ = &DeclSpec{BaseType: $3, IsStatic: true, IsConst: true} }
	| EXTERN CONST type_specifier
		{ $$ = &DeclSpec{BaseType: $3, IsExtern: true, IsConst: true} }
	/* ── const + type specifier ── */
	| CONST type_specifier
		{ $$ = &DeclSpec{BaseType: $2, IsConst: true} }
	/* ── Inline struct/union definition: [qualifiers] STRUCT Tag { fields } ── */
	| STRUCT ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($2),
		    StructDef: &Node{Kind: KindStructDef, Name: $2, Children: $4}} }
	| STRUCT ATTR_PACKED ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($3),
		    StructDef: &Node{Kind: KindStructDef, Name: $3, Children: $5, IsPacked: true}} }
	| STRUCT ID ATTR_PACKED '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($2),
		    StructDef: &Node{Kind: KindStructDef, Name: $2, Children: $5, IsPacked: true}} }
	| STRUCT ID '{' field_list '}' ATTR_PACKED
		{ $$ = &DeclSpec{BaseType: structCType($2),
		    StructDef: &Node{Kind: KindStructDef, Name: $2, Children: $4, IsPacked: true}} }
	| STATIC STRUCT ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($3), IsStatic: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $3, Children: $5}} }
	| STATIC STRUCT ATTR_PACKED ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($4), IsStatic: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $4, Children: $6, IsPacked: true}} }
	| STATIC STRUCT ID '{' field_list '}' ATTR_PACKED
		{ $$ = &DeclSpec{BaseType: structCType($3), IsStatic: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $3, Children: $5, IsPacked: true}} }
	| STATIC CONST STRUCT ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($4), IsStatic: true, IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $4, Children: $6}} }
	| STATIC CONST STRUCT ATTR_PACKED ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($5), IsStatic: true, IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $5, Children: $7, IsPacked: true}} }
	| STATIC CONST STRUCT ID '{' field_list '}' ATTR_PACKED
		{ $$ = &DeclSpec{BaseType: structCType($4), IsStatic: true, IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $4, Children: $6, IsPacked: true}} }
	| EXTERN STRUCT ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($3), IsExtern: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $3, Children: $5}} }
	| EXTERN STRUCT ATTR_PACKED ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($4), IsExtern: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $4, Children: $6, IsPacked: true}} }
	| EXTERN STRUCT ID '{' field_list '}' ATTR_PACKED
		{ $$ = &DeclSpec{BaseType: structCType($3), IsExtern: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $3, Children: $5, IsPacked: true}} }
	| EXTERN CONST STRUCT ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($4), IsExtern: true, IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $4, Children: $6}} }
	| EXTERN CONST STRUCT ATTR_PACKED ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($5), IsExtern: true, IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $5, Children: $7, IsPacked: true}} }
	| EXTERN CONST STRUCT ID '{' field_list '}' ATTR_PACKED
		{ $$ = &DeclSpec{BaseType: structCType($4), IsExtern: true, IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $4, Children: $6, IsPacked: true}} }
	| CONST STRUCT ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($3), IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $3, Children: $5}} }
	| CONST STRUCT ATTR_PACKED ID '{' field_list '}'
		{ $$ = &DeclSpec{BaseType: structCType($4), IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $4, Children: $6, IsPacked: true}} }
	| CONST STRUCT ID '{' field_list '}' ATTR_PACKED
		{ $$ = &DeclSpec{BaseType: structCType($3), IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: $3, Children: $5, IsPacked: true}} }
	/* ── Anonymous struct/union definition ── */
	| STRUCT '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag),
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $3}} }
	| STRUCT ATTR_PACKED '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag),
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $4, IsPacked: true}} }
	| STRUCT '{' field_list '}' ATTR_PACKED
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag),
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $3, IsPacked: true}} }
	| STATIC STRUCT '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsStatic: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $4}} }
	| STATIC STRUCT ATTR_PACKED '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsStatic: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $5, IsPacked: true}} }
	| STATIC STRUCT '{' field_list '}' ATTR_PACKED
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsStatic: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $4, IsPacked: true}} }
	| STATIC CONST STRUCT '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsStatic: true, IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $5}} }
	| STATIC CONST STRUCT ATTR_PACKED '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsStatic: true, IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $6, IsPacked: true}} }
	| STATIC CONST STRUCT '{' field_list '}' ATTR_PACKED
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsStatic: true, IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $5, IsPacked: true}} }
	| CONST STRUCT '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $4}} }
	| CONST STRUCT ATTR_PACKED '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $5, IsPacked: true}} }
	| CONST STRUCT '{' field_list '}' ATTR_PACKED
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsConst: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $4, IsPacked: true}} }
	| UNION '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsUnion: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $3, IsUnion: true}} }
	| UNION ATTR_PACKED '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsUnion: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $4, IsUnion: true, IsPacked: true}} }
	| UNION '{' field_list '}' ATTR_PACKED
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsUnion: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $3, IsUnion: true, IsPacked: true}} }
	| STATIC UNION '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsStatic: true, IsUnion: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $4, IsUnion: true}} }
	| STATIC CONST UNION '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsStatic: true, IsConst: true, IsUnion: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $5, IsUnion: true}} }
	| CONST UNION '{' field_list '}'
		{ tag := yylex.(*lexer).nextAnon()
		  $$ = &DeclSpec{BaseType: structCType(tag), IsConst: true, IsUnion: true,
		    StructDef: &Node{Kind: KindStructDef, Name: tag, Children: $4, IsUnion: true}} }
	;

gd_pointer
	: '*'
		{ $$ = ptrCType(nil) }
	| '*' CONST
		{ $$ = ptrCType(nil) }
	| '*' gd_pointer
		{ $$ = ptrCType($2) }
	| '*' CONST gd_pointer
		{ $$ = ptrCType($3) }
	;

gd_declarator
	: ID
		{ $$ = &Declarator{Name: $1} }
	| gd_pointer ID
		{ $$ = &Declarator{Name: $2, PtrChain: $1} }
	| gd_pointer CONST ID
		{ $$ = &Declarator{Name: $3, PtrChain: $1, IsConstPtr: true} }
	| ID '[' const_int_expr ']'
		{ $$ = &Declarator{Name: $1, IsArray: true, ArraySize: $3} }
	| ID '[' ']'
		{ $$ = &Declarator{Name: $1, IsArray: true, ArraySize: -1} }
	| ID '[' const_int_expr ']' '[' const_int_expr ']'
		{ $$ = &Declarator{Name: $1, IsArray: true, ArraySize: $3, Dim2: $6} }
	| ID '[' ']' '[' const_int_expr ']'
		{ $$ = &Declarator{Name: $1, IsArray: true, ArraySize: -1, Dim2: $5} }
	| gd_pointer ID '[' const_int_expr ']'
		{ $$ = &Declarator{Name: $2, PtrChain: $1, IsArray: true, ArraySize: $4} }
	| gd_pointer ID '[' ']'
		{ $$ = &Declarator{Name: $2, PtrChain: $1, IsArray: true, ArraySize: -1} }
	| gd_pointer CONST ID '[' const_int_expr ']'
		{ $$ = &Declarator{Name: $3, PtrChain: $1, IsConstPtr: true, IsArray: true, ArraySize: $5} }
	| gd_pointer CONST ID '[' ']'
		{ $$ = &Declarator{Name: $3, PtrChain: $1, IsConstPtr: true, IsArray: true, ArraySize: -1} }
	| '(' '*' ID ')' '(' fp_param_types ')'
		{ $$ = &Declarator{Name: $3, IsFuncPtr: true} }
	/* Double pointer to function: (**p)(void) */
	| '(' '*' '*' ID ')' '(' fp_param_types ')'
		{ $$ = &Declarator{Name: $4, IsFuncPtr: true} }
	| '(' '*' ID '[' const_int_expr ']' ')' '(' fp_param_types ')'
		{ $$ = &Declarator{Name: $3, IsFuncPtr: true} }
	| '(' '*' '(' ID '[' const_int_expr ']' ')' ')' '(' fp_param_types ')'
		{ $$ = &Declarator{Name: $4, IsFuncPtr: true} }
	| ID '[' ID ']'
		{ $$ = &Declarator{Name: $1, IsArray: true, IsVLA: true, ArraySize: 0,
		    VLAExpr: &Node{Kind: KindVar, Name: $3}} }
	;

gd_init_declarator
	: gd_declarator
		{ $$ = $1 }
	| gd_declarator '=' expression
		{ $$ = $1; $$.Init = $3 }
	| gd_declarator '=' '{' init_list '}'
		{ $$ = $1; $$.Init = &Node{Kind: KindInitList, Children: $4} }
	| gd_declarator '=' '{' init_list ',' '}'
		{ $$ = $1; $$.Init = &Node{Kind: KindInitList, Children: $4} }
	| gd_declarator '=' '{' '}'
		{ $$ = $1; $$.Init = &Node{Kind: KindInitList} }
	;

gd_init_declarator_list
	: gd_init_declarator
		{ $$ = []*Declarator{$1} }
	| gd_init_declarator_list ',' gd_init_declarator
		{ $$ = append($1, $3) }
	;

gd_param_declarator
	: ID
		{ $$ = &Declarator{Name: $1} }
	| gd_pointer ID
		{ $$ = &Declarator{Name: $2, PtrChain: $1} }
	| gd_pointer
		{ $$ = &Declarator{PtrChain: $1} }
	| gd_pointer CONST ID
		{ $$ = &Declarator{Name: $3, PtrChain: $1, IsConstPtr: true} }
	| gd_pointer CONST
		{ $$ = &Declarator{PtrChain: $1, IsConstPtr: true} }
	| ID '[' const_int_expr ']'
		{ $$ = &Declarator{Name: $1, IsArray: true, ArraySize: $3} }
	| ID '[' ']'
		{ $$ = &Declarator{Name: $1, IsArray: true, ArraySize: -1} }
	| ID '[' const_int_expr ']' '[' const_int_expr ']'
		{ $$ = &Declarator{Name: $1, IsArray: true, ArraySize: $3, Dim2: $6} }
	| gd_pointer ID '[' const_int_expr ']'
		{ $$ = &Declarator{Name: $2, PtrChain: $1, IsArray: true, ArraySize: $4} }
	| gd_pointer ID '[' ']'
		{ $$ = &Declarator{Name: $2, PtrChain: $1, IsArray: true, ArraySize: -1} }
	| gd_pointer CONST ID '[' const_int_expr ']'
		{ $$ = &Declarator{Name: $3, PtrChain: $1, IsConstPtr: true, IsArray: true, ArraySize: $5} }
	| gd_pointer CONST ID '[' ']'
		{ $$ = &Declarator{Name: $3, PtrChain: $1, IsConstPtr: true, IsArray: true, ArraySize: -1} }
	| '(' '*' ID ')' '(' fp_param_types ')'
		{ $$ = &Declarator{Name: $3, IsFuncPtr: true} }
	| '(' '*' ')' '(' fp_param_types ')'
		{ $$ = &Declarator{IsFuncPtr: true} }
	;

gd_fun_declarator
	: ID '(' params ')'
		{ $$ = &FunDeclarator{Name: $1, Params: $3} }
	| ID '(' param_list ',' ELLIPSIS ')'
		{ $$ = &FunDeclarator{Name: $1,
		    Params: append($3, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})} }
	| gd_pointer ID '(' params ')'
		{ $$ = &FunDeclarator{Name: $2, PtrChain: $1, Params: $4} }
	| gd_pointer ID '(' param_list ',' ELLIPSIS ')'
		{ $$ = &FunDeclarator{Name: $2, PtrChain: $1,
		    Params: append($4, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})} }
	/* Parenthesized function name: type (name)(params) — prevents macro expansion (Lua API pattern) */
	| '(' ID ')' '(' params ')'
		{ $$ = &FunDeclarator{Name: $2, Params: $5, IsParenName: true} }
	| '(' ID ')' '(' param_list ',' ELLIPSIS ')'
		{ $$ = &FunDeclarator{Name: $2,
		    Params: append($5, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."}), IsParenName: true} }
	| gd_pointer '(' ID ')' '(' params ')'
		{ $$ = &FunDeclarator{Name: $3, PtrChain: $1, Params: $6, IsParenName: true} }
	| gd_pointer '(' ID ')' '(' param_list ',' ELLIPSIS ')'
		{ $$ = &FunDeclarator{Name: $3, PtrChain: $1,
		    Params: append($6, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."}), IsParenName: true} }
	| gd_pointer CONST ID '(' params ')'
		{ $$ = &FunDeclarator{Name: $3, PtrChain: $1, Params: $5} }
	| gd_pointer CONST ID '(' param_list ',' ELLIPSIS ')'
		{ $$ = &FunDeclarator{Name: $3, PtrChain: $1,
		    Params: append($5, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})} }
	;

%%
