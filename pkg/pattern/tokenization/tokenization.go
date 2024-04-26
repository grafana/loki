package tokenization

import (
	"bytes"
	"slices"
	"unsafe"
)

var (
	placeholderNumber    = []byte("<NUM>")
	placeholderHex       = []byte("<HEX>")
	placeholderUUID      = []byte("<UUID>")
	placeholderTimestamp = []byte("<TIMESTAMP>")
	placeholderDuration  = []byte("<DURATION>")
	placeholderBytesize  = []byte("<BYTESIZE>")
	placeholderIP        = []byte("<IP>")
)

// Integer numbers after these words won't be replaced by `<NUM>`.
var numPreservingKeys = [][]byte{[]byte("status"), []byte("status_code"), []byte("httpStatus")}

const (
	placeholderEndOfLine = "<...>"
	maxYear              = 2100 // anything above that is unlikely to be a timestamp...
	minHexLen            = 12   // 48 bits min for free-standing hex strings (e.g "0123456789ab") or 42 bits for "0xABCDEF1234" strings
)

var boundryChars = [256]bool{}

func init() {
	for i := 0; i < 128; i++ { // for now, we don't consider non-ASCII chars as boundary
		if i < '0' || (i > '9' && i < 'A') || (i > 'Z' && i < 'a') || i > 'z' {
			boundryChars[i] = true
		}
	}

	// We need these keys sorted in an ascending length to efficiently compare them.
	slices.SortStableFunc(numPreservingKeys, func(a, b []byte) int {
		return len(a) - len(b)
	})
}

type replacer struct {
	source, dest []byte

	// This is the last position that was copied to the destination buffer.
	tail int
	// This is the current position we are working with
	cur int
	// This is essentially used for lookahed operations
	head int

	// Here's some ASCII art to visualize that (the long string of dashes
	// visualizes the log line:
	//
	// 0 <already in dest, copied or replaced >   `tail`   <about to be copied or replaced>   `cur`   <lookahead>   `head`   <remaining unprocessed log>   `len(source)`
	// |---------------------------------------------|------------------------------------------|---------------------|------------------------------------------|

	// A somewhat hacky solution that allows us to preserve specific numeric
	// values we care about, like status and status_code.
	preserveNextNumbers bool
}

// commit advances the current position marker to the lookahead position, i.e.
// we are committing to consume everything we've looked ahead so far.
func (r *replacer) commit() {
	r.cur = r.head
}

func (r *replacer) resetHead() {
	r.head = r.cur
}

func (r *replacer) resetHeadExpr() bool {
	r.head = r.cur
	return true // useful when included in boolean expressions with advance()
}

func (r *replacer) backtrack() {
	r.head--
}

func (r *replacer) consumeUpToCurrent() {
	r.tail = r.cur
}

// advanceUintRet returns the actual value of the number we read, as well as its
// length. The value might overflow for large numbers, so it's also important to
// check the length!
func (r *replacer) advanceUintRet() (val uint, length uint) {
	var c byte
	for ; r.head < len(r.source); r.head++ {
		c = r.source[r.head]
		if c < '0' || '9' < c {
			break
		}
		length++
		val = val*10 + uint(c-'0') // TODO: use bitwise trick?
	}

	return val, length
}

func (r *replacer) advanceUintOrRangeRet(lc, hc byte) (length uint) {
	var c byte
	for ; r.head < len(r.source); r.head++ {
		c = r.source[r.head]
		if !(('0' <= c && c <= '9') || (lc <= c && c <= hc)) {
			break
		}
		length++
	}

	return length
}

func (r *replacer) advanceUint() bool {
	_, l := r.advanceUintRet()
	return l > 0
}

func (r *replacer) advanceUintUpTo(val uint, length uint) bool {
	foundVal, foundLength := r.advanceUintRet()
	return foundLength > 0 && foundVal <= val && foundLength <= length
}

func (r *replacer) advanceYear() bool {
	return r.advanceUintUpTo(maxYear, 4)
}

func (r *replacer) advanceChar(c byte) bool {
	if r.head < len(r.source) && r.source[r.head] == c {
		r.head++
		return true
	}
	return false
}

func (r *replacer) peekNextIsBoundary() bool {
	if r.head >= len(r.source) {
		return true // we are at the end of the line
	}
	return boundryChars[r.source[r.head]]
}

func (r *replacer) peekFirstNonInt() (c byte) {
	for i := r.head; i < len(r.source); i++ {
		c = r.source[i]
		if c < '0' || '9' < c {
			return c
		}
	}
	return 0 // we can return the 0 value here!
}

func (r *replacer) peekNext4() (result [4]byte) {
	overhead := len(r.source) - r.head
	switch {
	case overhead > 3:
		result[3] = r.source[r.head+3]
		fallthrough
	case overhead > 2:
		result[2] = r.source[r.head+2]
		fallthrough
	case overhead > 1:
		result[1] = r.source[r.head+1]
		fallthrough
	case overhead > 0:
		result[0] = r.source[r.head+0]
	}
	return result
}

