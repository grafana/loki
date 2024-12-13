package retention

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
	"unsafe"

	"github.com/prometheus/common/model"
)

// unsafeGetString is like yolostring but with a meaningful name
func unsafeGetString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf))) // #nosec G103 -- we know the string is not mutated
}

func copyFile(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}

// ExtractIntervalFromTableName gives back the time interval for which the table is expected to hold the chunks index.
func ExtractIntervalFromTableName(tableName string) model.Interval {
	interval := model.Interval{
		Start: 0,
		End:   model.Now(),
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
