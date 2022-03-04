// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package widgets

import (
	"fmt"
	"image"

	. "github.com/gizak/termui/v3"
)

// Plot has two modes: line(default) and scatter.
// Plot also has two marker types: braille(default) and dot.
// A single braille character is a 2x4 grid of dots, so using braille
// gives 2x X resolution and 4x Y resolution over dot mode.
type Plot struct {
	Block

	Data       [][]float64
	DataLabels []string
	MaxVal     float64

	LineColors []Color
	AxesColor  Color // TODO
	ShowAxes   bool

	Marker          PlotMarker
	DotMarkerRune   rune
	PlotType        PlotType
	HorizontalScale int
	DrawDirection   DrawDirection // TODO
}

const (
	xAxisLabelsHeight = 1
	yAxisLabelsWidth  = 4
	xAxisLabelsGap    = 2
	yAxisLabelsGap    = 1
)

type PlotType uint

const (
	LineChart PlotType = iota
	ScatterPlot
)

type PlotMarker uint

const (
	MarkerBraille PlotMarker = iota
	MarkerDot
)

type DrawDirection uint

const (
	DrawLeft DrawDirection = iota
	DrawRight
)

func NewPlot() *Plot {
	return &Plot{
		Block:           *NewBlock(),
		LineColors:      Theme.Plot.Lines,
		AxesColor:       Theme.Plot.Axes,
		Marker:          MarkerBraille,
		DotMarkerRune:   DOT,
		Data:            [][]float64{},
		HorizontalScale: 1,
		DrawDirection:   DrawRight,
		ShowAxes:        true,
		PlotType:        LineChart,
	}
}

func (self *Plot) renderBraille(buf *Buffer, drawArea image.Rectangle, maxVal float64) {
	canvas := NewCanvas()
	canvas.Rectangle = drawArea

	switch self.PlotType {
	case ScatterPlot:
		for i, line := range self.Data {
			for j, val := range line {
				height := int((val / maxVal) * float64(drawArea.Dy()-1))
				canvas.SetPoint(
					image.Pt(
						(drawArea.Min.X+(j*self.HorizontalScale))*2,
						(drawArea.Max.Y-height-1)*4,
					),
					SelectColor(self.LineColors, i),
				)
			}
		}
	case LineChart:
		for i, line := range self.Data {
			previousHeight := int((line[1] / maxVal) * float64(drawArea.Dy()-1))
			for j, val := range line[1:] {
				height := int((val / maxVal) * float64(drawArea.Dy()-1))
				canvas.SetLine(
					image.Pt(
						(drawArea.Min.X+(j*self.HorizontalScale))*2,
						(drawArea.Max.Y-previousHeight-1)*4,
					),
					image.Pt(
						(drawArea.Min.X+((j+1)*self.HorizontalScale))*2,
						(drawArea.Max.Y-height-1)*4,
					),
					SelectColor(self.LineColors, i),
				)
				previousHeight = height
			}
		}
	}

	canvas.Draw(buf)
}

func (self *Plot) renderDot(buf *Buffer, drawArea image.Rectangle, maxVal float64) {
	switch self.PlotType {
	case ScatterPlot:
		for i, line := range self.Data {
			for j, val := range line {
				height := int((val / maxVal) * float64(drawArea.Dy()-1))
				point := image.Pt(drawArea.Min.X+(j*self.HorizontalScale), drawArea.Max.Y-1-height)
				if point.In(drawArea) {
					buf.SetCell(
						NewCell(self.DotMarkerRune, NewStyle(SelectColor(self.LineColors, i))),
						point,
					)
				}
			}
		}
	case LineChart:
		for i, line := range self.Data {
			for j := 0; j < len(line) && j*self.HorizontalScale < drawArea.Dx(); j++ {
				val := line[j]
				height := int((val / maxVal) * float64(drawArea.Dy()-1))
				buf.SetCell(
					NewCell(self.DotMarkerRune, NewStyle(SelectColor(self.LineColors, i))),
					image.Pt(drawArea.Min.X+(j*self.HorizontalScale), drawArea.Max.Y-1-height),
				)
			}
		}
	}
}

func (self *Plot) plotAxes(buf *Buffer, maxVal float64) {
	// draw origin cell
	buf.SetCell(
		NewCell(BOTTOM_LEFT, NewStyle(ColorWhite)),
		image.Pt(self.Inner.Min.X+yAxisLabelsWidth, self.Inner.Max.Y-xAxisLabelsHeight-1),
	)
	// draw x axis line
	for i := yAxisLabelsWidth + 1; i < self.Inner.Dx(); i++ {
		buf.SetCell(
			NewCell(HORIZONTAL_DASH, NewStyle(ColorWhite)),
			image.Pt(i+self.Inner.Min.X, self.Inner.Max.Y-xAxisLabelsHeight-1),
		)
	}
	// draw y axis line
	for i := 0; i < self.Inner.Dy()-xAxisLabelsHeight-1; i++ {
		buf.SetCell(
			NewCell(VERTICAL_DASH, NewStyle(ColorWhite)),
			image.Pt(self.Inner.Min.X+yAxisLabelsWidth, i+self.Inner.Min.Y),
		)
	}
	// draw x axis labels
	// draw 0
	buf.SetString(
		"0",
		NewStyle(ColorWhite),
		image.Pt(self.Inner.Min.X+yAxisLabelsWidth, self.Inner.Max.Y-1),
	)
	// draw rest
	for x := self.Inner.Min.X + yAxisLabelsWidth + (xAxisLabelsGap)*self.HorizontalScale + 1; x < self.Inner.Max.X-1; {
		label := fmt.Sprintf(
			"%d",
			(x-(self.Inner.Min.X+yAxisLabelsWidth)-1)/(self.HorizontalScale)+1,
		)
		buf.SetString(
			label,
			NewStyle(ColorWhite),
			image.Pt(x, self.Inner.Max.Y-1),
		)
		x += (len(label) + xAxisLabelsGap) * self.HorizontalScale
	}
	// draw y axis labels
	verticalScale := maxVal / float64(self.Inner.Dy()-xAxisLabelsHeight-1)
	for i := 0; i*(yAxisLabelsGap+1) < self.Inner.Dy()-1; i++ {
		buf.SetString(
			fmt.Sprintf("%.2f", float64(i)*verticalScale*(yAxisLabelsGap+1)),
			NewStyle(ColorWhite),
			image.Pt(self.Inner.Min.X, self.Inner.Max.Y-(i*(yAxisLabelsGap+1))-2),
		)
	}
}

func (self *Plot) Draw(buf *Buffer) {
	self.Block.Draw(buf)

	maxVal := self.MaxVal
	if maxVal == 0 {
		maxVal, _ = GetMaxFloat64From2dSlice(self.Data)
	}

	if self.ShowAxes {
		self.plotAxes(buf, maxVal)
	}

	drawArea := self.Inner
	if self.ShowAxes {
		drawArea = image.Rect(
			self.Inner.Min.X+yAxisLabelsWidth+1, self.Inner.Min.Y,
			self.Inner.Max.X, self.Inner.Max.Y-xAxisLabelsHeight-1,
		)
	}

	switch self.Marker {
	case MarkerBraille:
		self.renderBraille(buf, drawArea, maxVal)
	case MarkerDot:
		self.renderDot(buf, drawArea, maxVal)
	}
}
