package metastore

import (
	"flag"
	"fmt"
)

// Config is the configuration block for the metastore settings.
type Config struct {
	IndexStoragePrefix string `yaml:"index_storage_prefix" experimental:"true"`
	PartitionRatio     int    `yaml:"partition_ratio" experimental:"true"`
	// DataobjReadNewIndexSections enables reading new-format postings sections in index objects instead of legacy pointers sections. Default false; flipping requires a rolling restart of every Loki target that constructs an ObjectMetastore.
	DataobjReadNewIndexSections bool `yaml:"dataobj_read_new_index_sections" experimental:"true"`
}

// RegisterFlags registers the flags for the metastore settings.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	prefix := "dataobj-metastore."
	f.StringVar(&c.IndexStoragePrefix, prefix+"index-storage-prefix", "index/v0", "Experimental: A prefix to use for storing indexes in object storage. Used for testing only.")
	f.IntVar(&c.PartitionRatio, prefix+"partition-ratio", 10, "Experimental: The ratio of log partitions to metastore partitions. For example, a value of 10 means there is 1 metastore partition for every 10 log partitions.")
	f.BoolVar(&c.DataobjReadNewIndexSections, prefix+"dataobj-read-new-index-sections", false, "Experimental: When enabled, reads from new-format postings sections in index objects instead of the legacy pointers sections. Default false.")
}

// Validate validates the metastore settings.
func (c *Config) Validate() error {
	if c.PartitionRatio <= 0 {
		return fmt.Errorf("partition_ratio must be greater than 0, got %d", c.PartitionRatio)
	}
	return nil
}
