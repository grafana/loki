// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package termui

import (
	"strings"
)

const (
	tokenFg       = "fg"
	tokenBg       = "bg"
	tokenModifier = "mod"

	tokenItemSeparator  = ","
	tokenValueSeparator = ":"

	tokenBeginStyledText = '['
	tokenEndStyledText   = ']'

	tokenBeginStyle = '('
	tokenEndStyle   = ')'
)

type parserState uint

const (
	parserStateDefault parserState = iota
	parserStateStyleItems
	parserStateStyledText
)

// StyleParserColorMap can be modified to add custom color parsing to text
var StyleParserColorMap = map[string]Color{
	"red":     ColorRed,
	"blue":    ColorBlue,
	"black":   ColorBlack,
	"cyan":    ColorCyan,
	"yellow":  ColorYellow,
	"white":   ColorWhite,
	"clear":   ColorClear,
	"green":   ColorGreen,
	"magenta": ColorMagenta,
}

var modifierMap = map[string]Modifier{
	"bold":      ModifierBold,
	"underline": ModifierUnderline,
	"reverse":   ModifierReverse,
}

// readStyle translates an []rune like `fg:red,mod:bold,bg:white` to a style
func readStyle(runes []rune, defaultStyle Style) Style {
	style := defaultStyle
	split := strings.Split(string(runes), tokenItemSeparator)
	for _, item := range split {
		pair := strings.Split(item, tokenValueSeparator)
		if len(pair) == 2 {
			switch pair[0] {
			case tokenFg:
				style.Fg = StyleParserColorMap[pair[1]]
			case tokenBg:
				style.Bg = StyleParserColorMap[pair[1]]
			case tokenModifier:
				style.Modifier = modifierMap[pair[1]]
			}
		}
	}
	return style
}

// ParseStyles parses a string for embedded Styles and returns []Cell with the correct styling.
// Uses defaultStyle for any text without an embedded style.
// Syntax is of the form [text](fg:<color>,mod:<attribute>,bg:<color>).
// Ordering does not matter. All fields are optional.
func ParseStyles(s string, defaultStyle Style) []Cell {
	cells := []Cell{}
	runes := []rune(s)
	state := parserStateDefault
	styledText := []rune{}
	styleItems := []rune{}
	squareCount := 0

	reset := func() {
		styledText = []rune{}
		styleItems = []rune{}
		state = parserStateDefault
		squareCount = 0
	}

	rollback := func() {
		cells = append(cells, RunesToStyledCells(styledText, defaultStyle)...)
		cells = append(cells, RunesToStyledCells(styleItems, defaultStyle)...)
		reset()
	}

	// chop first and last runes
	chop := func(s []rune) []rune {
		return s[1 : len(s)-1]
	}

	for i, _rune := range runes {
		switch state {
		case parserStateDefault:
			if _rune == tokenBeginStyledText {
				state = parserStateStyledText
				squareCount = 1
				styledText = append(styledText, _rune)
			} else {
				cells = append(cells, Cell{_rune, defaultStyle})
			}
		case parserStateStyledText:
			switch {
			case squareCount == 0:
				switch _rune {
				case tokenBeginStyle:
					state = parserStateStyleItems
					styleItems = append(styleItems, _rune)
				default:
					rollback()
					switch _rune {
					case tokenBeginStyledText:
						state = parserStateStyledText
						squareCount = 1
						styleItems = append(styleItems, _rune)
					default:
						cells = append(cells, Cell{_rune, defaultStyle})
					}
				}
			case len(runes) == i+1:
				rollback()
				styledText = append(styledText, _rune)
			case _rune == tokenBeginStyledText:
				squareCount++
				styledText = append(styledText, _rune)
			case _rune == tokenEndStyledText:
				squareCount--
				styledText = append(styledText, _rune)
			default:
				styledText = append(styledText, _rune)
			}
		case parserStateStyleItems:
			styleItems = append(styleItems, _rune)
			if _rune == tokenEndStyle {
				style := readStyle(chop(styleItems), defaultStyle)
				cells = append(cells, RunesToStyledCells(chop(styledText), style)...)
				reset()
			} else if len(runes) == i+1 {
				rollback()
			}
		}
	}

	return cells
}
