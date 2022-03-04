// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package widgets

import (
	"fmt"
	"image"

	. "github.com/gizak/termui/v3"
)

type Gauge struct {
	Block
	Percent    int
	BarColor   Color
	Label      string
	LabelStyle Style
}

func NewGauge() *Gauge {
	return &Gauge{
		Block:      *NewBlock(),
		BarColor:   Theme.Gauge.Bar,
		LabelStyle: Theme.Gauge.Label,
	}
}

func (self *Gauge) Draw(buf *Buffer) {
	self.Block.Draw(buf)

	label := self.Label
	if label == "" {
		label = fmt.Sprintf("%d%%", self.Percent)
	}

	// plot bar
	barWidth := int((float64(self.Percent) / 100) * float64(self.Inner.Dx()))
	buf.Fill(
		NewCell(' ', NewStyle(ColorClear, self.BarColor)),
		image.Rect(self.Inner.Min.X, self.Inner.Min.Y, self.Inner.Min.X+barWidth, self.Inner.Max.Y),
	)

	// plot label
	labelXCoordinate := self.Inner.Min.X + (self.Inner.Dx() / 2) - int(float64(len(label))/2)
	labelYCoordinate := self.Inner.Min.Y + ((self.Inner.Dy() - 1) / 2)
	if labelYCoordinate < self.Inner.Max.Y {
		for i, char := range label {
			style := self.LabelStyle
			if labelXCoordinate+i+1 <= self.Inner.Min.X+barWidth {
				style = NewStyle(self.BarColor, ColorClear, ModifierReverse)
			}
			buf.SetCell(NewCell(char, style), image.Pt(labelXCoordinate+i, labelYCoordinate))
		}
	}
}
