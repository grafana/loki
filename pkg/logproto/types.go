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
func (c *ChunkRef) Less(x ChunkRef) bool {
	if c.Fingerprint != x.Fingerprint {
		return c.Fingerprint < x.Fingerprint
	}

	if c.From != x.From {
		return c.From < x.From
	}

	if c.Through != x.Through {
		return c.Through < x.Through
	}

	return c.Checksum < x.Checksum
}