func (r *replacer) advanceTimeZoneLetters() bool {
	UCLetters := 0
	for {
		nc, ok := r.advance()
		if !ok {
			break
		}
		if nc < 'A' || nc > 'Z' {
			r.backtrack()
			break
		}
		UCLetters++
	}

	return UCLetters >= 2 && UCLetters <= 5
}

func (r *replacer) advanceNumericTimeZone() bool {
	// See https://en.wikipedia.org/wiki/List_of_tz_database_time_zones
	return r.advanceOneOf('+', '-') && r.advanceUintUpTo(14000, 5)
}

// helper for handleWeirdTimestamp()
func (r *replacer) advanceStringOrNumericTimeZone(isOptional bool) bool {
	curHead := r.head
	if r.advanceChar(' ') && r.advanceNumericTimeZone() {
		return true
	}
	r.head = curHead
	if r.advanceChar(' ') && r.advanceTimeZoneLetters() {
		return true
	}
	r.head = curHead
	return isOptional
}

func (r *replacer) advanceOneOf(chars ...byte) bool {
	if r.head >= len(r.source) {
		return false
	}
	c := r.source[r.head]

	for _, ec := range chars {
		if c == ec {
			r.head++
			return true
		}
	}
	return false
}

func (r *replacer) advanceTime(secondsOptional bool) bool {
	return r.advanceUintUpTo(24, 2) && r.advanceChar(':') && r.advanceUintUpTo(60, 2) && (secondsOptional || (r.advanceChar(':') && r.advanceUintUpTo(60, 2)))
}

// Mon|Tue|Wed|Thu|Fri|Sat|Sun
func (r *replacer) advanceDayOfTheWeek() bool {
	c1, ok1 := r.advance()
	c2, ok2 := r.advance()
	c3, ok3 := r.advance()
	if !ok1 || !ok2 || !ok3 {
		return false
	}
	return (c1 == 'S' && ((c2 == 'a' && c3 == 't') || (c2 == 'u' && c3 == 'n'))) || // Sat, Sun
		(c1 == 'M' && c2 == 'o' && c3 == 'n') ||
		(c1 == 'T' && c2 == 'u' && c3 == 'e') ||
		(c1 == 'W' && c2 == 'e' && c3 == 'd') ||
		(c1 == 'T' && c2 == 'h' && c3 == 'u') ||
		(c1 == 'F' && c2 == 'r' && c3 == 'i')
}

// Jan|Feb|Mar|Apr|May|Jul|Jun|Aug|Sep|Oct|Nov|Dec
func (r *replacer) advanceMonthName() bool {
	c1, ok1 := r.advance()
	c2, ok2 := r.advance()
	c3, ok3 := r.advance()
	if !ok1 || !ok2 || !ok3 {
		return false
	}

	return (c1 == 'J' && ((c2 == 'u' && (c3 == 'n' || c3 == 'l')) || // Jun, Jul
		(c2 == 'a' && c3 == 'n'))) || // Jan
		(c1 == 'M' && c2 == 'a' && (c3 == 'r' || c3 == 'y')) || // Mar, May
		(c2 == 'e' && ((c1 == 'F' && c3 == 'b') || (c1 == 'S' && c3 == 'p') || (c1 == 'D' && c3 == 'c'))) || // Feb, Sep, Dec
		(c1 == 'A' && ((c2 == 'p' && c3 == 'r') || // Apr
			(c2 == 'u' && c3 == 'g'))) || // Aug
		(c1 == 'O' && c2 == 'c' && c3 == 't') || // Oct
		(c1 == 'N' && c2 == 'o' && c3 == 'v') // Nov
}

// Check if we in the middle of an UUID, exactly after the first 8 characters
// and the dash after them, e.g after "abcd0123-":
func (r *replacer) advanceUUIDAfterFirstDash(lc, hc byte) (endsWithBoundary bool) {
	return (r.advanceUintOrRangeRet(lc, hc) == 4) && r.advanceChar('-') &&
		(r.advanceUintOrRangeRet(lc, hc) == 4) && r.advanceChar('-') &&
		(r.advanceUintOrRangeRet(lc, hc) == 4) && r.advanceChar('-') &&
		(r.advanceUintOrRangeRet(lc, hc) == 12) && r.peekNextIsBoundary()
}

