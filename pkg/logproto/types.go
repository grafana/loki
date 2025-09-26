package logproto

import (
	"github.com/prometheus/common/model"
)

func (c *ChunkRef) FingerprintModel() model.Fingerprint {
	return model.Fingerprint(c.Fingerprint)
}

type ChunkRefWithSizingInfo struct {
	ChunkRef
	KB      uint32
	Entries uint32
}

// Less Compares chunks by (Fp, From, Through, checksum)
// Assumes User is equivalent
func (r ChunkRefWithSizingInfo) Less(x ChunkRefWithSizingInfo) bool {
	if r.Fingerprint != x.Fingerprint {
		return r.Fingerprint < x.Fingerprint
	}

	if r.From != x.From {
		return r.From < x.From
	}

	if r.Through != x.Through {
		return r.Through < x.Through
	}

	return r.Checksum < x.Checksum
}
