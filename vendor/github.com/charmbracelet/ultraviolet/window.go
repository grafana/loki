package uv

import (
	"github.com/charmbracelet/x/ansi"
)

// Window represents a rectangular area on the screen. It can be a root window
// with no parent, or a sub-window with a parent window. A window can have its
// own buffer or share the buffer of its parent window (view).
type Window struct {
	*Buffer

	method *WidthMethod
	parent *Window
	bounds Rectangle
}

var (
	_ Screen   = (*Window)(nil)
	_ Drawable = (*Window)(nil)
)

// HasParent returns whether the window has a parent window. This can be used
// to determine if the window is a root window or a sub-window.
func (w *Window) HasParent() bool {
	return w.parent != nil
}

// Parent returns the parent window of the current window.
// If the window does not have a parent, it returns nil.
func (w *Window) Parent() *Window {
	return w.parent
}

// MoveTo moves the window to the specified x and y coordinates.
func (w *Window) MoveTo(x, y int) {
	size := w.bounds.Size()
	w.bounds.Min.X = x
	w.bounds.Min.Y = y
	w.bounds.Max.X = x + size.X
	w.bounds.Max.Y = y + size.Y
}

// MoveBy moves the window by the specified delta x and delta y.
func (w *Window) MoveBy(dx, dy int) {
	w.bounds.Min.X += dx
	w.bounds.Min.Y += dy
	w.bounds.Max.X += dx
	w.bounds.Max.Y += dy
}

// Clone creates an exact copy of the window, including its buffer and values.
// The cloned window will have the same parent and method as the original
// window.
func (w *Window) Clone() *Window {
	return w.CloneArea(w.Buffer.Bounds())
}

// CloneArea creates an exact copy of the window, including its buffer and
// values, but only within the specified area. The cloned window will have the
// same parent and method as the original window, but its bounds will be
// limited to the specified area.
func (w *Window) CloneArea(area Rectangle) *Window {
	clone := new(Window)
	clone.Buffer = w.Buffer.CloneArea(area)
	clone.parent = w.parent
	clone.method = w.method
	clone.bounds = area
	return clone
}

// Resize resizes the window to the specified width and height.
func (w *Window) Resize(width, height int) {
	// Only resize the buffer if this window owns its buffer.
	if w.parent == nil || w.Buffer != w.parent.Buffer {
		w.Buffer.Resize(width, height)
	}
	w.bounds.Max.X = w.bounds.Min.X + width
	w.bounds.Max.Y = w.bounds.Min.Y + height
}

// WidthMethod returns the method used to calculate the width of characters in
// the window.
func (w *Window) WidthMethod() WidthMethod {
	return *w.method
}

// Bounds returns the bounds of the window as a rectangle.
func (w *Window) Bounds() Rectangle {
	return w.bounds
}

// NewWindow creates a new window with its own buffer relative to the parent
// window at the specified position and size.
//
// This will panic if width or height is negative.
func (w *Window) NewWindow(x, y, width, height int) *Window {
	return newWindow(w, x, y, width, height, w.method, false)
}

// NewView creates a new view into the parent window at the specified position
// and size. Unlike [Window.NewWindow], this view shares the same buffer as the
// parent window.
func (w *Window) NewView(x, y, width, height int) *Window {
	return newWindow(w, x, y, width, height, w.method, true)
}

// NewScreen creates a new root [Window] with the given size and width method.
//
// This will panic if width or height is negative.
func NewScreen(width, height int) *Window {
	var method WidthMethod = ansi.WcWidth
	return newWindow(nil, 0, 0, width, height, &method, false)
}

// SetWidthMethod sets the width method for the window.
func (w *Window) SetWidthMethod(method WidthMethod) {
	w.method = &method
}

// newWindow creates a new [Window] with the specified parent, position,
// method, and size.
func newWindow(parent *Window, x, y, width, height int, method *WidthMethod, view bool) *Window {
	w := new(Window)
	if view {
		w.Buffer = parent.Buffer
	} else {
		w.Buffer = NewBuffer(width, height)
	}
	w.parent = parent
	w.method = method
	w.bounds = Rect(x, y, width, height)
	return w
}
