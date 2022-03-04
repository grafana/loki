// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package termui

import (
	"fmt"

	tb "github.com/nsf/termbox-go"
)

/*
List of events:
	mouse events:
		<MouseLeft> <MouseRight> <MouseMiddle>
		<MouseWheelUp> <MouseWheelDown>
	keyboard events:
		any uppercase or lowercase letter like j or J
		<C-d> etc
		<M-d> etc
		<Up> <Down> <Left> <Right>
		<Insert> <Delete> <Home> <End> <Previous> <Next>
		<Backspace> <Tab> <Enter> <Escape> <Space>
		<C-<Space>> etc
	terminal events:
        <Resize>

    keyboard events that do not work:
        <C-->
        <C-2> <C-~>
        <C-h>
        <C-i>
        <C-m>
        <C-[> <C-3>
        <C-\\>
        <C-]>
        <C-/> <C-_>
        <C-8>
*/

type EventType uint

const (
	KeyboardEvent EventType = iota
	MouseEvent
	ResizeEvent
)

type Event struct {
	Type    EventType
	ID      string
	Payload interface{}
}

// Mouse payload.
type Mouse struct {
	Drag bool
	X    int
	Y    int
}

// Resize payload.
type Resize struct {
	Width  int
	Height int
}

// PollEvents gets events from termbox, converts them, then sends them to each of its channels.
func PollEvents() <-chan Event {
	ch := make(chan Event)
	go func() {
		for {
			ch <- convertTermboxEvent(tb.PollEvent())
		}
	}()
	return ch
}

var keyboardMap = map[tb.Key]string{
	tb.KeyF1:         "<F1>",
	tb.KeyF2:         "<F2>",
	tb.KeyF3:         "<F3>",
	tb.KeyF4:         "<F4>",
	tb.KeyF5:         "<F5>",
	tb.KeyF6:         "<F6>",
	tb.KeyF7:         "<F7>",
	tb.KeyF8:         "<F8>",
	tb.KeyF9:         "<F9>",
	tb.KeyF10:        "<F10>",
	tb.KeyF11:        "<F11>",
	tb.KeyF12:        "<F12>",
	tb.KeyInsert:     "<Insert>",
	tb.KeyDelete:     "<Delete>",
	tb.KeyHome:       "<Home>",
	tb.KeyEnd:        "<End>",
	tb.KeyPgup:       "<PageUp>",
	tb.KeyPgdn:       "<PageDown>",
	tb.KeyArrowUp:    "<Up>",
	tb.KeyArrowDown:  "<Down>",
	tb.KeyArrowLeft:  "<Left>",
	tb.KeyArrowRight: "<Right>",

	tb.KeyCtrlSpace:  "<C-<Space>>", // tb.KeyCtrl2 tb.KeyCtrlTilde
	tb.KeyCtrlA:      "<C-a>",
	tb.KeyCtrlB:      "<C-b>",
	tb.KeyCtrlC:      "<C-c>",
	tb.KeyCtrlD:      "<C-d>",
	tb.KeyCtrlE:      "<C-e>",
	tb.KeyCtrlF:      "<C-f>",
	tb.KeyCtrlG:      "<C-g>",
	tb.KeyBackspace:  "<C-<Backspace>>", // tb.KeyCtrlH
	tb.KeyTab:        "<Tab>",           // tb.KeyCtrlI
	tb.KeyCtrlJ:      "<C-j>",
	tb.KeyCtrlK:      "<C-k>",
	tb.KeyCtrlL:      "<C-l>",
	tb.KeyEnter:      "<Enter>", // tb.KeyCtrlM
	tb.KeyCtrlN:      "<C-n>",
	tb.KeyCtrlO:      "<C-o>",
	tb.KeyCtrlP:      "<C-p>",
	tb.KeyCtrlQ:      "<C-q>",
	tb.KeyCtrlR:      "<C-r>",
	tb.KeyCtrlS:      "<C-s>",
	tb.KeyCtrlT:      "<C-t>",
	tb.KeyCtrlU:      "<C-u>",
	tb.KeyCtrlV:      "<C-v>",
	tb.KeyCtrlW:      "<C-w>",
	tb.KeyCtrlX:      "<C-x>",
	tb.KeyCtrlY:      "<C-y>",
	tb.KeyCtrlZ:      "<C-z>",
	tb.KeyEsc:        "<Escape>", // tb.KeyCtrlLsqBracket tb.KeyCtrl3
	tb.KeyCtrl4:      "<C-4>",    // tb.KeyCtrlBackslash
	tb.KeyCtrl5:      "<C-5>",    // tb.KeyCtrlRsqBracket
	tb.KeyCtrl6:      "<C-6>",
	tb.KeyCtrl7:      "<C-7>", // tb.KeyCtrlSlash tb.KeyCtrlUnderscore
	tb.KeySpace:      "<Space>",
	tb.KeyBackspace2: "<Backspace>", // tb.KeyCtrl8:
}

// convertTermboxKeyboardEvent converts a termbox keyboard event to a more friendly string format.
// Combines modifiers into the string instead of having them as additional fields in an event.
func convertTermboxKeyboardEvent(e tb.Event) Event {
	ID := "%s"
	if e.Mod == tb.ModAlt {
		ID = "<M-%s>"
	}

	if e.Ch != 0 {
		ID = fmt.Sprintf(ID, string(e.Ch))
	} else {
		converted, ok := keyboardMap[e.Key]
		if !ok {
			converted = ""
		}
		ID = fmt.Sprintf(ID, converted)
	}

	return Event{
		Type: KeyboardEvent,
		ID:   ID,
	}
}

var mouseButtonMap = map[tb.Key]string{
	tb.MouseLeft:      "<MouseLeft>",
	tb.MouseMiddle:    "<MouseMiddle>",
	tb.MouseRight:     "<MouseRight>",
	tb.MouseRelease:   "<MouseRelease>",
	tb.MouseWheelUp:   "<MouseWheelUp>",
	tb.MouseWheelDown: "<MouseWheelDown>",
}

func convertTermboxMouseEvent(e tb.Event) Event {
	converted, ok := mouseButtonMap[e.Key]
	if !ok {
		converted = "Unknown_Mouse_Button"
	}
	Drag := e.Mod == tb.ModMotion
	return Event{
		Type: MouseEvent,
		ID:   converted,
		Payload: Mouse{
			X:    e.MouseX,
			Y:    e.MouseY,
			Drag: Drag,
		},
	}
}

// convertTermboxEvent turns a termbox event into a termui event.
func convertTermboxEvent(e tb.Event) Event {
	if e.Type == tb.EventError {
		panic(e.Err)
	}
	switch e.Type {
	case tb.EventKey:
		return convertTermboxKeyboardEvent(e)
	case tb.EventMouse:
		return convertTermboxMouseEvent(e)
	case tb.EventResize:
		return Event{
			Type: ResizeEvent,
			ID:   "<Resize>",
			Payload: Resize{
				Width:  e.Width,
				Height: e.Height,
			},
		}
	}
	return Event{}
}
