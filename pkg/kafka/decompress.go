package kafka

import (
	"math"

	"github.com/twmb/franz-go/pkg/kgo"
)

// DefaultMaxDecompressBatchBytes is the largest size a fetched batch may
// decompress to. franz-go >= 1.22 defaults to 1 GiB and stops consuming the
// partition on a bigger batch. This keeps the pre-1.22 limit, which is also
// the highest value franz-go accepts.
const DefaultMaxDecompressBatchBytes = math.MaxInt32

const maxDecompressBatchBytesUsage = "The largest size a fetched Kafka batch may decompress to. " +
	"A bigger batch stops consuming the partition. The default keeps the limit franz-go had before 1.22. " +
	"0 to use the franz-go default."

// MaxDecompressBatchBytesOpts returns the client option that sets the
// decompress limit, or none if maxBytes is 0 or less. Values above
// DefaultMaxDecompressBatchBytes are capped at it, for callers that don't
// validate their config.
func MaxDecompressBatchBytesOpts(maxBytes int) []kgo.Opt {
	if maxBytes <= 0 {
		return nil
	}
	return []kgo.Opt{kgo.MaxDecompressBatchBytes(min(maxBytes, DefaultMaxDecompressBatchBytes))}
}
