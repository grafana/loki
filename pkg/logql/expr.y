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
  MetricExpr              SampleExpr
  VectorOp                string
  BinOpExpr               SampleExpr
  binOp                   string
  str                     string
  duration                time.Duration
  LiteralExpr             *literalExpr
  BinOpModifier           BinOpOptions
}

%start root

%type <Expr>                  expr
%type <Filter>                filter
%type <Grouping>              grouping
%type <Labels>                labels
%type <LogExpr>               logExpr
%type <MetricExpr>            metricExpr
%type <LogRangeExpr>          logRangeExpr
%type <Matcher>               matcher
%type <Matchers>              matchers
%type <RangeAggregationExpr>  rangeAggregationExpr
%type <RangeOp>               rangeOp
%type <Selector>              selector
%type <VectorAggregationExpr> vectorAggregationExpr
%type <VectorOp>              vectorOp
%type <BinOpExpr>             binOpExpr
%type <LiteralExpr>           literalExpr
%type <BinOpModifier>         binOpModifier

%token <str>      IDENTIFIER STRING NUMBER
%token <duration> DURATION
%token <val>      MATCHERS LABELS EQ RE NRE OPEN_BRACE CLOSE_BRACE OPEN_BRACKET CLOSE_BRACKET COMMA DOT PIPE_MATCH PIPE_EXACT
                  OPEN_PARENTHESIS CLOSE_PARENTHESIS BY WITHOUT COUNT_OVER_TIME RATE SUM AVG MAX MIN COUNT STDDEV STDVAR BOTTOMK TOPK
                  BYTES_OVER_TIME BYTES_RATE BOOL

// Operators are listed with increasing precedence.
%left <binOp> OR
%left <binOp> AND UNLESS
%left <binOp> CMP_EQ NEQ LT LTE GT GTE
%left <binOp> ADD SUB
%left <binOp> MUL DIV MOD
%right <binOp> POW

%%

root: expr { exprlex.(*lexer).expr = $1 };

expr:
      logExpr                                      { $$ = $1 }
    | metricExpr                                   { $$ = $1 }
    ;

metricExpr:
      rangeAggregationExpr                          { $$ = $1 }
    | vectorAggregationExpr                         { $$ = $1 }
    | binOpExpr                                     { $$ = $1 }
    | literalExpr                                   { $$ = $1 }
    | OPEN_PARENTHESIS metricExpr CLOSE_PARENTHESIS { $$ = $2 }
    ;

logExpr:
      selector                                    { $$ = newMatcherExpr($1)}
    | logExpr filter STRING                       { $$ = NewFilterExpr( $1, $2, $3 ) }
    | OPEN_PARENTHESIS logExpr CLOSE_PARENTHESIS  { $$ = $2 }
    | logExpr filter error
    | logExpr error
    ;

logRangeExpr:
      logExpr DURATION { $$ = newLogRange($1, $2) } // <selector> <filters> <range>
    | logRangeExpr filter STRING                       { $$ = addFilterToLogRangeExpr( $1, $2, $3 ) }
    | OPEN_PARENTHESIS logRangeExpr CLOSE_PARENTHESIS  { $$ = $2 }
    | logRangeExpr filter error
    | logRangeExpr error
    ;

rangeAggregationExpr: rangeOp OPEN_PARENTHESIS logRangeExpr CLOSE_PARENTHESIS { $$ = newRangeAggregationExpr($3,$1) };

vectorAggregationExpr:
    // Aggregations with 1 argument.
      vectorOp OPEN_PARENTHESIS metricExpr CLOSE_PARENTHESIS                               { $$ = mustNewVectorAggregationExpr($3, $1, nil, nil) }
    | vectorOp grouping OPEN_PARENTHESIS metricExpr CLOSE_PARENTHESIS                      { $$ = mustNewVectorAggregationExpr($4, $1, $2, nil,) }
    | vectorOp OPEN_PARENTHESIS metricExpr CLOSE_PARENTHESIS grouping                      { $$ = mustNewVectorAggregationExpr($3, $1, $5, nil) }
    // Aggregations with 2 arguments.
    | vectorOp OPEN_PARENTHESIS NUMBER COMMA metricExpr CLOSE_PARENTHESIS                 { $$ = mustNewVectorAggregationExpr($5, $1, nil, &$3) }
    | vectorOp OPEN_PARENTHESIS NUMBER COMMA metricExpr CLOSE_PARENTHESIS grouping        { $$ = mustNewVectorAggregationExpr($5, $1, $7, &$3) }
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

// TODO(owen-d): add (on,ignoring) clauses to binOpExpr
// Operator precedence only works if each of these is listed separately.
binOpExpr:
         expr OR binOpModifier expr          { $$ = mustNewBinOpExpr("or", $3, $1, $4) }
         | expr AND binOpModifier expr       { $$ = mustNewBinOpExpr("and", $3, $1, $4) }
         | expr UNLESS binOpModifier expr    { $$ = mustNewBinOpExpr("unless", $3, $1, $4) }
         | expr ADD binOpModifier expr       { $$ = mustNewBinOpExpr("+", $3, $1, $4) }
         | expr SUB binOpModifier expr       { $$ = mustNewBinOpExpr("-", $3, $1, $4) }
         | expr MUL binOpModifier expr       { $$ = mustNewBinOpExpr("*", $3, $1, $4) }
         | expr DIV binOpModifier expr       { $$ = mustNewBinOpExpr("/", $3, $1, $4) }
         | expr MOD binOpModifier expr       { $$ = mustNewBinOpExpr("%", $3, $1, $4) }
         | expr POW binOpModifier expr       { $$ = mustNewBinOpExpr("^", $3, $1, $4) }
         | expr CMP_EQ binOpModifier expr    { $$ = mustNewBinOpExpr("==", $3, $1, $4) }
         | expr NEQ binOpModifier expr       { $$ = mustNewBinOpExpr("!=", $3, $1, $4) }
         | expr GT binOpModifier expr        { $$ = mustNewBinOpExpr(">", $3, $1, $4) }
         | expr GTE binOpModifier expr       { $$ = mustNewBinOpExpr(">=", $3, $1, $4) }
         | expr LT binOpModifier expr        { $$ = mustNewBinOpExpr("<", $3, $1, $4) }
         | expr LTE binOpModifier expr       { $$ = mustNewBinOpExpr("<=", $3, $1, $4) }
         ;

binOpModifier:
           { $$ = BinOpOptions{} }
           | BOOL { $$ = BinOpOptions{ ReturnBool: true } }
           ;

literalExpr:
           NUMBER         { $$ = mustNewLiteralExpr( $1, false ) }
           | ADD NUMBER   { $$ = mustNewLiteralExpr( $2, false ) }
           | SUB NUMBER   { $$ = mustNewLiteralExpr( $2, true ) }
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
      COUNT_OVER_TIME { $$ = OpRangeTypeCount }
    | RATE            { $$ = OpRangeTypeRate }
    | BYTES_OVER_TIME { $$ = OpRangeTypeBytes }
    | BYTES_RATE      { $$ = OpRangeTypeBytesRate }
    ;


labels:
      IDENTIFIER                 { $$ = []string{ $1 } }
    | labels COMMA IDENTIFIER    { $$ = append($1, $3) }
    ;

grouping:
      BY OPEN_PARENTHESIS labels CLOSE_PARENTHESIS        { $$ = &grouping{ without: false , groups: $3 } }
    | WITHOUT OPEN_PARENTHESIS labels CLOSE_PARENTHESIS   { $$ = &grouping{ without: true , groups: $3 } }
    ;
%%
