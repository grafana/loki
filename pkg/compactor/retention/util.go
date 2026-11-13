package retention

import (
	"strconv"
	"time"
	"unsafe"

	"github.com/prometheus/common/model"
)

// unsafeGetString is like yolostring but with a meaningful name
func unsafeGetString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf))) // #nosec G103 -- we know the string is not mutated -- nosemgrep: use-of-unsafe-block
}

func unsafeGetBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s)) // #nosec G103 -- we know the string is not mutated -- nosemgrep: use-of-unsafe-block
}

// ExtractIntervalFromTableName gives back the time interval for which the table is expected to hold the chunks index.
func ExtractIntervalFromTableName(tableName string) model.Interval {
	interval := model.Interval{
		Start: 0,
		End:   model.Now(),
	}
	// The table number is the trailing %05d of the name. Anything shorter cannot
	// carry one, and slicing it out would panic; keep the unbounded interval.
	if len(tableName) < 5 {
		return interval
	}
	tableNumber, err := strconv.ParseInt(tableName[len(tableName)-5:], 10, 64)
	if err != nil {
		return interval
	}

	interval.Start = model.TimeFromUnix(tableNumber * 86400)
	// subtract a millisecond here so that interval only covers a single table since adding 24 hours ends up covering the start time of next table as well.
	interval.End = interval.Start.Add(24*time.Hour) - 1
	return interval
}
