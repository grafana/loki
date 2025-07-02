package main

import (
	"github.com/grafana/loki/v3/pkg/plugins/guest"
	pushrequest "github.com/grafana/loki/v3/pkg/plugins/pushrequest/guest"
)

func main() {}

//export process_push_request
func ProcessPushRequest(reqPtr uint64) {
	pushrequest.IterateLines(reqPtr)
}

//export process_line
func ProcessLine(reqPtr, streamIdx, entryIdx uint64, linePtr uint64) {
	line := guest.ReadString(linePtr)

	if len(line) == 0 {
		return
	}

	pushrequest.AddStructuredMetadata(reqPtr, streamIdx, entryIdx, "new_metadata", "I added this")
}