// Only moves the head forward if it successfully matches a duration
func (r *replacer) advanceDuration() (matched bool) {
	curHead := r.head

	var secsLen int
	n := r.peekNext4()
	if n[0] == 'h' {
		r.head++
		if boundryChars[n[1]] {
			return true // e.g. "1h", "123h"
		}
		if !r.advanceUintUpTo(60, 2) {
			goto restore
		}
		n = r.peekNext4()
	}
	if n[0] == 'm' && (boundryChars[n[1]] || n[1] != 's') { // we don't want to match 'ms' here
		r.head++
		if boundryChars[n[1]] {
			return true // e.g. "2h21m", "121m"
		}
		if !(r.advanceUintUpTo(60, 2) && ((r.advanceChar('.') && r.advanceUint()) || true)) {
			goto restore
		}
		n = r.peekNext4()
	}

	if n[0] == 's' && boundryChars[n[1]] {
		secsLen = 1
	} else if n[1] == 's' && (n[0] == 'm' || n[0] == 'n' || n[0] == 'u') && boundryChars[n[2]] {
		secsLen = 2
	} else if n[2] == 's' && ((n[0] == 0xC2 && n[1] == 0xB5) || (n[0] == 0xCE && n[1] == 0xBC)) && boundryChars[n[3]] {
		// This checks for the unicode "µs" (U+00B5 = micro symbol) and "μs" (U+03BC = Greek letter mu)
		secsLen = 3
	} else {
		goto restore
	}

	r.head += secsLen
	return true

restore: // should be faster than a defer
	r.head = curHead
	return false
}

var byteSizes = [256]bool{'k': true, 'K': true, 'm': true, 'M': true, 'g': true, 'G': true, 't': true, 'T': true}

// Only moves the head forward if it successfully matches a duration
func (r *replacer) advanceBytesize(c1 byte) (matched bool) {
	if !byteSizes[c1] {
		return false
	}
	n := r.peekNext4()

	var unitLen int // not counting the first character c1, which is already advanced to
	if (n[0] == 'b' || n[0] == 'B') && boundryChars[n[1]] {
		unitLen = 1
	} else if n[0] == 'i' && (n[1] == 'b' || n[1] == 'B') && boundryChars[n[2]] {
		unitLen = 2
	} else if ((n[0] == 'b' && n[1] == 'p' && n[2] == 's') || (n[0] == 'b' && n[1] == 'i' && n[2] == 't')) && boundryChars[n[3]] {
		unitLen = 3
	}

	if unitLen > 0 {
		r.head += unitLen
		return true
	}
	return false
}

func (r *replacer) advance() (c byte, advanced bool) {
	if r.head >= len(r.source) {
		return 0, false
	}

	c = r.source[r.head]
	r.head++
	return c, true
}

func (r *replacer) emitNumber() {
	r.commit()
	r.dest = append(r.dest, placeholderNumber...)
	r.consumeUpToCurrent()
}

func (r *replacer) emitNumberOrCopyText(hasMinusPrefix bool) {
	r.commit()
	if !r.preserveNextNumbers {
		r.dest = append(r.dest, placeholderNumber...)
		r.consumeUpToCurrent()
	} else {
		r.maybeEmitDash(hasMinusPrefix)
		r.copyUpToCurrent()
	}
}

func (r *replacer) emit(hasMinusPrefix bool, placeholder []byte) {
	r.commit()
	r.maybeEmitDash(hasMinusPrefix)
	r.dest = append(r.dest, placeholder...)
	r.consumeUpToCurrent()
}

func (r *replacer) maybeEmitDash(hasMinusPrefix bool) {
	// This minus was actually a dash, so we just copy it to the result
	if hasMinusPrefix {
		r.dest = append(r.dest, '-')
	}
}

func (r *replacer) copyUpToCurrent() {
	r.dest = append(r.dest, r.source[r.tail:r.cur]...)
	r.consumeUpToCurrent()
}

func (r *replacer) handleHexOrUnit(hasMinusPrefix bool, n1, l1 uint, c1 byte) (endsWithBoundary bool) {
	// Special case that is likely a hex string of the format "0x", but we don't
	// know whether the rest is upper case or lower case yet.
	zeroHex := false
	if n1 == 0 && l1 == 1 && c1 == 'x' {
		zeroHex = true // these cannot be the start of an UUID
		c1 = r.peekFirstNonInt()
	}

	// Maybe we are at the start of a hex string, either something like
	// "[0-9]+[a-f]", "[0-9]+[A-F]", or "0x". We support both lower and upper
	// case letters, but to avoid false positives, we want hex replacements to
	// happen only if the strings are fully lower case or fully upper case.
	if 'a' <= c1 && c1 <= 'f' {
		return r.handleHex(hasMinusPrefix, l1+1, 'a', 'f', !zeroHex)
	} else if 'A' <= c1 && c1 <= 'F' {
		return r.handleHex(hasMinusPrefix, l1+1, 'A', 'F', !zeroHex)
	} else if zeroHex {
		// Well, it probably wasn't a zero-hex after all, or it contained only
		// digits, so try to handle that or emit what we absorbed
		_, l2 := r.advanceUintRet()
		if l2+2 >= minHexLen { // We consider "0x" to be part of the hex string when comparing with minHexLen
			r.emit(hasMinusPrefix, placeholderHex)
		} else {
			r.maybeEmitDash(hasMinusPrefix)
			r.commit()
			r.copyUpToCurrent()
		}
		return r.peekNextIsBoundary()
	}

	return r.handlePotentialUnitWithDecimal(hasMinusPrefix, c1)
}

