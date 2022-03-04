// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package widgets

import (
	"image"

	. "github.com/gizak/termui/v3"
)

// TabPane is a renderable widget which can be used to conditionally render certain tabs/views.
// TabPane shows a list of Tab names.
// The currently selected tab can be found through the `ActiveTabIndex` field.
type TabPane struct {
	Block
	TabNames         []string
	ActiveTabIndex   int
	ActiveTabStyle   Style
	InactiveTabStyle Style
}

func NewTabPane(names ...string) *TabPane {
	return &TabPane{
		Block:            *NewBlock(),
		TabNames:         names,
		ActiveTabStyle:   Theme.Tab.Active,
		InactiveTabStyle: Theme.Tab.Inactive,
	}
}

func (self *TabPane) FocusLeft() {
	if self.ActiveTabIndex > 0 {
		self.ActiveTabIndex--
	}
}

func (self *TabPane) FocusRight() {
	if self.ActiveTabIndex < len(self.TabNames)-1 {
		self.ActiveTabIndex++
	}
}

func (self *TabPane) Draw(buf *Buffer) {
	self.Block.Draw(buf)

	xCoordinate := self.Inner.Min.X
	for i, name := range self.TabNames {
		ColorPair := self.InactiveTabStyle
		if i == self.ActiveTabIndex {
			ColorPair = self.ActiveTabStyle
		}
		buf.SetString(
			TrimString(name, self.Inner.Max.X-xCoordinate),
			ColorPair,
			image.Pt(xCoordinate, self.Inner.Min.Y),
		)

		xCoordinate += 1 + len(name)

		if i < len(self.TabNames)-1 && xCoordinate < self.Inner.Max.X {
			buf.SetCell(
				NewCell(VERTICAL_LINE, NewStyle(ColorWhite)),
				image.Pt(xCoordinate, self.Inner.Min.Y),
			)
		}

		xCoordinate += 2
	}
}
