package jsonparser

import (
	"bytes"
	"io"
	"strconv"
)

const (
	defaultReaderBufferSize    = 64 * 1024
	defaultReaderMaxBufferSize = 64 * 1024 * 1024
	maxConsecutiveEmptyReads   = 100
)

// ReaderParser provides path-based access to a JSON stream. It is intended for
// one operation per stream: call Get, GetString, or ArrayEach after creating
// the parser. ReaderParser is not safe for concurrent use.
//
// The sliding window is bounded by Config.MaxBufferSize while skipping input.
// A returned value (or one ArrayEach element) may exceed that size because its
// complete byte slice must remain available to the caller.
// SYS-REQ-116
type ReaderParser struct {
	reader   io.Reader
	buf      []byte
	bufStart int64
	pos      int
	config   Config

	readErr    error
	emptyReads int
}

// NewReaderParser creates a streaming parser. If opts contains a Config, the
// first Config controls lenient parsing and the sliding-window size.
// SYS-REQ-116
func NewReaderParser(r io.Reader, opts ...Config) *ReaderParser {
	config := DefaultConfig
	if len(opts) > 0 {
		config = opts[0]
	}
	return &ReaderParser{
		reader: r,
		config: config,
	}
}

// Get returns the value addressed by keys without loading the whole document.
// As with the package-level Get, string values are returned without quotes but
// retain their JSON escapes.
// SYS-REQ-116
func (rp *ReaderParser) Get(keys ...string) (value []byte, vt ValueType, err error) {
	for _, key := range keys {
		if key == "" {
			return nil, NotExist, KeyPathNotFoundError
		}
	}

	if err := rp.skipWhitespace(); err != nil {
		if err == io.EOF {
			return nil, NotExist, KeyPathNotFoundError
		}
		return nil, NotExist, err
	}

	var raw []byte
	if len(keys) == 0 {
		raw, err = rp.captureValue()
	} else {
		var found bool
		found, raw, err = rp.findValue(keys, 0)
		if err == nil && !found {
			err = KeyPathNotFoundError
		}
	}
	if err != nil {
		return nil, NotExist, err
	}

	value, vt, _, err = rp.config.Get(raw)
	if err != nil {
		return value, vt, err
	}
	return value, vt, nil
}

// GetString returns and decodes the string addressed by keys.
// SYS-REQ-116
func (rp *ReaderParser) GetString(keys ...string) (string, error) {
	value, vt, err := rp.Get(keys...)
	if err != nil {
		return "", err
	}
	if vt != String {
		if vt == Null {
			return "", NullValueError
		}
		return "", valueTypeError("string", value)
	}
	if bytes.IndexByte(value, '\\') == -1 {
		return string(value), nil
	}

	var stackbuf [unescapeStackBufSize]byte
	unescaped, err := unescapeConfig(rp.config, value, stackbuf[:])
	if err != nil {
		return "", MalformedValueError
	}
	return string(unescaped), nil
}

// ArrayEach incrementally iterates a root JSON array. Callback value slices are
// valid for the duration of the callback and must be copied if retained.
// Memory use is bounded by the largest element plus the sliding read window.
// SYS-REQ-116
func (rp *ReaderParser) ArrayEach(cb func(value []byte, vt ValueType, err error)) error {
	if err := rp.skipWhitespace(); err != nil {
		if err == io.EOF {
			return MalformedObjectError
		}
		return err
	}

	b, err := rp.peekByte(false)
	if err != nil {
		return err
	}
	if b != '[' {
		return MalformedArrayError
	}
	rp.pos++

	if err := rp.skipWhitespace(); err != nil {
		if err == io.EOF {
			return MalformedArrayError
		}
		return err
	}
	b, err = rp.peekByte(false)
	if err != nil {
		return err
	}
	if b == ']' {
		rp.pos++
		return nil
	}

	for {
		raw, captureErr := rp.captureValue()
		if captureErr != nil {
			cb(nil, Unknown, captureErr)
			return captureErr
		}

		value, vt, _, valueErr := rp.config.Get(raw)
		cb(value, vt, valueErr)
		if valueErr != nil {
			return valueErr
		}

		if err := rp.skipWhitespace(); err != nil {
			if err == io.EOF {
				return MalformedArrayError
			}
			return err
		}
		b, err = rp.peekByte(false)
		if err != nil {
			return err
		}
		switch b {
		case ']':
			rp.pos++
			return nil
		case ',':
			rp.pos++
		default:
			return MalformedArrayError
		}

		if err := rp.skipWhitespace(); err != nil {
			if err == io.EOF {
				return MalformedArrayError
			}
			return err
		}
		b, err = rp.peekByte(false)
		if err != nil {
			return err
		}
		if b == ']' {
			return MalformedArrayError
		}
	}
}