func (r *replacer) handleHex(hasMinusPrefix bool, l1 uint, lc, hc byte, canBeUUID bool) (endsWithBoundary bool) {
	totalLen := l1 + r.advanceUintOrRangeRet(lc, hc)
	r.commit()

	if totalLen == 8 && canBeUUID {
		// We might be at the first part of a UUID, right before the first dash
		if r.advanceChar('-') && r.advanceUUIDAfterFirstDash(lc, hc) {
			r.emit(hasMinusPrefix, placeholderUUID)
			return true
		}
		r.resetHead()
	}

	if totalLen >= minHexLen && r.peekNextIsBoundary() {
		r.emit(hasMinusPrefix, placeholderHex)
		return true
	}

	r.copyUpToCurrent()
	return r.peekNextIsBoundary()
}

func (r *replacer) handlePotentialUnitWithDecimal(hasMinusPrefix bool, c1 byte) (endsWithBoundary bool) {
	if r.advanceBytesize(c1) {
		// We do not subsume a minus sign - byte sizes are unlikely to be
		// negative, it's more likely this is a dash as a part of a range
		r.emit(hasMinusPrefix, placeholderBytesize)
		return true
	}

	r.backtrack()
	if r.advanceDuration() {
		// We subsume hasMinusPrefix, since durations can be negative
		r.emit(false, placeholderDuration)
		return true
	}

	// We couldn't match anything, so just copy what existed.
	r.maybeEmitDash(hasMinusPrefix)
	r.copyUpToCurrent()
	return false
}

func (r *replacer) handleNumberWithDecimal(hasMinusPrefix bool, n1 uint, l1 uint) (endsWithBoundary bool) {
	n2, l2 := r.advanceUintRet()
	if l2 == 0 {
		// The dot wasn't followed by another number, so emit everything before it.
		r.backtrack()
		r.emitNumberOrCopyText(hasMinusPrefix)
		return false
	}

	// See if the number after the decimal is also followed by a boundary
	b2, hasNext := r.advance()

	// We are at the end of the string, which is a boundary, replace evertything
	// up to now with a number
	if !hasNext {
		r.emitNumber() // this also subsumes any minus sign we had
		return true
	}

	// The decimal number isn't followed by a boundary char (which include
	// things like '.', ':', '/', etc.), so the part after the decimal is either
	// not a real number, or it's some sort of a unit that can support decimals
	// like size (e.g. 12KB, 3MiB) or durations (e.g. 3.5124s), etc.
	if !boundryChars[b2] {
		return r.handlePotentialUnitWithDecimal(hasMinusPrefix, b2)
	}

	// We have a decimal number followed by a non-dot boundary, so this is not
	// an IP or a version number or anything like that.
	if b2 != '.' {
		r.backtrack()
		r.emitNumber()
		return true
	}

	n3, l3 := r.advanceUintRet()
	if l3 == 0 || !r.peekNextIsBoundary() {
		// The second dot wasn't followed by another number and a boundary, so
		// emit just the first number.
		r.resetHead()
		r.emitNumber()
		return true
	}

	// We have "<NUM>.<NUM>.<NUM>" at this point, so we either have something
	// like a version number, or an IP address, but certainly not a simple
	// decimal number we can just emit.
	r.commit()

	// Check if this is an IP address...
	if n1 <= 255 && l1 <= 3 && n2 <= 255 && l2 <= 3 && n3 <= 255 && l3 <= 3 && r.advanceChar('.') && r.advanceUintUpTo(255, 3) && r.peekNextIsBoundary() {
		r.emit(hasMinusPrefix, placeholderIP)
		return true
	}

	// This wasn't an IP after all, so just emit "<NUM>.<NUM>.<NUM>". We don't
	// want to assume this is a simple decimal number because of the second dot,
	// e.g. this might be something like a version number.
	r.resetHead()
	r.maybeEmitDash(hasMinusPrefix) // we preserve the dashes here, since it's likely not a minus
	r.dest = append(r.dest, placeholderNumber...)
	r.dest = append(r.dest, '.')
	r.dest = append(r.dest, placeholderNumber...)
	r.dest = append(r.dest, '.')
	r.dest = append(r.dest, placeholderNumber...)
	r.consumeUpToCurrent()
	return true
}

