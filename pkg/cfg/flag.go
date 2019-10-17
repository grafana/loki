package cfg

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/pkg/errors"
)

// Defaults registers flags to the command line using dst as the
// flagext.Registerer
func Defaults() Source {
	return dDefaults(flag.CommandLine)
}

// dDefaults registers flags to the flagSet using dst as the flagext.Registerer
func dDefaults(fs *flag.FlagSet) Source {
	return func(dst interface{}) error {
		r, ok := dst.(flagext.Registerer)
		if !ok {
			return errors.New("dst does not satisfy flagext.Registerer")
		}

		// already sets the defaults on r
		r.RegisterFlags(fs)
		return nil
	}
}

// Flags parses the flag from the command line, setting only user-supplied
// values on the flagext.Registerer passed to Defaults()
func Flags() Source {
	flag.Usage = categorizedUsage(flag.CommandLine)
	return dFlags(flag.CommandLine, os.Args[1:])
}

// dFlags parses the flagset, applying all values set on the slice
func dFlags(fs *flag.FlagSet, args []string) Source {
	return func(dst interface{}) error {
		// parse the final flagset
		return fs.Parse(args)
	}
}

func categorizedUsage(fs *flag.FlagSet) func() {
	categories := make(map[string][]string)
	return func() {
		if fs.Name() == "" {
			fmt.Fprintf(fs.Output(), "Usage:\n")
		} else {
			fmt.Fprintf(fs.Output(), "Usage of %s:\n", fs.Name())
		}

		fs.VisitAll(func(f *flag.Flag) {
			id := ""
			if strings.Contains(f.Name, ".") {
				id = strings.Split(f.Name, ".")[0]
			}

			kind, usage := flag.UnquoteUsage(f)
			if kind != "" {
				kind = " " + kind
			}
			def := f.DefValue
			if def != "" {
				def = fmt.Sprintf(" (default %s)", def)
			}
			categories[id] = append(categories[id], fmt.Sprintf("   -%s%s:\n      %s%s", f.Name, kind, usage, def))
		})

		for name, flags := range categories {
			if len(flags) == 1 {
				categories[""] = append(categories[""], flags[0])
				delete(categories, name)
			}
		}

		for name := range categories {
			sort.Strings(categories[name])
		}

		for _, u := range categories[""] {
			fmt.Fprintln(fs.Output(), u)
		}
		fmt.Fprintln(fs.Output())

		keys := make([]string, 0, len(categories))
		for k := range categories {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, name := range keys {
			if name == "" {
				continue
			}
			fmt.Fprintf(fs.Output(), " %s:\n", strings.Title(name))
			for _, u := range categories[name] {
				fmt.Fprintln(fs.Output(), u)
			}
			fmt.Fprintln(fs.Output())
		}
	}
}