// findValue resolves keys[depth:] against the value at rp.pos. A not-found
// result consumes the current value so a containing object can continue past a
// duplicate key. A found result leaves the final value in the sliding buffer.
// SYS-REQ-116
func (rp *ReaderParser) findValue(keys []string, depth int) (bool, []byte, error) {
	if err := rp.skipWhitespace(); err != nil {
		if err == io.EOF {
			return false, nil, MalformedJsonError
		}
		return false, nil, err
	}
	if depth == len(keys) {
		raw, err := rp.captureValue()
		return err == nil, raw, err
	}

	b, err := rp.peekByte(false)
	if err != nil {
		return false, nil, err
	}
	switch b {
	case '{':
		return rp.findObjectValue(keys, depth)
	case '[':
		return rp.findArrayValue(keys, depth)
	default:
		if err := rp.skipValue(); err != nil {
			return false, nil, err
		}
		return false, nil, nil
	}
}

// SYS-REQ-116
func (rp *ReaderParser) findObjectValue(keys []string, depth int) (bool, []byte, error) {
	rp.pos++ // opening {

	if err := rp.skipWhitespace(); err != nil {
		if err == io.EOF {
			return false, nil, MalformedObjectError
		}
		return false, nil, err
	}
	b, err := rp.peekByte(false)
	if err != nil {
		return false, nil, err
	}
	if b == '}' {
		rp.pos++
		return false, nil, nil
	}

	for {
		matches, err := rp.readObjectKey(keys[depth])
		if err != nil {
			return false, nil, err
		}

		if err := rp.skipWhitespace(); err != nil {
			if err == io.EOF {
				return false, nil, MalformedJsonError
			}
			return false, nil, err
		}
		b, err = rp.peekByte(false)
		if err != nil {
			return false, nil, err
		}
		if b != ':' {
			return false, nil, MalformedJsonError
		}
		rp.pos++

		if matches {
			found, raw, err := rp.findValue(keys, depth+1)
			if err != nil || found {
				return found, raw, err
			}
		} else {
			if err := rp.skipWhitespace(); err != nil {
				if err == io.EOF {
					return false, nil, MalformedJsonError
				}
				return false, nil, err
			}
			if err := rp.skipValue(); err != nil {
				return false, nil, err
			}
		}

		if err := rp.skipWhitespace(); err != nil {
			if err == io.EOF {
				return false, nil, MalformedObjectError
			}
			return false, nil, err
		}
		b, err = rp.peekByte(false)
		if err != nil {
			return false, nil, err
		}
		switch b {
		case '}':
			rp.pos++
			return false, nil, nil
		case ',':
			rp.pos++
		default:
			return false, nil, MalformedObjectError
		}

		if err := rp.skipWhitespace(); err != nil {
			if err == io.EOF {
				return false, nil, MalformedObjectError
			}
			return false, nil, err
		}
		b, err = rp.peekByte(false)
		if err != nil {
			return false, nil, err
		}
		if b == '}' {
			return false, nil, MalformedObjectError
		}
	}
}

