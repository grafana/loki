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
}

%start root

%type  <Expr>         expr
%type  <Matchers>     matchers
%type  <Matcher>      matcher

%token <str>  IDENTIFIER STRING
%token <val>  MATCHERS LABELS EQ NEQ RE NRE OPEN_BRACE CLOSE_BRACE COMMA DOT PIPE_MATCH PIPE_EXACT

%%

root: expr { exprlex.(*lexer).expr = $1 };

expr:
      OPEN_BRACE matchers CLOSE_BRACE  { $$ = &matchersExpr{ matchers: $2 } }
    | expr PIPE_MATCH STRING           { $$ = NewFilterExpr( $1, labels.MatchRegexp, $3 ) }
    | expr PIPE_EXACT STRING           { $$ = NewFilterExpr( $1, labels.MatchEqual, $3 ) }
    | expr NRE STRING                  { $$ = NewFilterExpr( $1, labels.MatchNotRegexp, $3 ) }
    | expr NEQ STRING                  { $$ = NewFilterExpr( $1, labels.MatchNotEqual, $3 ) }
    | expr PIPE_MATCH                 { exprlex.(*lexer).Error("unexpected end of query, expected string") }
    | expr STRING                     { exprlex.(*lexer).Error("unexpected string, expected pipe") }
    ;

matchers:
      matcher                          { $$ = []*labels.Matcher{ $1 } }
    | matchers COMMA matcher           { $$ = append($1, $3) }
    ;

matcher:
      IDENTIFIER EQ STRING             { $$ = mustNewMatcher(labels.MatchEqual, $1, $3) }
    | IDENTIFIER NEQ STRING            { $$ = mustNewMatcher(labels.MatchNotEqual, $1, $3) }
    | IDENTIFIER RE STRING             { $$ = mustNewMatcher(labels.MatchRegexp, $1, $3) }
    | IDENTIFIER NRE STRING            { $$ = mustNewMatcher(labels.MatchNotRegexp, $1, $3) }
    ;
%%