package uv

import (
	"bytes"
	"image/color"
	"io"
	"log"
	"os"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// TerminalScreen represents a terminal screen, providing methods for managing
// the screen state and rendering content.
type TerminalScreen struct {
	win     *Window
	w       io.Writer
	buf     *bytes.Buffer
	rend    *TerminalRenderer
	rbuf    *RenderBuffer
	env     Environ
	profile colorprofile.Profile

	// Terminal state
	altScreen            bool
	keyboardEnhancements *KeyboardEnhancements
	bracketedPaste       bool
	mouseMode            MouseMode
	cursor               *Cursor // initial state is cursor hidden
	backgroundColor      color.Color
	foregroundColor      color.Color
	progressBar          *ProgressBar
	windowTitle          string
}

var _ Screen = (*TerminalScreen)(nil)

// NewTerminalScreen creates a new [TerminalScreen] with the given writer and environment.
func NewTerminalScreen(w io.Writer, env Environ) *TerminalScreen {
	s := &TerminalScreen{}
	s.buf = &bytes.Buffer{}
	s.win = NewScreen(0, 0)
	s.w = w
	s.profile = colorprofile.Detect(w, env)
	s.rend = NewTerminalRenderer(s.buf, env)
	s.rend.SetFullscreen(false)    // by default, we start in inline mode
	s.rend.SetRelativeCursor(true) // by default, we start in inline mode
	s.rend.SetColorProfile(s.profile)
	s.rbuf = NewRenderBuffer(0, 0)
	s.env = env

	if f, err := os.OpenFile("uv_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		log.SetOutput(f)
		s.rend.SetLogger(log.Default())
	}

	// Configure renderer optimizations based on console settings.
	f, ok := w.(term.File)
	if ok {
		state, err := term.GetState(f.Fd())
		if err == nil {
			useTabs, useBspace := optimizeMovements(state)
			if useTabs {
				s.rend.SetTabStops(0) // the width will be set after calling [TerminalScreen.Resize]
			}
			s.rend.SetBackspace(useBspace)
		}
	}
	// XXX: Do we still need map nl to crlf handling in the renderer?
	s.rend.SetMapNewline(false)
	return s
}

// CellAt returns the cell at the specified x and y coordinates.
func (s *TerminalScreen) CellAt(x, y int) *Cell {
	return s.win.CellAt(x, y)
}

// SetCell sets the cell at the specified x and y coordinates.
func (s *TerminalScreen) SetCell(x, y int, cell *Cell) {
	s.win.SetCell(x, y, cell)
}

// Bounds returns the bounds of the terminal screen as a rectangle.
func (s *TerminalScreen) Bounds() Rectangle {
	return s.win.Bounds()
}

// WidthMethod returns the width method used by the terminal screen.
func (s *TerminalScreen) WidthMethod() WidthMethod {
	return s.win.WidthMethod()
}

// SetWidthMethod sets the width method for the terminal screen.
func (s *TerminalScreen) SetWidthMethod(method ansi.Method) {
	s.win.SetWidthMethod(method)
}

// SetColorProfile sets the color profile for the terminal screen.
// This is automatically detected when creating the terminal screen. However,
// you can override it using this method.
func (s *TerminalScreen) SetColorProfile(profile colorprofile.Profile) {
	s.profile = profile
	s.rend.SetColorProfile(profile)
}

// Resize resizes the terminal screen to the specified width and height,
// updating the render buffer and renderer accordingly.
func (s *TerminalScreen) Resize(width, height int) error {
	s.win.Resize(width, height)
	s.rbuf.Resize(width, height)
	s.rend.Resize(width, height)
	s.rend.Erase()
	s.rbuf.Touched = nil
	return nil
}

// Display clears the screen and draws the given [Drawable] onto the terminal
// screen and flushes the changes to the underlying writer.
//
// This is a convenience method that combines [TerminalScreen.Render] and
// [TerminalScreen.Flush].
func (s *TerminalScreen) Display(d Drawable) error {
	if d != nil {
		s.win.Clear()
		d.Draw(s, s.win.Bounds())
	}
	if err := s.Render(); err != nil {
		return err
	}
	return s.Flush()
}

// Render renders changes that transform the screen from its current state to
// the state represented by the [TerminalScreen].
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) Render() error {
	for y := 0; y < s.win.Height(); y++ {
		for x := 0; x < s.win.Width(); {
			cell := s.win.CellAt(x, y)
			if cell == nil || cell.IsZero() {
				x++
				continue
			}
			s.rbuf.SetCell(x, y, cell)
			width := cell.Width
			if width <= 0 {
				width = 1
			}
			x += width
		}
	}
	s.rend.Render(s.rbuf)
	return s.rend.Flush()
}

