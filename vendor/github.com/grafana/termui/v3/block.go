// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package termui

import (
	"image"
	"sync"
)

// Block is the base struct inherited by most widgets.
// Block manages size, position, border, and title.
// It implements all 3 of the methods needed for the `Drawable` interface.
// Custom widgets will override the Draw method.
type Block struct {
	Border      bool
	BorderStyle Style

	BorderLeft, BorderRight, BorderTop, BorderBottom bool

	PaddingLeft, PaddingRight, PaddingTop, PaddingBottom int

	image.Rectangle
	Inner image.Rectangle

	Title      string
	TitleStyle Style

	sync.Mutex
}

func NewBlock() *Block {
	return &Block{
		Border:       true,
		BorderStyle:  Theme.Block.Border,
		BorderLeft:   true,
		BorderRight:  true,
		BorderTop:    true,
		BorderBottom: true,

		TitleStyle: Theme.Block.Title,
	}
}

func (self *Block) drawBorder(buf *Buffer) {
	verticalCell := Cell{VERTICAL_LINE, self.BorderStyle}
	horizontalCell := Cell{HORIZONTAL_LINE, self.BorderStyle}

	// draw lines
	if self.BorderTop {
		buf.Fill(horizontalCell, image.Rect(self.Min.X, self.Min.Y, self.Max.X, self.Min.Y+1))
	}
	if self.BorderBottom {
		buf.Fill(horizontalCell, image.Rect(self.Min.X, self.Max.Y-1, self.Max.X, self.Max.Y))
	}
	if self.BorderLeft {
		buf.Fill(verticalCell, image.Rect(self.Min.X, self.Min.Y, self.Min.X+1, self.Max.Y))
	}
	if self.BorderRight {
		buf.Fill(verticalCell, image.Rect(self.Max.X-1, self.Min.Y, self.Max.X, self.Max.Y))
	}

	// draw corners
	if self.BorderTop && self.BorderLeft {
		buf.SetCell(Cell{TOP_LEFT, self.BorderStyle}, self.Min)
	}
	if self.BorderTop && self.BorderRight {
		buf.SetCell(Cell{TOP_RIGHT, self.BorderStyle}, image.Pt(self.Max.X-1, self.Min.Y))
	}
	if self.BorderBottom && self.BorderLeft {
		buf.SetCell(Cell{BOTTOM_LEFT, self.BorderStyle}, image.Pt(self.Min.X, self.Max.Y-1))
	}
	if self.BorderBottom && self.BorderRight {
		buf.SetCell(Cell{BOTTOM_RIGHT, self.BorderStyle}, self.Max.Sub(image.Pt(1, 1)))
	}
}

// Draw implements the Drawable interface.
func (self *Block) Draw(buf *Buffer) {
	if self.Border {
		self.drawBorder(buf)
	}
	buf.SetString(
		self.Title,
		self.TitleStyle,
		image.Pt(self.Min.X+2, self.Min.Y),
	)
}

// SetRect implements the Drawable interface.
func (self *Block) SetRect(x1, y1, x2, y2 int) {
	self.Rectangle = image.Rect(x1, y1, x2, y2)
	self.Inner = image.Rect(
		self.Min.X+1+self.PaddingLeft,
		self.Min.Y+1+self.PaddingTop,
		self.Max.X-1-self.PaddingRight,
		self.Max.Y-1-self.PaddingBottom,
	)
}

// GetRect implements the Drawable interface.
func (self *Block) GetRect() image.Rectangle {
	return self.Rectangle
}
