package cfg

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// DynamicCloneable must be implemented by config structs that can be dynamically unmarshalled
type DynamicCloneable interface {
	Cloneable
	ApplyDynamicConfig() Source
}

// StrictParser is implemented by config types that can toggle strict parsing.
// When StrictConfig reports true, unknown configuration options are treated as
// a fatal error; otherwise they are collected for non-strict reporting.
type StrictParser interface {
	StrictConfig() bool
}

// DynamicUnmarshal handles populating a config based on the following precedence:
// 1. Defaults provided by the `RegisterFlags` interface
// 2. Sections populated by dynamic logic. Configs passed to this function must implement ApplyDynamicConfig()
// 3. Any config options specified directly in the config file
// 4. Any config options specified on the command line.
//
// Strictness is resolved up front from the CLI flags. In strict mode (the
// default) an unknown YAML field fails fast with the decoder's native error,
// which includes the config file path, line number, and parent type, and an
// unknown CLI flag is reported after parsing. In non-strict mode unknown fields
// and flags are collected into the returned UnknownFields so the caller can
// report them once the logger and metrics registry are initialized.
func DynamicUnmarshal(dst DynamicCloneable, args []string, fs *flag.FlagSet) (*UnknownFields, error) {
	unknown := &UnknownFields{}

	// Discover the defined flags on a throwaway config clone so unknown CLI
	// flags can be filtered out before any parsing. This mirrors how
	// ConfigFileLoader enumerates flags and prevents the flag package from
	// aborting on unknown flags.
	clone := dst.Clone()
	known := flag.NewFlagSet("known-flags", flag.ContinueOnError)
	known.SetOutput(io.Discard)
	clone.RegisterFlags(known)
	args = filterUnknownFlags(known, args, unknown)

	// Resolve the effective strict setting from the (filtered) CLI flags before
	// deciding how to parse the config file.
	strict := true
	if sp, ok := clone.(StrictParser); ok {
		// args no longer contain unknown flags, so parsing only fails on
		// -h/-help, which is handled later by ConfigFileLoader; ignore it here.
		_ = known.Parse(args)
		strict = sp.StrictConfig()
	}

	// In strict mode unknown YAML fields must fail fast with the decoder's
	// native, fully contextual error, so no collector is passed to the YAML
	// loader. In non-strict mode they are collected for deferred reporting.
	var yamlUnknown *UnknownFields
	if !strict {
		yamlUnknown = unknown
	}

	err := Unmarshal(dst,
		// First populate the config with defaults including flags from the command line
		Defaults(fs),
		// Next populate the config from the config file, we do this to populate the `common`
		// section of the config file by taking advantage of the code in ConfigFileLoader which will load
		// and process the config file.
		ConfigFileLoader(args, "config.file", strict, yamlUnknown),
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
		ConfigFileLoader(args, "config.file", false, nil),
		// Load the flags again, this will supersede anything set from config file with flags from the command line.
		Flags(args, fs),
	)
	if err != nil {
		return unknown, err
	}

	// Unknown YAML fields already failed fast above in strict mode. Unknown CLI
	// flags are filtered before parsing, so their strictness is enforced here.
	if strict && unknown.Len() > 0 {
		return unknown, fmt.Errorf("found %d unknown CLI flag(s): %s", unknown.Len(), strings.Join(unknown.List(), ", "))
	}
	return unknown, nil
}
