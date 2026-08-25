// Package uv (Ultraviolet) provides primitives for building terminal user
// interfaces in Go.
//
// It offers cell-based screen buffers, a diffing terminal renderer, cross-platform
// input decoding (keyboard, mouse, paste, focus, resize), and a windowing model
// with off-screen buffers. Ultraviolet powers Bubble Tea v2 and Lip Gloss v2.
//
// # Getting Started
//
// Create a terminal, get its screen, and run an event loop:
//
//	t := uv.DefaultTerminal()
//	scr := t.Screen()
//	scr.EnterAltScreen()
//
//	if err := t.Start(); err != nil {
//	    log.Fatal(err)
//	}
//	defer t.Stop()
//
//	for ev := range t.Events() {
//	    switch ev := ev.(type) {
//	    case uv.WindowSizeEvent:
//	        scr.Resize(ev.Width, ev.Height)
//	    case uv.KeyPressEvent:
//	        if ev.MatchString("q") { return }
//	    }
//	}
//
// # Rendering
//
// Drawing is a two-step process. [TerminalScreen.Render] diffs the screen and
// writes escape sequences to an internal buffer. [TerminalScreen.Flush] sends
// the buffer to the terminal—this is the only method that performs real I/O.
//
//	scr.Render()
//	scr.Flush()
//
// Use the screen package for high-level drawing helpers like
// [screen.Context.DrawString] and [screen.Clear].
//
// # Packages
//
//   - screen — drawing context and screen manipulation helpers
//   - layout — constraint-based layout solver (Cassowary algorithm)
package uv
