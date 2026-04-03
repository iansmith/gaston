// grammar.y — C-minus grammar for the gaston compiler.
// Generate the parser with:  goyacc -o parser.go grammar.y
//
// Expect 0 shift/reduce conflicts (dangling-else resolved via %prec).
// The var/assignment ambiguity is handled by shift-default (correct).
%{
package main
%}

%union {
	ival  int
	sval  string
	node  *Node
	nodes []*Node
	typ   TypeKind
}

// Literals
%token <ival> NUM
%token <sval> ID

// Keywords
%token INT VOID IF ELSE WHILE RETURN FOR DO BREAK CONTINUE

// Multi-character operators
%token LE GE EQ NE LSHIFT RSHIFT
%token INC DEC PLUSEQ MINUSEQ STAREQ DIVEQ MODEQ

// Types for non-terminals
%type <node>  program declaration var_declaration fun_declaration
%type <nodes> declaration_list params param_list
%type <node>  param compound_stmt
%type <nodes> local_declarations statement_list
%type <node>  statement expression_stmt selection_stmt iteration_stmt for_stmt do_while_stmt return_stmt break_stmt continue_stmt
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
		{ $$ = append($1, $2) }
	| declaration
		{ $$ = []*Node{$1} }
	;

declaration
	: var_declaration { $$ = $1 }
	| fun_declaration { $$ = $1 }
	;

var_declaration
	: type_specifier ID ';'
		{ $$ = &Node{Kind: KindVarDecl, Type: $1, Name: $2} }
	| type_specifier ID '[' NUM ']' ';'
		{ $$ = &Node{Kind: KindVarDecl, Type: TypeIntArray, Name: $2, Val: $4} }
	| type_specifier ID '=' expression ';'
		{ n := &Node{Kind: KindVarDecl, Type: $1, Name: $2}; n.Children = []*Node{$4}; $$ = n }
	;

type_specifier
	: INT  { $$ = TypeInt }
	| VOID { $$ = TypeVoid }
	;

fun_declaration
	: type_specifier ID '(' params ')' compound_stmt
		{
			n := &Node{Kind: KindFunDecl, Type: $1, Name: $2}
			n.Children = append($4, $6)
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
	| param
		{ $$ = []*Node{$1} }
	;

param
	: INT ID
		{ $$ = &Node{Kind: KindParam, Type: TypeInt, Name: $2} }
	| INT ID '[' ']'
		{ $$ = &Node{Kind: KindParam, Type: TypeIntArray, Name: $2} }
	;

compound_stmt
	: '{' local_declarations statement_list '}'
		{ $$ = &Node{Kind: KindCompound, Children: append($2, $3...)} }
	;

local_declarations
	: local_declarations var_declaration
		{ $$ = append($1, $2) }
	| /* empty */
		{ $$ = nil }
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

expression
	: var '=' expression
		{ $$ = &Node{Kind: KindAssign, Children: []*Node{$1, $3}} }
	| var INC
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "+", Val: 1, Children: []*Node{$1, nil}} }
	| var DEC
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "-", Val: 1, Children: []*Node{$1, nil}} }
	| var PLUSEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "+", Children: []*Node{$1, $3}} }
	| var MINUSEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "-", Children: []*Node{$1, $3}} }
	| var STAREQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "*", Children: []*Node{$1, $3}} }
	| var DIVEQ expression
		{ $$ = &Node{Kind: KindCompoundAssign, Op: "/", Children: []*Node{$1, $3}} }
	| var MODEQ expression
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
	| var                 { $$ = $1 }
	| call                { $$ = $1 }
	| NUM                 { $$ = &Node{Kind: KindNum, Val: $1} }
	| '-' factor          { $$ = &Node{Kind: KindUnary, Op: "-", Children: []*Node{$2}} }
	| '!' factor          { $$ = &Node{Kind: KindUnary, Op: "!", Children: []*Node{$2}} }
	| '~' factor          { $$ = &Node{Kind: KindUnary, Op: "~", Children: []*Node{$2}} }
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

%%