// SYS-REQ-116
func (rp *ReaderParser) findArrayValue(keys []string, depth int) (bool, []byte, error) {
	wanted, ok := parseReaderArrayIndex(keys[depth])
	if !ok {
		if err := rp.skipValue(); err != nil {
			return false, nil, err
		}
		return false, nil, nil
	}

	rp.pos++ // opening [
	if err := rp.skipWhitespace(); err != nil {
		if err == io.EOF {
			return false, nil, MalformedArrayError
		}
		return false, nil, err
	}
	b, err := rp.peekByte(false)
	if err != nil {
		return false, nil, err
	}
	if b == ']' {
		rp.pos++
		return false, nil, nil
	}

	for index := 0; ; index++ {
		if index == wanted {
			found, raw, err := rp.findValue(keys, depth+1)
			if err != nil || found {
				return found, raw, err
			}
		} else {
			if err := rp.skipValue(); err != nil {
				return false, nil, err
			}
		}

		if err := rp.skipWhitespace(); err != nil {
			if err == io.EOF {
				return false, nil, MalformedArrayError
			}
			return false, nil, err
		}
		b, err = rp.peekByte(false)
		if err != nil {
			return false, nil, err
		}
		switch b {
		case ']':
			rp.pos++
			return false, nil, nil
		case ',':
			rp.pos++
		default:
			return false, nil, MalformedArrayError
		}

		if err := rp.skipWhitespace(); err != nil {
			if err == io.EOF {
				return false, nil, MalformedArrayError
			}
			return false, nil, err
		}
		b, err = rp.peekByte(false)
		if err != nil {
			return false, nil, err
		}
		if b == ']' {
			return false, nil, MalformedArrayError
		}
	}
}

// SYS-REQ-116
func parseReaderArrayIndex(key string) (int, bool) {
	if len(key) < 3 || key[0] != '[' || key[len(key)-1] != ']' {
		return 0, false
	}
	index, err := strconv.Atoi(key[1 : len(key)-1])
	return index, err == nil && index >= 0
}

// SYS-REQ-116
func (rp *ReaderParser) readObjectKey(wanted string) (bool, error) {
	b, err := rp.peekByte(false)
	if err != nil {
		return false, err
	}
	if b != '"' && !(b == '\'' && rp.config.AllowSingleQuotes) {
		return false, MalformedJsonError
	}

	// The parent prefix is no longer needed. Discarding it before retaining the
	// key ensures that an arbitrarily late key does not grow the window.
	rp.discardConsumed()
	start := rp.pos
	if err := rp.scanString(true); err != nil {
		return false, err
	}
	token := rp.buf[start:rp.pos]
	body := token[1 : len(token)-1]
	if bytes.IndexByte(body, '\\') == -1 {
		return string(body) == wanted, nil
	}

	var stackbuf [unescapeStackBufSize]byte
	unescaped, err := unescapeConfig(rp.config, body, stackbuf[:])
	if err != nil {
		return false, err
	}
	return string(unescaped) == wanted, nil
}

// SYS-REQ-116
func (rp *ReaderParser) captureValue() ([]byte, error) {
	if err := rp.skipWhitespace(); err != nil {
		if err == io.EOF {
			return nil, MalformedJsonError
		}
		return nil, err
	}

	// Retain only this value and any already-read suffix. The suffix is at most
	// one read chunk; prefixes consumed while locating the value are released.
	rp.discardConsumed()
	start := rp.pos
	if err := rp.scanValue(true); err != nil {
		return nil, err
	}
	return rp.buf[start:rp.pos], nil
}

// SYS-REQ-116
func (rp *ReaderParser) skipValue() error {
	return rp.scanValue(false)
}

// SYS-REQ-116
func (rp *ReaderParser) scanValue(retain bool) error {
	b, err := rp.peekByte(retain)
	if err != nil {
		if err == io.EOF {
			return MalformedJsonError
		}
		return err
	}

	switch {
	case b == '"' || (b == '\'' && rp.config.AllowSingleQuotes):
		return rp.scanString(retain)
	case b == '[' || b == '{':
		return rp.scanComposite(retain, b)
	default:
		return rp.scanScalar(retain)
	}
}

// SYS-REQ-116
func (rp *ReaderParser) scanString(retain bool) error {
	quote, err := rp.peekByte(retain)
	if err != nil {
		return err
	}
	rp.pos++
	escaped := false

	for {
		b, err := rp.peekByte(retain)
		if err != nil {
			if err == io.EOF {
				return MalformedStringError
			}
			return err
		}
		rp.pos++

		if escaped {
			escaped = false
			continue
		}
		if b == '\\' {
			escaped = true
			continue
		}
		if b == quote {
			return nil
		}
	}
}

