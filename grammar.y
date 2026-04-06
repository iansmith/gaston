// grammar.y — C-minus grammar for the gaston compiler.
// Generate the parser with:  goyacc -o parser.go grammar.y
//
// S/R conflicts: ~8 from multi-keyword type specifiers (LONG LONG, UNSIGNED INT, etc.),
// all resolved correctly by default shift preference.
// Dangling-else resolved via %prec LOWER_THAN_ELSE.
%{
package main
%}

%union {
	ival  int
	fval  float64
	sval  string
	node  *Node
	nodes []*Node
	typ   *CType
}

// Literals
%token <ival> NUM CHAR_LIT
%token <fval> FNUM
%token <sval> ID STRING_LIT

// Keywords
%token INT VOID IF ELSE WHILE RETURN FOR DO BREAK CONTINUE CONST CHAR EXTERN GOTO
%token LONG UNSIGNED SHORT FLOAT DOUBLE STRUCT SIZEOF ENUM UNION TYPEDEF STATIC VA_ARG TYPEOF INT128 SIGNED
%token <sval> TYPENAME

// Multi-character operators
%token LE GE EQ NE LSHIFT RSHIFT
%token INC DEC PLUSEQ MINUSEQ STAREQ DIVEQ MODEQ
%token ANDEQ OREQ XOREQ SHLEQ SHREQ
%token ARROW ELLIPSIS
%token ANDAND OROR
%token QUESTION

// Types for non-terminals
%type <node>  program fun_declaration
%type <nodes> declaration var_declaration const_declaration extern_declaration declaration_list params param_list id_list
%type <nodes> struct_declaration field_list union_declaration enum_declaration enum_list typedef_declaration
%type <nodes> fp_param_types fp_param_type_list
%type <nodes> field
%type <ival>  const_int_expr const_int_add const_int_mul const_int_primary
%type <node>  param compound_stmt postfix_expr enum_member fp_param_type
%type <nodes> local_declarations statement_list
%type <nodes> init_list
%type <node>  init_entry assign_expr
%type <node>  statement expression_stmt selection_stmt iteration_stmt for_stmt do_while_stmt return_stmt break_stmt continue_stmt goto_stmt
%type <node>  opt_expression
%type <node>  expression var simple_expression ternary_expression logical_expression comparison_expression bitwise_expression additive_expression term factor call
%type <nodes> args arg_list
%type <typ>   type_specifier
%type <sval>  relop addop mulop bitwiseop

// Logical operators: || < &&
%left OROR
%left ANDAND

// Resolve dangling-else: ELSE binds to the nearest IF.
%nonassoc LOWER_THAN_ELSE
%nonassoc ELSE

%%

program
	: declaration_list
		{ yylex.(*lexer).result = &Node{Kind: KindProgram, Children: $1} }
	;

declaration_list
	: declaration_list declaration
		{ $$ = append($1, $2...) }
	| declaration
		{ $$ = $1 }
	;

declaration
	: var_declaration    { $$ = $1 }
	| const_declaration  { $$ = $1 }
	| fun_declaration    { $$ = []*Node{$1} }
	| extern_declaration { $$ = $1 }
	| struct_declaration { $$ = $1 }
	| union_declaration  { $$ = $1 }
	| enum_declaration   { $$ = $1 }
	| typedef_declaration { $$ = $1 }
	;

