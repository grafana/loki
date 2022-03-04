// Copyright 2017 Zack Guo <zack.y.guo@gmail.com>. All rights reserved.
// Use of this source code is governed by a MIT license that can
// be found in the LICENSE file.

package termui

import (
	"fmt"
	"math"
	"reflect"

	rw "github.com/mattn/go-runewidth"
	wordwrap "github.com/mitchellh/go-wordwrap"
)

// InterfaceSlice takes an []interface{} represented as an interface{} and converts it
// https://stackoverflow.com/questions/12753805/type-converting-slices-of-interfaces-in-go
func InterfaceSlice(slice interface{}) []interface{} {
	s := reflect.ValueOf(slice)
	if s.Kind() != reflect.Slice {
		panic("InterfaceSlice() given a non-slice type")
	}

	ret := make([]interface{}, s.Len())

	for i := 0; i < s.Len(); i++ {
		ret[i] = s.Index(i).Interface()
	}

	return ret
}

// TrimString trims a string to a max length and adds '…' to the end if it was trimmed.
func TrimString(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if rw.StringWidth(s) > w {
		return rw.Truncate(s, w, string(ELLIPSES))
	}
	return s
}

func SelectColor(colors []Color, index int) Color {
	return colors[index%len(colors)]
}

func SelectStyle(styles []Style, index int) Style {
	return styles[index%len(styles)]
}

// Math ------------------------------------------------------------------------

func SumIntSlice(slice []int) int {
	sum := 0
	for _, val := range slice {
		sum += val
	}
	return sum
}

func SumFloat64Slice(data []float64) float64 {
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	return sum
}

func GetMaxIntFromSlice(slice []int) (int, error) {
	if len(slice) == 0 {
		return 0, fmt.Errorf("cannot get max value from empty slice")
	}
	var max int
	for _, val := range slice {
		if val > max {
			max = val
		}
	}
	return max, nil
}

func GetMaxFloat64FromSlice(slice []float64) (float64, error) {
	if len(slice) == 0 {
		return 0, fmt.Errorf("cannot get max value from empty slice")
	}
	var max float64
	for _, val := range slice {
		if val > max {
			max = val
		}
	}
	return max, nil
}

func GetMaxFloat64From2dSlice(slices [][]float64) (float64, error) {
	if len(slices) == 0 {
		return 0, fmt.Errorf("cannot get max value from empty slice")
	}
	var max float64
	for _, slice := range slices {
		for _, val := range slice {
			if val > max {
				max = val
			}
		}
	}
	return max, nil
}

func RoundFloat64(x float64) float64 {
	return math.Floor(x + 0.5)
}

func FloorFloat64(x float64) float64 {
	return math.Floor(x)
}

func AbsInt(x int) int {
	if x >= 0 {
		return x
	}
	return -x
}

func MinFloat64(x, y float64) float64 {
	if x < y {
		return x
	}
	return y
}

func MaxFloat64(x, y float64) float64 {
	if x > y {
		return x
	}
	return y
}

func MaxInt(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func MinInt(x, y int) int {
	if x < y {
		return x
	}
	return y
}

// []Cell ----------------------------------------------------------------------

// WrapCells takes []Cell and inserts Cells containing '\n' wherever a linebreak should go.
func WrapCells(cells []Cell, width uint) []Cell {
	str := CellsToString(cells)
	wrapped := wordwrap.WrapString(str, width)
	wrappedCells := []Cell{}
	i := 0
	for _, _rune := range wrapped {
		if _rune == '\n' {
			wrappedCells = append(wrappedCells, Cell{_rune, StyleClear})
		} else {
			wrappedCells = append(wrappedCells, Cell{_rune, cells[i].Style})
		}
		i++
	}
	return wrappedCells
}

func RunesToStyledCells(runes []rune, style Style) []Cell {
	cells := []Cell{}
	for _, _rune := range runes {
		cells = append(cells, Cell{_rune, style})
	}
	return cells
}

func CellsToString(cells []Cell) string {
	runes := make([]rune, len(cells))
	for i, cell := range cells {
		runes[i] = cell.Rune
	}
	return string(runes)
}

func TrimCells(cells []Cell, w int) []Cell {
	s := CellsToString(cells)
	s = TrimString(s, w)
	runes := []rune(s)
	newCells := []Cell{}
	for i, r := range runes {
		newCells = append(newCells, Cell{r, cells[i].Style})
	}
	return newCells
}

func SplitCells(cells []Cell, r rune) [][]Cell {
	splitCells := [][]Cell{}
	temp := []Cell{}
	for _, cell := range cells {
		if cell.Rune == r {
			splitCells = append(splitCells, temp)
			temp = []Cell{}
		} else {
			temp = append(temp, cell)
		}
	}
	if len(temp) > 0 {
		splitCells = append(splitCells, temp)
	}
	return splitCells
}

type CellWithX struct {
	X    int
	Cell Cell
}

func BuildCellWithXArray(cells []Cell) []CellWithX {
	cellWithXArray := make([]CellWithX, len(cells))
	index := 0
	for i, cell := range cells {
		cellWithXArray[i] = CellWithX{X: index, Cell: cell}
		index += rw.RuneWidth(cell.Rune)
	}
	return cellWithXArray
}
