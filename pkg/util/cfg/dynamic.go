package cfg

import (
	"flag"
	"fmt"
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
// Unknown configuration options are always collected. When dst implements
// StrictParser and requests strict parsing, their presence is a fatal error;
// otherwise the returned UnknownFields lets the caller report them once the
// logger and metrics registry are initialized.
func DynamicUnmarshal(dst DynamicCloneable, args []string, fs *flag.FlagSet) (*UnknownFields, error) {
	unknown := &UnknownFields{}

	// Discover the defined flags on a throwaway flagset so unknown CLI flags can
	// be filtered out before any parsing. This mirrors how ConfigFileLoader
	// enumerates flags and prevents the flag package from aborting on unknown
	// flags; strictness is enforced centrally once the config is resolved.
	known := flag.NewFlagSet("known-flags", flag.ContinueOnError)
	dst.Clone().RegisterFlags(known)
	args = filterUnknownFlags(known, args, unknown)

	err := Unmarshal(dst,
		// First populate the config with defaults including flags from the command line
		Defaults(fs),
		// Next populate the config from the config file, we do this to populate the `common`
		// section of the config file by taking advantage of the code in ConfigFileLoader which will load
		// and process the config file.
		ConfigFileLoader(args, "config.file", true, unknown),
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
		// That is why we don't use strict for the second marshaling. Unknown fields were already
		// collected on the first pass, so we pass a nil collector here to avoid double counting.
		ConfigFileLoader(args, "config.file", false, nil),
		// Load the flags again, this will supersede anything set from config file with flags from the command line.
		Flags(args, fs),
	)
	if err != nil {
		return unknown, err
	}

	// Enforce strictness only after the full config is resolved, since the
	// strict toggle is itself a configuration option.
	if sp, ok := dst.(StrictParser); ok && sp.StrictConfig() && unknown.Len() > 0 {
		return unknown, fmt.Errorf("found %d unknown configuration option(s): %s", unknown.Len(), strings.Join(unknown.List(), ", "))
	}
	return unknown, nil
}
