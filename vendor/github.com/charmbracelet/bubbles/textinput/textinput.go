package textinput

import (
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/runeutil"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	rw "github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// Internal messages for clipboard operations.
type (
	pasteMsg    string
	pasteErrMsg struct{ error }
)

// EchoMode sets the input behavior of the text input field.
type EchoMode int

const (
	// EchoNormal displays text as is. This is the default behavior.
	EchoNormal EchoMode = iota

	// EchoPassword displays the EchoCharacter mask instead of actual
	// characters. This is commonly used for password fields.
	EchoPassword

	// EchoNone displays nothing as characters are entered. This is commonly
	// seen for password fields on the command line.
	EchoNone
)

// ValidateFunc is a function that returns an error if the input is invalid.
type ValidateFunc func(string) error

// KeyMap is the key bindings for different actions within the textinput.
type KeyMap struct {
	CharacterForward        key.Binding
	CharacterBackward       key.Binding
	WordForward             key.Binding
	WordBackward            key.Binding
	DeleteWordBackward      key.Binding
	DeleteWordForward       key.Binding
	DeleteAfterCursor       key.Binding
	DeleteBeforeCursor      key.Binding
	DeleteCharacterBackward key.Binding
	DeleteCharacterForward  key.Binding
	LineStart               key.Binding
	LineEnd                 key.Binding
	Paste                   key.Binding
	AcceptSuggestion        key.Binding
	NextSuggestion          key.Binding
	PrevSuggestion          key.Binding
}

// DefaultKeyMap is the default set of key bindings for navigating and acting
// upon the textinput.
var DefaultKeyMap = KeyMap{
	CharacterForward:        key.NewBinding(key.WithKeys("right", "ctrl+f")),
	CharacterBackward:       key.NewBinding(key.WithKeys("left", "ctrl+b")),
	WordForward:             key.NewBinding(key.WithKeys("alt+right", "ctrl+right", "alt+f")),
	WordBackward:            key.NewBinding(key.WithKeys("alt+left", "ctrl+left", "alt+b")),
	DeleteWordBackward:      key.NewBinding(key.WithKeys("alt+backspace", "ctrl+w")),
	DeleteWordForward:       key.NewBinding(key.WithKeys("alt+delete", "alt+d")),
	DeleteAfterCursor:       key.NewBinding(key.WithKeys("ctrl+k")),
	DeleteBeforeCursor:      key.NewBinding(key.WithKeys("ctrl+u")),
	DeleteCharacterBackward: key.NewBinding(key.WithKeys("backspace", "ctrl+h")),
	DeleteCharacterForward:  key.NewBinding(key.WithKeys("delete", "ctrl+d")),
	LineStart:               key.NewBinding(key.WithKeys("home", "ctrl+a")),
	LineEnd:                 key.NewBinding(key.WithKeys("end", "ctrl+e")),
	Paste:                   key.NewBinding(key.WithKeys("ctrl+v")),
	AcceptSuggestion:        key.NewBinding(key.WithKeys("tab")),
	NextSuggestion:          key.NewBinding(key.WithKeys("down", "ctrl+n")),
	PrevSuggestion:          key.NewBinding(key.WithKeys("up", "ctrl+p")),
}

// Model is the Bubble Tea model for this text input element.
type Model struct {
	Err error

	// General settings.
	Prompt        string
	Placeholder   string
	EchoMode      EchoMode
	EchoCharacter rune
	Cursor        cursor.Model

	// Deprecated: use [cursor.BlinkSpeed] instead.
	BlinkSpeed time.Duration

	// Styles. These will be applied as inline styles.
	//
	// For an introduction to styling with Lip Gloss see:
	// https://github.com/charmbracelet/lipgloss
	PromptStyle      lipgloss.Style
	TextStyle        lipgloss.Style
	PlaceholderStyle lipgloss.Style
	CompletionStyle  lipgloss.Style

	// Deprecated: use Cursor.Style instead.
	CursorStyle lipgloss.Style

	// CharLimit is the maximum amount of characters this input element will
	// accept. If 0 or less, there's no limit.
	CharLimit int

	// Width is the maximum number of characters that can be displayed at once.
	// It essentially treats the text field like a horizontally scrolling
	// viewport. If 0 or less this setting is ignored.
	Width int

	// KeyMap encodes the keybindings recognized by the widget.
	KeyMap KeyMap

	// Underlying text value.
	value []rune

	// focus indicates whether user input focus should be on this input
	// component. When false, ignore keyboard input and hide the cursor.
	focus bool

	// Cursor position.
	pos int

	// Used to emulate a viewport when width is set and the content is
	// overflowing.
	offset      int
	offsetRight int

	// Validate is a function that checks whether or not the text within the
	// input is valid. If it is not valid, the `Err` field will be set to the
	// error returned by the function. If the function is not defined, all
	// input is considered valid.
	Validate ValidateFunc

	// rune sanitizer for input.
	rsan runeutil.Sanitizer

	// Should the input suggest to complete
	ShowSuggestions bool

	// suggestions is a list of suggestions that may be used to complete the
	// input.
	suggestions            [][]rune
	matchedSuggestions     [][]rune
	currentSuggestionIndex int
}

// New creates a new model with default settings.
func New() Model {
	return Model{
		Prompt:           "> ",
		EchoCharacter:    '*',
		CharLimit:        0,
		PlaceholderStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		ShowSuggestions:  false,
		CompletionStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		Cursor:           cursor.New(),
		KeyMap:           DefaultKeyMap,

		suggestions: [][]rune{},
		value:       nil,
		focus:       false,
		pos:         0,
	}
}

// NewModel creates a new model with default settings.
//
// Deprecated: Use [New] instead.
var NewModel = New

// SetValue sets the value of the text input.
func (m *Model) SetValue(s string) {
	// Clean up any special characters in the input provided by the
	// caller. This avoids bugs due to e.g. tab characters and whatnot.
	runes := m.san().Sanitize([]rune(s))
	err := m.validate(runes)
	m.setValueInternal(runes, err)
}

func (m *Model) setValueInternal(runes []rune, err error) {
	m.Err = err

	empty := len(m.value) == 0

	if m.CharLimit > 0 && len(runes) > m.CharLimit {
		m.value = runes[:m.CharLimit]
	} else {
		m.value = runes
	}
	if (m.pos == 0 && empty) || m.pos > len(m.value) {
		m.SetCursor(len(m.value))
	}
	m.handleOverflow()
}

// Value returns the value of the text input.
func (m Model) Value() string {
	return string(m.value)
}

// Position returns the cursor position.
func (m Model) Position() int {
	return m.pos
}

// SetCursor moves the cursor to the given position. If the position is
// out of bounds the cursor will be moved to the start or end accordingly.
func (m *Model) SetCursor(pos int) {
	m.pos = clamp(pos, 0, len(m.value))
	m.handleOverflow()
}

// CursorStart moves the cursor to the start of the input field.
func (m *Model) CursorStart() {
	m.SetCursor(0)
}

// CursorEnd moves the cursor to the end of the input field.
func (m *Model) CursorEnd() {
	m.SetCursor(len(m.value))
}

// Focused returns the focus state on the model.
func (m Model) Focused() bool {
	return m.focus
}

// Focus sets the focus state on the model. When the model is in focus it can
// receive keyboard input and the cursor will be shown.
func (m *Model) Focus() tea.Cmd {
	m.focus = true
	return m.Cursor.Focus()
}

// Blur removes the focus state on the model.  When the model is blurred it can
// not receive keyboard input and the cursor will be hidden.
func (m *Model) Blur() {
	m.focus = false
	m.Cursor.Blur()
}

// Reset sets the input to its default state with no input.
func (m *Model) Reset() {
	m.value = nil
	m.SetCursor(0)
}

// SetSuggestions sets the suggestions for the input.
func (m *Model) SetSuggestions(suggestions []string) {
	m.suggestions = make([][]rune, len(suggestions))
	for i, s := range suggestions {
		m.suggestions[i] = []rune(s)
	}

	m.updateSuggestions()
}

// rsan initializes or retrieves the rune sanitizer.
func (m *Model) san() runeutil.Sanitizer {
	if m.rsan == nil {
		// Textinput has all its input on a single line so collapse
		// newlines/tabs to single spaces.
		m.rsan = runeutil.NewSanitizer(
			runeutil.ReplaceTabs(" "), runeutil.ReplaceNewlines(" "))
	}
	return m.rsan
}

func (m *Model) insertRunesFromUserInput(v []rune) {
	// Clean up any special characters in the input provided by the
	// clipboard. This avoids bugs due to e.g. tab characters and
	// whatnot.
	paste := m.san().Sanitize(v)

	var availSpace int
	if m.CharLimit > 0 {
		availSpace = m.CharLimit - len(m.value)

		// If the char limit's been reached, cancel.
		if availSpace <= 0 {
			return
		}

		// If there's not enough space to paste the whole thing cut the pasted
		// runes down so they'll fit.
		if availSpace < len(paste) {
			paste = paste[:availSpace]
		}
	}

	// Stuff before and after the cursor
	head := m.value[:m.pos]
	tailSrc := m.value[m.pos:]
	tail := make([]rune, len(tailSrc))
	copy(tail, tailSrc)

	// Insert pasted runes
	for _, r := range paste {
		head = append(head, r)
		m.pos++
		if m.CharLimit > 0 {
			availSpace--
			if availSpace <= 0 {
				break
			}
		}
	}

	// Put it all back together
	value := append(head, tail...)
	inputErr := m.validate(value)
	m.setValueInternal(value, inputErr)
}

// If a max width is defined, perform some logic to treat the visible area
// as a horizontally scrolling viewport.
func (m *Model) handleOverflow() {
	if m.Width <= 0 || uniseg.StringWidth(string(m.value)) <= m.Width {
		m.offset = 0
		m.offsetRight = len(m.value)
		return
	}

	// Correct right offset if we've deleted characters
	m.offsetRight = min(m.offsetRight, len(m.value))

	if m.pos < m.offset {
		m.offset = m.pos

		w := 0
		i := 0
		runes := m.value[m.offset:]

		for i < len(runes) && w <= m.Width {
			w += rw.RuneWidth(runes[i])
			if w <= m.Width+1 {
				i++
			}
		}

		m.offsetRight = m.offset + i
	} else if m.pos >= m.offsetRight {
		m.offsetRight = m.pos

		w := 0
		runes := m.value[:m.offsetRight]
		i := len(runes) - 1

		for i > 0 && w < m.Width {
			w += rw.RuneWidth(runes[i])
			if w <= m.Width {
				i--
			}
		}

		m.offset = m.offsetRight - (len(runes) - 1 - i)
	}
}

// deleteBeforeCursor deletes all text before the cursor.
func (m *Model) deleteBeforeCursor() {
	m.value = m.value[m.pos:]
	m.Err = m.validate(m.value)
	m.offset = 0
	m.SetCursor(0)
}

// deleteAfterCursor deletes all text after the cursor. If input is masked
// delete everything after the cursor so as not to reveal word breaks in the
// masked input.
func (m *Model) deleteAfterCursor() {
	m.value = m.value[:m.pos]
	m.Err = m.validate(m.value)
	m.SetCursor(len(m.value))
}

// deleteWordBackward deletes the word left to the cursor.
func (m *Model) deleteWordBackward() {
	if m.pos == 0 || len(m.value) == 0 {
		return
	}

	if m.EchoMode != EchoNormal {
		m.deleteBeforeCursor()
		return
	}

	// Linter note: it's critical that we acquire the initial cursor position
	// here prior to altering it via SetCursor() below. As such, moving this
	// call into the corresponding if clause does not apply here.
	oldPos := m.pos //nolint:ifshort

	m.SetCursor(m.pos - 1)
	for unicode.IsSpace(m.value[m.pos]) {
		if m.pos <= 0 {
			break
		}
		// ignore series of whitespace before cursor
		m.SetCursor(m.pos - 1)
	}

	for m.pos > 0 {
		if !unicode.IsSpace(m.value[m.pos]) {
			m.SetCursor(m.pos - 1)
		} else {
			if m.pos > 0 {
				// keep the previous space
				m.SetCursor(m.pos + 1)
			}
			break
		}
	}

	if oldPos > len(m.value) {
		m.value = m.value[:m.pos]
	} else {
		m.value = append(m.value[:m.pos], m.value[oldPos:]...)
	}
	m.Err = m.validate(m.value)
}

// deleteWordForward deletes the word right to the cursor. If input is masked
// delete everything after the cursor so as not to reveal word breaks in the
// masked input.
func (m *Model) deleteWordForward() {
	if m.pos >= len(m.value) || len(m.value) == 0 {
		return
	}

	if m.EchoMode != EchoNormal {
		m.deleteAfterCursor()
		return
	}

	oldPos := m.pos
	m.SetCursor(m.pos + 1)
	for unicode.IsSpace(m.value[m.pos]) {
		// ignore series of whitespace after cursor
		m.SetCursor(m.pos + 1)

		if m.pos >= len(m.value) {
			break
		}
	}

	for m.pos < len(m.value) {
		if !unicode.IsSpace(m.value[m.pos]) {
			m.SetCursor(m.pos + 1)
		} else {
			break
		}
	}

	if m.pos > len(m.value) {
		m.value = m.value[:oldPos]
	} else {
		m.value = append(m.value[:oldPos], m.value[m.pos:]...)
	}
	m.Err = m.validate(m.value)

	m.SetCursor(oldPos)
}

// wordBackward moves the cursor one word to the left. If input is masked, move
// input to the start so as not to reveal word breaks in the masked input.
func (m *Model) wordBackward() {
	if m.pos == 0 || len(m.value) == 0 {
		return
	}

	if m.EchoMode != EchoNormal {
		m.CursorStart()
		return
	}

	i := m.pos - 1
	for i >= 0 {
		if unicode.IsSpace(m.value[i]) {
			m.SetCursor(m.pos - 1)
			i--
		} else {
			break
		}
	}

	for i >= 0 {
		if !unicode.IsSpace(m.value[i]) {
			m.SetCursor(m.pos - 1)
			i--
		} else {
			break
		}
	}
}

// wordForward moves the cursor one word to the right. If the input is masked,
// move input to the end so as not to reveal word breaks in the masked input.
func (m *Model) wordForward() {
	if m.pos >= len(m.value) || len(m.value) == 0 {
		return
	}

	if m.EchoMode != EchoNormal {
		m.CursorEnd()
		return
	}

	i := m.pos
	for i < len(m.value) {
		if unicode.IsSpace(m.value[i]) {
			m.SetCursor(m.pos + 1)
			i++
		} else {
			break
		}
	}

	for i < len(m.value) {
		if !unicode.IsSpace(m.value[i]) {
			m.SetCursor(m.pos + 1)
			i++
		} else {
			break
		}
	}
}

func (m Model) echoTransform(v string) string {
	switch m.EchoMode {
	case EchoPassword:
		return strings.Repeat(string(m.EchoCharacter), uniseg.StringWidth(v))
	case EchoNone:
		return ""
	case EchoNormal:
		return v
	default:
		return v
	}
}

// Update is the Bubble Tea update loop.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focus {
		return m, nil
	}

	// Need to check for completion before, because key is configurable and might be double assigned
	keyMsg, ok := msg.(tea.KeyMsg)
	if ok && key.Matches(keyMsg, m.KeyMap.AcceptSuggestion) {
		if m.canAcceptSuggestion() {
			m.value = append(m.value, m.matchedSuggestions[m.currentSuggestionIndex][len(m.value):]...)
			m.CursorEnd()
		}
	}

	// Let's remember where the position of the cursor currently is so that if
	// the cursor position changes, we can reset the blink.
	oldPos := m.pos //nolint

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.KeyMap.DeleteWordBackward):
			m.deleteWordBackward()
		case key.Matches(msg, m.KeyMap.DeleteCharacterBackward):
			m.Err = nil
			if len(m.value) > 0 {
				m.value = append(m.value[:max(0, m.pos-1)], m.value[m.pos:]...)
				m.Err = m.validate(m.value)
				if m.pos > 0 {
					m.SetCursor(m.pos - 1)
				}
			}
		case key.Matches(msg, m.KeyMap.WordBackward):
			m.wordBackward()
		case key.Matches(msg, m.KeyMap.CharacterBackward):
			if m.pos > 0 {
				m.SetCursor(m.pos - 1)
			}
		case key.Matches(msg, m.KeyMap.WordForward):
			m.wordForward()
		case key.Matches(msg, m.KeyMap.CharacterForward):
			if m.pos < len(m.value) {
				m.SetCursor(m.pos + 1)
			}
		case key.Matches(msg, m.KeyMap.LineStart):
			m.CursorStart()
		case key.Matches(msg, m.KeyMap.DeleteCharacterForward):
			if len(m.value) > 0 && m.pos < len(m.value) {
				m.value = append(m.value[:m.pos], m.value[m.pos+1:]...)
				m.Err = m.validate(m.value)
			}
		case key.Matches(msg, m.KeyMap.LineEnd):
			m.CursorEnd()
		case key.Matches(msg, m.KeyMap.DeleteAfterCursor):
			m.deleteAfterCursor()
		case key.Matches(msg, m.KeyMap.DeleteBeforeCursor):
			m.deleteBeforeCursor()
		case key.Matches(msg, m.KeyMap.Paste):
			return m, Paste
		case key.Matches(msg, m.KeyMap.DeleteWordForward):
			m.deleteWordForward()
		case key.Matches(msg, m.KeyMap.NextSuggestion):
			m.nextSuggestion()
		case key.Matches(msg, m.KeyMap.PrevSuggestion):
			m.previousSuggestion()
		default:
			// Input one or more regular characters.
			m.insertRunesFromUserInput(msg.Runes)
		}

		// Check again if can be completed
		// because value might be something that does not match the completion prefix
		m.updateSuggestions()

	case pasteMsg:
		m.insertRunesFromUserInput([]rune(msg))

	case pasteErrMsg:
		m.Err = msg
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd

	m.Cursor, cmd = m.Cursor.Update(msg)
	cmds = append(cmds, cmd)

	if oldPos != m.pos && m.Cursor.Mode() == cursor.CursorBlink {
		m.Cursor.Blink = false
		cmds = append(cmds, m.Cursor.BlinkCmd())
	}

	m.handleOverflow()
	return m, tea.Batch(cmds...)
}

