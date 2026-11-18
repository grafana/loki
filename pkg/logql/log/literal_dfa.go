package log

// literalDFA reports whether a line contains any literal of a fixed set. It is an Aho-Corasick
// automaton compiled to a DFA, so a scan does one table lookup per byte of the line.
//
// Its tables hold 4 bytes for each state and byte class. There is at most one state per byte of
// the literals, plus the root.
//
// It is safe for concurrent use: Contains only reads the tables.
type literalDFA struct {
	// transitions holds one row of numClasses entries for each state. An entry is the offset of
	// the row of the next state. dfaMatchBit is set in an entry when the next state ends a
	// literal.
	transitions []uint32

	// classes maps a byte to its column in a row. Bytes that no literal holds share column 0.
	classes [256]uint16
}

const dfaMatchBit = 1 << 31

// newLiteralDFA builds a literalDFA for literals. When foldASCII is true, an ASCII letter matches
// both of its cases, and the literals must hold no uppercase ASCII letter.
//
// No literal may be empty. The caller must keep the total size of the literals small enough that
// states times classes stays below 2^31.
func newLiteralDFA(literals [][]byte, foldASCII bool) *literalDFA {
	d := &literalDFA{}
	stride := d.assignClasses(literals, foldASCII)

	// Build the trie. A zero entry means "no child", because no edge leads back to the root.
	transitions := make([]uint32, stride)
	ends := []bool{false}
	for _, literal := range literals {
		state := 0
		for _, b := range literal {
			i := state*stride + int(d.classes[b])
			if transitions[i] == 0 {
				transitions = append(transitions, make([]uint32, stride)...)
				ends = append(ends, false)
				transitions[i] = uint32(len(ends) - 1)
			}
			state = int(transitions[i])
		}
		ends[state] = true
	}

	// Visit the states in breadth-first order, so the failure state of a state is complete before
	// the state itself. A missing edge then takes the edge of the failure state.
	failures := make([]uint32, len(ends))
	queue := make([]uint32, 0, len(ends))
	for column := range stride {
		if child := transitions[column]; child != 0 {
			queue = append(queue, child)
		}
	}
	for head := 0; head < len(queue); head++ {
		state := queue[head]
		ends[state] = ends[state] || ends[failures[state]]
		row, failureRow := int(state)*stride, int(failures[state])*stride
		for column := range stride {
			if child := transitions[row+column]; child != 0 {
				failures[child] = transitions[failureRow+column]
				queue = append(queue, child)
			} else {
				transitions[row+column] = transitions[failureRow+column]
			}
		}
	}

	for i, next := range transitions {
		entry := next * uint32(stride)
		if ends[next] {
			entry |= dfaMatchBit
		}
		transitions[i] = entry
	}
	d.transitions = transitions
	return d
}

// assignClasses gives each byte that a literal holds its own class, and returns the number of
// classes. With foldASCII, the two cases of a letter share a class.
func (d *literalDFA) assignClasses(literals [][]byte, foldASCII bool) int {
	var used [256]bool
	for _, literal := range literals {
		for _, b := range literal {
			used[b] = true
		}
	}

	next := uint16(1)
	for b := range used {
		if used[b] {
			d.classes[b] = next
			next++
		}
	}
	if foldASCII {
		for upper := byte('A'); upper <= 'Z'; upper++ {
			d.classes[upper] = d.classes[upper+'a'-'A']
		}
	}
	return int(next)
}

// Contains reports whether line contains any of the literals.
func (d *literalDFA) Contains(line []byte) bool {
	state := uint32(0)
	for _, b := range line {
		state = d.transitions[state+uint32(d.classes[b])]
		if state&dfaMatchBit != 0 {
			return true
		}
	}
	return false
}
