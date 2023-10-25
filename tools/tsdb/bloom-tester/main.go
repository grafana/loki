package main

import (
	"fmt"
	"github.com/go-kit/log/level"
	util_log "github.com/grafana/loki/pkg/util/log"
	"os"
	"strings"
)

// go build ./tools/tsdb/bloom-tester && HOSTNAME="bloom-tester-121" NUM_TESTERS="128" BUCKET="19625" DIR=/Users/progers/dev/bloom WRITE_MODE="false" BUCKET_PREFIX="new-experiments" ./tools/tsdb/bloom-tester/bloom-tester --config.file=/Users/progers/dev/bloom/config.yaml
func main() {
	writeMode := os.Getenv("WRITE_MODE")

	if strings.EqualFold(writeMode, "true") {
		fmt.Println("write mode")
		level.Info(util_log.Logger).Log("msg", "starting up in write mode")
		//time.Sleep(3000 * time.Second)
		execute()
	} else {
		fmt.Println("read mode")
		level.Info(util_log.Logger).Log("msg", "starting up in read mode")
		//time.Sleep(3000 * time.Second)

		executeRead()
	}
}