// View renders the textinput in its current state.
func (m Model) View() string {
	// Placeholder text
	if len(m.value) == 0 && m.Placeholder != "" {
		return m.placeholderView()
	}

	styleText := m.TextStyle.Inline(true).Render

	value := m.value[m.offset:m.offsetRight]
	pos := max(0, m.pos-m.offset)
	v := styleText(m.echoTransform(string(value[:pos])))

	if pos < len(value) {
		char := m.echoTransform(string(value[pos]))
		m.Cursor.SetChar(char)
		v += m.Cursor.View()                                   // cursor and text under it
		v += styleText(m.echoTransform(string(value[pos+1:]))) // text after cursor
		v += m.completionView(0)                               // suggested completion
	} else {
		if m.canAcceptSuggestion() {
			suggestion := m.matchedSuggestions[m.currentSuggestionIndex]
			if len(value) < len(suggestion) {
				m.Cursor.TextStyle = m.CompletionStyle
				m.Cursor.SetChar(m.echoTransform(string(suggestion[pos])))
				v += m.Cursor.View()
				v += m.completionView(1)
			} else {
				m.Cursor.SetChar(" ")
				v += m.Cursor.View()
			}
		} else {
			m.Cursor.SetChar(" ")
			v += m.Cursor.View()
		}
	}

	// If a max width and background color were set fill the empty spaces with
	// the background color.
	valWidth := uniseg.StringWidth(string(value))
	if m.Width > 0 && valWidth <= m.Width {
		padding := max(0, m.Width-valWidth)
		if valWidth+padding <= m.Width && pos < len(value) {
			padding++
		}
		v += styleText(strings.Repeat(" ", padding))
	}

	return m.PromptStyle.Render(m.Prompt) + v
}

