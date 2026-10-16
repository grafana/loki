package providers

import (
	"fmt"
	"runtime"
	"strings"
	"unicode"
)

// splitProcessCommand splits process_command into argv with quote support.
// Whitespace outside quotes separates arguments. Double/single quotes group a
// single argument so Windows paths like "C:\Program Files\tool.exe" work.
//
// On Unix, escape rules follow POSIX shlex: outside quotes, '\' escapes the
// next rune; inside double quotes, '\' only escapes '"', '\', '$' and '`';
// backslash-newline is a line continuation (both removed) outside single
// quotes; inside single quotes, all characters are literal.
//
// On Windows, '\' is a path separator and is treated as a literal (except
// '\"' inside double quotes), so unquoted paths from filepath.Join keep their
// backslashes.
func splitProcessCommand(command string) ([]string, error) {
	return splitProcessCommandForOS(command, runtime.GOOS)
}

func splitProcessCommandForOS(command, goos string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("process_command is empty")
	}

	windows := goos == "windows"
	var args []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	// hasToken tracks that a token has started even if it is empty, so quoted
	// empty arguments like `tool "" arg` keep their empty argv element.
	hasToken := false

	flush := func() {
		if hasToken {
			args = append(args, current.String())
			current.Reset()
			hasToken = false
		}
	}

	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inSingle {
			if r == '\'' {
				inSingle = false
			} else {
				current.WriteRune(r)
			}
			continue
		}
		if inDouble {
			if r == '"' {
				inDouble = false
				continue
			}
			if r == '\\' && i+1 < len(runes) {
				next := runes[i+1]
				if windows {
					// On Windows only \" is an escape inside double quotes.
					if next == '"' {
						current.WriteRune(next)
						i++
						continue
					}
				} else if next == '\n' {
					// Backslash-newline is a line continuation: both removed.
					i++
					continue
				} else if next == '"' || next == '\\' || next == '$' || next == '`' {
					current.WriteRune(next)
					i++
					continue
				}
			}
			current.WriteRune(r)
			continue
		}
		// unquoted
		if r == '\\' {
			if windows {
				// Path separator — keep literal.
				hasToken = true
				current.WriteRune(r)
				continue
			}
			if i+1 >= len(runes) {
				return nil, fmt.Errorf("invalid process_command: trailing backslash")
			}
			if runes[i+1] == '\n' {
				// Backslash-newline is a line continuation: both removed.
				i++
				continue
			}
			hasToken = true
			current.WriteRune(runes[i+1])
			i++
			continue
		}
		if r == '\'' {
			inSingle = true
			hasToken = true
			continue
		}
		if r == '"' {
			inDouble = true
			hasToken = true
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		hasToken = true
		current.WriteRune(r)
	}

	if inSingle || inDouble {
		return nil, fmt.Errorf("invalid process_command: unclosed quote")
	}
	flush()
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("process_command is empty")
	}
	return args, nil
}
