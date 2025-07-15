package cfg

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
)

// Defaults registers flags to the flagSet using dst as the flagext.Registerer
func Defaults(fs *flag.FlagSet) Source {
	return func(dst Cloneable) error {
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
func Flags(args []string, fs *flag.FlagSet) Source {
	flag.Usage = categorizedUsage(fs)
	return dFlags(fs, args)
}

// dFlags parses the flagset, applying all values set on the slice
func dFlags(fs *flag.FlagSet, args []string) Source {
	return func(_ Cloneable) error {
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
			fmt.Fprintf(fs.Output(), " %s:\n", cases.Title(language.Und, cases.NoLower).String(name))
			for _, u := range categories[name] {
				fmt.Fprintln(fs.Output(), u)
			}
			fmt.Fprintln(fs.Output())
		}
	}
}
