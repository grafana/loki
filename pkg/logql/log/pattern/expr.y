%{

package pattern

%}

%union{
  Expr                 []node
  Node                 node

  literal              rune
  str                  string
  token                int
}

%start root

%type <Expr>             expr
%type <Node>             node

%token <str>              IDENTIFIER
%token <literal>          LITERAL
%token <token>            LESS_THAN MORE_THAN UNDERSCORE

%%

root:
    expr { exprlex.(*lexer).expr = $1 };

expr:
    node { $$ = []node{$1} }
    | expr node { $$ = append($1, $2) }
    ;

node:
     IDENTIFIER  { $$ = capture($1) }
    | LITERAL  { $$ = literal($1) }
    ;
%%