func (r *replacer) handleSaneTimestamp(hasMinusPrefix bool, n1 uint, delim byte) (endsWithBoundary bool) {
	if r.advanceUintUpTo(12, 2) && r.advanceChar(delim) && r.advanceUintUpTo(31, 2) && r.advanceOneOf('T', ' ') && r.advanceTime(false) {
		r.commit()
		// continue down to parsing sub-second and timezone values
	} else if r.resetHeadExpr() && n1 <= 31 && r.advanceChar(delim) && r.advanceMonthName() && r.advanceChar(delim) && r.advanceYear() && r.advanceChar(':') && r.advanceTime(false) && r.advanceChar(' ') && r.advanceNumericTimeZone() && r.peekNextIsBoundary() {
		// We might not have a sane timestamp, but apparently a not-so-sane
		// timestamp format first, e.g. "27/Mar/2024:14:34:37 +0000"...
		r.emit(hasMinusPrefix, placeholderTimestamp)
		return true
	} else {
		// Apparently that wasn't it either, we probably just have a dash or
		// forward slash after a number, so we backtrack and emit the number.
		r.resetHead()
		r.emitNumberOrCopyText(hasMinusPrefix)
		return true
	}

	// We have a date that looks like `YY[YY]-MM-DD hh:mm:ss` or
	// `YY[YY]/MM/DDZhh:mm:ss`
	r.commit() // we want to keep this

	// Now we need to also check for sub-second precision and time zones:
	c, canAdvance := r.advance()
	if !canAdvance {
		// End of the string
		r.emit(hasMinusPrefix, placeholderTimestamp)
		return true
	}
	if c == '.' {
		_, intl := r.advanceUintRet()
		if intl == 0 {
			// No sub-second precision, the dot was not part of the timestamp
			r.backtrack()
			r.emit(hasMinusPrefix, placeholderTimestamp)
			return true
		}

		// We had sub-second precision, capture that too
		r.commit()

		// Get the next char to see if we have time zone
		c, canAdvance = r.advance()
		if !canAdvance {
			// We are at the end of the sting after we captured the
			// sub-second value.
			r.emit(hasMinusPrefix, placeholderTimestamp)
			return true
		}
	}

	if c == 'Z' || c == 'z' {
		//UTC string, nothing to do after that
		r.emit(hasMinusPrefix, placeholderTimestamp)
		return true
	}
	// See https://en.wikipedia.org/wiki/List_of_tz_database_time_zones
	if (c == '+' || c == '-') && r.advanceUintUpTo(14, 2) && r.advanceChar(':') && r.advanceUintUpTo(60, 2) {
		// e.g. "2020-09-30T00:00:59.9999+03:00"
		r.commit()
	} else if r.resetHeadExpr() && r.advanceChar(' ') && r.advanceNumericTimeZone() && r.advanceChar(' ') && r.advanceTimeZoneLetters() && r.peekNextIsBoundary() {
		// e.g. "2023-09-05 23:20:28.030285153 +0000 UTC"
		r.commit()
	} else {
		r.resetHead()
	}
	r.emit(hasMinusPrefix, placeholderTimestamp)
	return true
}

func (r *replacer) handleNumberStart(hasMinusPrefix bool) (endsWithBoundary bool) {
	r.resetHead() // keep the head pos in sync with the current pos
	// We know we were at a digit due to how handleNumber() is called
	n1, l1 := r.advanceUintRet()
	r.commit() // we will consume this one way or another for sure

	// See if the number is followed by a boundary
	b1, hasNext := r.advance()

	switch {
	// We are at the end of the string, which is a boundary, replace evertything
	// up to now with a number
	case !hasNext:
		r.emitNumberOrCopyText(hasMinusPrefix) // this also may subsume any minus sign we had
		return true

	// The number isn't followed by a boundary char (which include things like
	// '.', ' ', '/', etc.), so it's either not a real number or date, or it's
	// some sort of a unit like a duration (e.g. 3h5m2s), size, (e.g. 12KB,
	// 3MiB), etc.
	case !boundryChars[b1]:
		return r.handleHexOrUnit(hasMinusPrefix, n1, l1, b1)

	// We have a decimal point, so this can either be a plain number, a unit
	// that can have a float value, or an IP address.
	case b1 == '.':
		return r.handleNumberWithDecimal(hasMinusPrefix, n1, l1)

	// This might be a timestamp that looks like '2024-04-01...' or
	// `2024/03/27...`; timestamps in the far future are not supported :)
	case n1 <= maxYear && l1 <= 4 && (b1 == '-' || b1 == '/'):
		return r.handleSaneTimestamp(hasMinusPrefix, n1, b1)

	// Weird RFC822 dates like "02 Jan 06 15:04 MST"
	case n1 <= 31 && l1 <= 2 && b1 == ' ':
		if r.advanceMonthName() && r.advanceChar(' ') && r.advanceYear() && r.advanceChar(' ') && r.advanceTime(true) && r.advanceStringOrNumericTimeZone(false) {
			r.commit()
			r.emit(hasMinusPrefix, placeholderTimestamp)
			return true
		}
		// if not, go to default handler after switch statement

	// It could be a UUID that starts with 8 digits
	case l1 == 8 && b1 == '-':
		if r.advanceUUIDAfterFirstDash('a', 'f') || (r.resetHeadExpr() && r.advanceChar('-') && r.advanceUUIDAfterFirstDash('A', 'F')) {
			r.emit(hasMinusPrefix, placeholderUUID)
			return true
		}
		// if not, go to default handler after switch statement
	}

	// Number with an unknown boundary - emit the number and leave the boundary
	// for the following passes.
	r.resetHead()
	r.emitNumberOrCopyText(hasMinusPrefix)
	return true
}