extern_declaration
	/* Forward struct/union declaration: struct Foo; */
	: STRUCT ID ';'
		{ $$ = nil }
	| UNION ID ';'
		{ $$ = nil }
	| EXTERN type_specifier ID ';'
		{ n := ctNode(KindVarDecl, $2, $3); n.IsExtern = true; $$ = []*Node{n} }
	| EXTERN type_specifier ID '[' ']' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, IsExtern: true, ElemType: $2.Kind}} }
	| EXTERN type_specifier '*' ID '[' ']' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntArray, Name: $4, IsExtern: true, ElemType: TypePtr, ElemPointee: $2}} }
	| EXTERN type_specifier '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4, IsExtern: true}; n.Pointee = $2; $$ = []*Node{n} }
	| EXTERN type_specifier '*' '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $5, IsExtern: true}; n.Pointee = ptrCType($2); $$ = []*Node{n} }
	| EXTERN STRUCT ID '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $5, IsExtern: true}; n.Pointee = structCType($3); $$ = []*Node{n} }
	| EXTERN STRUCT ID ID '(' params ')' ';'
		{
			n := &Node{Kind: KindFunDecl, Type: TypeStruct, StructTag: $3, Name: $4}
			n.IsExtern = true; n.Children = $6; $$ = []*Node{n}
		}
	| EXTERN STRUCT ID ID '(' param_list ',' ELLIPSIS ')' ';'
		{
			n := &Node{Kind: KindFunDecl, Type: TypeStruct, StructTag: $3, Name: $4}
			n.IsExtern = true
			n.Children = append($6, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})
			$$ = []*Node{n}
		}
	| EXTERN type_specifier ID '(' params ')' ';'
		{
			n := ctNode(KindFunDecl, $2, $3); n.IsExtern = true
			n.Children = $5
			$$ = []*Node{n}
		}
	| EXTERN type_specifier ID '(' param_list ',' ELLIPSIS ')' ';'
		{
			n := ctNode(KindFunDecl, $2, $3); n.IsExtern = true
			n.Children = append($5, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})
			$$ = []*Node{n}
		}
	/* Bare function prototypes (no extern keyword) — common in system headers.
	   Note: $indices differ from the EXTERN versions (no leading EXTERN token). */
	| type_specifier ID '(' params ')' ';'
		{
			n := ctNode(KindFunDecl, $1, $2); n.IsExtern = true
			n.Children = $4
			$$ = []*Node{n}
		}
	| type_specifier ID '(' param_list ',' ELLIPSIS ')' ';'
		{
			n := ctNode(KindFunDecl, $1, $2); n.IsExtern = true
			n.Children = append($4, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})
			$$ = []*Node{n}
		}
	| type_specifier '*' ID '(' params ')' ';'
		{
			n := &Node{Kind: KindFunDecl, Type: TypePtr, Name: $3}
			n.Pointee = $1; n.IsExtern = true; n.Children = $5
			$$ = []*Node{n}
		}
	| STRUCT ID ID '(' params ')' ';'
		{
			n := &Node{Kind: KindFunDecl, Type: TypeStruct, StructTag: $2, Name: $3}
			n.IsExtern = true; n.Children = $5
			$$ = []*Node{n}
		}
	/* Combined struct definition + extern variable: "extern [const] struct Name { fields } var;" */
	| EXTERN STRUCT ID '{' field_list '}' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5}
			vd := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $7, StructTag: $3, IsExtern: true}
			$$ = []*Node{sd, vd}
		}
	| EXTERN CONST STRUCT ID '{' field_list '}' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $4, Children: $6}
			vd := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $8, StructTag: $4, IsExtern: true}
			$$ = []*Node{sd, vd}
		}
	| CONST STRUCT ID '{' field_list '}' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5}
			vd := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $7, StructTag: $3}
			$$ = []*Node{sd, vd}
		}
	;

