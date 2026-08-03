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

// filterUnknownFlags returns args with any flags not defined in fs removed,
// recording each removed flag name in u. It lets non-strict parsing tolerate
// unknown CLI flags instead of letting the flag package abort. A space-
// separated value of an unknown flag is dropped too, since Loki takes no
// positional arguments and leaving a stray token would terminate flag parsing.
func filterUnknownFlags(fs *flag.FlagSet, args []string, u *UnknownFields) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append(out, args[i:]...)
		}

		name, ok := flagName(arg)
		if !ok {
			out = append(out, arg)
			continue
		}

		hasValue := strings.ContainsRune(arg, '=')
		if f := fs.Lookup(name); f != nil {
			out = append(out, arg)
			// A known non-boolean flag in `-name value` form consumes the next
			// token as its value; keep them together so filtering an adjacent
			// unknown flag cannot separate them.
			if !hasValue && !isBoolFlag(f) && i+1 < len(args) {
				out = append(out, args[i+1])
				i++
			}
			continue
		}

		u.add(name)
		if !hasValue && i+1 < len(args) {
			if _, isFlag := flagName(args[i+1]); !isFlag && args[i+1] != "--" {
				i++
			}
		}
	}
	return out
}

// flagName extracts the flag name from a CLI token, mirroring the flag
// package's own parsing of `-name`, `--name`, and `-name=value` forms. It
// returns false for non-flag tokens and the `--` terminator.
func flagName(arg string) (string, bool) {
	if len(arg) < 2 || arg[0] != '-' {
		return "", false
	}
	numMinus := 1
	if arg[1] == '-' {
		numMinus = 2
	}
	name := arg[numMinus:]
	if len(name) == 0 || name[0] == '-' || name[0] == '=' {
		return "", false
	}
	if eq := strings.IndexByte(name, '='); eq >= 0 {
		name = name[:eq]
	}
	return name, true
}

func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
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