var longDayNames = [...][]byte{
	[]byte("Sunday"),
	[]byte("Monday"),
	[]byte("Tuesday"),
	[]byte("Wednesday"),
	[]byte("Thursday"),
	[]byte("Friday"),
	[]byte("Saturday"),
}

func (r *replacer) handleWeirdTimestamp() (endsWithBoundary bool) {
	r.resetHead()

	if r.advanceDayOfTheWeek() {
		r.commit() // we will always consume this

		// RFC1123 and RFC1123Z, e.g.:
		// - "Mon, 02 Jan 2006 15:04:05 MST"
		// - "Mon, 02 Jan 2006 15:04:05 -0700"
		if r.advanceChar(',') && r.advanceChar(' ') && r.advanceUintUpTo(31, 2) && r.advanceChar(' ') && r.advanceMonthName() && r.advanceChar(' ') && r.advanceYear() && r.advanceChar(' ') && r.advanceTime(false) && r.advanceStringOrNumericTimeZone(false) {
			r.emit(false, placeholderTimestamp)
			return true
		}
		r.resetHead()

		// ANSIC, UnixDatem, RubyDate e.g
		// - "Mon Jan  2 15:04:05 2006"
		// - "Mon Jan  2 15:04:05 MST 2006"
		// - "Mon Jan 02 15:04:05 -0700 2006"
		if r.advanceChar(' ') && r.advanceMonthName() && r.advanceChar(' ') && (r.advanceChar(' ') || true) && r.advanceUintUpTo(31, 2) && r.advanceChar(' ') && r.advanceTime(false) && r.advanceStringOrNumericTimeZone(true) && r.advanceChar(' ') && r.advanceYear() {
			r.emit(false, placeholderTimestamp)
			return true
		}
		r.resetHead()

		// Linux, e.g.
		// - "Mon  2 Jan 15:04:05 MST 2006"
		// - "Tue 23 Jan 15:04:05 -0700 2023"
		if r.advanceChar(' ') && (r.advanceChar(' ') || true) && r.advanceUintUpTo(31, 2) && r.advanceChar(' ') && r.advanceMonthName() && r.advanceChar(' ') && r.advanceTime(false) && r.advanceStringOrNumericTimeZone(false) && r.advanceChar(' ') && r.advanceYear() {
			r.emit(false, placeholderTimestamp)
			return true
		}
		r.resetHead()

		// RFC850, e.g.
		//  - "Monday, 02-Jan-06 15:04:05 MST"
		backtrackedSlice := r.source[r.head-3:]
		var matchedDay []byte
		for _, dw := range longDayNames {
			if bytes.HasPrefix(backtrackedSlice, dw) {
				matchedDay = dw
				break
			}
		}
		if matchedDay != nil {
			r.head += len(matchedDay) - 3
			if r.advanceChar(',') && r.advanceChar(' ') && r.advanceUintUpTo(31, 2) && r.advanceChar('-') && r.advanceMonthName() && r.advanceChar('-') && r.advanceUintUpTo(99, 2) && r.advanceChar(' ') && r.advanceTime(false) && r.advanceStringOrNumericTimeZone(true) {
				r.emit(false, placeholderTimestamp)
				return true
			}
		}

		r.cur -= 3 // unconsume
		r.resetHead()
		return false
	}
	r.resetHead()

	if r.advanceMonthName() {
		r.commit() // provisionally consume this

		// Linux journald logs and others similar like this:
		//  - Feb 29 23:00:14
		//  - Apr-10 23:43:46.807
		//  - Jul  1 00:21:28
		if (r.advanceChar('-') || (r.advanceChar(' ') && (r.advanceChar(' ') || true))) && r.advanceUintUpTo(31, 2) && r.advanceChar(' ') && r.advanceTime(false) {
			r.commit()
			// This is already a timestamp, but let's try to match subseconds as well
			if r.advanceChar('.') && r.advanceUint() {
				r.commit()
			} else {
				r.resetHead()
			}
			r.emit(false, placeholderTimestamp)
			return true
		}
		r.cur -= 3 // unconsume
		r.resetHead()
		return false
	}
	r.resetHead()

	return false
}

func (r *replacer) wasNumPreservingKeyword() bool {
	for _, key := range numPreservingKeys {
		pPos := r.cur - 1 - len(key)
		if pPos < -1 {
			return false // all subsequent keys are longer
		}
		if pPos != -1 && !boundryChars[r.source[pPos]] {
			continue
		}
		if bytes.HasPrefix(r.source[pPos+1:], key) {
			return true
		}
	}
	return false
}

