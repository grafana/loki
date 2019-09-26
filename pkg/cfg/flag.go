package cfg

import (
	"flag"
	"os"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/pkg/errors"
)

// Defaults registers flags using dst as the flagext.Registerer
func Defaults() Source {
	return func(dst interface{}) error {
		r, ok := dst.(flagext.Registerer)
		if !ok {
			return errors.New("dst does not satisfy flagext.Registerer")
		}

		// already sets the defaults on r
		r.RegisterFlags(flag.CommandLine)
		return nil
	}
}

// Flags parses the flag from the command line, setting only user-supplied
// values on the flagext.Registerer passed to Defaults()
func Flags() Source {
	return dFlags(os.Args[1:])
}

// dFlags parses the flagset, applying all values set on the slice
func dFlags(args []string) Source {
	return func(dst interface{}) error {
		// parse the final flagset
		return flag.CommandLine.Parse(args)
	}
}
