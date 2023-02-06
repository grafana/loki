package main

import (
	"context"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/stores/tsdb"
)

func main() {

	t, _, err := tsdb.NewTSDBIndexFromFile("./29.tsdb")
	if err != nil {
		panic(err)
	}

	err = t.MoreStats(context.Background(), labels.MustNewMatcher(labels.MatchEqual, "", ""))
	if err != nil {
		panic(err)
	}

}
