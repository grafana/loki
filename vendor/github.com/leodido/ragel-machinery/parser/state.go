package parser

// State represents the ragel state variables for parsing.
type State parsingState

type parsingState struct {
	cs         int // _start at time 0, then eventually current state
	errorState int // _error > fixme(leodido)
	finalState int // _first_final > fixme(leodido)

	p, pe, eof int    // parsing pointers
	data       []byte // data pointer
}

// Set sets the state variables of a ragel parser.
func (s *State) Set(cs, p, pe, eof int) {
	s.cs, s.p, s.pe, s.eof = cs, p, pe, eof
}

// Get retrieves the state variables of a ragel parser.
func (s *State) Get() (cs, p, pe, eof int, data []byte) {
	return s.cs, s.p, s.pe, s.eof, s.data
}