// placeholderView returns the prompt and placeholder view, if any.
func (m Model) placeholderView() string {
	var (
		v     string
		p     = []rune(m.Placeholder)
		style = m.PlaceholderStyle.Inline(true).Render
	)

	m.Cursor.TextStyle = m.PlaceholderStyle
	m.Cursor.SetChar(string(p[:1]))
	v += m.Cursor.View()

	// If the entire placeholder is already set and no padding is needed, finish
	if m.Width < 1 && len(p) <= 1 {
		return m.PromptStyle.Render(m.Prompt) + v
	}

	// If Width is set then size placeholder accordingly
	if m.Width > 0 {
		// available width is width - len + cursor offset of 1
		minWidth := lipgloss.Width(m.Placeholder)
		availWidth := m.Width - minWidth + 1

		// if width < len, 'subtract'(add) number to len and dont add padding
		if availWidth < 0 {
			minWidth += availWidth
			availWidth = 0
		}
		// append placeholder[len] - cursor, append padding
		v += style(string(p[1:minWidth]))
		v += style(strings.Repeat(" ", availWidth))
	} else {
		// if there is no width, the placeholder can be any length
		v += style(string(p[1:]))
	}

	return m.PromptStyle.Render(m.Prompt) + v
}

// Blink is a command used to initialize cursor blinking.
func Blink() tea.Msg {
	return cursor.Blink()
}

