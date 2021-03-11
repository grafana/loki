package parser

// Machiner is an interface that wraps the Exec method.
type Machiner interface {
	// Exec contains the ragel finite-state machine code and returns boundaries.
	Exec(state *State) (p int, pe int)
	// OnErr is a method called when an error is encountered.
	OnErr(chunk []byte, err error)
	// OnEOF is a method called when an EOF is encountered.
	OnEOF(chunk []byte)
	// OnCompletion is a method called when the parser loop completes.
	OnCompletion()
}
