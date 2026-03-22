# tdigest

This is an implementation of Ted Dunning's [t-digest](https://github.com/tdunning/t-digest/) in Go.

The implementation is based off [Derrick Burns' C++ implementation](https://github.com/derrickburns/tdigest).

## Example

```go
package main

import (
	"log"

	"github.com/influxdata/tdigest"
)

func main() {
	td := tdigest.NewWithCompression(1000)
	for _, x := range []float64{1, 2, 3, 4, 5, 5, 4, 3, 2, 1} {
		td.Add(x, 1)
	}

	// Compute Quantiles
	log.Println("50th", td.Quantile(0.5))
	log.Println("75th", td.Quantile(0.75))
	log.Println("90th", td.Quantile(0.9))
	log.Println("99th", td.Quantile(0.99))

	// Compute CDFs
	log.Println("CDF(1) = ", td.CDF(1))
	log.Println("CDF(2) = ", td.CDF(2))
	log.Println("CDF(3) = ", td.CDF(3))
	log.Println("CDF(4) = ", td.CDF(4))
	log.Println("CDF(5) = ", td.CDF(5))
}
```
