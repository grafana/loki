package jsonparser

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Errors
var (
	KeyPathNotFoundError       = errors.New("Key path not found")
	UnknownValueTypeError      = errors.New("Unknown value type")
	MalformedJsonError         = errors.New("Malformed JSON error")
	MalformedStringError       = errors.New("Value is string, but can't find closing '\"' symbol")
	MalformedArrayError        = errors.New("Value is array, but can't find closing ']' symbol")
	MalformedObjectError       = errors.New("Value looks like object, but can't find closing '}' symbol")
	MalformedValueError        = errors.New("Value looks like Number/Boolean/None, but can't find its end: ',' or '}' symbol")
	OverflowIntegerError       = errors.New("Value is number, but overflowed while parsing")
	MalformedStringEscapeError = errors.New("Encountered an invalid escape sequence in a string")
	NullValueError             = errors.New("Value is null")
)

// How much stack space to allocate for unescaping JSON strings; if a string longer
// than this needs to be escaped, it will result in a heap allocation
const unescapeStackBufSize = 64

// SYS-REQ-044
//
//	reqproof:lemma tokenEnd_in_range func(data []byte) bool {
//	  r := tokenEnd(data)
//	  return r >= 0 && r <= len(data)
//	}
//
//	reqproof:lemma tokenEnd_nonneg func(data []byte) bool {
//	  // tokenEnd never signals via a negative sentinel — the empty-input
//	  // path returns len(data)==0 (still nonneg), and any hit returns the
//	  // loop index (also nonneg).
//	  return tokenEnd(data) >= 0
//	}
//
//	reqproof:lemma tokenEnd_empty_zero func(data []byte) bool {
//	  return !(len(data) == 0) || tokenEnd(data) == 0
//	}
//
//	reqproof:lemma tokenEnd_path_indexable_implies_nonneg func(data []byte) bool {
//	  r := tokenEnd(data)
//	  if r < len(data) {
//	    return r >= 0
//	  }
//	  return true
//	}
func tokenEnd(data []byte) int {
	for i, c := range data {
		// reqproof:invariant 0 <= i
		// reqproof:invariant i <= len(data)
		if c != 32 && c != 10 && c != 13 && c != 9 && c != 44 && c != 125 && c != 93 {
			continue
		}
		return i
	}

	return len(data)
}

// isJSONWhitespace reports whether b is one of the four JSON whitespace bytes
// (space 0x20, tab 0x09, LF 0x0A, CR 0x0D) per RFC 8259 §2. Used by Delete's
// trailing-comma cleanup to decide whether the byte at endOffset+tokEnd is
// whitespace preceding a comma, so the cleanup advances past both. The byte
// set mirrors tokenEnd's whitespace classification above.
// SYS-REQ-010, SYS-REQ-034, SYS-REQ-035
func isJSONWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// SYS-REQ-001
// NOTE: findTokenStart's two-conditional-return body shape exposes
// the translator's __early_val scoping bug; we leave it without an
// in-range lemma. (Documented as a Phase S.2c.4 follow-up.)
func findTokenStart(data []byte, token byte) int {
	for i := len(data) - 1; i >= 0; i-- {
		switch data[i] {
		case token:
			return i
		case '[', '{':
			return 0
		}
	}

	return 0
}

// SYS-REQ-001, SYS-REQ-020, SYS-REQ-024
func findKeyStart(data []byte, key string) (int, error) {
	return findKeyStartConfig(DefaultConfig, data, key)
}

// SYS-REQ-115
func findKeyStartConfig(config Config, data []byte, key string) (int, error) {
	i := nextTokenConfig(config, data)
	if i == -1 {
		return i, KeyPathNotFoundError
	}
	ln := len(data)
	// Note: nextToken returning non-negative (checked above) guarantees ln > 0,
	// so the former ln > 0 guard was tautological and has been removed.
	if data[i] == '{' || data[i] == '[' {
		i += 1
	}
	var stackbuf [unescapeStackBufSize]byte // stack-allocated array for allocation-free unescaping of small strings

	if ku, err := unescapeConfig(config, StringToBytes(key), stackbuf[:]); err == nil {
		key = bytesToString(&ku)
	}

	for i < ln {
		switch data[i] {
		case '"', '\'':
			quote := data[i]
			if quote == '\'' && !config.AllowSingleQuotes {
				break
			}
			i++
			keyBegin := i

			strEnd, keyEscaped := stringEndConfig(config, data[i:], quote)
			if strEnd == -1 {
				break
			}
			i += strEnd
			keyEnd := i - 1

			valueOffset := nextTokenConfig(config, data[i:])
			if valueOffset == -1 {
				break
			}

			i += valueOffset

			// if string is a key, and key level match
			k := data[keyBegin:keyEnd]
			// for unescape: if there are no escape sequences, this is cheap; if there are, it is a
			// bit more expensive, but causes no allocations unless len(key) > unescapeStackBufSize
			if keyEscaped {
				if ku, err := unescapeConfig(config, k, stackbuf[:]); err != nil {
					break
				} else {
					k = ku
				}
			}

			if data[i] == ':' && len(key) == len(k) && bytesToString(&k) == key {
				return keyBegin - 1, nil
			}

		case '[':
			end := blockEndConfig(config, data[i:], data[i], ']')
			if end != -1 {
				i = i + end
			}
		case '{':
			end := blockEndConfig(config, data[i:], data[i], '}')
			if end != -1 {
				i = i + end
			}
		}
		i++
	}

	return -1, KeyPathNotFoundError
}

// SYS-REQ-001
//
//	reqproof:lemma tokenStart_in_range func(data []byte) bool {
//	  r := tokenStart(data)
//	  return r >= 0 && r <= len(data)
//	}
//
//	reqproof:lemma tokenStart_nonneg func(data []byte) bool {
//	  return tokenStart(data) >= 0
//	}
//
//	reqproof:lemma tokenStart_empty_zero func(data []byte) bool {
//	  return !(len(data) == 0) || tokenStart(data) == 0
//	}
//
//	reqproof:lemma tokenStart_path_indexable_when_nonempty func(data []byte) bool {
//	  r := tokenStart(data)
//	  if len(data) > 0 {
//	    return r >= 0 && r < len(data)
//	  }
//	  return r == 0
//	}
func tokenStart(data []byte) int {
	for i := len(data) - 1; i >= 0; i-- {
		// reqproof:invariant -1 <= i
		// reqproof:invariant i < len(data)
		c := data[i]
		if c != 10 && c != 13 && c != 9 && c != 44 && c != 123 && c != 91 {
			continue
		}
		return i
	}

	return 0
}

// SYS-REQ-001
// Find position of next character which is not whitespace
//
//	reqproof:lemma nextToken_in_range func(data []byte) bool {
//	  r := nextToken(data)
//	  return r >= -1 && r < len(data)
//	}
//
//	reqproof:lemma nextToken_empty_neg func(data []byte) bool {
//	  return !(len(data) == 0) || nextToken(data) == -1
//	}
//
//	reqproof:lemma nextToken_signed_disjoint func(data []byte) bool {
//	  r := nextToken(data)
//	  // Result is either -1 (sentinel) or a non-negative index — never -2 or below
//	  return r == -1 || r >= 0
//	}
//
//	reqproof:lemma nextToken_path_indexable_implies_lt_len func(data []byte) bool {
//	  r := nextToken(data)
//	  if r >= 0 {
//	    return r < len(data)
//	  }
//	  return true
//	}
func nextToken(data []byte) int {
	return nextTokenConfig(DefaultConfig, data)
}

// SYS-REQ-115
func nextTokenConfig(_ Config, data []byte) int {
	for i, c := range data {
		// reqproof:invariant 0 <= i
		// reqproof:invariant i <= len(data)
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			continue
		}
		return i
	}

	return -1
}

// SYS-REQ-001
// Find position of last character which is not whitespace
//
//	reqproof:lemma lastToken_in_range func(data []byte) bool {
//	  r := lastToken(data)
//	  return r >= -1 && r < len(data)
//	}
//
//	reqproof:lemma lastToken_empty_neg func(data []byte) bool {
//	  return !(len(data) == 0) || lastToken(data) == -1
//	}
//
//	reqproof:lemma lastToken_signed_disjoint func(data []byte) bool {
//	  r := lastToken(data)
//	  // Result is either -1 (sentinel) or a non-negative index — never -2 or below
//	  return r == -1 || r >= 0
//	}
//
//	reqproof:lemma lastToken_path_indexable_implies_lt_len func(data []byte) bool {
//	  r := lastToken(data)
//	  if r >= 0 {
//	    return r < len(data)
//	  }
//	  return true
//	}
func lastToken(data []byte) int {
	for i := len(data) - 1; i >= 0; i-- {
		// reqproof:invariant -1 <= i
		// reqproof:invariant i < len(data)
		c := data[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			continue
		}
		return i
	}

	return -1
}

// SYS-REQ-045
// Tries to find the end of string
// Support if string contains escaped quote symbols.
func stringEnd(data []byte) (int, bool) {
	return stringEndConfig(DefaultConfig, data, '"')
}

