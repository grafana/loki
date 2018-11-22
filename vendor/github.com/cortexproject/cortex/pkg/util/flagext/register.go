package flagext

import "flag"

// Registerer is a thing that can RegisterFlags
type Registerer interface {
	RegisterFlags(*flag.FlagSet)
}

// RegisterFlags registers flags with the provided Registerers
func RegisterFlags(rs ...Registerer) {
	for _, r := range rs {
		r.RegisterFlags(flag.CommandLine)
	}
}

// DefaultValues intiates a set of configs (Registerers) with their defaults.
func DefaultValues(rs ...Registerer) {
	fs := flag.NewFlagSet("", flag.PanicOnError)
	for _, r := range rs {
		r.RegisterFlags(fs)
	}
	fs.Parse([]string{})
}