func (r *replacer) replaceWithPlaceholders() {
	lineLen := len(r.source)

	var c byte
	onBoundary := true
	for r.cur = 0; r.cur < lineLen; r.cur++ {
		c = r.source[r.cur]
		switch {
		// If we're currently not at a boundary, the only thing we need to check
		// is whether the current char would create a boundary in the next iteration.
		case !onBoundary:
			onBoundary = boundryChars[c]

			// We weren't at a boundary and now we are, so check if we match one
			// of the special keywords that will preserve numbers
			if onBoundary {
				r.preserveNextNumbers = r.wasNumPreservingKeyword()
			}

		// If we've reached this far, it means we're currently at a boundary!

		// A lot of very complex logic if we encounter a number at a boundary,
		// so we move that to a helper function.
		case '0' <= c && c <= '9':
			r.copyUpToCurrent()
			onBoundary = r.handleNumberStart(false)

		// Handle negative numbers, potentially
		case c == '-':
			next := r.cur + 1
			// This might be a number, a date, an IP address, etc. So we don't
			// know if this is a minus sign to replace or a dash to copy yet.
			if next < lineLen && '0' <= r.source[next] && r.source[next] <= '9' {
				// Copy everything before the dash, but mark it as consumed.
				r.copyUpToCurrent()
				r.cur++
				r.consumeUpToCurrent()
				onBoundary = r.handleNumberStart(true)
			} else {
				onBoundary = true
			}

		// Try to match weird timestamps. They require a lot of remaining
		// length, generally start with a capitalized day of the week or month
		// name (1 upper case letter followed by 2 lower case letters).
		//
		// We are basically looking for something that may match this here:
		//   Mon|Tue|Wed|Thu|Fri|Sat|Sun|Jan|Feb|Mar|Apr|May|Jul|Jun|Aug|Sep|Oct|Nov|Dec
		//
		// The detailed check would be performed by the actual handler:
		case 'A' <= c && c <= 'W' && lineLen-r.cur >= 14 &&
			'a' <= r.source[r.cur+1] && r.source[r.cur+1] <= 'u' &&
			'b' <= r.source[r.cur+2] && r.source[r.cur+2] <= 'y':
			r.copyUpToCurrent()
			onBoundary = r.handleWeirdTimestamp()

		// This could be the start of an lower case hex string:
		case 'a' <= c && c <= 'f':
			r.copyUpToCurrent()
			r.resetHead()
			onBoundary = r.handleHex(false, 0, 'a', 'f', true)

		// This could be the start of an upper case hex string:
		case 'A' <= c && c <= 'F':
			r.copyUpToCurrent()
			r.resetHead()
			onBoundary = r.handleHex(false, 0, 'A', 'F', true)

		// If we haven't actually matched anything, update whether we're still
		// on a boundary character and continue onto the next one.
		default:
			onBoundary = boundryChars[c]
		}
	}

	if r.cur > r.tail {
		r.dest = append(r.dest, r.source[r.tail:]...)
		r.consumeUpToCurrent()
	}
}

type tokenizer struct {
	// Input
	rawLine   []byte
	maxTokens int

	// State and position iterators
	buf        []byte
	tpos       int
	tokenCount int
	maybeJSON  bool

	// Result values, the values in the `tokens` slice reference line and shouldn't
	// allocate new memory.
	line   string
	tokens []string
}

// Outside of quoted strings, these are the delimiters between tokens. However,
// they are not going to be a part of the tokens themselves and will be replaced
// by spaces in the actual log template.
var delimiters = [256]bool{0: true, '\t': true, '\n': true, '\v': true, '\f': true, '\r': true, ' ': true}

func (t *tokenizer) countOrSaveToken(endTokenPos, skip int) {
	if t.tokens != nil {
		// Intentionally written like this and not with append(), so this can
		// panic if we ever exceed the preallocated slice size, since that means
		// we have a nasty bug in handleNextToken() below.
		t.tokens[t.tokenCount] = t.line[t.tpos:endTokenPos]
	}
	t.tokenCount++
	t.tpos = endTokenPos + skip
}