// Flush writes any pending output to the underlying writer.
func (s *TerminalScreen) Flush() error {
	if s.cursor != nil && !s.cursor.Hidden && s.cursor.X >= 0 && s.cursor.Y >= 0 {
		s.rend.MoveTo(s.cursor.X, s.cursor.Y)
	} else if !s.altScreen {
		// We don't want the cursor to be dangling at the end of the line in
		// inline mode because it can cause unwanted line wraps in some
		// terminals. So we move it to the beginning of the next line if
		// necessary.
		// This is only needed when the cursor is hidden because when it's
		// visible, we already set its position above.
		x, y := s.rend.Position()
		if x >= s.win.Width()-1 {
			s.rend.MoveTo(0, y)
		}
	}

	_, err := s.w.Write(s.buf.Bytes())
	if err != nil {
		return err
	}
	s.buf.Reset()
	return nil
}

// EnterAltScreen switches the terminal to the alternate screen buffer, allowing
// applications to use a separate screen for their output without affecting the
// main screen.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) EnterAltScreen() error {
	var sb strings.Builder
	sb.WriteString(ansi.SetModeAltScreenSaveCursor)
	if s.cursor == nil || s.cursor.Hidden {
		sb.WriteString(ansi.HideCursor)
	} else if s.cursor != nil && !s.cursor.Hidden {
		sb.WriteString(ansi.ShowCursor)
	}
	if s.keyboardEnhancements != nil {
		_ = EncodeKeyboardEnhancements(&sb, s.keyboardEnhancements)
	}
	_, err := s.buf.WriteString(sb.String())
	if err != nil {
		return err
	}

	if !s.altScreen {
		s.rend.SaveCursor()
		s.rend.Erase()
		s.rend.SetFullscreen(true)
		s.rend.SetRelativeCursor(false)
		s.altScreen = true
	}

	return nil
}

// ExitAltScreen switches the terminal back to the main screen buffer, restoring
// the previous screen state.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) ExitAltScreen() error {
	var sb strings.Builder
	sb.WriteString(ansi.ResetModeAltScreenSaveCursor)
	if s.cursor == nil || s.cursor.Hidden {
		sb.WriteString(ansi.HideCursor)
	} else if s.cursor != nil && !s.cursor.Hidden {
		sb.WriteString(ansi.ShowCursor)
	}
	if s.keyboardEnhancements != nil {
		_ = EncodeKeyboardEnhancements(&sb, s.keyboardEnhancements)
	}
	_, err := s.buf.WriteString(sb.String())
	if err != nil {
		return err
	}

	if s.altScreen {
		s.rend.RestoreCursor()
		s.rend.Erase()
		s.rend.SetFullscreen(false)
		s.rend.SetRelativeCursor(true)
		s.altScreen = false
	}

	return nil
}

// AltScreen returns whether the terminal is currently in the alternate screen
// buffer.
func (s *TerminalScreen) AltScreen() bool {
	return s.altScreen
}

// HideCursor hides the terminal cursor.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) HideCursor() error {
	_, err := s.buf.WriteString(ansi.HideCursor)
	if err != nil {
		return err
	}

	if s.cursor != nil {
		s.cursor.Hidden = true
	}

	return nil
}

// ShowCursor shows the terminal cursor.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) ShowCursor() error {
	_, err := s.buf.WriteString(ansi.ShowCursor)
	if err != nil {
		return err
	}

	if s.cursor != nil {
		s.cursor.Hidden = false
	} else {
		s.cursor = NewCursor(-1, -1)
	}

	return nil
}

// CursorVisible returns whether the terminal cursor is currently visible.
func (s *TerminalScreen) CursorVisible() bool {
	return s.cursor != nil && !s.cursor.Hidden
}