// Paste is a command for pasting from the clipboard into the text input.
func Paste() tea.Msg {
	str, err := clipboard.ReadAll()
	if err != nil {
		return pasteErrMsg{err}
	}
	return pasteMsg(str)
}

func clamp(v, low, high int) int {
	if high < low {
		low, high = high, low
	}
	return min(high, max(low, v))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Deprecated.

// Deprecated: use cursor.Mode.
type CursorMode int

const (
	// Deprecated: use cursor.CursorBlink.
	CursorBlink = CursorMode(cursor.CursorBlink)
	// Deprecated: use cursor.CursorStatic.
	CursorStatic = CursorMode(cursor.CursorStatic)
	// Deprecated: use cursor.CursorHide.
	CursorHide = CursorMode(cursor.CursorHide)
)

func (c CursorMode) String() string {
	return cursor.Mode(c).String()
}

// Deprecated: use cursor.Mode().
func (m Model) CursorMode() CursorMode {
	return CursorMode(m.Cursor.Mode())
}

// Deprecated: use cursor.SetMode().
func (m *Model) SetCursorMode(mode CursorMode) tea.Cmd {
	return m.Cursor.SetMode(cursor.Mode(mode))
}

func (m Model) completionView(offset int) string {
	var (
		value = m.value
		style = m.PlaceholderStyle.Inline(true).Render
	)

	if m.canAcceptSuggestion() {
		suggestion := m.matchedSuggestions[m.currentSuggestionIndex]
		if len(value) < len(suggestion) {
			return style(string(suggestion[len(value)+offset:]))
		}
	}
	return ""
}

// AvailableSuggestions returns the list of available suggestions.
func (m *Model) AvailableSuggestions() []string {
	suggestions := make([]string, len(m.suggestions))
	for i, s := range m.suggestions {
		suggestions[i] = string(s)
	}

	return suggestions
}

// CurrentSuggestion returns the currently selected suggestion.
func (m *Model) CurrentSuggestion() string {
	if m.currentSuggestionIndex >= len(m.matchedSuggestions) {
		return ""
	}

	return string(m.matchedSuggestions[m.currentSuggestionIndex])
}

// canAcceptSuggestion returns whether there is an acceptable suggestion to
// autocomplete the current value.
func (m *Model) canAcceptSuggestion() bool {
	return len(m.matchedSuggestions) > 0
}

// updateSuggestions refreshes the list of matching suggestions.
func (m *Model) updateSuggestions() {
	if !m.ShowSuggestions {
		return
	}

	if len(m.value) <= 0 || len(m.suggestions) <= 0 {
		m.matchedSuggestions = [][]rune{}
		return
	}

	matches := [][]rune{}
	for _, s := range m.suggestions {
		suggestion := string(s)

		if strings.HasPrefix(strings.ToLower(suggestion), strings.ToLower(string(m.value))) {
			matches = append(matches, []rune(suggestion))
		}
	}
	if !reflect.DeepEqual(matches, m.matchedSuggestions) {
		m.currentSuggestionIndex = 0
	}

	m.matchedSuggestions = matches
}

// nextSuggestion selects the next suggestion.
func (m *Model) nextSuggestion() {
	m.currentSuggestionIndex = (m.currentSuggestionIndex + 1)
	if m.currentSuggestionIndex >= len(m.matchedSuggestions) {
		m.currentSuggestionIndex = 0
	}
}

// previousSuggestion selects the previous suggestion.
func (m *Model) previousSuggestion() {
	m.currentSuggestionIndex = (m.currentSuggestionIndex - 1)
	if m.currentSuggestionIndex < 0 {
		m.currentSuggestionIndex = len(m.matchedSuggestions) - 1
	}
}

func (m Model) validate(v []rune) error {
	if m.Validate != nil {
		return m.Validate(string(v))
	}
	return nil
}
