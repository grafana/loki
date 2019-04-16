%{
package logql

import (
  "github.com/prometheus/prometheus/pkg/labels"
)
%}

%union{
  Expr         Expr
  Matchers     []*labels.Matcher
  Matcher      *labels.Matcher
  str          string
  int          int64
  Identifier   string
}

%start root

%type  <Expr>         expr
%type  <Matchers>     matchers
%type  <Matcher>      matcher
%type  <Identifier>   identifier

%token <str>  IDENTIFIER STRING
%token <val>  MATCHERS LABELS EQ NEQ RE NRE OPEN_BRACE CLOSE_BRACE COMMA DOT PIPE_MATCH PIPE_EXACT

%%

root: expr { exprlex.(*lexer).expr = $1 };

expr:
      OPEN_BRACE matchers CLOSE_BRACE  { $$ = &matchersExpr{ $2 } }
    | expr PIPE_MATCH STRING           { $$ = &matchExpr{ $1, labels.MatchRegexp, $3 } }
    | expr PIPE_EXACT STRING           { $$ = &matchExpr{ $1, labels.MatchEqual, $3 } }
    | expr NRE STRING                  { $$ = &matchExpr{ $1, labels.MatchNotRegexp, $3 } }
    | expr NEQ STRING                  { $$ = &matchExpr{ $1, labels.MatchNotEqual, $3 } }
    ;

matchers:
      matcher                          { $$ = []*labels.Matcher{ $1 } }
    | matchers COMMA matcher           { $$ = append($1, $3) }
    ;

matcher:
      identifier EQ STRING             { $$ = mustNewMatcher(labels.MatchEqual, $1, $3) }
    | identifier NEQ STRING            { $$ = mustNewMatcher(labels.MatchNotEqual, $1, $3) }
    | identifier RE STRING             { $$ = mustNewMatcher(labels.MatchRegexp, $1, $3) }
    | identifier NRE STRING            { $$ = mustNewMatcher(labels.MatchNotRegexp, $1, $3) }
    ;

identifier:
      IDENTIFIER                       { $$ = $1 }
    | identifier DOT IDENTIFIER        { $$ = $1 + "." + $3 }
    ;
%%