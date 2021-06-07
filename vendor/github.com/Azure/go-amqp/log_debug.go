// +build debug

package amqp

import "log"
import "os"
import "strconv"

var (
	debugLevel = 1
	logger     = log.New(os.Stderr, "", log.Lmicroseconds)
)

func init() {
	level, err := strconv.Atoi(os.Getenv("DEBUG_LEVEL"))
	if err != nil {
		return
	}

	debugLevel = level
}

func debug(level int, format string, v ...interface{}) {
	if level <= debugLevel {
		logger.Printf(format, v...)
	}
}
