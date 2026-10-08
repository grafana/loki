package metastore

import (
	"flag"
)

// Config is the configuration block for the metastore settings.
type Config struct {
	IndexStoragePrefix     string `yaml:"index_storage_prefix" experimental:"true"`
	ReadPostingsSections   bool   `yaml:"read_postings_sections" experimental:"true"`
	IndexReadPrefetchBytes int    `yaml:"index_read_prefetch_bytes" experimental:"true"`
}

// RegisterFlags registers the flags for the metastore settings.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	prefix := "dataobj-metastore."
	f.StringVar(&c.IndexStoragePrefix, prefix+"index-storage-prefix", "index/v0", "Experimental: A prefix to use for storing indexes in object storage. Used for testing only.")
	f.BoolVar(&c.ReadPostingsSections, prefix+"read-postings-sections", false, "Experimental: When enabled, reads from new-format postings sections in index objects instead of the streams sections. Defaults to false.")
	f.IntVar(&c.IndexReadPrefetchBytes, prefix+"index-read-prefetch-bytes", 256*1024, "Experimental: Bytes to prefetch from the head of each index object when resolving sections. A larger value serves the file and section metadata from one read instead of many small range reads. The effective minimum is 16KiB.")
}