// SYS-REQ-115
func stringEndConfig(_ Config, data []byte, quote byte) (int, bool) {
	// SWAR (SIMD-Within-A-Register) fast path: scan 8 bytes at a time for
	// either the closing quote or a backslash, then a per-byte tail that
	// counts the run of '\\' before each quote candidate.
	//
	// Investigation: easyjson's jlexer.findStringLen
	// (mailru/easyjson@v0.9.2/jlexer/lexer.go:247) uses a single SIMD
	// bytes.IndexByte('"') plus a backward backslash-run count, deferring the
	// separate backslash scan to unescapeStringToken. That style was ported
	// and benchmarked here as "single IndexByte for the quote + a bounded
	// bytes.IndexByte(data[:firstQuote], '\\') for the escape flag". On
	// arm64 (Apple M4 Max, NEON) the two IndexByte *function calls* cost more
	// than this inline 8-byte SWAR loop, because jsonparser must compute the
	// escape flag inline (its callers gate unescapeConfig on it), so the
	// second scan cannot be deferred the way easyjson defers it.
	//
	// Measured on M4 Max (BenchmarkJsonParserLarge, median of 5):
	//   SWAR (this):          ~21000 ns/op
	//   two-IndexByte port:   ~22600 ns/op  (-7%)
	// For reference easyjson itself is ~33200 ns/op here, so jsonparser
	// already leads; the easyjson technique is not beneficial on arm64.
	const swarLsb = 0x0101010101010101
	const swarMsb = 0x8080808080808080
	broadcastQuote := uint64(quote) * swarLsb
	broadcastBackslash := uint64('\\') * swarLsb

	i := 0
	n := len(data)
	for i+8 <= n {
		w := binary.LittleEndian.Uint64(data[i:])
		xq := w ^ broadcastQuote
		xb := w ^ broadcastBackslash
		quoteHit := (xq - swarLsb) & ^xq & swarMsb
		bsHit := (xb - swarLsb) & ^xb & swarMsb
		if quoteHit|bsHit == 0 {
			i += 8
			continue
		}
		break
	}
	escaped := false
	for ; i < n; i++ {
		c := data[i]
		if c == quote {
			if !escaped {
				return i + 1, false
			}
			j := i - 1
			for {
				if j < 0 || data[j] != '\\' {
					return i + 1, true // even run of backslashes
				}
				j--
				if j < 0 || data[j] != '\\' {
					break // odd run of backslashes -> quote is escaped
				}
				j--
			}
		} else if c == '\\' {
			escaped = true
		}
	}
	return -1, escaped
}

// SYS-REQ-046
// Find end of the data structure, array or object.
// For array openSym and closeSym will be '[' and ']', for object '{' and '}'
func blockEnd(data []byte, openSym byte, closeSym byte) int {
	return blockEndConfig(DefaultConfig, data, openSym, closeSym)
}

// SYS-REQ-115
func blockEndConfig(config Config, data []byte, openSym byte, closeSym byte) int {
	level := 0
	i := 0
	ln := len(data)

	for i < ln {
		switch data[i] {
		case '"', '\'': // If inside a configured string, skip it
			quote := data[i]
			if quote == '\'' && !config.AllowSingleQuotes {
				break
			}
			se, _ := stringEndConfig(config, data[i+1:], quote)
			if se == -1 {
				return -1
			}
			i += se
		case openSym: // If open symbol, increase level
			level++
		case closeSym: // If close symbol, increase level
			level--

			// If we have returned to the original level, we're done
			if level == 0 {
				return i + 1
			}
		}
		i++
	}

	return -1
}

// SYS-REQ-001, SYS-REQ-020, SYS-REQ-021, SYS-REQ-022, SYS-REQ-023, SYS-REQ-047, SYS-REQ-111
func searchKeys(data []byte, keys ...string) int {
	return searchKeysConfig(DefaultConfig, data, keys...)
}

// SYS-REQ-115
func searchKeysConfig(config Config, data []byte, keys ...string) int {
	keyLevel := 0
	level := 0
	i := 0
	ln := len(data)
	lk := len(keys)
	lastMatched := true

	if lk == 0 {
		return 0
	}

	var stackbuf [unescapeStackBufSize]byte // stack-allocated array for allocation-free unescaping of small strings

	for i < ln {
		switch data[i] {
		case '"', '\'':
			quote := data[i]
			if quote == '\'' && !config.AllowSingleQuotes {
				break
			}
			i++
			keyBegin := i

			strEnd, keyEscaped := stringEndConfig(config, data[i:], quote)
			if strEnd == -1 {
				return -1
			}
			i += strEnd
			keyEnd := i - 1

			valueOffset := nextTokenConfig(config, data[i:])
			if valueOffset == -1 {
				return -1
			}

			i += valueOffset

			// if string is a key
			if data[i] == ':' {
				if level < 1 {
					return -1
				}

				key := data[keyBegin:keyEnd]

				// for unescape: if there are no escape sequences, this is cheap; if there are, it is a
				// bit more expensive, but causes no allocations unless len(key) > unescapeStackBufSize
				var keyUnesc []byte
				if !keyEscaped {
					keyUnesc = key
				} else if ku, err := unescapeConfig(config, key, stackbuf[:]); err != nil {
					return -1
				} else {
					keyUnesc = ku
				}

				if level <= len(keys) {
					if equalStr(&keyUnesc, keys[level-1]) {
						lastMatched = true

						// if key level match
						if keyLevel == level-1 {
							keyLevel++
							// If we found all keys in path
							if keyLevel == lk {
								return i + 1
							}
						}
					} else {
						lastMatched = false
					}
				} else {
					return -1
				}
			} else {
				i--
			}
		case '{':

			// in case parent key is matched then only we will increase the level otherwise can directly
			// can move to the end of this block
			if !lastMatched {
				end := blockEndConfig(config, data[i:], '{', '}')
				if end == -1 {
					return -1
				}
				i += end - 1
			} else {
				level++
			}
		case '}':
			level--
			if level == keyLevel {
				keyLevel--
			}
		case '[':
			// If we want to get array element by index
			// guard: empty key component — not an array index, fall through to skip.
			if keyLevel == level && len(keys[level]) > 0 && keys[level][0] == '[' {
				keyLen := len(keys[level])
				// Note: keys[level][0] == '[' is guaranteed by the outer if-guard,
				// so the former middle term `keys[level][0] != '['` was always false
				// (dead code) and has been removed.
				// guard: bounds-check on the same variable before the [keyLen-1] deref.
				if len(keys[level]) < 3 || keys[level][keyLen-1] != ']' {
					return -1
				}
				aIdx, err := strconv.Atoi(keys[level][1 : keyLen-1])
				if err != nil {
					return -1
				}
				var curIdx int
				var valueFound []byte
				var valueOffset int
				curI := i
				arrayEachConfig(config, data[i:], func(value []byte, dataType ValueType, offset int, err error) {
					if curIdx == aIdx {
						valueFound = value
						valueOffset = offset
						if dataType == String {
							valueOffset = valueOffset - 2
							valueFound = data[curI+valueOffset : curI+valueOffset+len(value)+2]
						}
					}
					curIdx += 1
				})

				if valueFound == nil {
					return -1
				} else {
					subIndex := searchKeysConfig(config, valueFound, keys[level+1:]...)
					if subIndex < 0 {
						return -1
					}
					return i + valueOffset + subIndex
				}
			} else {
				// Do not search for keys inside arrays
				if arraySkip := blockEndConfig(config, data[i:], '[', ']'); arraySkip == -1 {
					return -1
				} else {
					i += arraySkip - 1
				}
			}
		case ':': // If encountered, JSON data is malformed
			return -1
		}

		i++
	}

	return -1
}

// SYS-REQ-008
func sameTree(p1, p2 []string) bool {
	minLen := len(p1)
	if len(p2) < minLen {
		minLen = len(p2)
	}

	for pi_1, p_1 := range p1[:minLen] {
		if p2[pi_1] != p_1 {
			return false
		}
	}

	return true
}

const stackArraySize = 128

