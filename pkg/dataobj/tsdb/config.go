package tsdb

import "flag"

type Config struct {
	TSDBStoragePrefix string
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.TSDBStoragePrefix, "dataobj.tsdb-storage-prefix", "index/tsdb/", "The prefix to use for the tsdb storage bucket.")
}
