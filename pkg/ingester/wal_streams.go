package ingester

import (
	"sync"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// retiredWALStream keeps the recovery metadata of a stream removed from memory:
// its labels, for WAL records that still refer to it, and its entry counter, which
// a returning stream continues so replay doesn't drop its entries as duplicates.
type retiredWALStream struct {
	labels  labels.Labels
	entryCt int64
}

type retiredWALStreamRef struct {
	instance *instance
	fp       model.Fingerprint
	state    *retiredWALStream
}

func checkpointMutex(w WAL) *sync.RWMutex {
	if guarded, ok := w.(interface{ checkpointMutex() *sync.RWMutex }); ok {
		return guarded.checkpointMutex()
	}
	return nil
}

// forgetRetired removes only metadata retired before the checkpoint boundary.
// The new checkpoint excludes these streams and supersedes their old WAL. A
// stream recreated or retired again during checkpointing has a different state
// pointer and must survive until a later checkpoint.
func (s *streamIterator) forgetRetired() {
	for _, retired := range s.retired {
		retired.instance.retiredWALStreams.CompareAndDelete(retired.fp, retired.state)
	}
}
