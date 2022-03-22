package cursor

const (
	// Show returns ANSI escape sequence to show the cursor
	Show = "\x1b[?25h"
	// Hide hides the terminal cursor.
	Hide = "\x1b[?25l"
	// ClearLine clears the terminal line.
	ClearLine = "\x1b[2K"
)
