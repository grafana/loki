package main

import (
	"context"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/stores/tsdb"
)

func main() {

	t, _, err := tsdb.NewTSDBIndexFromFile("19388.tsdb")
	if err != nil {
		panic(err)
	}

	err = t.MoreStats(context.Background(), labels.MustNewMatcher(labels.MatchRegexp, "route_paths_1", ".+"))
	if err != nil {
		panic(err)
	}

	//err = t.LabelValueDistribution(context.Background(), "route_paths_1", labels.MustNewMatcher(labels.MatchEqual, "stream_filter", "JPAGEC"))
	//if err != nil {
	//	panic(err)
	//}

}
