package pattern

%%{
    machine pattern;
    write data;
    access lex.;
    variable p lex.p;
    variable pe lex.pe;
    prepush {
        if len(lex.stack) <= lex.top {
            lex.stack = append(lex.stack, 0)
        }
    }
}%%

const LEXER_ERROR = 0

%%{
        identifier = '<' (alpha| '_') (alnum | '_' )* '>';
        literal = any;
}%%

func (lex *lexer) Lex(out *exprSymType) int {
    eof := lex.pe
    tok := 0

    %%{

        main := |*
            identifier => { tok = lex.handle(lex.identifier(out)); fbreak; };
            literal => { tok = lex.handle(lex.literal(out)); fbreak; };
        *|;

        write exec;
    }%%

    return tok;
}


func (lex *lexer) init() {
    %% write init;
}