// SYS-REQ-008, SYS-REQ-085, SYS-REQ-111
func EachKey(data []byte, cb func(int, []byte, ValueType, error), paths ...[]string) int {
	var x struct{}
	var level, pathsMatched, i int
	ln := len(data)

	pathFlags := make([]bool, stackArraySize)[:]
	if len(paths) > cap(pathFlags) {
		pathFlags = make([]bool, len(paths))[:]
	}
	pathFlags = pathFlags[0:len(paths)]

	var maxPath int
	for _, p := range paths {
		if len(p) > maxPath {
			maxPath = len(p)
		}
	}

	pathsBuf := make([]string, stackArraySize)[:]
	if maxPath > cap(pathsBuf) {
		pathsBuf = make([]string, maxPath)[:]
	}
	pathsBuf = pathsBuf[0:maxPath]

	for i < ln {
		switch data[i] {
		case '"':
			i++
			keyBegin := i

			strEnd, keyEscaped := stringEnd(data[i:])
			if strEnd == -1 {
				return -1
			}
			i += strEnd

			keyEnd := i - 1

			valueOffset := nextToken(data[i:])
			if valueOffset == -1 {
				return -1
			}

			i += valueOffset

			// if string is a key, and key level match
			if data[i] == ':' {
				match := -1
				key := data[keyBegin:keyEnd]

				// for unescape: if there are no escape sequences, this is cheap; if there are, it is a
				// bit more expensive, but causes no allocations unless len(key) > unescapeStackBufSize
				var keyUnesc []byte
				if !keyEscaped {
					keyUnesc = key
				} else {
					var stackbuf [unescapeStackBufSize]byte
					if ku, err := Unescape(key, stackbuf[:]); err != nil {
						return -1
					} else {
						keyUnesc = ku
					}
				}

				if maxPath >= level {
					if level < 1 {
						cb(-1, nil, Unknown, MalformedJsonError)
						return -1
					}

					pathsBuf[level-1] = bytesToString(&keyUnesc)
					for pi, p := range paths {
						if len(p) != level || pathFlags[pi] || !equalStr(&keyUnesc, p[level-1]) || !sameTree(p, pathsBuf[:level]) {
							continue
						}

						match = pi

						pathsMatched++
						pathFlags[pi] = true

						v, dt, _, e := Get(data[i+1:])
						cb(pi, v, dt, e)

						if pathsMatched == len(paths) {
							break
						}
					}
					if pathsMatched == len(paths) {
						return i
					}
				}

				if match == -1 {
					tokenOffset := nextToken(data[i+1:])
					i += tokenOffset
					// Note: i is now at the character BEFORE the value (the colon
					// when tokenOffset==0, or the last whitespace character otherwise).
					// The former `if data[i] == '{'` block-skip was structurally dead
					// code because i never reaches the opening brace — the outer loop's
					// i++ advances to it on the next iteration.  Likewise, the former
					// `if i < ln` guard was tautological since i remains within bounds.
				}

				switch data[i] {
				case '{', '}', '[', '"':
					i--
				}
			} else {
				i--
			}
		case '{':
			level++
		case '}':
			level--
		case '[':
			var ok bool
			arrIdxFlags := make(map[int]struct{})

			pIdxFlags := make([]bool, stackArraySize)[:]
			if len(paths) > cap(pIdxFlags) {
				pIdxFlags = make([]bool, len(paths))[:]
			}
			pIdxFlags = pIdxFlags[0:len(paths)]

			if level < 0 {
				cb(-1, nil, Unknown, MalformedJsonError)
				return -1
			}

			for pi, p := range paths {
				// guard: empty key component — skip this path (not an array index).
				if len(p) < level+1 || pathFlags[pi] || len(p[level]) == 0 || p[level][0] != '[' || !sameTree(p, pathsBuf[:level]) {
					continue
				}

				indexComponent := p[level]
				if len(indexComponent) < 3 || indexComponent[len(indexComponent)-1] != ']' {
					continue
				}
				aIdx, err := strconv.Atoi(indexComponent[1 : len(indexComponent)-1])
				if err != nil {
					continue
				}
				arrIdxFlags[aIdx] = x
				pIdxFlags[pi] = true
			}

			if len(arrIdxFlags) > 0 {
				level++

				var curIdx int
				arrOff, _ := ArrayEach(data[i:], func(value []byte, dataType ValueType, offset int, err error) {
					if _, ok = arrIdxFlags[curIdx]; ok {
						for pi, p := range paths {
							if !pIdxFlags[pi] || pathFlags[pi] {
								continue
							}

							indexComponent := p[level-1]
							aIdx, parseErr := strconv.Atoi(indexComponent[1 : len(indexComponent)-1])
							if parseErr != nil || curIdx != aIdx {
								continue
							}

							if level == len(p) {
								// ArrayEach has already parsed the terminal value.
								// In particular, string values do not include their
								// quotes and therefore cannot be reparsed by Get.
								pathsMatched++
								pathFlags[pi] = true
								cb(pi, value, dataType, err)
								continue
							}

							of := searchKeys(value, p[level:]...)
							if of == -1 {
								continue
							}

							v, dt, _, e := Get(value[of:])
							pathsMatched++
							pathFlags[pi] = true
							cb(pi, v, dt, e)
						}
					}

					curIdx += 1
				})

				if pathsMatched == len(paths) {
					return i
				}

				i += arrOff - 1
			} else {
				// Do not search for keys inside arrays
				if arraySkip := blockEnd(data[i:], '[', ']'); arraySkip == -1 {
					return -1
				} else {
					i += arraySkip - 1
				}
			}
		case ']':
			level--
		}

		i++
	}

	return -1
}

// EachKeyErr finds the requested paths and allows the callback to stop
// iteration by returning an error. io.EOF stops iteration gracefully.
// SYS-REQ-008
func EachKeyErr(data []byte, cb func(idx int, value []byte, vt ValueType, err error) error, paths ...[]string) error {
	var x struct{}
	var level, pathsMatched, i int
	ln := len(data)

	pathFlags := make([]bool, stackArraySize)[:]
	if len(paths) > cap(pathFlags) {
		pathFlags = make([]bool, len(paths))[:]
	}
	pathFlags = pathFlags[0:len(paths)]

	var maxPath int
	for _, p := range paths {
		if len(p) > maxPath {
			maxPath = len(p)
		}
	}

	pathsBuf := make([]string, stackArraySize)[:]
	if maxPath > cap(pathsBuf) {
		pathsBuf = make([]string, maxPath)[:]
	}
	pathsBuf = pathsBuf[0:maxPath]

	for i < ln {
		switch data[i] {
		case '"':
			i++
			keyBegin := i

			strEnd, keyEscaped := stringEnd(data[i:])
			if strEnd == -1 {
				return nil
			}
			i += strEnd

			keyEnd := i - 1

			valueOffset := nextToken(data[i:])
			if valueOffset == -1 {
				return nil
			}

			i += valueOffset

			if data[i] == ':' {
				match := -1
				key := data[keyBegin:keyEnd]

				var keyUnesc []byte
				if !keyEscaped {
					keyUnesc = key
				} else {
					var stackbuf [unescapeStackBufSize]byte
					if ku, unescapeErr := Unescape(key, stackbuf[:]); unescapeErr != nil {
						return nil
					} else {
						keyUnesc = ku
					}
				}

				if maxPath >= level {
					if level < 1 {
						callbackErr := cb(-1, nil, Unknown, MalformedJsonError)
						if callbackErr != nil && !errors.Is(callbackErr, io.EOF) {
							return callbackErr
						}
						return nil
					}

					pathsBuf[level-1] = bytesToString(&keyUnesc)
					for pi, p := range paths {
						if len(p) != level || pathFlags[pi] || !equalStr(&keyUnesc, p[level-1]) || !sameTree(p, pathsBuf[:level]) {
							continue
						}

						match = pi

						pathsMatched++
						pathFlags[pi] = true

						v, dt, _, parseErr := Get(data[i+1:])
						if callbackErr := cb(pi, v, dt, parseErr); callbackErr != nil {
							if errors.Is(callbackErr, io.EOF) {
								return nil
							}
							return callbackErr
						}

						if pathsMatched == len(paths) {
							break
						}
					}
					if pathsMatched == len(paths) {
						return nil
					}
				}

				if match == -1 {
					tokenOffset := nextToken(data[i+1:])
					i += tokenOffset
				}

				switch data[i] {
				case '{', '}', '[', '"':
					i--
				}
			} else {
				i--
			}
		case '{':
			level++
		case '}':
			level--
		case '[':
			var ok bool
			arrIdxFlags := make(map[int]struct{})

			pIdxFlags := make([]bool, stackArraySize)[:]
			if len(paths) > cap(pIdxFlags) {
				pIdxFlags = make([]bool, len(paths))[:]
			}
			pIdxFlags = pIdxFlags[0:len(paths)]

			if level < 0 {
				callbackErr := cb(-1, nil, Unknown, MalformedJsonError)
				if callbackErr != nil && !errors.Is(callbackErr, io.EOF) {
					return callbackErr
				}
				return nil
			}

			for pi, p := range paths {
				if len(p) < level+1 || pathFlags[pi] || len(p[level]) == 0 || p[level][0] != '[' || !sameTree(p, pathsBuf[:level]) {
					continue
				}

				indexComponent := p[level]
				if len(indexComponent) < 3 || indexComponent[len(indexComponent)-1] != ']' {
					continue
				}
				aIdx, parseErr := strconv.Atoi(indexComponent[1 : len(indexComponent)-1])
				if parseErr != nil {
					continue
				}
				arrIdxFlags[aIdx] = x
				pIdxFlags[pi] = true
			}

			if len(arrIdxFlags) > 0 {
				level++

				var curIdx int
				var callbackErr error
				var stopped bool
				arrOff, _, _ := arrayEachErr(data[i:], func(value []byte, dataType ValueType, offset int, parseErr error) error {
					if _, ok = arrIdxFlags[curIdx]; ok {
						for pi, p := range paths {
							if !pIdxFlags[pi] || pathFlags[pi] {
								continue
							}

							indexComponent := p[level-1]
							aIdx, indexErr := strconv.Atoi(indexComponent[1 : len(indexComponent)-1])
							if indexErr != nil || curIdx != aIdx {
								continue
							}

							if level == len(p) {
								pathsMatched++
								pathFlags[pi] = true
								callbackErr = cb(pi, value, dataType, parseErr)
								if callbackErr != nil {
									stopped = true
									return callbackErr
								}
								continue
							}

							of := searchKeys(value, p[level:]...)
							if of == -1 {
								continue
							}

							v, dt, _, getErr := Get(value[of:])
							pathsMatched++
							pathFlags[pi] = true
							callbackErr = cb(pi, v, dt, getErr)
							if callbackErr != nil {
								stopped = true
								return callbackErr
							}
						}
					}

					curIdx++
					return nil
				})

				if stopped {
					if errors.Is(callbackErr, io.EOF) {
						return nil
					}
					return callbackErr
				}

				if pathsMatched == len(paths) {
					return nil
				}

				i += arrOff - 1
			} else {
				if arraySkip := blockEnd(data[i:], '[', ']'); arraySkip == -1 {
					return nil
				} else {
					i += arraySkip - 1
				}
			}
		case ']':
			level--
		}

		i++
	}

	return nil
}

