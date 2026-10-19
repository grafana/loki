package ansi

import (
	"encoding/base64"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ProgramState is the state of a program reported with the Program Status
// Protocol (OSC 7501).
//
// See: https://www.superlogical.com/rex/docs/build/program-status
type ProgramState string

// Program states.
const (
	// ProgramStateIdle means the program is at rest, waiting for the user's
	// next instruction.
	ProgramStateIdle ProgramState = "idle"
	// ProgramStateWorking means the program is running.
	ProgramStateWorking ProgramState = "working"
	// ProgramStateDone means the program finished a piece of work and the
	// result is ready to look at.
	ProgramStateDone ProgramState = "done"
	// ProgramStateBlocked means the program cannot continue until the user
	// does something.
	ProgramStateBlocked ProgramState = "blocked"
	// ProgramStateError means the program failed and stopped.
	ProgramStateError ProgramState = "error"
	// ProgramStateClear removes the addressed record and every record beneath
	// it. With no id, it removes every record on the terminal.
	ProgramStateClear ProgramState = "clear"
)

// ProgramStatusKind says what a blocked program is waiting for.
type ProgramStatusKind string

// Program status kinds.
const (
	// ProgramStatusKindPermission means the program waits for approval to do
	// something.
	ProgramStatusKindPermission ProgramStatusKind = "permission"
	// ProgramStatusKindQuestion means the user must type an answer.
	ProgramStatusKindQuestion ProgramStatusKind = "question"
	// ProgramStatusKindAuth means the program waits for a login, token, or
	// credential.
	ProgramStatusKindAuth ProgramStatusKind = "auth"
)

// ProgramStatus is a report of the Program Status Protocol (OSC 7501).
//
// Each report replaces its record completely, so fields such as App and Title
// should be included in every report.
//
// See: https://www.superlogical.com/rex/docs/build/program-status
type ProgramStatus struct {
	// State is the program state. Required.
	State ProgramState
	// ID addresses a record. Empty means the root record. It is a "/"
	// separated path of segments matching [A-Za-z0-9_.+-]{1,32}.
	ID string
	// App is a stable machine-readable program name matching
	// [A-Za-z0-9_.+-]{1,32}. Invalid values are omitted.
	App string
	// Kind says what a blocked program waits for. Only used with
	// [ProgramStateBlocked].
	Kind ProgramStatusKind
	// Progress is a percentage between 0 and 100. Only used with
	// [ProgramStateWorking] and [ProgramStateBlocked] when HasProgress is
	// true. Otherwise progress is indeterminate.
	Progress int
	// HasProgress reports whether Progress is set.
	HasProgress bool
	// Title is a short human-readable label for the record.
	Title string
	// Message is one human-readable line describing the record.
	Message string
}

// Program Status Protocol limits on decoded text.
const (
	programStatusMaxTitle   = 192
	programStatusMaxMessage = 2048
)

// ClearProgramStatus is a sequence that removes every program status record
// on the terminal.
//
//	OSC 7501 ; state=clear BEL
//
// See: https://www.superlogical.com/rex/docs/build/program-status
const ClearProgramStatus = "\x1b]7501;state=clear\x07"

// RequestProgramStatusSupport is a sequence that asks the terminal whether it
// supports the Program Status Protocol. A supporting terminal replies with
// OSC 7501 ; ? ST.
//
//	OSC 7501 ; ? BEL
//
// See: https://www.superlogical.com/rex/docs/build/program-status
const RequestProgramStatusSupport = "\x1b]7501;?\x07"

// ClearProgramStatusID returns a sequence that removes the record with the
// given id and every record beneath it. An empty id removes every record. It
// returns an empty string if the id is invalid.
//
//	OSC 7501 ; state=clear:id=Id BEL
//
// See: https://www.superlogical.com/rex/docs/build/program-status
func ClearProgramStatusID(id string) string {
	return SetProgramStatus(ProgramStatus{State: ProgramStateClear, ID: id})
}

// SetProgramStatus returns a sequence that reports a program status using the
// Program Status Protocol (OSC 7501).
//
//	OSC 7501 ; key=value:key=value BEL
//
// Terminals discard a whole report when any part of it is invalid, so the
// report is sanitized: control characters in Title and Message are replaced
// with spaces and the text is truncated to the protocol limits, and an invalid
// App, or a Kind or Progress used with the wrong state, is omitted. It returns
// an empty string if the state is unknown or the id is invalid.
//
// See: https://www.superlogical.com/rex/docs/build/program-status
func SetProgramStatus(s ProgramStatus) string {
	switch s.State {
	case ProgramStateIdle, ProgramStateWorking, ProgramStateDone,
		ProgramStateBlocked, ProgramStateError, ProgramStateClear:
	default:
		return ""
	}
	if s.ID != "" && !validProgramStatusID(s.ID) {
		return ""
	}

	pairs := []string{"state=" + string(s.State)}
	if s.ID != "" {
		pairs = append(pairs, "id="+s.ID)
	}
	if s.State == ProgramStateClear {
		return programStatusSequence(pairs)
	}
	if s.State == ProgramStateBlocked && validProgramStatusKind(s.Kind) {
		pairs = append(pairs, "kind="+string(s.Kind))
	}
	if s.HasProgress && (s.State == ProgramStateWorking || s.State == ProgramStateBlocked) {
		pairs = append(pairs, "progress="+strconv.Itoa(min(max(s.Progress, 0), 100)))
	}
	if validProgramStatusSegment(s.App) {
		pairs = append(pairs, "app="+s.App)
	}
	if t := programStatusText(s.Title, programStatusMaxTitle); t != "" {
		pairs = append(pairs, "title="+t)
	}
	if m := programStatusText(s.Message, programStatusMaxMessage); m != "" {
		pairs = append(pairs, "msg="+m)
	}

	return programStatusSequence(pairs)
}

func programStatusSequence(pairs []string) string {
	return "\x1b]7501;" + strings.Join(pairs, ":") + "\x07"
}

func validProgramStatusKind(k ProgramStatusKind) bool {
	switch k {
	case ProgramStatusKindPermission, ProgramStatusKindQuestion, ProgramStatusKindAuth:
		return true
	}
	return false
}

// programStatusText sanitizes, truncates, and base64 encodes free text.
func programStatusText(s string, limit int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	var sb strings.Builder
	space := false
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			if !space {
				sb.WriteByte(' ')
				space = true
			}
			continue
		}
		space = false
		sb.WriteRune(r)
	}
	s = strings.TrimSpace(sb.String())
	if len(s) > limit {
		s = s[:limit]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		s = strings.TrimSpace(s)
	}
	if s == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func validProgramStatusID(id string) bool {
	if len(id) > 128 {
		return false
	}
	segs := strings.Split(id, "/")
	if len(segs) > 8 {
		return false
	}
	for _, seg := range segs {
		if !validProgramStatusSegment(seg) {
			return false
		}
	}
	return true
}

func validProgramStatusSegment(s string) bool {
	if len(s) < 1 || len(s) > 32 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') &&
			c != '_' && c != '.' && c != '+' && c != '-' {
			return false
		}
	}
	return true
}
