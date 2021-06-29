package sizing

import (
	"fmt"
	"math"
)

// CPUSize measures thousandths of cpu cores
type CPUSize uint64

func (c *CPUSize) String() string {
	if *c%1000 == 0 {
		return fmt.Sprint(c.Cores())
	}
	return fmt.Sprintf("%vm", c.Millis())
}

// Show fractional core count
func (c *CPUSize) Millis() int {
	return int(*c)
}

// specify by thousandths of a core
func (c *CPUSize) SetMillis(n int) {
	*c = CPUSize(n)
}

// Show core count
func (c *CPUSize) Cores() int {
	return int(*c) / 1000
}

// specify by core count
func (c *CPUSize) SetCores(n float64) {
	thousandths := math.Round(n * 1000)
	*c = CPUSize(thousandths)
}

func CPUCores(n float64) (s CPUSize) {
	s.SetCores(n)
	return s
}

func CPUThousandths(n int) (s CPUSize) {
	s.SetMillis(n)
	return s
}