var_declaration
	: type_specifier ID ';'
		{ n := ctNode(KindVarDecl, $1, $2); maybeSetTypeofExpr(n, $1, yylex); $$ = []*Node{n} }
	| type_specifier ID '[' NUM ']' '[' NUM ']' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, Val: $4, Dim2: $7, ElemType: $1.Kind}} }
	| type_specifier ID '[' NUM ']' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, Val: $4, ElemType: $1.Kind}} }
	| type_specifier ID '[' ID ']' ';'
		{
			n := &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, IsVLA: true, ElemType: $1.Kind}
			n.Children = []*Node{{Kind: KindVar, Name: $4}}
			$$ = []*Node{n}
		}
	| type_specifier ID '=' expression ';'
		{ n := ctNode(KindVarDecl, $1, $2); n.Children = []*Node{$4}; maybeSetTypeofExpr(n, $1, yylex); $$ = []*Node{n} }
	| type_specifier id_list ';'
		{ $$ = makeMultiDecl($1, $2) }
	| type_specifier '*' ID '[' NUM ']' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5, ElemType: TypePtr, ElemPointee: $1}} }
	| type_specifier '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $3}; n.Pointee = $1; $$ = []*Node{n} }
	| type_specifier '*' ID '=' expression ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $3}; n.Pointee = $1; n.Children = []*Node{$5}; $$ = []*Node{n} }
	| type_specifier '*' '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = ptrCType($1); $$ = []*Node{n} }
	| type_specifier '*' '*' ID '=' expression ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = ptrCType($1); n.Children = []*Node{$6}; $$ = []*Node{n} }
	| type_specifier '*' '*' '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $5}; n.Pointee = ptrCType(ptrCType($1)); $$ = []*Node{n} }
	| STRUCT ID ID ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2}} }
	| STRUCT ID '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); $$ = []*Node{n} }
	| STRUCT ID '*' ID '=' expression ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); n.Children = []*Node{$6}; $$ = []*Node{n} }
	| STRUCT ID '*' '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $5}; n.Pointee = ptrCType(structCType($2)); $$ = []*Node{n} }
	| UNION ID ID ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2}} }
	| UNION ID '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); $$ = []*Node{n} }
	| type_specifier '(' '*' ID ')' '(' fp_param_types ')' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeFuncPtr, Name: $4}} }
	| CONST type_specifier '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4, IsConstTarget: true}; n.Pointee = $2; $$ = []*Node{n} }
	| CONST type_specifier '*' ID '=' expression ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4, IsConstTarget: true}; n.Pointee = $2; n.Children = []*Node{$6}; $$ = []*Node{n} }
	| STATIC type_specifier ID ';'
		{ n := ctNode(KindVarDecl, $2, $3); n.IsStatic = true; maybeSetTypeofExpr(n, $2, yylex); $$ = []*Node{n} }
	| STATIC type_specifier ID '=' expression ';'
		{ n := ctNode(KindVarDecl, $2, $3); n.IsStatic = true; n.Children = []*Node{$5}; maybeSetTypeofExpr(n, $2, yylex); $$ = []*Node{n} }
	| STATIC type_specifier ID '[' NUM ']' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5, ElemType: $2.Kind, IsStatic: true}} }
	/* ── Brace-initializer for struct/scalar local ── */
	| type_specifier ID '=' '{' init_list '}' ';'
		{ n := ctNode(KindVarDecl, $1, $2)
		  n.Children = []*Node{{Kind: KindInitList, Children: $5}}
		  maybeSetTypeofExpr(n, $1, yylex)
		  $$ = []*Node{n} }
	| type_specifier ID '=' '{' init_list ',' '}' ';'
		{ n := ctNode(KindVarDecl, $1, $2)
		  n.Children = []*Node{{Kind: KindInitList, Children: $5}}
		  maybeSetTypeofExpr(n, $1, yylex)
		  $$ = []*Node{n} }
	/* ── Brace-initializer for array ── */
	| type_specifier ID '[' NUM ']' '=' '{' init_list '}' ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $2,
		             Val: $4, ElemType: $1.Kind,
		             Children: []*Node{{Kind: KindInitList, Children: $8}}}
		  $$ = []*Node{n} }
	| type_specifier ID '[' NUM ']' '=' '{' init_list ',' '}' ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $2,
		             Val: $4, ElemType: $1.Kind,
		             Children: []*Node{{Kind: KindInitList, Children: $8}}}
		  $$ = []*Node{n} }
	/* ── Brace-initializer for struct/union variable ── */
	| STRUCT ID ID '=' '{' init_list '}' ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2,
		             Children: []*Node{{Kind: KindInitList, Children: $6}}}
		  $$ = []*Node{n} }
	| STRUCT ID ID '=' '{' init_list ',' '}' ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2,
		             Children: []*Node{{Kind: KindInitList, Children: $6}}}
		  $$ = []*Node{n} }
	| UNION ID ID '=' '{' init_list '}' ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2,
		             Children: []*Node{{Kind: KindInitList, Children: $6}}}
		  $$ = []*Node{n} }
	| UNION ID ID '=' '{' init_list ',' '}' ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2,
		             Children: []*Node{{Kind: KindInitList, Children: $6}}}
		  $$ = []*Node{n} }
	/* ── Inline anonymous struct/union local variable ── */
	| UNION '{' field_list '}' ID ';'
		{ tag := yylex.(*lexer).nextAnon()
		  sd := &Node{Kind: KindStructDef, Name: tag, Children: $3, IsUnion: true}
		  vd := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $5, StructTag: tag}
		  $$ = []*Node{sd, vd} }
	| UNION '{' field_list '}' ID '=' '{' init_list '}' ';'
		{ tag := yylex.(*lexer).nextAnon()
		  sd := &Node{Kind: KindStructDef, Name: tag, Children: $3, IsUnion: true}
		  vd := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $5, StructTag: tag,
		              Children: []*Node{{Kind: KindInitList, Children: $8}}}
		  $$ = []*Node{sd, vd} }
	| UNION '{' field_list '}' ID '=' '{' init_list ',' '}' ';'
		{ tag := yylex.(*lexer).nextAnon()
		  sd := &Node{Kind: KindStructDef, Name: tag, Children: $3, IsUnion: true}
		  vd := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $5, StructTag: tag,
		              Children: []*Node{{Kind: KindInitList, Children: $8}}}
		  $$ = []*Node{sd, vd} }
	| STRUCT '{' field_list '}' ID ';'
		{ tag := yylex.(*lexer).nextAnon()
		  sd := &Node{Kind: KindStructDef, Name: tag, Children: $3}
		  vd := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $5, StructTag: tag}
		  $$ = []*Node{sd, vd} }
	| STRUCT '{' field_list '}' ID '=' '{' init_list '}' ';'
		{ tag := yylex.(*lexer).nextAnon()
		  sd := &Node{Kind: KindStructDef, Name: tag, Children: $3}
		  vd := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $5, StructTag: tag,
		              Children: []*Node{{Kind: KindInitList, Children: $8}}}
		  $$ = []*Node{sd, vd} }
	| STRUCT '{' field_list '}' ID '=' '{' init_list ',' '}' ';'
		{ tag := yylex.(*lexer).nextAnon()
		  sd := &Node{Kind: KindStructDef, Name: tag, Children: $3}
		  vd := &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $5, StructTag: tag,
		              Children: []*Node{{Kind: KindInitList, Children: $8}}}
		  $$ = []*Node{sd, vd} }
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
	| '.' ID '=' '{' init_list '}'
		{ $$ = &Node{Kind: KindInitEntry, Op: ".", Name: $2, Children: []*Node{
		      {Kind: KindInitList, Children: $5}}} }
	| '.' ID '=' '{' init_list ',' '}'
		{ $$ = &Node{Kind: KindInitEntry, Op: ".", Name: $2, Children: []*Node{
		      {Kind: KindInitList, Children: $5}}} }
	| '[' NUM ']' '=' assign_expr
		{ $$ = &Node{Kind: KindInitEntry, Op: "[", Val: $2, Children: []*Node{$5}} }
	| '[' NUM ']' '=' '{' init_list '}'
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

