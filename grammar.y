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
	typ   TypeKind
}

// Literals
%token <ival> NUM CHAR_LIT
%token <fval> FNUM
%token <sval> ID STRING_LIT

// Keywords
%token INT VOID IF ELSE WHILE RETURN FOR DO BREAK CONTINUE CONST CHAR EXTERN GOTO
%token LONG UNSIGNED SHORT FLOAT DOUBLE STRUCT

// Multi-character operators
%token LE GE EQ NE LSHIFT RSHIFT
%token INC DEC PLUSEQ MINUSEQ STAREQ DIVEQ MODEQ
%token ARROW ELLIPSIS

// Types for non-terminals
%type <node>  program fun_declaration
%type <nodes> declaration var_declaration const_declaration extern_declaration declaration_list params param_list id_list
%type <nodes> struct_declaration field_list
%type <node>  field param compound_stmt postfix_expr
%type <nodes> local_declarations statement_list
%type <node>  statement expression_stmt selection_stmt iteration_stmt for_stmt do_while_stmt return_stmt break_stmt continue_stmt goto_stmt
%type <node>  opt_expression
%type <node>  expression var simple_expression bitwise_expression additive_expression term factor call
%type <nodes> args arg_list
%type <typ>   type_specifier
%type <sval>  relop addop mulop bitwiseop

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
	;

extern_declaration
	: EXTERN type_specifier ID ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: $2, Name: $3, IsExtern: true}} }
	| EXTERN type_specifier ID '[' ']' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntArray, Name: $3, IsExtern: true}} }
	| EXTERN type_specifier '*' ID ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: ptrType($2), Name: $4, IsExtern: true}} }
	| EXTERN STRUCT ID '*' ID ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntPtr, Name: $5, IsExtern: true, StructTag: $3}} }
	| EXTERN type_specifier ID '(' params ')' ';'
		{
			n := &Node{Kind: KindFunDecl, Type: $2, Name: $3, IsExtern: true}
			n.Children = $5
			$$ = []*Node{n}
		}
	| EXTERN type_specifier ID '(' param_list ',' ELLIPSIS ')' ';'
		{
			n := &Node{Kind: KindFunDecl, Type: $2, Name: $3, IsExtern: true}
			n.Children = append($5, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})
			$$ = []*Node{n}
		}
	;

var_declaration
	: type_specifier ID ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: $1, Name: $2}} }
	| type_specifier ID '[' NUM ']' ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, Val: $4}} }
	| type_specifier ID '[' ID ']' ';'
		{
			n := &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, IsVLA: true}
			n.Children = []*Node{{Kind: KindVar, Name: $4}}
			$$ = []*Node{n}
		}
	| type_specifier ID '=' expression ';'
		{ n := &Node{Kind: KindVarDecl, Type: $1, Name: $2}; n.Children = []*Node{$4}; $$ = []*Node{n} }
	| type_specifier id_list ';'
		{ $$ = makeMultiDecl($1, $2) }
	| type_specifier '*' ID ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: ptrType($1), Name: $3}} }
	| type_specifier '*' ID '=' expression ';'
		{ n := &Node{Kind: KindVarDecl, Type: ptrType($1), Name: $3}; n.Children = []*Node{$5}; $$ = []*Node{n} }
	| STRUCT ID ID ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeStruct, Name: $3, StructTag: $2}} }
	| STRUCT ID '*' ID ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: TypeIntPtr, Name: $4, StructTag: $2}} }
	| STRUCT ID '*' ID '=' expression ';'
		{ n := &Node{Kind: KindVarDecl, Type: TypeIntPtr, Name: $4, StructTag: $2}; n.Children = []*Node{$6}; $$ = []*Node{n} }
	;

const_declaration
	: CONST type_specifier ID '=' NUM ';'
		{ $$ = []*Node{{Kind: KindVarDecl, Type: $2, Name: $3, Val: $5, IsConst: true}} }
	;

type_specifier
	: INT                { $$ = TypeInt }
	| VOID               { $$ = TypeVoid }
	| CHAR               { $$ = TypeChar }
	| LONG               { $$ = TypeInt }
	| LONG LONG          { $$ = TypeInt }
	| SHORT              { $$ = TypeShort }
	| SHORT INT          { $$ = TypeShort }
	| UNSIGNED           { $$ = TypeUnsignedInt }
	| UNSIGNED INT       { $$ = TypeUnsignedInt }
	| UNSIGNED LONG      { $$ = TypeUnsignedInt }
	| UNSIGNED LONG LONG { $$ = TypeUnsignedInt }
	| UNSIGNED CHAR      { $$ = TypeUnsignedChar }
	| UNSIGNED SHORT     { $$ = TypeUnsignedShort }
	| UNSIGNED SHORT INT { $$ = TypeUnsignedShort }
	| FLOAT              { $$ = TypeFloat }
	| DOUBLE             { $$ = TypeDouble }
	;

fun_declaration
	: type_specifier ID '(' params ')' compound_stmt
		{
			n := &Node{Kind: KindFunDecl, Type: $1, Name: $2}
			n.Children = append($4, $6)
			$$ = n
		}
	| type_specifier ID '(' param_list ',' ELLIPSIS ')' compound_stmt
		{
			n := &Node{Kind: KindFunDecl, Type: $1, Name: $2}
			params := append($4, &Node{Kind: KindParam, Type: TypeVoid, Name: "..."})
			n.Children = append(params, $8)
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
		{ $$ = &Node{Kind: KindParam, Type: $1, Name: $2} }
	| type_specifier ID '[' ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $2} }
	| type_specifier '*' ID
		{ $$ = &Node{Kind: KindParam, Type: ptrType($1), Name: $3} }
	| STRUCT ID '*' ID
		{ $$ = &Node{Kind: KindParam, Type: TypeIntPtr, Name: $4, StructTag: $2} }
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
	;

expression
	: postfix_expr '=' expression
		{ $$ = &Node{Kind: KindAssign, Children: []*Node{$1, $3}} }
	| '*' factor '=' expression
		{ lhs := &Node{Kind: KindDeref, Children: []*Node{$2}}; $$ = &Node{Kind: KindAssign, Children: []*Node{lhs, $4}} }
	| postfix_expr INC
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "+", Val: 1, Children: []*Node{$1, nil}} }
	| postfix_expr DEC
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "-", Val: 1, Children: []*Node{$1, nil}} }
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
	| simple_expression
		{ $$ = $1 }
	;

var
	: ID
		{ $$ = &Node{Kind: KindVar, Name: $1} }
	| ID '[' expression ']'
		{ $$ = &Node{Kind: KindArrayVar, Name: $1, Children: []*Node{$3}} }
	;

simple_expression
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
	| '&' var             { $$ = &Node{Kind: KindAddrOf, Children: []*Node{$2}} }
	| '*' factor          { $$ = &Node{Kind: KindDeref, Children: []*Node{$2}} }
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

field_list
	: field_list field
		{ $$ = append($1, $2) }
	| field
		{ $$ = []*Node{$1} }
	;

field
	: type_specifier ID ';'
		{ $$ = &Node{Kind: KindVarDecl, Type: $1, Name: $2} }
	| type_specifier '*' ID ';'
		{ $$ = &Node{Kind: KindVarDecl, Type: ptrType($1), Name: $3} }
	| STRUCT ID '*' ID ';'
		{ $$ = &Node{Kind: KindVarDecl, Type: TypeIntPtr, Name: $4, StructTag: $2} }
	;

%%
