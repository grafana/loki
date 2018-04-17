%{
package parser

import (
  "github.com/prometheus/prometheus/pkg/labels"
)
%}

%union{
  MatchersExpr []*labels.Matcher
  Matchers     []*labels.Matcher
  Matcher      *labels.Matcher
  LabelsExpr   labels.Labels
  Labels       labels.Labels
  Label        labels.Label
  str          string
  int          int64
  Identifier   string
}

%start expr

%type  <Matchers>     matchers
%type  <Matcher>      matcher
%type  <Labels>       labels
%type  <Label>        label
%type  <Identifier>   identifier

%token <str>  IDENTIFIER STRING
%token <val>  MATCHERS LABELS EQ NEQ RE NRE OPEN_BRACE CLOSE_BRACE COMMA DOT

%%

expr:
      MATCHERS OPEN_BRACE matchers CLOSE_BRACE  { labelslex.(*lexer).matcher = $3 };
    | LABELS OPEN_BRACE labels CLOSE_BRACE      { labelslex.(*lexer).labels = $3 }

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

labels:
      label                            { $$ = labels.Labels{ $1 } }
    | labels COMMA label               { $$ = append($1, $3) }
    ;

label:
      identifier EQ STRING             { $$ = labels.Label{Name: $1, Value: $3} }
    ;

identifier:
      IDENTIFIER                       { $$ = $1 }
    | identifier DOT IDENTIFIER        { $$ = $1 + "." + $3 }
    ;
%%