func (t *tokenizer) handleNextToken() bool {
	escaped := false
	var c, curQuoteChar byte
	curQuotePos := -1

	lineLen := len(t.line)
	for p := t.tpos; p < lineLen; p++ {
		c = t.line[p]
		switch {

		// If the previous character was a backslash, we ignore the next
		// character, unless it was an non-token delimiter (space, tab, etc.)
		// outside of a quoted string.
		case escaped:
			if curQuotePos < 0 && delimiters[c] {
				t.countOrSaveToken(p, 1)
				return true
			} else {
				escaped = false
			}

		// If we weren't already escaped and we encounter a backslash, toggle
		// the escaped flag and ignore the current byte.
		case c == '\\':
			escaped = true

		// Non-ASCII / UTF8 / invalid character, consider it a part of the
		// current token, for lack of a better efficient option...
		case c > 127:
			// Intentionally blank, continues to the next char

		// If we are currently in a quoted part of the string, the current
		// character is also part of the current token. The only special case
		// here is if the current character is a matching quote, that means
		// we'll no longer be quoted.
		case curQuotePos >= 0:
			if c == curQuoteChar { // end quote
				curQuotePos = -1
			}

		// If we encounter a qoute character and we were not already in a quoted
		// part of the line, mark that we are now in a quote from that type.
		case c == '"' || c == '\'' || c == '`':
			curQuoteChar = c
			curQuotePos = p

		// If we encounter a delimiter outside of a quote, count or save the
		// token and skip the delimiter.
		case delimiters[c]:
			t.countOrSaveToken(p, 1)
			return true

		// Handle likely JSON object keys that have been serialized without
		// spaces. For example, something like this:
		//   `{"context":{"taskId":1},"message":"starting",...}`
		//
		// If the line starts or ends with curly braces, we consider it might be
		// a JSON log and try to detect the `":` part of the message that isn't
		// followed by a delimiter character. If we find that pattern, we
		// consider everything up to and including the `:` character as a
		// separate token.
		//
		// Similarly, we try to detect the `,"` pattern and also split a token
		// before the comma value. The `p > t.tpos` check is crucial here,
		// because it ensures that we're not at the start of a token, i.e. there
		// wasn't a delimiter right before the comma.
		case t.maybeJSON && p > t.tpos && (c == ':' || c == ',') && p+1 < lineLen:
			if c == ':' && t.line[p-1] == '"' && !delimiters[t.line[p+1]] {
				t.countOrSaveToken(p+1, 0)
				return true
			}
			if c == ',' && t.line[p+1] == '"' {
				t.countOrSaveToken(p, 0)
				return true
			}
		}

		// By default we do nothing, simply advance one character forward
		// because the current character was a part of the current token.
	}

	// We have an unterminated single quote at position `curQuotePos`. To handle
	// this edge case somewhat gracefully, we can emit everything up to that
	// unterminated quote and the quote itself as a single token, and continue
	// fairly normally from there.
	if curQuotePos > 0 {
		t.countOrSaveToken(curQuotePos+1, 0)
		return true
	}

	if t.tpos < len(t.line) {
		t.countOrSaveToken(len(t.line), 0)
		return true
	}

	return false
}

// This function is called twice! The first time it counts the tokens but
// doesn't save them. Afterwards we allocate the tokens return slice with
// exactly the correct capacity and we call it again, this time to save them.
func (t *tokenizer) process() {
	// We want to handle the end of the string as a single token, so we start
	// the loop from 1.
	for i := 1; i < t.maxTokens; i++ {
		if !t.handleNextToken() {
			break
		}
	}

	if t.tpos >= len(t.line) {
		return
	}

	// We have token count more than or equal to maxTokens, add one last token
	// containing placeholderEndOfLine to signal that.
	if t.tokens != nil {
		t.tokens[t.tokenCount] = placeholderEndOfLine
	}
	t.tokenCount++
	t.tpos += len(placeholderEndOfLine)
}

func (t *tokenizer) tokenize() []string {
	t.buf = Preprocess(t.rawLine)

	// We use unsafe to convert buf to a string without any new allocations.
	// This is safe because t.buf won't be used or referenced anywhere else
	// besides here from now on.
	t.line = unsafe.String(unsafe.SliceData(t.buf), len(t.buf))

	if len(t.line) >= 2 && (t.line[0] == '{' || t.line[len(t.line)-1] == '}') {
		t.maybeJSON = true
	}

	t.process()

	// If we have super long lines (more than twice the size we need to get the
	// maxTokens we want), copy just the part we need so the tokens don't hold a
	// reference to the original huge []byte slice.
	if t.tokenCount == t.maxTokens && t.tpos*2 < len(t.line) {
		t.line = string(t.buf[0 : t.tpos+1])
	}

	t.tokens = make([]string, t.tokenCount) // intentionally like this, see comment in countOrSaveToken()
	t.tokenCount = 0
	t.tpos = 0
	t.process()

	return t.tokens
}

func Preprocess(content []byte) []byte {
	// ~floor(120%), to allow for some expansion from replacements, hopefully
	// without needing to allocate more memory
	r := replacer{source: content, dest: make([]byte, 0, len(content)*120/100)}
	r.replaceWithPlaceholders()
	return r.dest
}

func PreprocessAndTokenize(content []byte) []string {
	content = bytes.TrimSpace(content)

	t := tokenizer{rawLine: content, maxTokens: 100} // TODO: parametrize maxTokens

	return t.tokenize()
}
