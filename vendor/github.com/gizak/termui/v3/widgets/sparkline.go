// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package widgets

import (
	"image"

	. "github.com/gizak/termui/v3"
)

// Sparkline is like: ▅▆▂▂▅▇▂▂▃▆▆▆▅▃. The data points should be non-negative integers.
type Sparkline struct {
	Data       []float64
	Title      string
	TitleStyle Style
	LineColor  Color
	MaxVal     float64
	MaxHeight  int // TODO
}

// SparklineGroup is a renderable widget which groups together the given sparklines.
type SparklineGroup struct {
	Block
	Sparklines []*Sparkline
}

// NewSparkline returns a unrenderable single sparkline that needs to be added to a SparklineGroup
func NewSparkline() *Sparkline {
	return &Sparkline{
		TitleStyle: Theme.Sparkline.Title,
		LineColor:  Theme.Sparkline.Line,
	}
}

func NewSparklineGroup(sls ...*Sparkline) *SparklineGroup {
	return &SparklineGroup{
		Block:      *NewBlock(),
		Sparklines: sls,
	}
}

func (self *SparklineGroup) Draw(buf *Buffer) {
	self.Block.Draw(buf)

	sparklineHeight := self.Inner.Dy() / len(self.Sparklines)

	for i, sl := range self.Sparklines {
		heightOffset := (sparklineHeight * (i + 1))
		barHeight := sparklineHeight
		if i == len(self.Sparklines)-1 {
			heightOffset = self.Inner.Dy()
			barHeight = self.Inner.Dy() - (sparklineHeight * i)
		}
		if sl.Title != "" {
			barHeight--
		}

		maxVal := sl.MaxVal
		if maxVal == 0 {
			maxVal, _ = GetMaxFloat64FromSlice(sl.Data)
		}

		// draw line
		for j := 0; j < len(sl.Data) && j < self.Inner.Dx(); j++ {
			data := sl.Data[j]
			height := int((data / maxVal) * float64(barHeight))
			sparkChar := BARS[len(BARS)-1]
			for k := 0; k < height; k++ {
				buf.SetCell(
					NewCell(sparkChar, NewStyle(sl.LineColor)),
					image.Pt(j+self.Inner.Min.X, self.Inner.Min.Y-1+heightOffset-k),
				)
			}
			if height == 0 {
				sparkChar = BARS[1]
				buf.SetCell(
					NewCell(sparkChar, NewStyle(sl.LineColor)),
					image.Pt(j+self.Inner.Min.X, self.Inner.Min.Y-1+heightOffset),
				)
			}
		}

		if sl.Title != "" {
			// draw title
			buf.SetString(
				TrimString(sl.Title, self.Inner.Dx()),
				sl.TitleStyle,
				image.Pt(self.Inner.Min.X, self.Inner.Min.Y-1+heightOffset-barHeight),
			)
		}
	}
}