const_declaration
	: CONST type_specifier ID '=' NUM ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: $2.Kind, Name: $3, Val: $5, IsConst: true}} }
	;

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
	;

fun_declaration
	: type_specifier ID '(' params ')' compound_stmt
		{
			n := ctNode(KindFunDecl, $1, $2)
			n.Children = append($4, $6)
			$$ = n
		}
	| type_specifier ID '(' param_list ',' ELLIPSIS ')' compound_stmt
		{
			n := ctNode(KindFunDecl, $1, $2)
			params := append($4, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})
			n.Children = append(params, $8)
			$$ = n
		}
	| type_specifier '*' ID '(' params ')' compound_stmt
		{
			n := &Node{Kind: KindFunDecl, Type: TypePtr, Name: $3}
			n.Pointee = $1
			n.Children = append($5, $7)
			$$ = n
		}
	| type_specifier '*' ID '(' param_list ',' ELLIPSIS ')' compound_stmt
		{
			n := &Node{Kind: KindFunDecl, Type: TypePtr, Name: $3}
			n.Pointee = $1
			params := append($5, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})
			n.Children = append(params, $9)
			$$ = n
		}
	| STRUCT ID '*' ID '(' params ')' compound_stmt
		{
			n := &Node{Kind: KindFunDecl, Type: TypePtr, Name: $4}
			n.Pointee = structCType($2)
			n.Children = append($6, $8)
			$$ = n
		}
	| STRUCT ID ID '(' params ')' compound_stmt
		{
			n := &Node{Kind: KindFunDecl, Type: TypeStruct, StructTag: $2, Name: $3}
			n.Children = append($5, $7)
			$$ = n
		}
	| STRUCT ID ID '(' param_list ',' ELLIPSIS ')' compound_stmt
		{
			n := &Node{Kind: KindFunDecl, Type: TypeStruct, StructTag: $2, Name: $3}
			params := append($5, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})
			n.Children = append(params, $9)
			$$ = n
		}
	| STATIC type_specifier ID '(' params ')' compound_stmt
		{
			n := ctNode(KindFunDecl, $2, $3)
			n.IsStatic = true
			n.Children = append($5, $7)
			$$ = n
		}
	| STATIC type_specifier ID '(' param_list ',' ELLIPSIS ')' compound_stmt
		{
			n := ctNode(KindFunDecl, $2, $3)
			n.IsStatic = true
			params := append($5, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})
			n.Children = append(params, $9)
			$$ = n
		}
	| STATIC type_specifier '*' ID '(' params ')' compound_stmt
		{
			n := &Node{Kind: KindFunDecl, Type: TypePtr, Name: $4}
			n.IsStatic = true
			n.Pointee = $2
			n.Children = append($6, $8)
			$$ = n
		}
	| STATIC type_specifier '*' ID '(' param_list ',' ELLIPSIS ')' compound_stmt
		{
			n := &Node{Kind: KindFunDecl, Type: TypePtr, Name: $4}
			n.IsStatic = true
			n.Pointee = $2
			params := append($6, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})
			n.Children = append(params, $10)
			$$ = n
		}
	;

