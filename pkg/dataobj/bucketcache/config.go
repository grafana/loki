package bucketcache

import (
	"errors"
	"flag"
	"fmt"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

const (
	// DefaultSubrangeSize is the size of the aligned blocks the cache stores. A read fetches whole
	// blocks, so up to two blocks at its edges hold bytes it did not ask for. Small blocks keep that
	// waste low.
	DefaultSubrangeSize = 16000

	// MaxSubrangeSize is the largest subrange size that Validate accepts. A memcached server with
	// the default 1 MiB item limit stores a block of this size, with room for its header and key.
	MaxSubrangeSize = 512 << 10

	// DefaultAttributesInMemoryMaxItems is how many object attributes each cache keeps in memory.
	DefaultAttributesInMemoryMaxItems = 50000

	// DefaultMaxGetRangeSubrequests is the most requests that one read sends for the blocks that the
	// cache does not hold.
	DefaultMaxGetRangeSubrequests = 3
)

// Config configures the bucket cache of one kind of object. The cache is on when a backend is
// configured.
type Config struct {
	cache.Config `yaml:",inline"`

	SubrangeSize               int `yaml:"subrange_size" category:"experimental"`
	AttributesInMemoryMaxItems int `yaml:"attributes_in_memory_max_items" category:"experimental"`
	MaxGetRangeSubrequests     int `yaml:"max_get_range_subrequests" category:"experimental"`
}

// RegisterFlagsWithPrefix registers the flags of the cache backend and of the bucket cache.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Config.RegisterFlagsWithPrefix(prefix, "", f)
	f.IntVar(&cfg.SubrangeSize, prefix+"subrange-size", DefaultSubrangeSize, fmt.Sprintf("Size in bytes of the aligned blocks that the cache stores. Reads are split into blocks of this size. The maximum is %d.", MaxSubrangeSize))
	f.IntVar(&cfg.AttributesInMemoryMaxItems, prefix+"attributes-in-memory-max-items", DefaultAttributesInMemoryMaxItems, "Maximum number of object attributes to keep in memory. A lookup that misses goes to the cache backend. 0 disables the in-memory cache.")
	f.IntVar(&cfg.MaxGetRangeSubrequests, prefix+"max-get-range-subrequests", DefaultMaxGetRangeSubrequests, "Maximum number of requests that one read sends to object storage to fetch the blocks that the cache does not hold. When the missing blocks need more requests, the cache merges them and reads the cached blocks between them again. 0 means no limit.")
}

// Validate checks the sizes.
func (cfg *Config) Validate() error {
	if cfg.SubrangeSize <= 0 || cfg.SubrangeSize > MaxSubrangeSize {
		return fmt.Errorf("the bucket cache subrange size must be between 1 and %d", MaxSubrangeSize)
	}
	if cfg.AttributesInMemoryMaxItems < 0 {
		return errors.New("the bucket cache attributes in-memory max items must not be negative")
	}
	if cfg.MaxGetRangeSubrequests < 0 {
		return errors.New("the bucket cache max get range subrequests must not be negative")
	}
	return nil
}