// SYS-REQ-116
func (rp *ReaderParser) scanComposite(retain bool, opening byte) error {
	stack := []byte{closingDelimiter(opening)}
	rp.pos++

	for len(stack) > 0 {
		b, err := rp.peekByte(retain)
		if err != nil {
			if err == io.EOF {
				if opening == '[' {
					return MalformedArrayError
				}
				return MalformedObjectError
			}
			return err
		}

		if b == '"' || (b == '\'' && rp.config.AllowSingleQuotes) {
			if err := rp.scanString(retain); err != nil {
				return err
			}
			continue
		}

		switch b {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if b != stack[len(stack)-1] {
				return MalformedJsonError
			}
			stack = stack[:len(stack)-1]
		}
		rp.pos++
	}
	return nil
}

// SYS-REQ-116
func closingDelimiter(opening byte) byte {
	if opening == '[' {
		return ']'
	}
	return '}'
}

// SYS-REQ-116
func (rp *ReaderParser) scanScalar(retain bool) error {
	consumed := false
	for {
		b, err := rp.peekByte(retain)
		if err != nil {
			if err == io.EOF && consumed {
				return nil
			}
			if err == io.EOF {
				return MalformedJsonError
			}
			return err
		}
		if isReaderValueDelimiter(b) {
			if !consumed {
				return MalformedJsonError
			}
			return nil
		}
		consumed = true
		rp.pos++
	}
}

// SYS-REQ-116
func isReaderValueDelimiter(b byte) bool {
	return isJSONWhitespace(b) || b == ',' || b == '}' || b == ']'
}

// SYS-REQ-116
func (rp *ReaderParser) skipWhitespace() error {
	for {
		b, err := rp.peekByte(false)
		if err != nil {
			return err
		}
		if !isJSONWhitespace(b) {
			return nil
		}
		rp.pos++
	}
}

// peekByte returns the next byte. When retain is false, fully consumed chunks
// are discarded before reading more data.
// SYS-REQ-116
func (rp *ReaderParser) peekByte(retain bool) (byte, error) {
	for rp.pos >= len(rp.buf) {
		if !retain {
			rp.discardConsumed()
		}
		if err := rp.readMore(); err != nil {
			return 0, err
		}
	}
	return rp.buf[rp.pos], nil
}

// SYS-REQ-116
func (rp *ReaderParser) readMore() error {
	if rp.readErr != nil {
		return rp.readErr
	}
	if rp.reader == nil {
		rp.readErr = io.EOF
		return rp.readErr
	}

	readSize := defaultReaderBufferSize
	maxBufferSize := rp.config.MaxBufferSize
	if maxBufferSize <= 0 {
		maxBufferSize = defaultReaderMaxBufferSize
	}
	if readSize > maxBufferSize {
		readSize = maxBufferSize
	}
	if readSize < 1 {
		readSize = 1
	}

	oldLen := len(rp.buf)
	if cap(rp.buf)-oldLen < readSize {
		grown := make([]byte, oldLen, oldLen+readSize)
		copy(grown, rp.buf)
		rp.buf = grown
	}
	rp.buf = rp.buf[:oldLen+readSize]
	n, err := rp.reader.Read(rp.buf[oldLen:])
	rp.buf = rp.buf[:oldLen+n]

	if n > 0 {
		rp.emptyReads = 0
		if err != nil {
			rp.readErr = err
		}
		return nil
	}
	if err != nil {
		rp.readErr = err
		return err
	}

	rp.emptyReads++
	if rp.emptyReads >= maxConsecutiveEmptyReads {
		rp.readErr = io.ErrNoProgress
		return rp.readErr
	}
	return nil
}

// SYS-REQ-116
func (rp *ReaderParser) discardConsumed() {
	if rp.pos == 0 {
		return
	}
	rp.bufStart += int64(rp.pos)
	rp.buf = rp.buf[rp.pos:]
	rp.pos = 0
}

// SYS-REQ-116
func valueTypeError(want string, value []byte) error {
	return &readerTypeError{want: want, value: string(value)}
}

type readerTypeError struct {
	want  string
	value string
}

// SYS-REQ-116
func (e *readerTypeError) Error() string {
	return "Value is not a " + e.want + ": " + e.value
}
