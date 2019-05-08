%{
package logql

import (
  "github.com/prometheus/prometheus/pkg/labels"
)
%}

%union{
  Expr         Expr
  Filter       labels.MatchType
  Selector     []*labels.Matcher
  Matchers     []*labels.Matcher
  Matcher      *labels.Matcher
  str          string
  int          int64
}

%start root

%type <Expr>         expr
%type <Filter>       filter
%type <Selector>     selector
%type <Matchers>     matchers
%type <Matcher>      matcher

%token <str>  IDENTIFIER STRING
%token <val>  MATCHERS LABELS EQ NEQ RE NRE OPEN_BRACE CLOSE_BRACE COMMA DOT PIPE_MATCH PIPE_EXACT

%%

root: expr { exprlex.(*lexer).expr = $1 };

expr:
      selector                         { $$ = &matchersExpr{ matchers: $1 } }
    | expr filter STRING               { $$ = NewFilterExpr( $1, $2, $3 ) }
    | expr filter error
    | expr error
    ;

filter:
      PIPE_MATCH                       { $$ = labels.MatchRegexp }
    | PIPE_EXACT                       { $$ = labels.MatchEqual }
    | NRE                              { $$ = labels.MatchNotRegexp }
    | NEQ                              { $$ = labels.MatchNotEqual }
    ;

selector:
      OPEN_BRACE matchers CLOSE_BRACE  { $$ = $2 }
    | OPEN_BRACE matchers error        { $$ = $2 }
    | OPEN_BRACE error CLOSE_BRACE     { }
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