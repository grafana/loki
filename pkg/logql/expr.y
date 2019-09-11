%{
package logql

import (
  "time"
  "github.com/prometheus/prometheus/pkg/labels"
)
%}

%union{
  Expr                    Expr
  Filter                  labels.MatchType
  Grouping                *grouping
  Labels                  []string
  LogExpr                 LogSelectorExpr
  LogRangeExpr            *logRange
  Matcher                 *labels.Matcher
  Matchers                []*labels.Matcher
  RangeAggregationExpr    SampleExpr
  RangeOp                 string
  Selector                []*labels.Matcher
  VectorAggregationExpr   SampleExpr
  VectorOp                string
  str                     string
  duration                time.Duration
  int                     int64
}

%start root

%type <Expr>                  expr
%type <Filter>                filter
%type <Grouping>              grouping
%type <Labels>                labels
%type <LogExpr>               logExpr
%type <LogRangeExpr>          logRangeExpr
%type <Matcher>               matcher
%type <Matchers>              matchers
%type <RangeAggregationExpr>  rangeAggregationExpr
%type <RangeOp>               rangeOp
%type <Selector>              selector
%type <VectorAggregationExpr> vectorAggregationExpr
%type <VectorOp>              vectorOp

%token <str>      IDENTIFIER STRING
%token <duration> DURATION
%token <val>      MATCHERS LABELS EQ NEQ RE NRE OPEN_BRACE CLOSE_BRACE OPEN_BRACKET CLOSE_BRACKET COMMA DOT PIPE_MATCH PIPE_EXACT
                  OPEN_PARENTHESIS CLOSE_PARENTHESIS BY WITHOUT COUNT_OVER_TIME RATE SUM AVG MAX MIN COUNT STDDEV STDVAR BOTTOMK TOPK

%%

root: expr { exprlex.(*lexer).expr = $1 };

expr:
      logExpr                    { $$ = $1 }
    | rangeAggregationExpr       { $$ = $1 }
    | vectorAggregationExpr      { $$ = $1 }
    ;

logExpr:
      selector                                    { $$ = newMatcherExpr($1)}
    | logExpr filter STRING                       { $$ = NewFilterExpr( $1, $2, $3 ) }
    | OPEN_PARENTHESIS logExpr CLOSE_PARENTHESIS  { $$ = $2}
    | logExpr filter error
    | logExpr error
    ;


logRangeExpr: logExpr DURATION { $$ = mustNewRange($1, $2) };


rangeAggregationExpr: rangeOp OPEN_PARENTHESIS logRangeExpr CLOSE_PARENTHESIS { $$ = newRangeAggregationExpr($3,$1) };

vectorAggregationExpr:
    // Aggregations with 1 argument.
      vectorOp OPEN_PARENTHESIS rangeAggregationExpr CLOSE_PARENTHESIS                               { $$ = mustNewVectorAggregationExpr($3, $1, nil, nil) }
    | vectorOp OPEN_PARENTHESIS vectorAggregationExpr CLOSE_PARENTHESIS                              { $$ = mustNewVectorAggregationExpr($3, $1, nil, nil) }
    | vectorOp grouping OPEN_PARENTHESIS rangeAggregationExpr CLOSE_PARENTHESIS                      { $$ = mustNewVectorAggregationExpr($4, $1, $2, nil,) }
    | vectorOp grouping OPEN_PARENTHESIS vectorAggregationExpr CLOSE_PARENTHESIS                     { $$ = mustNewVectorAggregationExpr($4, $1, $2, nil,) }
    | vectorOp OPEN_PARENTHESIS rangeAggregationExpr CLOSE_PARENTHESIS grouping                      { $$ = mustNewVectorAggregationExpr($3, $1, $5, nil) }
    | vectorOp OPEN_PARENTHESIS vectorAggregationExpr CLOSE_PARENTHESIS grouping                     { $$ = mustNewVectorAggregationExpr($3, $1, $5, nil) }
    // Aggregations with 2 arguments.
    | vectorOp OPEN_PARENTHESIS IDENTIFIER COMMA vectorAggregationExpr CLOSE_PARENTHESIS             { $$ = mustNewVectorAggregationExpr($5, $1, nil, &$3) }
    | vectorOp OPEN_PARENTHESIS IDENTIFIER COMMA vectorAggregationExpr CLOSE_PARENTHESIS grouping    { $$ = mustNewVectorAggregationExpr($5, $1, $7, &$3) }
    | vectorOp OPEN_PARENTHESIS IDENTIFIER COMMA rangeAggregationExpr CLOSE_PARENTHESIS              { $$ = mustNewVectorAggregationExpr($5, $1, nil, &$3) }
    | vectorOp OPEN_PARENTHESIS IDENTIFIER COMMA rangeAggregationExpr CLOSE_PARENTHESIS grouping     { $$ = mustNewVectorAggregationExpr($5, $1, $7, &$3) }
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

vectorOp:
        SUM     { $$ = OpTypeSum }
      | AVG     { $$ = OpTypeAvg }
      | COUNT   { $$ = OpTypeCount }
      | MAX     { $$ = OpTypeMax }
      | MIN     { $$ = OpTypeMin }
      | STDDEV  { $$ = OpTypeStddev }
      | STDVAR  { $$ = OpTypeStdvar }
      | BOTTOMK { $$ = OpTypeBottomK }
      | TOPK    { $$ = OpTypeTopK }
      ;

rangeOp:
      COUNT_OVER_TIME { $$ = OpTypeCountOverTime }
    | RATE            { $$ = OpTypeRate }


labels:
      IDENTIFIER                 { $$ = []string{ $1 } }
    | labels COMMA IDENTIFIER    { $$ = append($1, $3) }
    ;

grouping:
      BY OPEN_PARENTHESIS labels CLOSE_PARENTHESIS        { $$ = &grouping{ without: false , groups: $3 } }
    | WITHOUT OPEN_PARENTHESIS labels CLOSE_PARENTHESIS   { $$ = &grouping{ without: true , groups: $3 } }
    ;
%%
