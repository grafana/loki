# Tutorial: Hello World

This tutorial walks through building a simple Ultraviolet application that
displays centered "Hello, World!" text. You'll learn the core concepts:
creating a terminal, managing a screen, handling input events, and rendering.

## Creating a Terminal

A `Terminal` manages the console, input event loop, and screen state.

```go
t := uv.DefaultTerminal()
```

You can also create a terminal with a custom console and options:

```go
con := uv.NewConsole(os.Stdin, os.Stdout, os.Environ())
t := uv.NewTerminal(con, &uv.Options{
    Logger: myLogger, // optional, for debugging I/O
})
```

## Getting the Screen

The terminal's screen is where you draw content, manage the alternate screen
buffer, and set cells.

```go
scr := t.Screen()
```

## Alternate Screen

The alternate screen buffer lets your application display content without
affecting the user's scrollback. Most fullscreen TUIs use it.

```go
scr.EnterAltScreen()
```

## Starting the Terminal

Starting puts the console into raw mode (disabling echoing and line buffering
so we receive individual keypresses like <kbd>ctrl+c</kbd>), initializes the
input event loop, and prepares the screen for rendering.

```go
if err := t.Start(); err != nil {
    log.Fatalf("failed to start terminal: %v", err)
}
defer t.Stop()
```

`Stop()` restores the console, exits the alternate screen, and cleans up. It
is safe to call multiple times and supports suspend/resume cycles.

## Drawing Text

You can set individual cells directly:

```go
for i, r := range "Hello, World!" {
    scr.SetCell(i, 0, &uv.Cell{Content: string(r), Width: 1})
}
```

Or use the `screen` helper package for convenience:

```go
ctx := screen.NewContext(scr)
ctx.DrawString("Hello, World!", 0, 0)
```

The `Context` supports styled text, links, and wrapping. It implements
`io.Writer`, so you can use `fmt.Fprint` and friends.

## Rendering

Drawing to the screen is a two-step process:

1. **Render** — computes the minimal diff between the current and new screen
   state, writing ANSI escape sequences to an internal buffer.
2. **Flush** — writes the buffer to the terminal. This is the only step that
   performs real I/O and can return an error.

```go
scr.Render()
if err := scr.Flush(); err != nil {
    log.Fatalf("flush failed: %v", err)
}
```

## Handling Events

The terminal provides a channel of input events. Range over it to process
keyboard, mouse, and resize events:

```go
for ev := range t.Events() {
    switch ev := ev.(type) {
    case uv.WindowSizeEvent:
        scr.Resize(ev.Width, ev.Height)
    case uv.KeyPressEvent:
        if ev.MatchString("q", "ctrl+c") {
            return
        }
    }
}
```

`MatchString` accepts key names and modifier combinations like `"ctrl+a"`,
`"shift+enter"`, or `"alt+tab"`.

## Putting It Together

Here's the complete program—centered "Hello, World!" that redraws on resize
and exits on `q` or `ctrl+c`:

```go
package main

import (
	"log"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
)

func main() {
	t := uv.DefaultTerminal()
	scr := t.Screen()

	scr.EnterAltScreen()

	if err := t.Start(); err != nil {
		log.Fatalf("failed to start terminal: %v", err)
	}
	defer t.Stop()

	ctx := screen.NewContext(scr)
	text := "Hello, World!"
	textWidth := scr.StringWidth(text)

	display := func() {
		screen.Clear(scr)
		bounds := scr.Bounds()
		x := (bounds.Dx() - textWidth) / 2
		y := bounds.Dy() / 2
		ctx.DrawString(text, x, y)
		scr.Render()
		scr.Flush()
	}

	for ev := range t.Events() {
		switch ev := ev.(type) {
		case uv.WindowSizeEvent:
			scr.Resize(ev.Width, ev.Height)
			display()
		case uv.KeyPressEvent:
			if ev.MatchString("q", "ctrl+c") {
				return
			}
		}
	}
}
```

---

Part of [Charm](https://charm.sh).

<a href="https://charm.sh/"><img alt="The Charm logo" src="https://stuff.charm.sh/charm-badge.jpg" width="400"></a>

Charm热爱开源 • Charm loves open source • نحنُ نحب المصادر المفتوحة