// SetCursorPosition sets the position of the terminal cursor to the specified
// coordinates.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) SetCursorPosition(x, y int) error {
	if s.cursor == nil {
		s.cursor = NewCursor(x, y)
		s.cursor.Hidden = true
	} else {
		s.cursor.X = x
		s.cursor.Y = y
	}
	return nil
}

// CursorPosition returns the last set cursor position of the terminal. If the
// cursor position is not set, it returns (-1, -1).
//
// This can be affected by [TerminalScreen.Render] and
// [TerminalScreen.SetCursorPosition] calls.
func (s *TerminalScreen) CursorPosition() (x, y int) {
	if s.cursor != nil {
		return s.cursor.X, s.cursor.Y
	}
	return -1, -1
}

// SetCursorStyle sets the style of the terminal cursor.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) SetCursorStyle(shape CursorShape, blink bool) error {
	if err := EncodeCursorStyle(s.buf, shape, blink); err != nil {
		return err
	}

	if s.cursor == nil {
		s.cursor = NewCursor(-1, -1)
	}
	s.cursor.Shape = shape
	s.cursor.Blink = blink

	return nil
}

// CursorStyle returns the current style of the terminal cursor.
func (s *TerminalScreen) CursorStyle() (shape CursorShape, blink bool) {
	if s.cursor != nil {
		return s.cursor.Shape, s.cursor.Blink
	}
	return CursorBlock, true
}

// SetCursorColor sets the color of the terminal cursor.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) SetCursorColor(c color.Color) error {
	if err := EncodeCursorColor(s.buf, c); err != nil {
		return err
	}

	if s.cursor == nil {
		s.cursor = NewCursor(-1, -1)
	}
	s.cursor.Color = c

	return nil
}

// CursorColor returns the current color of the terminal cursor.
//
// A nil color indicates that the cursor color is the default terminal cursor
// color.
func (s *TerminalScreen) CursorColor() color.Color {
	if s.cursor != nil {
		return s.cursor.Color
	}
	return nil
}

// SetBackgroundColor sets the background color of the terminal.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) SetBackgroundColor(c color.Color) error {
	if err := EncodeBackgroundColor(s.buf, c); err != nil {
		return err
	}

	s.backgroundColor = c

	return nil
}

// BackgroundColor returns the current background color of the terminal.
//
// A nil color indicates that the background color is the default terminal
// background color.
func (s *TerminalScreen) BackgroundColor() color.Color {
	return s.backgroundColor
}

// SetForegroundColor sets the foreground color of the terminal.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) SetForegroundColor(c color.Color) error {
	if err := EncodeForegroundColor(s.buf, c); err != nil {
		return err
	}

	s.foregroundColor = c

	return nil
}

// ForegroundColor returns the current foreground color of the terminal.
//
// A nil color indicates that the foreground color is the default terminal
// foreground color.
func (s *TerminalScreen) ForegroundColor() color.Color {
	return s.foregroundColor
}

// EnableBracketedPaste enables bracketed paste mode, allowing the terminal to
// distinguish between pasted content and user input.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) EnableBracketedPaste() error {
	_, err := s.buf.WriteString(ansi.SetModeBracketedPaste)
	if err != nil {
		return err
	}

	s.bracketedPaste = true

	return nil
}

// DisableBracketedPaste disables bracketed paste mode.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) DisableBracketedPaste() error {
	_, err := s.buf.WriteString(ansi.ResetModeBracketedPaste)
	if err != nil {
		return err
	}

	s.bracketedPaste = false

	return nil
}

// BracketedPaste returns whether bracketed paste mode is currently enabled.
func (s *TerminalScreen) BracketedPaste() bool {
	return s.bracketedPaste
}

// SetMouseMode sets the mouse mode for the terminal, allowing applications to
// receive mouse events.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) SetMouseMode(mode MouseMode) error {
	if err := EncodeMouseMode(s.buf, mode); err != nil {
		return err
	}

	s.mouseMode = mode

	return nil
}

// MouseMode returns the current mouse mode of the terminal.
func (s *TerminalScreen) MouseMode() MouseMode {
	return s.mouseMode
}

// SetWindowTitle sets the title of the terminal window.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) SetWindowTitle(title string) error {
	if err := EncodeWindowTitle(s.buf, title); err != nil {
		return err
	}

	s.windowTitle = title

	return nil
}

