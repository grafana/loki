// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package widgets

import (
	"image"

	. "github.com/gizak/termui/v3"
)

/*Table is like:
┌ Awesome Table ───────────────────────────────────────────────┐
│  Col0          | Col1 | Col2 | Col3  | Col4  | Col5  | Col6  |
│──────────────────────────────────────────────────────────────│
│  Some Item #1  | AAA  | 123  | CCCCC | EEEEE | GGGGG | IIIII |
│──────────────────────────────────────────────────────────────│
│  Some Item #2  | BBB  | 456  | DDDDD | FFFFF | HHHHH | JJJJJ |
└──────────────────────────────────────────────────────────────┘
*/
type Table struct {
	Block
	Rows          [][]string
	ColumnWidths  []int
	TextStyle     Style
	RowSeparator  bool
	TextAlignment Alignment
	RowStyles     map[int]Style
	FillRow       bool

	// ColumnResizer is called on each Draw. Can be used for custom column sizing.
	ColumnResizer func()
}

func NewTable() *Table {
	return &Table{
		Block:         *NewBlock(),
		TextStyle:     Theme.Table.Text,
		RowSeparator:  true,
		RowStyles:     make(map[int]Style),
		ColumnResizer: func() {},
	}
}

func (self *Table) Draw(buf *Buffer) {
	self.Block.Draw(buf)

	self.ColumnResizer()

	columnWidths := self.ColumnWidths
	if len(columnWidths) == 0 {
		columnCount := len(self.Rows[0])
		columnWidth := self.Inner.Dx() / columnCount
		for i := 0; i < columnCount; i++ {
			columnWidths = append(columnWidths, columnWidth)
		}
	}

	yCoordinate := self.Inner.Min.Y

	// draw rows
	for i := 0; i < len(self.Rows) && yCoordinate < self.Inner.Max.Y; i++ {
		row := self.Rows[i]
		colXCoordinate := self.Inner.Min.X

		rowStyle := self.TextStyle
		// get the row style if one exists
		if style, ok := self.RowStyles[i]; ok {
			rowStyle = style
		}

		if self.FillRow {
			blankCell := NewCell(' ', rowStyle)
			buf.Fill(blankCell, image.Rect(self.Inner.Min.X, yCoordinate, self.Inner.Max.X, yCoordinate+1))
		}

		// draw row cells
		for j := 0; j < len(row); j++ {
			col := ParseStyles(row[j], rowStyle)
			// draw row cell
			if len(col) > columnWidths[j] || self.TextAlignment == AlignLeft {
				for _, cx := range BuildCellWithXArray(col) {
					k, cell := cx.X, cx.Cell
					if k == columnWidths[j] || colXCoordinate+k == self.Inner.Max.X {
						cell.Rune = ELLIPSES
						buf.SetCell(cell, image.Pt(colXCoordinate+k-1, yCoordinate))
						break
					} else {
						buf.SetCell(cell, image.Pt(colXCoordinate+k, yCoordinate))
					}
				}
			} else if self.TextAlignment == AlignCenter {
				xCoordinateOffset := (columnWidths[j] - len(col)) / 2
				stringXCoordinate := xCoordinateOffset + colXCoordinate
				for _, cx := range BuildCellWithXArray(col) {
					k, cell := cx.X, cx.Cell
					buf.SetCell(cell, image.Pt(stringXCoordinate+k, yCoordinate))
				}
			} else if self.TextAlignment == AlignRight {
				stringXCoordinate := MinInt(colXCoordinate+columnWidths[j], self.Inner.Max.X) - len(col)
				for _, cx := range BuildCellWithXArray(col) {
					k, cell := cx.X, cx.Cell
					buf.SetCell(cell, image.Pt(stringXCoordinate+k, yCoordinate))
				}
			}
			colXCoordinate += columnWidths[j] + 1
		}

		// draw vertical separators
		separatorStyle := self.Block.BorderStyle

		separatorXCoordinate := self.Inner.Min.X
		verticalCell := NewCell(VERTICAL_LINE, separatorStyle)
		for i, width := range columnWidths {
			if self.FillRow && i < len(columnWidths)-1 {
				verticalCell.Style.Bg = rowStyle.Bg
			} else {
				verticalCell.Style.Bg = self.Block.BorderStyle.Bg
			}

			separatorXCoordinate += width
			buf.SetCell(verticalCell, image.Pt(separatorXCoordinate, yCoordinate))
			separatorXCoordinate++
		}

		yCoordinate++

		// draw horizontal separator
		horizontalCell := NewCell(HORIZONTAL_LINE, separatorStyle)
		if self.RowSeparator && yCoordinate < self.Inner.Max.Y && i != len(self.Rows)-1 {
			buf.Fill(horizontalCell, image.Rect(self.Inner.Min.X, yCoordinate, self.Inner.Max.X, yCoordinate+1))
			yCoordinate++
		}
	}
}
