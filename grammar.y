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
%token LONG UNSIGNED SHORT FLOAT DOUBLE STRUCT SIZEOF ENUM UNION TYPEDEF STATIC VA_ARG
%token <sval> TYPENAME

// Multi-character operators
%token LE GE EQ NE LSHIFT RSHIFT
%token INC DEC PLUSEQ MINUSEQ STAREQ DIVEQ MODEQ
%token ANDEQ OREQ XOREQ SHLEQ SHREQ
%token ARROW ELLIPSIS
%token ANDAND OROR

// Types for non-terminals
%type <node>  program fun_declaration
%type <nodes> declaration var_declaration const_declaration extern_declaration declaration_list params param_list id_list
%type <nodes> struct_declaration field_list union_declaration enum_declaration enum_list typedef_declaration
%type <nodes> fp_param_types fp_param_type_list
%type <node>  field param compound_stmt postfix_expr enum_member fp_param_type
%type <nodes> local_declarations statement_list
%type <node>  statement expression_stmt selection_stmt iteration_stmt for_stmt do_while_stmt return_stmt break_stmt continue_stmt goto_stmt
%type <node>  opt_expression
%type <node>  expression var simple_expression logical_expression comparison_expression bitwise_expression additive_expression term factor call
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
	: EXTERN type_specifier ID ';'
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
	;

var_declaration
	: type_specifier ID ';'
		{ $$ = []*Node{ctNode(KindVarDecl, $1, $2)} }
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
		{ n := ctNode(KindVarDecl, $1, $2); n.Children = []*Node{$4}; $$ = []*Node{n} }
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
		{ n := ctNode(KindVarDecl, $2, $3); n.IsStatic = true; $$ = []*Node{n} }
	| STATIC type_specifier ID '=' expression ';'
		{ n := ctNode(KindVarDecl, $2, $3); n.IsStatic = true; n.Children = []*Node{$5}; $$ = []*Node{n} }
	| STATIC type_specifier ID '[' NUM ']' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5, ElemType: $2.Kind, IsStatic: true}} }
	;

const_declaration
	: CONST type_specifier ID '=' NUM ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: $2.Kind, Name: $3, Val: $5, IsConst: true}} }
	;

type_specifier
	: INT                { $$ = leafCType(TypeInt) }
	| VOID               { $$ = leafCType(TypeVoid) }
	| CHAR               { $$ = leafCType(TypeChar) }
	| LONG               { $$ = leafCType(TypeInt) }
	| LONG LONG          { $$ = leafCType(TypeInt) }
	| SHORT              { $$ = leafCType(TypeShort) }
	| SHORT INT          { $$ = leafCType(TypeShort) }
	| UNSIGNED           { $$ = leafCType(TypeUnsignedInt) }
	| UNSIGNED INT       { $$ = leafCType(TypeUnsignedInt) }
	| UNSIGNED LONG      { $$ = leafCType(TypeUnsignedInt) }
	| UNSIGNED LONG LONG { $$ = leafCType(TypeUnsignedInt) }
	| UNSIGNED CHAR      { $$ = leafCType(TypeUnsignedChar) }
	| UNSIGNED SHORT     { $$ = leafCType(TypeUnsignedShort) }
	| UNSIGNED SHORT INT { $$ = leafCType(TypeUnsignedShort) }
	| FLOAT              { $$ = leafCType(TypeFloat) }
	| DOUBLE             { $$ = leafCType(TypeDouble) }
	| TYPENAME           { $$ = yylex.(*lexer).lookupTypedefCType($1) }
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
	: logical_expression
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
		{ $$ = append($1, $2) }
	| field
		{ $$ = []*Node{$1} }
	;

field
	: type_specifier ID ';'
		{ $$ = ctNode(KindVarDecl, $1, $2) }
	| type_specifier ID ':' NUM ';'
		{ n := ctNode(KindVarDecl, $1, $2); n.BitWidth = $4; $$ = n }
	| type_specifier ID '[' ']' ';'
		{ $$ = &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, Val: -1, ElemType: $1.Kind} }
	| type_specifier '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $3}; n.Pointee = $1; $$ = n }
	| type_specifier '*' ID '[' NUM ']' ';'
		{ $$ = &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, Val: $5, ElemType: TypePtr, ElemPointee: $1} }
	| STRUCT ID '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); $$ = n }
	| STRUCT ID ID ';'
		{ $$ = &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2} }
	| UNION ID ID ';'
		{ $$ = &Node{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2} }
	| UNION ID '*' ID ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypePtr, Name: $4}; n.Pointee = structCType($2); $$ = n }
	;

%%