// WindowTitle returns the current title of the terminal window.
func (s *TerminalScreen) WindowTitle() string {
	return s.windowTitle
}

// SetKeyboardEnhancements sets the keyboard enhancements for the terminal,
// allowing applications to receive enhanced keyboard input.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) SetKeyboardEnhancements(enh *KeyboardEnhancements) error {
	if err := EncodeKeyboardEnhancements(s.buf, enh); err != nil {
		return err
	}

	s.keyboardEnhancements = enh

	return nil
}

// KeyboardEnhancements returns the current keyboard enhancements of the terminal.
//
// A nil value indicates that no keyboard enhancements are currently enabled.
func (s *TerminalScreen) KeyboardEnhancements() *KeyboardEnhancements {
	return s.keyboardEnhancements
}

// SetProgressBar sets the progress bar for the terminal, allowing applications
// to display progress information.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) SetProgressBar(pb *ProgressBar) error {
	if err := EncodeProgressBar(s.buf, pb); err != nil {
		return err
	}

	s.progressBar = pb

	return nil
}

// ProgressBar returns the current progress bar of the terminal.
//
// A nil value indicates that no progress bar is currently set.
func (s *TerminalScreen) ProgressBar() *ProgressBar {
	return s.progressBar
}

// Reset resets the terminal screen to its default state, clearing the screen,
// switching back to the main screen buffer if necessary, and resetting all
// terminal settings to their defaults.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) Reset() error {
	var sb strings.Builder

	hasKeyboardEnhancements := s.keyboardEnhancements != nil

	if s.altScreen {
		if hasKeyboardEnhancements {
			sb.WriteString(ansi.KittyKeyboard(0, 1))
		}
		sb.WriteString(ansi.ResetModeAltScreenSaveCursor)
	}
	if hasKeyboardEnhancements {
		sb.WriteString(ansi.KittyKeyboard(0, 1))
	}
	if s.mouseMode != MouseModeNone {
		_ = EncodeMouseMode(&sb, MouseModeNone)
	}

	if s.cursor == nil || !s.cursor.Hidden {
		sb.WriteString(ansi.ShowCursor)
	}
	if s.cursor != nil {
		if s.cursor.Shape != CursorBlock || !s.cursor.Blink {
			sb.WriteString(ansi.SetCursorStyle(0))
		}
		if s.cursor.Color != nil {
			sb.WriteString(ansi.ResetCursorColor)
		}
	}
	if s.backgroundColor != nil {
		sb.WriteString(ansi.ResetBackgroundColor)
	}
	if s.foregroundColor != nil {
		sb.WriteString(ansi.ResetForegroundColor)
	}
	if s.bracketedPaste {
		sb.WriteString(ansi.ResetModeBracketedPaste)
	}
	if s.windowTitle != "" {
		sb.WriteString(ansi.SetWindowTitle(""))
	}
	if s.progressBar != nil && s.progressBar.State != ProgressBarNone {
		sb.WriteString(ansi.ResetProgressBar)
	}

	_, err := s.buf.WriteString(sb.String())
	if err != nil {
		return err
	}

	// Go to the bottom of the screen.
	// We need to go to the bottom of the screen regardless of whether
	// we're in alt screen mode or not to avoid leaving the cursor in the
	// middle in terminals that don't support alt screen mode.
	//
	// This comes after resetting the screen state to ensure that moving the
	// cursor is the last thing we do, preventing any unwanted cursor movements
	// after resetting the screen.
	//
	// Note that both [TerminalScreen.rend] writes to [TerminalScreen.buf].
	s.rend.MoveTo(0, s.win.Height()-1)

	return nil
}

