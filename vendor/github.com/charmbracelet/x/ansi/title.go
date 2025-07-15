package ansi

// SetIconNameWindowTitle returns a sequence for setting the icon name and
// window title.
//
//	OSC 0 ; title ST
//	OSC 0 ; title BEL
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h2-Operating-System-Commands
func SetIconNameWindowTitle(s string) string {
	return "\x1b]0;" + s + "\x07"
}

// SetIconName returns a sequence for setting the icon name.
//
//	OSC 1 ; title ST
//	OSC 1 ; title BEL
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h2-Operating-System-Commands
func SetIconName(s string) string {
	return "\x1b]1;" + s + "\x07"
}

// SetWindowTitle returns a sequence for setting the window title.
//
//	OSC 2 ; title ST
//	OSC 2 ; title BEL
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h2-Operating-System-Commands
func SetWindowTitle(s string) string {
	return "\x1b]2;" + s + "\x07"
}
