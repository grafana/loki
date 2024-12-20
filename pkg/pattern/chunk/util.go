package chunk

import (
	"time"

	"github.com/prometheus/common/model"
)

const (
	TimeResolution = model.Time(int64(time.Second*10) / 1e6)
	MaxChunkTime   = 15 * time.Minute
)

func TruncateTimestamp(ts, step model.Time) model.Time { return ts - ts%step }
