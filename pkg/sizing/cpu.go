package sizing

import (
	"fmt"
	"math"

	"github.com/grafana/loki/pkg/util/flagext"
)

// CPUSize measures thousandths of cpu cores
type CPUSize uint64

func (c CPUSize) String() string {
	if c%1000 == 0 {
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

type ReadableBytes flagext.ByteSize

func (b ReadableBytes) String() string {
	order := map[int]string{
		0: "B",
		1: "KB",
		2: "MB",
		3: "GB",
		4: "TB",
	}

	degree := 0
	x := float64(b)
	for x/1024 >= 1 {
		x = x / 1024
		degree++
	}
	return fmt.Sprintf("%.2f%s", x, order[degree])
}
