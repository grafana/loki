package encoder

// The opcode after the value of a member of omitempty ( OpUnwriteEmpty ) unwrites the member if the value is empty,
// for the semantics of encoding/json/v2, which omit a member by what its value writes. The output is compact, a
// value followed by a comma.

// newUnwriteEmptyCode returns the opcode after the value of a member of omitempty, whose key is key, which
// unwrites the member if the value is empty ( see UnwriteEmptyMember ).
func newUnwriteEmptyCode(ctx *compileContext, key string) *Opcode {
	code := &Opcode{
		Op:         OpUnwriteEmpty,
		Idx:        opcodeOffset(ctx.ptrIndex),
		Key:        key,
		DisplayIdx: ctx.opcodeIndex,
		Indent:     ctx.indent,
	}
	ctx.incOpcodeIndex()
	return code
}

// UnwriteEmptyMember unwrites the member which ends the output b, whose key has keyLen bytes, if its value is
// empty, as the v2 semantics define it: null, "", {} or []. The opcode runs only right after the opcodes of its
// field wrote the member, the key followed by the value and a comma: a field which is omitted goes to NextField,
// past the opcode, so the member starts keyLen bytes before its value.
//
// The value is empty if it is one of them right after the colon of the key: a longer value can end with the bytes
// of one, as "\"" ends with "", but doesn't start there.
func UnwriteEmptyMember(ctx *RuntimeContext, b []byte, keyLen int) []byte {
	end := len(b) - 1 // the comma
	n := 2
	if end < n {
		return b
	}
	switch b[end-1] {
	case '"':
		if b[end-2] != '"' {
			return b
		}
	case '}':
		if b[end-2] != '{' {
			return b
		}
	case ']':
		if b[end-2] != '[' {
			return b
		}
	case 'l':
		n = 4
		if end < n || string(b[end-n:end]) != "null" {
			return b
		}
	default:
		return b
	}
	if end-n-1 < 0 || b[end-n-1] != ':' {
		return b
	}
	start := end - n - keyLen
	if start < 0 {
		return b
	}
	ctx.Rewrote(start)
	return b[:start]
}
