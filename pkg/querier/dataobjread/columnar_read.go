package dataobjread

import (
	"os"
	"strconv"
	"sync/atomic"
)

// columnarReadEnv names the environment variable that selects the reader of the
// logs sections. Set it to false to read rows one at a time with the row
// reader. The columnar reader is the default.
const columnarReadEnv = "LOKI_DATAOBJ_COLUMNAR_READ"

var columnarRead atomic.Bool

func init() {
	enabled := true
	if value, ok := os.LookupEnv(columnarReadEnv); ok {
		if parsed, err := strconv.ParseBool(value); err == nil {
			enabled = parsed
		}
	}
	columnarRead.Store(enabled)
}

// SetColumnarRead selects the reader of the logs sections for the scans that
// start after the call, and returns the previous setting. It exists for tests
// and benchmarks that compare the two readers.
func SetColumnarRead(enabled bool) (previous bool) {
	return columnarRead.Swap(enabled)
}
