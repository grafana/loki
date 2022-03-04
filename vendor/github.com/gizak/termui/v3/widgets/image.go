// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package widgets

import (
	"image"
	"image/color"

	. "github.com/gizak/termui/v3"
)

type Image struct {
	Block
	Image               image.Image
	Monochrome          bool
	MonochromeThreshold uint8
	MonochromeInvert    bool
}

func NewImage(img image.Image) *Image {
	return &Image{
		Block:               *NewBlock(),
		MonochromeThreshold: 128,
		Image:               img,
	}
}

func (self *Image) Draw(buf *Buffer) {
	self.Block.Draw(buf)

	if self.Image == nil {
		return
	}

	bufWidth := self.Inner.Dx()
	bufHeight := self.Inner.Dy()
	imageWidth := self.Image.Bounds().Dx()
	imageHeight := self.Image.Bounds().Dy()

	if self.Monochrome {
		if bufWidth > imageWidth/2 {
			bufWidth = imageWidth / 2
		}
		if bufHeight > imageHeight/2 {
			bufHeight = imageHeight / 2
		}
		for bx := 0; bx < bufWidth; bx++ {
			for by := 0; by < bufHeight; by++ {
				ul := self.colorAverage(
					2*bx*imageWidth/bufWidth/2,
					(2*bx+1)*imageWidth/bufWidth/2,
					2*by*imageHeight/bufHeight/2,
					(2*by+1)*imageHeight/bufHeight/2,
				)
				ur := self.colorAverage(
					(2*bx+1)*imageWidth/bufWidth/2,
					(2*bx+2)*imageWidth/bufWidth/2,
					2*by*imageHeight/bufHeight/2,
					(2*by+1)*imageHeight/bufHeight/2,
				)
				ll := self.colorAverage(
					2*bx*imageWidth/bufWidth/2,
					(2*bx+1)*imageWidth/bufWidth/2,
					(2*by+1)*imageHeight/bufHeight/2,
					(2*by+2)*imageHeight/bufHeight/2,
				)
				lr := self.colorAverage(
					(2*bx+1)*imageWidth/bufWidth/2,
					(2*bx+2)*imageWidth/bufWidth/2,
					(2*by+1)*imageHeight/bufHeight/2,
					(2*by+2)*imageHeight/bufHeight/2,
				)
				buf.SetCell(
					NewCell(blocksChar(ul, ur, ll, lr, self.MonochromeThreshold, self.MonochromeInvert)),
					image.Pt(self.Inner.Min.X+bx, self.Inner.Min.Y+by),
				)
			}
		}
	} else {
		if bufWidth > imageWidth {
			bufWidth = imageWidth
		}
		if bufHeight > imageHeight {
			bufHeight = imageHeight
		}
		for bx := 0; bx < bufWidth; bx++ {
			for by := 0; by < bufHeight; by++ {
				c := self.colorAverage(
					bx*imageWidth/bufWidth,
					(bx+1)*imageWidth/bufWidth,
					by*imageHeight/bufHeight,
					(by+1)*imageHeight/bufHeight,
				)
				buf.SetCell(
					NewCell(c.ch(), NewStyle(c.fgColor(), ColorBlack)),
					image.Pt(self.Inner.Min.X+bx, self.Inner.Min.Y+by),
				)
			}
		}
	}
}

func (self *Image) colorAverage(x0, x1, y0, y1 int) colorAverager {
	var c colorAverager
	for x := x0; x < x1; x++ {
		for y := y0; y < y1; y++ {
			c = c.add(
				self.Image.At(
					x+self.Image.Bounds().Min.X,
					y+self.Image.Bounds().Min.Y,
				),
			)
		}
	}
	return c
}

type colorAverager struct {
	rsum, gsum, bsum, asum, count uint64
}

func (self colorAverager) add(col color.Color) colorAverager {
	r, g, b, a := col.RGBA()
	return colorAverager{
		rsum:  self.rsum + uint64(r),
		gsum:  self.gsum + uint64(g),
		bsum:  self.bsum + uint64(b),
		asum:  self.asum + uint64(a),
		count: self.count + 1,
	}
}

func (self colorAverager) RGBA() (uint32, uint32, uint32, uint32) {
	if self.count == 0 {
		return 0, 0, 0, 0
	}
	return uint32(self.rsum/self.count) & 0xffff,
		uint32(self.gsum/self.count) & 0xffff,
		uint32(self.bsum/self.count) & 0xffff,
		uint32(self.asum/self.count) & 0xffff
}

func (self colorAverager) fgColor() Color {
	return palette.Convert(self).(paletteColor).attribute
}

func (self colorAverager) ch() rune {
	gray := color.GrayModel.Convert(self).(color.Gray).Y
	switch {
	case gray < 51:
		return SHADED_BLOCKS[0]
	case gray < 102:
		return SHADED_BLOCKS[1]
	case gray < 153:
		return SHADED_BLOCKS[2]
	case gray < 204:
		return SHADED_BLOCKS[3]
	default:
		return SHADED_BLOCKS[4]
	}
}

func (self colorAverager) monochrome(threshold uint8, invert bool) bool {
	return self.count != 0 && (color.GrayModel.Convert(self).(color.Gray).Y < threshold != invert)
}

type paletteColor struct {
	rgba      color.RGBA
	attribute Color
}

func (self paletteColor) RGBA() (uint32, uint32, uint32, uint32) {
	return self.rgba.RGBA()
}

var palette = color.Palette([]color.Color{
	paletteColor{color.RGBA{0, 0, 0, 255}, ColorBlack},
	paletteColor{color.RGBA{255, 0, 0, 255}, ColorRed},
	paletteColor{color.RGBA{0, 255, 0, 255}, ColorGreen},
	paletteColor{color.RGBA{255, 255, 0, 255}, ColorYellow},
	paletteColor{color.RGBA{0, 0, 255, 255}, ColorBlue},
	paletteColor{color.RGBA{255, 0, 255, 255}, ColorMagenta},
	paletteColor{color.RGBA{0, 255, 255, 255}, ColorCyan},
	paletteColor{color.RGBA{255, 255, 255, 255}, ColorWhite},
})

func blocksChar(ul, ur, ll, lr colorAverager, threshold uint8, invert bool) rune {
	index := 0
	if ul.monochrome(threshold, invert) {
		index |= 1
	}
	if ur.monochrome(threshold, invert) {
		index |= 2
	}
	if ll.monochrome(threshold, invert) {
		index |= 4
	}
	if lr.monochrome(threshold, invert) {
		index |= 8
	}
	return IRREGULAR_BLOCKS[index]
}
