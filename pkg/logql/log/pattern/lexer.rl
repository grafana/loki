%%{
    machine pattern;
    write data;
    access lex.;
    variable p lex.p;
    variable pe lex.pe;
    prepush {
        // grow the stack if needed
        if len(lex.stack) <= lex.top {
            lex.stack = append(lex.stack, 0)
        }
    }
}%%
