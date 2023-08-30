package filter

import (
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

type Func func(ts time.Time, s string, nonIndexedLabels ...labels.Label) bool