// Data types available in valid JSON data.
type ValueType int

const (
	NotExist = ValueType(iota)
	String
	Number
	Object
	Array
	Boolean
	Null
	Unknown
)

// SYS-REQ-001
func (vt ValueType) String() string {
	switch vt {
	case NotExist:
		return "non-existent"
	case String:
		return "string"
	case Number:
		return "number"
	case Object:
		return "object"
	case Array:
		return "array"
	case Boolean:
		return "boolean"
	case Null:
		return "null"
	default:
		return "unknown"
	}
}

var (
	trueLiteral  = []byte("true")
	falseLiteral = []byte("false")
	nullLiteral  = []byte("null")
)

// SYS-REQ-009, SYS-REQ-110, SYS-REQ-111
func createInsertComponent(keys []string, setValue []byte, comma, object bool) []byte {
	// guard: empty key component — not an array index.
	isIndex := len(keys[0]) > 0 && string(keys[0][0]) == "["
	offset := 0
	lk := calcAllocateSpace(keys, setValue, comma, object)
	buffer := make([]byte, lk, lk)
	if comma {
		offset += WriteToBuffer(buffer[offset:], ",")
	}
	if isIndex && !comma {
		offset += WriteToBuffer(buffer[offset:], "[")
	} else {
		if object {
			offset += WriteToBuffer(buffer[offset:], "{")
		}
		if !isIndex {
			offset += WriteToBuffer(buffer[offset:], "\"")
			offset += WriteToBuffer(buffer[offset:], keys[0])
			offset += WriteToBuffer(buffer[offset:], "\":")
		}
	}

	for i := 1; i < len(keys); i++ {
		// guard: empty key component — treat as object key, not array index.
		if len(keys[i]) > 0 && string(keys[i][0]) == "[" {
			offset += WriteToBuffer(buffer[offset:], "[")
		} else {
			offset += WriteToBuffer(buffer[offset:], "{\"")
			offset += WriteToBuffer(buffer[offset:], keys[i])
			offset += WriteToBuffer(buffer[offset:], "\":")
		}
	}
	offset += copy(buffer[offset:], setValue)
	for i := len(keys) - 1; i > 0; i-- {
		// guard: empty key component — treat as object key, not array index.
		if len(keys[i]) > 0 && string(keys[i][0]) == "[" {
			offset += WriteToBuffer(buffer[offset:], "]")
		} else {
			offset += WriteToBuffer(buffer[offset:], "}")
		}
	}
	if isIndex && !comma {
		offset += WriteToBuffer(buffer[offset:], "]")
	}
	if object && !isIndex {
		offset += WriteToBuffer(buffer[offset:], "}")
	}
	return buffer
}

// SYS-REQ-009, SYS-REQ-111
func calcAllocateSpace(keys []string, setValue []byte, comma, object bool) int {
	// guard: empty key component — not an array index.
	isIndex := len(keys[0]) > 0 && string(keys[0][0]) == "["
	lk := 0
	if comma {
		// ,
		lk += 1
	}
	if isIndex && !comma {
		// []
		lk += 2
	} else {
		if object {
			// {
			lk += 1
		}
		if !isIndex {
			// "keys[0]"
			lk += len(keys[0]) + 3
		}
	}

	lk += len(setValue)
	for i := 1; i < len(keys); i++ {
		// guard: empty key component — treat as object key, not array index.
		if len(keys[i]) > 0 && string(keys[i][0]) == "[" {
			// []
			lk += 2
		} else {
			// {"keys[i]":setValue}
			lk += len(keys[i]) + 5
		}
	}

	if object && !isIndex {
		// }
		lk += 1
	}

	return lk
}

// SYS-REQ-009
func WriteToBuffer(buffer []byte, str string) int {
	copy(buffer, str)
	return len(str)
}

/*

Del - Receives existing data structure, path to delete.

Returns:
`data` - return modified data

*/
// SYS-REQ-010, SYS-REQ-033, SYS-REQ-034, SYS-REQ-035, SYS-REQ-048, SYS-REQ-049, SYS-REQ-050, SYS-REQ-056
func Delete(data []byte, keys ...string) []byte {
	result, _ := deleteFoundConfig(DefaultConfig, data, keys...)
	return result
}

// DeleteFound removes the value addressed by keys and reports whether the
// value was found and removed. When found is false, result is data unchanged.
// SYS-REQ-010
func DeleteFound(data []byte, keys ...string) (result []byte, found bool) {
	return deleteFoundConfig(DefaultConfig, data, keys...)
}

// SYS-REQ-115
func deleteFoundConfig(config Config, data []byte, keys ...string) (result []byte, found bool) {
	lk := len(keys)
	if lk == 0 {
		// Deleting the root produces an empty document whose backing array
		// must not alias the caller's input.
		return make([]byte, 0), true
	}

	array := false
	if len(keys[lk-1]) > 0 && string(keys[lk-1][0]) == "[" {
		array = true
	}

	var startOffset, keyOffset int
	endOffset := len(data)
	var err error
	if !array {
		if len(keys) > 1 {
			_, _, startOffset, endOffset, err = internalGetConfig(config, data, keys[:lk-1]...)
			if err != nil {
				// problem parsing the data
				return data, false
			}
		}

		keyOffset, err = findKeyStartConfig(config, data[startOffset:endOffset], keys[lk-1])
		if err == KeyPathNotFoundError {
			// problem parsing the data
			return data, false
		}
		keyOffset += startOffset
		var subEndOffset int
		_, _, _, subEndOffset, err = internalGetConfig(config, data[startOffset:endOffset], keys[lk-1])
		if err != nil {
			return data, false
		}
		endOffset = startOffset + subEndOffset
		tokEnd := tokenEnd(data[endOffset:])
		tokStart := findTokenStart(data[:keyOffset], ","[0])

		if endOffset+tokEnd >= len(data) {
			// tokenEnd sentinel: no delimiter found, input is truncated
			return data, false
		}

		// guard: tokenEnd sentinel may return -1 on truncated input; bounds-check before deref.
		idx := endOffset + tokEnd
		// Scan forward from idx through any JSON whitespace to find the next
		// real token. The original check only matched a single ' ' byte
		// before the comma, so inputs like '{"a":1,\n"b":2}' or '[0,0  ,0]'
		// (multiple whitespace bytes) bypassed the cleanup and left a
		// dangling comma sequence. Found by FuzzPathMutation.
		nextTokIdx := idx
		for nextTokIdx < len(data) && isJSONWhitespace(data[nextTokIdx]) {
			nextTokIdx++
		}
		if len(data) > idx && data[idx] == ',' {
			endOffset += tokEnd + 1
		} else if len(data) > nextTokIdx && data[nextTokIdx] == ',' {
			// Symmetric with the array-branch case below: when the bytes
			// after the deleted element are "<ws>+," (one or more JSON
			// whitespace bytes then a comma), advance endOffset past all
			// of them so the trailing comma is removed with the element.
			endOffset += nextTokIdx - idx + tokEnd + 1
		} else if len(data) > idx && data[idx] == '}' && data[tokStart] == ',' {
			keyOffset = tokStart
		}
	} else {
		_, _, keyOffset, endOffset, err = internalGetConfig(config, data, keys...)
		if err != nil {
			// problem parsing the data
			return data, false
		}

		tokEnd := tokenEnd(data[endOffset:])
		tokStart := findTokenStart(data[:keyOffset], ","[0])

		if endOffset+tokEnd >= len(data) {
			// tokenEnd sentinel: no delimiter found, input is truncated
			return data, false
		}

		// guard: tokenEnd sentinel may return -1 on truncated input; bounds-check before deref.
		idx := endOffset + tokEnd
		// Scan forward from idx through any JSON whitespace to find the next
		// real token (mirrors the object-branch cleanup above). The original
		// check only matched a single ' ' byte before the comma, so inputs
		// like '[0,0  ,0]' or '[0,0\n\n,0]' bypassed the cleanup. Found by
		// FuzzPathMutation.
		nextTokIdx := idx
		for nextTokIdx < len(data) && isJSONWhitespace(data[nextTokIdx]) {
			nextTokIdx++
		}
		if len(data) > idx && data[idx] == ',' {
			endOffset += tokEnd + 1
		} else if len(data) > nextTokIdx && data[nextTokIdx] == ',' {
			// Symmetric with the object-branch case above: when the bytes
			// after the deleted element are "<ws>+," (one or more JSON
			// whitespace bytes then a comma), advance endOffset past all
			// of them so the trailing comma is removed with the element.
			endOffset += nextTokIdx - idx + tokEnd + 1
		} else if len(data) > idx && data[idx] == ']' && data[tokStart] == ',' {
			keyOffset = tokStart
		}
	}

	// We need to remove remaining trailing comma if we delete last element in the object.
	// Extract nextToken once to avoid the redundant double call in the original code.
	prevTok := lastToken(data[:keyOffset])
	remainedValue := data[endOffset:]
	remainedTok := nextTokenConfig(config, remainedValue)

	var newOffset int
	// Cleanup must remove the trailing comma both for objects (close '}') and
	// arrays (close ']'). The original check only handled '}', so deleting
	// the last array element left a dangling ',]' / ', ]' sequence and
	// produced malformed JSON output (found by FuzzPathMutation,
	// e.g. Delete("[0,0 ]", "[1]") -> "[0, ]"). The array close is now
	// covered symmetrically with the object close.
	if prevTok > -1 && remainedTok > -1 && (remainedValue[remainedTok] == '}' || remainedValue[remainedTok] == ']') && data[prevTok] == ',' {
		newOffset = prevTok
	} else if prevTok > -1 {
		newOffset = prevTok + 1
	} else {
		newOffset = 0
	}

	// Allocate the exact result size so neither the copy operation nor later
	// appends to the returned slice can write into the input backing array.
	result = make([]byte, newOffset+len(data)-endOffset)
	copy(result, data[:newOffset])
	copy(result[newOffset:], data[endOffset:])

	return result, true
}

