// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package widgets

import (
	"fmt"
	"image"

	rw "github.com/mattn/go-runewidth"

	. "github.com/gizak/termui/v3"
)

type StackedBarChart struct {
	Block
	BarColors    []Color
	LabelStyles  []Style
	NumStyles    []Style // only Fg and Modifier are used
	NumFormatter func(float64) string
	Data         [][]float64
	Labels       []string
	BarWidth     int
	BarGap       int
	MaxVal       float64
}

func NewStackedBarChart() *StackedBarChart {
	return &StackedBarChart{
		Block:        *NewBlock(),
		BarColors:    Theme.StackedBarChart.Bars,
		LabelStyles:  Theme.StackedBarChart.Labels,
		NumStyles:    Theme.StackedBarChart.Nums,
		NumFormatter: func(n float64) string { return fmt.Sprint(n) },
		BarGap:       1,
		BarWidth:     3,
	}
}

func (self *StackedBarChart) Draw(buf *Buffer) {
	self.Block.Draw(buf)

	maxVal := self.MaxVal
	if maxVal == 0 {
		for _, data := range self.Data {
			maxVal = MaxFloat64(maxVal, SumFloat64Slice(data))
		}
	}

	barXCoordinate := self.Inner.Min.X

	for i, bar := range self.Data {
		// draw stacked bars
		stackedBarYCoordinate := 0
		for j, data := range bar {
			// draw each stacked bar
			height := int((data / maxVal) * float64(self.Inner.Dy()-1))
			for x := barXCoordinate; x < MinInt(barXCoordinate+self.BarWidth, self.Inner.Max.X); x++ {
				for y := (self.Inner.Max.Y - 2) - stackedBarYCoordinate; y > (self.Inner.Max.Y-2)-stackedBarYCoordinate-height; y-- {
					c := NewCell(' ', NewStyle(ColorClear, SelectColor(self.BarColors, j)))
					buf.SetCell(c, image.Pt(x, y))
				}
			}

			// draw number
			numberXCoordinate := barXCoordinate + int((float64(self.BarWidth) / 2)) - 1
			buf.SetString(
				self.NumFormatter(data),
				NewStyle(
					SelectStyle(self.NumStyles, j+1).Fg,
					SelectColor(self.BarColors, j),
					SelectStyle(self.NumStyles, j+1).Modifier,
				),
				image.Pt(numberXCoordinate, (self.Inner.Max.Y-2)-stackedBarYCoordinate),
			)

			stackedBarYCoordinate += height
		}

		// draw label
		if i < len(self.Labels) {
			labelXCoordinate := barXCoordinate + MaxInt(
				int((float64(self.BarWidth)/2))-int((float64(rw.StringWidth(self.Labels[i]))/2)),
				0,
			)
			buf.SetString(
				TrimString(self.Labels[i], self.BarWidth),
				SelectStyle(self.LabelStyles, i),
				image.Pt(labelXCoordinate, self.Inner.Max.Y-1),
			)
		}

		barXCoordinate += (self.BarWidth + self.BarGap)
	}
}