params
	: param_list { $$ = $1 }
	| VOID       { $$ = nil }
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
	| type_specifier ID '[' NUM ']' '[' NUM ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $2, ElemType: $1.Kind, Dim2: $7} }
	| type_specifier ID '[' ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $2, ElemType: $1.Kind} }
	| type_specifier '*' ID '[' ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $3, ElemType: TypePtr, ElemPointee: $1} }
	| type_specifier '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $3}; n.Pointee = $1; $$ = n }
	| type_specifier '*' '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $4}; n.Pointee = ptrCType($1); $$ = n }
	| STRUCT ID ID
		{ $$ = &Node{Kind: KindParam, Type: TypeStruct, Name: $3, StructTag: $2} }
	| STRUCT ID '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); $$ = n }
	| STRUCT ID '*' '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $5}; n.Pointee = ptrCType(structCType($2)); $$ = n }
	| CONST type_specifier '*' ID
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: $4, IsConstTarget: true}; n.Pointee = $2; $$ = n }
	/* Nameless pointer parameters (in extern / forward declarations) — only pointer forms
	   to avoid a R/R conflict with the "params: VOID" production for func(void). */
	| type_specifier '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = $1; $$ = n }
	| type_specifier '*' '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = ptrCType($1); $$ = n }
	| CONST type_specifier '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: "", IsConstTarget: true}; n.Pointee = $2; $$ = n }
	| STRUCT ID '*'
		{ n := &Node{Kind: KindParam, Type: TypePtr, Name: ""}; n.Pointee = structCType($2); $$ = n }
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
	| SIGNED         { $$ = &Node{Kind: KindParam, Type: TypeInt,      Name: ""} }
	| SIGNED INT     { $$ = &Node{Kind: KindParam, Type: TypeInt,      Name: ""} }
	| SIGNED CHAR    { $$ = &Node{Kind: KindParam, Type: TypeChar,     Name: ""} }
	| SIGNED LONG    { $$ = &Node{Kind: KindParam, Type: TypeLong,     Name: ""} }
	| INT128         { $$ = &Node{Kind: KindParam, Type: TypeInt128,   Name: ""} }
	| UNSIGNED INT128 { $$ = &Node{Kind: KindParam, Type: TypeUint128, Name: ""} }
	| TYPENAME { ct := yylex.(*lexer).lookupTypedefCType($1); $$ = &Node{Kind: KindParam, Type: ct.Kind, Name: "", StructTag: ct.Tag} }
	;

compound_stmt
	: '{' local_declarations statement_list '}'
		{ $$ = &Node{Kind: KindCompound, Children: append($2, $3...)} }
	;

local_declarations
	: local_declarations var_declaration
		{ $$ = append($1, $2...) }
	| local_declarations const_declaration
		{ $$ = append($1, $2...) }
	| /* empty */
		{ $$ = nil }
	;

