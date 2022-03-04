package widgets

import (
	"image"
	"math"

	. "github.com/gizak/termui/v3"
)

const (
	piechartOffsetUp = -.5 * math.Pi // the northward angle
	resolutionFactor = .0001         // circle resolution: precision vs. performance
	fullCircle       = 2.0 * math.Pi // the full circle angle
	xStretch         = 2.0           // horizontal adjustment
)

// PieChartLabel callback
type PieChartLabel func(dataIndex int, currentValue float64) string

type PieChart struct {
	Block
	Data           []float64     // list of data items
	Colors         []Color       // colors to by cycled through
	LabelFormatter PieChartLabel // callback function for labels
	AngleOffset    float64       // which angle to start drawing at? (see piechartOffsetUp)
}

// NewPieChart Creates a new pie chart with reasonable defaults and no labels.
func NewPieChart() *PieChart {
	return &PieChart{
		Block:       *NewBlock(),
		Colors:      Theme.PieChart.Slices,
		AngleOffset: piechartOffsetUp,
	}
}

func (self *PieChart) Draw(buf *Buffer) {
	self.Block.Draw(buf)

	center := self.Inner.Min.Add(self.Inner.Size().Div(2))
	radius := MinFloat64(float64(self.Inner.Dx()/2/xStretch), float64(self.Inner.Dy()/2))

	// compute slice sizes
	sum := SumFloat64Slice(self.Data)
	sliceSizes := make([]float64, len(self.Data))
	for i, v := range self.Data {
		sliceSizes[i] = v / sum * fullCircle
	}

	borderCircle := &circle{center, radius}
	middleCircle := circle{Point: center, radius: radius / 2.0}

	// draw sectors
	phi := self.AngleOffset
	for i, size := range sliceSizes {
		for j := 0.0; j < size; j += resolutionFactor {
			borderPoint := borderCircle.at(phi + j)
			line := line{P1: center, P2: borderPoint}
			line.draw(NewCell(SHADED_BLOCKS[1], NewStyle(SelectColor(self.Colors, i))), buf)
		}
		phi += size
	}

	// draw labels
	if self.LabelFormatter != nil {
		phi = self.AngleOffset
		for i, size := range sliceSizes {
			labelPoint := middleCircle.at(phi + size/2.0)
			if len(self.Data) == 1 {
				labelPoint = center
			}
			buf.SetString(
				self.LabelFormatter(i, self.Data[i]),
				NewStyle(SelectColor(self.Colors, i)),
				image.Pt(labelPoint.X, labelPoint.Y),
			)
			phi += size
		}
	}
}

type circle struct {
	image.Point
	radius float64
}

// computes the point at a given angle phi
func (self circle) at(phi float64) image.Point {
	x := self.X + int(RoundFloat64(xStretch*self.radius*math.Cos(phi)))
	y := self.Y + int(RoundFloat64(self.radius*math.Sin(phi)))
	return image.Point{X: x, Y: y}
}

// computes the perimeter of a circle
func (self circle) perimeter() float64 {
	return 2.0 * math.Pi * self.radius
}

// a line between two points
type line struct {
	P1, P2 image.Point
}

// draws the line
func (self line) draw(cell Cell, buf *Buffer) {
	isLeftOf := func(p1, p2 image.Point) bool {
		return p1.X <= p2.X
	}
	isTopOf := func(p1, p2 image.Point) bool {
		return p1.Y <= p2.Y
	}
	p1, p2 := self.P1, self.P2
	buf.SetCell(NewCell('*', cell.Style), self.P2)
	width, height := self.size()
	if width > height { // paint left to right
		if !isLeftOf(p1, p2) {
			p1, p2 = p2, p1
		}
		flip := 1.0
		if !isTopOf(p1, p2) {
			flip = -1.0
		}
		for x := p1.X; x <= p2.X; x++ {
			ratio := float64(height) / float64(width)
			factor := float64(x - p1.X)
			y := ratio * factor * flip
			buf.SetCell(cell, image.Pt(x, int(RoundFloat64(y))+p1.Y))
		}
	} else { // paint top to bottom
		if !isTopOf(p1, p2) {
			p1, p2 = p2, p1
		}
		flip := 1.0
		if !isLeftOf(p1, p2) {
			flip = -1.0
		}
		for y := p1.Y; y <= p2.Y; y++ {
			ratio := float64(width) / float64(height)
			factor := float64(y - p1.Y)
			x := ratio * factor * flip
			buf.SetCell(cell, image.Pt(int(RoundFloat64(x))+p1.X, y))
		}
	}
}

// width and height of a line
func (self line) size() (w, h int) {
	return AbsInt(self.P2.X - self.P1.X), AbsInt(self.P2.Y - self.P1.Y)
}