// Restore restores the terminal screen to its previous state, applying any
// previous settings and state that were reset by the [TerminalScreen.Reset] method.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) Restore() error {
	var sb strings.Builder

	if s.altScreen {
		sb.WriteString(ansi.SetModeAltScreenSaveCursor)
	}
	if s.cursor != nil && !s.cursor.Hidden {
		sb.WriteString(ansi.ShowCursor)
	} else {
		// Hide the cursor by default.
		sb.WriteString(ansi.HideCursor)
	}
	if s.keyboardEnhancements != nil {
		if err := EncodeKeyboardEnhancements(&sb, s.keyboardEnhancements); err != nil {
			return err
		}
	}
	if s.mouseMode != MouseModeNone {
		if err := EncodeMouseMode(&sb, s.mouseMode); err != nil {
			return err
		}
	}
	if s.cursor != nil {
		if s.cursor.Shape != CursorBlock || !s.cursor.Blink {
			_ = EncodeCursorStyle(&sb, s.cursor.Shape, s.cursor.Blink)
		}
		if s.cursor.Color != nil {
			if err := EncodeCursorColor(&sb, s.cursor.Color); err != nil {
				return err
			}
		}
	}
	if s.backgroundColor != nil {
		if err := EncodeBackgroundColor(&sb, s.backgroundColor); err != nil {
			return err
		}
	}
	if s.foregroundColor != nil {
		if err := EncodeForegroundColor(&sb, s.foregroundColor); err != nil {
			return err
		}
	}
	if s.bracketedPaste {
		sb.WriteString(ansi.SetModeBracketedPaste)
	}
	if s.windowTitle != "" {
		if err := EncodeWindowTitle(&sb, s.windowTitle); err != nil {
			return err
		}
	}
	if s.progressBar != nil && s.progressBar.State != ProgressBarNone {
		if err := EncodeProgressBar(&sb, s.progressBar); err != nil {
			return err
		}
	}

	_, err := s.buf.WriteString(sb.String())
	if err != nil {
		return err
	}

	// This needs to be called after restoring the screen state and writing to
	// the buffer.
	//
	// [TerminalScreen.Render] will write to [TerminalScreen.buf], so we need
	// to call it after writing the restore commands to the buffer to ensure
	// that the restore commands are included in the render output. This
	// ensures that the screen is properly restored before rendering any
	// changes.
	if err := s.Render(); err != nil {
		return err
	}

	// Cursor position will be restored by the caller after calling
	// [TerminalScreen.Flush].

	return nil
}

// Write writes data to the underlying buffer queuing it for output.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) Write(p []byte) (n int, err error) {
	return s.buf.Write(p)
}

// WriteString writes a string to the underlying buffer queuing it for output.
//
// The changes can be committed to the underlying writer by calling the
// [TerminalScreen.Flush] method.
func (s *TerminalScreen) WriteString(str string) (n int, err error) {
	return s.buf.WriteString(str)
}

// InsertAbove inserts content above the screen pushing the current content
// down.
//
// This is useful for inserting content above the current screen content
// without affecting the current cursor position or screen state.
//
// Note that this won't have any visible effect if the screen is in alt screen
// mode, as the content will be inserted above the alt screen buffer, which is
// not visible. However, if the screen is in inline mode, the content will be
// inserted above and will not be managed by the renderer.
//
// Unlike other methods that modify the screen state, this method writes
// directly to the underlying writer, so there is no need to call
// [TerminalScreen.Flush] after calling this method.
func (s *TerminalScreen) InsertAbove(content string) error {
	if len(content) == 0 {
		return nil
	}

	var sb strings.Builder
	w, h := s.win.Width(), s.win.Height()
	_, y := s.rend.Position()

	// We need to scroll the screen up by the number of lines in the queue.
	sb.WriteByte('\r')
	down := h - y - 1
	if down > 0 {
		sb.WriteString(ansi.CursorDown(down))
	}

	lines := strings.Split(content, "\n")
	offset := len(lines)
	for _, line := range lines {
		lineWidth := s.win.WidthMethod().StringWidth(line)
		if w > 0 && lineWidth > w {
			offset += (lineWidth / w)
		}
	}

	// Scroll the screen up by the offset to make room for the new lines.
	sb.WriteString(strings.Repeat("\n", offset))

	// XXX: Now go to the top of the screen, insert new lines, and write
	// the queued strings. It is important to use [Screen.moveCursor]
	// instead of [Screen.move] because we don't want to perform any checks
	// on the cursor position.
	up := offset + h - 1
	sb.WriteString(ansi.CursorUp(up))
	sb.WriteString(ansi.InsertLine(offset))
	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteString(ansi.EraseLineRight)
		sb.WriteString("\r\n")
	}

	s.rend.SetPosition(0, 0)

	_, err := io.WriteString(s.w, sb.String())
	return err
}
