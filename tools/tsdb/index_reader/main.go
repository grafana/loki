package main

import (
	"context"
	"fmt"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"time"
)

func main() {
	path := ``
	idx, _, err := tsdb.NewTSDBIndexFromFile(path)
	if err != nil {
		panic(err)
	}

	// "2006-01-02 15:04:05"
	from, err := time.Parse(time.DateTime, "2023-12-01 00:00:00")
	if err != nil {
		panic(err)
	}

	to, err := time.Parse(time.DateTime, "2023-12-31 23:59:59")
	if err != nil {
		panic(err)
	}

	fmt.Println("getting chunk refs")
	var refs []tsdb.ChunkRef
	chRefs, err := idx.GetChunkRefs(
		context.Background(),
		"<tenant>",
		model.TimeFromUnix(from.Unix()),
		model.TimeFromUnix(to.Unix()),
		refs,
		nil,
		labels.MustNewMatcher(labels.MatchEqual, "", ""),
	)
	if err != nil {
		panic(err)
	}

	// chunks are in reverse order?
	firstChunk := chRefs[len(chRefs)-1]
	fmt.Println(firstChunk.Start.Time())
	fmt.Println(firstChunk.End.Time())
	fmt.Println(ExternalKey(firstChunk))

	lastChunk := chRefs[0]
	fmt.Println(lastChunk.Start.Time())
	fmt.Println(lastChunk.End.Time())
	fmt.Println(ExternalKey(lastChunk))

	//for _, c := range chRefs {
	//	fmt.Println(newerExternalKey(c))
	//}
}

func ExternalKey(ref tsdb.ChunkRef) string {
	// Print the fingerprint as a string here, it's the right format
	return fmt.Sprintf("%s/%s/%x:%x:%x", ref.User, ref.Fingerprint, int64(ref.Start), int64(ref.End), ref.Checksum)
}
