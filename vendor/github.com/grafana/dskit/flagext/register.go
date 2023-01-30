package flagext

import (
	"flag"

	"github.com/go-kit/log"
)

// Registerer is a thing that can RegisterFlags
type Registerer interface {
	RegisterFlags(*flag.FlagSet)
}

// RegistererWithLogger is a thing that can RegisterFlags with a Logger
type RegistererWithLogger interface {
	RegisterFlags(*flag.FlagSet, log.Logger)
}

// RegisterFlags registers flags with the provided Registerers
func RegisterFlags(rs ...Registerer) {
	for _, r := range rs {
		r.RegisterFlags(flag.CommandLine)
	}
}

// RegisterFlagsWithLogger registers flags with the provided Registerers
func RegisterFlagsWithLogger(logger log.Logger, rs ...interface{}) {
	for _, v := range rs {
		switch r := v.(type) {
		case Registerer:
			r.RegisterFlags(flag.CommandLine)
		case RegistererWithLogger:
			r.RegisterFlags(flag.CommandLine, logger)
		default:
			panic("RegisterFlagsWithLogger must be passed a Registerer or RegistererWithLogger")
		}
	}
}

// DefaultValues initiates a set of configs (Registerers) with their defaults.
func DefaultValues(rs ...interface{}) {
	fs := flag.NewFlagSet("", flag.PanicOnError)
	logger := log.NewNopLogger()
	for _, v := range rs {
		switch r := v.(type) {
		case Registerer:
			r.RegisterFlags(fs)
		case RegistererWithLogger:
			r.RegisterFlags(fs, logger)
		default:
			panic("RegisterFlagsWithLogger must be passed a Registerer")
		}
	}
	_ = fs.Parse([]string{})
}