/*

Set - Receives existing data structure, path to set, and data to set at that key.

Returns:
`value` - modified byte array
`err` - On any parsing error

*/
// SYS-REQ-009, SYS-REQ-051, SYS-REQ-068, SYS-REQ-069, SYS-REQ-070, SYS-REQ-110
func Set(data []byte, setValue []byte, keys ...string) (value []byte, err error) {
	return setConfig(DefaultConfig, data, setValue, keys...)
}

// SYS-REQ-115
func setConfig(config Config, data []byte, setValue []byte, keys ...string) (value []byte, err error) {
	// ensure keys are set
	if len(keys) == 0 {
		return nil, KeyPathNotFoundError
	}

	_, _, startOffset, endOffset, err := internalGetConfig(config, data, keys...)
	if err != nil {
		if err != KeyPathNotFoundError {
			// problem parsing the data
			return nil, err
		}
		// full path doesnt exist
		// does any subpath exist?
		var depth int
		for i := range keys {
			_, _, start, end, sErr := internalGetConfig(config, data, keys[:i+1]...)
			if sErr != nil {
				break
			} else {
				endOffset = end
				startOffset = start
				depth++
			}
		}
		comma := true
		object := false
		// KI-3: when the path's next component expects one container type but
		// the existing structure is the other (array-index [N] under an object,
		// or an object key under an array), auto-coerce the container to the
		// type expected by the path and proceed with fresh insertion. The
		// mismatched container is treated as if it needs to be (re)created, so
		// the output is always valid JSON.
		coerceTopLevel := false
		coerceStart := 0
		topLevelArrayAppend := false
		topLevelArrayEmpty := false
		topLevelArrayStart := 0
		topLevelArrayInsertOffset := 0
		if endOffset == -1 {
			firstToken := nextTokenConfig(config, data)
			if firstToken < 0 {
				return nil, KeyPathNotFoundError
			}
			pathIsIndex := len(keys[0]) > 0 && keys[0][0] == '['
			// An empty trailing key component is the degenerate "no path
			// provided" case (SYS-REQ-111), not a real object key — it must
			// keep returning KeyPathNotFoundError on a non-object root, so it
			// is excluded from auto-coerce.
			pathIsObjectKey := len(keys[0]) > 0 && !pathIsIndex
			dataIsObject := data[firstToken] == '{'
			dataIsArray := data[firstToken] == '['
			if (pathIsIndex && dataIsObject) || (pathIsObjectKey && dataIsArray) {
				// SYS-REQ-009: cross-type Set at the top level — replace the
				// mismatched container with a fresh container of the type the
				// path expects, then perform a fresh insertion.
				coerceTopLevel = true
				coerceStart = firstToken
				comma = false
				object = !pathIsIndex
				endOffset = lastToken(data)
			} else if pathIsIndex && dataIsArray {
				// SYS-REQ-110: a missing index in a top-level array appends at
				// the array's end, just as it does for a nested array.
				arrayEnd := blockEndConfig(config, data[firstToken:], '[', ']')
				if arrayEnd == -1 {
					return nil, MalformedArrayError
				}
				endOffset = firstToken + arrayEnd - 1
				topLevelArrayAppend = true
				topLevelArrayStart = firstToken
				topLevelArrayInsertOffset = endOffset

				interior := data[firstToken+1 : endOffset]
				lastInteriorToken := lastToken(interior)
				if lastInteriorToken == -1 {
					// createInsertComponent wraps an index component in brackets
					// when comma is false, so replace the empty root array.
					comma = false
					topLevelArrayEmpty = true
				} else if interior[lastInteriorToken] == ',' {
					// Replace a trailing comma (and any whitespace after it)
					// with the normal comma-prefixed append component.
					beforeComma := lastToken(interior[:lastInteriorToken])
					if beforeComma == -1 {
						comma = false
						topLevelArrayEmpty = true
					} else {
						topLevelArrayInsertOffset = firstToken + 1 + lastInteriorToken
					}
				}
			} else if !dataIsObject {
				// Non-container input and the degenerate empty-key-on-non-object
				// case are rejected.
				return nil, KeyPathNotFoundError
			} else {
				// Don't need a comma if the input is an empty object
				secondToken := firstToken + 1 + nextTokenConfig(config, data[firstToken+1:])
				if data[secondToken] == '}' {
					comma = false
				}
				// Set the top level key at the end (accounting for any trailing whitespace)
				// This assumes last token is valid like '}', could check and return error
				endOffset = lastToken(data)
			}
		}
		depthOffset := endOffset
		if depth != 0 {
			trailing := keys[depth:]
			pathIsIndex := len(trailing) > 0 && len(trailing[0]) > 0 && trailing[0][0] == '['
			containerIsObject := data[startOffset] == '{'
			containerIsArray := data[startOffset] == '['
			if (pathIsIndex && containerIsObject) || (!pathIsIndex && containerIsArray) {
				// SYS-REQ-009: cross-type Set under a subpath — replace the
				// mismatched container (data[startOffset:depthOffset]) with a
				// fresh container of the type the path expects. startOffset and
				// depthOffset already bound the existing container, so keep them
				// and let createInsertComponent build the replacement.
				comma = false
				object = !pathIsIndex
			} else {
				// if subpath is a non-empty object, add to it
				// or if subpath is a non-empty array, add to it
				// guard: nextToken returns -1 on truncated input; bounds-check the computed offset.
				subObjOff := startOffset + 1 + nextTokenConfig(config, data[startOffset+1:])
				// The array-append condition must fire for ANY non-empty array
				// (scalar, string, bool, null, nested, or object elements), not
				// just arrays whose first element happens to be '{'. The former
				// `data[subObjOff] == '{'` check silently destroyed scalar
				// arrays on beyond-length Set (SYS-REQ-110 violation: the whole
				// array was replaced with a single-element [value]).
				if (containerIsObject && subObjOff >= 0 && subObjOff < len(data) && data[subObjOff] != '}') ||
					(containerIsArray && subObjOff >= 0 && subObjOff < len(data) && data[subObjOff] != ']' && pathIsIndex) {
					depthOffset--
					startOffset = depthOffset
					// otherwise, over-write it with a new object
				} else {
					comma = false
					object = true
				}
			}
		} else {
			if coerceTopLevel {
				startOffset = coerceStart
				depthOffset = endOffset + 1
			} else if topLevelArrayAppend {
				if topLevelArrayEmpty {
					startOffset = topLevelArrayStart
					depthOffset = endOffset + 1
				} else {
					startOffset = topLevelArrayInsertOffset
				}
			} else {
				startOffset = depthOffset
			}
		}
		insertComponent := createInsertComponent(keys[depth:], setValue, comma, object)
		value = make([]byte, startOffset+len(insertComponent)+len(data)-depthOffset)
		offset := copy(value, data[:startOffset])
		offset += copy(value[offset:], insertComponent)
		copy(value[offset:], data[depthOffset:])
	} else {
		// path currently exists
		startComponent := data[:startOffset]
		endComponent := data[endOffset:]

		value = make([]byte, len(startComponent)+len(endComponent)+len(setValue))
		newEndOffset := startOffset + len(setValue)
		copy(value[0:startOffset], startComponent)
		copy(value[startOffset:newEndOffset], setValue)
		copy(value[newEndOffset:], endComponent)
	}
	return value, nil
}

