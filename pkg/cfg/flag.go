package cfg

import (
	"flag"
	"os"

	"github.com/cortexproject/cortex/pkg/util/flagext"
)

// Defaults registers fs (shared with Flags()) with dst as the
// flagext.Registerer Flags are also copied read-only to the global flag-set so
// that intermediate uses of flag.Parse() work just fine
func Defaults(fs *flag.FlagSet) Source {
	return func(dst interface{}) error {
		r, ok := dst.(flagext.Registerer)
		if !ok {
			panic("no")
		}

		// already sets the defaults on r
		r.RegisterFlags(fs)

		// copy it all to the global flagset for help messages
		flag.CommandLine = flag.NewFlagSet(os.Args[0]+" (cfg/global)", flag.ExitOnError)

		fs.VisitAll(func(f *flag.Flag) {
			flag.Var(discardValue{f.Value.String()}, f.Name, f.Usage)
		})
		return nil
	}
}

// Flags parses the internal flagset, applying all values set on the command line
func Flags(fs *flag.FlagSet) Source {
	return dFlags(fs, os.Args[1:])
}

// dFlags parses the internal flagset, applying all values set on slice
func dFlags(fs *flag.FlagSet, args []string) Source {
	return func(dst interface{}) error {
		// copy everything intermediate from the global set for help messages
		flag.VisitAll(func(f *flag.Flag) {
			if fs.Lookup(f.Name) == nil {
				fs.Var(discardValue{f.Value.String()}, f.Name, f.Usage)
			}
		})

		// parse the final flagset
		fs.Parse(args)
		return nil
	}
}

// discardValue implements flag.Value, but discards any changes This is required
// when you want a FlagSet to include helps from another one, without affecting
// the actual values
type discardValue struct{ def string }

func (d discardValue) String() string {
	return d.def
}

func (d discardValue) Set(string) error {
	return nil
}
