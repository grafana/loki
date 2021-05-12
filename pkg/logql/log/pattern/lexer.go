package pattern

type lexer struct {
	data        []byte
	p, pe, cs   int
	ts, te, act int

	stack []int
	top   int

	lastnewline int
	curline     int

	// errs  []parseError
	// exprs []expr.Expr
}
