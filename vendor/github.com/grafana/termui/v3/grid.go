// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package termui

type gridItemType uint

const (
	col gridItemType = 0
	row gridItemType = 1
)

type Grid struct {
	Block
	Items []*GridItem
}

// GridItem represents either a Row or Column in a grid.
// Holds sizing information and either an []GridItems or a widget.
type GridItem struct {
	Type        gridItemType
	XRatio      float64
	YRatio      float64
	WidthRatio  float64
	HeightRatio float64
	Entry       interface{} // Entry.type == GridBufferer if IsLeaf else []GridItem
	IsLeaf      bool
	ratio       float64
}

func NewGrid() *Grid {
	g := &Grid{
		Block: *NewBlock(),
	}
	g.Border = false
	return g
}

// NewCol takes a height percentage and either a widget or a Row or Column
func NewCol(ratio float64, i ...interface{}) GridItem {
	_, ok := i[0].(Drawable)
	entry := i[0]
	if !ok {
		entry = i
	}
	return GridItem{
		Type:   col,
		Entry:  entry,
		IsLeaf: ok,
		ratio:  ratio,
	}
}

// NewRow takes a width percentage and either a widget or a Row or Column
func NewRow(ratio float64, i ...interface{}) GridItem {
	_, ok := i[0].(Drawable)
	entry := i[0]
	if !ok {
		entry = i
	}
	return GridItem{
		Type:   row,
		Entry:  entry,
		IsLeaf: ok,
		ratio:  ratio,
	}
}

// Set is used to add Columns and Rows to the grid.
// It recursively searches the GridItems, adding leaves to the grid and calculating the dimensions of the leaves.
func (self *Grid) Set(entries ...interface{}) {
	entry := GridItem{
		Type:   row,
		Entry:  entries,
		IsLeaf: false,
		ratio:  1.0,
	}
	self.setHelper(entry, 1.0, 1.0)
}

func (self *Grid) setHelper(item GridItem, parentWidthRatio, parentHeightRatio float64) {
	var HeightRatio float64
	var WidthRatio float64
	switch item.Type {
	case col:
		HeightRatio = 1.0
		WidthRatio = item.ratio
	case row:
		HeightRatio = item.ratio
		WidthRatio = 1.0
	}
	item.WidthRatio = parentWidthRatio * WidthRatio
	item.HeightRatio = parentHeightRatio * HeightRatio

	if item.IsLeaf {
		self.Items = append(self.Items, &item)
	} else {
		XRatio := 0.0
		YRatio := 0.0
		cols := false
		rows := false

		children := InterfaceSlice(item.Entry)

		for i := 0; i < len(children); i++ {
			if children[i] == nil {
				continue
			}
			child, _ := children[i].(GridItem)

			child.XRatio = item.XRatio + (item.WidthRatio * XRatio)
			child.YRatio = item.YRatio + (item.HeightRatio * YRatio)

			switch child.Type {
			case col:
				cols = true
				XRatio += child.ratio
				if rows {
					item.HeightRatio /= 2
				}
			case row:
				rows = true
				YRatio += child.ratio
				if cols {
					item.WidthRatio /= 2
				}
			}

			self.setHelper(child, item.WidthRatio, item.HeightRatio)
		}
	}
}

func (self *Grid) Draw(buf *Buffer) {
	width := float64(self.Dx()) + 1
	height := float64(self.Dy()) + 1

	for _, item := range self.Items {
		entry, _ := item.Entry.(Drawable)

		x := int(width*item.XRatio) + self.Min.X
		y := int(height*item.YRatio) + self.Min.Y
		w := int(width * item.WidthRatio)
		h := int(height * item.HeightRatio)

		if x+w > self.Dx() {
			w--
		}
		if y+h > self.Dy() {
			h--
		}

		entry.SetRect(x, y, x+w, y+h)

		entry.Lock()
		entry.Draw(buf)
		entry.Unlock()
	}
}
