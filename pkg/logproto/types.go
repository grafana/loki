package logproto

import (
	"github.com/prometheus/common/model"
)

func (c *ChunkRef) FingerprintModel() model.Fingerprint {
	return model.Fingerprint(c.Fingerprint)
}
