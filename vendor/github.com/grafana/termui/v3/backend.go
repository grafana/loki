// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package termui

import (
	tb "github.com/nsf/termbox-go"
)

// Init initializes termbox-go and is required to render anything.
// After initialization, the library must be finalized with `Close`.
func Init() error {
	if err := tb.Init(); err != nil {
		return err
	}
	tb.SetInputMode(tb.InputEsc | tb.InputMouse)
	tb.SetOutputMode(tb.Output256)
	return nil
}

// Close closes termbox-go.
func Close() {
	tb.Close()
}

func TerminalDimensions() (int, int) {
	tb.Sync()
	width, height := tb.Size()
	return width, height
}

func Clear() {
	tb.Clear(tb.ColorDefault, tb.Attribute(Theme.Default.Bg+1))
}
