package cfg

import (
	"flag"
)

// DynamicCloneable must be implemented by config structs that can be dynamically unmarshalled
type DynamicCloneable interface {
	Cloneable
	ApplyDynamicConfig() Source
}

// FileLoader creates a Source that locates and decodes a config file from args.
// args are the CLI arguments, name is the config-file flag name (e.g. "config.file"),
// and strict controls whether unknown YAML fields are rejected.
type FileLoader func(args []string, name string, strict bool) Source

// DynamicUnmarshal handles populating a config based on the following precedence:
// 1. Defaults provided by the `RegisterFlags` interface
// 2. Sections populated by dynamic logic. Configs passed to this function must implement ApplyDynamicConfig()
// 3. Any config options specified directly in the config file
// 4. Any config options specified on the command line.
//
// An optional fileLoader overrides how the config file is located and decoded on both
// passes. If omitted, ConfigFileLoader is used.
func DynamicUnmarshal(dst DynamicCloneable, args []string, fs *flag.FlagSet, fileLoader ...FileLoader) error {
	loader := FileLoader(func(args []string, name string, strict bool) Source {
		return ConfigFileLoader(args, name, strict)
	})
	if len(fileLoader) > 0 {
		loader = fileLoader[0]
	}
	return Unmarshal(dst,
		// First populate the config with defaults including flags from the command line
		Defaults(fs),
		// Next populate the config from the config file, we do this to populate the `common`
		// section of the config file by taking advantage of the code in ConfigFileLoader which will load
		// and process the config file.
		loader(args, "config.file", true),
		// Now load the flags again, this will supersede anything set from config file with flags from the command line.
		Flags(args, fs),
		// Apply any dynamic logic to set other defaults in the config. This function is called after parsing the
		// config files so that values from a common, or shared, section can be used in
		// the dynamic evaluation
		dst.ApplyDynamicConfig(),
		// Load configs from the config file a second time, this will supersede anything set by the common
		// config with values specified in the config file.
		// By loading the config file twice and unmarshaling it into the same object,
		// using strict yaml unmarshal causes an `already set in map` error with the `Clients` config,
		// because it's a map that already has the keys we are trying to unmarshal into it.
		// That is why we don't use strict for the second marshaling.
		loader(args, "config.file", false),
		// Load the flags again, this will supersede anything set from config file with flags from the command line.
		Flags(args, fs),
	)
}
