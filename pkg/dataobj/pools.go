package dataobj

import (
	"sync"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
)

// encoderPool holds a pool of [encoding.Encoder] instances. Callers must
// always reset pooled encoders before use.
var encoderPool = sync.Pool{
	New: func() any {
		return encoding.NewEncoder(nil)
	},
}
