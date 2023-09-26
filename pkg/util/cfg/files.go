package cfg

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
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
// When expandEnvVars is true, variables in the supplied '.yaml\ file are expanded
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

func ConfigFileLoader(args []string, name string, strict bool) Source {
	return func(dst Cloneable) error {
		freshFlags := flag.NewFlagSet("config-file-loader", flag.ContinueOnError)

		// Ensure we register flags on a copy of the config so as to not mutate it while
		// parsing out the config file location.
		dst.Clone().RegisterFlags(freshFlags)

		usage := freshFlags.Usage
		freshFlags.Usage = func() { /* don't do anything by default, we will print usage ourselves, but only when requested. */ }

		err := freshFlags.Parse(args)
		if err == flag.ErrHelp {
			// print available parameters to stdout, so that users can grep/less it easily
			freshFlags.SetOutput(os.Stdout)
			usage()
			os.Exit(2)
		} else if err != nil {
			fmt.Fprintln(freshFlags.Output(), "Run with -help to get list of available parameters")
			os.Exit(2)
		}

		f := freshFlags.Lookup(name)
		if f == nil || f.Value.String() == "" {
			return nil
		}

		for _, val := range strings.Split(f.Value.String(), ",") {
			val := strings.TrimSpace(val)
			expandEnv := false
			expandEnvFlag := freshFlags.Lookup("config.expand-env")
			if expandEnvFlag != nil {
				expandEnv, _ = strconv.ParseBool(expandEnvFlag.Value.String()) // Can ignore error as false returned
			}
			if _, err := os.Stat(val); err == nil {
				err := YAML(val, expandEnv, strict)(dst)
				if err != nil && !expandEnv {
					err = fmt.Errorf("%w. Use `-config.expand-env=true` flag if you want to expand environment variables in your config file", err)
				}
				return err
			}
		}
		return fmt.Errorf("%s does not exist, set %s for custom config path", f.Value.String(), name)
	}
}