// SYS-REQ-001, SYS-REQ-027
func getType(data []byte, offset int) ([]byte, ValueType, int, error) {
	return getTypeConfig(DefaultConfig, data, offset)
}

// SYS-REQ-115
func getTypeConfig(config Config, data []byte, offset int) ([]byte, ValueType, int, error) {
	var dataType ValueType
	endOffset := offset

	// if string value
	if data[offset] == '"' || (config.AllowSingleQuotes && data[offset] == '\'') {
		dataType = String
		if idx, _ := stringEndConfig(config, data[offset+1:], data[offset]); idx != -1 {
			endOffset += idx + 1
		} else {
			return nil, dataType, offset, MalformedStringError
		}
	} else if data[offset] == '[' { // if array value
		dataType = Array
		// break label, for stopping nested loops
		endOffset = blockEndConfig(config, data[offset:], '[', ']')

		if endOffset == -1 {
			return nil, dataType, offset, MalformedArrayError
		}

		endOffset += offset
	} else if data[offset] == '{' { // if object value
		dataType = Object
		// break label, for stopping nested loops
		endOffset = blockEndConfig(config, data[offset:], '{', '}')

		if endOffset == -1 {
			return nil, dataType, offset, MalformedObjectError
		}

		endOffset += offset
	} else {
		// Number, Boolean or None
		// tokenEnd returns len(data) when no delimiter is found, never -1,
		// so the old end == -1 guard was dead code and has been removed.
		end := tokenEnd(data[endOffset:])

		value := data[offset : endOffset+end]

		switch data[offset] {
		case 't', 'f': // true or false
			if bytes.Equal(value, trueLiteral) || bytes.Equal(value, falseLiteral) {
				dataType = Boolean
			} else {
				return nil, Unknown, offset, UnknownValueTypeError
			}
		case 'u', 'n': // undefined or null
			if bytes.Equal(value, nullLiteral) {
				dataType = Null
			} else {
				return nil, Unknown, offset, UnknownValueTypeError
			}
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '-':
			dataType = Number
		default:
			return nil, Unknown, offset, UnknownValueTypeError
		}

		endOffset += end
	}
	return data[offset:endOffset], dataType, endOffset, nil
}

/*
Get - Receives data structure, and key path to extract value from.

Returns:
`value` - Pointer to original data structure containing key value, or just empty slice if nothing found or error
`dataType` -    Can be: `NotExist`, `String`, `Number`, `Object`, `Array`, `Boolean` or `Null`
`offset` - Offset from provided data structure where key value ends. Used mostly internally, for example for `ArrayEach` helper.
`err` - If key not found or any other parsing issue it should return error. If key not found it also sets `dataType` to `NotExist`

Accept multiple keys to specify path to JSON value (in case of quering nested structures).
If no keys provided it will try to extract closest JSON value (simple ones or object/array), useful for reading streams or arrays, see `ArrayEach` implementation.
*/
// SYS-REQ-001, SYS-REQ-016, SYS-REQ-017, SYS-REQ-018, SYS-REQ-019, SYS-REQ-025, SYS-REQ-026, SYS-REQ-041, SYS-REQ-042, SYS-REQ-043
func Get(data []byte, keys ...string) (value []byte, dataType ValueType, offset int, err error) {
	a, b, _, d, e := internalGet(data, keys...)
	return a, b, d, e
}

// SYS-REQ-001
func internalGet(data []byte, keys ...string) (value []byte, dataType ValueType, offset, endOffset int, err error) {
	return internalGetConfig(DefaultConfig, data, keys...)
}

// SYS-REQ-115
func internalGetConfig(config Config, data []byte, keys ...string) (value []byte, dataType ValueType, offset, endOffset int, err error) {
	if len(keys) > 0 {
		if offset = searchKeysConfig(config, data, keys...); offset == -1 {
			return nil, NotExist, -1, -1, KeyPathNotFoundError
		}
	}

	// Go to closest value
	nO := nextTokenConfig(config, data[offset:])
	if nO == -1 {
		return nil, NotExist, offset, -1, MalformedJsonError
	}

	offset += nO
	value, dataType, endOffset, err = getTypeConfig(config, data, offset)
	if err != nil {
		return value, dataType, offset, endOffset, err
	}

	// Strip quotes from string values
	if dataType == String {
		value = value[1 : len(value)-1]
	}

	return value[:len(value):len(value)], dataType, offset, endOffset, nil
}

// SYS-REQ-006, SYS-REQ-028, SYS-REQ-029, SYS-REQ-052, SYS-REQ-053, SYS-REQ-055, SYS-REQ-083
// ArrayEach is used when iterating arrays, accepts a callback function with the same return arguments as `Get`.
func ArrayEach(data []byte, cb func(value []byte, dataType ValueType, offset int, err error), keys ...string) (offset int, err error) {
	return arrayEachConfig(DefaultConfig, data, cb, keys...)
}

// SYS-REQ-115
func arrayEachConfig(config Config, data []byte, cb func(value []byte, dataType ValueType, offset int, err error), keys ...string) (offset int, err error) {
	if len(data) == 0 {
		return -1, MalformedObjectError
	}

	nT := nextTokenConfig(config, data)
	if nT == -1 {
		return -1, MalformedJsonError
	}

	// Guard: when ArrayEach is called without a key path, the addressed
	// root value must be an array. Without this guard, the main loop below
	// happily parses the first token of a non-array value (e.g. the opening
	// key of an object, or a bare number) as if it were an array element,
	// invoking the callback once with bogus data before eventually returning
	// MalformedArrayError. A caller performing side effects in the callback
	// would observe a spurious invocation on input that is not an array at
	// all. (SYS-REQ-029 partition: non-array root, no key path.)
	// When a key path IS provided, the keys block below already enforces the
	// same contract via its own `data[offset] != '['` check after resolving
	// the path, so the guard is only needed for the no-keys case.
	if len(keys) == 0 && data[nT] != '[' {
		return -1, MalformedArrayError
	}

	offset = nT + 1

	if len(keys) > 0 {
		if offset = searchKeysConfig(config, data, keys...); offset == -1 {
			return offset, KeyPathNotFoundError
		}

		// Go to closest value
		nO := nextTokenConfig(config, data[offset:])
		if nO == -1 {
			return offset, MalformedJsonError
		}

		offset += nO

		if data[offset] != '[' {
			return offset, MalformedArrayError
		}

		offset++
	}

	nO := nextTokenConfig(config, data[offset:])
	if nO == -1 {
		return offset, MalformedJsonError
	}

	offset += nO

	if data[offset] == ']' {
		return offset, nil
	}

	for {
		v, t, o, e := config.Get(data[offset:])

		if o == 0 {
			// When Get returns endOffset==0, it always means a parse error
			// (no valid value found at the current position). The former
			// e==nil/break branch was structurally unreachable because Get
			// never returns endOffset==0 without an error.
			return offset, e
		}

		// Pass the error to the callback — the callback signature declares
		// an err parameter, so callers who check it should see real errors.
		cb(v, t, offset+o-len(v), e)

		if e != nil {
			return offset, e
		}

		offset += o

		skipToToken := nextTokenConfig(config, data[offset:])
		if skipToToken == -1 {
			return offset, MalformedArrayError
		}
		offset += skipToToken

		if data[offset] == ']' {
			break
		}

		if data[offset] != ',' {
			return offset, MalformedArrayError
		}

		offset++
	}

	return offset, nil
}

// ArrayEachErr is used when iterating arrays and allows the callback to stop
// iteration by returning an error. io.EOF stops iteration without returning an
// error. The returned count includes the element whose callback stopped the
// iteration.
// SYS-REQ-004
func ArrayEachErr(data []byte, cb func(value []byte, dataType ValueType, offset int, err error) error, keys ...string) (count int, err error) {
	_, count, err = arrayEachErr(data, cb, keys...)
	return count, err
}