id_list
	: id_list ',' ID
		{ $$ = append($1, &Node{Kind: KindVar, Name: $3}) }
	| ID ',' ID
		{ $$ = []*Node{{Kind: KindVar, Name: $1}, {Kind: KindVar, Name: $3}} }
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
	| return_stmt     { $$ = $1 }
	| break_stmt      { $$ = $1 }
	| continue_stmt   { $$ = $1 }
	| goto_stmt       { $$ = $1 }
	| ID ':' statement
		{ $$ = &Node{Kind: KindLabel, Name: $1, Children: []*Node{$3}} }
	;

expression_stmt
	: expression ';' { $$ = &Node{Kind: KindExprStmt, Children: []*Node{$1}} }
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
	;

for_stmt
	: FOR '(' opt_expression ';' opt_expression ';' opt_expression ')' statement
		{ $$ = &Node{Kind: KindFor, Children: []*Node{$3, $5, $7, $9}} }
	;

do_while_stmt
	: DO statement WHILE '(' expression ')' ';'
		{ $$ = &Node{Kind: KindDoWhile, Children: []*Node{$2, $5}} }
	;

goto_stmt
	: GOTO ID ';'
		{ $$ = &Node{Kind: KindGoto, Name: $2} }
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
	;

expression
	: postfix_expr '=' expression
		{ $$ = &Node{Kind: KindAssign, Children: []*Node{$1, $3}} }
	| '(' expression ')' '=' expression
		{ $$ = &Node{Kind: KindAssign, Children: []*Node{$2, $5}} }
	| '*' factor '=' expression
		{ lhs := &Node{Kind: KindDeref, Children: []*Node{$2}}; $$ = &Node{Kind: KindAssign, Children: []*Node{lhs, $4}} }
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
	| '*' factor          { $$ = &Node{Kind: KindDeref, Children: []*Node{$2}} }
	| SIZEOF '(' type_specifier ')'
		{ $$ = &Node{Kind: KindSizeof, Type: $3.Kind, StructTag: $3.Tag} }
	| SIZEOF '(' STRUCT ID ')'
		{ $$ = &Node{Kind: KindSizeof, StructTag: $4} }
	| SIZEOF '(' UNION ID ')'
		{ $$ = &Node{Kind: KindSizeof, StructTag: $4} }
	| SIZEOF '(' expression ')'
		{ $$ = &Node{Kind: KindSizeof, Children: []*Node{$3}} }
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
	| '(' type_specifier '*' '*' ')' factor
		{ n := &Node{Kind: KindCast, Type: TypePtr}; n.Pointee = ptrCType($2); n.Children = []*Node{$6}; $$ = n }
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
	| '(' type_specifier '[' NUM ']' ')' '{' init_list '}'
		{ n := &Node{Kind: KindCompoundLit, Type: TypeIntArray, ElemType: $2.Kind, Val: $4}
		  n.Children = []*Node{{Kind: KindInitList, Children: $8}}
		  $$ = n }
	| '(' type_specifier '[' NUM ']' ')' '{' init_list ',' '}'
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
	| '(' '{' local_declarations statement_list '}' ')'
		{ $$ = &Node{Kind: KindStmtExpr, Children: append($3, $4...)} }
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
	;

union_declaration
	: UNION ID '{' field_list '}' ';'
		{ $$ = []*Node{{Kind: KindStructDef, Name: $2, Children: $4, IsUnion: true}} }
	;

enum_declaration
	: ENUM '{' enum_list '}' ';'
		{ $$ = $3 }
	;

enum_list
	: enum_list ',' enum_member
		{ $$ = append($1, $3) }
	| enum_member
		{ $$ = []*Node{$1} }
	;

