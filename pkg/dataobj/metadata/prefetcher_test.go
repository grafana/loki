package metadata

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrefetcherConfig_Validate(t *testing.T) {
	// valid returns an enabled config with the default values.
	valid := func(mutate func(*PrefetcherConfig)) PrefetcherConfig {
		var cfg PrefetcherConfig
		cfg.RegisterFlagsWithPrefix("test.", flag.NewFlagSet("test", flag.PanicOnError))
		cfg.Enabled = true
		if mutate != nil {
			mutate(&cfg)
		}
		return cfg
	}

	tests := map[string]struct {
		cfg     PrefetcherConfig
		wantErr string
	}{
		"accepts the default values": {cfg: valid(nil)},
		"accepts a disabled config with invalid values": {
			cfg: PrefetcherConfig{Compression: "bogus"},
		},
		"rejects an unknown compression": {
			cfg:     valid(func(c *PrefetcherConfig) { c.Compression = "gzip" }),
			wantErr: `unsupported metadata prefetcher compression "gzip"`,
		},
		"rejects a compression ratio below one": {
			cfg:     valid(func(c *PrefetcherConfig) { c.MinCompressionRatio = 0.5 }),
			wantErr: "the metadata prefetcher minimum compression ratio must be at least 1",
		},
		"rejects a head size below 16 KiB": {
			cfg:     valid(func(c *PrefetcherConfig) { c.HeadBytes = 16*1024 - 1 }),
			wantErr: "the metadata prefetcher head bytes must be at least 16384",
		},
		"accepts a head size of 16 KiB": {
			cfg: valid(func(c *PrefetcherConfig) { c.HeadBytes = 16 * 1024 }),
		},
		"rejects a max region size of zero": {
			cfg:     valid(func(c *PrefetcherConfig) { c.MaxRegionBytes = 0 }),
			wantErr: "the metadata prefetcher max region bytes must be positive",
		},
		"rejects a load timeout of zero": {
			cfg:     valid(func(c *PrefetcherConfig) { c.LoadTimeout = 0 }),
			wantErr: "the metadata prefetcher load timeout must be positive",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := tc.cfg.Validate()

			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

func TestPrefetchSet_prefetchedRangesFor(t *testing.T) {
	fileMetadata := PrefetchedRange{Offset: 0, Data: []byte("fm")}
	coreFirst := PrefetchedRange{Offset: 2, Data: []byte("core")}
	coreLast := PrefetchedRange{Offset: 30, Data: []byte("core2")}
	message := PrefetchedRange{Offset: 10, Data: []byte("msg")}
	traceID := PrefetchedRange{Offset: 20, Data: []byte("tid")}
	level := PrefetchedRange{Offset: 40, Data: []byte("lvl")}

	groups := map[prefetchedRangesID]PrefetchedRanges{
		{Kind: kindFileMetadata}:                   {fileMetadata},
		{Kind: kindCore}:                           {coreFirst, coreLast},
		{Kind: kindMessage}:                        {message},
		{Kind: kindColumn, ColumnName: "trace_id"}: {traceID},
		{Kind: kindColumn, ColumnName: "level"}:    {level},
	}
	region := []byte("the whole region")

	tests := map[string]struct {
		set  PrefetchSet
		want PrefetchedRanges
	}{
		"returns the file metadata and the core groups for the zero value": {
			set:  PrefetchSet{},
			want: PrefetchedRanges{fileMetadata, coreFirst, coreLast},
		},
		"adds the message group when the set reads the message": {
			set:  PrefetchSet{Message: true},
			want: PrefetchedRanges{fileMetadata, coreFirst, message, coreLast},
		},
		"adds the groups of the named columns only": {
			set:  PrefetchSet{MetadataNames: []string{"trace_id"}},
			want: PrefetchedRanges{fileMetadata, coreFirst, traceID, coreLast},
		},
		"sorts the ranges of all the groups together": {
			set:  PrefetchSet{Message: true, MetadataNames: []string{"level", "trace_id"}},
			want: PrefetchedRanges{fileMetadata, coreFirst, message, traceID, coreLast, level},
		},
		"skips a named column that the object does not have": {
			set:  PrefetchSet{MetadataNames: []string{"missing"}},
			want: PrefetchedRanges{fileMetadata, coreFirst, coreLast},
		},
		"returns the whole region as one range when the set reads every column": {
			set:  PrefetchSet{AllMetadata: true, Message: true, MetadataNames: []string{"trace_id"}},
			want: PrefetchedRanges{{Offset: 0, Data: region}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.set.prefetchedRangesFor(groups, region)

			require.Equal(t, tc.want, got)
			require.True(t, got.Valid())
		})
	}
}
