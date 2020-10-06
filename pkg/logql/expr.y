%{
package logql

import (
  "time"
  "github.com/prometheus/prometheus/pkg/labels"
  "github.com/grafana/loki/pkg/logql/labelfilter"
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
  ConvOp                  string
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
  LabelParser             *labelParserExpr
  LineFilters             *lineFilterExpr
  PipelineExpr            MultiPipelineExpr
  PipelineStage           PipelineExpr
  NumberFilter            labelfilter.Filterer
  DurationFilter          labelfilter.Filterer
  LabelFilter             labelfilter.Filterer
  LineFormatExpr          *lineFmtExpr
  LabelFormatExpr         *labelFmtExpr
  LabelFormat             labelFmt
  LabelsFormat            []labelFmt
  UnwrapExpr              *unwrapExpr
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
%type <ConvOp>                convOp
%type <Selector>              selector
%type <VectorAggregationExpr> vectorAggregationExpr
%type <VectorOp>              vectorOp
%type <BinOpExpr>             binOpExpr
%type <LiteralExpr>           literalExpr
%type <BinOpModifier>         binOpModifier
%type <LabelParser>           labelParser
%type <PipelineExpr>          pipelineExpr
%type <PipelineStage>         pipelineStage
%type <NumberFilter>          numberFilter
%type <DurationFilter>        durationFilter
%type <LabelFilter>           labelFilter
%type <LineFilters>           lineFilters
%type <LineFormatExpr>        lineFormatExpr
%type <LabelFormatExpr>       labelFormatExpr
%type <LabelFormat>           labelFormat
%type <LabelsFormat>          labelsFormat
%type <UnwrapExpr>            unwrapExpr

%token <str>      IDENTIFIER STRING NUMBER
%token <duration> DURATION RANGE
%token <val>      MATCHERS LABELS EQ RE NRE OPEN_BRACE CLOSE_BRACE OPEN_BRACKET CLOSE_BRACKET COMMA DOT PIPE_MATCH PIPE_EXACT
                  OPEN_PARENTHESIS CLOSE_PARENTHESIS BY WITHOUT COUNT_OVER_TIME RATE SUM AVG MAX MIN COUNT STDDEV STDVAR BOTTOMK TOPK
                  BYTES_OVER_TIME BYTES_RATE BOOL JSON REGEXP LOGFMT PIPE LINE_FMT LABEL_FMT UNWRAP AVG_OVER_TIME SUM_OVER_TIME MIN_OVER_TIME
                  MAX_OVER_TIME STDVAR_OVER_TIME STDDEV_OVER_TIME QUANTILE_OVER_TIME DURATION_CONV DURATION_SECONDS_CONV

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
    | selector pipelineExpr                       { $$ = newPipelineExpr(newMatcherExpr($1), $2)}
    | OPEN_PARENTHESIS logExpr CLOSE_PARENTHESIS  { $$ = $2 }
    ;

logRangeExpr:
      selector RANGE                                                             { $$ = newLogRange(newMatcherExpr($1), $2, nil) }
    | OPEN_PARENTHESIS selector CLOSE_PARENTHESIS RANGE                          { $$ = newLogRange(newMatcherExpr($2), $4, nil) }
    | selector RANGE unwrapExpr                                                  { $$ = newLogRange(newMatcherExpr($1), $2 , $3) }
    | OPEN_PARENTHESIS selector CLOSE_PARENTHESIS RANGE unwrapExpr               { $$ = newLogRange(newMatcherExpr($2), $4 , $5) }
    | selector unwrapExpr RANGE                                                  { $$ = newLogRange(newMatcherExpr($1), $3, $2 ) }
    | OPEN_PARENTHESIS selector unwrapExpr CLOSE_PARENTHESIS RANGE               { $$ = newLogRange(newMatcherExpr($2), $5, $3 ) }
    | selector pipelineExpr RANGE                                                { $$ = newLogRange(newPipelineExpr(newMatcherExpr($1), $2), $3, nil ) }
    | OPEN_PARENTHESIS selector pipelineExpr CLOSE_PARENTHESIS RANGE             { $$ = newLogRange(newPipelineExpr(newMatcherExpr($2), $3), $5, nil ) }
    | selector pipelineExpr unwrapExpr RANGE                                     { $$ = newLogRange(newPipelineExpr(newMatcherExpr($1), $2), $4, $3) }
    | OPEN_PARENTHESIS selector pipelineExpr unwrapExpr CLOSE_PARENTHESIS RANGE  { $$ = newLogRange(newPipelineExpr(newMatcherExpr($2), $3), $6, $4) }
    | selector RANGE pipelineExpr                                                { $$ = newLogRange(newPipelineExpr(newMatcherExpr($1), $3), $2, nil) }
    | selector RANGE pipelineExpr unwrapExpr                                     { $$ = newLogRange(newPipelineExpr(newMatcherExpr($1), $3), $2, $4 ) }
    | OPEN_PARENTHESIS logRangeExpr CLOSE_PARENTHESIS                            { $$ = $2 }
    | logRangeExpr error
    ;

unwrapExpr:
    PIPE UNWRAP IDENTIFIER                                                   { $$ = newUnwrapExpr($3, "")}
  | PIPE UNWRAP convOp OPEN_PARENTHESIS IDENTIFIER CLOSE_PARENTHESIS         { $$ = newUnwrapExpr($5, $3)}
  ;

convOp:
    DURATION_CONV           { $$ = OpConvDuration }
  | DURATION_SECONDS_CONV   { $$ = OpConvDurationSeconds }
  ;

rangeAggregationExpr:
      rangeOp OPEN_PARENTHESIS logRangeExpr CLOSE_PARENTHESIS                        { $$ = newRangeAggregationExpr($3, $1, nil, nil) }
    | rangeOp OPEN_PARENTHESIS NUMBER COMMA logRangeExpr CLOSE_PARENTHESIS           { $$ = newRangeAggregationExpr($5, $1, nil, &$3) }
    | rangeOp OPEN_PARENTHESIS logRangeExpr CLOSE_PARENTHESIS grouping               { $$ = newRangeAggregationExpr($3, $1, $5, nil) }
    | rangeOp OPEN_PARENTHESIS NUMBER COMMA logRangeExpr CLOSE_PARENTHESIS grouping  { $$ = newRangeAggregationExpr($5, $1, $7, &$3) }
    ;

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

pipelineExpr:
      pipelineStage                  { $$ = MultiPipelineExpr{ $1 } }
    | pipelineExpr pipelineStage     { $$ = append($1, $2)}
    ;

pipelineStage:
   lineFilters                   { $$ = $1 }
  | PIPE labelParser             { $$ = $2 }
  | PIPE labelFilter             { $$ = &labelFilterExpr{Filterer: $2 }}
  | PIPE lineFormatExpr          { $$ = $2 }
  | PIPE labelFormatExpr         { $$ = $2 }
  ;

lineFilters:
    filter STRING                 { $$ = newLineFilterExpr(nil, $1, $2 ) }
  | lineFilters filter STRING     { $$ = newLineFilterExpr($1, $2, $3 ) }

labelParser:
    JSON           { $$ = newLabelParserExpr(OpParserTypeJSON, "") }
  | LOGFMT         { $$ = newLabelParserExpr(OpParserTypeLogfmt, "") }
  | REGEXP STRING  { $$ = newLabelParserExpr(OpParserTypeRegexp, $2) }
  ;

lineFormatExpr: LINE_FMT STRING { $$ = newLineFmtExpr($2) };

labelFormat:
     IDENTIFIER EQ IDENTIFIER { $$ = newRenameLabelFmt($1, $3)}
  |  IDENTIFIER EQ STRING     { $$ = newTemplateLabelFmt($1, $3)}
  ;

labelsFormat:
    labelFormat                    { $$ = []labelFmt{ $1 } }
  | labelsFormat COMMA labelFormat { $$ = append($1, $3) }
  | labelsFormat COMMA error
  ;

labelFormatExpr: LABEL_FMT labelsFormat { $$ = newLabelFmtExpr($2) };

labelFilter:
      matcher { $$ = labelfilter.NewString($1) }
    | durationFilter { $$ = $1 }
    | numberFilter { $$ = $1 }
    | OPEN_PARENTHESIS labelFilter CLOSE_PARENTHESIS { $$ = $2 }
    | labelFilter labelFilter { $$ = labelfilter.NewAnd($1, $2 ) }
    | labelFilter AND labelFilter { $$ = labelfilter.NewAnd($1, $3 ) }
    | labelFilter COMMA labelFilter { $$ = labelfilter.NewAnd($1, $3 ) }
    | labelFilter OR labelFilter { $$ = labelfilter.NewOr($1, $3 ) }
    ;

durationFilter:
      IDENTIFIER GT DURATION  { $$ = labelfilter.NewDuration(labelfilter.FilterGreaterThan, $1, $3) }
    | IDENTIFIER GTE DURATION { $$ = labelfilter.NewDuration(labelfilter.FilterGreaterThanOrEqual, $1, $3) }
    | IDENTIFIER LT DURATION  { $$ = labelfilter.NewDuration(labelfilter.FilterLesserThan, $1, $3) }
    | IDENTIFIER LTE DURATION { $$ = labelfilter.NewDuration(labelfilter.FilterLesserThanOrEqual, $1, $3) }
    | IDENTIFIER NEQ DURATION { $$ = labelfilter.NewDuration(labelfilter.FilterNotEqual, $1, $3) }
    | IDENTIFIER EQ DURATION  { $$ = labelfilter.NewDuration(labelfilter.FilterEqual, $1, $3) }
    | IDENTIFIER CMP_EQ DURATION  { $$ = labelfilter.NewDuration(labelfilter.FilterEqual, $1, $3) }
    ;

numberFilter:
      IDENTIFIER GT NUMBER  { $$ = labelfilter.NewNumeric(labelfilter.FilterGreaterThan, $1, mustNewFloat($3))}
    | IDENTIFIER GTE NUMBER { $$ = labelfilter.NewNumeric(labelfilter.FilterGreaterThanOrEqual, $1, mustNewFloat($3))}
    | IDENTIFIER LT NUMBER  { $$ = labelfilter.NewNumeric(labelfilter.FilterLesserThan, $1, mustNewFloat($3))}
    | IDENTIFIER LTE NUMBER { $$ = labelfilter.NewNumeric(labelfilter.FilterLesserThanOrEqual, $1, mustNewFloat($3))}
    | IDENTIFIER NEQ NUMBER { $$ = labelfilter.NewNumeric(labelfilter.FilterNotEqual, $1, mustNewFloat($3))}
    | IDENTIFIER EQ NUMBER  { $$ = labelfilter.NewNumeric(labelfilter.FilterEqual, $1, mustNewFloat($3))}
    | IDENTIFIER CMP_EQ NUMBER  { $$ = labelfilter.NewNumeric(labelfilter.FilterEqual, $1, mustNewFloat($3))}
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
      COUNT_OVER_TIME    { $$ = OpRangeTypeCount }
    | RATE               { $$ = OpRangeTypeRate }
    | BYTES_OVER_TIME    { $$ = OpRangeTypeBytes }
    | BYTES_RATE         { $$ = OpRangeTypeBytesRate }
    | AVG_OVER_TIME      { $$ = OpRangeTypeAvg }
    | SUM_OVER_TIME      { $$ = OpRangeTypeSum }
    | MIN_OVER_TIME      { $$ = OpRangeTypeMin }
    | MAX_OVER_TIME      { $$ = OpRangeTypeMax }
    | STDVAR_OVER_TIME   { $$ = OpRangeTypeStdvar }
    | STDDEV_OVER_TIME   { $$ = OpRangeTypeStddev }
    | QUANTILE_OVER_TIME { $$ = OpRangeTypeQuantile }
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
