# Ultraviolet

<img width="400" alt="Charm Ultraviolet" src="https://github.com/user-attachments/assets/3484e4b0-3741-4e8c-bebf-9ea51f5bb49c" />

<p>
    <a href="https://pkg.go.dev/github.com/charmbracelet/ultraviolet?tab=doc"><img src="https://godoc.org/github.com/charmbracelet/ultraviolet?status.svg" alt="GoDoc"></a>
    <a href="https://github.com/charmbracelet/ultraviolet/actions"><img src="https://github.com/charmbracelet/ultraviolet/actions/workflows/build.yml/badge.svg" alt="Build Status"></a>
</p>

Ultraviolet is a set of primitives for building terminal user interfaces in Go.
It provides cell-based rendering, cross-platform input handling, and a diffing
renderer inspired by [ncurses](https://invisible-island.net/ncurses/)—without
the need for `terminfo` or `termcap` databases.

Ultraviolet powers [Bubble Tea v2][bbt] and [Lip Gloss v2][lg]. It replaces the
ad-hoc terminal primitives from earlier versions with a cohesive, imperative API
that can also be used standalone.

[bbt]: https://github.com/charmbracelet/bubbletea
[lg]: https://github.com/charmbracelet/lipgloss

## Install

```bash
go get github.com/charmbracelet/ultraviolet@latest
```

## Quick Start

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

## Architecture

Ultraviolet is organized as a set of layered primitives:

- **Terminal** — manages the application lifecycle: raw mode, input event loop,
  start/stop. Create one with `DefaultTerminal()` or `NewTerminal(console, opts)`.

- **TerminalScreen** — the screen state manager. Handles rendering, alternate
  screen buffer, cursor, mouse modes, keyboard enhancements, bracketed paste,
  window title, and more. Access it via `terminal.Screen()`.

- **Screen** — a minimal interface (`Bounds`, `CellAt`, `SetCell`,
  `WidthMethod`) implemented by `TerminalScreen`, `Buffer`, `Window`, and
  `ScreenBuffer`. Write code against `Screen` to stay decoupled from the
  terminal.

- **Buffer / Window** — off-screen cell buffers. `Buffer` is a flat grid of
  cells. `Window` adds parent/child relationships and shared-buffer views.
  Both implement `Screen` and `Drawable`.

- **screen package** — drawing helpers that operate on any `Screen`: a
  `Context` for styled text rendering (`Print`, `DrawString`, etc.) and
  utility functions like `Clear`, `Fill`, `Clone`.

- **layout package** — a constraint-based layout solver built on the
  [Cassowary algorithm][casso]. Partition screen space with `Len`, `Min`,
  `Max`, `Percent`, `Ratio`, and `Fill` constraints.

[casso]: https://en.wikipedia.org/wiki/Cassowary_(software)

## Features

- **Cell-based diffing renderer** — only redraws what changed. Optimizes
  cursor movement, uses ECH/REP/ICH/DCH when available, and supports scroll
  optimizations. Minimal bandwidth, critical for SSH.

- **Universal input** — unified keyboard and mouse event handling across
  platforms. Supports legacy encodings, Kitty keyboard protocol, SGR mouse,
  and Windows Console input.

- **Inline and fullscreen** — works in both alternate screen (fullscreen) and
  inline mode. Inline TUIs preserve terminal context and scrollback.

- **Cross-platform** — first-class support for Unix (termios + ANSI) and
  Windows (Console API). Consistent behavior across terminal emulators.

- **Suspend/resume** — `Stop()` and `Start()` can be called repeatedly for
  suspend/resume cycles, shelling out to editors, or process suspension
  via `uv.Suspend()`.

## Examples

See the [`examples/`](./examples/) directory for core examples and
[`examples/advanced/`](./examples/advanced/) for more complex demos.

## Tutorial

See [TUTORIAL.md](./TUTORIAL.md) for a step-by-step guide to building your
first Ultraviolet application.

> [!NOTE]
> Ultraviolet is in active development. The API may change.

## Feedback

We'd love to hear your thoughts on this project. Feel free to drop us a note!

- [Twitter](https://twitter.com/charmcli)
- [Discord](https://charm.land/discord)
- [Slack](https://charm.land/slack)
- [The Fediverse](https://mastodon.social/@charmcli)

## License

[MIT](./LICENSE)

---

Part of [Charm](https://charm.land).

<a href="https://charm.sh/"><img alt="The Charm logo" width="400" src="https://stuff.charm.sh/charm-banner-next.jpg" /></a>

Charm热爱开源 • Charm loves open source • نحنُ نحب المصادر المفتوحة
