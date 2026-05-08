package cfg

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/drone/envsubst"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

// JSON returns a Source that opens the supplied `.json` file and loads it.
func JSON(f *string) Source {
	return func(dst Cloneable) error {
		if f == nil {
			return nil
		}

		j, err := os.ReadFile(*f)
		if err != nil {
			return err
		}

		err = dJSON(j)(dst)
		return errors.Wrap(err, *f)
	}
}

// dJSON returns a JSON source and allows dependency injection
func dJSON(y []byte) Source {
	return func(dst Cloneable) error {
		return json.Unmarshal(y, dst)
	}
}

// YAML returns a Source that opens the supplied `.yaml` file and loads it.
// When expandEnvVars is true, variables in the supplied '.yaml' file are expanded
// using https://pkg.go.dev/github.com/drone/envsubst?tab=overview
func YAML(f string, expandEnvVars bool, strict bool) Source {
	return func(dst Cloneable) error {
		y, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if expandEnvVars {
			s, err := envsubst.EvalEnv(string(y))
			if err != nil {
				return err
			}
			y = []byte(s)
		}
		if strict {
			err = dYAMLStrict(y)(dst)
		} else {
			err = dYAML(y)(dst)
		}

		return errors.Wrap(err, f)
	}
}

// dYAMLStrict returns a YAML source and allows dependency injection
func dYAMLStrict(y []byte) Source {
	return func(dst Cloneable) error {
		return yaml.UnmarshalStrict(y, dst)
	}
}

// dYAML is the same as dYAMLStrict but with non strict unmarshaling
func dYAML(y []byte) Source {
	return func(dst Cloneable) error {
		return yaml.Unmarshal(y, dst)
	}
}

// FindConfigFileFromArgs extracts the config file path and expand-env flag from
// args. Only these two flags are registered, so any other flags in args are
// intentionally skipped: the goal is solely to locate the config file before
// full flag parsing begins, not to validate or interpret the full flag set.
// Unknown flags are therefore not an error here; they will be caught later
// during normal flag parsing.
func FindConfigFileFromArgs(args []string, name string) (configFile string, expandEnv bool) {
	fs := flag.NewFlagSet("find-config-file", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&configFile, name, "", "")
	fs.BoolVar(&expandEnv, "config.expand-env", false, "")

	remaining := args
	for len(remaining) > 0 {
		// Parse as many flags as possible. On error, fs.Args() returns the unparsed
		// remainder starting after the unknown flag name (Go's flag package advances
		// past the flag name before checking whether it is known). If that next element
		// looks like a value rather than a flag (does not start with "-"), skip it too,
		// so that a flag-value pair like "-target ingester" does not leave a bare
		// "ingester" that stops the parser.
		//
		// Exception: malformed flag syntax (e.g. "---foo", "-=val") causes Go's parser
		// to return an error WITHOUT advancing, so fs.Args() equals remaining. In that
		// case force-advance by one to avoid an infinite loop, then apply the same
		// value-skip in case the malformed flag also has a trailing value token.
		before := remaining
		if err := fs.Parse(remaining); err == nil {
			break
		}
		remaining = fs.Args()
		if len(remaining) == len(before) {
			remaining = remaining[1:]
		}
		if len(remaining) > 0 && !strings.HasPrefix(remaining[0], "-") {
			remaining = remaining[1:]
		}
	}
	return
}

func ConfigFileLoader(args []string, name string, strict bool) Source {
	return func(dst Cloneable) error {
		configFile, expandEnv := FindConfigFileFromArgs(args, name)
		if configFile == "" {
			return nil
		}

		for _, val := range strings.Split(configFile, ",") {
			val := strings.TrimSpace(val)
			if _, err := os.Stat(val); err == nil {
				err := YAML(val, expandEnv, strict)(dst)
				if err != nil && !expandEnv {
					err = fmt.Errorf("%w. Use `-config.expand-env=true` flag if you want to expand environment variables in your config file", err)
				}
				return err
			}
		}
		return fmt.Errorf("%s does not exist, set %s for custom config path", configFile, name)
	}
}
