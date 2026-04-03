package uv

import (
	"fmt"
	"image/color"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

var (
	// ErrNotTerminal is an error that indicates that the file is not a terminal.
	ErrNotTerminal = fmt.Errorf("not a terminal")
	// ErrPlatformNotSupported is an error that indicates that the platform is not supported.
	ErrPlatformNotSupported = fmt.Errorf("platform not supported")
)

// Drawable represents a drawable component on a [Screen].
type Drawable interface {
	// Draw renders the component on the screen for the given area.
	Draw(scr Screen, area Rectangle)
}

// DrawableFunc is a function that implements the [Drawable] interface.
type DrawableFunc func(scr Screen, rect Rectangle)

var _ Drawable = (DrawableFunc)(nil)

// Draw implements the [Drawable] interface.
func (f DrawableFunc) Draw(scr Screen, rect Rectangle) {
	f(scr, rect)
}

// WidthMethod determines how many columns a grapheme occupies on the screen.
type WidthMethod interface {
	StringWidth(s string) int
}

// Screen represents a screen that can be drawn to.
type Screen interface {
	// Bounds returns the bounds of the screen. This is the rectangle that
	// includes the start and end points of the screen.
	Bounds() Rectangle

	// CellAt returns the cell at the given position. If the position is out of
	// bounds, it returns nil. Otherwise, it always returns a cell, even if it
	// is empty (i.e., a cell with a space character and a width of 1).
	CellAt(x, y int) *Cell

	// SetCell sets the cell at the given position. A nil cell is treated as an
	// empty cell with a space character and a width of 1.
	SetCell(x, y int, c *Cell)

	// WidthMethod returns the width method used by the screen.
	WidthMethod() WidthMethod
}

// Cursor represents a cursor on the terminal screen.
type Cursor struct {
	// Position is a [Position] that determines the cursor's position on the
	// screen relative to the top left corner of the frame.
	Position

	// Color is a [color.Color] that determines the cursor's color.
	Color color.Color

	// Shape is a [CursorShape] that determines the cursor's shape.
	Shape CursorShape

	// Blink is a boolean that determines whether the cursor should blink.
	Blink bool

	// Hidden is a boolean that determines whether the cursor is hidden. You
	// can use this if you want to hide the cursor but still want to change its
	// position.
	Hidden bool
}

// NewCursor returns a new cursor with the default settings and the given
// position.
func NewCursor(x, y int) *Cursor {
	return &Cursor{
		Position: Position{X: x, Y: y},
		Color:    nil,
		Shape:    CursorBlock,
		Blink:    true,
	}
}

// ProgressBarState represents the state of the progress bar.
type ProgressBarState int

// Progress bar states.
const (
	ProgressBarNone ProgressBarState = iota
	ProgressBarDefault
	ProgressBarError
	ProgressBarIndeterminate
	ProgressBarWarning
)

// String return a human-readable value for the given [ProgressBarState].
func (s ProgressBarState) String() string {
	return [...]string{
		"None",
		"Default",
		"Error",
		"Indeterminate",
		"Warning",
	}[s]
}

// ProgressBar represents the terminal progress bar.
//
// Support depends on the terminal.
//
// See https://learn.microsoft.com/en-us/windows/terminal/tutorials/progress-bar-sequences
type ProgressBar struct {
	// State is the current state of the progress bar. It can be one of
	// [ProgressBarNone], [ProgressBarDefault], [ProgressBarError],
	// [ProgressBarIndeterminate], and [ProgressBarWarning].
	State ProgressBarState
	// Value is the current value of the progress bar. It should be between
	// 0 and 100.
	Value int
}

// NewProgressBar returns a new progress bar with the given state and value.
// The value is ignored if the state is [ProgressBarNone] or
// [ProgressBarIndeterminate].
func NewProgressBar(state ProgressBarState, value int) *ProgressBar {
	return &ProgressBar{
		State: state,
		Value: min(max(value, 0), 100),
	}
}

// KeyboardEnhancements defines different keyboard enhancement features that
// can be requested from the terminal.
type KeyboardEnhancements struct {
	// DisambiguateEscapeCodes requests the terminal to report ambiguous keys
	// such as Ctrl+i and Tab, and Ctrl+m and Enter, and others as distinct key
	// code sequences.
	// If supported, your program will receive distinct [KeyPressEvent]s for
	// these keys.
	DisambiguateEscapeCodes bool

	// ReportEventTypes requests the terminal to report key repeat and release
	// events.
	// If supported, your program will receive [KeyReleaseEvent]s and
	// [KeyPressEvent] with the [Key.IsRepeat] field set indicating that this
	// is a it's part of a key repeat sequence.
	ReportEventTypes bool
}

// NewKeyboardEnhancements returns a new [KeyboardEnhancements] with the given
// options as flags.
//
// A zero, or negative, flags value is treated as no enhancements.
//
// See [ansi.KittyKeyboard] for more details on the supported keyboard enhancements.
func NewKeyboardEnhancements(flags int) *KeyboardEnhancements {
	if flags <= 0 {
		return &KeyboardEnhancements{}
	}

	return &KeyboardEnhancements{
		DisambiguateEscapeCodes: flags&ansi.KittyDisambiguateEscapeCodes != 0,
		ReportEventTypes:        flags&ansi.KittyReportEventTypes != 0,
	}
}

// Flags returns the keyboard enhancements as bits that can be used to set the
// appropriate terminal modes.
func (ke KeyboardEnhancements) Flags() int {
	bits := 0
	if ke.DisambiguateEscapeCodes {
		bits |= ansi.KittyDisambiguateEscapeCodes
	}
	if ke.ReportEventTypes {
		bits |= ansi.KittyReportEventTypes
	}

	return bits
}

// EncodeBackgroundColor encodes the background color to the given writer. Use
// nil to reset the background color to the default.
func EncodeBackgroundColor(w io.Writer, c color.Color) error {
	var seq string
	if c == nil {
		seq = ansi.ResetBackgroundColor
	} else if col, ok := colorful.MakeColor(c); ok {
		seq = ansi.SetBackgroundColor(col.Hex())
	} else {
		return fmt.Errorf("invalid color: %v", c)
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set background color: %w", err)
	}

	return nil
}

// EncodeForegroundColor encodes the foreground color to the given writer. Use
// nil to reset the foreground color to the default.
func EncodeForegroundColor(w io.Writer, c color.Color) error {
	var seq string
	if c == nil {
		seq = ansi.ResetForegroundColor
	} else if col, ok := colorful.MakeColor(c); ok {
		seq = ansi.SetForegroundColor(col.Hex())
	} else {
		return fmt.Errorf("invalid color: %v", c)
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set foreground color: %w", err)
	}

	return nil
}

// EncodeCursorColor encodes the cursor color to the given writer. Use nil to
// reset the cursor color to the default.
func EncodeCursorColor(w io.Writer, c color.Color) error {
	var seq string
	if c == nil {
		seq = ansi.ResetCursorColor
	} else if col, ok := colorful.MakeColor(c); ok {
		seq = ansi.SetCursorColor(col.Hex())
	} else {
		return fmt.Errorf("invalid color: %v", c)
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set cursor color: %w", err)
	}

	return nil
}

// EncodeCursorStyle encodes the cursor style to the given writer.
func EncodeCursorStyle(w io.Writer, shape CursorShape, blink bool) error {
	seq := ansi.SetCursorStyle(shape.Encode(blink))
	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set cursor style: %w", err)
	}

	return nil
}

