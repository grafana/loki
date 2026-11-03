package decoder

// The tokens of a stream are read as encoding/json reads them: the stream keeps where it is in the objects and
// the arrays which Token opened, so that a delimiter is checked by the grammar, and a value which Decode reads
// between the tokens follows the comma or the colon before it.

// tokenState is where the stream is between the tokens of the objects and the arrays which Token opened.
type tokenState uint8

const (
	// tokenTopValue is out of any: the values of the stream follow each other.
	tokenTopValue tokenState = iota
	tokenArrayStart
	tokenArrayValue
	tokenArrayComma
	tokenObjectStart
	tokenObjectKey
	tokenObjectColon
	tokenObjectValue
	tokenObjectComma
	// tokenFailed is after a read which failed: every read returns its error.
	tokenFailed
)

// tokenValueEnd moves the state after a value, which is the key of an object where one is waited for, as
// encoding/json of Go 1.27 reads it.
func (s *Stream) tokenValueEnd() {
	switch s.tokenState {
	case tokenArrayStart, tokenArrayValue:
		s.tokenState = tokenArrayComma
	case tokenObjectValue:
		s.tokenState = tokenObjectComma
	case tokenObjectStart, tokenObjectKey:
		s.tokenState = tokenObjectColon
	}
}
