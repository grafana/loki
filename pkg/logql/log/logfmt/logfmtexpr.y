

%{
package logfmt

func setScannerData(lex interface{}, data []interface{}) {
    lex.(*Scanner).data = data
}

%}

%union {
    str     string
    field   string
    list    []interface{}
}

%token<str>     STRING
%token<field>   FIELD

%type<str>  field value
%type<list> expressions

%%

logfmt:
    expressions  { setScannerData(LogfmtExprlex, $1) }

expressions:
    field              { $$ = []interface{}{$1} }
  | value              { $$ = []interface{}{$1} }
  | expressions value  { $$ = append($1, $2) }
  ;

field:
  FIELD     { $$ = $1 }

value:
  STRING    { $$ = $1 }