// EncodeBracketedPaste encodes the bracketed paste mode to the given writer.
func EncodeBracketedPaste(w io.Writer, enable bool) error {
	var seq string
	if enable {
		seq = ansi.SetModeBracketedPaste
	} else {
		seq = ansi.ResetModeBracketedPaste
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set bracketed paste mode: %w", err)
	}

	return nil
}

// EncodeMouseMode encodes the mouse mode to the given writer.
func EncodeMouseMode(w io.Writer, mode MouseMode) error {
	var sb strings.Builder
	switch mode {
	case MouseModeNone:
		sb.WriteString(ansi.ResetModeMouseNormal)
		sb.WriteString(ansi.ResetModeMouseButtonEvent)
		sb.WriteString(ansi.ResetModeMouseAnyEvent)
		sb.WriteString(ansi.ResetModeMouseExtSgr)
	case MouseModeClick:
		sb.WriteString(ansi.SetModeMouseNormal)
		sb.WriteString(ansi.SetModeMouseExtSgr)
	case MouseModeDrag:
		sb.WriteString(ansi.SetModeMouseButtonEvent)
		sb.WriteString(ansi.SetModeMouseExtSgr)
	case MouseModeMotion:
		sb.WriteString(ansi.SetModeMouseAnyEvent)
		sb.WriteString(ansi.SetModeMouseExtSgr)
	default:
		return fmt.Errorf("invalid mouse mode: %d", mode)
	}

	_, err := io.WriteString(w, sb.String())
	if err != nil {
		return fmt.Errorf("failed to set mouse mode: %w", err)
	}

	return nil
}

// EncodeProgressBar encodes the progress bar to the given writer.
func EncodeProgressBar(w io.Writer, pb *ProgressBar) error {
	if pb == nil {
		pb = &ProgressBar{State: ProgressBarNone}
	}

	var seq string
	percent := clamp(pb.Value, 0, 100)
	switch pb.State {
	case ProgressBarNone:
		seq = ansi.ResetProgressBar
	case ProgressBarDefault:
		seq = ansi.SetProgressBar(percent)
	case ProgressBarError:
		seq = ansi.SetErrorProgressBar(percent)
	case ProgressBarIndeterminate:
		seq = ansi.SetIndeterminateProgressBar
	case ProgressBarWarning:
		seq = ansi.SetWarningProgressBar(percent)
	default:
		return fmt.Errorf("invalid progress bar state: %d", pb.State)
	}

	_, err := io.WriteString(w, seq)
	if err != nil {
		return fmt.Errorf("failed to set progress bar: %w", err)
	}

	return nil
}

// EncodeKeyboardEnhancements encodes the keyboard enhancements to the given
// writer.
func EncodeKeyboardEnhancements(w io.Writer, ke *KeyboardEnhancements) error {
	var flags int
	if ke != nil {
		flags = ke.Flags()
	}
	_, err := io.WriteString(w, ansi.KittyKeyboard(flags, 1))
	if err != nil {
		return fmt.Errorf("failed to set keyboard enhancements: %w", err)
	}
	return nil
}

// EncodeWindowTitle encodes the window title to the given writer.
func EncodeWindowTitle(w io.Writer, title string) error {
	_, err := io.WriteString(w, ansi.SetWindowTitle(title))
	if err != nil {
		return fmt.Errorf("failed to set window title: %w", err)
	}
	return nil
}