enum_member
	: ID
		{
			n := &Node{Kind: KindVarDecl, Type: TypeInt, Name: $1,
				Val: yylex.(*lexer).enumAutoVal, IsConst: true}
			yylex.(*lexer).enumAutoVal++
			$$ = n
		}
	| ID '=' NUM
		{
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
	| TYPEDEF STRUCT ID ID ';'
		{
			yylex.(*lexer).registerTypedef($4, structCType($3))
			$$ = nil
		}
	| TYPEDEF STRUCT ID '*' ID ';'
		{
			yylex.(*lexer).registerTypedef($5, ptrCType(structCType($3)))
			$$ = nil
		}
	| TYPEDEF type_specifier '(' '*' ID ')' '(' fp_param_types ')' ';'
		{
			yylex.(*lexer).registerTypedef($5, leafCType(TypeFuncPtr))
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
	| TYPEDEF UNION '{' field_list '}' ID ';'
		{
			tag := yylex.(*lexer).nextAnon()
			sd := &Node{Kind: KindStructDef, Name: tag, Children: $4, IsUnion: true}
			yylex.(*lexer).registerTypedef($6, structCType(tag))
			$$ = []*Node{sd}
		}
	/* typedef of named struct/union defining the struct inline: typedef struct Tag { ... } Name; */
	| TYPEDEF STRUCT ID '{' field_list '}' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5}
			yylex.(*lexer).registerTypedef($7, structCType($3))
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION ID '{' field_list '}' ID ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5, IsUnion: true}
			yylex.(*lexer).registerTypedef($7, structCType($3))
			$$ = []*Node{sd}
		}
	| TYPEDEF STRUCT ID '{' field_list '}' ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5}
			$$ = []*Node{sd}
		}
	| TYPEDEF UNION ID '{' field_list '}' ';'
		{
			sd := &Node{Kind: KindStructDef, Name: $3, Children: $5, IsUnion: true}
			$$ = []*Node{sd}
		}
	;

fp_param_types
	: fp_param_type_list { $$ = $1 }
	| /* empty */        { $$ = nil }
	;

fp_param_type_list
	: fp_param_type_list ',' fp_param_type
		{ $$ = append($1, $3) }
	| fp_param_type
		{ $$ = []*Node{$1} }
	;

fp_param_type
	: type_specifier           { $$ = ctNode(KindParam, $1, "") }
	| type_specifier '*'       { n := &Node{Kind: KindParam, Type: TypePtr}; n.Pointee = $1; $$ = n }
	| type_specifier ID        { $$ = ctNode(KindParam, $1, $2) }
	| type_specifier '*' ID    { n := &Node{Kind: KindParam, Type: TypePtr, Name: $3}; n.Pointee = $1; $$ = n }
	;

field_list
	: field_list field
		{ $$ = append($1, $2...) }
	| field
		{ $$ = $1 }
	;

field
	: type_specifier ID ';'
		{ $$ = []*Node{ctNode(KindVarDecl, $1, $2)} }
	| type_specifier id_list ';'
		{ $$ = makeMultiDecl($1, $2) }
	| type_specifier ID ':' NUM ';'
		{ n := ctNode(KindVarDecl, $1, $2); n.BitWidth = $4; $$ = []*Node{n} }
	| type_specifier ID '[' const_int_expr ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, Val: $4, ElemType: $1.Kind}} }
	| type_specifier ID '[' ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, Val: -1, ElemType: $1.Kind}} }
	| type_specifier '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $3}; n.Pointee = $1; $$ = []*Node{n} }
	| type_specifier '*' ID '[' const_int_expr ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5, ElemType: TypePtr, ElemPointee: $1}} }
	| STRUCT ID '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); $$ = []*Node{n} }
	| STRUCT ID ID ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2}} }
	| STRUCT ID ID '[' const_int_expr ']' ';'
		{ $$ = []*Node{&Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5, ElemType: TypeStruct, ElemPointee: structCType($2)}} }
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
	;

const_int_expr
	: const_int_add                           { $$ = $1 }
	| const_int_expr LSHIFT const_int_add     { $$ = $1 << $3 }
	| const_int_expr RSHIFT const_int_add     { $$ = $1 >> $3 }
	;

const_int_add
	: const_int_mul                           { $$ = $1 }
	| const_int_add '+' const_int_mul         { $$ = $1 + $3 }
	| const_int_add '-' const_int_mul         { $$ = $1 - $3 }
	;

const_int_mul
	: const_int_primary                       { $$ = $1 }
	| const_int_mul '*' const_int_primary     { $$ = $1 * $3 }
	;

const_int_primary
	: NUM                                     { $$ = $1 }
	| '(' const_int_expr ')'                  { $$ = $2 }
	;

%%
