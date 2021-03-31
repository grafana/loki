package retention

import (
	"time"

	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/common/model"
)

type fakeRule struct {
	streams []StreamRule
	tenants map[string]time.Duration
}

func (f fakeRule) PerTenant(userID string) time.Duration {
	return f.tenants[userID]
}

func (f fakeRule) PerStream() []StreamRule {
	return f.streams
}

func newChunkRef(userID, labels string, from, through model.Time) ChunkRef {
	lbs, err := logql.ParseLabels(labels)
	if err != nil {
		panic(err)
	}
	return ChunkRef{
		UserID:   []byte(userID),
		SeriesID: labelsSeriesID(lbs),
		From:     from,
		Through:  through,
	}
}