// arrayEachErr also returns ArrayEach's closing-bracket offset for use by
// EachKeyErr while traversing array-index paths.
// SYS-REQ-004
func arrayEachErr(data []byte, cb func(value []byte, dataType ValueType, offset int, err error) error, keys ...string) (offset, count int, err error) {
	if len(data) == 0 {
		return -1, 0, MalformedObjectError
	}

	nT := nextToken(data)
	if nT == -1 {
		return -1, 0, MalformedJsonError
	}

	if len(keys) == 0 && data[nT] != '[' {
		return -1, 0, MalformedArrayError
	}

	offset = nT + 1

	if len(keys) > 0 {
		if offset = searchKeys(data, keys...); offset == -1 {
			return offset, 0, KeyPathNotFoundError
		}

		nO := nextToken(data[offset:])
		if nO == -1 {
			return offset, 0, MalformedJsonError
		}

		offset += nO

		if data[offset] != '[' {
			return offset, 0, MalformedArrayError
		}

		offset++
	}

	nO := nextToken(data[offset:])
	if nO == -1 {
		return offset, 0, MalformedJsonError
	}

	offset += nO

	if data[offset] == ']' {
		return offset, 0, nil
	}

	for {
		v, t, o, parseErr := Get(data[offset:])

		if o == 0 {
			return offset, count, parseErr
		}

		count++
		if callbackErr := cb(v, t, offset+o-len(v), parseErr); callbackErr != nil {
			if errors.Is(callbackErr, io.EOF) {
				return offset, count, nil
			}
			return offset, count, callbackErr
		}

		if parseErr != nil {
			return offset, count, parseErr
		}

		offset += o

		skipToToken := nextToken(data[offset:])
		if skipToToken == -1 {
			return offset, count, MalformedArrayError
		}
		offset += skipToToken

		if data[offset] == ']' {
			break
		}

		if data[offset] != ',' {
			return offset, count, MalformedArrayError
		}

		offset++
	}

	return offset, count, nil
}

// SYS-REQ-007, SYS-REQ-030, SYS-REQ-031, SYS-REQ-032, SYS-REQ-054, SYS-REQ-084
// ObjectEach iterates over the key-value pairs of a JSON object, invoking a given callback for each such entry
func ObjectEach(data []byte, callback func(key []byte, value []byte, dataType ValueType, offset int) error, keys ...string) (err error) {
	return objectEachConfig(DefaultConfig, data, callback, keys...)
}

// SYS-REQ-115
func objectEachConfig(config Config, data []byte, callback func(key []byte, value []byte, dataType ValueType, offset int) error, keys ...string) (err error) {
	offset := 0

	// Descend to the desired key, if requested
	if len(keys) > 0 {
		if off := searchKeysConfig(config, data, keys...); off == -1 {
			return KeyPathNotFoundError
		} else {
			offset = off
		}
	}

	// Validate and skip past opening brace
	if off := nextTokenConfig(config, data[offset:]); off == -1 {
		return MalformedObjectError
	} else if offset += off; data[offset] != '{' {
		return MalformedObjectError
	} else {
		offset++
	}

	// Skip to the first token inside the object, or stop if we find the ending brace
	if off := nextTokenConfig(config, data[offset:]); off == -1 {
		return MalformedJsonError
	} else if offset += off; data[offset] == '}' {
		return nil
	}

	// Loop pre-condition: data[offset] points to what should be either the next entry's key,
	// or the closing brace (if it's anything else, the JSON is malformed).
	// Every iteration either returns or advances offset past a token, so the loop
	// always exits via return; the former `offset < len(data)` guard was structurally
	// always true because internal nextToken/stringEnd calls return errors before
	// offset can reach len(data).
	for {
		// Step 1: find the next key
		var key []byte

		// Check what the the next token is: start of string, end of object, or something else (error)
		switch data[offset] {
		case '"':
			offset++ // accept as string and skip opening quote
		case '\'':
			if !config.AllowSingleQuotes {
				return MalformedObjectError
			}
			offset++ // accept as string and skip opening quote
		case '}':
			return nil // we found the end of the object; stop and return success
		default:
			return MalformedObjectError
		}

		// Find the end of the key string
		var keyEscaped bool
		quote := data[offset-1]
		if off, esc := stringEndConfig(config, data[offset:], quote); off == -1 {
			return MalformedJsonError
		} else {
			key, keyEscaped = data[offset:offset+off-1], esc
			offset += off
		}

		// Unescape the string if needed
		if keyEscaped {
			var stackbuf [unescapeStackBufSize]byte // stack-allocated array for allocation-free unescaping of small strings
			if keyUnescaped, err := unescapeConfig(config, key, stackbuf[:]); err != nil {
				return MalformedStringEscapeError
			} else {
				key = keyUnescaped
			}
		}

		// Step 2: skip the colon
		if off := nextTokenConfig(config, data[offset:]); off == -1 {
			return MalformedJsonError
		} else if offset += off; data[offset] != ':' {
			return MalformedJsonError
		} else {
			offset++
		}

		// Step 3: find the associated value, then invoke the callback
		if value, valueType, off, err := config.Get(data[offset:]); err != nil {
			return err
		} else if err := callback(key, value, valueType, offset+off); err != nil { // Invoke the callback here!
			return err
		} else {
			offset += off
		}

		// Step 4: skip over the next comma to the following token, or stop if we hit the ending brace
		if off := nextTokenConfig(config, data[offset:]); off == -1 {
			return MalformedArrayError
		} else {
			offset += off
			switch data[offset] {
			case '}':
				return nil // Stop if we hit the close brace
			case ',':
				offset++ // Ignore the comma
			default:
				return MalformedObjectError
			}
		}

		// Skip to the next token after the comma
		if off := nextTokenConfig(config, data[offset:]); off == -1 {
			return MalformedArrayError
		} else {
			offset += off
		}
	}

	return MalformedObjectError // we shouldn't get here; it's expected that we will return via finding the ending brace
}

// SYS-REQ-011, SYS-REQ-080, SYS-REQ-081, SYS-REQ-082
// GetUnsafeString returns the value retrieved by `Get`, use creates string without memory allocation by mapping string to slice memory. It does not handle escape symbols.
func GetUnsafeString(data []byte, keys ...string) (val string, err error) {
	v, _, _, e := Get(data, keys...)

	if e != nil {
		return "", e
	}

	return bytesToString(&v), nil
}

// SYS-REQ-002, SYS-REQ-071, SYS-REQ-072, SYS-REQ-073, SYS-REQ-074
// GetString returns the value retrieved by `Get`, cast to a string if possible, trying to properly handle escape and utf8 symbols
// If key data type do not match, it will return an error.
func GetString(data []byte, keys ...string) (val string, err error) {
	v, t, _, e := Get(data, keys...)

	if e != nil {
		return "", e
	}

	if t != String {
		if t == Null {
			return "", NullValueError
		}
		return "", fmt.Errorf("Value is not a string: %s", string(v))
	}

	// If no escapes return raw content
	if bytes.IndexByte(v, '\\') == -1 {
		return string(v), nil
	}

	return ParseString(v)
}

// GetFloat returns the value retrieved by `Get`, cast to a float64 if possible.
// The offset is the same as in `Get`.
// If key data type do not match, it will return an error.
// SYS-REQ-004
func GetFloat(data []byte, keys ...string) (val float64, err error) {
	v, t, _, e := Get(data, keys...)

	if e != nil {
		return 0, e
	}

	if t != Number {
		if t == Null {
			return 0, NullValueError
		}
		return 0, fmt.Errorf("Value is not a number: %s", string(v))
	}

	return ParseFloat(v)
}

// GetInt returns the value retrieved by `Get`, cast to a int64 if possible.
// If key data type do not match, it will return an error.
// SYS-REQ-003, SYS-REQ-075, SYS-REQ-076, SYS-REQ-077, SYS-REQ-078
func GetInt(data []byte, keys ...string) (val int64, err error) {
	v, t, _, e := Get(data, keys...)

	if e != nil {
		return 0, e
	}

	if t != Number {
		if t == Null {
			return 0, NullValueError
		}
		return 0, fmt.Errorf("Value is not a number: %s", string(v))
	}

	return ParseInt(v)
}

// GetBoolean returns the value retrieved by `Get`, cast to a bool if possible.
// The offset is the same as in `Get`.
// If key data type do not match, it will return error.
// SYS-REQ-005, SYS-REQ-079
func GetBoolean(data []byte, keys ...string) (val bool, err error) {
	v, t, _, e := Get(data, keys...)

	if e != nil {
		return false, e
	}

	if t != Boolean {
		if t == Null {
			return false, NullValueError
		}
		return false, fmt.Errorf("Value is not a boolean: %s", string(v))
	}

	return ParseBoolean(v)
}

// ParseBoolean parses a Boolean ValueType into a Go bool (not particularly useful, but here for completeness)
// SYS-REQ-012, SYS-REQ-036, SYS-REQ-057, SYS-REQ-066
func ParseBoolean(b []byte) (bool, error) {
	switch {
	case bytes.Equal(b, trueLiteral):
		return true, nil
	case bytes.Equal(b, falseLiteral):
		return false, nil
	default:
		return false, MalformedValueError
	}
}

// ParseString parses a String ValueType into a Go string (the main parsing work is unescaping the JSON string)
// SYS-REQ-014, SYS-REQ-038, SYS-REQ-060, SYS-REQ-063, SYS-REQ-067
func ParseString(b []byte) (string, error) {
	var stackbuf [unescapeStackBufSize]byte // stack-allocated array for allocation-free unescaping of small strings
	if bU, err := Unescape(b, stackbuf[:]); err != nil {
		return "", MalformedValueError
	} else {
		return string(bU), nil
	}
}

// ParseNumber parses a Number ValueType into a Go float64
// SYS-REQ-013, SYS-REQ-037, SYS-REQ-065
func ParseFloat(b []byte) (float64, error) {
	if v, err := parseFloat(&b); err != nil {
		return 0, MalformedValueError
	} else {
		return v, nil
	}
}

// ParseInt parses a Number ValueType into a Go int64
// SYS-REQ-015, SYS-REQ-039, SYS-REQ-040, SYS-REQ-058, SYS-REQ-059, SYS-REQ-064
func ParseInt(b []byte) (int64, error) {
	if v, ok, overflow := parseInt(b); !ok {
		if overflow {
			return 0, OverflowIntegerError
		}
		return 0, MalformedValueError
	} else {
		return v, nil
	}
}

// GetArrayLen returns the number of elements in the addressed JSON array.
// It scans the array without invoking a callback.
// SYS-REQ-112
func GetArrayLen(data []byte, keys ...string) (int, error) {
	offset, err := containerStart(data, '[', keys...)
	if err != nil {
		return 0, err
	}

	return scanContainerLen(data, offset, '[')
}

// GetObjectLen returns the number of key-value pairs in the addressed JSON
// object. It scans the object without invoking a callback.
// SYS-REQ-112
func GetObjectLen(data []byte, keys ...string) (int, error) {
	offset, err := containerStart(data, '{', keys...)
	if err != nil {
		return 0, err
	}

	return scanContainerLen(data, offset, '{')
}

// GetUint64 returns the value retrieved by internalGet, cast to a uint64 if
// possible. Negative values and malformed integers return MalformedValueError;
// values larger than uint64 return OverflowIntegerError.
// SYS-REQ-003
func GetUint64(data []byte, keys ...string) (uint64, error) {
	v, t, _, _, err := internalGet(data, keys...)
	if err != nil {
		return 0, err
	}

	if t != Number {
		if t == Null {
			return 0, NullValueError
		}
		return 0, fmt.Errorf("Value is not a number: %s", string(v))
	}

	if n, ok, _ := parseInt(v); ok {
		if n < 0 {
			return 0, MalformedValueError
		}
		return uint64(n), nil
	}

	n, parseErr := strconv.ParseUint(string(v), 10, 64)
	if parseErr == nil {
		return n, nil
	}

	var numErr *strconv.NumError
	if errors.As(parseErr, &numErr) && numErr.Err == strconv.ErrRange {
		return 0, OverflowIntegerError
	}
	return 0, MalformedValueError
}

// SYS-REQ-112
func containerStart(data []byte, open byte, keys ...string) (int, error) {
	offset := 0
	if len(keys) > 0 {
		offset = searchKeys(data, keys...)
		if offset == -1 {
			return -1, KeyPathNotFoundError
		}
	}

	tokenOffset := nextToken(data[offset:])
	if tokenOffset == -1 {
		return -1, KeyPathNotFoundError
	}
	offset += tokenOffset

	if data[offset] != open {
		return -1, KeyPathNotFoundError
	}
	return offset, nil
}

// SYS-REQ-112
func scanContainerLen(data []byte, offset int, open byte) (int, error) {
	const (
		scanArrayValue = iota
		scanArrayPrimitive
		scanArrayDelimiter
		scanObjectKey
		scanObjectColon
		scanObjectValue
		scanObjectPrimitive
		scanObjectDelimiter
	)

	close := byte(']')
	malformedErr := MalformedArrayError
	state := scanArrayValue
	if open == '{' {
		close = '}'
		malformedErr = MalformedObjectError
		state = scanObjectKey
	}

	var fixedStack [32]byte
	stack := fixedStack[:1]
	stack[0] = close

	count := 0
	primitiveStart := -1

	for i := offset + 1; i < len(data); i++ {
		c := data[i]

		// Only delimiters at the addressed container's top level affect its
		// length. Strings and nested containers are skipped structurally.
		if len(stack) > 1 {
			switch c {
			case '"':
				stringLength, _ := stringEnd(data[i+1:])
				if stringLength == -1 {
					return 0, malformedErr
				}
				i += stringLength
			case '[':
				stack = append(stack, ']')
			case '{':
				stack = append(stack, '}')
			case ']', '}':
				if c != stack[len(stack)-1] {
					return 0, malformedErr
				}
				stack = stack[:len(stack)-1]
			}
			continue
		}

		switch state {
		case scanArrayValue:
			if isContainerWhitespace(c) {
				continue
			}
			if c == close {
				if count == 0 {
					return 0, nil
				}
				return 0, malformedErr
			}
			if c == ',' || c == '}' || c == ':' {
				return 0, malformedErr
			}

			count++
			switch c {
			case '"':
				stringLength, _ := stringEnd(data[i+1:])
				if stringLength == -1 {
					return 0, malformedErr
				}
				i += stringLength
				state = scanArrayDelimiter
			case '[':
				stack = append(stack, ']')
				state = scanArrayDelimiter
			case '{':
				stack = append(stack, '}')
				state = scanArrayDelimiter
			default:
				primitiveStart = i
				state = scanArrayPrimitive
			}

		case scanArrayPrimitive:
			switch c {
			case ',':
				if !validContainerPrimitive(data[primitiveStart:i]) {
					return 0, malformedErr
				}
				state = scanArrayValue
			case ']':
				if !validContainerPrimitive(data[primitiveStart:i]) {
					return 0, malformedErr
				}
				return count, nil
			case '}':
				return 0, malformedErr
			}

		case scanArrayDelimiter:
			if isContainerWhitespace(c) {
				continue
			}
			switch c {
			case ',':
				state = scanArrayValue
			case ']':
				return count, nil
			default:
				return 0, malformedErr
			}

		case scanObjectKey:
			if isContainerWhitespace(c) {
				continue
			}
			if c == close {
				if count == 0 {
					return 0, nil
				}
				return 0, malformedErr
			}
			if c != '"' {
				return 0, malformedErr
			}

			stringLength, _ := stringEnd(data[i+1:])
			if stringLength == -1 {
				return 0, malformedErr
			}
			i += stringLength
			state = scanObjectColon

		case scanObjectColon:
			if isContainerWhitespace(c) {
				continue
			}
			if c != ':' {
				return 0, malformedErr
			}
			state = scanObjectValue

		case scanObjectValue:
			if isContainerWhitespace(c) {
				continue
			}
			if c == ',' || c == '}' || c == ']' || c == ':' {
				return 0, malformedErr
			}

			count++
			switch c {
			case '"':
				stringLength, _ := stringEnd(data[i+1:])
				if stringLength == -1 {
					return 0, malformedErr
				}
				i += stringLength
				state = scanObjectDelimiter
			case '[':
				stack = append(stack, ']')
				state = scanObjectDelimiter
			case '{':
				stack = append(stack, '}')
				state = scanObjectDelimiter
			default:
				primitiveStart = i
				state = scanObjectPrimitive
			}

		case scanObjectPrimitive:
			switch c {
			case ',':
				if !validContainerPrimitive(data[primitiveStart:i]) {
					return 0, malformedErr
				}
				state = scanObjectKey
			case '}':
				if !validContainerPrimitive(data[primitiveStart:i]) {
					return 0, malformedErr
				}
				return count, nil
			case ']':
				return 0, malformedErr
			}

		case scanObjectDelimiter:
			if isContainerWhitespace(c) {
				continue
			}
			switch c {
			case ',':
				state = scanObjectKey
			case '}':
				return count, nil
			default:
				return 0, malformedErr
			}
		}
	}

	return 0, malformedErr
}

// SYS-REQ-112
func isContainerWhitespace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}

// SYS-REQ-112
func validContainerPrimitive(value []byte) bool {
	for len(value) > 0 && isContainerWhitespace(value[len(value)-1]) {
		value = value[:len(value)-1]
	}
	if len(value) == 0 {
		return false
	}

	if bytes.Equal(value, trueLiteral) || bytes.Equal(value, falseLiteral) || bytes.Equal(value, nullLiteral) {
		return true
	}
	return validJSONNumber(value)
}

// SYS-REQ-112
func validJSONNumber(value []byte) bool {
	i := 0
	if value[i] == '-' {
		i++
		if i == len(value) {
			return false
		}
	}

	if value[i] == '0' {
		i++
		if i < len(value) && value[i] >= '0' && value[i] <= '9' {
			return false
		}
	} else {
		if value[i] < '1' || value[i] > '9' {
			return false
		}
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
	}

	if i < len(value) && value[i] == '.' {
		i++
		start := i
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}

	if i < len(value) && (value[i] == 'e' || value[i] == 'E') {
		i++
		if i < len(value) && (value[i] == '+' || value[i] == '-') {
			i++
		}
		start := i
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}

	return i == len(value)
}
